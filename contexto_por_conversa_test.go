package main

// contexto_por_conversa_test.go — "cada conversa com o seu contexto" (04/11/2026).
// Cobrança do dono: "como está agora ele já abre a conversa nova lembrando do que falou na
// outra". Estes testes travam a regra: contexto é por id de conversa, nunca global.

import (
	"strings"
	"testing"
)

func limpaHistMemoria() {
	natMu.Lock()
	natHistorico = map[string][]Mensagem{}
	natMu.Unlock()
}

func TestConversaNovaNaoHerdaContexto(t *testing.T) {
	limpaHistMemoria()
	a := NovaConversaLocal()
	if a == "" {
		t.Fatal("sem id")
	}
	defer ApagaConversaLocal(a)

	natMu.Lock()
	natHistorico[a] = []Mensagem{
		{Role: "user", Content: "meu cliente é a empresa Y"},
		{Role: "assistant", Content: "anotado"},
	}
	natMu.Unlock()

	// conversa nova: contexto tem que nascer vazio
	b := NovaConversaLocal()
	if b == "" {
		t.Fatal("sem id")
	}
	defer ApagaConversaLocal(b)
	if h := historicoDaConversa(b); len(h) != 0 {
		t.Fatalf("conversa nova herdou %d mensagens da anterior", len(h))
	}

	// e a antiga continua com o contexto dela
	if h := historicoDaConversa(a); len(h) != 2 {
		t.Fatalf("conversa antiga perdeu contexto: %d mensagens", len(h))
	}
}

func TestContextoNaoVazaEntreConversas(t *testing.T) {
	limpaHistMemoria()
	a := NovaConversaLocal()
	b := NovaConversaLocal()
	if a == "" || b == "" {
		t.Fatal("sem id")
	}
	defer ApagaConversaLocal(a)
	defer ApagaConversaLocal(b)

	GuardaMensagensLocal(a, "segredo da conversa A", "resposta A")
	if h := historicoDaConversa(b); len(h) != 0 {
		t.Fatalf("conversa B recebeu %d mensagens da A", len(h))
	}
	h := historicoDaConversa(a)
	if len(h) != 2 || !strings.Contains(h[0].Content, "segredo da conversa A") {
		t.Fatalf("conversa A não releu o próprio histórico: %+v", h)
	}
}

func TestHistoricoRespeitaTeto(t *testing.T) {
	limpaHistMemoria()
	a := NovaConversaLocal()
	if a == "" {
		t.Fatal("sem id")
	}
	defer ApagaConversaLocal(a)

	natMu.Lock()
	for i := 0; i < 40; i++ {
		natHistorico[a] = append(natHistorico[a], Mensagem{Role: "user", Content: "m"})
	}
	natMu.Unlock()

	if h := historicoDaConversa(a); len(h) > natMaxHistorico {
		t.Fatalf("histórico passou do teto: %d > %d", len(h), natMaxHistorico)
	}
}
