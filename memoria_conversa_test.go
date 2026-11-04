package main

// memoria_conversa_test.go — a memória não pode contar a conversa de outra conversa.
// Causa do relato "abre a conversa nova lembrando do que falou na outra" (04/11/2026):
// ContextoParaPrompt injetava os últimos eventos do harness, e esses eventos eram os
// pedidos/respostas da conversa anterior, sem nenhuma marca de origem.

import (
	"path/filepath"
	"strings"
	"testing"
)

// memoriaNova usa um arquivo temporário: teste nunca toca a memória real do usuário.
func memoriaNova(t *testing.T) *Memoria {
	t.Helper()
	m := AbrirMemoria(filepath.Join(t.TempDir(), "memoria.json"))
	if m == nil {
		t.Fatal("não consegui abrir memória de teste")
	}
	return m
}

func TestMemoriaNaoVazaDialogoEntreConversas(t *testing.T) {
	m := memoriaNova(t)
	m.EventoEm("local-A", "pedido", "meu cliente é a empresa Y")
	m.EventoEm("local-A", "resposta", "anotado, empresa Y")

	novo := m.ContextoParaPromptDe("local-B", "qual o faturamento?", 6, 4)
	if strings.Contains(novo, "empresa Y") {
		t.Fatalf("conversa nova recebeu conteúdo da anterior:\n%s", novo)
	}

	mesma := m.ContextoParaPromptDe("local-A", "e o contrato?", 6, 4)
	if !strings.Contains(mesma, "empresa Y") {
		t.Fatalf("a própria conversa perdeu o contexto:\n%s", mesma)
	}
}

func TestMemoriaMantemFatosGlobais(t *testing.T) {
	m := memoriaNova(t)
	if _, err := m.Memorizar("cliente principal é a Conceitto", []string{"cliente"}); err != nil {
		t.Fatalf("memorizar falhou: %v", err)
	}
	ctx := m.ContextoParaPromptDe("local-qualquer", "quem é meu cliente?", 6, 4)
	if !strings.Contains(ctx, "Conceitto") {
		t.Fatalf("fato memorizado (longo prazo) deveria continuar disponível:\n%s", ctx)
	}
}

func TestMemoriaEventoGlobalEntraSempre(t *testing.T) {
	m := memoriaNova(t)
	m.Evento("erro", "provedor caiu às 10h") // operação, sem conversa
	ctx := m.ContextoParaPromptDe("local-A", "alguma coisa", 6, 4)
	if !strings.Contains(ctx, "provedor caiu") {
		t.Fatalf("evento global deveria aparecer:\n%s", ctx)
	}
}
