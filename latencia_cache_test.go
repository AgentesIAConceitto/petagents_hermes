package main

// latencia_cache_test.go — o app escolhe provedor por medição de agora, e não repete
// resposta que já tem. (03/11/2026)

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOrdenaPorLatenciaMedida(t *testing.T) {
	LimparMedicoes()
	defer LimparMedicoes()

	registrarLatencia("gemini#1", 19*time.Second, nil)
	registrarLatencia("nvidia#1", 1500*time.Millisecond, nil)

	elos := []ProvedorEscolhido{
		{Rotulo: "gemini#1", Modelo: "m"},
		{Rotulo: "nvidia#1", Modelo: "m"},
	}
	ordenados := ordenarPorLatencia(elos)
	if ordenados[0].Rotulo != "nvidia#1" {
		t.Fatalf("o mais rápido deveria vir primeiro, veio %s", ordenados[0].Rotulo)
	}
	if ordenados[1].Rotulo != "gemini#1" {
		t.Fatalf("ordem errada: %v", ordenados)
	}
}

func TestNaoMedidoNaoEhExcluido(t *testing.T) {
	LimparMedicoes()
	defer LimparMedicoes()
	registrarLatencia("gemini#1", 19*time.Second, nil)

	elos := []ProvedorEscolhido{
		{Rotulo: "gemini#1", Modelo: "m"},
		{Rotulo: "nvidia#1", Modelo: "m"}, // sem medição
	}
	ordenados := ordenarPorLatencia(elos)
	if len(ordenados) != 2 {
		t.Fatalf("ninguém pode ser excluído por falta de medição: %v", ordenados)
	}
	if ordenados[0].Rotulo != "gemini#1" {
		t.Fatalf("o medido vem antes do não medido, veio %v", ordenados)
	}
}

func TestProvedorQueFalhouVaiParaOFim(t *testing.T) {
	LimparMedicoes()
	defer LimparMedicoes()
	registrarLatencia("ruim#1", time.Second, errors.New("HTTP 500"))
	registrarLatencia("bom#1", 8*time.Second, nil)

	ordenados := ordenarPorLatencia([]ProvedorEscolhido{
		{Rotulo: "ruim#1", Modelo: "m"},
		{Rotulo: "bom#1", Modelo: "m"},
	})
	if ordenados[0].Rotulo != "bom#1" {
		t.Fatalf("quem falhou deveria ir para o fim: %v", ordenados)
	}
}

func TestMedicaoVence(t *testing.T) {
	LimparMedicoes()
	defer LimparMedicoes()
	latMu.Lock()
	latCache["velho#1"] = medicaoLatencia{Quando: time.Now().Add(-2 * validadeMedicao), Duracao: time.Second}
	latMu.Unlock()

	if _, ok := latenciaDe("velho#1"); ok {
		t.Fatal("medição vencida não pode ser usada")
	}
}

func TestCacheGuardaRespostaDeLeitura(t *testing.T) {
	LimparCacheRespostas()
	defer LimparCacheRespostas()

	GuardarResposta("qual a cotação do dólar", "R$ 5,40", []string{"buscar_web"})
	if txt, ok := BuscarResposta("qual a cotacao do dolar  "); !ok || !strings.Contains(txt, "5,40") {
		t.Fatalf("deveria achar a resposta mesmo com a pergunta escrita diferente: %q %v", txt, ok)
	}
}

func TestCacheNaoGuardaQuemAlterouOMundo(t *testing.T) {
	LimparCacheRespostas()
	defer LimparCacheRespostas()

	GuardarResposta("crie a pasta x", "Criei a pasta x", []string{"criar_pasta"})
	if _, ok := BuscarResposta("crie a pasta x"); ok {
		t.Fatal("resposta que criou pasta NUNCA pode vir do cache")
	}
}

func TestCacheVence(t *testing.T) {
	LimparCacheRespostas()
	defer LimparCacheRespostas()
	GuardarResposta("pergunta", "resposta", nil)
	rcMu.Lock()
	e := rcMapa[chaveCache("pergunta")]
	e.Quando = time.Now().Add(-2 * validadeCache)
	rcMapa[chaveCache("pergunta")] = e
	rcMu.Unlock()

	if _, ok := BuscarResposta("pergunta"); ok {
		t.Fatal("cache vencido não pode ser servido")
	}
}

func TestMedirProvedoresPreencheCache(t *testing.T) {
	LimparMedicoes()
	defer LimparMedicoes()

	elo := ProvedorEscolhido{Rotulo: "fake#1", Modelo: "m", Cliente: &clienteRoteirizado{
		nome: "fake", respostas: []RespostaGeracao{{Texto: "ok", Finish: "stop"}},
	}}
	if n := MedirProvedores(context.Background(), []ProvedorEscolhido{elo}); n != 1 {
		t.Fatalf("deveria medir 1 elo, mediu %d", n)
	}
	if _, ok := latenciaDe("fake#1"); !ok {
		t.Fatal("a medição deveria estar no cache")
	}
}
