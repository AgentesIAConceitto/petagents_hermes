package main

// harness_agenda.go — agendador persistente do agente nativo (30/10/2026).
//
// REQUISITO: "agende uma revisão amanhã às 9h" tem que funcionar e SOBREVIVER a fechar o
// app. Agendamento em memória morre no primeiro fechamento — não serve.
//
// DESENHO
//   - agenda.json em %LOCALAPPDATA%\petagent (escrita atômica, 0600), como os outros.
//   - Cada agendamento guarda o PEDIDO (texto para o motor) e a REGRA de tempo.
//   - O runner verifica vencidos a cada 30 s e chama o motor; ao terminar, calcula a
//     próxima ocorrência e grava o resultado (ou o erro) no próprio agendamento.
//   - Relógio injetável (campo agora) para teste não depender de esperar a hora passar.
//
// REGRAS
//   - nunca dispara duas vezes o mesmo agendamento (flag Rodando + Proxima recalculada);
//   - falha não desativa o agendamento: registra o erro e mantém para a próxima;
//   - agendamento de uma vez só desativa sozinho depois de rodar;
//   - no máximo 20 agendamentos ativos (evita a pessoa criar 500 e queimar a cota).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	agendaMaxAtivos = 20
	agendaMaxPedido = 4000
)

// Agendamento é uma tarefa marcada.
type Agendamento struct {
	ID         string    `json:"id"`
	Pedido     string    `json:"pedido"`
	Regra      string    `json:"regra"` // texto original que a pessoa escreveu
	Ativo      bool      `json:"ativo"`
	Criado     time.Time `json:"criado"`
	Proxima    time.Time `json:"proxima"`
	Ultima     time.Time `json:"ultima,omitempty"`
	UltimoErro string    `json:"ultimo_erro,omitempty"`
	Execucoes  int       `json:"execucoes"`
	// Rodando evita disparo duplo; não persiste como verdade (é transiente).
	Rodando bool `json:"-"`
}

// Agenda guarda e executa os agendamentos.
type Agenda struct {
	mu      sync.Mutex
	arquivo string
	itens   []Agendamento
	proxID  int
	agora   func() time.Time
}

// AbrirAgenda carrega (ou cria) a agenda.
func AbrirAgenda(caminho string) *Agenda {
	if caminho == "" {
		caminho = filepath.Join(pastaPetAgente(), "agenda.json")
	}
	a := &Agenda{arquivo: caminho, agora: time.Now}
	if b, err := os.ReadFile(caminho); err == nil {
		_ = json.Unmarshal(b, &a.itens)
	}
	a.proxID = len(a.itens) + 1
	for _, it := range a.itens {
		var n int
		if _, err := fmt.Sscanf(it.ID, "a%d", &n); err == nil && n >= a.proxID {
			a.proxID = n + 1
		}
	}
	return a
}

// definirRelogio troca a fonte de tempo (uso em teste).
func (a *Agenda) definirRelogio(f func() time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.agora = f
}

// salvar grava de forma atômica (chamar com o mutex travado).
func (a *Agenda) salvar() error {
	if err := os.MkdirAll(filepath.Dir(a.arquivo), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a.itens, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.arquivo + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.arquivo)
}

var (
	reDiario    = regexp.MustCompile(`(?i)^(todo dia|todos os dias|diariamente|diario|diária|diaria)\s+(?:às\s+|as\s+)?(\d{1,2})[:h](\d{2})?`)
	reIntervalo = regexp.MustCompile(`(?i)^(a cada|cada)\s+(\d+)\s*(m|min|minuto|minutos|h|hora|horas)`)
	reUmaVez    = regexp.MustCompile(`(?i)^(uma vez|uma|em)\s+(.+)$`)
)

// ProximaOcorrencia interpreta a regra e devolve quando ela deve rodar de novo.
// Regras aceitas (em português, porque é o que a pessoa escreve):
//
//	"todo dia 09:00"      → hoje às 9h se ainda não passou; senão amanhã às 9h
//	"a cada 2h" / "cada 30m" → base + intervalo
//	"uma vez 31/10/2026 09:00" / "uma vez 31/10 09:00" → data informada
//	"2026-10-31T09:00"    → formato ISO
func ProximaOcorrencia(regra string, base time.Time) (time.Time, error) {
	r := strings.TrimSpace(regra)
	if r == "" {
		return time.Time{}, errors.New("regra vazia")
	}

	if m := reDiario.FindStringSubmatch(r); m != nil {
		h, _ := strconv.Atoi(m[2])
		min := 0
		if m[3] != "" {
			min, _ = strconv.Atoi(m[3])
		}
		if h < 0 || h > 23 || min < 0 || min > 59 {
			return time.Time{}, fmt.Errorf("hora inválida em %q", regra)
		}
		alvo := time.Date(base.Year(), base.Month(), base.Day(), h, min, 0, 0, base.Location())
		if !alvo.After(base) {
			alvo = alvo.AddDate(0, 0, 1)
		}
		return alvo, nil
	}

	if m := reIntervalo.FindStringSubmatch(r); m != nil {
		n, err := strconv.Atoi(m[2])
		if err != nil || n <= 0 {
			return time.Time{}, fmt.Errorf("intervalo inválido em %q", regra)
		}
		unidade := strings.ToLower(m[3])
		if strings.HasPrefix(unidade, "h") {
			return base.Add(time.Duration(n) * time.Hour), nil
		}
		return base.Add(time.Duration(n) * time.Minute), nil
	}

	// ISO direto
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, r, base.Location()); err == nil {
			return t, nil
		}
	}

	if m := reUmaVez.FindStringSubmatch(r); m != nil {
		alvo := strings.TrimSpace(m[2])
		for _, layout := range []string{"02/01/2006 15:04", "02/01/2006 15h04", "02/01/2006", "02/01 15:04", "02/01 15h04"} {
			if t, err := time.ParseInLocation(layout, alvo, base.Location()); err == nil {
				if layout == "02/01/2006" || layout == "02/01" {
					t = time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, base.Location())
					if t.Year() == 0 {
						t = t.AddDate(base.Year(), 0, 0)
					}
				}
				if strings.HasPrefix(layout, "02/01 1") {
					t = time.Date(base.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, base.Location())
					if t.Before(base) {
						t = t.AddDate(1, 0, 0)
					}
				}
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("não entendi a data em %q (use 31/10/2026 09:00)", regra)
	}

	return time.Time{}, fmt.Errorf("não entendi quando rodar: %q (use \"todo dia 09:00\", \"a cada 2h\" ou \"uma vez 31/10 09:00\")", regra)
}

// ehUmaVez diz se a regra é de execução única.
func ehUmaVez(regra string) bool {
	r := strings.ToLower(strings.TrimSpace(regra))
	return strings.HasPrefix(r, "uma vez") || strings.HasPrefix(r, "uma ") ||
		regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]`).MatchString(r) ||
		regexp.MustCompile(`^\d{2}/\d{2}(/\d{4})?\s`).MatchString(r)
}

// Agendar cria um agendamento novo.
func (a *Agenda) Agendar(pedido, regra string) (Agendamento, error) {
	pedido = strings.TrimSpace(pedido)
	if pedido == "" {
		return Agendamento{}, errors.New("pedido vazio")
	}
	if len(pedido) > agendaMaxPedido {
		pedido = pedido[:agendaMaxPedido]
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	ativos := 0
	for _, it := range a.itens {
		if it.Ativo {
			ativos++
		}
	}
	if ativos >= agendaMaxAtivos {
		return Agendamento{}, fmt.Errorf("limite de %d agendamentos ativos atingido — cancele algum antes", agendaMaxAtivos)
	}
	// Deduplicação: o modelo às vezes chama a ferramenta duas vezes na mesma resposta
	// (visto no teste de 30/10/2026: [agendar agendar] criou dois). Agendamento idêntico e
	// ativo já existente é reaproveitado, não duplicado.
	regraLimpa := strings.TrimSpace(regra)
	for _, it := range a.itens {
		if it.Ativo && strings.EqualFold(strings.TrimSpace(it.Pedido), pedido) && strings.EqualFold(it.Regra, regraLimpa) {
			return it, nil
		}
	}
	base := a.agora()
	prox, err := ProximaOcorrencia(regra, base)
	if err != nil {
		return Agendamento{}, err
	}
	a.proxID++
	novo := Agendamento{
		ID: fmt.Sprintf("a%d", a.proxID), Pedido: pedido, Regra: regraLimpa,
		Ativo: true, Criado: base, Proxima: prox,
	}
	a.itens = append(a.itens, novo)
	if err := a.salvar(); err != nil {
		return novo, err
	}
	return novo, nil
}

// Listar devolve os agendamentos ordenados pela próxima execução.
func (a *Agenda) Listar() []Agendamento {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Agendamento, len(a.itens))
	copy(out, a.itens)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Proxima.Before(out[j].Proxima) })
	return out
}

// Cancelar desativa (não apaga, para o histórico ficar) um agendamento pelo ID.
func (a *Agenda) Cancelar(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.itens {
		if a.itens[i].ID == strings.TrimSpace(id) {
			a.itens[i].Ativo = false
			_ = a.salvar()
			return true
		}
	}
	return false
}

// Remover apaga de vez.
func (a *Agenda) Remover(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.itens {
		if a.itens[i].ID == strings.TrimSpace(id) {
			a.itens = append(a.itens[:i], a.itens[i+1:]...)
			_ = a.salvar()
			return true
		}
	}
	return false
}

// Vencidos devolve os agendamentos cuja hora já passou (marca como Rodando para não repetir).
func (a *Agenda) Vencidos() []Agendamento {
	a.mu.Lock()
	defer a.mu.Unlock()
	agora := a.agora()
	var due []Agendamento
	for i := range a.itens {
		it := &a.itens[i]
		if !it.Ativo || it.Rodando || it.Proxima.After(agora) {
			continue
		}
		it.Rodando = true
		due = append(due, *it)
	}
	return due
}

// Concluir registra o resultado de uma execução e calcula a próxima.
func (a *Agenda) Concluir(id string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.itens {
		it := &a.itens[i]
		if it.ID != id {
			continue
		}
		agora := a.agora()
		it.Rodando = false
		it.Ultima = agora
		it.Execucoes++
		if err != nil {
			it.UltimoErro = err.Error()
		} else {
			it.UltimoErro = ""
		}
		if ehUmaVez(it.Regra) {
			it.Ativo = false
		} else {
			if prox, errP := ProximaOcorrencia(it.Regra, agora); errP == nil {
				it.Proxima = prox
			} else {
				it.Ativo = false
				it.UltimoErro = "regra inválida: " + errP.Error()
			}
		}
		_ = a.salvar()
		return
	}
}

// ProcessarVencidos roda o que venceu, uma vez. É o que o runner chama a cada tique.
// Devolve quantos agendamentos executou.
func (a *Agenda) ProcessarVencidos(ctx context.Context, exec func(Agendamento) error) int {
	due := a.Vencidos()
	for _, it := range due {
		var err error
		if exec != nil {
			err = exec(it)
		}
		a.Concluir(it.ID, err)
	}
	return len(due)
}

// Iniciar dispara o laço de verificação em background. Devolve função de parada.
func (a *Agenda) Iniciar(ctx context.Context, intervalo time.Duration, exec func(Agendamento) error) func() {
	if intervalo <= 0 {
		intervalo = 30 * time.Second
	}
	ctx, cancelar := context.WithCancel(ctx)
	go func() {
		tique := time.NewTicker(intervalo)
		defer tique.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tique.C:
				a.ProcessarVencidos(ctx, exec)
			}
		}
	}()
	return cancelar
}

// RegistrarFerramentasAgenda dá ao agente o poder de marcar tarefas.
func RegistrarFerramentasAgenda(r *Registro, a *Agenda, memoria *Memoria) {
	r.Registrar(&Ferramenta{
		Nome: "agendar",
		Descricao: "Marca uma tarefa para rodar depois (o app executa sozinho, mesmo fechado e " +
			"reaberto). Use quando a pessoa pedir \"amanhã\", \"toda segunda\", \"daqui 2 horas\".",
		Parametros: schemaObjeto(map[string]any{
			"pedido": schemaString("O que o agente deve fazer quando chegar a hora"),
			"quando": schemaString("Quando: \"todo dia 09:00\", \"a cada 2h\" ou \"uma vez 31/10 09:00\""),
		}, "pedido", "quando"),
		Executar: func(args map[string]any) (string, error) {
			ag, err := a.Agendar(argTexto(args, "pedido"), argTexto(args, "quando"))
			if err != nil {
				return "", err
			}
			if memoria != nil {
				memoria.Evento("agenda", fmt.Sprintf("agendado %s para %s", ag.ID, ag.Proxima.Format("02/01 15:04")))
			}
			return fmt.Sprintf("ok: agendado %s para %s", ag.ID, ag.Proxima.Format("02/01/2006 15:04")), nil
		},
	})

	r.Registrar(&Ferramenta{
		Nome:       "listar_agendamentos",
		Descricao:  "Lista as tarefas agendadas e quando rodam.",
		Parametros: schemaObjeto(map[string]any{}),
		Executar: func(args map[string]any) (string, error) {
			itens := a.Listar()
			if len(itens) == 0 {
				return "nenhum agendamento", nil
			}
			var b strings.Builder
			for _, it := range itens {
				estado := "ativo"
				if !it.Ativo {
					estado = "cancelado"
				}
				b.WriteString(fmt.Sprintf("- [%s] %s · %s · %s · rodou %dx: %s\n",
					it.ID, it.Proxima.Format("02/01/2006 15:04"), estado, it.Regra, it.Execucoes, cortarTexto(it.Pedido, 60)))
				if it.UltimoErro != "" {
					b.WriteString("    último erro: " + cortarTexto(it.UltimoErro, 80) + "\n")
				}
			}
			return b.String(), nil
		},
	})

	r.Registrar(&Ferramenta{
		Nome:       "cancelar_agendamento",
		Descricao:  "Cancela uma tarefa agendada pelo ID.",
		Parametros: schemaObjeto(map[string]any{"id": schemaString("ID do agendamento (ex.: a3)")}, "id"),
		Executar: func(args map[string]any) (string, error) {
			if a.Cancelar(argTexto(args, "id")) {
				return "ok: agendamento cancelado", nil
			}
			return "", errors.New("não achei esse agendamento")
		},
	})
}
