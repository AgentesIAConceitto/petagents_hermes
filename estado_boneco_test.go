package main

// estado_boneco_test.go — o boneco não pode parecer triste quando está tudo certo.
// Bug relatado pelo dono em 03/11/2026: no modo grátis (sem Hermes instalado) o app se
// marcava "erro" para sempre e o sprite ia para a linha de erro — o boneco parecia sofrer.

import (
	"strings"
	"testing"
)

func TestBonecoNoModoGratisNuncaEntraEmErro(t *testing.T) {
	casos := []struct {
		nome   string
		chaves int
		age    int64
	}{
		{"sem chave nenhuma", 0, -1},
		{"com uma chave", 1, -1},
		{"com três chaves", 3, -1},
		{"gateway nunca visto", 2, -1},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			agente, estado, detalhe := estadoDoBoneco(ModoNativo, c.chaves, c.age)
			if agente != "nativo" {
				t.Fatalf("agente deveria ser nativo, veio %q", agente)
			}
			if estado == "erro" {
				t.Fatalf("modo grátis NÃO pode mostrar erro (detalhe: %q)", detalhe)
			}
			if estado != "idle" {
				t.Fatalf("esperava idle, veio %q", estado)
			}
		})
	}
}

func TestBonecoSemChaveAvisaSemAssustar(t *testing.T) {
	_, estado, detalhe := estadoDoBoneco(ModoNativo, 0, -1)
	if estado != "idle" {
		t.Fatalf("sem chave ainda é idle (não é erro), veio %q", estado)
	}
	if !strings.Contains(detalhe, "chave") {
		t.Fatalf("o detalhe deveria orientar a configurar a chave: %q", detalhe)
	}
}

func TestBonecoComChavesDizQueOMotorEstaAtivo(t *testing.T) {
	_, _, detalhe := estadoDoBoneco(ModoNativo, 2, 0)
	if !strings.Contains(detalhe, "2") {
		t.Fatalf("deveria informar a quantidade de chaves: %q", detalhe)
	}
}

func TestBonecoNoModoHermesUsaOGateway(t *testing.T) {
	casos := []struct {
		nome     string
		age      int64
		esperado string
	}{
		{"sem heartbeat", -1, "erro"},
		{"ativo agora", 30, "idle"},
		{"sem sinal há 5 min", 300, "pensando"},
		{"sem resposta há 30 min", 1800, "dormindo"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			agente, estado, _ := estadoDoBoneco(ModoHermes, 0, c.age)
			if agente != "hermes" {
				t.Fatalf("agente deveria ser hermes, veio %q", agente)
			}
			if estado != c.esperado {
				t.Fatalf("esperava %q, veio %q", c.esperado, estado)
			}
		})
	}
}
