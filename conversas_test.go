package main

import (
	"reflect"
	"testing"
)

func TestMesclaConversas(t *testing.T) {
	lista := []conversaHermes{
		{ID: "antiga", Titulo: "Viagem", Inicio: 100, Mensagens: 4},
		{ID: "vazia", Inicio: 300, Mensagens: 0},
		{ID: "atual", Inicio: 400, Mensagens: 0},
		{ID: "nova", Titulo: "Orçamento", Inicio: 200, Mensagens: 2},
	}
	d := dadosConversas{Conversas: map[string]metaConversa{
		"nova": {Pasta: "Clientes", Etiquetas: []string{"orçamento"}},
	}}
	got := mesclaConversas(lista, d, "atual")
	ids := []string{}
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	// mais recente primeiro; a vazia largada some, a vazia ATUAL fica
	if want := []string{"atual", "nova", "antiga"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ordem/filtro: %v, esperava %v", ids, want)
	}
	if got[1].Pasta != "Clientes" || !reflect.DeepEqual(got[1].Etiquetas, []string{"orçamento"}) {
		t.Errorf("pasta/etiquetas não vieram: %+v", got[1])
	}
	if got[2].Etiquetas == nil {
		t.Error("sem etiqueta deve ser lista vazia (o JS faz .map), não null")
	}
}

func TestNormalizaEtiquetas(t *testing.T) {
	got := normalizaEtiquetas([]string{"#Orçamento", " orçamento ", "", "muito urgente", "##x"})
	if want := []string{"orçamento", "muito-urgente", "x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizaEtiquetas = %v, esperava %v", got, want)
	}
}

func TestComPasta(t *testing.T) {
	p := comPasta([]string{"Pessoal"}, "Clientes")
	p = comPasta(p, "clientes")
	if want := []string{"Clientes", "Pessoal"}; !reflect.DeepEqual(p, want) {
		t.Fatalf("comPasta = %v, esperava %v", p, want)
	}
}

func TestReservaNaConversaCerta(t *testing.T) {
	tem := func(args []string, s string) bool {
		for _, a := range args {
			if a == s {
				return true
			}
		}
		return false
	}
	if a := argsReservaEm("oi", "20261022_abc", true); !tem(a, "--resume") || !tem(a, "20261022_abc") || tem(a, "--continue") {
		t.Errorf("home do boneco + conversa: deveria retomar a conversa: %q", a)
	}
	if a := argsReservaEm("oi", "20261022_abc", false); tem(a, "--resume") {
		t.Errorf("home do dono não tem a conversa do boneco: %q", a)
	}
}
