package main

// prebusca_test.go — quando o app deve buscar antes de perguntar ao modelo (03/11/2026).
// Regra: só consulta de dado do mundo. Pedido de ação nunca.

import (
	"path/filepath"
	"testing"
)

func TestPreBuscaReconheceConsulta(t *testing.T) {
	casos := []string{
		"qual a cotação do dólar",
		"qual a cotacao do dolar?",
		"quanto está o dólar hoje",
		"quem é o prefeito de Natal",
		"qual a previsão do tempo em Natal",
		"me diga a população do Rio de Janeiro",
	}
	for _, c := range casos {
		if !pareceConsultaDados(c) {
			t.Errorf("deveria pré-buscar: %q", c)
		}
	}
}

func TestPreBuscaNaoDisparaEmAcao(t *testing.T) {
	casos := []string{
		"crie uma pasta chamada projeto-x",
		"escreva um PRD sobre o dólar",
		"agende um lembrete para amanhã 9h",
		"resuma esse texto sobre inflação",
		"apague o arquivo nota.txt",
		"qual o nome do arquivo que eu criei ontem", // ação no meio
	}
	for _, c := range casos {
		if pareceConsultaDados(c) {
			t.Errorf("NÃO deveria pré-buscar (é ação): %q", c)
		}
	}
}

func TestPreBuscaIgnoraConversa(t *testing.T) {
	casos := []string{"oi", "bom dia", "obrigado", "tudo bem?", ""}
	for _, c := range casos {
		if pareceConsultaDados(c) {
			t.Errorf("conversa não precisa de busca: %q", c)
		}
	}
}

func TestConsultaParaBuscaLimpaAMoldura(t *testing.T) {
	got := consultaParaBusca("me diga a cotação do dólar?")
	if got != "a cotação do dólar" {
		t.Fatalf("consulta mal formada: %q", got)
	}
}

// TestConfigWebDoCofreTemClienteHTTP: o panic de 03/11/2026 aconteceu porque a config vinha
// sem defaults e o cliente HTTP era nil. Este teste trava isso.
func TestConfigWebDoCofreTemClienteHTTP(t *testing.T) {
	c := AbrirCofre(filepath.Join(t.TempDir(), "chaves.json"))
	cfg := c.ConfigWeb()
	if cfg.HTTP == nil {
		t.Fatal("ConfigWeb() sem cliente HTTP: qualquer busca estoura panic")
	}
}

// TestConversaNaoLevaFerramenta: "Bom dia" não precisa de 27 KB de schema de ferramenta.
// Medido em 03/11/2026: com 10 ferramentas 30,8 s, sem ferramentas 21,4 s.
func TestConversaNaoLevaFerramenta(t *testing.T) {
	conversas := []string{"Bom dia", "oi", "boa noite!", "tudo bem?", "obrigado", "valeu!", "ok", ""}
	for _, c := range conversas {
		if !pareceConversa(c) {
			t.Errorf("deveria ser tratada como conversa: %q", c)
		}
	}
	naoConversa := []string{
		"crie uma pasta chamada projeto-x",
		"qual a cotação do dólar",
		"escreva um PRD de uma página",
		"me diga a população do Rio",
	}
	for _, c := range naoConversa {
		if pareceConversa(c) {
			t.Errorf("NÃO é conversa simples: %q", c)
		}
	}
}

// TestConversaNaoFazPreBusca: conversa não gasta cota com busca.
func TestConversaNaoFazPreBusca(t *testing.T) {
	if pareceConsultaDados("Bom dia") {
		t.Fatal("saudação não pode disparar pré-busca")
	}
}
