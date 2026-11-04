package main

// harness_chaves_test.go — testes do cofre de chaves do harness (30/10/2026).
//
// O que não pode regredir:
//   - a chave fica CIFRADA no disco (o arquivo não contém o texto da chave);
//   - a tela recebe máscara, nunca a chave inteira;
//   - apagar funciona e o estado reflete;
//   - a configuração de web sai do cofre (Brave/Searx) e cai para env quando vazio.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCofreCifraNoDisco(t *testing.T) {
	arq := filepath.Join(t.TempDir(), "chaves.json")
	c := AbrirCofre(arq)
	chave := "nvapi-0123456789abcdef0123456789abcdef"
	if err := c.Definir(ChaveNVIDIA, chave); err != nil {
		t.Skip("DPAPI indisponível neste ambiente: " + err.Error())
	}
	bruto, err := os.ReadFile(arq)
	if err != nil {
		t.Fatalf("arquivo não foi criado: %v", err)
	}
	if strings.Contains(string(bruto), chave) {
		t.Fatal("a chave apareceu em texto puro no disco")
	}
	if strings.Contains(string(bruto), "nvapi-") {
		t.Fatal("o prefixo da chave vazou para o disco")
	}
	// reabre e decifra
	c2 := AbrirCofre(arq)
	if got := c2.Obter(ChaveNVIDIA); got != chave {
		t.Fatalf("chave não voltou igual: %q", got)
	}
}

func TestCofreMascaraNaoEntregaChave(t *testing.T) {
	c := AbrirCofre(filepath.Join(t.TempDir(), "c.json"))
	chave := "AIzaSyFAKE00000000000000000000000000000"
	if err := c.Definir(ChaveGemini, chave); err != nil {
		t.Skip("DPAPI indisponível: " + err.Error())
	}
	m := c.Mascarado(ChaveGemini)
	if m == "" || m == chave {
		t.Fatalf("máscara inválida: %q", m)
	}
	if strings.Contains(m, "FAKE000000") {
		t.Fatalf("máscara mostrou o miolo da chave: %q", m)
	}
	// o estado da tela não pode conter a chave em claro
	for _, item := range c.Estado() {
		for _, v := range item {
			if v == chave {
				t.Fatal("Estado() devolveu a chave em claro")
			}
		}
	}
}

func TestCofreApagarEEstado(t *testing.T) {
	c := AbrirCofre(filepath.Join(t.TempDir(), "c.json"))
	if err := c.Definir(ChaveBrave, "chave-teste-123456"); err != nil {
		t.Skip("DPAPI indisponível: " + err.Error())
	}
	estado := c.Estado()
	var brave map[string]string
	for _, e := range estado {
		if e["id"] == "brave" {
			brave = e
		}
	}
	if brave == nil || brave["configurada"] != "sim" {
		t.Fatalf("estado não marcou como configurada: %+v", brave)
	}
	if err := c.Definir(ChaveBrave, ""); err != nil {
		t.Fatalf("apagar falhou: %v", err)
	}
	if c.Obter(ChaveBrave) != "" {
		t.Fatal("chave não foi apagada")
	}
}

func TestConfigWebVemDoCofre(t *testing.T) {
	c := AbrirCofre(filepath.Join(t.TempDir(), "c.json"))
	if err := c.Definir(ChaveBrave, "brave-key-abcdef"); err != nil {
		t.Skip("DPAPI indisponível: " + err.Error())
	}
	if err := c.DefinirSearx("http://127.0.0.1:8888"); err != nil {
		t.Fatalf("searx: %v", err)
	}
	cfg := c.ConfigWeb()
	if cfg.BraveKey != "brave-key-abcdef" {
		t.Fatalf("brave não veio do cofre: %q", cfg.BraveKey)
	}
	if cfg.SearxURL != "http://127.0.0.1:8888" {
		t.Fatalf("searx não veio do cofre: %q", cfg.SearxURL)
	}
}

func TestClientePadraoPrefereGemini(t *testing.T) {
	c := AbrirCofre(filepath.Join(t.TempDir(), "c.json"))
	if err := c.Definir(ChaveGemini, "AIza-teste-abcdef"); err != nil {
		t.Skip("DPAPI indisponível: " + err.Error())
	}
	if err := c.Definir(ChaveNVIDIA, "nvapi-teste-abcdef"); err != nil {
		t.Skip("DPAPI indisponível: " + err.Error())
	}
	cli, modelo := c.ClientePadrao()
	if cli == nil {
		t.Fatal("deveria devolver cliente")
	}
	if cli.Nome() != "gemini" {
		t.Fatalf("deveria preferir Gemini (mais rápido no teste medido), veio %s", cli.Nome())
	}
	if modelo != ModeloPadraoGemini {
		t.Fatalf("modelo padrão inesperado: %q", modelo)
	}
}

func TestClientePadraoSoNvidia(t *testing.T) {
	c := AbrirCofre(filepath.Join(t.TempDir(), "c.json"))
	if err := c.Definir(ChaveNVIDIA, "nvapi-teste-abcdef"); err != nil {
		t.Skip("DPAPI indisponível: " + err.Error())
	}
	cli, modelo := c.ClientePadrao()
	if cli == nil || cli.Nome() != "nvidia" {
		t.Fatalf("deveria usar NVIDIA como reserva: %v", cli)
	}
	if modelo != ModeloPadraoNVIDIA {
		t.Fatalf("modelo inesperado: %q", modelo)
	}
}

func TestClientePadraoSemChave(t *testing.T) {
	c := AbrirCofre(filepath.Join(t.TempDir(), "c.json"))
	cli, modelo := c.ClientePadrao()
	if cli != nil || modelo != "" {
		t.Fatal("sem chave deveria devolver nil")
	}
}

func TestModeloPadraoConfiguravel(t *testing.T) {
	c := AbrirCofre(filepath.Join(t.TempDir(), "c.json"))
	if err := c.DefinirModeloPadrao("gemini-2.5-flash"); err != nil {
		t.Fatalf("gravar modelo: %v", err)
	}
	if got := AbrirCofre(c.arquivo).ModeloPadrao(); got != "gemini-2.5-flash" {
		t.Fatalf("modelo não persistiu: %q", got)
	}
}

func TestChavesConhecidasTemPagina(t *testing.T) {
	for _, info := range chavesConhecidas {
		if info.Pagina == "" || info.Ajuda == "" {
			t.Fatalf("chave %s sem página/ajuda para a tela", info.ID)
		}
	}
}

// TestTestarChaveValidaERecusa cobre o botão "Testar" da tela: precisa dizer na hora se a
// chave colada funciona (foi assim que descobrimos chave errada e cota estourada).
func TestTestarChaveValidaERecusa(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "boa" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("x-ratelimit-remaining", "37")
		fmt.Fprint(w, `{"web":{"results":[{"title":"t","url":"https://x","description":"d"}]}}`)
	}))
	defer ok.Close()

	if msg := testaBraveComURL(&http.Client{}, ok.URL, "boa"); !strings.Contains(msg, "válida") || !strings.Contains(msg, "37") {
		t.Fatalf("deveria validar e mostrar cota: %q", msg)
	}
	if msg := testaBraveComURL(&http.Client{}, ok.URL, "ruim"); !strings.Contains(msg, "recusada") {
		t.Fatalf("deveria recusar: %q", msg)
	}
}
