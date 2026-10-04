package main

import (
	"fmt"
	"os"
	"syscall"
)

// ── Aparência da janela: fora da barra de tarefas e fora das capturas ──────────
//
// O PetAgent vive na BANDEJA, mas a janela dele (que cobre a tela toda, para o
// boneco andar por qualquer borda) se comportava como um programa normal:
//
//  1. botão na barra de tarefas — desnecessário, o app já está na bandeja;
//  2. pior: a captura de UMA janela no Windows parou de funcionar (só aceitava a
//     tela inteira), porque o "topo" da tela era sempre a nossa janela. Relatado
//     pelo dono em 03/10/2026.
//
// Nada disso mexe no desenho: é estilo estendido da janela + afinidade de captura.

const (
	wsExToolWindow        = 0x00000080 // some da barra de tarefas e do Alt+Tab
	wsExAppWindow         = 0x00040000 // ⚠️ FORÇA o botão na barra — e o Wails seta
	wdaExcludeFromCapture = 0x00000011 // some de TODA captura (Win10 2004+, 19041+)
)

var procSetWindowDisplayAffinity = user32.NewProc("SetWindowDisplayAffinity")

// foraDaBarraDeTarefas — marca a janela como TOOL WINDOW.
// SWP_FRAMECHANGED faz o Windows reavaliar os atributos sem precisar
// esconder/mostrar a janela (esconder/mostrar pisca e pode atrapalhar o WebView2).
func foraDaBarraDeTarefas(hwnd syscall.Handle) {
	if hwnd == 0 {
		return
	}
	const ( // flags locais: mesmos valores do SetWindowPos, nomes à parte
		swpNoMoveT       = 0x0002
		swpNoSizeT       = 0x0001
		swpNoZOrderT     = 0x0004
		swpNoActivateT   = 0x0010
		swpFrameChangedT = 0x0020
	)
	ex, _, _ := procGetWindowLongPtr.Call(uintptr(hwnd), gwlpIndex())
	// ⚠️ MEDIDO 03/10/2026: aplicar só o TOOLWINDOW não tirou o botão da barra — o
	// EXSTYLE ficou 0x2D00A8, com TOOLWINDOW **e** APPWINDOW juntos. APPWINDOW força
	// um top-level para a barra e GANHA do TOOLWINDOW. Tem que LIMPAR o APPWINDOW.
	novo := (ex | uintptr(wsExToolWindow)) &^ uintptr(wsExAppWindow)
	procSetWindowLongPtr.Call(uintptr(hwnd), gwlpIndex(), novo)
	procSetWindowPos.Call(uintptr(hwnd), 0, 0, 0, 0, 0,
		uintptr(swpFrameChangedT|swpNoMoveT|swpNoSizeT|swpNoZOrderT|swpNoActivateT))
	depois, _, _ := procGetWindowLongPtr.Call(uintptr(hwnd), gwlpIndex())
	logMsg(fmt.Sprintf("janela: exstyle 0x%X -> 0x%X (TOOLWINDOW posto, APPWINDOW limpo)", ex, depois))
}

// foraDoPrint — SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE): a janela deixa de
// aparecer em QUALQUER captura.
//
// ⚠️ MEDIDO 03/10/2026: isso NÃO resolveu o "só captura a tela inteira". Exclusão de
// captura muda só o CONTEÚDO capturado; a escolha de janela continua sendo por
// hit-test, e a nossa janela (topmost, cobrindo a tela toda) segue sendo o "topo" —
// então capturar "a janela" devolvia o desktop inteiro. Pior: com a exclusão, o
// boneco também sumia dos prints do dono, sem ganho.
// Solução de verdade: `escondeDuranteCaptura()` (captura.go), que esconde a janela
// enquanto a Ferramenta de Captura está aberta. Ficou OPT-IN: só aplica com
// PETAGENT_PRINT_INVISIVEL=1.
func foraDoPrint(hwnd syscall.Handle) bool {
	if hwnd == 0 {
		return false
	}
	if os.Getenv("PETAGENT_PRINT_INVISIVEL") == "" {
		return false
	}
	r, _, err := procSetWindowDisplayAffinity.Call(uintptr(hwnd), wdaExcludeFromCapture)
	if r == 0 {
		// Windows antigo (pré-2004) não conhece o modo e pode cair no WDA_MONITOR,
		// que pinta a janela de PRETO nos prints — pior que o problema. Se falhou,
		// ficamos como está e o log diz por quê.
		logMsg(fmt.Sprintf("janela: nao consegui excluir das capturas (%v) - segue aparecendo em print", err))
		return false
	}
	logMsg("janela: excluida das capturas (WDA_EXCLUDEFROMCAPTURE)")
	return true
}
