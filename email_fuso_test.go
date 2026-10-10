package main

import (
	"testing"
	"time"
)

// O poll grava o carimbo com (Get-Date).ToString("s") — hora LOCAL, SEM fuso.
// Se o app interpretar como UTC (o que `time.Parse` faz quando o layout não tem
// zona), a idade sai inflada pelo offset local e o e-mail fica MUDO.
// Medido em 05/10/2026: com o Windows no Pacífico o log acusava
// "leitura VELHA (420 min)" = 7h de offset + 0 de idade real.
func TestIdadeLeituraComCarimboLocal(t *testing.T) {
	agora := time.Now().Format("2006-01-02T15:04:05")
	if idade := idadeLeitura(agora); idade > time.Minute {
		t.Fatalf("carimbo de AGORA deu idade %v (esperado < 1min): o fuso foi perdido na leitura", idade)
	}

	meiaHora := time.Now().Add(-30 * time.Minute).Format("2006-01-02T15:04:05")
	if idade := idadeLeitura(meiaHora); idade < 25*time.Minute || idade > 35*time.Minute {
		t.Fatalf("carimbo de 30min atras deu %v (esperado ~30min)", idade)
	}

	if idade := idadeLeitura("isso nao e uma data"); idade != 24*time.Hour {
		t.Fatalf("data ilegivel devia contar como 24h, deu %v", idade)
	}
	if idade := idadeLeitura(""); idade != 24*time.Hour {
		t.Fatalf("carimbo vazio devia contar como 24h, deu %v", idade)
	}
}

// ehNova usa o mesmo carimbo sem fuso: um e-mail lido que chegou ha 10 min
// PRECISA contar como novo (é a regra dos 45 min).
func TestEhNovaNaoInflacionaFuso(t *testing.T) {
	m := CabecalhoEmail{
		NaoLida: false,
		Quando:  time.Now().Add(-10 * time.Minute).Format("2006-01-02T15:04:05"),
	}
	if !ehNova(m) {
		t.Fatal("e-mail LIDO que chegou ha 10 min devia contar como novo: o fuso inflou a idade")
	}

	antigo := CabecalhoEmail{
		NaoLida: false,
		Quando:  time.Now().Add(-3 * time.Hour).Format("2006-01-02T15:04:05"),
	}
	if ehNova(antigo) {
		t.Fatal("e-mail LIDO de 3h atras NAO devia contar como novo")
	}
}
