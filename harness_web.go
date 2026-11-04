package main

// harness_web.go — ferramentas de web do harness nativo (30/10/2026).
//
// REQUISITO: consulta web é essencial para o agente com LLM free. Sem ela, o modelo só
// sabe o que estava no treino.
//
// O QUE FOI MEDIDO nesta máquina (30/10/2026), antes de escolher o caminho:
//   - DuckDuckGo (lite e html)  -> bloqueia: devolve página vazia ou HTTP 202 sem resultados
//   - SearxNG públicas          -> instâncias testadas negam JSON ou devolvem vazio
//   - Brave (HTML)              -> carrega, mas os resultados são renderizados por JS
//   - Wikipédia (API oficial)   -> FUNCIONA, sem chave e sem cota: 3 resultados em 1,4s
//   - Ler página (GET + limpeza) -> FUNCIONA: g1.globo.com, 891 KB -> 15.784 chars em 4,3s
//   - Gemini "google_search"    -> depende de cota; o free tier estourou no dia dos testes
//
// Por isso o desenho é: BUSCA com o que é livre de verdade (SearxNG próprio, se houver +
// Wikipédia) e LEITURA de página sempre disponível. O agente que precisa de busca ampla
// pode apontar um SearxNG (Docker/VPS) por PETAGENT_BUSCA_URL, sem chave de API.
//
// SEGURANÇA: o modelo escolhe a URL. Sem trava, ele poderia pedir file:///C:/... ou
// http://127.0.0.1:<porta> e ler o que não deve (SSRF). Só http/https, só host público,
// e a resposta é limitada em tamanho e tempo.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// ConfigWeb ajusta as ferramentas de web.
type ConfigWeb struct {
	// SearxURL é a base de um SearxNG próprio (ex.: http://127.0.0.1:8888).
	// Vazio = usa só Wikipédia. Lido de PETAGENT_BUSCA_URL quando não informado.
	SearxURL string
	// BraveKey habilita a Brave Search API (a mesma que o Hermes usa). É a fonte de
	// melhor qualidade: resultado real de web, não só enciclopédia. Vazio = tenta
	// PETAGENT_BUSCA_CHAVE e depois BRAVE_SEARCH_API_KEY do ambiente.
	BraveKey string
	// Wikipedia base (padrão pt.wikipedia.org).
	Wikipedia string
	// MaxChars limita o texto devolvido ao modelo (padrão 8000).
	MaxChars int
	// HTTP permite injetar cliente em teste.
	HTTP *http.Client
	// BraveURLOverride permite apontar a Brave para um servidor falso em teste.
	BraveURLOverride string
}

func (c ConfigWeb) comDefaults() ConfigWeb {
	if c.Wikipedia == "" {
		c.Wikipedia = "https://pt.wikipedia.org"
	}
	if c.MaxChars <= 0 {
		c.MaxChars = 8000
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 25 * time.Second}
	}
	if c.SearxURL == "" {
		c.SearxURL = strings.TrimSpace(os.Getenv("PETAGENT_BUSCA_URL"))
	}
	if c.BraveKey == "" {
		c.BraveKey = strings.TrimSpace(os.Getenv("PETAGENT_BUSCA_CHAVE"))
	}
	if c.BraveKey == "" {
		c.BraveKey = strings.TrimSpace(os.Getenv("BRAVE_SEARCH_API_KEY"))
	}
	return c
}

const userAgentWeb = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

// urlPermitida bloqueia esquema diferente de http/https e host privado/loopback.
func urlPermitida(bruto string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(bruto))
	if err != nil {
		return nil, fmt.Errorf("URL inválida: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("só http/https é permitido (recebi %q)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("URL sem host")
	}
	if strings.EqualFold(host, "localhost") {
		return nil, errors.New("host local não é permitido")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return nil, fmt.Errorf("endereço interno não é permitido: %s", host)
		}
	}
	return u, nil
}

var (
	reBlocoRuimWeb = regexp.MustCompile(`(?is)<(script|style|noscript|svg|head|iframe)\b[^>]*>.*?</(script|style|noscript|svg|head|iframe)>`)
	reTagsWeb      = regexp.MustCompile(`(?s)<[^>]+>`)
	reEspacosWeb   = regexp.MustCompile(`[ \t\r\f\v]+`)
	reLinhasWeb    = regexp.MustCompile(`\n{3,}`)
)

// limparHTML devolve o texto visível de uma página (tira script/style/tags).
func limparHTML(h string) string {
	h = reBlocoRuimWeb.ReplaceAllString(h, " ")
	h = strings.ReplaceAll(h, "</p>", "\n")
	h = strings.ReplaceAll(h, "</div>", "\n")
	h = strings.ReplaceAll(h, "<br>", "\n")
	h = strings.ReplaceAll(h, "<br/>", "\n")
	h = reTagsWeb.ReplaceAllString(h, " ")
	h = strings.ReplaceAll(h, "&nbsp;", " ")
	h = strings.ReplaceAll(h, "&amp;", "&")
	h = strings.ReplaceAll(h, "&lt;", "<")
	h = strings.ReplaceAll(h, "&gt;", ">")
	h = strings.ReplaceAll(h, "&quot;", `"`)
	h = strings.ReplaceAll(h, "&#39;", "'")
	h = reEspacosWeb.ReplaceAllString(h, " ")
	h = reLinhasWeb.ReplaceAllString(h, "\n\n")
	return strings.TrimSpace(h)
}

func cortarTexto(s string, max int) string {
	r := []rune(s)
	if max > 0 && len(r) > max {
		return string(r[:max]) + "\n[... texto cortado]"
	}
	return s
}

// buscarSearx consulta um SearxNG próprio (formato JSON).
func (c ConfigWeb) buscarSearx(consulta string, limite int) ([]string, error) {
	if c.SearxURL == "" {
		return nil, errors.New("sem SearxNG configurado")
	}
	alvo := fmt.Sprintf("%s/search?q=%s&format=json", strings.TrimRight(c.SearxURL, "/"), url.QueryEscape(consulta))
	req, err := http.NewRequest("GET", alvo, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgentWeb)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("SearxNG respondeu HTTP %d", resp.StatusCode)
	}
	var r struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("SearxNG devolveu JSON inválido: %w", err)
	}
	var out []string
	for i, it := range r.Results {
		if i >= limite {
			break
		}
		out = append(out, fmt.Sprintf("%d. %s\n   %s\n   %s", i+1, it.Title, it.URL, cortarTexto(it.Content, 300)))
	}
	return out, nil
}

// buscarBrave consulta a Brave Search API (mesma fonte que o Hermes usa).
//
// MEDIDO em 30/10/2026 com a chave do dono: HTTP 200, 3 resultados reais em 3,6s.
// Os headers trazem o limite da conta (x-ratelimit-limit / x-ratelimit-policy), então o
// harness consegue saber quando está perto do teto em vez de descobrir no erro.
func (c ConfigWeb) buscarBrave(consulta string, limite int) ([]string, string, error) {
	if c.BraveKey == "" {
		return nil, "", errors.New("sem chave da Brave")
	}
	base := "https://api.search.brave.com"
	if c.BraveURLOverride != "" {
		base = c.BraveURLOverride
	}
	alvo := fmt.Sprintf("%s/res/v1/web/search?q=%s&count=%d",
		base, url.QueryEscape(consulta), limite)
	req, err := http.NewRequest("GET", alvo, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("X-Subscription-Token", c.BraveKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgentWeb)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	restante := resp.Header.Get("x-ratelimit-remaining")
	if resp.StatusCode != 200 {
		return nil, restante, fmt.Errorf("Brave respondeu HTTP %d", resp.StatusCode)
	}
	var r struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, restante, fmt.Errorf("Brave devolveu JSON inválido: %w", err)
	}
	var out []string
	for i, it := range r.Web.Results {
		if i >= limite {
			break
		}
		out = append(out, fmt.Sprintf("%d. %s\n   %s\n   %s", i+1, it.Title, it.URL, cortarTexto(limparHTML(it.Description), 300)))
	}
	return out, restante, nil
}

// buscarWikipedia consulta a API oficial (sem chave, sem cota).
func (c ConfigWeb) buscarWikipedia(consulta string, limite int) ([]string, error) {
	alvo := fmt.Sprintf("%s/w/api.php?action=query&list=search&srsearch=%s&format=json&srlimit=%d",
		strings.TrimRight(c.Wikipedia, "/"), url.QueryEscape(consulta), limite)
	req, err := http.NewRequest("GET", alvo, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgentWeb)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Wikipédia respondeu HTTP %d", resp.StatusCode)
	}
	var r struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&r); err != nil {
		return nil, err
	}
	var out []string
	for i, it := range r.Query.Search {
		if i >= limite {
			break
		}
		resumo := limparHTML(it.Snippet)
		link := fmt.Sprintf("%s/wiki/%s", strings.TrimRight(c.Wikipedia, "/"), url.PathEscape(strings.ReplaceAll(it.Title, " ", "_")))
		out = append(out, fmt.Sprintf("%d. %s\n   %s\n   %s", i+1, it.Title, link, cortarTexto(resumo, 300)))
	}
	return out, nil
}

// buscar tenta as fontes em ordem de qualidade: Brave (web real) -> SearxNG próprio
// -> Wikipédia (sempre funciona). Cada falha vira aviso, nunca erro fatal se outra servir.
func (c ConfigWeb) buscar(consulta string, limite int) (string, error) {
	var avisos []string

	if c.BraveKey != "" {
		res, restante, err := c.buscarBrave(consulta, limite)
		if err == nil && len(res) > 0 {
			rodape := ""
			if restante != "" {
				rodape = fmt.Sprintf("\n\n[cota Brave restante neste minuto: %s]", restante)
			}
			return "resultados (Brave):\n\n" + strings.Join(res, "\n\n") + rodape, nil
		}
		if err != nil {
			avisos = append(avisos, "Brave: "+err.Error())
		}
	} else {
		avisos = append(avisos, "sem chave da Brave")
	}

	if c.SearxURL != "" {
		if res, err := c.buscarSearx(consulta, limite); err == nil && len(res) > 0 {
			return "resultados (SearxNG):\n\n" + strings.Join(res, "\n\n"), nil
		} else if err != nil {
			avisos = append(avisos, "SearxNG: "+err.Error())
		}
	}

	res, err := c.buscarWikipedia(consulta, limite)
	if err != nil {
		return "", fmt.Errorf("busca falhou (%s; Wikipédia: %v)", strings.Join(avisos, "; "), err)
	}
	if len(res) == 0 {
		return "nada encontrado para: " + consulta, nil
	}
	return "resultados (Wikipédia):\n\n" + strings.Join(res, "\n\n"), nil
}

// lerPagina baixa a URL e devolve texto limpo (com o guard de SSRF).
func (c ConfigWeb) lerPagina(bruto string) (string, error) {
	u, err := urlPermitida(bruto)
	if err != nil {
		return "", err
	}
	return c.baixarTexto(u)
}

// lerPaginaForcadoParaTeste existe só para teste: aponta para o servidor httptest, que
// roda em 127.0.0.1 e é (corretamente) bloqueado pelo guard. Não usar no app.
func (c ConfigWeb) lerPaginaForcadoParaTeste(bruto string) (string, error) {
	u, err := url.Parse(bruto)
	if err != nil {
		return "", err
	}
	return c.baixarTexto(u)
}

// resumoWikipedia usa a API REST de resumo, que devolve o parágrafo de introdução já
// limpo. Medido em 30/10/2026: ler o HTML de /wiki/Paulinho_Freire devolvia 243 KB de
// menu/infobox — o modelo não achava a resposta e ficava pedindo mais texto (4 turnos
// perdidos). O resumo entrega o conteúdo útil em uma chamada.
func (c ConfigWeb) resumoWikipedia(u *url.URL) (string, bool, error) {
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, "wikipedia.org") || !strings.HasPrefix(u.Path, "/wiki/") {
		return "", false, nil
	}
	titulo := strings.TrimPrefix(u.Path, "/wiki/")
	alvo := fmt.Sprintf("https://%s/api/rest_v1/page/summary/%s", host, titulo)
	req, err := http.NewRequest("GET", alvo, nil)
	if err != nil {
		return "", true, err
	}
	req.Header.Set("User-Agent", userAgentWeb)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", true, fmt.Errorf("resumo da Wikipédia respondeu HTTP %d", resp.StatusCode)
	}
	var r struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Extract     string `json:"extract"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return "", true, err
	}
	if r.Extract == "" {
		return "", true, errors.New("resumo vazio")
	}
	txt := fmt.Sprintf("%s — %s\n\n%s", r.Title, r.Description, r.Extract)
	return fmt.Sprintf("%s (resumo Wikipédia)\n---\n%s", u.String(), cortarTexto(txt, c.MaxChars)), true, nil
}

// baixarTexto faz o GET e limpa o conteúdo.
func (c ConfigWeb) baixarTexto(u *url.URL) (string, error) {
	if out, tratado, err := c.resumoWikipedia(u); tratado {
		if err == nil {
			return out, nil
		}
		// se o resumo falhar, segue para o HTML normal
	}
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgentWeb)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("não consegui abrir %s: %w", u.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%s respondeu HTTP %d", u.Host, resp.StatusCode)
	}
	corpo, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") || strings.Contains(ct, "text/plain") {
		return fmt.Sprintf("%s (%s)\n---\n%s", u.String(), ct, cortarTexto(string(corpo), c.MaxChars)), nil
	}
	texto := limparHTML(string(corpo))
	if texto == "" {
		return "", fmt.Errorf("não consegui extrair texto de %s", u.Host)
	}
	return fmt.Sprintf("%s (%d KB baixados)\n---\n%s", u.String(), len(corpo)/1024, cortarTexto(texto, c.MaxChars)), nil
}

// RegistrarFerramentasWeb registra buscar_web e ler_pagina.
func RegistrarFerramentasWeb(r *Registro, cfg ConfigWeb) {
	cfg = cfg.comDefaults()

	r.Registrar(&Ferramenta{
		Nome: "buscar_web",
		Descricao: "Pesquisa na web e devolve títulos, links e resumos. Use para fatos atuais, " +
			"notícias e definições. Não devolve a página inteira: se precisar do conteúdo, " +
			"chame ler_pagina com o link.",
		Parametros: schemaObjeto(map[string]any{
			"consulta": schemaString("O que procurar"),
			"limite":   map[string]any{"type": "integer", "description": "Quantos resultados (padrão 5, máx 10)"},
		}, "consulta"),
		Executar: func(args map[string]any) (string, error) {
			consulta := strings.TrimSpace(argTexto(args, "consulta"))
			if consulta == "" {
				return "", errors.New("consulta vazia")
			}
			lim := argInt(args, "limite", 5)
			if lim < 1 {
				lim = 5
			}
			if lim > 10 {
				lim = 10
			}
			return cfg.buscar(consulta, lim)
		},
	})

	r.Registrar(&Ferramenta{
		Nome: "ler_pagina",
		Descricao: "Abre uma página da web (http/https) e devolve o texto principal já limpo " +
			"(até 8000 caracteres por padrão). Use depois de buscar_web. Não precisa pedir " +
			"poucos caracteres: o padrão já traz o conteúdo útil.",
		Parametros: schemaObjeto(map[string]any{
			"url":       schemaString("Endereço completo, começando com http:// ou https://"),
			"max_chars": map[string]any{"type": "integer", "description": "Limite de caracteres (padrão 8000)"},
		}, "url"),
		Executar: func(args map[string]any) (string, error) {
			alvo := strings.TrimSpace(argTexto(args, "url"))
			if alvo == "" {
				return "", errors.New("url vazia")
			}
			c := cfg
			if n := argInt(args, "max_chars", 0); n > 0 {
				// piso de 1000: pedir 200 chars devolve só cabeçalho/menu e o modelo
				// fica repetindo a chamada pedindo mais (visto no teste de 30/10/2026)
				if n < 1000 {
					n = 1000
				}
				if n > 40000 {
					n = 40000
				}
				c.MaxChars = n
			}
			return c.lerPagina(alvo)
		},
	})
}
