package main

import (
	"testing"
	"time"
)

// TestEsconderPorVoltaSozinho — o relógio do "esconder N min" é do lado Go de
// propósito: com a janela escondida, os timers do WebView2 podem ser estrangulados e
// o boneco não voltaria na hora certa. Este teste prova que ele voltа sozinho.
//
// Usa o menor valor aceito pela API (1 min) e ctx nil — sem o runtime do Wails, o
// WindowShow é pulado, então o que se mede aqui é o RELÓGIO e o flag `hidden`. O
// WindowShow em si é medido no app real (bandeja "Mostrar bonecos" / volta do tempo).
func TestEsconderPorVoltaSozinho(t *testing.T) {
	a := NewApp() // ctx nil de propósito
	a.hidden = true
	ini := time.Now()

	if s := a.EsconderPor(1); s == "" {
		t.Fatal("EsconderPor(1) devolveu vazio")
	}
	a.mu.Lock()
	escondido := a.hidden
	a.mu.Unlock()
	if !escondido {
		t.Fatal("EsconderPor(1) não marcou hidden = true")
	}

	limite := time.Now().Add(80 * time.Second)
	for time.Now().Before(limite) {
		a.mu.Lock()
		h := a.hidden
		a.mu.Unlock()
		if !h {
			t.Logf("voltou sozinho em %.1fs (esperado ~60s)", time.Since(ini).Seconds())
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("não voltou sozinho em 80s")
}

// TestEsconderPorTimerAntigoNaoReexibe — BUG MEDIDO em 03/10/2026 (relatado pelo
// dono): cada clique em "Esconder 15min" criava um timer PRÓPRIO, e o MAIS ANTIGO
// disparava depois, reexibindo a janela no meio da captura dele.
// Log real: 13:21 escondida 15min · 13:35 escondida 15min · 13:36 "de volta"
// (o timer do primeiro clique venceu no meio do teste do terceiro).
//
// Roteiro aqui: esconde 1 min; 5 s depois esconde 1 min de novo (o mais novo manda).
//
//	t≈60s → o timer ANTIGO vence: tem de ser DESCARTADO (janela segue escondida)
//	t≈65s → o timer NOVO vence: aí sim volta
func TestEsconderPorTimerAntigoNaoReexibe(t *testing.T) {
	a := NewApp()    // ctx nil: mede o relógio e o flag, não o WindowShow
	a.EsconderPor(1) // timer A vence em t≈60s
	time.Sleep(5 * time.Second)
	a.EsconderPor(1) // timer B (mais novo) vence em t≈65s — ele manda

	// ⚠️ cronômetro: medir ENTRE os dois vencimentos. Antes eu dormia 5+62=67s, ou
	// seja DEPOIS de B vencer, e o teste acusava bug onde não havia (lição).
	time.Sleep(58 * time.Second) // t≈63s: A já venceu, B ainda não
	a.mu.Lock()
	h := a.hidden
	a.mu.Unlock()
	if !h {
		t.Fatal("a janela voltou pelo timer ANTIGO com o esconder novo ainda valendo (bug de volta)")
	}
	t.Log("t≈63s: segue escondida — timer antigo descartado OK")

	time.Sleep(10 * time.Second) // t≈73s: B já venceu
	a.mu.Lock()
	h = a.hidden
	a.mu.Unlock()
	if h {
		t.Fatal("não voltou depois do esconder mais novo vencer")
	}
	t.Log("t≈73s: voltou pelo timer novo OK")
}
