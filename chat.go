package main

// Chat do boneco com o HERMES de verdade.
//
// Caminho normal: a ponte quente (ponte.go + chat_bridge.py). A RESERVA, só com a ponte
// fora do ar, roda (13/10/2026):
//
//	hermes chat -Q --query=texto --max-turns 20 --continue petagent_chat --create-if-missing
//
// * `chat -Q` é o modo de uma pergunta só: roda o agente completo e imprime SÓ a
//   resposta no stdout (o session_id vai para o stderr);
// * `--max-turns` é o teto de iterações da IA. Antes era `hermes -z`, que monta o agente
//   SEM limite (sys.maxsize): um agente em loop só parava no timeout de 5 min, gastando;
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
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	sessaoChat    = "petagent_chat" // sessão do Hermes usada pelo chat do boneco
	tempoResposta = 300 * time.Second
	// maxIteracoesIA — teto de voltas do agente (modelo → ferramenta → modelo) por
	// mensagem, para um loop não queimar os créditos de uma vez. O mesmo número está em
	// MAX_ITERACOES_IA no chat_bridge.py (caminho quente) — os dois andam juntos.
	// ⚠️ NÃO usar 0: no Hermes 0 é ILIMITADO.
	maxIteracoesIA = 20
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
// Reserva: `hermes chat -Q`, que sobe um agente novo do zero a cada mensagem (~30 s).
func (a *App) chamarHermes(texto string) string {
	if resp, ok := a.perguntaPonte(texto); ok {
		return resp
	}
	return a.chamarHermesFrio(texto)
}

// argsReserva — argumentos do `hermes` na reserva lenta, com o teto de iterações.
// `--query=` vai colado: um texto que começa com "-" viraria flag no argparse.
func argsReserva(texto string) []string {
	return []string{"chat", "-Q", "--query=" + texto,
		"--max-turns", strconv.Itoa(maxIteracoesIA),
		"--continue", sessaoChat, "--create-if-missing"}
}

// chamarHermesFrio — reserva (lenta). Só usada com a ponte fora do ar: se a ponte só
// demorou, ela segue trabalhando e a reserva repetiria a pergunta (ver perguntaPonte).
func (a *App) chamarHermesFrio(texto string) string {
	exe := caminhoHermes()
	if exe == "" {
		return "Não encontrei o Hermes nesta máquina — sem ele eu não consigo conversar de verdade."
	}
	ctx, cancel := context.WithTimeout(context.Background(), tempoResposta)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, argsReserva(texto)...)
	// IA só do boneco: a reserva usa o home espelhado (que a ponte monta com o modelo e a
	// chave do boneco) — senão responderia com a IA do Hermes do dono
	if iaAgora().Modo == "boneco" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			cmd.Env = append(os.Environ(), "HERMES_HOME="+filepath.Join(base, "petagent", "hermes-home"))
		}
	}
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
	logMsg(fmt.Sprintf("chat: reserva (hermes chat -Q) respondeu %d chars (err=%v)", len(resp), err != nil))
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

// InfoChat — o que o cabeçalho do chat mostra: o modelo em uso (vem da ponte; vazio
// enquanto ela não subiu), o saldo já formatado ("US$ 4.82", ou o motivo de não ter) e se
// o agente quente está de pé (é ele que responde o chat — não o gateway).
func (a *App) InfoChat() string {
	a.mu.Lock()
	saldo := a.balance
	a.mu.Unlock()
	ponteEstadoMu.Lock()
	pronta, erro := pontePronta, ponteErro
	ponteEstadoMu.Unlock()
	ef := iaAgora()
	nome := ef.Provedor
	if p, ok := provedorPorID(ef.Provedor); ok {
		nome = p.Nome
	}
	modelo := modeloPonte()
	if modelo == "" {
		modelo = ef.Modelo // a ponte ainda não disse: o que está configurado
	}
	b, _ := json.Marshal(map[string]any{"modelo": modelo, "saldo": saldo, "pronta": pronta, "erro": erro,
		"provedor": nome, "modo": ef.Modo})
	return string(b)
}

// ChatIntegrado — o chat está ligado ao Hermes? (o frontend mostra isso na tela)
func (a *App) ChatIntegrado() string {
	exe := caminhoHermes()
	ok := exe != ""
	b, _ := json.Marshal(map[string]any{"ok": ok, "hermes": exe, "sessao": sessaoChat,
		"erro": map[bool]string{true: "", false: "hermes não encontrado"}[ok]})
	return string(b)
}
