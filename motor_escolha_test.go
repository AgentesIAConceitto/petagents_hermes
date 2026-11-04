package main

// motor_escolha_test.go — a chave única do motor (03/11/2026).
// Pedido do dono: "uma chave na tela de inteligência definindo se vai ser integrado ao
// Hermes, se vai rodar sozinho com chave própria ou com chaves grátis".

import "testing"

func TestEscolhaDoMotor(t *testing.T) {
	casos := []struct {
		nome     string
		efetivo  ModoAgente
		iaModo   string
		esperado string
	}{
		{"motor próprio sempre é chaves grátis", ModoNativo, "hermes", MotorGratis},
		{"motor próprio ignora modo antigo boneco", ModoNativo, "boneco", MotorGratis},
		{"hermes com IA do dono", ModoHermes, "hermes", MotorHermes},
		{"hermes isolado com chave própria", ModoHermes, "boneco", MotorBoneco},
		{"modo vazio cai em hermes", ModoHermes, "", MotorHermes},
		{"modo com caixa alta", ModoHermes, "BONECO", MotorBoneco},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := escolhaDoMotor(c.efetivo, c.iaModo); got != c.esperado {
				t.Fatalf("esperava %q, veio %q", c.esperado, got)
			}
		})
	}
}

func TestModoHarnessPara(t *testing.T) {
	if modoHarnessPara(MotorGratis) != ModoNativo {
		t.Fatal("chaves grátis = motor nativo")
	}
	if modoHarnessPara(MotorHermes) != ModoHermes {
		t.Fatal("integrado ao Hermes = motor Hermes")
	}
	if modoHarnessPara(MotorBoneco) != ModoHermes {
		t.Fatal("chave própria também roda pelo Hermes")
	}
}
