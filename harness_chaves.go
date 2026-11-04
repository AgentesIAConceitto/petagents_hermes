package main

// harness_chaves.go — chaves do harness configuráveis DENTRO do PetAgent (30/10/2026).
//
// REQUISITO DO DONO: "essas chaves precisam todas serem configuráveis no PetAgent".
// Antes, as chaves dos provedores free só existiam em variável de ambiente — o que serve
// para mim (que rodo script) e não serve para quem instala o app.
//
// Segue o MESMO padrão de inteligencia.go: JSON em %LOCALAPPDATA%\petagent, chave cifrada
// com DPAPI (só o usuário do Windows abre), 0600 no arquivo, e a tela só recebe máscara.
//
// O que é segredo (DPAPI): chave da NVIDIA, do Google e da Brave.
// O que não é segredo: URL do SearxNG (endereço público/local) e o modelo padrão.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ChaveProvedor identifica de onde vem cada credencial.
type ChaveProvedor string

const (
	ChaveNVIDIA ChaveProvedor = "nvidia"
	ChaveGemini ChaveProvedor = "gemini"
	ChaveBrave  ChaveProvedor = "brave"
)

// infoChave descreve a chave para a tela de configuração (equivalente ao provedorIA).
type infoChave struct {
	ID      ChaveProvedor
	Nome    string
	Var     string // variável de ambiente equivalente (retrocompatível)
	Pagina  string // onde criar
	Prefixo string // começo típico, só dica no campo
	Ajuda   string
}

// chavesConhecidas é a lista que a tela de configuração mostra.
var chavesConhecidas = []infoChave{
	{ID: ChaveNVIDIA, Nome: "NVIDIA (grátis)", Var: "NVIDIA_API_KEY",
		Pagina: "https://build.nvidia.com/settings/api-keys", Prefixo: "nvapi-",
		Ajuda: "40 req/min sem cobrança por token. Modelos abertos (Nemotron, gpt-oss, Mistral)."},
	{ID: ChaveGemini, Nome: "Google Gemini (grátis)", Var: "GEMINI_API_KEY",
		Pagina: "https://aistudio.google.com/apikey", Prefixo: "AIza",
		Ajuda: "Free tier sem cartão. É o modelo mais rápido para tarefa básica (responde em ~2s)."},
	{ID: ChaveBrave, Nome: "Brave Search (busca web)", Var: "BRAVE_SEARCH_API_KEY",
		Pagina: "https://api.search.brave.com/app/keys", Prefixo: "",
		Ajuda: "Sem ela o agente busca só na Wikipédia. Com ela busca a web inteira."},
}

func infoDeChave(id ChaveProvedor) (infoChave, bool) {
	for _, i := range chavesConhecidas {
		if i.ID == id {
			return i, true
		}
	}
	return infoChave{}, false
}

// chavesSalvas é o que fica no disco. Campos de chave guardam DPAPI+base64.
type chavesSalvas struct {
	// Listas: o produto precisa de VÁRIAS chaves grátis do mesmo provedor, porque a cota de
	// cada uma é pequena. Uma chave só não atende demanda.
	NVIDIALista []string `json:"nvidia_lista,omitempty"`
	GeminiLista []string `json:"gemini_lista,omitempty"`
	BraveLista  []string `json:"brave_lista,omitempty"`
	// Campos antigos (uma chave por provedor): lidos para migrar sem perder o que já existe.
	NVIDIA       string `json:"nvidia,omitempty"`
	Gemini       string `json:"gemini,omitempty"`
	Brave        string `json:"brave,omitempty"`
	Searx        string `json:"searx,omitempty"`
	ModeloPadrao string `json:"modelo_padrao,omitempty"`
	// Modo é o motor escolhido pela pessoa: "auto" (padrão), "hermes" ou "nativo".
	Modo string `json:"modo,omitempty"`
}

// CofreChaves lê e grava as chaves do harness (protegido por mutex).
type CofreChaves struct {
	mu      sync.Mutex
	arquivo string
	dados   chavesSalvas
}

// AbrirCofre carrega (ou cria) o cofre em %LOCALAPPDATA%\petagent\harness_chaves.json.
func AbrirCofre(caminho string) *CofreChaves {
	if caminho == "" {
		caminho = filepath.Join(pastaPetAgente(), "harness_chaves.json")
	}
	c := &CofreChaves{arquivo: caminho}
	if b, err := os.ReadFile(caminho); err == nil {
		_ = json.Unmarshal(b, &c.dados)
	}
	return c
}

// salvar grava com 0600 (chamar com o mutex travado).
func (c *CofreChaves) salvar() error {
	if err := os.MkdirAll(filepath.Dir(c.arquivo), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c.dados, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.arquivo + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.arquivo)
}

// Definir substitui a lista do provedor por UMA chave (compatibilidade com o código
// antigo e com os testes). Passar vazio limpa tudo.
func (c *CofreChaves) Definir(id ChaveProvedor, valor string) error {
	valor = strings.TrimSpace(valor)
	c.mu.Lock()
	switch id {
	case ChaveNVIDIA:
		c.dados.NVIDIALista, c.dados.NVIDIA = nil, ""
	case ChaveGemini:
		c.dados.GeminiLista, c.dados.Gemini = nil, ""
	case ChaveBrave:
		c.dados.BraveLista, c.dados.Brave = nil, ""
	default:
		c.mu.Unlock()
		return fmt.Errorf("chave desconhecida: %s", id)
	}
	c.mu.Unlock()
	if valor == "" {
		return c.salvar()
	}
	return c.AdicionarChave(id, valor)
}

// Obter devolve a PRIMEIRA chave do provedor (compatibilidade).
func (c *CofreChaves) Obter(id ChaveProvedor) string {
	lista := c.Chaves(id)
	if len(lista) == 0 {
		return ""
	}
	return lista[0]
}

// Mascarado devolve a máscara da primeira chave (compatibilidade).
func (c *CofreChaves) Mascarado(id ChaveProvedor) string {
	return mascara(c.Obter(id))
}

// DefinirSearx grava a URL do SearxNG (não é segredo, vai em claro).
func (c *CofreChaves) DefinirSearx(url string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dados.Searx = strings.TrimSpace(url)
	return c.salvar()
}

// Searx devolve a URL configurada do SearxNG.
func (c *CofreChaves) Searx() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dados.Searx
}

// DefinirModeloPadrao grava o modelo preferido do agente free.
func (c *CofreChaves) DefinirModeloPadrao(m string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dados.ModeloPadrao = strings.TrimSpace(m)
	return c.salvar()
}

// ModeloPadrao devolve o modelo configurado ("" = usar o padrão do código).
func (c *CofreChaves) ModeloPadrao() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dados.ModeloPadrao
}

// Estado devolve o que a tela precisa saber: por provedor, se está configurada, a máscara
// e onde criar. Nunca devolve a chave em claro.
func (c *CofreChaves) Estado() []map[string]string {
	out := make([]map[string]string, 0, len(chavesConhecidas))
	for _, info := range chavesConhecidas {
		m := c.Mascarado(info.ID)
		configurada := "nao"
		if m != "" {
			configurada = "sim"
		}
		out = append(out, map[string]string{
			"id":          string(info.ID),
			"nome":        info.Nome,
			"configurada": configurada,
			"mascara":     m,
			"pagina":      info.Pagina,
			"prefixo":     info.Prefixo,
			"ajuda":       info.Ajuda,
			"var":         info.Var,
		})
	}
	return out
}

// ConfigWeb monta a configuração das ferramentas de web a partir do cofre, caindo para
// variável de ambiente quando o usuário ainda não configurou pela tela (retrocompatível
// com quem já usa PETAGENT_BUSCA_CHAVE / BRAVE_SEARCH_API_KEY).
func (c *CofreChaves) ConfigWeb() ConfigWeb {
	// comDefaults() é obrigatório: sem ele o cliente HTTP vai NIL e qualquer busca
	// estoura panic (aconteceu em 03/11/2026 na pré-busca). Aqui, todo chamador recebe
	// uma configuração utilizável.
	cfg := ConfigWeb{BraveKey: c.Obter(ChaveBrave), SearxURL: c.Searx()}
	return cfg.comDefaults()
}

// ChavesBrave devolve todas as chaves de busca configuradas (a ferramenta usa uma; se ela
// falhar, pode tentar a próxima).
func (c *CofreChaves) ChavesBrave() []string { return c.Chaves(ChaveBrave) }

// ClientePadrao escolhe o provedor do agente nativo: Gemini primeiro (mais rápido para
// tarefa básica), NVIDIA depois. Devolve nil quando não há nenhuma chave.
func (c *CofreChaves) ClientePadrao() (Cliente, string) {
	modelo := c.ModeloPadrao()
	cadeia := c.CadeiaDeChaves(modelo, modelo)
	if elo, ok := cadeia.Atual(); ok {
		return elo.Cliente, elo.Modelo
	}
	return nil, ""
}

// Modelos escolhidos por medição (30/10/2026): Gemini 3.5-flash-lite escreve sem cadeia de
// raciocínio (resposta em ~2s); Nemotron/gpt-oss ficam como reserva.
const (
	ModeloPadraoGemini = "gemini-3.5-flash-lite"
	ModeloPadraoNVIDIA = "openai/gpt-oss-20b"
	URLNVIDIA          = "https://integrate.api.nvidia.com/v1/chat/completions"
)

// ErroSemChave é devolvido quando falta credencial para o agente nativo.
var ErroSemChave = errors.New("nenhuma chave configurada (NVIDIA ou Google)")
