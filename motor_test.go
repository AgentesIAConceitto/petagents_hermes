package main

// motor_test.go — testes do motor (30/10/2026).
//
// O que não pode regredir:
//   - ferramenta pedida é EXECUTADA e o resultado volta para o modelo;
//   - teto de voltas evita loop gastando cota;
//   - 429/cota estourada troca de provedor em vez de matar o turno;
//   - sem chave, o erro é claro (ErroSemChave);
//   - progresso é reportado (a tela depende disso);
//   - tudo que aconteceu vai para a trilha de eventos da memória.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clienteRoteirizado devolve respostas na ordem e registra o que recebeu.
type clienteRoteirizado struct {
	nome      string
	respostas []RespostaGeracao
	erros     []error
	chamadas  int
	recebidas []PedidoGeracao
}

func (c *clienteRoteirizado) Nome() string {
	if c.nome != "" {
		return c.nome
	}
	return "roteirizado"
}

func (c *clienteRoteirizado) Gerar(ctx context.Context, p PedidoGeracao) (RespostaGeracao, error) {
	c.recebidas = append(c.recebidas, p)
	i := c.chamadas
	c.chamadas++
	var r RespostaGeracao
	if i < len(c.respostas) {
		r = c.respostas[i]
	}
	r.Chamadas = 1
	if i < len(c.erros) && c.erros[i] != nil {
		return r, c.erros[i]
	}
	return r, nil
}

func harnessTemp(t *testing.T) *Harness {
	t.Helper()
	raiz := t.TempDir()
	return NovoHarnessEm(raiz, filepath.Join(t.TempDir(), "memoria.json"))
}

func chamadaFerramenta(id, nome, args string) map[string]any {
	return map[string]any{
		"id":       id,
		"type":     "function",
		"function": map[string]any{"name": nome, "arguments": args},
	}
}

func TestMotorExecutaFerramentaEResponde(t *testing.T) {
	h := harnessTemp(t)
	cli := &clienteRoteirizado{respostas: []RespostaGeracao{
		{Finish: "tool", ChamadasFerramenta: []map[string]any{
			chamadaFerramenta("c1", "criar_pasta", `{"caminho":"projeto-x"}`)}},
		{Texto: "Criei a pasta projeto-x.", Finish: "stop"},
	}}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: "m", Cliente: cli})}

	res, err := m.Responder(context.Background(), nil, "crie a pasta projeto-x")
	if err != nil {
		t.Fatalf("turno falhou: %v", err)
	}
	if res.Voltas != 2 {
		t.Fatalf("esperava 2 voltas, veio %d", res.Voltas)
	}
	if len(res.Ferramentas) != 1 || res.Ferramentas[0] != "criar_pasta" {
		t.Fatalf("ferramenta não registrada: %+v", res.Ferramentas)
	}
	if !strings.Contains(res.Texto, "Criei a pasta") {
		t.Fatalf("resposta final errada: %q", res.Texto)
	}
	// a pasta existe de verdade
	if _, err := os.Stat(filepath.Join(h.Raiz, "projeto-x")); err != nil {
		t.Fatalf("a ferramenta não criou a pasta: %v", err)
	}
	// o resultado da ferramenta voltou para o modelo como mensagem role=tool
	segunda := cli.recebidas[1].Mensagens
	achouTool := false
	for _, msg := range segunda {
		if msg.Role == "tool" && strings.Contains(msg.Content, "pasta pronta") {
			achouTool = true
		}
	}
	if !achouTool {
		t.Fatalf("resultado da ferramenta não voltou ao modelo: %+v", segunda)
	}
	// eventos gravados
	_, eventos := h.Memoria.Quantos()
	if eventos < 2 {
		t.Fatalf("esperava eventos de pedido+ferramenta+resposta, tenho %d", eventos)
	}
}

func TestMotorRespeitaTetoDeVoltas(t *testing.T) {
	h := harnessTemp(t)
	// sempre pede ferramenta: o motor não pode entrar em loop infinito
	resp := RespostaGeracao{Finish: "tool", ChamadasFerramenta: []map[string]any{
		chamadaFerramenta("c", "listar_pasta", `{"caminho":""}`)}}
	cli := &clienteRoteirizado{respostas: []RespostaGeracao{resp, resp, resp, resp, resp, resp, resp}}
	m := &Motor{Harness: h, MaxVoltas: 3, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: "m", Cliente: cli})}

	_, err := m.Responder(context.Background(), nil, "faça algo")
	if err == nil {
		t.Fatal("deveria falhar ao estourar o teto de voltas")
	}
	if cli.chamadas != 3 {
		t.Fatalf("deveria parar em 3 chamadas, fez %d", cli.chamadas)
	}
}

func TestMotorTrocaDeProvedorNo429(t *testing.T) {
	LimparCacheRespostas() // cache global: cada teste comeca limpo
	h := harnessTemp(t)
	primario := &clienteRoteirizado{nome: "google", erros: []error{errors.New("HTTP 429: quota")}}
	secundario := &clienteRoteirizado{nome: "nvidia", respostas: []RespostaGeracao{{Texto: "resposta pelo segundo", Finish: "stop"}}}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(
		ProvedorEscolhido{Modelo: "gemini", Cliente: primario},
		ProvedorEscolhido{Modelo: "gpt-oss", Cliente: secundario},
	)}
	var trocas int
	m.AoProgresso = func(e Etapa) {
		if e.Tipo == "troca" {
			trocas++
		}
	}
	res, err := m.Responder(context.Background(), nil, "oi")
	if err != nil {
		t.Fatalf("deveria ter respondido pelo segundo provedor: %v", err)
	}
	if res.Texto != "resposta pelo segundo" {
		t.Fatalf("resposta errada: %q", res.Texto)
	}
	if res.Trocas != 1 || trocas != 1 {
		t.Fatalf("esperava 1 troca, veio %d/%d", res.Trocas, trocas)
	}
	if res.Provedor != "nvidia" {
		t.Fatalf("provedor final deveria ser nvidia: %q", res.Provedor)
	}
}

func TestMotorSemChave(t *testing.T) {
	m := &Motor{Harness: harnessTemp(t), Cadeia: NovaCadeia()}
	if _, err := m.Responder(context.Background(), nil, "oi"); err != ErroSemChave {
		t.Fatalf("esperava ErroSemChave, veio %v", err)
	}
}

func TestMotorTodosProvedoresFalham(t *testing.T) {
	LimparCacheRespostas() // cache global: cada teste comeca limpo
	h := harnessTemp(t)
	a := &clienteRoteirizado{nome: "a", erros: []error{errors.New("429")}}
	b := &clienteRoteirizado{nome: "b", erros: []error{errors.New("500")}}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(
		ProvedorEscolhido{Modelo: "m1", Cliente: a},
		ProvedorEscolhido{Modelo: "m2", Cliente: b},
	)}
	_, err := m.Responder(context.Background(), nil, "oi")
	if err == nil || !strings.Contains(err.Error(), "nenhum provedor") {
		t.Fatalf("erro deveria dizer que nenhum respondeu: %v", err)
	}
	// a falha fica registrada na memória
	_, eventos := h.Memoria.Quantos()
	if eventos == 0 {
		t.Fatal("deveria registrar o erro na trilha")
	}
}

func TestMotorReportaProgresso(t *testing.T) {
	LimparCacheRespostas() // cache global: cada teste comeca limpo
	h := harnessTemp(t)
	cli := &clienteRoteirizado{respostas: []RespostaGeracao{{Texto: "ok", Finish: "stop"}}}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: "m", Cliente: cli})}
	var tipos []string
	m.AoProgresso = func(e Etapa) { tipos = append(tipos, e.Tipo) }
	if _, err := m.Responder(context.Background(), nil, "oi"); err != nil {
		t.Fatalf("turno: %v", err)
	}
	juntos := strings.Join(tipos, ",")
	for _, esperado := range []string{"pedido", "pensando", "resposta"} {
		if !strings.Contains(juntos, esperado) {
			t.Fatalf("progresso sem %q: %s", esperado, juntos)
		}
	}
}

func TestMotorUsaMemoriaNoPrompt(t *testing.T) {
	h := harnessTemp(t)
	if _, err := h.Memoria.Memorizar("O cliente do projeto X é a empresa Y", []string{"cliente"}); err != nil {
		t.Skip("memória indisponível: " + err.Error())
	}
	cli := &clienteRoteirizado{respostas: []RespostaGeracao{{Texto: "ok", Finish: "stop"}}}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: "m", Cliente: cli})}
	if _, err := m.Responder(context.Background(), nil, "quem é o cliente do projeto X?"); err != nil {
		t.Fatalf("turno: %v", err)
	}
	sistema := cli.recebidas[0].Mensagens[0]
	if sistema.Role != "system" || !strings.Contains(sistema.Content, "empresa Y") {
		t.Fatalf("memória não entrou no prompt de sistema: %q", sistema.Content)
	}
}

func TestMotorHistoricoLongoECortado(t *testing.T) {
	h := harnessTemp(t)
	cli := &clienteRoteirizado{respostas: []RespostaGeracao{{Texto: "ok", Finish: "stop"}}}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: "m", Cliente: cli})}
	var hist []Mensagem
	for i := 0; i < 40; i++ {
		hist = append(hist, Mensagem{Role: "user", Content: "msg"})
	}
	if _, err := m.Responder(context.Background(), hist, "pergunta"); err != nil {
		t.Fatalf("turno: %v", err)
	}
	// system + 12 do histórico + pedido = 14
	if n := len(cli.recebidas[0].Mensagens); n > 14 {
		t.Fatalf("histórico não foi cortado: %d mensagens", n)
	}
}

// TestMotorEscalaOrcamentoQuandoSoRaciocina: 1500 tokens gastos pensando -> sobe para 3000
// e tenta de novo, sem trocar de provedor nem devolver turno vazio.
func TestMotorEscalaOrcamentoQuandoSoRaciocina(t *testing.T) {
	h := harnessTemp(t)
	cli := &clienteRoteirizado{
		respostas: []RespostaGeracao{
			{Texto: "", Finish: "stop", Reasoning: "pensei muito", TokensSaida: 1500},
			{Texto: "resposta depois de escalar", Finish: "stop"},
		},
		erros: []error{ErrSoRaciocinou, nil},
	}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: "m", Cliente: cli})}
	res, err := m.Responder(context.Background(), nil, "tarefa")
	if err != nil {
		t.Fatalf("deveria ter escalado e respondido: %v", err)
	}
	if !strings.Contains(res.Texto, "depois de escalar") {
		t.Fatalf("resposta errada: %q", res.Texto)
	}
	if res.Trocas != 0 {
		t.Fatalf("não deveria trocar de provedor, só aumentar orçamento (trocas=%d)", res.Trocas)
	}
	if len(cli.recebidas) < 2 || cli.recebidas[1].MaxTokens <= cli.recebidas[0].MaxTokens {
		t.Fatalf("orçamento não subiu: %d -> %d", cli.recebidas[0].MaxTokens, cli.recebidas[len(cli.recebidas)-1].MaxTokens)
	}
}

// TestMotorMarcaCooldownDaChaveQueFalhou: quando uma chave leva 429, ela entra em espera e a
// próxima da cadeia assume — é o que permite juntar várias chaves grátis.
func TestMotorMarcaCooldownDaChaveQueFalhou(t *testing.T) {
	LimparCacheRespostas() // cache global: cada teste comeca limpo
	c := AbrirCofre(filepath.Join(t.TempDir(), "c.json"))
	LimparCooldowns()
	for _, k := range []string{"AIza-primeira-111", "AIza-segunda-222"} {
		if err := c.AdicionarChave(ChaveGemini, k); err != nil {
			t.Skip("DPAPI indisponível: " + err.Error())
		}
	}
	h := harnessTemp(t)
	h.Cofre = c

	falha := &clienteRoteirizado{nome: "g1", erros: []error{errors.New("HTTP 429 quota")}}
	ok := &clienteRoteirizado{nome: "g2", respostas: []RespostaGeracao{{Texto: "respondi pela segunda", Finish: "stop"}}}
	m := &Motor{Harness: h, Cadeia: NovaCadeia(
		ProvedorEscolhido{Rotulo: "gemini#1", Modelo: "m", Cliente: falha},
		ProvedorEscolhido{Rotulo: "gemini#2", Modelo: "m", Cliente: ok},
	)}
	res, err := m.Responder(context.Background(), nil, "oi")
	if err != nil {
		t.Fatalf("deveria responder pela segunda chave: %v", err)
	}
	if !strings.Contains(res.Texto, "segunda") {
		t.Fatalf("resposta inesperada: %q", res.Texto)
	}
	if !ChaveEmCooldown("AIza-primeira-111") {
		t.Fatal("a chave que falhou deveria estar em espera")
	}
	if ChaveEmCooldown("AIza-segunda-222") {
		t.Fatal("a chave boa não pode entrar em espera")
	}
	LimparCooldowns()
}
