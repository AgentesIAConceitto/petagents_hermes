package main

// harness_pool_test.go — testes do pool de chaves grátis (31/10/2026).
//
// O que não pode regredir:
//   - várias chaves do mesmo provedor convivem e TODAS entram na cadeia;
//   - chave repetida é recusada; remover por índice funciona;
//   - 429 em uma chave a coloca em espera e a próxima assume;
//   - o formato antigo (uma chave) continua funcionando (migração).

import (
	"path/filepath"
	"strings"
	"testing"
)

func cofreTemp(t *testing.T) *CofreChaves {
	t.Helper()
	c := AbrirCofre(filepath.Join(t.TempDir(), "chaves.json"))
	LimparCooldowns()
	return c
}

func TestVariasChavesPorProvedor(t *testing.T) {
	c := cofreTemp(t)
	for _, k := range []string{"AIza-chave-um-0001", "AIza-chave-dois-0002", "AIza-chave-tres-0003"} {
		if err := c.AdicionarChave(ChaveGemini, k); err != nil {
			t.Skip("DPAPI indisponível: " + err.Error())
		}
	}
	if n := len(c.Chaves(ChaveGemini)); n != 3 {
		t.Fatalf("esperava 3 chaves, tenho %d", n)
	}
	if c.TotalChaves() != 3 {
		t.Fatalf("total errado: %d", c.TotalChaves())
	}
	// persiste
	c2 := AbrirCofre(c.arquivo)
	if n := len(c2.Chaves(ChaveGemini)); n != 3 {
		t.Fatalf("não persistiu: %d", n)
	}
}

func TestChaveRepetidaERecusada(t *testing.T) {
	c := cofreTemp(t)
	k := "AIza-repetida-123456"
	if err := c.AdicionarChave(ChaveGemini, k); err != nil {
		t.Skip("DPAPI indisponível")
	}
	if err := c.AdicionarChave(ChaveGemini, k); err == nil {
		t.Fatal("deveria recusar chave repetida")
	}
	if n := len(c.Chaves(ChaveGemini)); n != 1 {
		t.Fatalf("não deveria duplicar: %d", n)
	}
}

func TestRemoverPorIndice(t *testing.T) {
	c := cofreTemp(t)
	for _, k := range []string{"chave-A-1111", "chave-B-2222", "chave-C-3333"} {
		if err := c.AdicionarChave(ChaveNVIDIA, k); err != nil {
			t.Skip("DPAPI indisponível")
		}
	}
	if err := c.RemoverChave(ChaveNVIDIA, 1); err != nil {
		t.Fatalf("remover: %v", err)
	}
	restantes := c.Chaves(ChaveNVIDIA)
	if len(restantes) != 2 || restantes[0] != "chave-A-1111" || restantes[1] != "chave-C-3333" {
		t.Fatalf("removido errado: %v", restantes)
	}
	if err := c.RemoverChave(ChaveNVIDIA, 9); err == nil {
		t.Fatal("índice inválido deveria falhar")
	}
}

func TestCadeiaUsaTodasAsChavesNaOrdem(t *testing.T) {
	c := cofreTemp(t)
	for _, k := range []string{"AIza-g1", "AIza-g2"} {
		if err := c.AdicionarChave(ChaveGemini, k); err != nil {
			t.Skip("DPAPI indisponível")
		}
	}
	for _, k := range []string{"nvapi-n1", "nvapi-n2"} {
		if err := c.AdicionarChave(ChaveNVIDIA, k); err != nil {
			t.Skip("DPAPI indisponível")
		}
	}
	cadeia := c.CadeiaDeChaves("", "")
	if cadeia.Quantos() != 4 {
		t.Fatalf("cadeia deveria ter 4 elos, tem %d", cadeia.Quantos())
	}
	var rotulos []string
	for {
		elo, ok := cadeia.Atual()
		if !ok {
			break
		}
		rotulos = append(rotulos, elo.Rotulo)
		if _, tem := cadeia.Avancar(); !tem {
			break
		}
	}
	esperado := "gemini#1,gemini#2,nvidia#1,nvidia#2"
	if strings.Join(rotulos, ",") != esperado {
		t.Fatalf("ordem errada: %v (esperava %s)", rotulos, esperado)
	}
}

func TestCooldownJogaChaveParaOFim(t *testing.T) {
	c := cofreTemp(t)
	for _, k := range []string{"AIza-boa-1", "AIza-castigada-2"} {
		if err := c.AdicionarChave(ChaveGemini, k); err != nil {
			t.Skip("DPAPI indisponível")
		}
	}
	MarcarCooldown("AIza-castigada-2", "HTTP 429")
	if !ChaveEmCooldown("AIza-castigada-2") {
		t.Fatal("deveria estar em cooldown")
	}
	cadeia := c.CadeiaDeChaves("", "")
	primeiro, _ := cadeia.Atual()
	if primeiro.Rotulo != "gemini#1" {
		t.Fatalf("a chave boa deveria vir primeiro, veio %s", primeiro.Rotulo)
	}
	LimparCooldowns()
	if ChaveEmCooldown("AIza-castigada-2") {
		t.Fatal("cooldown deveria ter sido limpo")
	}
}

func TestCooldownDeTodasAindaMontaCadeia(t *testing.T) {
	c := cofreTemp(t)
	if err := c.AdicionarChave(ChaveGemini, "AIza-unica"); err != nil {
		t.Skip("DPAPI indisponível")
	}
	MarcarCooldown("AIza-unica", "429")
	if cadeia := c.CadeiaDeChaves("", ""); cadeia.Vazia() {
		t.Fatal("com todas em espera a cadeia não pode ficar vazia (melhor tentar e receber 429)")
	}
	LimparCooldowns()
}

func TestFormatoAntigoMigra(t *testing.T) {
	c := cofreTemp(t)
	// Definir é o formato antigo de uma chave só
	if err := c.Definir(ChaveBrave, "brave-antiga-123456"); err != nil {
		t.Skip("DPAPI indisponível")
	}
	if got := c.Chaves(ChaveBrave); len(got) != 1 || got[0] != "brave-antiga-123456" {
		t.Fatalf("migração falhou: %v", got)
	}
	// adicionar outra deve manter a primeira
	if err := c.AdicionarChave(ChaveBrave, "brave-nova-654321"); err != nil {
		t.Fatalf("adicionar: %v", err)
	}
	if n := len(c.Chaves(ChaveBrave)); n != 2 {
		t.Fatalf("esperava 2 depois de migrar, tenho %d", n)
	}
}

func TestChaveDoRotulo(t *testing.T) {
	c := cofreTemp(t)
	if err := c.AdicionarChave(ChaveNVIDIA, "nvapi-rotulo-1"); err != nil {
		t.Skip("DPAPI indisponível")
	}
	if err := c.AdicionarChave(ChaveNVIDIA, "nvapi-rotulo-2"); err != nil {
		t.Skip("DPAPI indisponível")
	}
	if got := c.ChaveDoRotulo("nvidia#2"); got != "nvapi-rotulo-2" {
		t.Fatalf("rótulo errado: %q", got)
	}
	if got := c.ChaveDoRotulo("nvidia#9"); got != "" {
		t.Fatalf("rótulo inexistente deveria devolver vazio: %q", got)
	}
}

func TestEstadoChavesParaTela(t *testing.T) {
	c := cofreTemp(t)
	if err := c.AdicionarChave(ChaveGemini, "AIza-tela-111111"); err != nil {
		t.Skip("DPAPI indisponível")
	}
	if err := c.AdicionarChave(ChaveGemini, "AIza-tela-222222"); err != nil {
		t.Skip("DPAPI indisponível")
	}
	estado := c.EstadoChaves()
	if len(estado) == 0 {
		t.Fatal("estado vazio")
	}
	var gemini map[string]any
	for _, e := range estado {
		if e["id"] == "gemini" {
			gemini = e
		}
	}
	if gemini == nil {
		t.Fatal("não veio o provedor gemini")
	}
	if gemini["quantas"] != 2 {
		t.Fatalf("quantas errado: %v", gemini["quantas"])
	}
	chaves, _ := gemini["chaves"].([]map[string]any)
	if len(chaves) != 2 {
		t.Fatalf("lista de chaves errada: %v", chaves)
	}
	if m, _ := chaves[0]["mascara"].(string); m == "" || strings.Contains(m, "tela-111111") {
		t.Fatalf("máscara vazou: %q", m)
	}
}
