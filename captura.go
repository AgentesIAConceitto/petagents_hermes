package main

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ── Esconder a janela enquanto a Ferramenta de Captura está aberta ─────────────
//
// PROBLEMA (relatado 03/10/2026, e que a exclusão de captura NÃO resolveu): a janela
// do boneco cobre a tela toda e é TOPMOST, então ela é sempre o "topo" da tela — a
// Ferramenta de Captura do Windows, mesmo em modo "janela", acabava capturando a
// tela inteira. `SetWindowDisplayAffinity` muda só o CONTEÚDO gravado, não a escolha
// da janela (que é por hit-test) — medido: continuou capturando tudo.
//
// SOLUÇÃO: enquanto houver janela visível de um processo de captura do Windows,
// escondemos a nossa; ao fechar o app, ela volta (SW_SHOWNA, sem roubar o foco).
// É o único caminho que faz o "capturar janela" enxergar a janela DE BAIXO.

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procIsIconic                   = user32.NewProc("IsIconic")
	procGetClassNameW              = user32.NewProc("GetClassNameW")
)

// ── overlays ALHEIOS de tela cheia ─────────────────────────────────────────────
//
// MEDIDO em 03/10/2026: o backend de chat do PetAgent sobe o MCP `cua-driver`
// (configurado no Hermes do dono, `mcp_servers.cua-driver`), e CADA instância abre uma
// janela `Cua.AgentCursorOverlay` de 3840x1440 — as DUAS telas. É invisível, mas está
// presente: qualquer ferramenta que capture ou grave UMA janela escolhe "a do topo" e
// acaba pegando tudo. O dono pediu tela limpa ao esconder; como não podemos mexer no
// Hermes dele, o PetAgent esconde também esses overlays alheios durante o modo
// captura limpa (e devolve depois, para não bagunçar o estado do driver).
var (
	cbOverlayUma     sync.Once
	cbOverlayPtr     uintptr
	overlayAcao      uintptr
	overlayFeitos    int
	overlayUltimoLog int
)

// procTerminateProcess — para MATAR de vez o processo que desenha o overlay.
//
// MEDIDO 03/10/2026: esconder a janela não segura — o `cua-driver` reafirma os
// overlays dele (5 escondidos, 3 de volta em 7 s). Matar o processo vence. E não é
// preciso restaurar nada: medido que, mortos todos, eles NÃO ressuscitam sozinhos
// (60 s parado = 0), e UMA chamada de computer-use faz nascer instância nova com o
// overlay — ou seja, quem precisa deles os recria sozinho, de forma transparente.
var procTerminateProcess = kernel32.NewProc("TerminateProcess")

var (
	cbMataUma sync.Once
	cbMataPtr uintptr
	mortos    int
)

// mataOverlaysAlheios — mata os processos donos de janelas `...AgentCursorOverlay`.
// Vai pelo dono da JANELA (não pela lista de processos), assim só morre quem está
// desenhando overlay de verdade. Devolve quantos.
func mataOverlaysAlheios() int {
	cbMataUma.Do(func() {
		cbMataPtr = syscall.NewCallback(func(hwnd syscall.Handle, _ uintptr) uintptr {
			buf := make([]uint16, 128)
			n, _, _ := procGetClassNameW.Call(uintptr(hwnd),
				uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			if n == 0 {
				return 1
			}
			if !strings.Contains(strings.ToLower(syscall.UTF16ToString(buf[:n])),
				"agentcursoroverlay") {
				return 1
			}
			var pid uint32
			procGetWindowThreadPID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
			if pid == 0 {
				return 1
			}
			const processTerminate = 0x0001
			h, _, _ := procOpenProcess.Call(processTerminate, 0, uintptr(pid))
			if h == 0 {
				return 1
			}
			procTerminateProcess.Call(h, 1)
			procCloseHandle.Call(h)
			mortos++
			return 1
		})
	})
	mortos = 0
	procEnumWindows.Call(cbMataPtr, 0)
	return mortos
}

// operarOverlaysAlheios — esconde (mostrar=false) ou devolve (true) os overlays de
// cursor de outras ferramentas. Devolve quantas janelas mexeu.
func operarOverlaysAlheios(mostrar bool) int {
	cbOverlayUma.Do(func() {
		cbOverlayPtr = syscall.NewCallback(func(hwnd syscall.Handle, _ uintptr) uintptr {
			buf := make([]uint16, 128)
			n, _, _ := procGetClassNameW.Call(uintptr(hwnd),
				uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			if n == 0 {
				return 1
			}
			cls := strings.ToLower(syscall.UTF16ToString(buf[:n]))
			if strings.Contains(cls, "agentcursoroverlay") {
				procShowWindow.Call(uintptr(hwnd), overlayAcao)
				overlayFeitos++
			}
			return 1
		})
	})
	const (
		swHideOp   = 0
		swShowNaOp = 8
	)
	if mostrar {
		overlayAcao = swShowNaOp
	} else {
		overlayAcao = swHideOp
	}
	overlayFeitos = 0
	procEnumWindows.Call(cbOverlayPtr, 0)
	// log só quando MUDA (esta função roda a cada 400 ms no modo captura)
	if overlayFeitos != overlayUltimoLog {
		if overlayFeitos > 0 {
			verbo := "escondidos"
			if mostrar {
				verbo = "devolvidos"
			}
			logMsg(fmt.Sprintf("captura: %d overlay(s) alheio(s) %s", overlayFeitos, verbo))
		}
		overlayUltimoLog = overlayFeitos
	}
	return overlayFeitos
}

// processos de captura do Windows 10/11 — Win+Shift+S é o ScreenClippingHost
var alvosCaptura = []string{"snippingtool.exe", "screensketch.exe", "screenclippinghost.exe"}

var (
	cbCapturaUma      sync.Once // callback criado UMA vez (criar a cada volta vaza)
	cbEnumJanelas     uintptr
	achouCaptura      bool
	nomeCapturaAchado string
)

// nomeDoProcessoDaJanela — nome do .exe dono daquela janela.
func nomeDoProcessoDaJanela(hwnd syscall.Handle) string {
	var pid uint32
	procGetWindowThreadPID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	const processQueryLimitedInformation = 0x1000
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 512)
	n := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return strings.ToLower(syscall.UTF16ToString(buf[:n]))
}

// capturaAberta — algum processo de captura tem janela visível na tela?
func capturaAberta() (bool, string) {
	cbCapturaUma.Do(func() {
		cbEnumJanelas = syscall.NewCallback(func(hwnd syscall.Handle, _ uintptr) uintptr {
			if v, _, _ := procIsWindowVisible.Call(uintptr(hwnd)); v == 0 {
				return 1
			}
			// minimizada não conta: o dono que deixa a Ferramenta de Captura aberta e
			// minimizada não está capturando — e não faz sentido o boneco sumir à toa
			if v, _, _ := procIsIconic.Call(uintptr(hwnd)); v != 0 {
				return 1
			}
			nome := nomeDoProcessoDaJanela(hwnd)
			for _, alvo := range alvosCaptura {
				if strings.HasSuffix(nome, alvo) {
					achouCaptura = true
					nomeCapturaAchado = nome
					return 0 // achou: para de enumerar
				}
			}
			return 1
		})
	})
	achouCaptura = false
	nomeCapturaAchado = ""
	procEnumWindows.Call(cbEnumJanelas, 0)
	return achouCaptura, nomeCapturaAchado
}

// escondeDuranteCaptura — esconde/mostra a janela conforme a Ferramenta de Captura
// entra e sai. Sem detecção, nada muda (a janela fica onde está).
func (a *App) escondeDuranteCaptura() {
	const (
		swHide   = 0
		swShowNa = 8 // mostra sem ativar/roubar foco
	)
	time.Sleep(3 * time.Second) // deixa a janela subir antes de mexer nela
	escondido := false
	voltas := 0
	for {
		time.Sleep(400 * time.Millisecond)
		voltas++
		a.mu.Lock()
		hwnd := a.hwnd
		a.mu.Unlock()
		if hwnd == 0 {
			continue
		}
		aberta, nome := capturaAberta()
		a.mu.Lock()
		pedidoDoDono := a.hidden
		a.mu.Unlock()
		precisaLimpar := aberta || pedidoDoDono // tela limpa de verdade, para capturar
		if precisaLimpar {
			if !escondido {
				procShowWindow.Call(uintptr(hwnd), swHide)
				escondido = true
				motivo := "app de captura"
				if !aberta {
					motivo = "esconder pedido pelo dono"
				}
				extra := ""
				if aberta {
					extra = " (" + nome + ")"
				}
				logMsg("captura: " + motivo + " -> janela escondida" + extra)
			}
			// ⚠️ sustentar: o cua-driver REAFIRMA os overlays dele (medido: 5
			// escondidos, 3 voltaram em 7 s). Esconder de vez em quando não basta:
			// a cada ~2 s mata quem estiver desenhando overlay (dono aprovou o
			// custo: pode interromper uma sessão de computer_use em andamento).
			operarOverlaysAlheios(false)
			if voltas%5 == 0 {
				if n := mataOverlaysAlheios(); n > 0 {
					logMsg(fmt.Sprintf("captura: %d overlay(s) alheio(s) MORTOS (esconder não segura)", n))
				}
			}
		} else if escondido {
			procShowWindow.Call(uintptr(hwnd), swShowNa)
			operarOverlaysAlheios(true) // devolve o estado dos overlays alheios
			escondido = false
			logMsg("captura: fim do modo captura -> janela do boneco de volta")
		}
	}
}
