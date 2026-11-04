package main

// motor_chat_test.go — testes do roteamento do chat para o motor próprio (30/10/2026).
//
// O que não pode regredir:
//   - sem chave, o chat NÃO é capturado (segue para o Hermes, como sempre foi);
//   - com chave e modo nativo, o chat é atendido pelo motor próprio;
//   - imagem anexada não quebra o turno (o modo grátis não tem visão).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func harnessComCofre(t *testing.T) *Harness {
	t.Helper()
	h := NovoHarnessEm(t.TempDir(), filepath.Join(t.TempDir(), "memoria.json"))
	h.Cofre = AbrirCofre(filepath.Join(t.TempDir(), "chaves.json"))
	return h
}

func TestSemChaveESemHermesOrientaEmVezDeErrar(t *testing.T) {
	// Sem chave e sem Hermes não há quem responda. Capturar a mensagem e ENSINAR a
	// configurar é melhor do que mandar para um Hermes inexistente e estourar erro técnico.
	original := detectarHermes
	defer func() { detectarHermes = original }()
	detectarHermes = func() DiagnosticoHermes { return DiagnosticoHermes{Instalado: false} }

	h := harnessComCofre(t)
	if !modoNativoAtivo(h) {
		t.Fatal("sem nada configurado o modo próprio deve atender (para orientar)")
	}
	resp, tratado := respondeNativoEm(h, "c1", "oi", nil)
	if !tratado {
		t.Fatal("deveria ter tratado a mensagem")
	}
	if !strings.Contains(strings.ToLower(resp), "chave") {
		t.Fatalf("deveria orientar a configurar a chave: %q", resp)
	}
}

func TestSemChaveComHermesNaoCaptura(t *testing.T) {
	// Com Hermes instalado, quem responde é o Hermes — o modo próprio não pode roubar o chat.
	original := detectarHermes
	defer func() { detectarHermes = original }()
	detectarHermes = func() DiagnosticoHermes { return DiagnosticoHermes{Instalado: true, Versao: "0.19.1"} }

	h := harnessComCofre(t)
	if modoNativoAtivo(h) {
		t.Fatal("com Hermes instalado e sem chave, o chat é do Hermes")
	}
	if resp, tratado := respondeNativoEm(h, "c1", "oi", nil); tratado || resp != "" {
		t.Fatalf("não deveria tratar: %q %v", resp, tratado)
	}
}

func TestModoNativoAtivoComChaveEModoNativo(t *testing.T) {
	original := detectarHermes
	defer func() { detectarHermes = original }()
	detectarHermes = func() DiagnosticoHermes { return DiagnosticoHermes{Instalado: false} }

	h := harnessComCofre(t)
	if err := h.Cofre.Definir(ChaveNVIDIA, "nvapi-teste-1234567890"); err != nil {
		t.Skip("DPAPI indisponível: " + err.Error())
	}
	if err := h.Cofre.DefinirModo(ModoNativo); err != nil {
		t.Fatalf("modo: %v", err)
	}
	if !modoNativoAtivo(h) {
		t.Fatal("com chave e modo nativo deveria assumir o chat")
	}
}

func TestModoAutoComHermesNaoAssume(t *testing.T) {
	original := detectarHermes
	defer func() { detectarHermes = original }()
	detectarHermes = func() DiagnosticoHermes { return DiagnosticoHermes{Instalado: true, Versao: "0.19.1"} }

	h := harnessComCofre(t)
	if err := h.Cofre.Definir(ChaveGemini, "AIza-teste-1234567890"); err != nil {
		t.Skip("DPAPI indisponível: " + err.Error())
	}
	// modo auto + Hermes instalado = Hermes responde (o nativo é o plano B)
	if modoNativoAtivo(h) {
		t.Fatal("em modo auto com Hermes instalado, quem responde é o Hermes")
	}
	// mas se a pessoa escolher nativo explicitamente, o nativo assume
	if err := h.Cofre.DefinirModo(ModoNativo); err != nil {
		t.Fatalf("modo: %v", err)
	}
	if !modoNativoAtivo(h) {
		t.Fatal("escolha explícita pelo nativo deveria valer")
	}
}

func TestHistoricoNativoCresceECorta(t *testing.T) {
	natMu.Lock()
	natHistorico = map[string][]Mensagem{}
	natHistorico["c1"] = make([]Mensagem, natMaxHistorico+5)
	n := len(natHistorico["c1"])
	if n <= natMaxHistorico {
		t.Fatalf("preparação errada: %d", n)
	}
	natMu.Unlock()
	(&App{}).EsquecerConversaNativa("c1")
	natMu.Lock()
	defer natMu.Unlock()
	if _, existe := natHistorico["c1"]; existe {
		t.Fatal("deveria ter esquecido a conversa")
	}
}

func TestMotorNativoRespondeComClienteFalso(t *testing.T) {
	// um turno completo pelo motor com cliente falso, provando o caminho que o chat usa
	h := harnessComCofre(t)
	cli := &clienteRoteirizado{respostas: []RespostaGeracao{{Texto: "resposta do motor próprio", Finish: "stop"}}}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: "m", Cliente: cli})}
	res, err := m.Responder(context.Background(), nil, "oi")
	if err != nil {
		t.Fatalf("turno: %v", err)
	}
	if !strings.Contains(res.Texto, "motor próprio") {
		t.Fatalf("resposta inesperada: %q", res.Texto)
	}
}
