package main

// harness_geracao_test.go — testes do bloco 3 (geração longa + adaptadores).
//
// O que está sendo protegido:
//   - resposta cortada (finish=length) vira texto completo, sem repetir trecho;
//   - limite de continuações é respeitado (não vira loop infinito gastando cota);
//   - erro no meio NÃO joga fora o que já foi gerado;
//   - pedido de ferramenta não é tratado como corte;
//   - os dois adaptadores (OpenAI e Gemini) leem o que cada provedor devolve.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// clienteFalso devolve respostas programadas, sem rede.
type clienteFalso struct {
	pedacos      []RespostaGeracao
	erros        []error
	chamadas     int
	piso         int
	ultimoPedido PedidoGeracao
}

// PisoOrcamento permite testar a regra do piso por provedor.
func (c *clienteFalso) PisoOrcamento() int { return c.piso }

func (c *clienteFalso) Nome() string { return "falso" }

func (c *clienteFalso) Gerar(ctx context.Context, p PedidoGeracao) (RespostaGeracao, error) {
	c.ultimoPedido = p
	i := c.chamadas
	c.chamadas++
	var r RespostaGeracao
	if i < len(c.pedacos) {
		r = c.pedacos[i]
	}
	r.Chamadas = 1
	if i < len(c.erros) && c.erros[i] != nil {
		return r, c.erros[i]
	}
	return r, nil
}

func TestGerarLongoContinuaAteTerminar(t *testing.T) {
	cli := &clienteFalso{pedacos: []RespostaGeracao{
		{Texto: "Primeira parte do texto. ", Finish: "length", TokensEntrada: 10, TokensSaida: 5},
		{Texto: "Segunda parte. ", Finish: "length", TokensEntrada: 20, TokensSaida: 6},
		{Texto: "Fim.", Finish: "stop", TokensEntrada: 30, TokensSaida: 3},
	}}
	r, err := GerarLongo(context.Background(), cli,
		PedidoGeracao{Mensagens: []Mensagem{{Role: "user", Content: "escreva"}}}, OpcoesGeracaoLonga{})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cli.chamadas != 3 || r.Chamadas != 3 {
		t.Fatalf("esperava 3 chamadas, tenho %d/%d", cli.chamadas, r.Chamadas)
	}
	esperado := "Primeira parte do texto. Segunda parte. Fim."
	if r.Texto != esperado {
		t.Fatalf("texto colado errado:\n  esperado %q\n  obtido   %q", esperado, r.Texto)
	}
	if r.Finish != "stop" {
		t.Fatalf("finish deveria ser stop, veio %q", r.Finish)
	}
	if r.TokensEntrada != 60 || r.TokensSaida != 14 {
		t.Fatalf("tokens não somaram: %d/%d", r.TokensEntrada, r.TokensSaida)
	}
}

func TestGerarLongoRemoveSobreposicao(t *testing.T) {
	primeira := "O relatório começa aqui e segue com bastante conteúdo descritivo."
	// o modelo repete o fim da primeira ao continuar
	cli := &clienteFalso{pedacos: []RespostaGeracao{
		{Texto: primeira, Finish: "length"},
		{Texto: "conteúdo descritivo. E agora termina.", Finish: "stop"},
	}}
	r, _ := GerarLongo(context.Background(), cli, PedidoGeracao{}, OpcoesGeracaoLonga{})
	if strings.Count(r.Texto, "conteúdo descritivo") != 1 {
		t.Fatalf("trecho repetido não foi removido: %q", r.Texto)
	}
	if !strings.HasSuffix(r.Texto, "E agora termina.") {
		t.Fatalf("final inesperado: %q", r.Texto)
	}
}

func TestGerarLongoRespeitaMaxContinuacoes(t *testing.T) {
	pedacos := make([]RespostaGeracao, 10)
	for i := range pedacos {
		pedacos[i] = RespostaGeracao{Texto: fmt.Sprintf("bloco %d. ", i), Finish: "length"}
	}
	cli := &clienteFalso{pedacos: pedacos}
	r, err := GerarLongo(context.Background(), cli, PedidoGeracao{}, OpcoesGeracaoLonga{MaxContinuacoes: 2})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if cli.chamadas != 3 {
		t.Fatalf("esperava 3 chamadas (1 + 2 continuações), tenho %d", cli.chamadas)
	}
	if r.Finish != "length" {
		t.Fatalf("finish deveria continuar length, veio %q", r.Finish)
	}
}

func TestGerarLongoDevolveParcialEmErro(t *testing.T) {
	cli := &clienteFalso{
		pedacos: []RespostaGeracao{{Texto: "parte gerada. ", Finish: "length"}},
		erros:   []error{nil, errors.New("HTTP 429: quota")},
	}
	r, err := GerarLongo(context.Background(), cli, PedidoGeracao{}, OpcoesGeracaoLonga{})
	if err == nil {
		t.Fatal("esperava erro")
	}
	if !strings.Contains(r.Texto, "parte gerada") {
		t.Fatalf("perdeu o parcial: %q", r.Texto)
	}
}

func TestGerarLongoNaoContinuaComFerramenta(t *testing.T) {
	cli := &clienteFalso{pedacos: []RespostaGeracao{
		{Texto: "", Finish: "tool", ChamadasFerramenta: []map[string]any{{"id": "1"}}},
		{Texto: "não deveria chegar aqui", Finish: "stop"},
	}}
	r, _ := GerarLongo(context.Background(), cli, PedidoGeracao{}, OpcoesGeracaoLonga{})
	if cli.chamadas != 1 {
		t.Fatalf("não deveria continuar quando o modelo pede ferramenta (chamadas=%d)", cli.chamadas)
	}
	if r.Finish != "tool" {
		t.Fatalf("finish deveria ser tool, veio %q", r.Finish)
	}
}

func TestGerarLongoEnviaInstrucaoDeContinuar(t *testing.T) {
	cli := &clienteFalso{pedacos: []RespostaGeracao{
		{Texto: "a", Finish: "length"},
		{Texto: "b", Finish: "stop"},
	}}
	_, _ = GerarLongo(context.Background(), cli, PedidoGeracao{
		Mensagens: []Mensagem{{Role: "user", Content: "escreva o PRD"}}}, OpcoesGeracaoLonga{})
	// na segunda chamada o histórico tem que ter assistant + instrução
	if len(cli.ultimoPedido.Mensagens) < 3 {
		t.Fatalf("histórico de continuação incompleto: %+v", cli.ultimoPedido.Mensagens)
	}
	ultima := cli.ultimoPedido.Mensagens[len(cli.ultimoPedido.Mensagens)-1]
	if !strings.Contains(ultima.Content, "Continue exatamente de onde parou") {
		t.Fatalf("última mensagem não é a instrução de continuar: %q", ultima.Content)
	}
}

func TestNormalizarFinish(t *testing.T) {
	casos := map[string]string{
		"stop": "stop", "STOP": "stop", "end_turn": "stop", "complete": "stop",
		"length": "length", "MAX_TOKENS": "length", "max_tokens": "length",
		"tool_calls": "tool", "tool_use": "tool", "function_call": "tool",
	}
	for entrada, esperado := range casos {
		if got := normalizarFinish(entrada, false); got != esperado {
			t.Fatalf("%q -> %q (esperado %q)", entrada, got, esperado)
		}
	}
	if got := normalizarFinish("stop", true); got != "tool" {
		t.Fatalf("stop com ferramenta deveria virar tool, veio %q", got)
	}
}

func TestClienteOpenAIParse(t *testing.T) {
	var recebido map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&recebido)
		if r.Header.Get("Authorization") != "Bearer teste" {
			t.Errorf("header de autorização errado: %q", r.Header.Get("Authorization"))
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"texto gerado","tool_calls":[]},
			"finish_reason":"length"}],"usage":{"prompt_tokens":12,"completion_tokens":34}}`)
	}))
	defer srv.Close()

	c := &ClienteOpenAI{BaseURL: srv.URL, Chave: "teste"}
	r, err := c.Gerar(context.Background(), PedidoGeracao{
		Modelo: "m", MaxTokens: 100, Temperatura: 0.2,
		Mensagens: []Mensagem{{Role: "user", Content: "oi"}}})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if r.Texto != "texto gerado" || r.Finish != "length" {
		t.Fatalf("parse errado: %+v", r)
	}
	if r.TokensEntrada != 12 || r.TokensSaida != 34 {
		t.Fatalf("tokens errados: %+v", r)
	}
	if recebido["model"] != "m" || recebido["max_tokens"] != float64(100) {
		t.Fatalf("pedido mal montado: %+v", recebido)
	}
}

func TestClienteGeminiParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("caminho inesperado: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"texto do gemini"}]},
			"finishReason":"MAX_TOKENS"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":9}}`)
	}))
	defer srv.Close()

	c := &ClienteGemini{Chave: "k", Base: srv.URL}
	r, err := c.Gerar(context.Background(), PedidoGeracao{
		Modelo: "gemini-x", Mensagens: []Mensagem{{Role: "system", Content: "seja breve"}, {Role: "user", Content: "oi"}}})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if r.Texto != "texto do gemini" {
		t.Fatalf("texto errado: %q", r.Texto)
	}
	if r.Finish != "length" {
		t.Fatalf("MAX_TOKENS deveria virar length, veio %q", r.Finish)
	}
	if r.TokensEntrada != 7 || r.TokensSaida != 9 {
		t.Fatalf("tokens errados: %+v", r)
	}
}

func TestErroHTTPViraErroLegivel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"message":"quota estourada"}}`)
	}))
	defer srv.Close()
	c := &ClienteOpenAI{BaseURL: srv.URL}
	_, err := c.Gerar(context.Background(), PedidoGeracao{Modelo: "m"})
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("erro deveria citar 429 e a mensagem: %v", err)
	}
}

// TestGerarLongoContinuaQuandoCorteVemVazio cobre o caso real de 30/10/2026: modelo com
// raciocínio gasta todo o max_tokens pensando, devolve finish=length e content vazio.
// Isso NÃO é fim de resposta — é pedido de mais espaço.
func TestGerarLongoContinuaQuandoCorteVemVazio(t *testing.T) {
	cli := &clienteFalso{pedacos: []RespostaGeracao{
		{Texto: "", Finish: "length", Reasoning: "pensando muito...", TokensSaida: 150},
		{Texto: "## 1 agora sim o texto", Finish: "stop", TokensSaida: 20},
	}}
	r, err := GerarLongo(context.Background(), cli, PedidoGeracao{}, OpcoesGeracaoLonga{})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if cli.chamadas != 2 {
		t.Fatalf("deveria ter continuado após corte vazio (chamadas=%d)", cli.chamadas)
	}
	if !strings.Contains(r.Texto, "## 1") {
		t.Fatalf("perdeu o texto da continuação: %q", r.Texto)
	}
}

// TestGerarLongoDesisteDepoisDeDuasVazias evita loop gastando cota quando o modelo só pensa.
func TestGerarLongoDesisteDepoisDeDuasVazias(t *testing.T) {
	cli := &clienteFalso{pedacos: []RespostaGeracao{
		{Texto: "", Finish: "length"}, {Texto: "", Finish: "length"},
		{Texto: "", Finish: "length"}, {Texto: "nunca chega", Finish: "stop"},
	}}
	_, _ = GerarLongo(context.Background(), cli, PedidoGeracao{}, OpcoesGeracaoLonga{MaxContinuacoes: 5})
	if cli.chamadas > 3 {
		t.Fatalf("deveria desistir após 2 vazias, fez %d chamadas", cli.chamadas)
	}
}

// TestGerarLongoEscalaOrcamentoQuandoSoPensa cobre o caso do nemotron (30/10/2026):
// content vazio + reasoning presente = o modelo gastou tudo pensando. O harness tem que
// AUMENTAR o max_tokens, não pedir continuação (pedir "continue" faz ele pensar de novo).
func TestGerarLongoEscalaOrcamentoQuandoSoPensa(t *testing.T) {
	cli := &clienteFalso{pedacos: []RespostaGeracao{
		{Texto: "", Finish: "length", Reasoning: "pensando", TokensSaida: 150},
		{Texto: "agora veio o texto", Finish: "stop", TokensSaida: 40},
	}}
	r, err := GerarLongo(context.Background(), cli,
		PedidoGeracao{MaxTokens: 150}, OpcoesGeracaoLonga{MaxContinuacoes: 1})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if !strings.Contains(r.Texto, "agora veio o texto") {
		t.Fatalf("não aproveitou a escalada: %q", r.Texto)
	}
	if cli.ultimoPedido.MaxTokens <= 150 {
		t.Fatalf("deveria ter aumentado o max_tokens, ficou em %d", cli.ultimoPedido.MaxTokens)
	}
	if len(cli.ultimoPedido.Mensagens) != 0 {
		t.Fatalf("escalada não deve inventar mensagem de continuação: %+v", cli.ultimoPedido.Mensagens)
	}
}

// TestGerarLongoAplicaPisoDeOrcamento: pedir 150 tokens num modelo de raciocínio devolve
// resposta vazia (medido em 30/10/2026). O harness tem que subir para o piso antes de gastar
// chamada.
func TestGerarLongoAplicaPisoDeOrcamento(t *testing.T) {
	cli := &clienteFalso{pedacos: []RespostaGeracao{{Texto: "ok", Finish: "stop"}}, piso: harnessOrcamentoMinimo}
	_, err := GerarLongo(context.Background(), cli, PedidoGeracao{MaxTokens: 150}, OpcoesGeracaoLonga{})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if cli.ultimoPedido.MaxTokens < harnessOrcamentoMinimo {
		t.Fatalf("piso não aplicado: enviou max_tokens=%d (piso %d)", cli.ultimoPedido.MaxTokens, harnessOrcamentoMinimo)
	}
}

// TestGerarLongoRetornaErrSoRaciocinou: gastou token e não escreveu nada -> erro tipado.
func TestGerarLongoRetornaErrSoRaciocinou(t *testing.T) {
	cli := &clienteFalso{pedacos: []RespostaGeracao{
		{Texto: "", Finish: "stop", Reasoning: "pensei muito", TokensSaida: 900},
	}}
	r, err := GerarLongo(context.Background(), cli, PedidoGeracao{}, OpcoesGeracaoLonga{})
	if err != ErrSoRaciocinou {
		t.Fatalf("esperava ErrSoRaciocinou, veio %v", err)
	}
	if r.TokensSaida != 900 {
		t.Fatalf("tokens deveriam ser reportados: %d", r.TokensSaida)
	}
}

// TestGerarLongoNaoAplicaPisoQuandoProvedorNaoPrecisa: Gemini escreve com 200 tokens;
// forçar 1.500 atrasaria a resposta e impediria corte controlado.
func TestGerarLongoNaoAplicaPisoQuandoProvedorNaoPrecisa(t *testing.T) {
	cli := &clienteFalso{pedacos: []RespostaGeracao{{Texto: "ok", Finish: "stop"}}, piso: 0}
	_, _ = GerarLongo(context.Background(), cli, PedidoGeracao{MaxTokens: 200}, OpcoesGeracaoLonga{})
	if cli.ultimoPedido.MaxTokens != 200 {
		t.Fatalf("não deveria mexer no orçamento: enviou %d", cli.ultimoPedido.MaxTokens)
	}
}

// TestGeminiDevolveThoughtSignature: o Gemini 3.x exige o thoughtSignature VERBATIM no turno
// do modelo. Sem isso a API responde 400 e o agente morre na primeira ferramenta — foi o
// que aconteceu na VM limpa em 03/11/2026 com as chaves reais do dono.
func TestGeminiDevolveThoughtSignature(t *testing.T) {
	msgs := []Mensagem{
		{Role: "user", Content: "crie uma pasta"},
		{Role: "assistant", ToolCalls: []map[string]any{
			{
				"type":              "function",
				"function":          map[string]any{"name": "criar_pasta", "arguments": `{"nome":"prova"}`},
				"thought_signature": "SIG-abc-123",
			},
		}},
		{Role: "tool", ToolName: "criar_pasta", Content: "pasta criada"},
	}
	contents, _ := montaConteudoGemini(msgs)
	if len(contents) != 3 {
		t.Fatalf("esperava 3 turnos, veio %d", len(contents))
	}
	turnoModelo := contents[1]
	if turnoModelo["role"] != "model" {
		t.Fatalf("turno 1 deveria ser model, veio %v", turnoModelo["role"])
	}
	parts, _ := turnoModelo["parts"].([]map[string]any)
	if len(parts) == 0 {
		t.Fatal("turno do modelo sem parts")
	}
	if _, temFC := parts[0]["functionCall"]; !temFC {
		t.Fatalf("primeira part deveria ser functionCall: %v", parts[0])
	}
	sig, _ := parts[0]["thoughtSignature"].(string)
	if sig != "SIG-abc-123" {
		t.Fatalf("thoughtSignature perdido: %q", sig)
	}
}

// Sem assinatura, a chave nao pode aparecer (nao inventar campo vazio).
func TestGeminiSemAssinaturaNaoInventaCampo(t *testing.T) {
	msgs := []Mensagem{
		{Role: "user", Content: "oi"},
		{Role: "assistant", ToolCalls: []map[string]any{
			{"type": "function", "function": map[string]any{"name": "ler_arquivo", "arguments": `{"caminho":"a.txt"}`}},
		}},
	}
	contents, _ := montaConteudoGemini(msgs)
	parts, _ := contents[1]["parts"].([]map[string]any)
	if _, tem := parts[0]["thoughtSignature"]; tem {
		t.Fatal("nao deveria incluir thoughtSignature vazio")
	}
}
