package main

import "testing"

// TestComandoLocalSoComPalavraSozinha — "saldo" e "status" respondem local (zero token)
// SÓ quando são o comando inteiro. Uma pergunta que apenas CONTÉM a palavra é do agente:
// antes, "qual o saldo do projeto?" devolvia o saldo da DeepSeek e a pergunta nunca
// chegava ao Hermes.
func TestComandoLocalSoComPalavraSozinha(t *testing.T) {
	casos := []struct {
		texto, quer string
	}{
		// comandos: respondem local
		{"saldo", "saldo"},
		{"  Saldo? ", "saldo"},
		{"status", "status"},
		{"STATUS!", "status"},
		{"gateway", "status"},
		{"gateway?", "status"},
		// conversa que só menciona a palavra: vai para o agente
		{"qual o saldo do projeto X?", ""},
		{"me ajuda a calcular o saldo de horas", ""},
		{"configure o gateway do Telegram", ""},
		{"o gateway caiu, o que eu faço?", ""},
		{"status do pedido 123", ""},
		{"/status", ""}, // comando de barra é da ponte (chat_bridge.py)
		{"oi", ""},
	}
	for _, c := range casos {
		if got := comandoLocal(c.texto); got != c.quer {
			t.Errorf("comandoLocal(%q) = %q, queria %q", c.texto, got, c.quer)
		}
	}
}
