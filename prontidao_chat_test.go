package main

// prontidao_chat_test.go — o cabeçalho do chat não pode mentir (03/11/2026).
// Print do dono: "mostra a mensagem de agente indisponível, sendo que ele está trabalhando
// com as chaves grátis" — enquanto a resposta levava 1,1 s.

import (
	"strings"
	"testing"
)

func TestCabecalhoProntoNoModoGratisMesmoComPonteCaida(t *testing.T) {
	// ponte do Hermes caída (pronta=false, erro preenchido) — situação normal no modo grátis
	pronta, erro, modelo, provedor := prontidaoChat(false, "ponte não subiu", ModoNativo, 3, "gemini-3.5-flash-lite")

	if !pronta {
		t.Fatal("modo grátis com 3 chaves: o chat ESTÁ pronto")
	}
	if erro != "" {
		t.Fatalf("não pode carregar erro da ponte: %q", erro)
	}
	if !strings.Contains(modelo, "grátis") {
		t.Fatalf("o modelo deveria dizer que é grátis: %q", modelo)
	}
	if provedor != "Modo grátis" {
		t.Fatalf("provedor errado: %q", provedor)
	}
}

func TestCabecalhoRespeitaPonteNoModoHermes(t *testing.T) {
	pronta, erro, modelo, provedor := prontidaoChat(false, "agente fora do ar", ModoHermes, 3, "")
	if pronta {
		t.Fatal("no modo Hermes, a ponte caída significa NÃO pronto")
	}
	if erro != "agente fora do ar" {
		t.Fatalf("o erro real deve ser preservado: %q", erro)
	}
	if modelo != "" || provedor != "" {
		t.Fatalf("não deve inventar modelo grátis no modo Hermes: %q %q", modelo, provedor)
	}
}

func TestCabecalhoSemChaveNaoFingePronto(t *testing.T) {
	pronta, _, modelo, _ := prontidaoChat(false, "", ModoNativo, 0, "")
	if pronta {
		t.Fatal("modo nativo SEM chave não está pronto")
	}
	if modelo != "" {
		t.Fatalf("sem chave não há modelo grátis: %q", modelo)
	}
}

func TestCabecalhoUsaModeloPadraoQuandoVazio(t *testing.T) {
	_, _, modelo, _ := prontidaoChat(false, "", ModoNativo, 1, "")
	if !strings.Contains(modelo, ModeloPadraoGemini) {
		t.Fatalf("deveria cair no modelo padrão: %q", modelo)
	}
}
