package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/emersion/go-imap/client"
)

// TestEmailSemContasNaoConecta — o e-mail é OPCIONAL. Sem conta adicionada, o app
// não tenta conectar em nada nem diz "ok" ao botão "Verificar agora".
func TestEmailSemContasNaoConecta(t *testing.T) {
	pastaIsolada(t)
	antes := conectaIMAP
	t.Cleanup(func() { conectaIMAP = antes })
	conexoes := 0
	conectaIMAP = func(string, int) (*client.Client, error) { conexoes++; return nil, os.ErrInvalid }

	r := NewApp().EmailVerificarAgora()

	if conexoes != 0 {
		t.Errorf("tentou conectar %d vez(es) sem nenhuma conta", conexoes)
	}
	if !strings.Contains(r, "não configurado") {
		t.Errorf("EmailVerificarAgora() = %q, queria aviso de e-mail não configurado", r)
	}
	var d map[string]any
	_ = json.Unmarshal([]byte(NewApp().EmailResumo()), &d)
	if d["sem_contas"] != true {
		t.Errorf("EmailResumo sem contas devia trazer sem_contas=true: %v", d)
	}
}

// TestContaGuardaSenhaCifrada — a senha só é gravada depois que o login funciona,
// vai CIFRADA para o disco e nunca volta para a tela.
func TestContaGuardaSenhaCifrada(t *testing.T) {
	pastaIsolada(t)
	host, porta, _ := servidorIMAP(t)
	a := NewApp()

	ruim, _ := json.Marshal(pedidoConta{Provedor: "outro", Usuario: "username", Senha: "errada", Host: host, Porta: porta})
	var r map[string]any
	_ = json.Unmarshal([]byte(a.EmailAdicionarConta(string(ruim))), &r)
	if r["ok"] == true || len(leContas()) != 0 {
		t.Fatalf("senha errada não podia salvar a conta: %v", r)
	}

	bom, _ := json.Marshal(pedidoConta{Provedor: "outro", Usuario: "username", Senha: "password", Host: host, Porta: porta})
	_ = json.Unmarshal([]byte(a.EmailAdicionarConta(string(bom))), &r)
	if r["ok"] != true {
		t.Fatalf("EmailAdicionarConta = %v", r)
	}

	b, _ := os.ReadFile(arquivoContas())
	if strings.Contains(string(b), "\"password\"") || strings.Contains(string(b), ":\"password") {
		t.Fatalf("a senha foi gravada em texto puro: %s", b)
	}
	l := leContas()
	if len(l) != 1 {
		t.Fatalf("queria 1 conta, tem %d", len(l))
	}
	if s, err := decifra(l[0].Segredo); err != nil || s != "password" {
		t.Fatalf("decifra = %q, %v", s, err)
	}
	tela := a.EmailContas()
	if strings.Contains(tela, l[0].Segredo) || strings.Contains(tela, "segredo") {
		t.Fatalf("o segredo vazou para a tela: %s", tela)
	}

	// a mesma conta de novo: substitui, não duplica
	_ = a.EmailAdicionarConta(string(bom))
	if n := len(leContas()); n != 1 {
		t.Fatalf("adicionar a mesma conta duplicou: %d contas", n)
	}

	// o resumo já sai com a primeira leitura, com a conta em cada mensagem
	var res struct {
		Conta         string           `json:"conta"`
		Desconhecidos []CabecalhoEmail `json:"desconhecidos"`
	}
	_ = json.Unmarshal([]byte(a.EmailResumo()), &res)
	if res.Conta != "username" || len(res.Desconhecidos) == 0 || res.Desconhecidos[0].Conta != "username" {
		t.Fatalf("EmailResumo = %+v", res)
	}

	if r := a.EmailVerificarAgora(); r != "ok" {
		t.Fatalf("EmailVerificarAgora() = %q", r)
	}
	_ = json.Unmarshal([]byte(a.EmailRemoverConta(l[0].ID)), &r)
	if r["ok"] != true || len(leContas()) != 0 {
		t.Fatalf("EmailRemoverConta = %v; sobraram %d", r, len(leContas()))
	}
}

// TestMicrosoftIndisponivelSemClientID — sem o app registrado na Microsoft, as
// opções aparecem desabilitadas com o motivo, e o login não abre.
func TestMicrosoftIndisponivelSemClientID(t *testing.T) {
	antes := msClientID
	t.Cleanup(func() { msClientID = antes })
	msClientID = ""
	p, _ := provedorEmail("hotmail")
	if p.Disponivel || p.Motivo == "" {
		t.Fatalf("hotmail sem client ID: %+v", p)
	}
	var r map[string]any
	_ = json.Unmarshal([]byte(NewApp().EmailEntrarMicrosoft("hotmail", "")), &r)
	if r["ok"] == true {
		t.Fatal("abriu o login da Microsoft sem client ID")
	}
	if g, _ := provedorEmail("gmail"); !g.Disponivel {
		t.Fatal("Gmail tem de funcionar sempre")
	}
}
