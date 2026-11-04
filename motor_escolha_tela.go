package main

// motor_escolha_tela.go — a escolha ÚNICA do motor, na tela de Inteligência (03/11/2026).
//
// PEDIDO DO DONO: "deveria ter uma chave na tela de configuração da inteligência, definindo
// se ele vai ser integrado ao Hermes, se vai rodar sozinho com chave própria ou se vai rodar
// com chaves grátis. Do jeito que ficou está estranho e confuso."
//
// Existiam DOIS lugares decidindo coisas parecidas: o painel Inteligência (modo
// hermes|boneco, sobre a chave do OpenRouter) e o painel Chaves grátis (modo
// auto|hermes|nativo, sobre o motor próprio). Nenhum dos dois dizia "quem responde o chat".
//
// Agora existe UMA escolha, com três opções que correspondem ao que o produto realmente é:
//
//	hermes  → quem responde é o Hermes instalado, com a IA dele
//	boneco  → o Hermes, mas isolado com a SUA chave (o "só o boneco usa")
//	gratis  → motor próprio do PetAgent, com as chaves grátis (não precisa de Hermes)
//
// As duas primeiras exigem Hermes instalado; a terceira não. A escolha é gravada nos dois
// sistemas de propósito (compatibilidade), mas a tela mostra UMA.

import (
	"encoding/json"
	"strings"
)

// Modos que a tela oferece.
const (
	MotorHermes = "hermes"
	MotorBoneco = "boneco"
	MotorGratis = "gratis"
)

// escolhaDoMotor traduz o estado real (motor efetivo + modo da IA antiga) na opção da tela.
// Função pura: é o que os testes travam.
func escolhaDoMotor(efetivo ModoAgente, iaModo string) string {
	if efetivo == ModoNativo {
		return MotorGratis
	}
	if strings.EqualFold(strings.TrimSpace(iaModo), "boneco") {
		return MotorBoneco
	}
	return MotorHermes
}

// modoHarnessPara diz qual valor do harness corresponde à escolha da tela.
func modoHarnessPara(escolha string) ModoAgente {
	if escolha == MotorGratis {
		return ModoNativo
	}
	return ModoHermes
}

// ModoMotorAtual devolve, em JSON, a escolha atual e o que a tela precisa saber para não mentir.
func (a *App) ModoMotorAtual() string {
	h := detectarHermes()
	harn := NovoHarness()
	chaves, escolhidoHarness := 0, ModoAuto
	modeloGratis := ""
	if harn != nil && harn.Cofre != nil {
		chaves = harn.Cofre.TotalChaves()
		escolhidoHarness = harn.Cofre.ModoEscolhido()
		modeloGratis = harn.Cofre.ModeloPadrao()
	}
	efetivo, aviso := ResolverModo(escolhidoHarness, h, chaves > 0)
	ia := leIA()
	escolha := escolhaDoMotor(efetivo, ia.Modo)

	b, _ := json.Marshal(map[string]any{
		"escolha":         escolha,
		"efetivo":         string(efetivo),
		"hermesInstalado": h.Instalado,
		"hermesVersao":    h.Versao,
		"chavesGratis":    chaves,
		"modeloGratis":    modeloGratis,
		"aviso":           aviso,
	})
	return string(b)
}

// DefinirModoMotor é a chave da tela: grava a escolha nos dois sistemas e devolve "" ou erro.
func (a *App) DefinirModoMotor(qual string) string {
	qual = strings.ToLower(strings.TrimSpace(qual))
	switch qual {
	case MotorHermes, MotorBoneco, MotorGratis:
	default:
		return "Escolha inválida."
	}

	h := detectarHermes()
	if qual != MotorGratis && !h.Instalado {
		return "Essa opção precisa do Hermes instalado, e ele não foi encontrado nesta máquina. " +
			"Use \"Chaves grátis\" (funciona sem Hermes) ou instale o Hermes primeiro."
	}
	if qual == MotorBoneco {
		atual := leIA()
		if strings.TrimSpace(atual.Provedor) == "" || strings.TrimSpace(atual.Chave) == "" {
			return "Para usar a sua chave, preencha primeiro o provedor, a chave e o modelo aqui embaixo."
		}
	}

	harn := NovoHarness()
	if harn == nil || harn.Cofre == nil {
		return "Não consegui abrir a configuração do motor próprio."
	}
	if err := harn.Cofre.DefinirModo(modoHarnessPara(qual)); err != nil {
		return "Não consegui salvar a escolha: " + err.Error()
	}

	// o modo da IA antiga ("só o boneco") continua válido e é usado pelo Hermes isolado
	atual := leIA()
	if qual == MotorBoneco {
		atual.Modo = "boneco"
	} else {
		atual.Modo = "hermes"
	}
	if err := gravaIA(atual); err != nil {
		return "Não consegui salvar a escolha: " + err.Error()
	}

	LimparCacheRespostas() // trocar de motor invalida resposta em cache
	logMsg("motor: escolha do dono = " + qual)
	return ""
}
