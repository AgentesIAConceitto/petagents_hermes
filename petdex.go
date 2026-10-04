package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Petdex — bonecos da galeria pública do Hermes/Petdex (MIT, petdex.dev).
//
// FORMATO MEDIDO (01/10/2026), igual em todos os pets instalados:
//   %LOCALAPPDATA%\hermes\pets\<slug>\pet.json          → id, displayName, spritesheetPath
//   %LOCALAPPDATA%\hermes\pets\<slug>\spritesheet.webp  → folha 1536x1872 = grade 8x9
//                                                         de frames 192x208 (72 frames)
// Linhas da folha (conferido na visão): 0=repouso, 1/2=andando, 4=pulinho,
// 5=caído/cansado, 6=falando. O overlay usa esse mapa para animar por estado.
//
// Só LEITURA: nada é copiado, movido nem alterado — o pet continua sendo do Hermes.

// PetDex — o boneco da galeria, pronto para o frontend desenhar.
type PetDex struct {
	OK     bool   `json:"ok"`
	Erro   string `json:"erro"`
	Slug   string `json:"slug"`
	Nome   string `json:"nome"`
	Desc   string `json:"desc"`
	Pasta  string `json:"pasta"`
	Fw     int    `json:"fw"`
	Fh     int    `json:"fh"`
	Cols   int    `json:"cols"`
	Linhas int    `json:"linhas"`
	Sheet  string `json:"sheet"` // data:image/webp;base64,...
}

func pastaPetsHermes() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return ""
	}
	return filepath.Join(base, "hermes", "pets")
}

// petDexSlug — qual pet está ativo no Hermes (config.yaml, display.pet.slug).
// Sem parser de YAML: acompanha o aninhamento display → pet → slug.
func petDexSlug() string {
	base := os.Getenv("LOCALAPPDATA")
	if base != "" {
		if b, err := os.ReadFile(filepath.Join(base, "hermes", "config.yaml")); err == nil {
			emDisplay, emPet := false, false
			for _, ln := range strings.Split(string(b), "\n") {
				t := strings.TrimSpace(ln)
				ind := len(ln) - len(strings.TrimLeft(ln, " \t"))
				switch {
				case strings.HasPrefix(t, "display:"):
					emDisplay, emPet = true, ind == 0
				case emDisplay && strings.HasPrefix(t, "pet:"):
					emPet = true
				case emDisplay && emPet && strings.HasPrefix(t, "slug:"):
					v := strings.TrimSpace(strings.TrimPrefix(t, "slug:"))
					v = strings.Trim(v, "\"'")
					if v != "" && v != "null" {
						return v
					}
				case ind == 0 && t != "" && !strings.HasPrefix(t, "display:"):
					emDisplay, emPet = false, false
				}
			}
		}
	}
	return ""
}

// PetsInstalados — slugs dos pets já baixados na máquina.
func (a *App) PetsInstalados() string {
	pasta := pastaPetsHermes()
	ents, err := os.ReadDir(pasta)
	if err != nil {
		return "[]"
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	b, _ := json.Marshal(out)
	return string(b)
}

// PetDexAtual — devolve o pet ativo (ou o escolhido no nosso config) já com a
// folha embutida em data URL, para o frontend não precisar de caminho de arquivo.
func (a *App) PetDexAtual() string {
	falha := func(msg string) string {
		b, _ := json.Marshal(PetDex{OK: false, Erro: msg})
		return string(b)
	}

	slug := ""
	// 1) escolha nossa (se o usuário fixou um boneco no menu do overlay)
	if b, err := os.ReadFile(filepath.Join(pastaPetAgente(), "petdex.txt")); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" && v != "hermes" {
			slug = v
		}
	}
	// 2) senão, o pet ativo do Hermes
	if slug == "" {
		slug = petDexSlug()
	}
	// 3) senão, o primeiro instalado
	pastaPets := pastaPetsHermes()
	if slug == "" {
		if ents, err := os.ReadDir(pastaPets); err == nil {
			for _, e := range ents {
				if e.IsDir() {
					slug = e.Name()
					break
				}
			}
		}
	}
	if slug == "" {
		return falha("nenhum pet instalado (hermes pets install <nome>)")
	}

	pasta := filepath.Join(pastaPets, slug)
	man := map[string]any{}
	if b, err := os.ReadFile(filepath.Join(pasta, "pet.json")); err == nil {
		_ = json.Unmarshal(b, &man)
	}
	sheet := "spritesheet.webp"
	if v, ok := man["spritesheetPath"].(string); ok && v != "" {
		sheet = v
	}
	dados, err := os.ReadFile(filepath.Join(pasta, filepath.Base(sheet)))
	if err != nil {
		return falha("não achei a folha do pet " + slug + ": " + err.Error())
	}

	// O formato é fixo: 8x9 frames de 192x208 (medido em 4 pets). Se um dia
	// divergir, derivamos o tamanho do frame da própria folha (w/8, h/9).
	fw, fh, cols, linhas := 192, 208, 8, 9
	if tam := sniffWebpTamanho(dados); tam != nil && tam[0]%8 == 0 && tam[1]%9 == 0 {
		if tam[0] != 1536 || tam[1] != 1872 {
			fw, fh = tam[0]/8, tam[1]/9
			logMsg("petdex: folha fora do padrão " + fmt.Sprint(tam[0]) + "x" + fmt.Sprint(tam[1]) +
				" — usando frame " + fmt.Sprint(fw) + "x" + fmt.Sprint(fh))
		}
	}

	out := PetDex{
		OK: true, Slug: slug,
		Nome:   str(man["displayName"], slug),
		Desc:   str(man["description"], ""),
		Pasta:  pasta,
		Fw:     fw, Fh: fh, Cols: cols, Linhas: linhas,
		Sheet: "data:image/webp;base64," + base64.StdEncoding.EncodeToString(dados),
	}
	b, _ := json.Marshal(out)
	logMsg("petdex: boneco carregado " + slug + " (" + str(man["displayName"], slug) + "), folha " +
		fmt.Sprint(len(dados)/1024) + " KB")
	return string(b)
}

func str(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

var reWebpVP8X = regexp.MustCompile(`VP8X`)

// sniffWebpTamanho — lê largura/altura de um WebP estendido (VP8X) sem libs.
// Devolve nil se não reconhecer (aí o formato padrão 8x9/192x208 é usado).
func sniffWebpTamanho(b []byte) []int {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return nil
	}
	if !reWebpVP8X.Match(b) {
		return nil
	}
	// VP8X: bytes 24..26 = largura-1 (24 bits LE), 27..29 = altura-1
	w := int(b[24]) | int(b[25])<<8 | int(b[26])<<16
	h := int(b[27]) | int(b[28])<<8 | int(b[29])<<16
	return []int{w + 1, h + 1}
}
