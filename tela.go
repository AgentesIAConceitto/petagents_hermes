package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ── Monitores e ancoragem de borda ────────────────────────────────────
// A "linha acima da barra de tarefas" é o rcWork do monitor (já exclui a
// barra de tarefas). É nele que a janela se ancora.

type Monitor struct {
	Indice   int    `json:"indice"`
	Nome     string `json:"nome"`
	X        int32  `json:"x"`
	Y        int32  `json:"y"`
	W        int32  `json:"w"`
	H        int32  `json:"h"`
	Dpi      int    `json:"dpi"`
	Primario bool   `json:"primario"`
}

type TelaConfig struct {
	Monitor int    `json:"monitor"` // 1-based
	Borda   string `json:"borda"`   // baixo | cima | esquerda | direita
}

var (
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	shcore                  = syscall.NewLazyDLL("shcore.dll")
	procGetDpiForMonitor    = shcore.NewProc("GetDpiForMonitor")
)

// flags do SetWindowPos — só MOVER (sem mexer em tamanho, z-order ou foco)
const (
	swpNosize     = 0x0001
	swpNozorder   = 0x0004
	swpNoactivate = 0x0010
)

// moverJanela — põe a janela em (x,y) FÍSICOS pelo Win32.
// Não usar runtime.WindowSetPosition aqui: medido em 03/10/2026 que ele não cravava
// o alvo — a ida para a Tela 2 parou no DOBRO do X (7680 em vez de 3840) e a volta
// para a Tela 1 ficava na origem da Tela 2 (3840,0 com o tamanho da Tela 1), então a
// janela caía metade fora do desktop virtual e o boneco "desaparecia, não funcionava
// mais". SetWindowPos com SWP_NOSIZE é exato e não depende da escala/DPI.
func moverJanela(hwnd syscall.Handle, x, y int) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := procSetWindowPos.Call(uintptr(hwnd), 0,
		uintptr(uint32(int32(x))), uintptr(uint32(int32(y))), 0, 0,
		swpNosize|swpNozorder|swpNoactivate)
	return r != 0
}

// dpiDoMonitor — DPI efetivo daquele monitor (cada tela pode ter escala própria).
func dpiDoMonitor(hMon uintptr) int {
	// MDT_EFFECTIVE_DPI = 0
	var dx, dy uint32
	if r, _, _ := procGetDpiForMonitor.Call(hMon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx > 0 {
		return int(dx)
	}
	return systemDPI()
}

func listarMonitores() []Monitor {
	var out []Monitor
	type minfoex struct {
		CbSize    uint32
		RcMonitor rect
		RcWork    rect
		DwFlags   uint32
		SzDevice  [32]uint16
	}
	cb := syscall.NewCallback(func(hMon, hdc, lprc, lparam uintptr) uintptr {
		var mi minfoex
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if r, _, _ := procGetMonitorInfo.Call(hMon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			out = append(out, Monitor{
				Indice:   len(out) + 1,
				Nome:     syscall.UTF16ToString(mi.SzDevice[:]),
				X:        mi.RcWork.L,
				Y:        mi.RcWork.T,
				W:        mi.RcWork.R - mi.RcWork.L,
				H:        mi.RcWork.B - mi.RcWork.T,
				Dpi:      dpiDoMonitor(hMon),
				Primario: mi.DwFlags&1 != 0,
			})
		}
		return 1
	})
	procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return out
}

// ListarMonitores — o frontend usa isso para montar o seletor de tela.
func (a *App) ListarMonitores() string {
	b, _ := json.Marshal(listarMonitores())
	return string(b)
}

// TelaAtual — o frontend lê a escolha salva no boot (evita corrida com o evento).
func (a *App) TelaAtual() string {
	c := leTela()
	if c.Monitor == 0 {
		c = TelaConfig{Monitor: 1, Borda: "baixo"}
	}
	if c.Borda == "" {
		c.Borda = "baixo"
	}
	b, _ := json.Marshal(c)
	return string(b)
}

// tamanhoAtual — dimensões atuais da janela (DIP), para não redimensionar à toa.
var tamanhoAtual [2]int

// modoTeste — "cheio" (padrão) | "pequeno" | "barra". Só em "cheio" a janela
// é redimensionada para a área útil da tela (usado para isolar bugs de transparência).
var modoTeste = "cheio"

// tamanhoJanelaInicial — dimensões (DIP) da área útil da tela escolhida.
// Definir no STARTUP (e não redimensionar depois) é o que preserva a
// transparência do WebView2: redimensionar em runtime repinta a janela opaca.
func tamanhoJanelaInicial() (int, int) {
	cfg := leTela()
	ind := cfg.Monitor
	ms := listarMonitores()
	if len(ms) == 0 {
		return 1600, 900
	}
	if ind < 1 || ind > len(ms) {
		ind = 1
	}
	m := ms[ind-1]
	dpi := m.Dpi
	if dpi <= 0 {
		dpi = 96
	}
	es := float64(dpi) / 96.0
	return int(float64(m.W) / es), int(float64(m.H) / es)
}

// ConfigurarTela — a janela passa a COBRIR a área útil da tela escolhida, ou seja:
// o boneco fica SOLTO por toda a tela (não preso numa janelinha). A borda pedida
// é só a âncora/posição inicial dele.
func (a *App) ConfigurarTela(indice int, borda string) string {
	ms := listarMonitores()
	if indice < 1 || indice > len(ms) {
		return "monitor invalido"
	}
	m := ms[indice-1]
	if borda == "" {
		borda = "baixo"
	}

	dpi := m.Dpi
	if dpi <= 0 {
		dpi = 96
	}
	es := float64(dpi) / 96.0
	wDIP := int(float64(m.W) / es)
	hDIP := int(float64(m.H) / es)

	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	if a.ctx != nil && hwnd != 0 && modoTeste == "cheio" {
		// caminho normal: tamanho e posição em pixel FÍSICO pelo Win32 (ver encaixaNaArea)
		if encaixaNaArea(hwnd, m) {
			runtime.WindowSetBackgroundColour(a.ctx, 0, 0, 0, 0) // senão o WebView2 pinta branco
		}
		tamanhoAtual[0], tamanhoAtual[1] = wDIP, hDIP
		// mexer no tamanho faz o Windows reancorar a janela: reafirma logo depois
		go func() {
			time.Sleep(350 * time.Millisecond)
			if encaixaNaArea(hwnd, m) {
				runtime.WindowSetBackgroundColour(a.ctx, 0, 0, 0, 0)
			}
		}()
		runtime.EventsEmit(a.ctx, "tela-aplicada", map[string]interface{}{
			"borda": borda, "w": wDIP, "h": hDIP, "dpi": dpi, "monitor": indice,
		})
	} else if a.ctx != nil {
		// reserva (boot sem hwnd / modos de teste): o caminho antigo, em DIP pelo Wails.
		// Barra removida por Win32 → sem moldura. Posiciona de modo que o CLIENTE
		// caia exatamente na área útil: se o cliente não alcança o rodapé da
		// janela, empurra a janela para baixo o quanto falta.
		deltaY := 0
		if offCliente[2] > 0 {
			deltaY = int(m.H) - offCliente[2] // sobra entre o pé do cliente e o pé da janela
			if deltaY < 0 {
				deltaY = 0
			}
		}
		xAlvo := int(m.X) - offCliente[0]
		yAlvo := int(m.Y) - offCliente[1] + deltaY
		// posição pelo Win32 direto (ver moverJanela); o hwnd é relido a cada chamada
		// porque no boot ele ainda pode não existir
		mover := func() {
			a.mu.Lock()
			hw := a.hwnd
			a.mu.Unlock()
			if !moverJanela(hw, xAlvo, yAlvo) {
				runtime.WindowSetPosition(a.ctx, xAlvo, yAlvo) // reserva: boot sem hwnd
			}
		}
		// TAMANHO primeiro, POSIÇÃO depois — e a posição REAFIRMADA logo depois
		// (redimensionar faz o Windows reancorar a janela).
		// Só redimensiona se realmente mudou — e SEMPRE reafirma o fundo
		// transparente (senão o WebView2 pinta branco).
		if modoTeste == "cheio" && (wDIP != tamanhoAtual[0] || hDIP != tamanhoAtual[1]) {
			runtime.WindowSetSize(a.ctx, wDIP, hDIP)
			runtime.WindowSetBackgroundColour(a.ctx, 0, 0, 0, 0)
			tamanhoAtual[0], tamanhoAtual[1] = wDIP, hDIP
		}
		mover()
		go func() { time.Sleep(350 * time.Millisecond); mover() }()
		runtime.EventsEmit(a.ctx, "tela-aplicada", map[string]interface{}{
			"borda": borda, "w": wDIP, "h": hDIP, "dpi": dpi, "monitor": indice,
		})
	}
	gravaTela(TelaConfig{Monitor: indice, Borda: borda})
	logMsg(fmt.Sprintf("tela: monitor %d (%s) util=%dx%d em %d,%d dpi=%d (%dx%d DIP) | borda=%s",
		indice, m.Nome, m.W, m.H, m.X, m.Y, dpi, wDIP, hDIP, borda))
	// confere onde o cliente realmente caiu (o Windows pode cortar a altura)
	go func() {
		time.Sleep(1200 * time.Millisecond)
		a.mu.Lock()
		hw := a.hwnd
		a.mu.Unlock()
		if hw == 0 {
			return
		}
		cli := point{0, 0}
		procClientToScreen.Call(uintptr(hw), uintptr(unsafe.Pointer(&cli)))
		var wr, cr rect
		procGetWindowRect.Call(uintptr(hw), uintptr(unsafe.Pointer(&wr)))
		procGetClientRect.Call(uintptr(hw), uintptr(unsafe.Pointer(&cr)))
		cobre := cli.X == m.X && cli.Y == m.Y && cr.R == m.W && cr.B == m.H
		logMsg(fmt.Sprintf("medicao real: janela=(%d,%d)-(%d,%d) cliente=(%d,%d) %dx%d | area util %dx%d em %d,%d | cobre=%v",
			wr.L, wr.T, wr.R, wr.B, cli.X, cli.Y, cr.R, cr.B, m.W, m.H, m.X, m.Y, cobre))
	}()
	return "ok"
}

// encaixaNaArea — tamanho E posição exatos, em pixel FÍSICO, para o CLIENTE da
// janela cobrir a área útil `m`. Devolve se precisou mexer na janela.
//
// ⚠️ MEDIDO 13/10/2026 (log): na Tela 2 (2560x1032, 96 dpi) a janela ficava com 882 px
// de altura — o pé do boneco 150 px acima da barra de tarefas. O tamanho ia em DIP
// pelo Wails (WindowSetSize e o tamanho do startup), que converte pelo DPI do monitor
// ONDE A JANELA ESTÁ, a Tela 1 (120 dpi): 1032 × 1,25 = 1290 px físicos. O Windows corta
// no limite de arrasto (altura do desktop virtual + bordas = 1102) e, ao chegar na
// Tela 2, o WM_DPICHANGED escala por 96/120: 1102 × 0,8 = 882.
// Aqui não há conversão: a primeira passada já põe a janela inteira sobre o monitor
// (o Windows troca o DPI dela e o Wails aplica o retângulo sugerido, escalado); a
// segunda, já no DPI certo, crava o tamanho; a terceira só confere.
func encaixaNaArea(hwnd syscall.Handle, m Monitor) bool {
	mexeu := false
	for i := 0; i < 3; i++ {
		var wr, cr rect
		cli := point{0, 0}
		procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&wr)))
		procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&cr)))
		procClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&cli)))
		// moldura que sobrar em volta do cliente (com a barra removida, zero)
		esq, topo := cli.X-wr.L, cli.Y-wr.T
		dir, pe := wr.R-(cli.X+cr.R), wr.B-(cli.Y+cr.B)
		x, y := m.X-esq, m.Y-topo
		w, h := m.W+esq+dir, m.H+topo+pe
		if wr.L == x && wr.T == y && wr.R-wr.L == w && wr.B-wr.T == h {
			break
		}
		procSetWindowPos.Call(uintptr(hwnd), 0,
			uintptr(uint32(x)), uintptr(uint32(y)), uintptr(uint32(w)), uintptr(uint32(h)),
			swpNozorder|swpNoactivate)
		mexeu = true
		time.Sleep(120 * time.Millisecond) // deixa o WM_DPICHANGED ser atendido antes de medir de novo
	}
	return mexeu
}

// AplicarTelaSalva — chamado quando a janela já existe (senão não há o que posicionar).
// Sem configuração salva, o padrão é Tela 1 (primária) colada na linha inferior.
func (a *App) aplicarTelaSalva() {
	cfg := leTela()
	if cfg.Monitor == 0 {
		cfg = TelaConfig{Monitor: 1, Borda: "baixo"}
	}
	a.ConfigurarTela(cfg.Monitor, cfg.Borda)
}

func arquivoTela() string {
	d := filepath.Join(os.Getenv("LOCALAPPDATA"), "petagent")
	os.MkdirAll(d, 0o755)
	return filepath.Join(d, "config.json")
}

func gravaTela(c TelaConfig) {
	if b, err := json.Marshal(c); err == nil {
		os.WriteFile(arquivoTela(), b, 0o644)
	}
}

func leTela() TelaConfig {
	var c TelaConfig
	if b, err := os.ReadFile(arquivoTela()); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}
