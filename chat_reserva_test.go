package main

import (
	"strconv"
	"testing"
)

// TestArgsReservaTemTeto — a reserva lenta também tem teto de iterações da IA.
//
// Antes ela rodava `hermes -z`, que monta o agente SEM limite (sys.maxsize): um agente em
// loop só parava no timeout de 5 min, gastando créditos o tempo todo. `hermes chat -Q`
// respeita `--max-turns`.
func TestArgsReservaTemTeto(t *testing.T) {
	args := argsReserva("oi")
	tem := func(alvo ...string) bool {
		for i := 0; i+len(alvo) <= len(args); i++ {
			igual := true
			for j, a := range alvo {
				if args[i+j] != a {
					igual = false
					break
				}
			}
			if igual {
				return true
			}
		}
		return false
	}
	if len(args) < 2 || args[0] != "chat" || args[1] != "-Q" {
		t.Fatalf("argsReserva deveria começar com `chat -Q`: %q", args)
	}
	if maxIteracoesIA < 1 || maxIteracoesIA > 20 {
		t.Fatalf("maxIteracoesIA = %d: tem de ficar entre 1 e 20 (0 é ILIMITADO no Hermes)", maxIteracoesIA)
	}
	if !tem("--max-turns", strconv.Itoa(maxIteracoesIA)) {
		t.Errorf("sem `--max-turns %d`: %q", maxIteracoesIA, args)
	}
	if !tem("--continue", sessaoChat) {
		t.Errorf("sem `--continue %s` (o chat perderia a continuidade): %q", sessaoChat, args)
	}
	for _, a := range args {
		if a == "-z" {
			t.Errorf("ainda usa `-z`, que não tem teto: %q", args)
		}
	}
	// texto que começa com "-" não pode virar flag do argparse
	if args = argsReserva("-oi"); !tem("--query=-oi") {
		t.Errorf(`argsReserva("-oi") deveria levar "--query=-oi" num argumento só: %q`, args)
	}
}
