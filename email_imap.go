package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-sasl"
)

// ── Leitura por IMAP (22/10/2026) ─────────────────────────────────────
// SÓ LEITURA, como sempre foi: a caixa é aberta com EXAMINE (o servidor recusa
// qualquer mudança) e só se pedem cabeçalhos — ENVELOPE, FLAGS, INTERNALDATE,
// RFC822.SIZE. Nunca BODY[] (que marcaria como lido), nunca STORE/COPY/EXPUNGE.
//
// Duas regras herdadas do leitor antigo do Outlook (incidente de 04/10/2026) que
// continuam valendo aqui:
//  1. UMA LEITURA POR VEZ: o ciclo automático e o "Verificar agora" nunca rodam
//     juntos (leituraMu em email.go).
//  2. STATUS EXPLÍCITO: cada conta diz se leu (ok), falhou (erro) ou precisa de
//     novo login (precisa_entrar). Leitura que falhou não vira "0 mensagens novas".

const ultimasMensagens = 50

// credencial — senha (LOGIN) ou token de acesso da Microsoft (XOAUTH2).
type credencial struct {
	usuario string
	senha   string
	token   string
}

// conectaIMAP — variável para o teste trocar por uma conexão sem TLS.
var conectaIMAP = func(host string, porta int) (*client.Client, error) {
	d := &net.Dialer{Timeout: 20 * time.Second}
	return client.DialWithDialerTLS(d, net.JoinHostPort(host, strconv.Itoa(porta)), &tls.Config{ServerName: host})
}

// leCaixaIMAP — conecta, entra, lê os cabeçalhos das últimas mensagens e sai.
func leCaixaIMAP(c ContaEmail, cred credencial) (CaixaEmail, error) {
	var cx CaixaEmail
	cli, err := conectaIMAP(c.Host, c.Porta)
	if err != nil {
		return cx, erroIMAP(err, c)
	}
	defer cli.Logout()
	cli.Timeout = 30 * time.Second

	if cred.token != "" {
		err = cli.Authenticate(&xoauth2{usuario: cred.usuario, token: cred.token})
	} else {
		err = cli.Login(cred.usuario, cred.senha)
	}
	if err != nil {
		return cx, erroLogin(err, c)
	}

	mbox, err := cli.Select("INBOX", true) // true = EXAMINE (só leitura)
	if err != nil {
		return cx, fmt.Errorf("entrei, mas não consegui abrir a Caixa de Entrada: %v", err)
	}

	cx.Conta = c.Usuario
	cx.Contexto = "imap"
	cx.TotalPasta = int(mbox.Messages)
	cx.Mensagens = []CabecalhoEmail{}

	crit := imap.NewSearchCriteria()
	crit.WithoutFlags = []string{imap.SeenFlag}
	if ids, err := cli.Search(crit); err == nil {
		cx.NaoLidas = len(ids)
	}

	if mbox.Messages > 0 {
		de := uint32(1)
		if mbox.Messages > ultimasMensagens {
			de = mbox.Messages - ultimasMensagens + 1
		}
		seq := new(imap.SeqSet)
		seq.AddRange(de, mbox.Messages)
		itens := []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate, imap.FetchRFC822Size}
		ch := make(chan *imap.Message, 16)
		fim := make(chan error, 1)
		go func() { fim <- cli.Fetch(seq, itens, ch) }()
		for m := range ch {
			cx.Mensagens = append(cx.Mensagens, cabecalhoDe(m, c.Usuario))
		}
		if err := <-fim; err != nil {
			return cx, fmt.Errorf("a leitura dos cabeçalhos falhou no meio: %v", err)
		}
	}
	cx.LidasAgora = len(cx.Mensagens)
	cx.VerificadoEm = time.Now().Format("2006-01-02T15:04:05")
	return cx, nil
}

// cabecalhoDe — a hora sai em hora LOCAL sem fuso, no mesmo formato que ehNova e
// idadeLeitura esperam (ver email_fuso_test.go).
func cabecalhoDe(m *imap.Message, conta string) CabecalhoEmail {
	h := CabecalhoEmail{Conta: conta, Tamanho: int(m.Size), NaoLida: true}
	for _, f := range m.Flags {
		if f == imap.SeenFlag {
			h.NaoLida = false
		}
	}
	quando := m.InternalDate
	if e := m.Envelope; e != nil {
		h.Assunto = e.Subject
		if len(e.From) > 0 && e.From[0] != nil {
			h.DeNome = e.From[0].PersonalName
			h.DeEmail = e.From[0].Address()
		}
		if quando.IsZero() {
			quando = e.Date
		}
	}
	if !quando.IsZero() {
		h.Quando = quando.In(time.Local).Format("2006-01-02T15:04:05")
	}
	return h
}

// erroIMAP — falha de conexão em português (a tela mostra isto para a pessoa).
func erroIMAP(err error, c ContaEmail) error {
	var dns *net.DNSError
	var op *net.OpError
	switch {
	case errors.As(err, &dns):
		return fmt.Errorf("não encontrei o servidor %s. Confira o endereço ou a internet", c.Host)
	case errors.As(err, &op) && op.Timeout():
		return fmt.Errorf("o servidor %s não respondeu a tempo. Confira a internet e a porta %d", c.Host, c.Porta)
	case errors.As(err, &op):
		return fmt.Errorf("não consegui conectar em %s:%d (%v)", c.Host, c.Porta, op.Err)
	}
	if strings.Contains(err.Error(), "tls") || strings.Contains(err.Error(), "certificate") {
		return fmt.Errorf("a conexão segura com %s falhou: %v", c.Host, err)
	}
	return fmt.Errorf("não consegui conectar em %s:%d: %v", c.Host, c.Porta, err)
}

// erroLogin — o servidor recusou a entrada. O texto do servidor vai junto: é
// dele que sai a pista certa ("IMAP desligado", "senha de app necessária"...).
func erroLogin(err error, c ContaEmail) error {
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case c.Provedor == "gmail" && strings.Contains(low, "application-specific password"):
		return errors.New("o Google pediu uma senha de app: a senha normal da conta não funciona aqui")
	case strings.Contains(low, "imap") && (strings.Contains(low, "disabled") || strings.Contains(low, "not enabled")):
		return fmt.Errorf("o IMAP está desligado nesta conta. Ligue nas configurações do e-mail (%s)", msg)
	case c.auth() == "microsoft":
		return fmt.Errorf("a Microsoft recusou a entrada (%s). Se for conta de empresa, o TI pode ter bloqueado o IMAP", msg)
	}
	return fmt.Errorf("usuário ou senha recusados pelo servidor (%s)", msg)
}

// xoauth2 — o SASL XOAUTH2 que Microsoft e Google usam com token de acesso.
// Em erro, o servidor manda um desafio com o JSON do erro e espera resposta vazia.
type xoauth2 struct{ usuario, token string }

func (x *xoauth2) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.usuario + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}

func (x *xoauth2) Next(desafio []byte) ([]byte, error) { return []byte{}, nil }

var _ sasl.Client = (*xoauth2)(nil)
