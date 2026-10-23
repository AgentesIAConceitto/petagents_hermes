package main

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"
)

// tokenFalso — servidor de token da Microsoft de mentira.
func tokenFalso(t *testing.T, resposta int, corpo string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resposta)
		_, _ = w.Write([]byte(corpo))
	}))
	t.Cleanup(srv.Close)
	antesE, antesID := msEndpoint, msClientID
	t.Cleanup(func() { msEndpoint, msClientID = antesE, antesID })
	msEndpoint = oauth2.Endpoint{TokenURL: srv.URL, AuthStyle: oauth2.AuthStyleInParams}
	msClientID = "teste"
}

func contaMicrosoft(t *testing.T) ContaEmail {
	t.Helper()
	seg, err := cifra("rt-velho")
	if err != nil {
		t.Fatal(err)
	}
	c := ContaEmail{ID: "ms1", Provedor: "hotmail", Usuario: "fulana@hotmail.com",
		Host: "outlook.office365.com", Porta: 993, Segredo: seg}
	if err := salvaConta(c); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestRenovaTokenGuardaONovo — a Microsoft gira o refresh token: o novo tem de
// substituir o velho, senão a conta "cai" depois de alguns dias.
func TestRenovaTokenGuardaONovo(t *testing.T) {
	pastaIsolada(t)
	tokenFalso(t, 200, `{"access_token":"at-novo","refresh_token":"rt-novo","token_type":"Bearer","expires_in":3600}`)
	c := contaMicrosoft(t)

	at, err := tokenMicrosoft(c)
	if err != nil || at != "at-novo" {
		t.Fatalf("tokenMicrosoft = %q, %v", at, err)
	}
	salva, _ := achaConta("ms1")
	if rt, _ := decifra(salva.Segredo); rt != "rt-novo" {
		t.Fatalf("refresh token guardado = %q, queria o novo", rt)
	}
}

// TestRenovacaoRecusadaPedeLoginDeNovo — refresh token revogado/expirado: a conta
// fica "precisa_entrar" (o painel mostra "Entrar de novo"), não um erro genérico.
func TestRenovacaoRecusadaPedeLoginDeNovo(t *testing.T) {
	pastaIsolada(t)
	tokenFalso(t, 400, `{"error":"invalid_grant","error_description":"AADSTS70008: expired"}`)
	c := contaMicrosoft(t)

	_, err := leConta(c)
	if !errors.Is(err, errPrecisaEntrar) {
		t.Fatalf("leConta = %v, queria errPrecisaEntrar", err)
	}
	guardaLeitura(c, CaixaEmail{}, err)
	if e := estadoDe(c.ID); e.Estado != "precisa_entrar" {
		t.Fatalf("estado = %q, queria precisa_entrar", e.Estado)
	}
}

// TestUsuarioDoIDToken — o e-mail de quem entrou sai do id_token.
func TestUsuarioDoIDToken(t *testing.T) {
	carga := base64.RawURLEncoding.EncodeToString([]byte(`{"preferred_username":"fulana@hotmail.com","email":"x@y.com"}`))
	tok := (&oauth2.Token{}).WithExtra(map[string]any{"id_token": "cab." + carga + ".ass"})
	if u := usuarioDoIDToken(tok); u != "fulana@hotmail.com" {
		t.Fatalf("usuarioDoIDToken = %q", u)
	}
	if u := usuarioDoIDToken(&oauth2.Token{}); u != "" {
		t.Fatalf("sem id_token devia dar vazio, deu %q", u)
	}
}
