package main

// conversas_nativo_test.go — conversa do modo grátis tem que existir de verdade (04/11/2026).
// O relato foi "ao iniciar uma nova conversa ele diz que o agente não está iniciado", porque
// tudo dependia da ponte do Hermes. Estes testes travam o comportamento local.

import (
	"os"
	"strings"
	"testing"
)

func TestConversaLocalCriaELista(t *testing.T) {
	id := NovaConversaLocal()
	if id == "" {
		t.Fatal("NovaConversaLocal devolveu vazio: sem id não há conversa")
	}
	if !strings.HasPrefix(id, "local-") {
		t.Fatalf("id fora do padrão local-: %q", id)
	}
	defer ApagaConversaLocal(id)

	c := LeConversaLocal(id)
	if c == nil {
		t.Fatal("conversa criada não foi encontrada no disco")
	}
	if len(c.Mensagens) != 0 {
		t.Fatalf("conversa nova deveria nascer vazia, veio com %d mensagens", len(c.Mensagens))
	}

	achou := false
	for _, x := range ListaConversasLocal() {
		if x.ID == id {
			achou = true
		}
	}
	if !achou {
		t.Fatal("conversa criada não aparece na lista")
	}
}

func TestConversaLocalGuardaMensagens(t *testing.T) {
	id := NovaConversaLocal()
	if id == "" {
		t.Fatal("sem id")
	}
	defer ApagaConversaLocal(id)

	GuardaMensagensLocal(id, "qual a cotação do dólar", "R$ 5,50")
	GuardaMensagensLocal(id, "e o euro?", "R$ 6,10")

	c := LeConversaLocal(id)
	if c == nil {
		t.Fatal("conversa sumiu depois de gravar")
	}
	if len(c.Mensagens) != 4 {
		t.Fatalf("esperava 4 mensagens (2 pares), veio %d", len(c.Mensagens))
	}
	if c.Mensagens[0].De != "user" || c.Mensagens[1].De != "assistant" {
		t.Fatalf("ordem errada: %q, %q", c.Mensagens[0].De, c.Mensagens[1].De)
	}
	// o título sai da primeira pergunta — melhor que "Conversa 04/11 16:20"
	if !strings.Contains(c.Titulo, "cotação") {
		t.Fatalf("título deveria vir da primeira pergunta, veio %q", c.Titulo)
	}
}

func TestConversaLocalRenomeiaEApaga(t *testing.T) {
	id := NovaConversaLocal()
	if id == "" {
		t.Fatal("sem id")
	}
	GuardaMensagensLocal(id, "oi", "olá")

	if err := RenomeiaConversaLocal(id, "Cliente Y - orçamento"); err != nil {
		t.Fatalf("renomear falhou: %v", err)
	}
	if c := LeConversaLocal(id); c == nil || c.Titulo != "Cliente Y - orçamento" {
		t.Fatalf("título não foi renomeado: %+v", c)
	}

	if err := ApagaConversaLocal(id); err != nil {
		t.Fatalf("apagar falhou: %v", err)
	}
	if c := LeConversaLocal(id); c != nil {
		t.Fatal("conversa continua existindo depois de apagar")
	}
	if _, err := os.Stat(caminhoConversaLocal(id)); !os.IsNotExist(err) {
		t.Fatal("arquivo da conversa não foi removido do disco")
	}
}

func TestConversaLocalNaoMistura(t *testing.T) {
	a := NovaConversaLocal()
	b := NovaConversaLocal()
	if a == "" || b == "" || a == b {
		t.Fatalf("ids deveriam ser distintos: %q e %q", a, b)
	}
	defer ApagaConversaLocal(a)
	defer ApagaConversaLocal(b)

	GuardaMensagensLocal(a, "pergunta da A", "resposta da A")
	if c := LeConversaLocal(b); c == nil || len(c.Mensagens) != 0 {
		t.Fatal("a conversa B recebeu mensagem da conversa A")
	}
}
