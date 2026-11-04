package main

// harness_web_test.go — testes das ferramentas de web (30/10/2026).
//
// O que não pode regredir:
//   - o modelo não consegue pedir file:// nem endereço interno (SSRF);
//   - página é limpa (script/style fora) e cortada no limite;
//   - busca cai para Wikipédia quando o SearxNG não responde;
//   - erro de rede vira mensagem legível, não pânico.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestURLEstranhaEBloqueada(t *testing.T) {
	bloqueadas := []string{
		"file:///C:/Windows/win.ini",
		"ftp://exemplo.com/arq",
		"http://localhost:8080/admin",
		"http://127.0.0.1:31415/",
		"http://192.168.0.1/",
		"http://10.0.0.5/",
		"http://169.254.1.1/",
		"javascript:alert(1)",
		"",
	}
	for _, u := range bloqueadas {
		if _, err := urlPermitida(u); err == nil {
			t.Fatalf("deveria bloquear %q", u)
		}
	}
	for _, u := range []string{"https://g1.globo.com/", "http://pt.wikipedia.org/wiki/Natal"} {
		if _, err := urlPermitida(u); err != nil {
			t.Fatalf("deveria permitir %q: %v", u, err)
		}
	}
}

func TestLimparHTML(t *testing.T) {
	h := `<html><head><title>x</title><style>body{color:red}</style></head>
	<body><script>alert('oi')</script><h1>Titulo</h1><p>Primeiro paragrafo.</p>
	<p>Segundo&nbsp;paragrafo &amp; mais.</p></body></html>`
	s := limparHTML(h)
	if strings.Contains(s, "alert") || strings.Contains(s, "color:red") {
		t.Fatalf("script/style vazaram: %q", s)
	}
	if !strings.Contains(s, "Titulo") || !strings.Contains(s, "Segundo paragrafo") {
		t.Fatalf("texto visível faltando: %q", s)
	}
	if strings.Contains(s, "<") {
		t.Fatalf("sobrou tag: %q", s)
	}
}

func TestLerPaginaComServidorFalso(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><script>ruim()</script><p>Conteudo bom da pagina.</p></body></html>`)
	}))
	defer srv.Close()

	cfg := ConfigWeb{HTTP: &http.Client{}, MaxChars: 100}.comDefaults()
	// 127.0.0.1 é bloqueado pelo SSRF: para testar a leitura usamos a URL do servidor
	// apenas se o guard permitir localhost — não permite, então validamos o bloqueio aqui
	// e testamos a limpeza pelo caminho interno.
	if _, err := cfg.lerPagina(srv.URL); err == nil {
		t.Fatal("deveria bloquear 127.0.0.1 (SSRF)")
	}
	out, err := cfg.lerPaginaForcadoParaTeste(srv.URL)
	if err != nil {
		t.Fatalf("leitura forçada falhou: %v", err)
	}
	if !strings.Contains(out, "Conteudo bom da pagina") || strings.Contains(out, "ruim()") {
		t.Fatalf("página mal limpa: %q", out)
	}
	if !strings.Contains(out, "KB baixados") {
		t.Fatalf("cabeçalho ausente: %q", out)
	}
}

func TestLerPaginaCortaTexto(t *testing.T) {
	longo := strings.Repeat("palavra ", 500)
	cfg := ConfigWeb{MaxChars: 50}.comDefaults()
	out := cortarTexto(longo, cfg.MaxChars)
	if !strings.Contains(out, "texto cortado") {
		t.Fatalf("não sinalizou corte: %q", out)
	}
	if len([]rune(out)) > cfg.MaxChars+30 {
		t.Fatalf("corte não respeitado: %d runas", len([]rune(out)))
	}
}

func TestBuscaSearxEParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "format=json") {
			t.Errorf("não pediu JSON: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"results":[
			{"title":"Resultado A","url":"https://a.com","content":"resumo A"},
			{"title":"Resultado B","url":"https://b.com","content":"resumo B"}]}`)
	}))
	defer srv.Close()

	cfg := ConfigWeb{SearxURL: srv.URL, HTTP: &http.Client{}}.comDefaults()
	out, err := cfg.buscar("teste", 5)
	if err != nil {
		t.Fatalf("busca falhou: %v", err)
	}
	if !strings.Contains(out, "SearxNG") || !strings.Contains(out, "Resultado A") || !strings.Contains(out, "https://b.com") {
		t.Fatalf("saída inesperada: %q", out)
	}
}

func TestBuscaCaiParaWikipedia(t *testing.T) {
	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer searx.Close()
	wiki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"query":{"search":[
			{"title":"Natal (Rio Grande do Norte)","snippet":"Natal <span>é</span> a capital"}]}}`)
	}))
	defer wiki.Close()

	cfg := ConfigWeb{SearxURL: searx.URL, Wikipedia: wiki.URL, HTTP: &http.Client{}}.comDefaults()
	out, err := cfg.buscar("natal", 3)
	if err != nil {
		t.Fatalf("busca falhou: %v", err)
	}
	if !strings.Contains(out, "Wikipédia") || !strings.Contains(out, "Natal (Rio Grande do Norte)") {
		t.Fatalf("não caiu para Wikipédia: %q", out)
	}
	if strings.Contains(out, "<span>") {
		t.Fatalf("snippet não foi limpo: %q", out)
	}
}

func TestBuscaSemNadaConfiguradoAindaUsaWikipedia(t *testing.T) {
	wiki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"query":{"search":[{"title":"X","snippet":"y"}]}}`)
	}))
	defer wiki.Close()
	cfg := ConfigWeb{Wikipedia: wiki.URL, HTTP: &http.Client{}}.comDefaults()
	out, err := cfg.buscar("x", 2)
	if err != nil || !strings.Contains(out, "Wikipédia") {
		t.Fatalf("deveria usar Wikipédia mesmo sem SearxNG: %v %q", err, out)
	}
}

func TestFerramentasWebNoRegistro(t *testing.T) {
	r := NovoRegistro()
	RegistrarFerramentasWeb(r, ConfigWeb{})
	nomes := strings.Join(r.Nomes(), ",")
	if !strings.Contains(nomes, "buscar_web") || !strings.Contains(nomes, "ler_pagina") {
		t.Fatalf("ferramentas não registradas: %s", nomes)
	}
	// consulta vazia devolve erro legível
	if _, err := r.Executar("buscar_web", map[string]any{"consulta": "  "}); err == nil {
		t.Fatal("consulta vazia deveria falhar")
	}
	// URL interna bloqueada pela ferramenta
	if _, err := r.Executar("ler_pagina", map[string]any{"url": "http://127.0.0.1:1/"}); err == nil {
		t.Fatal("ferramenta deveria bloquear endereço interno")
	}
}

func TestResumoErroDeRede(t *testing.T) {
	cfg := ConfigWeb{HTTP: &http.Client{}, MaxChars: 100}.comDefaults()
	if _, err := cfg.lerPagina("https://dominio-que-nao-existe-xyz.invalid/"); err == nil {
		t.Fatal("deveria falhar em domínio inexistente")
	}
}

// TestBuscaUsaBraveQuandoTemChave: Brave é a primeira da cadeia (resultado de web real).
func TestBuscaUsaBraveQuandoTemChave(t *testing.T) {
	brave := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "chave-teste" {
			t.Errorf("header da Brave errado: %q", r.Header.Get("X-Subscription-Token"))
		}
		w.Header().Set("x-ratelimit-remaining", "42")
		fmt.Fprint(w, `{"web":{"results":[
			{"title":"Noticia real","url":"https://g1.globo.com/x","description":"resumo da noticia"},
			{"title":"Outra","url":"https://tse.jus.br/y","description":"outra"}]}}`)
	}))
	defer brave.Close()
	wiki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("não deveria cair na Wikipédia quando a Brave responde")
		fmt.Fprint(w, `{"query":{"search":[]}}`)
	}))
	defer wiki.Close()

	cfg := ConfigWeb{BraveKey: "chave-teste", Wikipedia: wiki.URL, HTTP: &http.Client{}}.comDefaults()
	// aponta a Brave para o servidor falso
	cfg.BraveURLOverride = brave.URL
	out, err := cfg.buscar("prefeito de natal", 3)
	if err != nil {
		t.Fatalf("busca falhou: %v", err)
	}
	if !strings.Contains(out, "Brave") || !strings.Contains(out, "Noticia real") || !strings.Contains(out, "g1.globo.com") {
		t.Fatalf("não veio da Brave: %q", out)
	}
	if !strings.Contains(out, "42") {
		t.Fatalf("não mostrou cota restante: %q", out)
	}
}

// TestBuscaBraveFalhaCaiParaSearx: se a Brave der erro, a cadeia tem que continuar.
func TestBuscaBraveFalhaCaiParaSearx(t *testing.T) {
	brave := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer brave.Close()
	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[{"title":"Do Searx","url":"https://s.com","content":"c"}]}`)
	}))
	defer searx.Close()

	cfg := ConfigWeb{BraveKey: "k", SearxURL: searx.URL, HTTP: &http.Client{}}.comDefaults()
	cfg.BraveURLOverride = brave.URL
	out, err := cfg.buscar("x", 2)
	if err != nil {
		t.Fatalf("falhou: %v", err)
	}
	if !strings.Contains(out, "SearxNG") || !strings.Contains(out, "Do Searx") {
		t.Fatalf("não caiu para Searx: %q", out)
	}
}
