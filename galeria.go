package main

// Galeria de bonecos (Petdex) — para o painel de escolha DENTRO do overlay.
//
// De onde vêm os dados (verificado 02/10/2026):
//   https://petdex.dev/api/manifest  → { total, pets: [{ slug, displayName, kind,
//        spritesheetUrl, petJsonUrl, zipUrl, submittedBy }] }
//   assets.petdex.dev               → os arquivos de cada boneco (webp + json)
//
// O que este arquivo faz:
//   * baixa e CACHEIA o catálogo (7 dias) para o painel funcionar offline depois;
//   * lista os bonecos JÁ instalados na máquina (com miniatura servida localmente);
//   * baixa um boneco da galeria direto para a pasta de pets do Hermes;
//   * grava a escolha em %LOCALAPPDATA%\petagent\petdex.txt.
//
// Nada aqui depende de navegador nem de abrir página web: é o app falando direto
// com o CDN do Petdex.

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	urlCatalogo = "https://petdex.dev/api/manifest"
	// O Petdex responde 403 sem User-Agent (verificado 02/10/2026): requisição com
	// UA padrão do Go/urllib é recusada. Sempre mandar um UA próprio.
	agenteHTTP = "PetAgent/0.1 (boneco do Hermes)"
)

// portaPets — porta local (127.0.0.1) que serve as folhas dos bonecos instalados
// para o painel desenhar as miniaturas. Sem isso, teríamos que mandar ~1,3 MB por
// boneco em base64 para o frontend.
var portaPets int

// ItemGaleria — um boneco na lista (instalado ou só do catálogo).
type ItemGaleria struct {
	Slug   string `json:"slug"`
	Nome   string `json:"nome"`
	Tipo   string `json:"tipo"`
	Por    string `json:"por"`
	Sprite string `json:"sprite"` // URL no CDN do Petdex
	Local  bool   `json:"local"`  // já instalado nesta máquina
}

// PetGaleria — resposta do painel.
type PetGaleria struct {
	OK      bool          `json:"ok"`
	Erro    string        `json:"erro"`
	Total   int           `json:"total"`
	TemMais bool          `json:"tem_mais"`
	Base    string        `json:"base"` // ex.: http://127.0.0.1:51234 (miniaturas locais)
	Itens   []ItemGaleria `json:"itens"`
}

func sobeServidorPets() {
	base := pastaPetsHermes()
	if base == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/sheet/", func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimPrefix(r.URL.Path, "/sheet/")
		// trava de travessia de caminho: só slug simples
		if slug == "" || strings.ContainsAny(slug, "/\\.") || strings.Contains(slug, "..") {
			http.NotFound(w, r)
			return
		}
		arq := filepath.Join(base, slug, "spritesheet.webp")
		if _, err := os.Stat(arq); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("Cache-Control", "max-age=1800")
		http.ServeFile(w, r, arq)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logMsg("petserver: nao subiu: " + err.Error())
		return
	}
	portaPets = ln.Addr().(*net.TCPAddr).Port
	go func() { _ = http.Serve(ln, mux) }()
	logMsg("petserver: http://127.0.0.1:" + fmt.Sprint(portaPets) + "/sheet/<boneco> (so local)")
}

// catalogoPetdex — baixa o catálogo (ou usa o cache de até 7 dias).
func catalogoPetdex() ([]map[string]any, error) {
	cache := filepath.Join(pastaPetAgente(), "galeria.json")
	leCache := func() []map[string]any {
		b, err := os.ReadFile(cache)
		if err != nil {
			return nil
		}
		var d struct {
			Pets []map[string]any `json:"pets"`
		}
		if json.Unmarshal(b, &d) != nil {
			return nil
		}
		return d.Pets
	}
	if st, err := os.Stat(cache); err == nil && time.Since(st.ModTime()) < 7*24*time.Hour {
		if p := leCache(); len(p) > 0 {
			return p, nil
		}
	}
	cli := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("GET", urlCatalogo, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", agenteHTTP)
	resp, err := cli.Do(req)
	if err != nil {
		if p := leCache(); len(p) > 0 { // sem rede: usa o catálogo velho
			return p, nil
		}
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var d struct {
		Pets []map[string]any `json:"pets"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	_ = os.WriteFile(cache, b, 0o644)
	logMsg("galeria: catalogo atualizado (" + fmt.Sprint(len(d.Pets)) + " bonecos)")
	return d.Pets, nil
}

func campo(m map[string]any, k string) string {
	if s, ok := m[k].(string); ok {
		return s
	}
	return ""
}

// PetGaleria — lista para o painel.
//
//	termo == ""  → os bonecos JÁ INSTALADOS na máquina (o "vetor" entra no front)
//	termo == "*" → navega a galeria inteira, em páginas
//	outro termo  → busca no catálogo por nome, slug ou tipo
//
// `deslocamento` pagina os resultados (a galeria tem ~4.900 bonecos).
func (a *App) PetGaleria(termo string, deslocamento int) string {
	const porPagina = 24
	resp := PetGaleria{Base: ""}
	if portaPets > 0 {
		resp.Base = "http://127.0.0.1:" + fmt.Sprint(portaPets)
	}
	if deslocamento < 0 {
		deslocamento = 0
	}
	termo = strings.ToLower(strings.TrimSpace(termo))
	navegar := termo == "*"
	base := pastaPetsHermes()

	instalados := map[string]bool{}
	if ents, err := os.ReadDir(base); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				instalados[e.Name()] = true
			}
		}
	}

	if termo == "" {
		// sem busca: mostra o que já está na máquina (ordem alfabética)
		nomes := make([]string, 0, len(instalados))
		for s := range instalados {
			nomes = append(nomes, s)
		}
		sort.Strings(nomes)
		for _, s := range nomes {
			nome := s
			if b, err := os.ReadFile(filepath.Join(base, s, "pet.json")); err == nil {
				var man map[string]any
				if json.Unmarshal(b, &man) == nil {
					nome = str(man["displayName"], s)
				}
			}
			resp.Itens = append(resp.Itens, ItemGaleria{Slug: s, Nome: nome, Local: true})
		}
		resp.OK, resp.Total = true, len(resp.Itens)
		b, _ := json.Marshal(resp)
		return string(b)
	}

	pets, err := catalogoPetdex()
	if err != nil {
		resp.Erro = "Não foi possível conectar à galeria Petdex: " + err.Error()
		b, _ := json.Marshal(resp)
		return string(b)
	}
	achados := 0
	for _, m := range pets {
		slug := campo(m, "slug")
		nome := campo(m, "displayName")
		tipo := campo(m, "kind")
		if !navegar && !strings.Contains(strings.ToLower(slug+" "+nome+" "+tipo), termo) {
			continue
		}
		if achados >= deslocamento && len(resp.Itens) < porPagina {
			resp.Itens = append(resp.Itens, ItemGaleria{
				Slug: slug, Nome: nome, Tipo: tipo, Por: campo(m, "submittedBy"),
				Sprite: campo(m, "spritesheetUrl"), Local: instalados[slug],
			})
		}
		achados++
	}
	resp.Total = achados
	resp.TemMais = len(resp.Itens) == porPagina && achados > deslocamento+porPagina
	if termo == "*" {
		resp.Total = len(pets)
		resp.Erro = ""
	}
	logMsg("galeria: '" + termo + "' pagina " + fmt.Sprint(deslocamento) + " -> " +
		fmt.Sprint(len(resp.Itens)) + " de " + fmt.Sprint(resp.Total))
	resp.OK = true
	b, _ := json.Marshal(resp)
	return string(b)
}

// respostaErro — {"ok":false,"erro":msg} montado pelo encoding/json. Colar o erro na
// mão quebrava o JSON quando ele trazia caminho do Windows ("C:\Users" → "\U" é escape
// inválido) ou aspas, e o painel mostrava "sem rede?" no lugar do motivo.
func respostaErro(msg string) string {
	b, _ := json.Marshal(struct {
		OK   bool   `json:"ok"`
		Erro string `json:"erro"`
	}{false, msg})
	return string(b)
}

// PetDexBaixar — baixa um boneco da galeria para a pasta de pets do Hermes.
func (a *App) PetDexBaixar(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.ContainsAny(slug, "/\\.") {
		return respostaErro("Nome de boneco inválido.")
	}
	if dir := filepath.Join(pastaPetsHermes(), slug); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "spritesheet.webp")); err == nil {
			return `{"ok":true,"ja_tinha":true}`
		}
	}
	pets, err := catalogoPetdex()
	if err != nil {
		return respostaErro("Não foi possível conectar à galeria Petdex: " + err.Error())
	}
	var alvo map[string]any
	for _, m := range pets {
		if campo(m, "slug") == slug {
			alvo = m
			break
		}
	}
	if alvo == nil {
		return respostaErro("Este boneco não foi encontrado na galeria.")
	}
	destino := filepath.Join(pastaPetsHermes(), slug)
	if err := os.MkdirAll(destino, 0o755); err != nil {
		return respostaErro("Não foi possível criar a pasta do boneco: " + err.Error())
	}
	cli := &http.Client{Timeout: 180 * time.Second}
	baixa := func(url, nomeDestino string) error {
		if url == "" {
			return fmt.Errorf("endereço de download ausente")
		}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", agenteHTTP)
		r, err := cli.Do(req)
		if err != nil {
			return err
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return fmt.Errorf("o servidor respondeu com o código %d", r.StatusCode)
		}
		f, err := os.Create(filepath.Join(destino, nomeDestino))
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, r.Body)
		return err
	}
	if err := baixa(campo(alvo, "petJsonUrl"), "pet.json"); err != nil {
		return respostaErro("Não foi possível baixar os dados do boneco (pet.json): " + err.Error())
	}
	if err := baixa(campo(alvo, "spritesheetUrl"), "spritesheet.webp"); err != nil {
		return respostaErro("Não foi possível baixar a imagem do boneco: " + err.Error())
	}
	logMsg("galeria: baixado boneco " + slug)
	return `{"ok":true}`
}

// PetDexEscolher — define o boneco que o overlay usa (nosso config, não mexe no Hermes).
func (a *App) PetDexEscolher(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return respostaErro("Nenhum boneco foi escolhido.")
	}
	if err := os.WriteFile(filepath.Join(pastaPetAgente(), "petdex.txt"), []byte(slug), 0o644); err != nil {
		return respostaErro("Não foi possível salvar a escolha do boneco: " + err.Error())
	}
	logMsg("galeria: boneco escolhido = " + slug)
	return `{"ok":true}`
}
