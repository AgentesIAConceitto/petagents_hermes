package main

// harness_geracao.go — bloco 3 do harness nativo (30/10/2026): geração longa.
//
// PROBLEMA MEDIDO (30/10/2026, free tier): quando o pedido é maior que o teto de saída do
// provedor, a resposta volta com finish_reason="length" e o texto termina no meio.
//   NVIDIA lightning, max_tokens=2000 → finish=length, exatamente 2.000 tokens (cortado)
//   NVIDIA lightning, max_tokens=8000 → finish=stop,   2.148 tokens (completo)
// Ou seja: pedir mais resolve parte do problema; a outra parte é CONTINUAR de onde parou.
// Sem isso, PRD de 2 páginas e arquivo de código grande saem truncados — e o usuário só
// descobre no fim.
//
// Este arquivo entrega:
//   - Cliente: interface de geração (o que o motor nativo precisa de um provedor);
//   - ClienteOpenAI: adaptador OpenAI-compatível (NVIDIA, Groq, OpenRouter, DeepSeek);
//   - ClienteGemini: adaptador do Google (protocolo próprio, tools em functionDeclarations);
//   - GerarLongo: chama, detecta corte e continua, colando os pedaços sem repetir texto.
//
// REGRA: falha no meio NÃO joga fora o que já foi gerado — devolve o parcial junto do erro,
// senão o usuário perde 2 minutos de geração por causa de um 429 no último pedaço.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Mensagem é uma fala no histórico enviado ao provedor.
//
// Precisa carregar chamada de ferramenta porque o motor tem que devolver o resultado:
// no protocolo OpenAI o assistente manda tool_calls e a resposta vai numa mensagem
// role="tool" com tool_call_id; no Gemini é functionCall e functionResponse. Os dois
// adaptadores traduzem a partir destes campos.
type Mensagem struct {
	Role       string // "system" | "user" | "assistant" | "tool"
	Content    string
	ToolCalls  []map[string]any // só em assistant (formato OpenAI: id/type/function)
	ToolCallID string           // só em role="tool"
	ToolName   string           // só em role="tool" (o Gemini exige o nome na resposta)
}

// PedidoGeracao é o que o motor quer gerar.
type PedidoGeracao struct {
	Modelo      string
	Mensagens   []Mensagem
	MaxTokens   int
	Temperatura float64
	// Ferramentas no schema OpenAI (nil = sem ferramentas).
	Ferramentas []map[string]any
}

// RespostaGeracao é o que voltou, já normalizado entre provedores.
type RespostaGeracao struct {
	Texto         string
	Finish        string // "stop" | "length" | "tool" | "outro"
	TokensEntrada int
	TokensSaida   int
	Chamadas      int // quantas requisições HTTP custaram (importa para cota)
	// Reasoning guarda o "pensamento" de modelos que expõem reasoning_content (ex.: os
	// Nemotron na NVIDIA). Ele NÃO entra no Texto: é rascunho, não resposta. Mas precisa
	// ser contado, porque ele consome o mesmo max_tokens — foi assim que uma geração de
	// 150 tokens voltou com finish=length e conteúdo vazio (teste de 30/10/2026).
	Reasoning string
	// ChamadasFerramenta guarda os pedidos de ferramenta do modelo (formato OpenAI).
	ChamadasFerramenta []map[string]any
}

// Cliente sabe falar com um provedor de modelo.
type Cliente interface {
	Nome() string
	Gerar(ctx context.Context, p PedidoGeracao) (RespostaGeracao, error)
}

// clienteComPiso é implementado por provedores cujos modelos gastam o orçamento pensando
// antes de escrever (os Nemotron/gpt-oss na NVIDIA). O piso é POR PROVEDOR, não global:
// o Gemini escreve já com 200 tokens (medido: 200 tokens → 932 caracteres), então forçar
// 1.500 nele só atrasaria a resposta e impediria o corte controlado.
type clienteComPiso interface {
	PisoOrcamento() int
}

// PisoOrcamento devolve o piso medido para os modelos OpenAI-compatíveis da NVIDIA.
func (c *ClienteOpenAI) PisoOrcamento() int { return harnessOrcamentoMinimo }

// PisoOrcamento do Gemini é zero: ele escreve dentro do orçamento pedido.
func (c *ClienteGemini) PisoOrcamento() int { return 0 }

const harnessHTTPTimeout = 70 * time.Second

// harnessOrcamentoMinimo é o piso de max_tokens por chamada.
//
// MEDIDO (30/10/2026, free tier NVIDIA): com max_tokens=150 os modelos de raciocínio
// (nemotron-lightning, gpt-oss-20b) gastam o orçamento inteiro no reasoning_content e
// devolvem content VAZIO com finish=length:
//
//	150 tokens  -> 0 caracteres de resposta (610 de raciocínio)
//	1500 tokens -> 804–945 caracteres de resposta, finish=stop
//
// Pedir menos que isso é queimar chamada para receber nada. Daí o piso.
const harnessOrcamentoMinimo = 1500

// normalizarFinish converte os nomes de cada provedor no nosso vocabulário.
func normalizarFinish(f string, temFerramenta bool) string {
	switch strings.ToLower(f) {
	case "stop", "stop_sequence", "end_turn", "complete":
		if temFerramenta {
			return "tool"
		}
		return "stop"
	case "length", "max_tokens", "max_output_tokens":
		return "length"
	case "tool_calls", "function_call", "tool_use":
		return "tool"
	case "":
		return "outro"
	default:
		return strings.ToLower(f)
	}
}

// ---------------------------------------------------------------------------
// ClienteOpenAI (NVIDIA, Groq, OpenRouter, DeepSeek, qualquer /v1 compatível)
// ---------------------------------------------------------------------------

// ClienteOpenAI fala o protocolo /chat/completions.
type ClienteOpenAI struct {
	Rotulo  string
	BaseURL string // ex.: https://integrate.api.nvidia.com/v1/chat/completions
	Chave   string
}

func (c *ClienteOpenAI) Nome() string {
	if c.Rotulo != "" {
		return c.Rotulo
	}
	return "openai"
}

func (c *ClienteOpenAI) Gerar(ctx context.Context, p PedidoGeracao) (RespostaGeracao, error) {
	var res RespostaGeracao
	msgs := make([]map[string]any, 0, len(p.Mensagens))
	for _, m := range p.Mensagens {
		item := map[string]any{"role": m.Role, "content": m.Content}
		if len(m.ToolCalls) > 0 {
			item["tool_calls"] = m.ToolCalls
			if m.Content == "" {
				item["content"] = nil
			}
		}
		if m.Role == "tool" && m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}
		msgs = append(msgs, item)
	}
	corpo := map[string]any{"model": p.Modelo, "messages": msgs}
	if p.MaxTokens > 0 {
		corpo["max_tokens"] = p.MaxTokens
	}
	if p.Temperatura > 0 {
		corpo["temperature"] = p.Temperatura
	}
	if len(p.Ferramentas) > 0 {
		corpo["tools"] = p.Ferramentas
		corpo["tool_choice"] = "auto"
	}
	j, err := httpPostJSON(ctx, c.BaseURL, c.Chave, corpo)
	if err != nil {
		return res, err
	}
	escolhas, _ := j["choices"].([]any)
	if len(escolhas) == 0 {
		return res, fmt.Errorf("%s: resposta sem choices", c.Nome())
	}
	primeira, _ := escolhas[0].(map[string]any)
	msg, _ := primeira["message"].(map[string]any)
	if txt, ok := msg["content"].(string); ok {
		res.Texto = txt
	}
	if rz, ok := msg["reasoning_content"].(string); ok {
		res.Reasoning = rz
	}
	tcs, _ := msg["tool_calls"].([]any)
	for _, raw := range tcs {
		if tc, ok := raw.(map[string]any); ok {
			res.ChamadasFerramenta = append(res.ChamadasFerramenta, tc)
		}
	}
	fin, _ := primeira["finish_reason"].(string)
	res.Finish = normalizarFinish(fin, len(res.ChamadasFerramenta) > 0)
	if u, ok := j["usage"].(map[string]any); ok {
		res.TokensEntrada = intComoInt(u["prompt_tokens"])
		res.TokensSaida = intComoInt(u["completion_tokens"])
	}
	res.Chamadas = 1
	return res, nil
}

// ---------------------------------------------------------------------------
// ClienteGemini (Google)
// ---------------------------------------------------------------------------

// ClienteGemini fala o protocolo generateContent.
type ClienteGemini struct {
	Chave  string
	Modelo string
	Base   string // padrão: https://generativelanguage.googleapis.com/v1beta
	Rotulo string
}

func (c *ClienteGemini) Nome() string {
	if c.Rotulo != "" {
		return c.Rotulo
	}
	return "gemini"
}

func (c *ClienteGemini) base() string {
	if c.Base != "" {
		return c.Base
	}
	return "https://generativelanguage.googleapis.com/v1beta"
}

// montaConteudoGemini converte as mensagens da casa para o formato do Gemini.
//
// PONTO CRITICO (03/11/2026): o Gemini 3.x exige que o `thoughtSignature` que veio junto do
// functionCall volte VERBATIM no turno do modelo. Sem ele a API responde:
//
//	HTTP 400 Function call is missing a thought_signature in functionCall parts
//
// e o agente morre na primeira ferramenta — foi exatamente o que aconteceu na VM limpa com
// as chaves do dono. Por isso a assinatura viaja dentro do proprio tool call.
func montaConteudoGemini(msgs []Mensagem) ([]map[string]any, string) {
	var contents []map[string]any
	instrucao := ""
	for _, m := range msgs {
		switch m.Role {
		case "system":
			if instrucao != "" {
				instrucao += "\n"
			}
			instrucao += m.Content
		case "assistant":
			parts := []map[string]any{}
			if m.Content != "" {
				parts = append(parts, map[string]any{"text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				fn, _ := tc["function"].(map[string]any)
				nome, _ := fn["name"].(string)
				var args any = map[string]any{}
				if s, ok := fn["arguments"].(string); ok && s != "" {
					_ = json.Unmarshal([]byte(s), &args)
				}
				parte := map[string]any{"functionCall": map[string]any{"name": nome, "args": args}}
				if sig, ok := tc["thought_signature"].(string); ok && sig != "" {
					parte["thoughtSignature"] = sig
				}
				parts = append(parts, parte)
			}
			if len(parts) == 0 {
				parts = append(parts, map[string]any{"text": ""})
			}
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
		case "tool":
			nome := m.ToolName
			if nome == "" {
				nome = "ferramenta"
			}
			contents = append(contents, map[string]any{"role": "user", "parts": []map[string]any{
				{"functionResponse": map[string]any{"name": nome, "response": map[string]any{"resultado": m.Content}}}}})
		default:
			contents = append(contents, map[string]any{"role": "user", "parts": []map[string]any{{"text": m.Content}}})
		}
	}
	return contents, instrucao
}

func (c *ClienteGemini) Gerar(ctx context.Context, p PedidoGeracao) (RespostaGeracao, error) {
	var res RespostaGeracao
	contents, instrucao := montaConteudoGemini(p.Mensagens)
	corpo := map[string]any{"contents": contents}
	if instrucao != "" {
		corpo["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": instrucao}}}
	}
	gen := map[string]any{}
	if p.MaxTokens > 0 {
		gen["maxOutputTokens"] = p.MaxTokens
	}
	if p.Temperatura > 0 {
		gen["temperature"] = p.Temperatura
	}
	if len(gen) > 0 {
		corpo["generationConfig"] = gen
	}
	if len(p.Ferramentas) > 0 {
		var decls []map[string]any
		for _, f := range p.Ferramentas {
			if fn, ok := f["function"].(map[string]any); ok {
				decls = append(decls, map[string]any{
					"name": fn["name"], "description": fn["description"], "parameters": fn["parameters"]})
			}
		}
		corpo["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.base(), p.Modelo, c.Chave)
	j, err := httpPostJSON(ctx, url, "", corpo)
	if err != nil {
		return res, err
	}
	cands, _ := j["candidates"].([]any)
	if len(cands) == 0 {
		return res, fmt.Errorf("%s: resposta sem candidates", c.Nome())
	}
	cand, _ := cands[0].(map[string]any)
	if cont, ok := cand["content"].(map[string]any); ok {
		if parts, ok := cont["parts"].([]any); ok {
			var b strings.Builder
			for _, raw := range parts {
				if pt, ok := raw.(map[string]any); ok {
					if txt, ok := pt["text"].(string); ok {
						b.WriteString(txt)
					}
					if fc, ok := pt["functionCall"].(map[string]any); ok {
						chamada := map[string]any{
							"type":     "function",
							"function": map[string]any{"name": fc["name"], "arguments": mustJSON(fc["args"])},
						}
						// guarda a assinatura para devolver verbatim no proximo turno
						if sig, ok := pt["thoughtSignature"].(string); ok && sig != "" {
							chamada["thought_signature"] = sig
						}
						res.ChamadasFerramenta = append(res.ChamadasFerramenta, chamada)
					}
				}
			}
			res.Texto = b.String()
		}
	}
	fin, _ := cand["finishReason"].(string)
	res.Finish = normalizarFinish(fin, len(res.ChamadasFerramenta) > 0)
	if u, ok := j["usageMetadata"].(map[string]any); ok {
		res.TokensEntrada = intComoInt(u["promptTokenCount"])
		res.TokensSaida = intComoInt(u["candidatesTokenCount"])
	}
	res.Chamadas = 1
	return res, nil
}

// ---------------------------------------------------------------------------
// Geração longa com continuação
// ---------------------------------------------------------------------------

// OpcoesGeracaoLonga controla a continuação.
type OpcoesGeracaoLonga struct {
	// MaxContinuacoes é quantas vezes o agente pode pedir "continue" (padrão 3).
	MaxContinuacoes int
	// AoContinuar é chamado a cada pedaço (para mostrar progresso na tela).
	AoContinuar func(pedaco, acumulado string, chamada int)
}

const instrucaoContinuar = "Continue exatamente de onde parou. Não repita o que já foi escrito, " +
	"não recomece, não escreva introdução. Siga o texto a partir da última palavra."

// GerarLongo chama o provedor e, quando a resposta vem cortada (finish=length), pede a
// continuação e cola os pedaços. Devolve o texto completo e quantas chamadas custou.
//
// Em erro depois de já ter conteúdo, devolve o parcial JUNTO do erro: perder o que já foi
// gerado é pior do que o erro.
func GerarLongo(ctx context.Context, cli Cliente, p PedidoGeracao, op OpcoesGeracaoLonga) (RespostaGeracao, error) {
	if op.MaxContinuacoes <= 0 {
		op.MaxContinuacoes = 3
	}
	var acumulado strings.Builder
	total := RespostaGeracao{}
	msgs := make([]Mensagem, len(p.Mensagens))
	copy(msgs, p.Mensagens)
	vazias := 0
	// Escalada de orçamento: modelo de raciocínio gasta o max_tokens pensando e devolve
	// content vazio. Pedir "continue" não adianta (ele pensa de novo do zero) — o que
	// resolve é dar mais espaço. 30/10/2026: com 150 tokens o nemotron devolvia 0 chars.
	orcamento := p.MaxTokens
	if cp, ok := cli.(clienteComPiso); ok {
		if piso := cp.PisoOrcamento(); piso > 0 && orcamento > 0 && orcamento < piso {
			orcamento = piso
		}
	}
	escaladas := 0

	for chamada := 0; chamada <= op.MaxContinuacoes; chamada++ {
		pedido := p
		pedido.Mensagens = msgs
		if orcamento > 0 {
			pedido.MaxTokens = orcamento
		}
		r, err := cli.Gerar(ctx, pedido)
		total.Chamadas++
		total.TokensEntrada += r.TokensEntrada
		total.TokensSaida += r.TokensSaida
		if r.Texto != "" {
			pedaco := r.Texto
			if acumulado.Len() > 0 {
				pedaco = semSobreposicao(acumulado.String(), pedaco)
			}
			acumulado.WriteString(pedaco)
			if op.AoContinuar != nil {
				op.AoContinuar(pedaco, acumulado.String(), chamada+1)
			}
		}
		total.Finish = r.Finish
		if err != nil {
			total.Texto = acumulado.String()
			return total, err
		}
		// Só para quando o provedor diz que terminou (ou pediu ferramenta, que o motor
		// precisa executar). CORTE COM CONTEÚDO VAZIO É CONTINUAÇÃO, NÃO FIM: modelos com
		// raciocínio (reasoning_content) gastam todo o max_tokens pensando e devolvem
		// finish=length com content vazio. Desistir aqui truncava o documento em zero
		// caracteres — foi o que o teste E2E pegou em 30/10/2026.
		if r.Finish != "length" {
			break
		}
		if r.Texto == "" && len(r.ChamadasFerramenta) == 0 {
			// gastou o orçamento pensando? então aumenta o espaço em vez de pedir continuação
			if r.Reasoning != "" && escaladas < 2 && orcamento > 0 {
				orcamento *= 3
				escaladas++
				continue
			}
			vazias++
			if vazias >= 2 {
				break
			}
		} else {
			vazias = 0
		}
		msgs = append(msgs,
			Mensagem{Role: "assistant", Content: r.Texto},
			Mensagem{Role: "user", Content: instrucaoContinuar},
		)
	}
	total.Texto = acumulado.String()
	if total.Texto == "" && total.TokensSaida > 0 {
		// consumiu tokens e não escreveu nada: foi raciocínio sem resposta
		return total, ErrSoRaciocinou
	}
	return total, nil
}

// semSobreposicao remove do pedaço novo o trecho que já está no fim do acumulado — modelos
// costumam repetir a última linha ao continuar. Só corta sobreposição "de verdade" (>=20
// caracteres) para não comer conteúdo legítimo.
func semSobreposicao(acumulado, novo string) string {
	limite := len(novo)
	if limite > 600 {
		limite = 600
	}
	if len(acumulado) < limite {
		limite = len(acumulado)
	}
	for k := limite; k >= 20; k-- {
		if strings.HasSuffix(acumulado, novo[:k]) {
			return novo[k:]
		}
	}
	return novo
}

// ---------------------------------------------------------------------------
// Utilidades HTTP
// ---------------------------------------------------------------------------

func httpPostJSON(ctx context.Context, url, chave string, corpo any) (map[string]any, error) {
	b, err := json.Marshal(corpo)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancelar := context.WithTimeout(ctx, harnessHTTPTimeout)
	defer cancelar()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if chave != "" {
		req.Header.Set("Authorization", "Bearer "+chave)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	lim := io.LimitReader(resp.Body, 8*1024*1024)
	var j map[string]any
	if err := json.NewDecoder(lim).Decode(&j); err != nil {
		return nil, fmt.Errorf("resposta ilegível (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 300 {
		return j, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resumoErro(j))
	}
	return j, nil
}

func resumoErro(j map[string]any) string {
	if j == nil {
		return "sem corpo"
	}
	if e, ok := j["error"].(map[string]any); ok {
		if m, ok := e["message"].(string); ok {
			return m
		}
	}
	b, _ := json.Marshal(j)
	if len(b) > 200 {
		b = b[:200]
	}
	return string(b)
}

func intComoInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ErrSoRaciocinou é devolvido quando o modelo gastou o orçamento pensando (reasoning_content)
// e não escreveu nenhum caractere de resposta, mesmo depois de aumentar o orçamento.
//
// 30/10/2026, medido: nvidia/nemotron-3.5-lightning-30b-a3b devolveu 3.300 tokens de
// raciocínio e ZERO de resposta em 4 chamadas. Continuar pedindo não resolve — quem chama
// (o motor) deve trocar de modelo. Por isso o erro é tipado, e não um texto solto.
var ErrSoRaciocinou = errors.New("modelo só raciocinou e não escreveu resposta (troque de modelo)")
