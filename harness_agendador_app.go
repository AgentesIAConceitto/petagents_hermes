package main

// harness_agendador_app.go — liga o agendador ao app (30/10/2026).
//
// O agendador já sabe QUANDO rodar; aqui ele ganha QUEM executa: o motor nativo, com a
// chave que o usuário configurou na tela (cofre). É o que faz o PetAgent cumprir
// "me lembra amanhã às 9h" mesmo depois de fechar e reabrir o app.
//
// Regras:
//   - um único agendador rodando por vez (Iniciar é idempotente);
//   - se não houver chave configurada, o agendador NÃO sobe (e diz por quê);
//   - cada execução vira evento na memória (dá para ver o que rodou e o que falhou);
//   - falha em um agendamento não derruba os outros.

import (
	"context"
	"fmt"
	"sync"
	"time"
)

var (
	agendadorMu    sync.Mutex
	agendadorParar func()
	agendadorErro  string
)

// constroiMotor monta o motor nativo com o que está configurado no cofre.
func constroiMotor(h *Harness) (*Motor, error) {
	if h == nil || h.Cofre == nil {
		return nil, ErroSemChave
	}
	cadeia := h.Cofre.CadeiaDeChaves(h.Cofre.ModeloPadrao(), h.Cofre.ModeloPadrao())
	if cadeia.Vazia() {
		return nil, ErroSemChave
	}
	return &Motor{Harness: h, Cadeia: cadeia}, nil
}

// IniciarAgendador sobe o laço do agendador. Devolve "" em caso de sucesso ou a razão.
func (a *App) IniciarAgendador() string {
	agendadorMu.Lock()
	defer agendadorMu.Unlock()
	if agendadorParar != nil {
		return "" // já está rodando
	}
	h := NovoHarness()
	// o agendador executa pelo MOTOR NATIVO. No modo Hermes, quem agenda é o fluxo do
	// Hermes (aqui não temos como disparar o bridge), então avisamos em vez de fingir.
	temChave := h.Cofre != nil && h.Cofre.TotalChaves() > 0
	modo, _ := ResolverModo(h.Cofre.ModoEscolhido(), detectarHermes(), temChave)
	if modo == ModoHermes {
		agendadorErro = "no modo Hermes o agendador usa o fluxo do Hermes; para agendar pelo app, escolha o modo nativo"
		return agendadorErro
	}
	motor, err := constroiMotor(h)
	if err != nil {
		agendadorErro = "sem chave configurada: o agendador só roda quando houver NVIDIA ou Google configurado"
		return agendadorErro
	}
	agendadorErro = ""
	exec := func(ag Agendamento) error {
		ctx, cancelar := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancelar()
		res, err := motor.Responder(ctx, nil, ag.Pedido)
		if err != nil {
			if h.Memoria != nil {
				h.Memoria.Evento("agenda", fmt.Sprintf("%s falhou: %v", ag.ID, err))
			}
			return err
		}
		if h.Memoria != nil {
			h.Memoria.Evento("agenda", fmt.Sprintf("%s ok: %s", ag.ID, cortarTexto(res.Texto, 120)))
		}
		return nil
	}
	agendadorParar = h.Agenda.Iniciar(context.Background(), 30*time.Second, exec)
	return ""
}

// agendadorLoop é chamado no startup: sobe o agendador se houver chave e registra o motivo
// quando não houver (sem chave, o app continua funcionando pelo modo Hermes).
func (a *App) agendadorLoop() {
	if msg := a.IniciarAgendador(); msg != "" {
		logMsg("agendador: não iniciado — " + msg)
		return
	}
	logMsg("agendador: iniciado")
}

// PararAgendador desliga o laço (chamado ao fechar o app).
func (a *App) PararAgendador() string {
	agendadorMu.Lock()
	defer agendadorMu.Unlock()
	if agendadorParar == nil {
		return ""
	}
	agendadorParar()
	agendadorParar = nil
	return ""
}

// EstadoAgendador diz se está rodando e por que não (para a tela).
func (a *App) EstadoAgendador() map[string]any {
	agendadorMu.Lock()
	defer agendadorMu.Unlock()
	rodando := agendadorParar != nil
	msg := ""
	if !rodando && agendadorErro != "" {
		msg = agendadorErro
	}
	return map[string]any{"rodando": rodando, "aviso": msg}
}

// AgendamentosHarness lista os agendamentos para a tela.
func (a *App) AgendamentosHarness() []map[string]any {
	h := NovoHarness()
	out := []map[string]any{}
	for _, it := range h.Agenda.Listar() {
		estado := "ativo"
		if !it.Ativo {
			estado = "cancelado"
		}
		out = append(out, map[string]any{
			"id":         it.ID,
			"pedido":     it.Pedido,
			"regra":      it.Regra,
			"estado":     estado,
			"proxima":    it.Proxima.Format("02/01/2006 15:04"),
			"ultima":     formatarSeTem(it.Ultima),
			"execucoes":  it.Execucoes,
			"ultimoErro": it.UltimoErro,
		})
	}
	return out
}

func formatarSeTem(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("02/01/2006 15:04")
}

// AgendarHarness cria um agendamento pela tela (ou pelo motor). Devolve "" ou o erro.
func (a *App) AgendarHarness(pedido, quando string) string {
	h := NovoHarness()
	ag, err := h.Agenda.Agendar(pedido, quando)
	if err != nil {
		return err.Error()
	}
	return "ok: " + ag.ID + " em " + ag.Proxima.Format("02/01/2006 15:04")
}

// CancelarAgendamentoHarness desativa um agendamento pela tela.
func (a *App) CancelarAgendamentoHarness(id string) string {
	h := NovoHarness()
	if !h.Agenda.Cancelar(id) {
		return "não achei esse agendamento"
	}
	return ""
}
