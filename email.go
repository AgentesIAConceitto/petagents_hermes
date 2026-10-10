package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ── Agente de e-mail (triagem que aprende) ────────────────────────────
// SÓ LEITURA: le cabecalhos de C:\petagent_mail\inbox.json (gerado fora daqui,
// em contexto nao-elevado, lendo do Outlook local). Nunca escreve na caixa:
// nao apaga, nao move, nao marca, nao responde.

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

const arquivoInbox = `C:\petagent_mail\inbox.json`
const arquivoStatusPoll = `C:\petagent_mail\status.json`

// StatusPoll — o que o poll.ps1 relata de cada tentativa (04/10/2026).
//
// Antes disto o app dormia 40 s no escuro e lia o inbox.json que estivesse lá —
// e reportava leitura VELHA como se fosse de agora (medido: 10:29:54 o poll falhou
// com 0x90740111 e às 10:30:29 o app logou "40 mensagens, 0 novos importantes",
// lendo o JSON do ciclo anterior). Agora o poll diz o que aconteceu:
//
//	ok          → leitura nova de verdade
//	sem_outlook → Outlook fechado; leitura ADIADA (nada foi iniciado)
//	ocupado     → já havia uma coleta rodando (mutex)
//	erro        → falhou (detalhe manda)
type StatusPoll struct {
	Estado    string `json:"estado"`
	Quando    string `json:"quando"`
	Mensagens int    `json:"mensagens"`
	Detalhe   string `json:"detalhe"`
}

func leStatusPoll() StatusPoll {
	var s StatusPoll
	b, err := os.ReadFile(arquivoStatusPoll)
	if err != nil {
		return s
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	_ = json.Unmarshal(b, &s)
	return s
}

// esperaStatusPoll — espera o poll PUBLICAR um status novo (Quando diferente do
// anterior). Devolve o status e se houve tempo hábil. Substitui o sleep cego de 40 s.
func esperaStatusPoll(antes StatusPoll, limite time.Duration) (StatusPoll, bool) {
	fim := time.Now().Add(limite)
	for time.Now().Before(fim) {
		s := leStatusPoll()
		if s.Quando != "" && s.Quando != antes.Quando {
			return s, true
		}
		time.Sleep(400 * time.Millisecond)
	}
	return leStatusPoll(), false
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
// ⚠️ 05/10/2026: o carimbo vem do Outlook com .ToString("s") = hora LOCAL, SEM fuso.
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

func leCaixa() (CaixaEmail, error) {
	var c CaixaEmail
	b, err := os.ReadFile(arquivoInbox)
	if err != nil {
		return c, err
	}
	// ⚠️ o PowerShell grava UTF-8 COM BOM; json.Unmarshal falha no BOM (falha silenciosa!).
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	err = json.Unmarshal(b, &c)
	return c, err
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
	return m.Quando + "|" + strings.ToLower(m.DeEmail) + "|" + strings.ToLower(m.Assunto)
}

// ── bindings ─────────────────────────────────────────────────────────

// EmailVerificarAgora — dispara a leitura do Outlook AGORA, SEM JANELA.
//
// Duas exigências ao mesmo tempo:
// 1) precisa rodar NÃO-elevado (o COM do Outlook recusa processo elevado) →
//    por isso passa pelo shell do usuário (explorer.exe);
// 2) precisa ser SILENCIOSO (nada de console piscando a cada varredura) →
//    por isso o alvo é um .vbs (host sem console) que lança o PowerShell oculto.
// Nunca voltar a chamar .cmd/.bat aqui: gera janela de terminal na tela do usuário.
func (a *App) EmailVerificarAgora() string {
	cmd := exec.Command("explorer.exe", `C:\petagent_mail\poll_silencioso.vbs`)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	if err := cmd.Start(); err != nil {
		logMsg("email: falha ao disparar leitura silenciosa: " + err.Error())
		return "erro ao disparar leitura: " + err.Error()
	}
	logMsg("email: leitura do Outlook disparada (silenciosa, sem janela)")
	return "ok"
}

// EmailResumo — resumo + listas + desconhecidos (para o painel do boneco).
func (a *App) EmailResumo() string {
	r := leRegras()
	out := map[string]interface{}{"regras": r}
	c, err := leCaixa()
	if err != nil {
		out["erro"] = "sem leitura ainda — clique em Verificar agora"
		b, _ := json.Marshal(out)
		return string(b)
	}
	imp := []CabecalhoEmail{}
	des := []CabecalhoEmail{}
	spam := 0
	for _, m := range c.Mensagens {
		switch classifica(r, m) {
		case "importante":
			imp = append(imp, m)
		case "spam":
			spam++
		default:
			des = append(des, m)
		}
	}
	out["conta"] = c.Conta
	out["verificado_em"] = c.VerificadoEm
	out["contexto"] = c.Contexto
	out["total_pasta"] = c.TotalPasta
	out["nao_lidas"] = c.NaoLidas
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

// emailLoop — a cada ~10 min: dispara a leitura e avisa o que for importante.
//
// 04/10/2026: era `Sleep(40s) + processaEmail()`, acontecesse o que acontecesse.
// Agora espera o STATUS do poll e reage a ele — adiado não é leitura, falha não é
// leitura, e dado velho nunca é apresentado como novo.
func (a *App) emailLoop() {
	time.Sleep(25 * time.Second) // deixa a janela subir primeiro
	for {
		antes := leStatusPoll()
		a.EmailVerificarAgora()
		s, aTempo := esperaStatusPoll(antes, 35*time.Second)
		if !aTempo {
			logMsg("email: poll nao publicou status em 35s — ciclo pulado (nada foi lido)")
		} else {
			switch s.Estado {
			case "ok":
				a.processaEmail()
			case "sem_outlook":
				logMsg("email: adiado — Outlook nao esta aberto (nada foi iniciado pelo agente)")
			case "ocupado":
				logMsg("email: ja havia uma coleta rodando — ciclo pulado (mutex)")
			case "erro":
				logMsg("email: o poll falhou — " + s.Detalhe)
			default:
				logMsg("email: estado desconhecido do poll: " + s.Estado)
			}
		}
		time.Sleep(9 * time.Minute)
	}
}

// idadeLeitura — há quanto tempo o poll gravou o inbox.json. Data ilegível conta
// como muito velha: sem saber a idade, não se avisa nada.
//
// ⚠️ 05/10/2026: mesmo defeito do ehNova — o poll grava hora LOCAL sem fuso e
// `time.Parse` assumia UTC, somando o offset local à idade (medido: "leitura VELHA
// (420 min)" com o Windows no Pacífico; 180 min em Brasília). Com o limite de 20 min
// isto MUDO o e-mail por completo. ParseInLocation(Local) é o conserto.
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
// ⚠️ 04/10/2026: antes ele não olhava a idade da leitura. Medido: às 10:29:54 o
// poll FALHOU (0x90740111) e às 10:30:29 o app logou "40 mensagens, 0 novos
// importantes" — lendo o JSON do ciclo anterior como se fosse leitura de agora.
// Num agente que existe para avisar e-mail importante, dado velho com cara de novo
// é pior que silêncio.
func (a *App) processaEmail() {
	c, err := leCaixa()
	if err != nil {
		return
	}
	if idade := idadeLeitura(c.VerificadoEm); idade > idadeMaximaLeitura {
		logMsg(fmt.Sprintf("email: leitura VELHA (%.0f min) — nao aviso nada com dado velho",
			idade.Minutes()))
		return
	}
	r := leRegras()
	vistos := leVistos()
	novos := make([]CabecalhoEmail, 0, 4)
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
	if len(novos) > 0 {
		gravaVistos(vistos)
		// teto de balõezinhos: no máximo 3 na tela, o resto vira resumo
		limite := 3
		for i, m := range novos {
			if i >= limite {
				a.aviso("📧", fmt.Sprintf("+%d e-mails importantes", len(novos)-limite),
					"abre o painel 📧 no menu do boneco")
				break
			}
			nome := m.DeNome
			if nome == "" {
				nome = m.DeEmail
			}
			a.aviso("📧", "E-mail importante", nome+" — "+m.Assunto)
			time.Sleep(900 * time.Millisecond)
		}
	}
	logMsg(fmt.Sprintf("email: %d mensagens, %d novos importantes", len(c.Mensagens), len(novos)))
}
