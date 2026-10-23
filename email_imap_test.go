package main

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/server"
)

// pastaIsolada — %LOCALAPPDATA% de mentira: o teste nunca toca nas contas e
// regras de verdade de quem roda.
func pastaIsolada(t *testing.T) {
	t.Helper()
	t.Setenv("LOCALAPPDATA", t.TempDir())
	cacheMu.Lock()
	caixas, estados = map[string]CaixaEmail{}, map[string]estadoConta{}
	cacheMu.Unlock()
}

// servidorIMAP — servidor IMAP de memória em 127.0.0.1 (usuário "username",
// senha "password"; a INBOX já vem com 1 mensagem LIDA). Acrescenta uma mensagem
// NÃO lida e troca a conexão TLS por uma conexão simples até ele.
func servidorIMAP(t *testing.T) (host string, porta int, inbox *memory.Mailbox) {
	t.Helper()
	be := memory.New()
	u, err := be.Login(nil, "username", "password")
	if err != nil {
		t.Fatal(err)
	}
	mb, _ := u.GetMailbox("INBOX")
	inbox = mb.(*memory.Mailbox)
	corpo := "From: Fulana de Tal <fulana@cliente.com.br>\r\n" +
		"To: username@localhost\r\n" +
		"Subject: Pedido 123 aprovado\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: <teste-1@localhost>\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"Pode confirmar a entrega para o dia 30? Valor: R$ 1.200."
	if err := inbox.CreateMessage([]string{}, time.Now(), bytes.NewBufferString(corpo)); err != nil {
		t.Fatal(err)
	}

	s := server.New(be)
	s.AllowInsecureAuth = true
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close() })

	comandosMu.Lock()
	comandos.Reset()
	comandosMu.Unlock()
	antes := conectaIMAP
	t.Cleanup(func() { conectaIMAP = antes })
	conectaIMAP = func(h string, p int) (*client.Client, error) {
		conn, err := net.Dial("tcp", net.JoinHostPort(h, strconv.Itoa(p)))
		if err != nil {
			return nil, err
		}
		return client.New(gravaConn{conn})
	}
	return "127.0.0.1", ln.Addr().(*net.TCPAddr).Port, inbox
}

// O servidor de memória do go-imap NUNCA marca \Seen (nem num BODY[] sem PEEK):
// conferir as flags dele não prova nada. Por isso o teste confere o que o app
// MANDA ao servidor — é isso que um Gmail ou Outlook de verdade obedece.
var (
	comandosMu sync.Mutex
	comandos   strings.Builder
)

type gravaConn struct{ net.Conn }

func (g gravaConn) Write(b []byte) (int, error) {
	comandosMu.Lock()
	comandos.Write(b)
	comandosMu.Unlock()
	return g.Conn.Write(b)
}

// confereSoLeitura — caixa aberta com EXAMINE, nada que altere a caixa, e o texto
// (quando pedido) só com BODY.PEEK[]. BODY[] sem PEEK marca como lido.
func confereSoLeitura(t *testing.T, querTexto bool) {
	t.Helper()
	comandosMu.Lock()
	cmds := strings.ToUpper(comandos.String())
	comandosMu.Unlock()
	if !strings.Contains(cmds, "EXAMINE INBOX") {
		t.Errorf("a caixa não foi aberta com EXAMINE (só leitura):\n%s", cmds)
	}
	for _, proibido := range []string{"SELECT ", "STORE", "EXPUNGE", "COPY ", "MOVE ", "BODY["} {
		if strings.Contains(cmds, proibido) {
			t.Errorf("o app mandou %q ao servidor — isso altera a caixa ou marca como lido:\n%s", proibido, cmds)
		}
	}
	if querTexto != strings.Contains(cmds, "BODY.PEEK[") {
		t.Errorf("BODY.PEEK[ esperado=%v nos comandos:\n%s", querTexto, cmds)
	}
}

// TestLeituraIMAPNaoMarcaComoLido — a promessa do recurso: SÓ LEITURA. A varredura
// abre a caixa com EXAMINE e só pede cabeçalhos.
func TestLeituraIMAPNaoMarcaComoLido(t *testing.T) {
	host, porta, _ := servidorIMAP(t)
	c := ContaEmail{Provedor: "outro", Usuario: "username", Host: host, Porta: porta}

	cx, err := leCaixaIMAP(c, credencial{usuario: "username", senha: "password"})
	if err != nil {
		t.Fatalf("leCaixaIMAP: %v", err)
	}
	if len(cx.Mensagens) != 2 || cx.TotalPasta != 2 {
		t.Fatalf("leu %d mensagens (total %d), queria 2", len(cx.Mensagens), cx.TotalPasta)
	}
	if cx.NaoLidas != 1 {
		t.Errorf("nao_lidas = %d, queria 1", cx.NaoLidas)
	}
	m := cx.Mensagens[1]
	if m.Assunto != "Pedido 123 aprovado" || m.DeEmail != "fulana@cliente.com.br" || m.DeNome != "Fulana de Tal" {
		t.Errorf("cabeçalho errado: %+v", m)
	}
	if !m.NaoLida || m.Conta != "username" {
		t.Errorf("NaoLida=%v Conta=%q; queria true e \"username\"", m.NaoLida, m.Conta)
	}
	// hora LOCAL sem fuso, no formato que ehNova/idadeLeitura entendem
	if !ehNova(m) || idadeLeitura(m.Quando) > time.Minute {
		t.Errorf("Quando=%q não foi entendido como agora (fuso perdido?)", m.Quando)
	}

	confereSoLeitura(t, false)
}

// TestSenhaErradaExplicaEmPortugues — o erro vai direto para a tela da pessoa.
func TestSenhaErradaExplicaEmPortugues(t *testing.T) {
	host, porta, _ := servidorIMAP(t)
	c := ContaEmail{Provedor: "outro", Usuario: "username", Host: host, Porta: porta}

	_, err := leCaixaIMAP(c, credencial{usuario: "username", senha: "errada"})
	if err == nil || !strings.Contains(err.Error(), "senha recusados") {
		t.Fatalf("erro = %v, queria \"usuário ou senha recusados\"", err)
	}
}

// TestServidorInexistenteExplicaEmPortugues — servidor digitado errado em "Outro".
func TestServidorInexistenteExplicaEmPortugues(t *testing.T) {
	c := ContaEmail{Provedor: "outro", Usuario: "x", Host: "127.0.0.1", Porta: 1}
	_, err := leCaixaIMAP(c, credencial{usuario: "x", senha: "y"})
	if err == nil || !strings.Contains(err.Error(), "não consegui conectar") {
		t.Fatalf("erro = %v, queria \"não consegui conectar\"", err)
	}
}
