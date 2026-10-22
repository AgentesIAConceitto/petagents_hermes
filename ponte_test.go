package main

import (
	"bufio"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// TestPonteNaoTrocaRespostas — a resposta ATRASADA de uma pergunta não pode virar a
// resposta da pergunta seguinte.
//
// Cenário real: a pergunta 1 estoura o prazo do app (que cai na reserva lenta), mas a
// ponte continua trabalhando nela e manda progresso e, depois, a resposta. Quando o
// dono faz a pergunta 2, a primeira linha "de resposta" que chega é a da pergunta 1.
//
// Aqui a ponte é de mentira (dois io.Pipe) e o prazo é encurtado para 300 ms.
func TestPonteNaoTrocaRespostas(t *testing.T) {
	prazoAntes := prazoPonte
	prazoPonte = 300 * time.Millisecond
	defer func() { prazoPonte = prazoAntes }()

	pedidosR, pedidosW := io.Pipe() // app -> stdin da ponte
	saidaR, saidaW := io.Pipe()     // stdout da ponte -> app
	defer pedidosW.Close()
	defer saidaW.Close()
	ligaPonte(pedidosW, saidaR)

	pedidos := bufio.NewReader(pedidosR)
	escreve := func(v map[string]any) {
		b, _ := json.Marshal(v)
		_, _ = saidaW.Write(append(b, '\n'))
	}
	leID := func() any { // a ponte recebe um pedido e devolve o id dele (se houver)
		l, err := pedidos.ReadString('\n')
		if err != nil {
			t.Fatalf("a ponte não recebeu o pedido: %v", err)
		}
		var p map[string]any
		_ = json.Unmarshal([]byte(l), &p)
		return p["id"]
	}

	escreve(map[string]any{"ok": true, "pronto": true})
	for fim := time.Now().Add(2 * time.Second); ; {
		if pronta, _ := estadoPonte(); pronta {
			break
		}
		if time.Now().After(fim) {
			t.Fatal("handshake não foi aceito")
		}
		time.Sleep(10 * time.Millisecond)
	}

	a := NewApp()

	// pergunta 1: a ponte demora mais que o prazo -> o app desiste
	feito1 := make(chan bool)
	go func() { _, ok := a.perguntaPonte("pergunta 1"); feito1 <- ok }()
	id1 := leID()
	if ok := <-feito1; ok {
		t.Fatal("a pergunta 1 deveria ter estourado o prazo")
	}
	// ...mas a ponte segue trabalhando nela e manda progresso
	escreve(map[string]any{"ok": true, "progresso": "passo", "texto": "terminal", "id": id1})

	// pergunta 2: a resposta atrasada da 1 chega antes da resposta da 2
	feito2 := make(chan string)
	go func() { r, _ := a.perguntaPonte("pergunta 2"); feito2 <- r }()
	id2 := leID()
	go func() {
		escreve(map[string]any{"ok": true, "id": id1, "resposta": "resposta da pergunta 1"})
		escreve(map[string]any{"ok": true, "id": id2, "resposta": "resposta da pergunta 2"})
	}()

	select {
	case r := <-feito2:
		if r != "resposta da pergunta 2" {
			t.Fatalf("pergunta 2 recebeu %q — resposta trocada", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pergunta 2 não terminou")
	}
}
