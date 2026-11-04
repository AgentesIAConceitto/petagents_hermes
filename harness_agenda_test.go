package main

// harness_agenda_test.go — testes do agendador (30/10/2026).
//
// O que não pode regredir:
//   - as regras em português são entendidas ("todo dia 09:00", "a cada 2h", "uma vez ...");
//   - não dispara duas vezes o mesmo agendamento;
//   - agendamento de uma vez desativa depois de rodar;
//   - diário e intervalo recalculam a próxima;
//   - falha não desativa (registra erro e mantém);
//   - sobrevive a fechar o app (persistência).

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProximaOcorrenciaRegras(t *testing.T) {
	base := time.Date(2026, 10, 30, 8, 0, 0, 0, time.Local)

	casos := []struct {
		regra  string
		espera time.Time
		erro   bool
	}{
		{"todo dia 09:00", time.Date(2026, 10, 30, 9, 0, 0, 0, time.Local), false},
		{"todo dia 7:30", time.Date(2026, 10, 31, 7, 30, 0, 0, time.Local), false}, // já passou hoje
		{"a cada 2h", base.Add(2 * time.Hour), false},
		{"cada 30m", base.Add(30 * time.Minute), false},
		{"a cada 15 minutos", base.Add(15 * time.Minute), false},
		{"uma vez 31/10/2026 14:00", time.Date(2026, 10, 31, 14, 0, 0, 0, time.Local), false},
		{"2026-11-01T10:00", time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local), false},
		{"qualquer coisa", time.Time{}, true},
		{"", time.Time{}, true},
	}
	for _, c := range casos {
		got, err := ProximaOcorrencia(c.regra, base)
		if c.erro {
			if err == nil {
				t.Fatalf("%q deveria dar erro", c.regra)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q falhou: %v", c.regra, err)
		}
		if !got.Equal(c.espera) {
			t.Fatalf("%q -> %v (esperava %v)", c.regra, got, c.espera)
		}
	}
}

func TestAgendarEListar(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "agenda.json"))
	ag, err := a.Agendar("revisar o PRD do projeto X", "todo dia 09:00")
	if err != nil {
		t.Fatalf("agendar: %v", err)
	}
	if ag.ID == "" || !ag.Ativo {
		t.Fatalf("agendamento inválido: %+v", ag)
	}
	if n := len(a.Listar()); n != 1 {
		t.Fatalf("esperava 1, tenho %d", n)
	}
	// persiste e reabre
	a2 := AbrirAgenda(a.arquivo)
	if n := len(a2.Listar()); n != 1 {
		t.Fatalf("não persistiu: %d", n)
	}
}

func TestNaoDisparaDuasVezes(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "a.json"))
	agora := time.Date(2026, 10, 30, 8, 0, 0, 0, time.Local)
	a.definirRelogio(func() time.Time { return agora })
	if _, err := a.Agendar("tarefa", "a cada 1h"); err != nil {
		t.Fatalf("agendar: %v", err)
	}
	// avança o relógio para depois do vencimento
	agora = agora.Add(2 * time.Hour)
	due := a.Vencidos()
	if len(due) != 1 {
		t.Fatalf("esperava 1 vencido, tenho %d", len(due))
	}
	// segunda chamada antes de concluir não pode devolver de novo
	if again := a.Vencidos(); len(again) != 0 {
		t.Fatalf("disparou duas vezes: %d", len(again))
	}
	a.Concluir(due[0].ID, nil)
	// e depois de concluir também não (a próxima foi recalculada para frente)
	if again := a.Vencidos(); len(again) != 0 {
		t.Fatalf("disparou de novo depois de concluir: %d", len(again))
	}
}

func TestUmaVezDesativaDepoisDeRodar(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "a.json"))
	agora := time.Date(2026, 10, 30, 8, 0, 0, 0, time.Local)
	a.definirRelogio(func() time.Time { return agora })
	ag, err := a.Agendar("tarefa única", "uma vez 30/10/2026 10:00")
	if err != nil {
		t.Fatalf("agendar: %v", err)
	}
	agora = time.Date(2026, 10, 30, 10, 1, 0, 0, time.Local)
	n := a.ProcessarVencidos(context.Background(), func(Agendamento) error { return nil })
	if n != 1 {
		t.Fatalf("deveria rodar 1, rodou %d", n)
	}
	var depois Agendamento
	for _, it := range a.Listar() {
		if it.ID == ag.ID {
			depois = it
		}
	}
	if depois.Ativo {
		t.Fatal("agendamento de uma vez deveria desativar")
	}
	if depois.Execucoes != 1 {
		t.Fatalf("execuções erradas: %d", depois.Execucoes)
	}
}

func TestDiarioRecalculaProxima(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "a.json"))
	agora := time.Date(2026, 10, 30, 8, 0, 0, 0, time.Local)
	a.definirRelogio(func() time.Time { return agora })
	ag, _ := a.Agendar("tarefa diária", "todo dia 09:00")
	agora = time.Date(2026, 10, 30, 9, 5, 0, 0, time.Local)
	a.ProcessarVencidos(context.Background(), func(Agendamento) error { return nil })
	for _, it := range a.Listar() {
		if it.ID == ag.ID {
			if !it.Ativo {
				t.Fatal("diário não deveria desativar")
			}
			if !it.Proxima.Equal(time.Date(2026, 10, 31, 9, 0, 0, 0, time.Local)) {
				t.Fatalf("próxima errada: %v", it.Proxima)
			}
		}
	}
}

func TestFalhaNaoDesativa(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "a.json"))
	agora := time.Date(2026, 10, 30, 8, 0, 0, 0, time.Local)
	a.definirRelogio(func() time.Time { return agora })
	ag, _ := a.Agendar("tarefa", "a cada 1h")
	agora = agora.Add(61 * time.Minute)
	a.ProcessarVencidos(context.Background(), func(Agendamento) error { return errors.New("cota estourou") })
	for _, it := range a.Listar() {
		if it.ID == ag.ID {
			if !it.Ativo {
				t.Fatal("falha não deveria desativar")
			}
			if !strings.Contains(it.UltimoErro, "cota") {
				t.Fatalf("erro não registrado: %q", it.UltimoErro)
			}
		}
	}
}

func TestCancelarERemover(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "a.json"))
	ag, _ := a.Agendar("tarefa", "todo dia 09:00")
	if !a.Cancelar(ag.ID) {
		t.Fatal("cancelar falhou")
	}
	for _, it := range a.Listar() {
		if it.ID == ag.ID && it.Ativo {
			t.Fatal("deveria estar cancelado")
		}
	}
	if !a.Remover(ag.ID) {
		t.Fatal("remover falhou")
	}
	if len(a.Listar()) != 0 {
		t.Fatal("deveria ter removido")
	}
	if a.Cancelar("naoexiste") {
		t.Fatal("não deveria achar ID inexistente")
	}
}

func TestLimiteDeAgendamentos(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "a.json"))
	// pedidos distintos: com dedupe, o mesmo pedido+regra reaproveitaria o agendamento
	for i := 0; i < agendaMaxAtivos; i++ {
		if _, err := a.Agendar(fmt.Sprintf("tarefa %d", i), "todo dia 09:00"); err != nil {
			t.Fatalf("agendar %d: %v", i, err)
		}
	}
	if _, err := a.Agendar("mais uma", "todo dia 09:00"); err == nil {
		t.Fatal("deveria recusar acima do limite")
	}
}

func TestFerramentasAgendaNoRegistro(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "a.json"))
	m := AbrirMemoria(filepath.Join(t.TempDir(), "m.json"))
	r := NovoRegistro()
	RegistrarFerramentasAgenda(r, a, m)

	saida, err := r.Executar("agendar", map[string]any{
		"pedido": "revisar o PRD", "quando": "todo dia 09:00"})
	if err != nil || !strings.Contains(saida, "agendado") {
		t.Fatalf("agendar via ferramenta: %v %q", err, saida)
	}
	lista, err := r.Executar("listar_agendamentos", map[string]any{})
	if err != nil || !strings.Contains(lista, "revisar o PRD") {
		t.Fatalf("listar: %v %q", err, lista)
	}
	var id string
	for _, it := range a.Listar() {
		id = it.ID
	}
	if _, err := r.Executar("cancelar_agendamento", map[string]any{"id": id}); err != nil {
		t.Fatalf("cancelar: %v", err)
	}
	if _, err := r.Executar("cancelar_agendamento", map[string]any{"id": "zzz"}); err == nil {
		t.Fatal("cancelar ID inexistente deveria falhar")
	}
	// regra inválida devolve erro legível
	if _, err := r.Executar("agendar", map[string]any{"pedido": "x", "quando": "quando der vontade"}); err == nil {
		t.Fatal("regra inválida deveria falhar")
	}
}

func TestRunnerExecutaEAvanca(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "a.json"))
	agora := time.Date(2026, 10, 30, 8, 0, 0, 0, time.Local)
	a.definirRelogio(func() time.Time { return agora })
	if _, err := a.Agendar("tarefa", "a cada 10m"); err != nil {
		t.Fatalf("agendar: %v", err)
	}
	var rodados int
	agora = agora.Add(11 * time.Minute)
	parar := a.Iniciar(context.Background(), 10*time.Millisecond, func(Agendamento) error {
		rodados++
		return nil
	})
	time.Sleep(80 * time.Millisecond)
	parar()
	if rodados != 1 {
		t.Fatalf("esperava 1 execução no laço, teve %d", rodados)
	}
	// a próxima foi para frente: nada mais vence agora
	if n := a.ProcessarVencidos(context.Background(), nil); n != 0 {
		t.Fatalf("não deveria vencer de novo: %d", n)
	}
}

// TestAgendarNaoDuplica: o mesmo pedido com a mesma regra reaproveita o agendamento ativo.
func TestAgendarNaoDuplica(t *testing.T) {
	a := AbrirAgenda(filepath.Join(t.TempDir(), "a.json"))
	primeiro, err := a.Agendar("revisar o PRD", "todo dia 09:00")
	if err != nil {
		t.Fatalf("agendar: %v", err)
	}
	segundo, err := a.Agendar("revisar o PRD", "todo dia 09:00")
	if err != nil {
		t.Fatalf("segundo agendar: %v", err)
	}
	if primeiro.ID != segundo.ID {
		t.Fatalf("duplicou: %s != %s", primeiro.ID, segundo.ID)
	}
	if n := len(a.Listar()); n != 1 {
		t.Fatalf("esperava 1 agendamento, tenho %d", n)
	}
	// pedido diferente cria outro
	if terceiro, _ := a.Agendar("outra coisa", "todo dia 09:00"); terceiro.ID == primeiro.ID {
		t.Fatal("pedido diferente deveria criar agendamento novo")
	}
}
