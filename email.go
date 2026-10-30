package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ── Agente de e-mail (triagem que aprende) ────────────────────────────
// SÓ LEITURA: lê os cabeçalhos das contas que a pessoa adicionou (email_contas.go),
// direto do servidor por IMAP (email_imap.go). Nunca escreve na caixa: não apaga,
// não move, não marca, não responde.
//
// 22/10/2026: antes lia o Outlook instalado no PC (COM, via C:\petagent_mail).
// Quem não usava Outlook ficava sem o recurso; agora vale para qualquer conta.

type RegrasEmail struct {
	Importantes []string `json:"importantes"` // "@dominio.com.br" | "fulano@x.com" | "assunto:termo"
	Spam        []string `json:"spam"`
	Aprendido   []string `json:"aprendido"` // historico de decisoes do dono
}

type CabecalhoEmail struct {
	Quando  string `json:"quando"`
	DeNome  string `json:"de_nome"`
	DeEmail string `json:"de_email"`
	Assunto string `json:"assunto"`
	NaoLida bool   `json:"nao_lida"`
	Tamanho int    `json:"tamanho"`
	Conta   string `json:"conta"` // de qual conta veio (várias contas ao mesmo tempo)
	ContaID string `json:"conta_id"`
	UID     uint32 `json:"uid"`    // para buscar o texto depois (resumo)
	MsgID   string `json:"msg_id"` // Message-ID: o link "Abrir" do Gmail acha o e-mail por ele
	Resumo  string `json:"resumo,omitempty"`
	Link    string `json:"link,omitempty"` // onde responder (webmail da conta)
}

type CaixaEmail struct {
	VerificadoEm string           `json:"verificado_em"`
	Conta        string           `json:"conta"`
	Contexto     string           `json:"contexto"`
	TotalPasta   int              `json:"total_pasta"`
	NaoLidas     int              `json:"nao_lidas"`
	LidasAgora   int              `json:"lidas_agora"`
	Mensagens    []CabecalhoEmail `json:"mensagens"`
}

// estadoConta — o que aconteceu na última tentativa de leitura de uma conta:
//
//	ok             → leitura nova de verdade
//	erro           → falhou (Erro diz por quê); a leitura anterior NÃO vira nova
//	precisa_entrar → a Microsoft pediu login de novo (refresh token recusado)
type estadoConta struct {
	Estado       string
	Erro         string
	VerificadoEm string
}

var (
	leituraMu sync.Mutex // UMA leitura por vez: ciclo automático e "Verificar agora" nunca juntos
	cacheMu   sync.Mutex
	caixas    = map[string]CaixaEmail{}  // id da conta → última leitura que deu certo
	estados   = map[string]estadoConta{} // id da conta → última tentativa
)

func guardaLeitura(c ContaEmail, cx CaixaEmail, err error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	agora := time.Now().Format("2006-01-02T15:04:05")
	switch {
	case err == nil:
		caixas[c.ID] = cx
		estados[c.ID] = estadoConta{Estado: "ok", VerificadoEm: cx.VerificadoEm}
	case errors.Is(err, errPrecisaEntrar):
		estados[c.ID] = estadoConta{Estado: "precisa_entrar", Erro: err.Error(), VerificadoEm: agora}
	default:
		estados[c.ID] = estadoConta{Estado: "erro", Erro: err.Error(), VerificadoEm: agora}
	}
}

func esqueceLeitura(id string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	delete(caixas, id)
	delete(estados, id)
}

func estadoDe(id string) estadoConta {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	return estados[id]
}

// leConta — uma conta, com a credencial certa (senha ou token renovado).
func leConta(c ContaEmail) (CaixaEmail, error) {
	cred, err := credencialDe(c)
	if err != nil {
		return CaixaEmail{}, err
	}
	return leCaixaIMAP(c, cred)
}

// leTodasContas — lê todas em paralelo (cada uma com o seu limite de tempo) e
// devolve os erros por conta. Sem contas, devolve ok=false.
func leTodasContas() (temContas bool, erros []string) {
	leituraMu.Lock()
	defer leituraMu.Unlock()
	contas := leContas()
	if len(contas) == 0 {
		return false, nil
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, c := range contas {
		wg.Add(1)
		go func(c ContaEmail) {
			defer wg.Done()
			type lida struct {
				cx  CaixaEmail
				err error
			}
			fim := make(chan lida, 1) // com folga: a leitura atrasada não fica presa
			go func() { cx, err := leConta(c); fim <- lida{cx, err} }()
			var cx CaixaEmail
			var err error
			select {
			case l := <-fim:
				cx, err = l.cx, l.err
			case <-time.After(45 * time.Second):
				err = errors.New("a leitura passou de 45 s e foi deixada para a próxima")
			}
			guardaLeitura(c, cx, err)
			if err != nil {
				mu.Lock()
				erros = append(erros, c.Usuario+": "+err.Error())
				mu.Unlock()
				logMsg("email: " + c.Usuario + " — " + err.Error())
			}
		}(c)
	}
	wg.Wait()
	return true, erros
}

func pastaPetAgente() string {
	d := filepath.Join(os.Getenv("LOCALAPPDATA"), "petagent")
	os.MkdirAll(d, 0o755)
	return d
}

func arquivoRegras() string { return filepath.Join(pastaPetAgente(), "email_regras.json") }
func arquivoVistos() string { return filepath.Join(pastaPetAgente(), "email_vistos.json") }

func leRegras() RegrasEmail {
	// Semente inicial (o dono pode corrigir tudo pelo painel do boneco,
	// com um clique por domínio). Regras dele: só avisa o que é NOVO/não lido.
	padrao := RegrasEmail{
		Importantes: []string{"@suaempresa.com.br", "@cliente-importante.com"}, // <- configure
		Spam:        []string{"@ventaro.boats", "assunto:prazo final para pagamento"},
	}
	b, err := os.ReadFile(arquivoRegras())
	if err != nil {
		gravaRegras(padrao) // primeira vez: deixa as listas visiveis/editaveis
		return padrao
	}
	r := padrao
	if json.Unmarshal(b, &r) != nil || r.Importantes == nil {
		return padrao
	}
	return r
}

// ehNova — só o que é novo interessa: não lida OU chegou nos últimos 45 min.
// (Evita avisar de e-mail velho que já estava na caixa antes do agente existir.)
//
// ⚠️ 05/10/2026: o carimbo é hora LOCAL, SEM fuso (cabecalhoDe grava assim).
// `time.Parse` num layout sem zona assume UTC — então a idade saía inflada pelo offset
// local (3h no Brasil) e NADA era considerado novo. Tem de ser ParseInLocation(Local).
func ehNova(m CabecalhoEmail) bool {
	if m.NaoLida {
		return true
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05", m.Quando, time.Local)
	if err != nil {
		return false
	}
	return time.Since(t) < 45*time.Minute
}

func gravaRegras(r RegrasEmail) {
	if b, err := json.MarshalIndent(r, "", "  "); err == nil {
		os.WriteFile(arquivoRegras(), b, 0o644)
	}
}

// casaPadrao — regra simples: "@dominio" (dominio), "algo@x" (endereco) ou
// "assunto:termo" (termo no assunto). Tudo case-insensitive.
func casaPadrao(m CabecalhoEmail, padrao string) bool {
	p := strings.ToLower(strings.TrimSpace(padrao))
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "assunto:") {
		return strings.Contains(strings.ToLower(m.Assunto), strings.TrimPrefix(p, "assunto:"))
	}
	de := strings.ToLower(m.DeEmail)
	if strings.HasPrefix(p, "@") {
		return strings.HasSuffix(de, p)
	}
	if strings.Contains(p, "@") {
		return de == p
	}
	return strings.Contains(de, p)
}

// classifica — importante | spam | desconhecido
func classifica(r RegrasEmail, m CabecalhoEmail) string {
	for _, p := range r.Spam {
		if casaPadrao(m, p) {
			return "spam"
		}
	}
	for _, p := range r.Importantes {
		if casaPadrao(m, p) {
			return "importante"
		}
	}
	return "desconhecido"
}

// caixasAtuais — as leituras guardadas das contas que ainda existem.
func caixasAtuais(contas []ContaEmail) []CaixaEmail {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	l := []CaixaEmail{}
	for _, c := range contas {
		if cx, ok := caixas[c.ID]; ok {
			l = append(l, cx)
		}
	}
	return l
}

// leVistos / gravaVistos — evita avisar duas vezes a mesma mensagem.
func leVistos() map[string]bool {
	v := map[string]bool{}
	if b, err := os.ReadFile(arquivoVistos()); err == nil {
		var l []string
		if json.Unmarshal(b, &l) == nil {
			for _, s := range l {
				v[s] = true
			}
		}
	}
	return v
}

func gravaVistos(v map[string]bool) {
	l := make([]string, 0, len(v))
	for k := range v {
		l = append(l, k)
	}
	if b, err := json.Marshal(l); err == nil {
		os.WriteFile(arquivoVistos(), b, 0o644)
	}
}

func impressao(m CabecalhoEmail) string {
	return strings.ToLower(m.Conta) + "|" + m.Quando + "|" + strings.ToLower(m.DeEmail) + "|" + strings.ToLower(m.Assunto)
}

// ── bindings ─────────────────────────────────────────────────────────

// EmailVerificarAgora — lê TODAS as contas agora e só responde quando terminar.
// "ok" = todas leram; senão, o motivo (sem contas, ou o erro de cada uma).
func (a *App) EmailVerificarAgora() string {
	tem, erros := leTodasContas()
	if !tem {
		return "Nenhuma conta de e-mail conectada. Adicione uma pelo botão Contas, no painel E-mail."
	}
	if len(erros) > 0 {
		return strings.Join(erros, " · ")
	}
	return "ok"
}

// EmailResumo — resumo + listas + desconhecidos (para o painel do boneco).
func (a *App) EmailResumo() string {
	r := leRegras()
	contas := leContas()
	telaContas := []map[string]any{}
	for _, c := range contas {
		telaContas = append(telaContas, contaParaTela(c))
	}
	out := map[string]interface{}{"regras": r, "contas": telaContas}
	if len(contas) == 0 {
		out["sem_contas"] = true
		out["erro"] = "Nenhuma conta conectada."
		b, _ := json.Marshal(out)
		return string(b)
	}
	cxs := caixasAtuais(contas)
	if len(cxs) == 0 {
		out["erro"] = "Nenhuma leitura ainda."
		b, _ := json.Marshal(out)
		return string(b)
	}
	imp := []CabecalhoEmail{}
	des := []CabecalhoEmail{}
	spam, total, naoLidas := 0, 0, 0
	verificado := ""
	todas := []CabecalhoEmail{}
	for _, cx := range cxs {
		todas = append(todas, cx.Mensagens...)
		total += cx.TotalPasta
		naoLidas += cx.NaoLidas
		if cx.VerificadoEm > verificado {
			verificado = cx.VerificadoEm
		}
	}
	// mais novas primeiro, misturando as contas
	sort.SliceStable(todas, func(i, j int) bool { return todas[i].Quando > todas[j].Quando })
	porID := map[string]ContaEmail{}
	for _, c := range contas {
		porID[c.ID] = c
	}
	resumos := func() map[string]resumoSalvo { resumosMu.Lock(); defer resumosMu.Unlock(); return leResumos() }()
	for _, m := range todas {
		m.Resumo = resumos[impressao(m)].Resumo
		m.Link = linkWebmail(porID[m.ContaID], m)
		switch classifica(r, m) {
		case "importante":
			imp = append(imp, m)
		case "spam":
			spam++
		default:
			des = append(des, m)
		}
	}
	if len(contas) == 1 {
		out["conta"] = contas[0].Usuario
	} else {
		out["conta"] = strconv.Itoa(len(contas)) + " contas"
	}
	out["verificado_em"] = verificado
	out["contexto"] = "imap"
	out["total_pasta"] = total
	out["nao_lidas"] = naoLidas
	out["importantes"] = imp
	out["desconhecidos"] = des
	out["spam_n"] = spam
	b, _ := json.Marshal(out)
	return string(b)
}

// EmailMarcar — APRENDIZADO: o dono diz o que é importante ou lixo.
// tipo = "importante" | "spam"; alvo = endereço, @domínio ou assunto:termo.
func (a *App) EmailMarcar(alvo, tipo string) string {
	alvo = strings.TrimSpace(alvo)
	if alvo == "" {
		return "alvo vazio"
	}
	r := leRegras()
	// tira da lista oposta (aprendizado de correção)
	tira := func(l []string) []string {
		out := []string{}
		for _, x := range l {
			if !strings.EqualFold(x, alvo) {
				out = append(out, x)
			}
		}
		return out
	}
	if tipo == "importante" {
		r.Spam = tira(r.Spam)
		r.Importantes = append(tira(r.Importantes), alvo)
	} else {
		r.Importantes = tira(r.Importantes)
		r.Spam = append(tira(r.Spam), alvo)
	}
	r.Aprendido = append(r.Aprendido, time.Now().Format("02/01 15:04")+" "+tipo+" "+alvo)
	gravaRegras(r)
	logMsg("email: aprendido -> " + tipo + " " + alvo)
	return "ok"
}

// avisoEmail — o balão de e-mail importante: com o resumo e o botão "Abrir"
// (link do webmail da conta, onde a pessoa responde).
func (a *App) avisoEmail(titulo, texto, link string) {
	logMsg("aviso: " + titulo + " | " + texto)
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "aviso", map[string]string{
		"icone": "📧", "titulo": titulo, "texto": texto, "tipo": "email", "link": link,
	})
}

// emailLoop — a cada ~10 min: lê as contas e avisa o que for importante.
// Conta que falhou não entra no aviso (processaEmail ignora leitura velha).
func (a *App) emailLoop() {
	time.Sleep(25 * time.Second) // deixa a janela subir primeiro
	desligado := false           // avisa no log uma vez, não a cada ciclo
	for {
		tem, _ := leTodasContas()
		if !tem {
			if !desligado {
				logMsg("email: desligado — nenhuma conta adicionada")
				desligado = true
			}
		} else {
			desligado = false
			a.processaEmail()
		}
		time.Sleep(10 * time.Minute)
	}
}

// idadeLeitura — há quanto tempo a conta foi lida. Data ilegível conta
// como muito velha: sem saber a idade, não se avisa nada.
//
// ⚠️ 05/10/2026: o carimbo é hora LOCAL sem fuso e `time.Parse` assumia UTC,
// somando o offset local à idade (medido: "leitura VELHA (420 min)" com o Windows no
// Pacífico; 180 min em Brasília). Com o limite de 20 min isto MUDO o e-mail por
// completo. ParseInLocation(Local) é o conserto.
func idadeLeitura(quando string) time.Duration {
	t, err := time.ParseInLocation("2006-01-02T15:04:05", quando, time.Local)
	if err != nil {
		return 24 * time.Hour
	}
	return time.Since(t)
}

// idadeMaximaLeitura — acima disto o dado é velho demais para virar aviso.
const idadeMaximaLeitura = 20 * time.Minute

// processaEmail — classifica e emite avisos (sem repetir o que já avisou).
//
// ⚠️ 04/10/2026: dado velho com cara de novo é pior que silêncio. Uma conta cuja
// leitura falhou fica com a caixa ANTERIOR no cache — e essa não vira aviso.
func (a *App) processaEmail() {
	contas := leContas()
	r := leRegras()
	vistos := leVistos()
	novos := make([]CabecalhoEmail, 0, 4)
	total := 0
	for _, c := range caixasAtuais(contas) {
		if idade := idadeLeitura(c.VerificadoEm); idade > idadeMaximaLeitura {
			logMsg(fmt.Sprintf("email: %s — leitura VELHA (%.0f min), nao aviso nada com dado velho",
				c.Conta, idade.Minutes()))
			continue
		}
		total += len(c.Mensagens)
		for _, m := range c.Mensagens {
			if classifica(r, m) != "importante" {
				continue
			}
			if !ehNova(m) { // só o que é NOVO / não lido (regra do dono)
				continue
			}
			fp := impressao(m)
			if vistos[fp] {
				continue
			}
			vistos[fp] = true
			novos = append(novos, m)
		}
	}
	if len(novos) > 0 {
		gravaVistos(vistos)
		porID := map[string]ContaEmail{}
		for _, c := range contas {
			porID[c.ID] = c
		}
		// os importantes novos são resumidos sozinhos (no máximo maxResumosCiclo por
		// leitura; o resto fica com o botão "Resumir" do painel). Falhou: avisa sem resumo.
		resumos := map[int]string{}
		for i, m := range novos {
			if i >= maxResumosCiclo {
				break
			}
			if r, err := resumeMensagem(porID[m.ContaID], m); err == nil {
				resumos[i] = r
			} else {
				logMsg("email: resumo automatico falhou (" + m.Conta + "): " + err.Error())
			}
		}
		// teto de balõezinhos: no máximo 3 na tela, o resto vira resumo
		limite := 3
		for i, m := range novos {
			if i >= limite {
				a.aviso("📧", fmt.Sprintf("Mais %d e-mails importantes", len(novos)-limite),
					"Veja todos no painel E-mail.")
				break
			}
			nome := m.DeNome
			if nome == "" {
				nome = m.DeEmail
			}
			titulo := "E-mail importante · " + nome
			if len(contas) > 1 {
				titulo += " · " + m.Conta
			}
			texto := m.Assunto
			if r := resumos[i]; r != "" {
				texto += " — " + r
			}
			a.avisoEmail(titulo, texto, linkWebmail(porID[m.ContaID], m))
			time.Sleep(900 * time.Millisecond)
		}
	}
	logMsg(fmt.Sprintf("email: %d mensagens em %d conta(s), %d novos importantes", total, len(contas), len(novos)))
}
