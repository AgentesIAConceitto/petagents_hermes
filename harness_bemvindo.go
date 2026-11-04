package main

// harness_bemvindo.go — primeira execução e cota visível (30/10/2026).
//
// PÚBLICO-ALVO MUDOU O DESENHO: o agente grátis é para quem está descobrindo IA e não pode
// pagar. Essa pessoa não sabe o que é "API key" — se o primeiro contato for um erro
// técnico, ela fecha o app e não volta. Então:
//
//   - PrecisaConfigurar(): o app descobre sozinho se ainda não há como conversar e abre o
//     guia (em vez de esperar a pessoa achar o menu).
//   - EstadoCotas(): a cota do plano grátis ACABA (aconteceu comigo hoje: estourei o dia do
//     Google testando). Em vez de erro seco, a tela mostra "sua cota do dia acabou, troquei
//     de provedor" — foi o próprio motor quem avisou na troca.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// detectarHermes é variável para o teste poder simular máquina sem Hermes.
var detectarHermes = DetectarHermes

// TrocaDeProvedor é o último failover registrado pelo motor.
type TrocaDeProvedor struct {
	De     string    `json:"de"`
	Para   string    `json:"para"`
	Motivo string    `json:"motivo"`
	Quando time.Time `json:"quando"`
}

var (
	cotaMu      sync.Mutex
	ultimaTroca TrocaDeProvedor
	cotaAviso   string // mensagem pronta para a tela
)

// registrarTroca é chamado pelo motor quando troca de provedor (429, cota, rede).
func registrarTroca(de, para, motivo string) {
	cotaMu.Lock()
	defer cotaMu.Unlock()
	ultimaTroca = TrocaDeProvedor{De: de, Para: para, Motivo: motivo, Quando: time.Now()}
	cotaAviso = "Sua cota do plano grátis acabou em " + de + ". Troquei para " + para + " automaticamente."
}

// limparTroca é chamado quando um turno termina sem troca (o aviso não fica eterno).
func limparTroca() {
	cotaMu.Lock()
	defer cotaMu.Unlock()
	cotaAviso = ""
}

// EstadoCotasHarness devolve o aviso de cota/troca para a tela.
func (a *App) EstadoCotasHarness() map[string]any {
	cotaMu.Lock()
	defer cotaMu.Unlock()
	out := map[string]any{"aviso": cotaAviso}
	if ultimaTroca.Quando.IsZero() {
		return out
	}
	out["de"] = ultimaTroca.De
	out["para"] = ultimaTroca.Para
	out["motivo"] = ultimaTroca.Motivo
	out["quando"] = ultimaTroca.Quando.Format("02/01 15:04")
	return out
}

// PrecisaConfigurarHarness diz se ainda falta o essencial para o agente conversar.
// É o gatilho do guia de primeira execução.
func (a *App) PrecisaConfigurarHarness() map[string]any {
	cofre := cofreDoApp()
	temChave := cofre.TotalChaves() > 0
	// detecta Hermes só se não houver chave (evita gastar 5s no startup de quem já configurou)
	temHermes := false
	motivoDetect := ""
	if !temChave {
		d := detectarHermes()
		temHermes = d.Instalado
		motivoDetect = d.Motivo
	}
	precisa := !temChave && !temHermes
	motivo := ""
	switch {
	case precisa:
		motivo = "Ainda não há como conversar: falta uma chave grátis (ou o Hermes instalado)."
	case !temChave && temHermes:
		motivo = "Você está usando o Hermes. Se quiser o modo próprio grátis, configure uma chave."
	}
	return map[string]any{
		"precisa":       precisa,
		"temChave":      temChave,
		"temHermes":     temHermes,
		"motivo":        motivo,
		"motivoHermes":  motivoDetect,
		"primeiraChave": "gemini", // medido: é o mais rápido para tarefa básica
	}
}

// PassosBemVindo devolve o roteiro do guia, na ordem, com o link de cada provedor.
// O roteiro mora no Go (não no JS) para não divergir do que o cofre espera.
func (a *App) PassosBemVindo() []map[string]string {
	return []map[string]string{
		{"n": "1", "titulo": "Crie a sua chave grátis",
			"texto": "Não precisa de cartão. O Google dá uma cota diária grátis; a NVIDIA dá 40 chamadas por minuto sem cobrar por token.",
			"dica":  "Comece pelo Google: é o mais rápido em tarefa simples."},
		{"n": "2", "titulo": "Cole aqui e teste",
			"texto": "Cole a chave no campo do provedor e clique em Testar. O app faz uma chamada de verdade e diz se a chave vale.",
			"dica":  "A chave fica guardada cifrada no seu Windows — só você abre."},
		{"n": "3", "titulo": "Pronto para conversar",
			"texto": "Com a chave validada, o agente já conversa, busca na web, lê arquivos e agenda tarefas.",
			"dica":  "Se a cota do dia acabar, o app troca de provedor sozinho."},
	}
}

// gravaDiagnostico escreve o estado essencial em %LOCALAPPDATA%\petagent\diag.txt.
// Serve para diagnosticar o app em máquina de terceiro, onde não há console nem devtools.
func gravaDiagnostico() {
	c := AbrirCofre("")
	var b strings.Builder
	b.WriteString("== PetAgent diagnostico ==\n")
	b.WriteString("quando: " + time.Now().Format("02/01/2006 15:04:05") + "\n")
	b.WriteString("build: " + buildInfo + "\n")
	b.WriteString(fmt.Sprintf("chaves google: %d\n", len(c.Chaves(ChaveGemini))))
	b.WriteString(fmt.Sprintf("chaves nvidia: %d\n", len(c.Chaves(ChaveNVIDIA))))
	b.WriteString(fmt.Sprintf("chaves brave: %d\n", len(c.Chaves(ChaveBrave))))
	b.WriteString("modo: " + string(c.ModoEscolhido()) + "\n")
	h := DetectarHermes()
	b.WriteString(fmt.Sprintf("hermes: %v (%s)\n", h.Instalado, h.Versao))
	b.WriteString("--- estado para a tela (JSON) ---\n")
	if jsonBytes, err := json.MarshalIndent(c.EstadoChaves(), "", " "); err == nil {
		b.Write(jsonBytes)
		b.WriteString("\n")
	} else {
		b.WriteString("erro ao montar estado: " + err.Error() + "\n")
	}
	b.WriteString("--- fim ---\n")
	_ = os.MkdirAll(pastaPetAgente(), 0o755)
	_ = os.WriteFile(filepath.Join(pastaPetAgente(), "diag.txt"), []byte(b.String()), 0o644)
}
