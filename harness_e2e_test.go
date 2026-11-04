package main

// harness_e2e_test.go — teste PONTA A PONTA do harness nativo (30/10/2026).
//
// POR QUE EXISTE: teste unitário prova que cada peça funciona; ele não prova que um MODELO
// de verdade consegue usar as ferramentas para concluir uma tarefa. É esse segundo teste
// que libera (ou não) o push da branch.
//
// Roda só quando pedido, porque faz chamada de API real:
//
//	PETAGENT_HARNESS_E2E=1 NVIDIA_API_KEY=nvapi-... go test -run TestE2E -v
//	PETAGENT_HARNESS_E2E=1 GEMINI_API_KEY=AIza...  go test -run TestE2E -v
//
// O que ele exige do modelo (e é o requisito do produto):
//   1. criar uma pasta;
//   2. escrever um arquivo de texto dentro dela;
//   3. ler o arquivo de volta;
//   4. gravar um fato na memória;
//   5. responder ao usuário dizendo o que fez.
//
// Nada é escrito fora da pasta temporária do teste.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type e2eProvedor struct {
	Nome    string
	URL     string
	Chave   string
	Modelo  string
	Formato string // "openai" ou "gemini"
}

func provedorE2E(t *testing.T) e2eProvedor {
	t.Helper()
	if os.Getenv("PETAGENT_HARNESS_E2E") != "1" {
		t.Skip("teste de integração desligado (defina PETAGENT_HARNESS_E2E=1)")
	}
	if k := os.Getenv("NVIDIA_API_KEY"); k != "" {
		m := os.Getenv("NVIDIA_MODELO")
		if m == "" {
			m = "nvidia/nemotron-3.5-lightning-30b-a3b"
		}
		return e2eProvedor{"nvidia", "https://integrate.api.nvidia.com/v1/chat/completions", k, m, "openai"}
	}
	if k := os.Getenv("GEMINI_API_KEY"); k != "" {
		m := os.Getenv("GEMINI_MODELO")
		if m == "" {
			m = "gemini-3.5-flash-lite"
		}
		return e2eProvedor{"gemini", "https://generativelanguage.googleapis.com/v1beta/models/" + m + ":generateContent?key=" + k, k, m, "gemini"}
	}
	t.Skip("nenhuma chave no ambiente (NVIDIA_API_KEY ou GEMINI_API_KEY)")
	return e2eProvedor{}
}

func httpJSON(t *testing.T, p e2eProvedor, corpo any) (map[string]any, error) {
	t.Helper()
	b, err := json.Marshal(corpo)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", p.URL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.Formato == "openai" {
		req.Header.Set("Authorization", "Bearer "+p.Chave)
	}
	cli := &http.Client{Timeout: 120 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("resposta ilegível (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("HTTP %d: %v", resp.StatusCode, out["error"])
	}
	return out, nil
}

// esquemaGemini converte o schema OpenAI para functionDeclarations.
func esquemaGemini(reg *Registro) []map[string]any {
	var decls []map[string]any
	for _, f := range reg.EsquemaOpenAI() {
		fn := f["function"].(map[string]any)
		decls = append(decls, map[string]any{
			"name":        fn["name"],
			"description": fn["description"],
			"parameters":  fn["parameters"],
		})
	}
	return []map[string]any{{"functionDeclarations": decls}}
}

func TestE2EAgenteUsaFerramentas(t *testing.T) {
	p := provedorE2E(t)
	raiz := t.TempDir()
	h := NovoHarnessEm(raiz, filepath.Join(t.TempDir(), "memoria.json"))

	pedido := "Faça: 1) crie a pasta projeto-x; 2) escreva o arquivo projeto-x/prd.md com um " +
		"PRD curto (objetivo e 3 requisitos); 3) leia o arquivo de volta para conferir; " +
		"4) memorize que o cliente do projeto X é a empresa Y. Depois responda em uma frase o que fez."

	t.Logf("provedor: %s | modelo: %s", p.Nome, p.Modelo)

	// ---- caminho OpenAI (NVIDIA) ----
	if p.Formato == "openai" {
		msgs := []map[string]any{
			{"role": "system", "content": "Você é um agente com ferramentas de arquivo e memória. Use-as."},
			{"role": "user", "content": pedido},
		}
		var final string
		for turno := 1; turno <= 8; turno++ {
			resp, err := httpJSON(t, p, map[string]any{
				"model": p.Modelo, "messages": msgs, "tools": h.Registro.EsquemaOpenAI(),
				"tool_choice": "auto", "temperature": 0, "max_tokens": 1200,
			})
			if err != nil {
				t.Fatalf("turno %d: %v", turno, err)
			}
			msg := ((resp["choices"].([]any))[0].(map[string]any))["message"].(map[string]any)
			tcs, _ := msg["tool_calls"].([]any)
			if len(tcs) == 0 {
				final, _ = msg["content"].(string)
				break
			}
			msgs = append(msgs, msg)
			for _, raw := range tcs {
				tc := raw.(map[string]any)
				fn := tc["function"].(map[string]any)
				nome, _ := fn["name"].(string)
				var args map[string]any
				_ = json.Unmarshal([]byte(fn["arguments"].(string)), &args)
				saida, errF := h.Executar(nome, args)
				if errF != nil {
					saida = "ERRO: " + errF.Error()
				}
				t.Logf("  turno %d · %s(%v) -> %s", turno, nome, args, primeiraLinha(saida))
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": tc["id"], "content": saida})
			}
		}
		if final == "" {
			t.Fatal("agente não deu resposta final")
		}
		t.Logf("resposta final: %s", final)
	}

	// ---- caminho Gemini ----
	if p.Formato == "gemini" {
		contents := []map[string]any{{"role": "user", "parts": []map[string]any{{"text": pedido}}}}
		var final string
		for turno := 1; turno <= 8; turno++ {
			resp, err := httpJSON(t, p, map[string]any{"contents": contents, "tools": esquemaGemini(h.Registro)})
			if err != nil {
				t.Fatalf("turno %d: %v", turno, err)
			}
			cand := (resp["candidates"].([]any))[0].(map[string]any)
			parts := cand["content"].(map[string]any)["parts"].([]any)
			var chamadas []map[string]any
			for _, raw := range parts {
				pt := raw.(map[string]any)
				if _, ok := pt["functionCall"].(map[string]any); ok {
					chamadas = append(chamadas, pt)
				}
			}
			if len(chamadas) == 0 {
				for _, raw := range parts {
					if txt, ok := raw.(map[string]any)["text"].(string); ok {
						final += txt
					}
				}
				break
			}
			// devolve as parts do modelo VERBATIM (leva o thoughtSignature do Gemini 3.x)
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
			var respostas []map[string]any
			for _, pt := range chamadas {
				fc := pt["functionCall"].(map[string]any)
				nome, _ := fc["name"].(string)
				args, _ := fc["args"].(map[string]any)
				saida, errF := h.Executar(nome, args)
				if errF != nil {
					saida = "ERRO: " + errF.Error()
				}
				t.Logf("  turno %d · %s(%v) -> %s", turno, nome, args, primeiraLinha(saida))
				respostas = append(respostas, map[string]any{"functionResponse": map[string]any{
					"name": nome, "response": map[string]any{"resultado": saida}}})
			}
			contents = append(contents, map[string]any{"role": "user", "parts": respostas})
		}
		if final == "" {
			t.Fatal("agente não deu resposta final")
		}
		t.Logf("resposta final: %s", final)
	}

	// ---- o que a tarefa exigia, verificado no disco e na memória ----
	prd := filepath.Join(raiz, "projeto-x", "prd.md")
	b, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("o agente não criou projeto-x/prd.md: %v", err)
	}
	if len(b) < 40 {
		t.Fatalf("PRD curto demais (%d bytes)", len(b))
	}
	t.Logf("PRD gravado: %d bytes", len(b))

	if res := h.Memoria.Buscar("cliente projeto", 5); len(res) == 0 {
		t.Fatal("o agente não memorizou o fato do cliente")
	} else {
		t.Logf("memória: %s", res[0].Fato.Texto)
	}
	if n, ev := h.Memoria.Quantos(); ev == 0 {
		t.Log("aviso: nenhum evento registrado")
	} else {
		t.Logf("memória: %d fato(s), %d evento(s)", n, ev)
	}
}

func primeiraLinha(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 90 {
		s = s[:90]
	}
	return s
}

func TestE2ESandboxNaoEscapa(t *testing.T) {
	p := provedorE2E(t)
	_ = p
	raiz := t.TempDir()
	h := NovoHarnessEm(raiz, filepath.Join(t.TempDir(), "m.json"))
	if _, err := h.Executar("escrever_arquivo", map[string]any{"caminho": "../../fora.txt", "conteudo": "x"}); err == nil {
		t.Fatal("sandbox deixou escrever fora da raiz")
	}
}

// TestE2EGeracaoLonga: corte + continuação com modelo real. Roda no Gemini quando há
// GEMINI_API_KEY, porque ele CORTA mantendo o texto (medido 30/10/2026: maxOutputTokens=200
// → 932 caracteres, finish=MAX_TOKENS, 2,2s). Os modelos de raciocínio da NVIDIA gastam
// 2.300+ caracteres pensando antes de escrever e não servem para este teste.
func TestE2EGeracaoLonga(t *testing.T) {
	gk := os.Getenv("GEMINI_API_KEY")
	if gk == "" {
		t.Skip("defina GEMINI_API_KEY (o teste de corte usa o Gemini, que corta com texto)")
	}
	if os.Getenv("PETAGENT_HARNESS_E2E") != "1" {
		t.Skip("teste de integração desligado (defina PETAGENT_HARNESS_E2E=1)")
	}
	modelo := os.Getenv("GEMINI_MODELO")
	if modelo == "" {
		modelo = "gemini-3.5-flash-lite"
	}
	cli := &ClienteGemini{Chave: gk, Modelo: modelo}
	pedido := PedidoGeracao{
		Modelo:    modelo,
		MaxTokens: 200, // curto: garante MAX_TOKENS com texto presente
		Mensagens: []Mensagem{{Role: "user", Content: "Escreva exatamente 3 secoes numeradas " +
			"(## 1, ## 2, ## 3) sobre financas pessoais. Cada secao com 120 a 150 palavras. " +
			"Nao resuma, nao pule secao, nao repita."}},
	}
	r, err := GerarLongo(context.Background(), cli, pedido, OpcoesGeracaoLonga{MaxContinuacoes: 6})
	if err != nil {
		t.Fatalf("geração longa falhou: %v", err)
	}
	t.Logf("modelo: %s | chamadas: %d | finish: %s | entrada %d tok | saida %d tok | %d chars",
		modelo, r.Chamadas, r.Finish, r.TokensEntrada, r.TokensSaida, len(r.Texto))
	if r.Chamadas < 2 {
		t.Fatalf("esperava pelo menos 2 chamadas (corte + continuação), tenho %d", r.Chamadas)
	}
	for _, secao := range []string{"## 1", "## 2", "## 3"} {
		if !strings.Contains(r.Texto, secao) {
			t.Fatalf("faltou a seção %q — a continuação não completou o documento (%d chars)",
				secao, len(r.Texto))
		}
	}
	if len(r.Texto) < 2500 {
		t.Fatalf("documento curto demais para 3 seções de 120-150 palavras: %d chars", len(r.Texto))
	}
	t.Logf("primeiros 180 chars: %q", primeiraN(r.Texto, 180))
}

// TestE2EModeloSoRaciocina trava o achado de 30/10/2026: modelo de raciocínio com orçamento
// curto consome tokens e não escreve nada. O harness tem que AVISAR (erro tipado) para o
// motor trocar de modelo, em vez de devolver documento vazio ao usuário.
// Ligue com PETAGENT_E2E_RACIOCINIO=1 (gasta ~3k tokens e ~50s).
func TestE2EModeloSoRaciocina(t *testing.T) {
	if os.Getenv("PETAGENT_E2E_RACIOCINIO") != "1" {
		t.Skip("caso lento: defina PETAGENT_E2E_RACIOCINIO=1")
	}
	p := provedorE2E(t)
	if p.Nome != "nvidia" {
		t.Skip("caso específico dos Nemotron (reasoning_content)")
	}
	cli := &ClienteOpenAI{Rotulo: p.Nome, BaseURL: p.URL, Chave: p.Chave}
	r, err := GerarLongo(context.Background(), cli, PedidoGeracao{
		Modelo: "nvidia/nemotron-3.5-lightning-30b-a3b", MaxTokens: 120,
		Mensagens: []Mensagem{{Role: "user", Content: "Escreva um PRD com 3 secoes numeradas, 80-120 palavras cada."}},
	}, OpcoesGeracaoLonga{MaxContinuacoes: 3})
	t.Logf("chamadas=%d saida=%d tok reasoning=%d chars texto=%d chars",
		r.Chamadas, r.TokensSaida, len(r.Reasoning), len(r.Texto))
	if r.Texto == "" && err == nil {
		t.Fatal("deveria devolver ErrSoRaciocinou quando não escreve nada")
	}
	if r.Texto == "" && err != nil && err != ErrSoRaciocinou {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func primeiraN(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// TestE2EAgenteBuscaWeb: o agente tem que usar buscar_web e responder com o que achou.
// A busca real usa Wikipédia (sem chave, sem cota), então este teste não depende de
// provedor de busca pago.
func TestE2EAgenteBuscaWeb(t *testing.T) {
	p := provedorE2E(t)
	raiz := t.TempDir()
	h := NovoHarnessEm(raiz, filepath.Join(t.TempDir(), "m.json"))
	RegistrarFerramentasWeb(h.Registro, ConfigWeb{Wikipedia: "https://pt.wikipedia.org"})

	pedido := "Quem é a atual prefeita ou prefeito de Natal (RN)? Use buscar_web e responda em uma frase."
	var final string
	usouBusca := false
	cli := &ClienteOpenAI{Rotulo: p.Nome, BaseURL: p.URL, Chave: p.Chave}
	modelo := "openai/gpt-oss-20b"
	if p.Nome != "nvidia" {
		cli = nil
		modelo = p.Modelo
	}
	_ = cli

	// loop simples de ferramentas (o mesmo desenho do motor)
	conversa := []map[string]any{{"role": "user", "content": pedido}}
	for turno := 1; turno <= 5; turno++ {
		resp, err := httpJSON(t, p, map[string]any{
			"model": modelo, "messages": conversa, "tools": h.Registro.EsquemaOpenAI(),
			"tool_choice": "auto", "temperature": 0, "max_tokens": 1500,
		})
		if err != nil {
			t.Fatalf("turno %d: %v", turno, err)
		}
		msg := ((resp["choices"].([]any))[0].(map[string]any))["message"].(map[string]any)
		tcs, _ := msg["tool_calls"].([]any)
		if len(tcs) == 0 {
			final, _ = msg["content"].(string)
			break
		}
		conversa = append(conversa, msg)
		for _, raw := range tcs {
			tc := raw.(map[string]any)
			fn := tc["function"].(map[string]any)
			nome, _ := fn["name"].(string)
			var args map[string]any
			_ = json.Unmarshal([]byte(fn["arguments"].(string)), &args)
			if nome == "buscar_web" || nome == "ler_pagina" {
				usouBusca = true
			}
			saida, errF := h.Executar(nome, args)
			if errF != nil {
				saida = "ERRO: " + errF.Error()
			}
			t.Logf("  turno %d · %s(%v) -> %s", turno, nome, args, primeiraLinha(saida))
			conversa = append(conversa, map[string]any{"role": "tool", "tool_call_id": tc["id"], "content": saida})
		}
	}
	if !usouBusca {
		t.Fatal("o agente não usou buscar_web/ler_pagina")
	}
	if strings.TrimSpace(final) == "" {
		t.Fatal("sem resposta final")
	}
	t.Logf("resposta: %s", primeiraN(final, 200))
}

// TestE2EMotorReal roda o MOTOR de produção (motor.go), não um loop de teste: modelo grátis
// de verdade, ferramentas de verdade, memória de verdade.
func TestE2EMotorReal(t *testing.T) {
	p := provedorE2E(t)
	raiz := t.TempDir()
	h := NovoHarnessEm(raiz, filepath.Join(t.TempDir(), "memoria.json"))
	RegistrarFerramentasWeb(h.Registro, ConfigWeb{})

	cli := &ClienteOpenAI{Rotulo: p.Nome, BaseURL: p.URL, Chave: p.Chave}
	modelo := "openai/gpt-oss-20b"
	if p.Nome != "nvidia" {
		cli = nil
		modelo = p.Modelo
	}
	var motor *Motor
	if cli != nil {
		motor = &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: modelo, Cliente: cli})}
	} else {
		motor = &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{
			Modelo: modelo, Cliente: &ClienteGemini{Chave: p.Chave, Modelo: modelo}})}
	}
	var etapas []string
	motor.AoProgresso = func(e Etapa) {
		etapas = append(etapas, e.Tipo+":"+primeiraN(e.Detalhe, 40))
	}

	res, err := motor.Responder(context.Background(), nil,
		"Crie a pasta projeto-x, escreva projeto-x/prd.md com um PRD curto (objetivo e 3 requisitos) "+
			"e memorize que o cliente do projeto X é a empresa Y. Depois responda em uma frase.")
	if err != nil {
		t.Fatalf("motor falhou: %v", err)
	}
	t.Logf("voltas=%d chamadas=%d ferramentas=%v tokens=%d/%d", res.Voltas, res.Chamadas, res.Ferramentas, res.TokensEntrada, res.TokensSaida)
	t.Logf("etapas: %s", strings.Join(etapas, " · "))
	t.Logf("resposta: %s", primeiraN(res.Texto, 180))

	if _, err := os.Stat(filepath.Join(raiz, "projeto-x", "prd.md")); err != nil {
		t.Fatalf("o motor não criou o arquivo: %v", err)
	}
	if busca := h.Memoria.Buscar("cliente projeto", 3); len(busca) == 0 {
		t.Fatal("o motor não memorizou o fato")
	} else {
		t.Logf("memória: %s", busca[0].Fato.Texto)
	}
	if strings.TrimSpace(res.Texto) == "" {
		t.Fatal("sem resposta final")
	}
}

// TestE2EAgendaReal: o agente tem que saber MARCAR uma tarefa quando a pessoa pede.
func TestE2EAgendaReal(t *testing.T) {
	p := provedorE2E(t)
	raiz := t.TempDir()
	h := NovoHarnessEm(raiz, filepath.Join(t.TempDir(), "memoria.json"))
	cli := &ClienteOpenAI{Rotulo: p.Nome, BaseURL: p.URL, Chave: p.Chave}
	modelo := "openai/gpt-oss-20b"
	if p.Nome != "nvidia" {
		cli = nil
		modelo = p.Modelo
	}
	var motor *Motor
	if cli != nil {
		motor = &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: modelo, Cliente: cli})}
	} else {
		motor = &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{
			Modelo: modelo, Cliente: &ClienteGemini{Chave: p.Chave, Modelo: modelo}})}
	}
	res, err := motor.Responder(context.Background(), nil,
		"Agende uma revisão do PRD do projeto X para amanhã às 9h. Use a ferramenta agendar.")
	if err != nil {
		t.Fatalf("motor: %v", err)
	}
	t.Logf("ferramentas: %v | resposta: %s", res.Ferramentas, primeiraN(res.Texto, 140))

	itens := h.Agenda.Listar()
	if len(itens) == 0 {
		t.Fatal("o agente não criou agendamento")
	}
	ag := itens[0]
	t.Logf("agendado: %s | regra=%q | próxima=%s", ag.ID, ag.Regra, ag.Proxima.Format("02/01/2006 15:04"))
	if !strings.Contains(strings.ToLower(ag.Pedido), "revis") {
		t.Fatalf("pedido do agendamento inesperado: %q", ag.Pedido)
	}
	amanha9 := time.Now().AddDate(0, 0, 1)
	alvo := time.Date(amanha9.Year(), amanha9.Month(), amanha9.Day(), 9, 0, 0, 0, time.Local)
	if diff := ag.Proxima.Sub(alvo); diff < -3*time.Hour || diff > 3*time.Hour {
		t.Fatalf("horário agendado inesperado: %s (esperava perto de %s)", ag.Proxima, alvo)
	}
}

// TestE2ELerDocumentoReal: o agente tem que abrir um DOCX e responder com o que leu.
func TestE2ELerDocumentoReal(t *testing.T) {
	p := provedorE2E(t)
	raiz := t.TempDir()
	h := NovoHarnessEm(raiz, filepath.Join(t.TempDir(), "memoria.json"))

	// monta um DOCX de verdade na área de trabalho do agente
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	fmt.Fprint(w, `<?xml version="1.0"?><w:document xmlns:w="w"><w:body>
<w:p><w:r><w:t>Contrato de prestacao de servico</w:t></w:r></w:p>
<w:p><w:r><w:t>Cliente: empresa Y</w:t></w:r></w:p>
<w:p><w:r><w:t>Valor mensal: 4.500 reais</w:t></w:r></w:p>
</w:body></w:document>`)
	_ = zw.Close()
	if err := os.WriteFile(filepath.Join(raiz, "contrato.docx"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	cli := &ClienteOpenAI{Rotulo: p.Nome, BaseURL: p.URL, Chave: p.Chave}
	modelo := "openai/gpt-oss-20b"
	if p.Nome != "nvidia" {
		cli = nil
		modelo = p.Modelo
	}
	var motor *Motor
	if cli != nil {
		motor = &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{Modelo: modelo, Cliente: cli})}
	} else {
		motor = &Motor{Harness: h, Cadeia: NovaCadeia(ProvedorEscolhido{
			Modelo: modelo, Cliente: &ClienteGemini{Chave: p.Chave, Modelo: modelo}})}
	}
	res, err := motor.Responder(context.Background(), nil,
		"Leia o arquivo contrato.docx e me diga o nome do cliente e o valor mensal.")
	if err != nil {
		t.Fatalf("motor: %v", err)
	}
	t.Logf("ferramentas: %v | resposta: %s", res.Ferramentas, primeiraN(res.Texto, 200))
	if !strings.Contains(res.Texto, "Y") {
		t.Fatalf("não citou o cliente: %q", res.Texto)
	}
	if !strings.Contains(strings.ReplaceAll(res.Texto, ".", ""), "4500") {
		t.Fatalf("não citou o valor: %q", res.Texto)
	}
}
