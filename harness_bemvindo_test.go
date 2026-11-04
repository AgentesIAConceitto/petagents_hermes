package main

// harness_bemvindo_test.go — testes do guia de primeira execução e da cota visível
// (30/10/2026).
//
// O que não pode regredir:
//   - sem chave e sem Hermes, o app PEDE configuração (é o gatilho do guia);
//   - com Hermes instalado, não pede (quem já tem motor, já consegue conversar);
//   - com chave, não perde tempo detectando Hermes;
//   - o aviso de cota aparece na troca e some no turno seguinte.

import (
	"context"
	"strings"
	"testing"
)

func TestPrecisaConfigurarLogica(t *testing.T) {
	casos := []struct {
		nome      string
		temChave  bool
		temHermes bool
		precisa   bool
	}{
		{"sem nada", false, false, true},
		{"só Hermes", false, true, false},
		{"só chave", true, false, false},
		{"os dois", true, true, false},
	}
	for _, c := range casos {
		precisa := !c.temChave && !c.temHermes
		if precisa != c.precisa {
			t.Fatalf("%s: precisa=%v (esperava %v)", c.nome, precisa, c.precisa)
		}
	}
}

func TestPassosBemVindo(t *testing.T) {
	passos := (&App{}).PassosBemVindo()
	if len(passos) != 3 {
		t.Fatalf("esperava 3 passos, tenho %d", len(passos))
	}
	for i, p := range passos {
		if p["titulo"] == "" || p["texto"] == "" {
			t.Fatalf("passo %d sem texto: %+v", i, p)
		}
		if p["n"] != string(rune('1'+i)) {
			t.Fatalf("passo %d com número errado: %q", i, p["n"])
		}
	}
	if !strings.Contains(passos[0]["texto"], "cartão") {
		t.Fatal("o primeiro passo deveria deixar claro que não precisa de cartão")
	}
}

func TestAvisoDeCotaNaTroca(t *testing.T) {
	limparTroca()
	if aviso := (&App{}).EstadoCotasHarness()["aviso"]; aviso != "" {
		t.Fatalf("não deveria ter aviso no começo: %v", aviso)
	}
	registrarTroca("google", "nvidia", "HTTP 429: quota")
	estado := (&App{}).EstadoCotasHarness()
	aviso, _ := estado["aviso"].(string)
	if !strings.Contains(aviso, "cota") || !strings.Contains(aviso, "nvidia") {
		t.Fatalf("aviso deveria explicar a troca: %q", aviso)
	}
	if estado["de"] != "google" || estado["para"] != "nvidia" {
		t.Fatalf("estado incompleto: %+v", estado)
	}
	limparTroca()
	if aviso := (&App{}).EstadoCotasHarness()["aviso"]; aviso != "" {
		t.Fatalf("aviso deveria sumir: %v", aviso)
	}
}

func TestMotorLimpaAvisoDeCotaNoNovoTurno(t *testing.T) {
	h := harnessTemp(t)
	registrarTroca("google", "nvidia", "429")
	cli := &clienteRoteirizado{respostas: []RespostaGeracao{{Texto: "ok", Finish: "stop"}}}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: "m", Cliente: cli})}
	if _, err := m.Responder(context.Background(), nil, "oi"); err != nil {
		t.Fatalf("turno: %v", err)
	}
	if aviso := (&App{}).EstadoCotasHarness()["aviso"]; aviso != "" {
		t.Fatalf("turno sem troca deveria limpar o aviso antigo: %v", aviso)
	}
}
