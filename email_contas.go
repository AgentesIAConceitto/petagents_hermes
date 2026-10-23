package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ── Contas de e-mail (22/10/2026) ─────────────────────────────────────
// Antes o boneco só lia o Outlook instalado no PC (COM). Agora a pessoa adiciona
// CONTAS — Gmail, Hotmail, a do trabalho — e o app lê direto do servidor (IMAP),
// sem depender de programa de e-mail nenhum. Ver email_imap.go e email_oauth.go.

// Provedor — uma linha da lista "Adicionar conta". Auth: "senha" | "microsoft".
type Provedor struct {
	ID         string `json:"id"`
	Nome       string `json:"nome"`
	Host       string `json:"host"`
	Porta      int    `json:"porta"`
	Auth       string `json:"auth"`
	Ajuda      string `json:"ajuda"`
	LinkAjuda  string `json:"link_ajuda,omitempty"`
	Disponivel bool   `json:"disponivel"`
	Motivo     string `json:"motivo,omitempty"` // por que está indisponível
}

var provedoresEmail = []Provedor{
	{ID: "gmail", Nome: "Gmail / Google Workspace", Host: "imap.gmail.com", Porta: 993, Auth: "senha",
		Ajuda:     "O Google não aceita a senha normal da conta em outros aplicativos. Crie uma senha de app (é preciso ter a verificação em duas etapas ativada) e cole-a abaixo.",
		LinkAjuda: "https://myaccount.google.com/apppasswords"},
	{ID: "hotmail", Nome: "Hotmail / Outlook.com / Live", Host: "outlook.office365.com", Porta: 993, Auth: "microsoft",
		Ajuda: "O acesso é feito no site da Microsoft, pelo navegador. A sua senha não passa pelo PetAgent."},
	{ID: "m365", Nome: "Microsoft 365 (empresa)", Host: "outlook.office365.com", Porta: 993, Auth: "microsoft",
		Ajuda: "O acesso é feito no site da Microsoft, pelo navegador. Se a empresa bloquear o acesso, peça à equipe de TI que libere o IMAP."},
	{ID: "yahoo", Nome: "Yahoo", Host: "imap.mail.yahoo.com", Porta: 993, Auth: "senha",
		Ajuda:     "O Yahoo exige uma senha de app. Gere-a nas configurações de segurança da conta e cole-a abaixo.",
		LinkAjuda: "https://login.yahoo.com/myaccount/security/app-password"},
	{ID: "outro", Nome: "Outro / e-mail empresarial (IMAP)", Porta: 993, Auth: "senha",
		Ajuda: "Informe o servidor IMAP indicado pelo seu provedor ou pela equipe de TI (ex.: imap.suaempresa.com.br, porta 993)."},
}

func provedorEmail(id string) (Provedor, bool) {
	for _, p := range provedoresEmail {
		if p.ID == id {
			if p.Auth == "microsoft" && msClientID == "" {
				p.Motivo = "Indisponível nesta versão: o aplicativo ainda não foi registrado na Microsoft (veja o README)."
			} else {
				p.Disponivel = true
			}
			return p, true
		}
	}
	return Provedor{}, false
}

// ContaEmail — o que fica no disco. O Segredo (senha IMAP ou refresh token da
// Microsoft) vai cifrado com DPAPI e NUNCA volta para a tela.
type ContaEmail struct {
	ID       string `json:"id"`
	Provedor string `json:"provedor"`
	Usuario  string `json:"usuario"`
	Host     string `json:"host"`
	Porta    int    `json:"porta"`
	Segredo  string `json:"segredo"` // DPAPI + base64
}

func (c ContaEmail) auth() string {
	if p, ok := provedorEmail(c.Provedor); ok {
		return p.Auth
	}
	return "senha"
}

var contasMu sync.Mutex // leitura-modifica-grava de email_contas.json

func arquivoContas() string { return filepath.Join(pastaPetAgente(), "email_contas.json") }

func leContas() []ContaEmail {
	var l []ContaEmail
	if b, err := os.ReadFile(arquivoContas()); err == nil {
		_ = json.Unmarshal(b, &l)
	}
	return l
}

func gravaContas(l []ContaEmail) error {
	if l == nil {
		l = []ContaEmail{}
	}
	b, _ := json.MarshalIndent(l, "", "  ")
	tmp := arquivoContas() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, arquivoContas())
}

// salvaConta — inclui ou substitui (mesmo ID, ou mesmo usuário no mesmo servidor).
func salvaConta(c ContaEmail) error {
	contasMu.Lock()
	defer contasMu.Unlock()
	l := leContas()
	for i, x := range l {
		if x.ID == c.ID || (strings.EqualFold(x.Usuario, c.Usuario) && strings.EqualFold(x.Host, c.Host)) {
			c.ID = x.ID
			l[i] = c
			return gravaContas(l)
		}
	}
	if c.ID == "" {
		c.ID = novoIDConta()
	}
	return gravaContas(append(l, c))
}

// trocaSegredo — a Microsoft gira o refresh token: guarda o novo sem mexer no resto.
func trocaSegredo(id, segredo string) {
	contasMu.Lock()
	defer contasMu.Unlock()
	l := leContas()
	for i := range l {
		if l[i].ID == id {
			l[i].Segredo = segredo
			_ = gravaContas(l)
			return
		}
	}
}

func removeConta(id string) bool {
	contasMu.Lock()
	defer contasMu.Unlock()
	l := leContas()
	out := make([]ContaEmail, 0, len(l))
	for _, x := range l {
		if x.ID != id {
			out = append(out, x)
		}
	}
	if len(out) == len(l) {
		return false
	}
	return gravaContas(out) == nil
}

func achaConta(id string) (ContaEmail, bool) {
	for _, c := range leContas() {
		if c.ID == id {
			return c, true
		}
	}
	return ContaEmail{}, false
}

func novoIDConta() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// contaParaTela — sem o segredo.
func contaParaTela(c ContaEmail) map[string]any {
	nome := c.Provedor
	if p, ok := provedorEmail(c.Provedor); ok {
		nome = p.Nome
	}
	e := estadoDe(c.ID)
	return map[string]any{
		"id": c.ID, "provedor": c.Provedor, "provedor_nome": nome, "usuario": c.Usuario,
		"servidor": c.Host + ":" + strconv.Itoa(c.Porta), "auth": c.auth(),
		"estado": e.Estado, "erro": e.Erro, "verificado_em": e.VerificadoEm,
	}
}

// ── bindings ─────────────────────────────────────────────────────────

// EmailProvedores — a lista do "Adicionar conta".
func (a *App) EmailProvedores() string {
	l := make([]Provedor, 0, len(provedoresEmail))
	for _, p := range provedoresEmail {
		x, _ := provedorEmail(p.ID)
		l = append(l, x)
	}
	return respostaJSON(l)
}

// EmailContas — as contas salvas, sem segredos.
func (a *App) EmailContas() string {
	l := []map[string]any{}
	for _, c := range leContas() {
		l = append(l, contaParaTela(c))
	}
	return respostaJSON(l)
}

type pedidoConta struct {
	Provedor string `json:"provedor"`
	Usuario  string `json:"usuario"`
	Senha    string `json:"senha"`
	Host     string `json:"host"`
	Porta    int    `json:"porta"`
}

// EmailAdicionarConta — TESTA o login antes e só grava se ele funcionar.
// Só para provedores de senha; os da Microsoft entram por EmailEntrarMicrosoft.
func (a *App) EmailAdicionarConta(pedido string) string {
	var p pedidoConta
	if json.Unmarshal([]byte(pedido), &p) != nil {
		return respostaJSON(map[string]any{"ok": false, "erro": "pedido inválido"})
	}
	prov, ok := provedorEmail(p.Provedor)
	if !ok || prov.Auth != "senha" {
		return respostaJSON(map[string]any{"ok": false, "erro": "provedor inválido para senha"})
	}
	c := ContaEmail{Provedor: prov.ID, Usuario: strings.TrimSpace(p.Usuario), Host: prov.Host, Porta: prov.Porta}
	if prov.ID == "outro" {
		c.Host = strings.TrimSpace(p.Host)
		if p.Porta > 0 {
			c.Porta = p.Porta
		}
	}
	senha := strings.ReplaceAll(strings.TrimSpace(p.Senha), " ", "") // senha de app do Google vem "abcd efgh ..."
	if prov.ID == "outro" {
		senha = strings.TrimSpace(p.Senha) // senha comum pode ter espaço
	}
	switch {
	case c.Usuario == "":
		return respostaJSON(map[string]any{"ok": false, "erro": "Digite o e-mail (usuário)."})
	case senha == "":
		return respostaJSON(map[string]any{"ok": false, "erro": "Digite a senha."})
	case c.Host == "":
		return respostaJSON(map[string]any{"ok": false, "erro": "Digite o servidor IMAP."})
	}
	cx, err := leCaixaIMAP(c, credencial{usuario: c.Usuario, senha: senha})
	if err != nil {
		return respostaJSON(map[string]any{"ok": false, "erro": err.Error()})
	}
	seg, err := cifra(senha)
	if err != nil {
		return respostaJSON(map[string]any{"ok": false, "erro": "não consegui guardar a senha com segurança: " + err.Error()})
	}
	c.Segredo = seg
	if err := salvaConta(c); err != nil {
		return respostaJSON(map[string]any{"ok": false, "erro": "não consegui salvar: " + err.Error()})
	}
	// pega o ID definitivo (salvaConta pode ter reaproveitado o de uma conta igual)
	for _, x := range leContas() {
		if strings.EqualFold(x.Usuario, c.Usuario) && strings.EqualFold(x.Host, c.Host) {
			c = x
		}
	}
	guardaLeitura(c, cx, nil)
	logMsg("email: conta adicionada (" + prov.ID + ") " + c.Usuario)
	return respostaJSON(map[string]any{"ok": true, "mensagens": len(cx.Mensagens)})
}

// EmailRemoverConta — apaga a conta e o segredo dela.
func (a *App) EmailRemoverConta(id string) string {
	if !removeConta(id) {
		return respostaJSON(map[string]any{"ok": false, "erro": "conta não encontrada"})
	}
	esqueceLeitura(id)
	logMsg("email: conta removida " + id)
	return respostaJSON(map[string]any{"ok": true})
}
