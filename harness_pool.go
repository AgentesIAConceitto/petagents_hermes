package main

// harness_pool.go — várias chaves grátis por provedor, com cota e cooldown (31/10/2026).
//
// POR QUE EXISTE: cada chave grátis tem cota pequena (Google ~50 req/min e teto diário;
// NVIDIA 40 req/min). Uma chave só não atende nem uma pessoa por muito tempo. O desenho do
// produto é o usuário juntar VÁRIAS chaves grátis (dele, de contas diferentes) e o app
// distribuir a carga entre elas.
//
// COMO FUNCIONA
//   - o cofre guarda uma LISTA de chaves por provedor (cifradas uma a uma com DPAPI);
//   - a cadeia de atendimento é montada com TODAS as chaves, na ordem: Google, depois NVIDIA;
//   - quando uma chave leva 429/estoura, ela entra em COOLDOWN e a próxima assume — o turno
//     não morre e a pessoa não vê erro;
//   - cooldown curto (padrão 60 s para limite por minuto) e o motor tenta de novo depois.
//
// A chave em si nunca é usada como rótulo público: o rótulo é "google#2", e o cooldown é
// indexado por hash — senão a chave apareceria em log/memória.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// cooldownPadrao é quanto tempo uma chave castigada fica de fora.
const cooldownPadrao = 60 * time.Second

// cotaDeChave é o estado de uma chave castigada.
type cotaDeChave struct {
	LiberarEm time.Time
	Motivo    string
}

var (
	poolMu     sync.Mutex
	cotaChaves = map[string]cotaDeChave{} // hash da chave -> estado
)

// hashChave identifica a chave sem guardá-la em claro.
func hashChave(chave string) string {
	s := sha256.Sum256([]byte(strings.TrimSpace(chave)))
	return hex.EncodeToString(s[:8])
}

// MarcarCooldown coloca uma chave de fora por um tempo (após 429/cota esgotada).
func MarcarCooldown(chave, motivo string) {
	if strings.TrimSpace(chave) == "" {
		return
	}
	poolMu.Lock()
	defer poolMu.Unlock()
	cotaChaves[hashChave(chave)] = cotaDeChave{LiberarEm: time.Now().Add(cooldownPadrao), Motivo: motivo}
}

// ChaveEmCooldown diz se a chave está de castigo agora.
func ChaveEmCooldown(chave string) bool {
	poolMu.Lock()
	defer poolMu.Unlock()
	c, ok := cotaChaves[hashChave(chave)]
	if !ok {
		return false
	}
	if time.Now().After(c.LiberarEm) {
		delete(cotaChaves, hashChave(chave))
		return false
	}
	return true
}

// LimparCooldowns zera a lista de castigos (botão "tentar todas de novo").
func LimparCooldowns() {
	poolMu.Lock()
	defer poolMu.Unlock()
	cotaChaves = map[string]cotaDeChave{}
}

// Chaves devolve as chaves em claro de um provedor (uso interno).
func (c *CofreChaves) Chaves(id ChaveProvedor) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chavesTravado(id)
}

// chavesTravado devolve a lista já migrada (chamar com o mutex travado).
func (c *CofreChaves) chavesTravado(id ChaveProvedor) []string {
	var cifradas []string
	switch id {
	case ChaveNVIDIA:
		cifradas = c.dados.NVIDIALista
		if len(cifradas) == 0 && c.dados.NVIDIA != "" {
			cifradas = []string{c.dados.NVIDIA} // migra o formato antigo
		}
	case ChaveGemini:
		cifradas = c.dados.GeminiLista
		if len(cifradas) == 0 && c.dados.Gemini != "" {
			cifradas = []string{c.dados.Gemini}
		}
	case ChaveBrave:
		cifradas = c.dados.BraveLista
		if len(cifradas) == 0 && c.dados.Brave != "" {
			cifradas = []string{c.dados.Brave}
		}
	}
	out := make([]string, 0, len(cifradas))
	for _, cif := range cifradas {
		if claro, err := decifra(cif); err == nil && strings.TrimSpace(claro) != "" {
			out = append(out, strings.TrimSpace(claro))
		}
	}
	return out
}

// AdicionarChave inclui uma chave na lista do provedor (recusa repetida).
func (c *CofreChaves) AdicionarChave(id ChaveProvedor, valor string) error {
	valor = strings.TrimSpace(valor)
	if valor == "" {
		return fmt.Errorf("chave vazia")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	existentes := c.chavesTravado(id)
	for _, e := range existentes {
		if e == valor {
			return fmt.Errorf("essa chave já está na lista")
		}
	}
	if len(existentes) >= 10 {
		return fmt.Errorf("limite de 10 chaves por provedor")
	}
	cifrado, err := cifra(valor)
	if err != nil {
		return fmt.Errorf("não consegui cifrar a chave: %w", err)
	}

	// consolida no formato novo (e zera o antigo, que já foi migrado)
	switch id {
	case ChaveNVIDIA:
		c.dados.NVIDIALista = append(cifradasDe(c.dados.NVIDIA, c.dados.NVIDIALista), cifrado)
		c.dados.NVIDIA = ""
	case ChaveGemini:
		c.dados.GeminiLista = append(cifradasDe(c.dados.Gemini, c.dados.GeminiLista), cifrado)
		c.dados.Gemini = ""
	case ChaveBrave:
		c.dados.BraveLista = append(cifradasDe(c.dados.Brave, c.dados.BraveLista), cifrado)
		c.dados.Brave = ""
	default:
		return fmt.Errorf("chave desconhecida: %s", id)
	}
	return c.salvar()
}

// cifradasDe junta o campo antigo (se houver) com a lista, sem duplicar.
func cifradasDe(antigo string, lista []string) []string {
	if antigo == "" {
		return lista
	}
	return append([]string{antigo}, lista...)
}

// RemoverChave tira a chave na posição i (0-based) do provedor.
func (c *CofreChaves) RemoverChave(id ChaveProvedor, i int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lista []string
	switch id {
	case ChaveNVIDIA:
		lista = cifradasDe(c.dados.NVIDIA, c.dados.NVIDIALista)
	case ChaveGemini:
		lista = cifradasDe(c.dados.Gemini, c.dados.GeminiLista)
	case ChaveBrave:
		lista = cifradasDe(c.dados.Brave, c.dados.BraveLista)
	default:
		return fmt.Errorf("chave desconhecida: %s", id)
	}
	if i < 0 || i >= len(lista) {
		return fmt.Errorf("posição inválida")
	}
	lista = append(lista[:i], lista[i+1:]...)

	switch id {
	case ChaveNVIDIA:
		c.dados.NVIDIALista, c.dados.NVIDIA = lista, ""
	case ChaveGemini:
		c.dados.GeminiLista, c.dados.Gemini = lista, ""
	case ChaveBrave:
		c.dados.BraveLista, c.dados.Brave = lista, ""
	}
	return c.salvar()
}

// CadeiaDeChaves monta a cadeia de atendimento com TODAS as chaves configuradas:
// primeiro as do Google (mais rápidas para tarefa básica), depois as da NVIDIA.
// Chaves em cooldown ficam de fora — e se TODAS estiverem, entra assim mesmo (melhor
// tentar e receber 429 do que responder "não tenho o que usar").
func (c *CofreChaves) CadeiaDeChaves(modeloGoogle, modeloNVIDIA string) *Cadeia {
	if modeloGoogle == "" {
		modeloGoogle = ModeloPadraoGemini
	}
	if modeloNVIDIA == "" {
		modeloNVIDIA = ModeloPadraoNVIDIA
	}
	var livres, castigadas []ProvedorEscolhido

	adiciona := func(lista []string, provedor ChaveProvedor, modelo string, criar func(chave string) Cliente) {
		for i, chave := range lista {
			rotulo := fmt.Sprintf("%s#%d", provedor, i+1)
			elo := ProvedorEscolhido{Rotulo: rotulo, Modelo: modelo, Cliente: criar(chave)}
			if ChaveEmCooldown(chave) {
				castigadas = append(castigadas, elo)
			} else {
				livres = append(livres, elo)
			}
		}
	}

	adiciona(c.Chaves(ChaveGemini), ChaveGemini, modeloGoogle, func(chave string) Cliente {
		return &ClienteGemini{Chave: chave, Modelo: ""}
	})
	adiciona(c.Chaves(ChaveNVIDIA), ChaveNVIDIA, modeloNVIDIA, func(chave string) Cliente {
		return &ClienteOpenAI{Rotulo: "nvidia", BaseURL: URLNVIDIA, Chave: chave}
	})

	// os medidos primeiro, do mais rápido para o mais lento; os sem medição depois, na
	// ordem original (nunca excluir alguém por falta de dado) — ver harness_latencia.go
	todos := ordenarPorLatencia(append(livres, castigadas...))
	return NovaCadeia(todos...)
}

// ChaveDoRotulo devolve a chave em claro a partir do rótulo "google#2" (para marcar cooldown).
func (c *CofreChaves) ChaveDoRotulo(rotulo string) string {
	partes := strings.SplitN(rotulo, "#", 2)
	if len(partes) != 2 {
		return ""
	}
	var id ChaveProvedor
	switch partes[0] {
	case string(ChaveGemini):
		id = ChaveGemini
	case string(ChaveNVIDIA):
		id = ChaveNVIDIA
	case string(ChaveBrave):
		id = ChaveBrave
	default:
		return ""
	}
	var i int
	if _, err := fmt.Sscanf(partes[1], "%d", &i); err != nil || i < 1 {
		return ""
	}
	lista := c.Chaves(id)
	if i-1 >= len(lista) {
		return ""
	}
	return lista[i-1]
}

// EstadoChaves devolve, por provedor, a lista de chaves mascaradas com estado de cota.
// É o que a tela usa para mostrar "3 chaves, 1 em espera".
func (c *CofreChaves) EstadoChaves() []map[string]any {
	out := []map[string]any{}
	for _, info := range chavesConhecidas {
		lista := c.Chaves(info.ID)
		itens := make([]map[string]any, 0, len(lista))
		emEspera := 0
		for i, chave := range lista {
			castigada := ChaveEmCooldown(chave)
			if castigada {
				emEspera++
			}
			itens = append(itens, map[string]any{
				"rotulo":   fmt.Sprintf("%s#%d", info.ID, i+1),
				"mascara":  mascara(chave),
				"emEspera": castigada,
			})
		}
		out = append(out, map[string]any{
			"id":          string(info.ID),
			"nome":        info.Nome,
			"quantas":     len(lista),
			"emEspera":    emEspera,
			"chaves":      itens,
			"pagina":      info.Pagina,
			"prefixo":     info.Prefixo,
			"ajuda":       info.Ajuda,
			"configurada": map[bool]string{true: "sim", false: "nao"}[len(lista) > 0],
		})
	}
	return out
}

// TotalChaves conta quantas chaves grátis de modelo existem (Google + NVIDIA).
func (c *CofreChaves) TotalChaves() int {
	return len(c.Chaves(ChaveGemini)) + len(c.Chaves(ChaveNVIDIA))
}

// buildInfo identifica a build (sobrescrita por -ldflags "-X main.buildInfo=...").
var buildInfo = "dev"
