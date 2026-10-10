package main

// Ponte QUENTE com o Hermes (o chat do boneco em ~3-8 s, não em ~30 s).
//
// Descoberta de 02/10/2026 (medida):
//   * `hermes -z` sobe um agente novo por mensagem  -> ~27-86 s por resposta;
//   * `hermes acp` mantém UM agente vivo e fala JSON-RPC por stdio -> ~3,8 s.
//
// Então o app sobe UMA vez a ponte Python (chat_bridge.py), que segura o agente
// quente, e conversa com ela por stdin/stdout (1 JSON por linha):
//
//	app -> {"prompt": "texto"}
//	app <- {"ok": true, "resposta": "...", "segundos": 3.8}
//
// Se a ponte não estiver de pé, cai na reserva lenta (`hermes -z`) - nunca fica mudo.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sync"
	"syscall"
	"time"
)

var (
	ponteIn     io.WriteCloser
	ponteOut    *bufio.Reader
	ponteMu     sync.Mutex
	ponteErro   string
	pontePronta bool
	ponteUma    sync.Once
)

// caminhoPonte — o script da ponte, ao lado do executável do app.
func caminhoPonte() string {
	cands := []string{}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), "chat_bridge.py"))
	}
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		cands = append(cands, filepath.Join(base, "petagent", "chat_bridge.py"))
	}
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// caminhoPythonHermes — o python do venv do Hermes (tem tudo que a ponte usa).
func caminhoPythonHermes() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		p := filepath.Join(base, "hermes", "hermes-agent", "venv", "Scripts", "python.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("python"); err == nil {
		return p
	}
	return ""
}

// sobePonte — liga a ponte (uma vez só). O agente leva ~10 s para ficar pronto;
// isso acontece no boot do app, fora do caminho da conversa.
func (a *App) sobePonte() {
	ponteUma.Do(func() {
		script, py := caminhoPonte(), caminhoPythonHermes()
		if script == "" || py == "" {
			ponteErro = "ponte indisponível (script ou python não encontrados)"
			logMsg("ponte: " + ponteErro)
			return
		}
		cmd := exec.Command(py, script)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
		}
		var err error
		if ponteIn, err = cmd.StdinPipe(); err != nil {
			ponteErro = err.Error()
			logMsg("ponte: stdin " + err.Error())
			return
		}
		var out io.ReadCloser
		if out, err = cmd.StdoutPipe(); err != nil {
			ponteErro = err.Error()
			logMsg("ponte: stdout " + err.Error())
			return
		}
		cmd.Stderr = nil // a ponte já manda o diagnóstico dela pro stderr dela
		if err = cmd.Start(); err != nil {
			ponteErro = err.Error()
			logMsg("ponte: start " + err.Error())
			return
		}
		ponteOut = bufio.NewReaderSize(out, 1<<20)
		// o stderr da ponte vai para arquivo: quando ela morria calada (02/10/2026)
		// eu não tinha como saber por quê
		arq := filepath.Join(filepath.Dir(script), "ponte_erro.log")
		if f, errArq := os.OpenFile(arq, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); errArq == nil {
			cmd.Stderr = f
		}
		logMsg("ponte: chat quente ligado (hermes acp via chat_bridge.py)")
		// espera o handshake: a ponte só vale quando diz "pronto: true"
		go func() {
			linha, errLeitura := ponteOut.ReadString('\n')
			if errLeitura != nil {
				ponteErro = "ponte morreu antes do handshake: " + errLeitura.Error()
				logMsg("ponte: " + ponteErro)
				return
			}
			var d struct {
				Pronto bool   `json:"pronto"`
				Erro   string `json:"erro"`
			}
			if json.Unmarshal([]byte(linha), &d) != nil {
				ponteErro = "handshake ilegível: " + strings.TrimSpace(linha)
				logMsg("ponte: " + ponteErro)
				return
			}
			if !d.Pronto {
				ponteErro = "ponte sem agente: " + d.Erro
				logMsg("ponte: " + ponteErro)
				return
			}
			pontePronta = true
			logMsg("ponte: agente quente confirmado (handshake ok)")
		}()
	})
}

// perguntaPonte — manda uma pergunta e espera a resposta da ponte.
func (a *App) perguntaPonte(texto string) (string, bool) {
	if ponteIn == nil || ponteOut == nil || !pontePronta {
		if ponteErro != "" {
			logMsg("ponte: indisponível (" + ponteErro + ") — usando a reserva lenta")
		} else {
			logMsg("ponte: ainda não confirmada — usando a reserva lenta")
		}
		return "", false
	}
	ponteMu.Lock()
	defer ponteMu.Unlock()

	pedido, _ := json.Marshal(map[string]string{"prompt": texto})
	if _, err := ponteIn.Write(append(pedido, '\n')); err != nil {
		logMsg("ponte: falha ao escrever (" + err.Error() + ") — reserva lenta")
		return "", false
	}
	t0 := time.Now()
	type linhaRes struct {
		linha string
		err   error
	}
	// lê até achar uma RESPOSTA de verdade: a ponte manda um frame de handshake
	// antes, e um frame extra não pode ser confundido com resposta (02/10/2026)
	for tentativa := 0; tentativa < 4; tentativa++ {
		ch := make(chan linhaRes, 1)
		go func() {
			l, err := ponteOut.ReadString('\n')
			ch <- linhaRes{l, err}
		}()
		select {
		case r := <-ch:
			if r.err != nil {
				logMsg("ponte: leitura falhou (" + r.err.Error() + ") — reserva lenta")
				return "", false
			}
			var d struct {
				OK        bool    `json:"ok"`
				Pronto    bool    `json:"pronto"`
				Resposta  string  `json:"resposta"`
				Progresso string  `json:"progresso"` // "texto" ou "passo" — NÃO é a resposta
				Texto     string  `json:"texto"`
				Erro      string  `json:"erro"`
				Segundos  float64 `json:"segundos"`
			}
			if err := json.Unmarshal([]byte(r.linha), &d); err != nil {
				txt := strings.TrimSpace(r.linha)
				if txt == "" {
					continue
				}
				logMsg("ponte: linha fora do formato — usando como texto")
				return txt, true
			}
			// LINHA DE PROGRESSO (10/10/2026) — o dono: "eu não fico nunca no vácuo aqui".
			//
			// Medido: no Telegram cada passo do agente vira uma mensagem (11 ferramentas = 11
			// mensagens) e ele nunca fica esperando no escuro; aqui eram 9 ferramentas virando
			// UMA mensagem no fim — 224 s olhando um balão parado. A ponte agora emite o que
			// está acontecendo ENQUANTO acontece; isto repassa para a tela e NÃO consome
			// tentativa (a linha não é resposta).
			if d.Progresso != "" {
				if a != nil && a.ctx != nil {
					runtime.EventsEmit(a.ctx, "chat-progresso", map[string]string{
						"tipo": d.Progresso, "texto": d.Texto,
					})
				}
				tentativa-- // progresso não gasta tentativa: só as 4 leituras "de verdade" contam
				continue
			}
			if d.Pronto && !d.OK { // handshake antigo/atrasado: não é resposta
				continue
			}
			if d.Pronto && d.Resposta == "" {
				continue
			}
			if !d.OK {
				if d.Erro != "" {
					logMsg("ponte: erro do agente (" + d.Erro + ") — reserva lenta")
				}
				return "", false
			}
			logMsg(fmt.Sprintf("ponte: resposta quente em %.1fs", time.Since(t0).Seconds()))
			return d.Resposta, true
		case <-time.After(330 * time.Second):
			// O APP TEM DE ESPERAR MAIS QUE A PONTE.
			//
			// Defeito medido em 10/10/2026 (relato do dono: "fiz uma pergunta simples e ele levou
			// 205 segundos"): aqui o prazo era 150 s, mas `chat_bridge.py::perguntar` espera 300 s
			// e, se o canal quente cair, RECONECTA uma vez e repete. Com 150 s o app desistia
			// ANTES da ponte terminar, caía na reserva lenta (`hermes -z`) e a reconexão nunca
			// chegava a rodar. Somando: 150 s de espera + ~56 s da reserva = os 205 s medidos no
			// log real (10:33:12 "tempo esgotado (150s)" → 10:34:08 "reserva respondeu").
			//
			// Com 330 s a ponte tem a janela dela inteira: ou responde quente, ou reconecta
			// sozinha — e a reserva lenta volta a ser ÚLTIMO recurso em vez de caminho comum.
			logMsg("ponte: tempo esgotado (330s) — reserva lenta")
			return "", false
		}
	}
	logMsg("ponte: frames demais sem resposta — reserva lenta")
	return "", false
}
