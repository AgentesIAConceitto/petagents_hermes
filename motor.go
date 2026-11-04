package main

// motor.go — o motor do agente nativo (30/10/2026).
//
// É a peça que junta tudo: persona + memória + ferramentas + provedor. Sem ele, o harness
// tinha as mãos (ferramentas) e a memória, mas ninguém para decidir o que fazer com elas.
//
// CICLO (o mesmo desenho que o teste E2E já provava no papel, agora em código de produção):
//   1. monta o pedido: persona + memória relevante + histórico recente + pergunta;
//   2. chama o modelo com o catálogo de ferramentas;
//   3. se o modelo pediu ferramenta: executa, devolve o resultado e volta ao passo 2;
//   4. se respondeu texto: acabou — grava o evento na memória e entrega.
//
// REGRAS QUE VALEM AQUI:
//   - teto de voltas (não entra em loop gastando cota do plano grátis);
//   - toda ferramenta executada vai para a trilha de eventos (dá para reconstruir depois);
//   - 429/cota estourada NÃO mata o turno: a cadeia troca de provedor e continua;
//   - progresso é reportado a cada passo (a tela precisa mostrar o que está acontecendo).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// teto de voltas de ferramenta por mensagem: 6 cabe uma tarefa real (pasta + arquivo +
	// leitura + memória) sem virar loop se o modelo enlouquecer.
	motorMaxVoltas = 6
	// teto de segurança: tempo total do turno (free tier pode ser lento).
	// 5 min transformava instabilidade do provedor em espera de 205 s (03/11/2026).
	// 100 s basta para o caso normal e falha rápido quando o provedor está ruim.
	motorTempoMaximo = 100 * time.Second
)

// Etapa é um passo do turno, para a tela mostrar progresso.
type Etapa struct {
	Volta   int
	Tipo    string // "pedido" | "pensando" | "ferramenta" | "resposta" | "troca" | "erro"
	Detalhe string
	Quando  time.Time
}

// ProvedorEscolhido é um elo da cadeia de failover.
type ProvedorEscolhido struct {
	Rotulo  string
	Modelo  string
	Cliente Cliente
}

// Cadeia é a lista ordenada de provedores: o primeiro é o preferido, os outros entram
// quando o anterior falha (429, cota do dia, erro de rede).
type Cadeia struct {
	elos  []ProvedorEscolhido
	atual int
}

// NovaCadeia monta a cadeia (ignora elos sem cliente).
func NovaCadeia(elos ...ProvedorEscolhido) *Cadeia {
	c := &Cadeia{}
	for _, e := range elos {
		if e.Cliente != nil && e.Modelo != "" {
			if e.Rotulo == "" {
				e.Rotulo = e.Cliente.Nome()
			}
			c.elos = append(c.elos, e)
		}
	}
	return c
}

// Vazia informa se não há nenhum provedor configurado.
func (c *Cadeia) Vazia() bool { return c == nil || len(c.elos) == 0 }

// Elos devolve a cadeia em ordem (cópia rasa) — usada para medir latência.
func (c *Cadeia) Elos() []ProvedorEscolhido {
	if c == nil {
		return nil
	}
	out := make([]ProvedorEscolhido, len(c.elos))
	copy(out, c.elos)
	return out
}

// Atual devolve o elo em uso.
func (c *Cadeia) Atual() (ProvedorEscolhido, bool) {
	if c.Vazia() || c.atual >= len(c.elos) {
		return ProvedorEscolhido{}, false
	}
	return c.elos[c.atual], true
}

// Avancar passa para o próximo provedor. Devolve o novo elo e true, ou false no fim.
func (c *Cadeia) Avancar() (ProvedorEscolhido, bool) {
	if c.Vazia() {
		return ProvedorEscolhido{}, false
	}
	c.atual++
	if c.atual >= len(c.elos) {
		return ProvedorEscolhido{}, false
	}
	return c.elos[c.atual], true
}

// Quantos devolve quantos provedores a cadeia tem.
func (c *Cadeia) Quantos() int {
	if c == nil {
		return 0
	}
	return len(c.elos)
}

// Motor executa um turno do agente.
type Motor struct {
	// Conversa é o id da conversa em curso: separa o contexto de cada uma (04/11/2026).
	Conversa    string
	Harness     *Harness
	Cadeia      *Cadeia
	Sistema     string
	MaxVoltas   int
	AoProgresso func(Etapa)
}

// ResultadoTurno é o que o turno produziu.
type ResultadoTurno struct {
	// DoCache indica que a resposta veio do cache (nada foi chamado agora)
	DoCache       bool
	Texto         string
	Voltas        int
	Chamadas      int
	Ferramentas   []string
	TokensEntrada int
	TokensSaida   int
	Provedor      string
	Modelo        string
	Trocas        int
}

func (m *Motor) maxVoltas() int {
	if m.MaxVoltas > 0 {
		return m.MaxVoltas
	}
	return motorMaxVoltas
}

func (m *Motor) etapa(e Etapa) {
	e.Quando = time.Now()
	if m.AoProgresso != nil {
		m.AoProgresso(e)
	}
}

// sistemaPadrao é a persona mínima do agente nativo. Curta de propósito: no free tier cada
// token do prompt é cota, e o modelo é simples (tarefa básica, não raciocínio profundo).
func (m *Motor) sistemaPadrao() string {
	if strings.TrimSpace(m.Sistema) != "" {
		return m.Sistema
	}
	return "Você é um assistente prático e direto, em português do Brasil. " +
		"Use as ferramentas quando elas resolverem melhor que responder de memória. " +
		"Quando usar busca ou arquivo, diga em uma frase o que encontrou. " +
		"Nunca invente dado: se a ferramenta não trouxe, diga que não encontrou."
}

// Responder roda um turno completo.
func (m *Motor) Responder(ctx context.Context, historico []Mensagem, pedido string) (ResultadoTurno, error) {
	var res ResultadoTurno
	if m == nil || m.Cadeia.Vazia() {
		return res, ErroSemChave
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancelar := context.WithTimeout(ctx, motorTempoMaximo)
	defer cancelar()

	// resposta repetida dentro de 10 min não gasta cota de novo (ver harness_cache.go)
	if texto, achou := BuscarResposta(pedido); achou {
		res.Texto = texto
		res.DoCache = true
		res.Chamadas = 0
		if elo, ok := m.Cadeia.Atual(); ok {
			res.Provedor, res.Modelo = elo.Rotulo, elo.Modelo
		}
		m.etapa(Etapa{Tipo: "cache", Detalhe: "resposta repetida"})
		return res, nil
	}

	// conversa curta não leva ferramenta: economiza ~10 s e reduz falha (medido 03/11/2026)
	conversa := pareceConversa(pedido)

	// PRÉ-BUSCA: pergunta de consulta busca ANTES e entrega o resultado na mesma chamada.
	// Economiza uma ida ao modelo (~19 s no free tier) — ver harness_prebusca.go.
	pedidoAoModelo := pedido
	if m.Harness != nil && m.Harness.Cofre != nil && pareceConsultaDados(pedido) {
		cfgWeb := m.Harness.Cofre.ConfigWeb()
		if cfgWeb.BraveKey != "" || cfgWeb.SearxURL != "" {
			if saida, err := cfgWeb.buscar(consultaParaBusca(pedido), 5); err == nil && saida != "" {
				pedidoAoModelo = pedido + "\n\n" + corteWeb + cortarTexto(saida, 6000)
				m.etapa(Etapa{Tipo: "prebusca", Detalhe: "busquei antes de perguntar"})
			}
		}
	}

	limparTroca() // o aviso de cota vale para o turno atual, não para sempre
	elo, ok := m.Cadeia.Atual()
	if !ok {
		return res, ErroSemChave
	}
	res.Provedor, res.Modelo = elo.Rotulo, elo.Modelo

	// memória relevante para ESTE pedido (não a memória inteira: custa cota)
	contextoMemoria := ""
	if m.Harness != nil && m.Harness.Memoria != nil {
		contextoMemoria = m.Harness.Memoria.ContextoParaPromptDe(m.Conversa, pedido, 6, 4)
	}
	sistema := m.sistemaPadrao()
	if contextoMemoria != "" {
		sistema += "\n\n" + contextoMemoria
	}

	msgs := make([]Mensagem, 0, len(historico)+2)
	msgs = append(msgs, Mensagem{Role: "system", Content: sistema})
	// histórico recente (as últimas 12 falas bastam para tarefa básica)
	if n := len(historico); n > 12 {
		historico = historico[n-12:]
	}
	msgs = append(msgs, historico...)
	msgs = append(msgs, Mensagem{Role: "user", Content: pedidoAoModelo})

	if m.Harness != nil && m.Harness.Memoria != nil {
		m.Harness.Memoria.EventoEm(m.Conversa, "pedido", pedido)
	}
	m.etapa(Etapa{Tipo: "pedido", Detalhe: pedido})

	// Orçamento de saída do turno. Começa no piso medido e SOBE quando o modelo gasta tudo
	// pensando e devolve texto vazio (reasoning_content) — foi o que aconteceu em 30/10 com
	// o gpt-oss-20b: 1500 tokens de raciocínio e nenhuma resposta.
	orcamento := harnessOrcamentoMinimo
	escaladas := 0

	ferramentas := []map[string]any(nil)
	if m.Harness != nil && m.Harness.Registro != nil && !conversa {
		ferramentas = m.Harness.Registro.EsquemaOpenAI()
	}

	for volta := 1; volta <= m.maxVoltas(); volta++ {
		res.Voltas = volta
		m.etapa(Etapa{Volta: volta, Tipo: "pensando", Detalhe: elo.Rotulo})

		resposta, err := elo.Cliente.Gerar(ctx, PedidoGeracao{
			Modelo:      elo.Modelo,
			Mensagens:   msgs,
			Ferramentas: ferramentas,
			MaxTokens:   orcamento,
			Temperatura: 0.2,
		})
		res.Chamadas++
		res.TokensEntrada += resposta.TokensEntrada
		res.TokensSaida += resposta.TokensSaida

		if err != nil {
			// gastou o orçamento pensando e não escreveu: sobe o espaço e tenta de novo no
			// mesmo provedor. Se já subiu duas vezes, aí sim troca de provedor.
			if errors.Is(err, ErrSoRaciocinou) && escaladas < 2 && orcamento < 8000 {
				orcamento *= 2
				escaladas++
				m.etapa(Etapa{Volta: volta, Tipo: "troca",
					Detalhe: fmt.Sprintf("modelo só pensou; aumentando orçamento para %d tokens", orcamento)})
				volta--
				continue
			}
			// failover: cota/limite/erro de provedor não pode matar o turno
			anterior := elo.Rotulo
			// marca a chave que falhou em espera: o pool pula ela nos próximos turnos
			if m.Harness != nil && m.Harness.Cofre != nil {
				if chave := m.Harness.Cofre.ChaveDoRotulo(anterior); chave != "" {
					MarcarCooldown(chave, resumirErro(err))
					if m.Harness.Memoria != nil {
						m.Harness.Memoria.Evento("cota", anterior+" em espera: "+resumirErro(err))
					}
				}
			}
			proximo, tem := m.Cadeia.Avancar()
			if !tem {
				m.etapa(Etapa{Volta: volta, Tipo: "erro", Detalhe: err.Error()})
				if m.Harness != nil && m.Harness.Memoria != nil {
					m.Harness.Memoria.Evento("erro", "todas as chaves falharam: "+err.Error())
				}
				return res, fmt.Errorf("nenhum provedor respondeu: %w", err)
			}
			res.Trocas++
			registrarTroca(anterior, proximo.Rotulo, resumirErro(err))
			elo = proximo
			res.Provedor, res.Modelo = elo.Rotulo, elo.Modelo
			m.etapa(Etapa{Volta: volta, Tipo: "troca",
				Detalhe: fmt.Sprintf("%s falhou (%s) → usando %s", anterior, resumirErro(err), elo.Rotulo)})
			volta-- // a volta que falhou não conta
			continue
		}

		// pediu ferramenta?
		if len(resposta.ChamadasFerramenta) > 0 {
			msgs = append(msgs, Mensagem{Role: "assistant", Content: resposta.Texto, ToolCalls: resposta.ChamadasFerramenta})
			for _, chamada := range resposta.ChamadasFerramenta {
				nome, args := nomeEArgs(chamada)
				res.Ferramentas = append(res.Ferramentas, nome)
				m.etapa(Etapa{Volta: volta, Tipo: "ferramenta", Detalhe: nome})
				saida, errF := "", error(nil)
				if m.Harness != nil {
					saida, errF = m.Harness.Executar(nome, args)
				} else {
					errF = errors.New("harness ausente")
				}
				if errF != nil {
					saida = "ERRO na ferramenta: " + errF.Error()
				}
				msgs = append(msgs, Mensagem{
					Role: "tool", Content: cortarTexto(saida, 8000), ToolCallID: idDaChamada(chamada), ToolName: nome,
				})
			}
			continue
		}

		// resposta final
		res.Texto = strings.TrimSpace(resposta.Texto)
		if res.Texto == "" {
			return res, ErrSoRaciocinou
		}
		if m.Harness != nil && m.Harness.Memoria != nil {
			m.Harness.Memoria.EventoEm(m.Conversa, "resposta", cortarTexto(res.Texto, 300))
		}
		m.etapa(Etapa{Volta: volta, Tipo: "resposta", Detalhe: cortarTexto(res.Texto, 120)})
		// guarda para repetição (só se o turno não alterou nada — ver harness_cache.go)
		GuardarResposta(pedido, res.Texto, res.Ferramentas)
		return res, nil
	}

	return res, fmt.Errorf("não terminei em %d voltas (o modelo ficou pedindo ferramenta)", m.maxVoltas())
}

// corteWeb acompanha os resultados da pré-busca: manda o modelo responder direto, sem
// gastar outra chamada chamando a ferramenta de novo.
const corteWeb = "[O aplicativo JÁ pesquisou na web para você. Use os resultados abaixo e " +
	"responda DIRETAMENTE, sem chamar ferramenta. Se os resultados não bastarem, responda " +
	"com o que tem e diga o que faltou.]\n\n"

// nomeEArgs extrai nome e argumentos de uma chamada no formato OpenAI.
func nomeEArgs(chamada map[string]any) (string, map[string]any) {
	fn, _ := chamada["function"].(map[string]any)
	nome, _ := fn["name"].(string)
	args := map[string]any{}
	switch v := fn["arguments"].(type) {
	case string:
		if v != "" {
			_ = json.Unmarshal([]byte(v), &args)
		}
	case map[string]any:
		args = v
	}
	return nome, args
}

func idDaChamada(chamada map[string]any) string {
	if s, ok := chamada["id"].(string); ok {
		return s
	}
	return "call_local"
}

// resumirErro deixa a mensagem curta para a tela.
func resumirErro(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 90 {
		s = s[:90]
	}
	return s
}
