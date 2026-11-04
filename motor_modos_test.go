package main

// motor_modos_test.go — testes da escolha de motor e da detecção do Hermes (30/10/2026).
//
// O que não pode regredir:
//   - modo "hermes" sem Hermes instalado cai para nativo COM aviso de como instalar;
//   - modo "nativo" nunca finge que vai usar Hermes;
//   - "auto" prefere Hermes quando existe;
//   - a escolha persiste no cofre.

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolverModo(t *testing.T) {
	comHermes := DiagnosticoHermes{Instalado: true, Versao: "0.19.1", Caminho: `C:\x\python.exe`}
	semHermes := DiagnosticoHermes{Instalado: false, Motivo: "não encontrei"}

	casos := []struct {
		nome      string
		escolhido ModoAgente
		diag      DiagnosticoHermes
		chave     bool
		espera    ModoAgente
		aviso     bool
	}{
		{"auto com hermes", ModoAuto, comHermes, true, ModoHermes, false},
		{"auto sem hermes, com chave", ModoAuto, semHermes, true, ModoNativo, false},
		{"auto sem hermes e sem chave", ModoAuto, semHermes, false, ModoNativo, true},
		{"hermes pedido e instalado", ModoHermes, comHermes, false, ModoHermes, false},
		{"hermes pedido e ausente", ModoHermes, semHermes, true, ModoNativo, true},
		{"nativo pedido com chave", ModoNativo, comHermes, true, ModoNativo, false},
		{"nativo pedido sem chave", ModoNativo, semHermes, false, ModoNativo, true},
	}
	for _, c := range casos {
		modo, aviso := ResolverModo(c.escolhido, c.diag, c.chave)
		if modo != c.espera {
			t.Fatalf("%s: modo %s (esperava %s)", c.nome, modo, c.espera)
		}
		if (aviso != "") != c.aviso {
			t.Fatalf("%s: aviso=%q (esperava aviso? %v)", c.nome, aviso, c.aviso)
		}
	}
}

func TestAvisoDeHermesAusenteEnsinaInstalar(t *testing.T) {
	_, aviso := ResolverModo(ModoHermes, DiagnosticoHermes{Instalado: false}, true)
	if !strings.Contains(aviso, "pip") {
		t.Fatalf("aviso deveria ensinar a instalar com pip/pipx: %q", aviso)
	}
	if !strings.Contains(strings.ToLower(aviso), "modo nativo") {
		t.Fatalf("aviso deveria dizer que caiu para o nativo: %q", aviso)
	}
}

func TestModoPersisteNoCofre(t *testing.T) {
	c := AbrirCofre(filepath.Join(t.TempDir(), "c.json"))
	if got := c.ModoEscolhido(); got != ModoAuto {
		t.Fatalf("padrão deveria ser auto, veio %s", got)
	}
	if err := c.DefinirModo(ModoNativo); err != nil {
		t.Fatalf("definir modo: %v", err)
	}
	if got := AbrirCofre(c.arquivo).ModoEscolhido(); got != ModoNativo {
		t.Fatalf("não persistiu: %s", got)
	}
	if err := c.DefinirModo(ModoAgente("bobagem")); err == nil {
		t.Fatal("modo inválido deveria ser recusado")
	}
}

func TestComoInstalarHermesTemComando(t *testing.T) {
	if !strings.Contains(comoInstalarHermes, "hermes-agent") {
		t.Fatalf("texto de instalação sem o pacote: %q", comoInstalarHermes)
	}
}

// TestDetectarHermesNestaMaquina usa a instalação real quando existe (aqui existe: 0.19.x).
// Em máquina sem Hermes, o teste é pulado — não é falha.
func TestDetectarHermesNestaMaquina(t *testing.T) {
	d := DetectarHermes()
	if !d.Instalado {
		t.Skip("sem Hermes nesta máquina: " + d.Motivo)
	}
	if d.Versao == "" || d.Caminho == "" {
		t.Fatalf("detecção incompleta: %+v", d)
	}
	t.Logf("Hermes detectado: versão %s em %s", d.Versao, d.Caminho)
}
