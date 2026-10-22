package main

// Chat do boneco com o HERMES de verdade.
//
// Como funciona (verificado 02/10/2026):
//
//	hermes -z "texto" --continue petagent_chat
//
// * `-z` é o modo one-shot: roda o agente completo e imprime a resposta;
// * `--continue <sessão>` mantém a conversa — o chat do boneco tem continuidade,
//   então ele lembra do que foi dito antes;
// * custa ~30 s e tokens da conta compartilhada (por isso "saldo" e "status"
//   continuam respondendo local, sem gastar nada).
//
// Regras respeitadas: nada de janela de console (CREATE_NO_WINDOW) e nada de
// mexer em credencial — o processo roda com o ambiente que já existe.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	sessaoChat    = "petagent_chat" // sessão do Hermes usada pelo chat do boneco
	tempoResposta = 300 * time.Second
)

// caminhoHermes — onde está o executável do Hermes nesta máquina.
func caminhoHermes() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		p := filepath.Join(base, "hermes", "hermes-agent", "venv", "Scripts", "hermes.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		// Hermes 0.21+ (Windows): sem venv fixo, o lançador fica em hermes\bin
		p = filepath.Join(base, "hermes", "bin", "hermes.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("hermes"); err == nil {
		return p
	}
	return ""
}

// chamarHermes — manda o texto para o agente e devolve a resposta.
//
// Caminho normal: a PONTE QUENTE (hermes acp via chat_bridge.py) — o agente fica
// vivo e responde no tempo do modelo (~3-8 s; medido 02/10/2026).
// Reserva: `hermes -z`, que sobe um agente novo do zero a cada mensagem (~30 s).
func (a *App) chamarHermes(texto string) string {
	if resp, ok := a.perguntaPonte(texto); ok {
		return resp
	}
	return a.chamarHermesFrio(texto)
}

// chamarHermesFrio — reserva (lenta). Só usada se a ponte não estiver de pé.
func (a *App) chamarHermesFrio(texto string) string {
	exe := caminhoHermes()
	if exe == "" {
		return "Não encontrei o Hermes nesta máquina — sem ele eu não consigo conversar de verdade."
	}
	ctx, cancel := context.WithTimeout(context.Background(), tempoResposta)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-z", texto, "--continue", sessaoChat)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW (nada de console piscando)
	}
	// stdout = a resposta; stderr = log/diagnóstico do CLI. Separá-los evita que
	// linha de log entre no meio da resposta (medido 02/10/2026).
	var erros bytes.Buffer
	cmd.Stderr = &erros
	saida, err := cmd.Output()
	resp := strings.TrimSpace(string(saida))
	logMsg(fmt.Sprintf("chat: reserva (hermes -z) respondeu %d chars (err=%v)", len(resp), err != nil))
	if err != nil && resp == "" {
		if ctx.Err() == context.DeadlineExceeded {
			return "O Hermes demorou demais para responder (mais de 5 min) — tente de novo."
		}
		detalhe := strings.TrimSpace(erros.String())
		if len(detalhe) > 300 {
			detalhe = detalhe[len(detalhe)-300:]
		}
		if detalhe == "" {
			detalhe = err.Error()
		}
		return "Não consegui falar com o Hermes: " + detalhe
	}
	if resp == "" {
		return "(o Hermes não devolveu texto)"
	}
	return resp
}

// ChatIntegrado — o chat está ligado ao Hermes? (o frontend mostra isso na tela)
func (a *App) ChatIntegrado() string {
	exe := caminhoHermes()
	ok := exe != ""
	b, _ := json.Marshal(map[string]any{"ok": ok, "hermes": exe, "sessao": sessaoChat,
		"erro": map[bool]string{true: "", false: "hermes não encontrado"}[ok]})
	return string(b)
}
