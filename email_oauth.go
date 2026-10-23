package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"
)

// ── Login da Microsoft (Hotmail, Outlook.com, Microsoft 365) ──────────
// A Microsoft não aceita mais senha em programa de terceiros (IMAP): só OAuth.
// Fluxo de app desktop, sem segredo de cliente: PKCE + retorno num endereço local
// (http://localhost:<porta aleatória>/). A senha da pessoa fica no site da
// Microsoft; o boneco só guarda o refresh token, cifrado com DPAPI.

// msClientID — o "Application (client) ID" do app registrado no Entra ID (ver
// README, seção E-mail). Vazio = as opções da Microsoft ficam desabilitadas.
var msClientID = ""

// msEndpoint — "common" aceita conta pessoal e de empresa. Variável para o teste.
var msEndpoint = microsoft.AzureADEndpoint("common")

var msEscopos = []string{
	"https://outlook.office.com/IMAP.AccessAsUser.All",
	"offline_access",             // é o que dá o refresh token
	"openid", "email", "profile", // é o que diz QUAL conta entrou
}

var errPrecisaEntrar = errors.New("a Microsoft pediu para você entrar de novo nesta conta")

func msConfig(redirect string) *oauth2.Config {
	return &oauth2.Config{ClientID: msClientID, Endpoint: msEndpoint, RedirectURL: redirect, Scopes: msEscopos}
}

// tokenMicrosoft — troca o refresh token guardado por um token de acesso novo.
// A Microsoft costuma girar o refresh token: o novo é gravado no lugar do velho.
func tokenMicrosoft(c ContaEmail) (string, error) {
	rt, err := decifra(c.Segredo)
	if err != nil || rt == "" {
		return "", errPrecisaEntrar
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, err := msConfig("").TokenSource(ctx, &oauth2.Token{RefreshToken: rt}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && (re.ErrorCode == "invalid_grant" || re.ErrorCode == "interaction_required") {
			return "", errPrecisaEntrar
		}
		return "", fmt.Errorf("não consegui renovar o acesso com a Microsoft: %v", err)
	}
	if tok.RefreshToken != "" && tok.RefreshToken != rt {
		if seg, err := cifra(tok.RefreshToken); err == nil {
			trocaSegredo(c.ID, seg)
		}
	}
	return tok.AccessToken, nil
}

// usuarioDoIDToken — o e-mail de quem entrou. O id_token veio direto do servidor
// de token da Microsoft por HTTPS, então só se lê o conteúdo (sem validar assinatura).
func usuarioDoIDToken(tok *oauth2.Token) string {
	idt, _ := tok.Extra("id_token").(string)
	partes := strings.Split(idt, ".")
	if len(partes) < 2 {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(partes[1], "="))
	if err != nil {
		return ""
	}
	var cl struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
	}
	if json.Unmarshal(b, &cl) != nil {
		return ""
	}
	if strings.Contains(cl.PreferredUsername, "@") {
		return cl.PreferredUsername
	}
	return cl.Email
}

// abreNavegador — variável para o teste não abrir navegador de verdade.
var abreNavegador = func(a *App, url string) {
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, url)
	}
}

var (
	oauthMu      sync.Mutex
	oauthCancela context.CancelFunc // só um login aberto por vez: o novo cancela o velho
)

// EmailEntrarMicrosoft — abre o login da Microsoft no navegador e devolve na hora.
// O resultado chega depois pelo evento "email-oauth" {ok, erro, conta}.
// contaID vazio = conta nova; preenchido = entrar de novo numa conta que já existe.
func (a *App) EmailEntrarMicrosoft(provedorID, contaID string) string {
	if contaID != "" {
		if c, ok := achaConta(contaID); ok {
			provedorID = c.Provedor
		}
	}
	prov, ok := provedorEmail(provedorID)
	if !ok || prov.Auth != "microsoft" {
		return respostaJSON(map[string]any{"ok": false, "erro": "provedor inválido"})
	}
	if !prov.Disponivel {
		return respostaJSON(map[string]any{"ok": false, "erro": prov.Motivo})
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return respostaJSON(map[string]any{"ok": false, "erro": "não consegui abrir o retorno local: " + err.Error()})
	}
	porta := ln.Addr().(*net.TCPAddr).Port
	lns := []net.Listener{ln}
	// o navegador pode resolver "localhost" para ::1 primeiro
	if ln6, err := net.Listen("tcp", "[::1]:"+strconv.Itoa(porta)); err == nil {
		lns = append(lns, ln6)
	}
	conf := msConfig(fmt.Sprintf("http://localhost:%d/", porta))
	verificador := oauth2.GenerateVerifier()
	estado := novoIDConta() + novoIDConta()
	url := conf.AuthCodeURL(estado, oauth2.S256ChallengeOption(verificador),
		oauth2.SetAuthURLParam("prompt", "select_account"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	oauthMu.Lock()
	if oauthCancela != nil {
		oauthCancela()
	}
	oauthCancela = cancel
	oauthMu.Unlock()

	go a.esperaRetornoMicrosoft(ctx, cancel, lns, conf, estado, verificador, prov, contaID)
	abreNavegador(a, url)
	logMsg("email: login da Microsoft aberto no navegador (" + prov.ID + ")")
	return respostaJSON(map[string]any{"ok": true})
}

func (a *App) esperaRetornoMicrosoft(ctx context.Context, cancel context.CancelFunc, lns []net.Listener,
	conf *oauth2.Config, estado, verificador string, prov Provedor, contaID string) {
	defer cancel()
	type retorno struct {
		tok *oauth2.Token
		err error
	}
	res := make(chan retorno, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		var rt retorno
		switch {
		case q.Get("state") != estado:
			rt.err = errors.New("resposta do login não confere (state). Tente de novo")
		case q.Get("error") != "":
			rt.err = fmt.Errorf("a Microsoft não liberou o acesso: %s", firstNonEmpty(q.Get("error_description"), q.Get("error")))
		default:
			tctx, tcancel := context.WithTimeout(ctx, 30*time.Second)
			rt.tok, rt.err = conf.Exchange(tctx, q.Get("code"), oauth2.VerifierOption(verificador))
			tcancel()
			if rt.err != nil {
				rt.err = fmt.Errorf("não consegui concluir o login com a Microsoft: %v", rt.err)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		msg := "Pronto! Pode fechar esta aba e voltar para o PetAgent."
		if rt.err != nil {
			msg = "Não deu certo: " + rt.err.Error()
		}
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>PetAgent</title>
<body style="font-family:system-ui;max-width:32em;margin:4em auto;line-height:1.5"><h2>PetAgent · e-mail</h2><p>%s</p></body>`,
			html.EscapeString(msg))
		select {
		case res <- rt:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	for _, ln := range lns {
		go func(ln net.Listener) { _ = srv.Serve(ln) }(ln)
	}
	defer srv.Close()

	var rt retorno
	select {
	case rt = <-res:
	case <-ctx.Done():
		rt.err = errors.New("o login da Microsoft não foi concluído em 5 minutos")
	}
	err := rt.err
	if err == nil {
		var c ContaEmail
		c, err = a.concluiMicrosoft(rt.tok, prov, contaID)
		if err == nil {
			a.emiteOAuth(true, "", c.Usuario)
			return
		}
	}
	logMsg("email: login da Microsoft falhou: " + err.Error())
	a.emiteOAuth(false, err.Error(), "")
}

// concluiMicrosoft — descobre quem entrou, testa a leitura e só então salva.
func (a *App) concluiMicrosoft(tok *oauth2.Token, prov Provedor, contaID string) (ContaEmail, error) {
	usuario := usuarioDoIDToken(tok)
	if usuario == "" {
		return ContaEmail{}, errors.New("a Microsoft não informou o e-mail da conta. Tente de novo")
	}
	if tok.RefreshToken == "" {
		return ContaEmail{}, errors.New("a Microsoft não devolveu acesso permanente (offline_access)")
	}
	c := ContaEmail{Provedor: prov.ID, Usuario: usuario, Host: prov.Host, Porta: prov.Porta}
	if velha, ok := achaConta(contaID); ok && strings.EqualFold(velha.Usuario, usuario) {
		c.ID = velha.ID
	}
	cx, err := leCaixaIMAP(c, credencial{usuario: usuario, token: tok.AccessToken})
	if err != nil {
		return c, err
	}
	seg, err := cifra(tok.RefreshToken)
	if err != nil {
		return c, fmt.Errorf("não consegui guardar o acesso com segurança: %v", err)
	}
	c.Segredo = seg
	if err := salvaConta(c); err != nil {
		return c, fmt.Errorf("não consegui salvar: %v", err)
	}
	for _, x := range leContas() {
		if strings.EqualFold(x.Usuario, c.Usuario) && strings.EqualFold(x.Host, c.Host) {
			c = x
		}
	}
	guardaLeitura(c, cx, nil)
	logMsg("email: conta adicionada (" + prov.ID + ") " + usuario)
	return c, nil
}

func (a *App) emiteOAuth(ok bool, erro, conta string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "email-oauth", map[string]any{"ok": ok, "erro": erro, "conta": conta})
	}
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
