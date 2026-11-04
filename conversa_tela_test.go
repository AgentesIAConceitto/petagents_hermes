package main

// conversa_tela_test.go — o formato que a TELA lê (04/11/2026).
// Relato: "não consigo nem trocar de conversa". O clique chamava ConversaAbrir, mas eu
// devolvia role/content/quando-ISO; a tela espera quem/texto/quando-em-segundos. Resultado:
// a conversa abria vazia e o clique parecia morto.

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMensagensParaTelaFormato(t *testing.T) {
	c := &conversaLocal{
		ID:     "local-teste",
		Titulo: "teste",
		Mensagens: []mensagemLocal{
			{De: "user", Texto: "bom dia", Quando: "2026-11-04T16:31:00Z"},
			{De: "assistant", Texto: "olá!", Quando: "2026-11-04T16:31:02Z"},
		},
	}
	msgs := MensagensParaTela(c)
	if len(msgs) != 2 {
		t.Fatalf("esperava 2 mensagens, veio %d", len(msgs))
	}
	if msgs[0]["quem"] != "eu" {
		t.Fatalf("mensagem do usuário deveria ser quem=eu, veio %v", msgs[0]["quem"])
	}
	if msgs[1]["quem"] != "ele" {
		t.Fatalf("mensagem do agente deveria ser quem=ele, veio %v", msgs[1]["quem"])
	}
	if msgs[0]["texto"] != "bom dia" {
		t.Fatalf("texto não veio: %v", msgs[0]["texto"])
	}
	seg, ok := msgs[0]["quando"].(int64)
	if !ok || seg == 0 {
		t.Fatalf("quando deveria ser segundos (int64 != 0), veio %v (%T)", msgs[0]["quando"], msgs[0]["quando"])
	}
	// a tela faz new Date(seg*1000): segundos, não milissegundos
	if seg > 20000000000 {
		t.Fatalf("quando parece milissegundos: %d", seg)
	}
}

func TestMensagensParaTelaJSON(t *testing.T) {
	c := &conversaLocal{ID: "x", Mensagens: []mensagemLocal{{De: "user", Texto: "oi", Quando: "2026-11-04T16:31:00Z"}}}
	b, err := json.Marshal(MensagensParaTela(c))
	if err != nil {
		t.Fatalf("marshal falhou: %v", err)
	}
	s := string(b)
	for _, campo := range []string{`"quem"`, `"texto"`, `"quando"`} {
		if !strings.Contains(s, campo) {
			t.Fatalf("JSON sem o campo %s: %s", campo, s)
		}
	}
	if strings.Contains(s, `"role"`) || strings.Contains(s, `"content"`) {
		t.Fatalf("JSON ainda tem o formato antigo: %s", s)
	}
}

func TestMensagensParaTelaVazia(t *testing.T) {
	if got := MensagensParaTela(nil); len(got) != 0 {
		t.Fatalf("conversa nil deveria dar lista vazia, veio %d", len(got))
	}
	if got := MensagensParaTela(&conversaLocal{}); len(got) != 0 {
		t.Fatalf("conversa vazia deveria dar lista vazia, veio %d", len(got))
	}
}
