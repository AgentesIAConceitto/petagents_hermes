package main

// harness_latencia.go — o app mede a latência real dos provedores e escolhe por ela (03/11/2026).
//
// POR QUE EXISTE (relato do dono, 03/11/2026): "qual a cotação do dólar" levou 54,5 s. A causa
// não era o código: o Gemini free estava a ~19 s por chamada NAQUELE momento, contra 3,0 s
// medidos dois dias antes. O defeito de raiz era de desenho — a cadeia de atendimento seguia
// uma ORDEM FIXA escrita de antemão, decidida com uma amostra velha. Se o provedor piora, o
// usuário descobre com o cronômetro na mão.
//
// REGRA DA CASA: medir > confiar. Então:
//   - cada provedor é cronometrado com uma chamada mínima (1 token), barata em cota;
//   - a medição vale por uma janela (padrão 1 h) e é refeita depois disso;
//   - a cadeia é montada com os medidos primeiro, do mais rápido para o mais lento;
//   - provedor ainda não medido continua na cadeia (na ordem original), só atrás dos medidos —
//     nunca se exclui ninguém por falta de dado;
//   - chave em cooldown continua indo para o fim (regra do pool).
//
// Sem isso, "rápido" é opinião. Com isso, é número de agora.

import (
	"context"
	"sort"
	"sync"
	"time"
)

// validadeMedicao diz por quanto tempo uma medição de latência é considerada atual.
const validadeMedicao = time.Hour

// timeoutMedicao limita a chamada de medição — provedor pendurado não trava a montagem.
const timeoutMedicao = 20 * time.Second

type medicaoLatencia struct {
	Quando  time.Time
	Duracao time.Duration
	Erro    string
}

var (
	latMu    sync.Mutex
	latCache = map[string]medicaoLatencia{} // rótulo (gemini#1) -> medição
)

// registrarLatencia guarda o tempo medido de um rótulo.
func registrarLatencia(rotulo string, d time.Duration, err error) {
	latMu.Lock()
	defer latMu.Unlock()
	m := medicaoLatencia{Quando: time.Now(), Duracao: d}
	if err != nil {
		m.Erro = err.Error()
		// erro também é informação: provedor que falhou vai para o fim
		m.Duracao = 24 * time.Hour
	}
	latCache[rotulo] = m
}

// latenciaDe devolve a medição ainda válida do rótulo.
func latenciaDe(rotulo string) (time.Duration, bool) {
	latMu.Lock()
	defer latMu.Unlock()
	m, ok := latCache[rotulo]
	if !ok || time.Since(m.Quando) > validadeMedicao {
		return 0, false
	}
	return m.Duracao, true
}

// LimparMedicoes zera o cache (usado nos testes e no botão "medir de novo").
func LimparMedicoes() {
	latMu.Lock()
	defer latMu.Unlock()
	latCache = map[string]medicaoLatencia{}
}

// ordenarPorLatencia devolve os elos ordenados: medidos primeiro (mais rápido na frente),
// depois os não medidos na ordem em que chegaram.
func ordenarPorLatencia(elos []ProvedorEscolhido) []ProvedorEscolhido {
	type item struct {
		elo     ProvedorEscolhido
		medido  bool
		duracao time.Duration
		posicao int
	}
	itens := make([]item, 0, len(elos))
	for i, e := range elos {
		d, ok := latenciaDe(e.Rotulo)
		itens = append(itens, item{elo: e, medido: ok, duracao: d, posicao: i})
	}
	sort.SliceStable(itens, func(i, j int) bool {
		if itens[i].medido != itens[j].medido {
			return itens[i].medido // medido vem antes do não medido
		}
		if itens[i].medido && itens[i].duracao != itens[j].duracao {
			return itens[i].duracao < itens[j].duracao
		}
		return itens[i].posicao < itens[j].posicao
	})
	out := make([]ProvedorEscolhido, len(itens))
	for i, it := range itens {
		out[i] = it.elo
	}
	return out
}

// medirElo faz uma chamada mínima e cronometra. 1 token de saída: custo de cota desprezível.
func medirElo(ctx context.Context, elo ProvedorEscolhido) {
	if elo.Cliente == nil {
		return
	}
	ctxMedicao, cancelar := context.WithTimeout(ctx, timeoutMedicao)
	defer cancelar()
	inicio := time.Now()
	_, err := elo.Cliente.Gerar(ctxMedicao, PedidoGeracao{
		Modelo:    elo.Modelo,
		Mensagens: []Mensagem{{Role: "user", Content: "ok"}},
		MaxTokens: 1,
	})
	registrarLatencia(elo.Rotulo, time.Since(inicio), err)
}

// MedirProvedores mede, em paralelo, todos os elos ainda sem medição válida.
// Devolve quantos foram medidos nesta chamada.
func MedirProvedores(ctx context.Context, elos []ProvedorEscolhido) int {
	var pendentes []ProvedorEscolhido
	for _, e := range elos {
		if _, ok := latenciaDe(e.Rotulo); !ok {
			pendentes = append(pendentes, e)
		}
	}
	if len(pendentes) == 0 {
		return 0
	}
	var wg sync.WaitGroup
	for _, e := range pendentes {
		wg.Add(1)
		go func(elo ProvedorEscolhido) {
			defer wg.Done()
			medirElo(ctx, elo)
		}(e)
	}
	wg.Wait()
	return len(pendentes)
}

// ResumoLatencia devolve "gemini#1: 19,0s · nvidia#1: 1,4s" para a tela e para o log.
func ResumoLatencia(elos []ProvedorEscolhido) string {
	if len(elos) == 0 {
		return "nenhum provedor"
	}
	partes := make([]string, 0, len(elos))
	for _, e := range elos {
		if d, ok := latenciaDe(e.Rotulo); ok {
			partes = append(partes, e.Rotulo+": "+(time.Duration(d).Round(100*time.Millisecond)).String())
		} else {
			partes = append(partes, e.Rotulo+": sem medição")
		}
	}
	return joinCom(partes, " · ")
}

// joinCom evita importar strings só para isso.
func joinCom(itens []string, sep string) string {
	out := ""
	for i, s := range itens {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
