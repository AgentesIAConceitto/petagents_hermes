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
	ponteLinhas chan string // stdout da ponte, linha a linha — de UM leitor só (ligaPonte)
	ponteMu     sync.Mutex  // uma pergunta por vez
	ponteSeq    int         // id da pergunta em andamento (só muda com ponteMu)
	ponteUma    sync.Once

	// escritos pelo leitor da ponte e lidos a cada pergunta: sempre com a trava
	ponteEstadoMu sync.Mutex
	ponteErro     string
	pontePronta   bool

	// prazoPonte — quanto o app espera cada linha da ponte (variável só para o teste
	// encurtar; o porquê dos 330 s está em perguntaPonte).
	prazoPonte = 330 * time.Second
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
			marcaPonte(false, "ponte indisponível (script ou python não encontrados)")
			return
		}
		cmd := exec.Command(py, script)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
		}
		in, err := cmd.StdinPipe()
		if err != nil {
			marcaPonte(false, "stdin "+err.Error())
			return
		}
		var out io.ReadCloser
		if out, err = cmd.StdoutPipe(); err != nil {
			marcaPonte(false, "stdout "+err.Error())
			return
		}
		// o stderr da ponte vai para arquivo: quando ela morria calada (02/10/2026)
		// eu não tinha como saber por quê. ⚠️ Tem de ser ANTES do Start: o exec só
		// olha cmd.Stderr ao iniciar (antes era depois, e o arquivo nunca recebia nada).
		arq := filepath.Join(filepath.Dir(script), "ponte_erro.log")
		if f, errArq := os.OpenFile(arq, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); errArq == nil {
			cmd.Stderr = f
		}
		if err = cmd.Start(); err != nil {
			marcaPonte(false, "start "+err.Error())
			return
		}
		logMsg("ponte: chat quente ligado (hermes acp via chat_bridge.py)")
		ligaPonte(in, out)
	})
}

func estadoPonte() (pronta bool, erro string) {
	ponteEstadoMu.Lock()
	defer ponteEstadoMu.Unlock()
	return pontePronta, ponteErro
}

func marcaPonte(pronta bool, erro string) {
	ponteEstadoMu.Lock()
	pontePronta, ponteErro = pronta, erro
	ponteEstadoMu.Unlock()
	if erro != "" {
		logMsg("ponte: " + erro)
	}
}

// ligaPonte — passa a conversar com a ponte por estes canos.
//
// UM LEITOR SÓ para o stdout da ponte. Antes, cada pergunta criava a própria goroutine
// de leitura e, quando o prazo estourava, ela ficava ÓRFÃ lendo o mesmo bufio.Reader
// que a da pergunta seguinte. A órfã comia uma linha (um progresso, por exemplo) e a
// resposta ATRASADA da pergunta anterior era entregue como resposta da nova — o dono
// perguntava B e lia a resposta de A (ver ponte_test.go). Agora só esta goroutine lê;
// cada pergunta leva um id e perguntaPonte descarta o que for de outra.
func ligaPonte(in io.WriteCloser, out io.Reader) {
	linhas := make(chan string, 256)
	ponteMu.Lock()
	ponteIn, ponteLinhas = in, linhas
	ponteMu.Unlock()
	go func() {
		defer close(linhas)
		r := bufio.NewReaderSize(out, 1<<20)
		// espera o handshake: a ponte só vale quando diz "pronto: true"
		linha, err := r.ReadString('\n')
		if err != nil {
			marcaPonte(false, "ponte morreu antes do handshake: "+err.Error())
			return
		}
		var d struct {
			Pronto bool   `json:"pronto"`
			Erro   string `json:"erro"`
		}
		if json.Unmarshal([]byte(linha), &d) != nil {
			marcaPonte(false, "handshake ilegível: "+strings.TrimSpace(linha))
			return
		}
		if !d.Pronto {
			marcaPonte(false, "ponte sem agente: "+d.Erro)
			return
		}
		marcaPonte(true, "")
		logMsg("ponte: agente quente confirmado (handshake ok)")
		for {
			linha, err := r.ReadString('\n')
			if strings.TrimSpace(linha) != "" {
				linhas <- linha
			}
			if err != nil {
				marcaPonte(false, "ponte encerrou: "+err.Error())
				return
			}
		}
	}()
}

// perguntaPonte — manda uma pergunta e espera a resposta da ponte.
//
// ok=true: a ponte cuidou da pergunta — o texto é a resposta OU um aviso para a tela.
// ok=false: a ponte está fora do ar e vale a reserva lenta. Se a ponte está de pé e só
// demorou, NÃO é caso de reserva: ela segue trabalhando na pergunta e a reserva repetiria
// a mesma pergunta, gastando créditos em dobro (13/10/2026).
func (a *App) perguntaPonte(texto string) (string, bool) {
	if pronta, erro := estadoPonte(); !pronta {
		if erro != "" {
			logMsg("ponte: indisponível (" + erro + ") — usando a reserva lenta")
		} else {
			logMsg("ponte: ainda não confirmada — usando a reserva lenta")
		}
		return "", false
	}
	ponteMu.Lock()
	defer ponteMu.Unlock()

	ponteSeq++
	id := ponteSeq
	pedido, _ := json.Marshal(map[string]any{"id": id, "prompt": texto})
	if _, err := ponteIn.Write(append(pedido, '\n')); err != nil {
		logMsg("ponte: falha ao escrever (" + err.Error() + ") — reserva lenta")
		return "", false
	}
	t0 := time.Now()
	// lê até achar uma RESPOSTA de verdade: a ponte manda um frame de handshake
	// antes, e um frame extra não pode ser confundido com resposta (02/10/2026)
	for tentativa := 0; tentativa < 4; tentativa++ {
		select {
		case linha, aberta := <-ponteLinhas:
			if !aberta {
				logMsg("ponte: encerrou no meio da pergunta — reserva lenta")
				return "", false
			}
			var d struct {
				ID        int     `json:"id"` // eco do id do pedido (ponte antiga não manda: 0)
				OK        bool    `json:"ok"`
				Pronto    bool    `json:"pronto"`
				Resposta  string  `json:"resposta"`
				Progresso string  `json:"progresso"` // "texto" ou "passo" — NÃO é a resposta
				Texto     string  `json:"texto"`
				Erro      string  `json:"erro"`
				Segundos  float64 `json:"segundos"`
			}
			if err := json.Unmarshal([]byte(linha), &d); err != nil {
				txt := strings.TrimSpace(linha)
				if txt == "" {
					continue
				}
				logMsg("ponte: linha fora do formato — usando como texto")
				return txt, true
			}
			// linha de OUTRA pergunta: a que estourou o prazo e que a ponte só terminou
			// agora. Não é desta e não gasta tentativa.
			if d.ID != 0 && d.ID != id {
				if d.Progresso == "" {
					logMsg(fmt.Sprintf("ponte: resposta atrasada da pergunta %d descartada", d.ID))
				}
				tentativa--
				continue
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
		case <-time.After(prazoPonte):
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
			//
			// SEM RESERVA AQUI (13/10/2026, para não queimar créditos): estourar o prazo não
			// para a ponte — o agente segue trabalhando na pergunta (até o teto de iterações).
			// Cair na reserva rodava a MESMA pergunta num segundo agente, as duas gastando ao
			// mesmo tempo. Agora o boneco avisa e a pessoa decide se pergunta de novo.
			logMsg("ponte: tempo esgotado (330s) — sem reserva (a ponte segue na pergunta)")
			return msgPonteDemorou, true
		}
	}
	// ponte de pé, só confusa: mesma regra do tempo esgotado — nada de repetir a pergunta
	logMsg("ponte: frames demais sem resposta — sem reserva")
	return msgPonteDemorou, true
}

const msgPonteDemorou = "A resposta está demorando demais — parei de esperar para não gastar " +
	"créditos em dobro (o Hermes ainda pode estar terminando). Tente de novo daqui a pouco."
