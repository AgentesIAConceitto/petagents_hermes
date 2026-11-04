package main

// harness_memoria.go — bloco 2 do harness nativo do PetAgent (30/10/2026).
//
// POR QUE EXISTE: o requisito é que o PetAgent continue "inteligente e com memória" mesmo
// rodando com chave grátis. Memória não pode depender do modelo: o modelo esquece entre
// chamadas (o contexto é finito e o free tier é mais apertado). Então a memória mora aqui.
//
// TRÊS CAMADAS (as duas primeiras neste arquivo):
//   1. FATOS   — o que precisa sobreviver para sempre: "o cliente é a empresa Y",
//                "o PRD fica em projeto-x/prd.md". O agente grava com `memorizar`.
//   2. EVENTOS — trilha do que aconteceu, com hora (limitada e rotativa). Serve para o
//                agente reconstruir "o que eu estava fazendo ontem".
//   3. ARQUIVOS — o conteúdo de verdade fica em disco (workspace); o agente lê o arquivo
//                 em vez de tentar lembrar do texto. Já coberto por harness_ferramentas.go.
//
// BUSCA SEM EMBEDDING, DE PROPÓSITO: chamar API de embedding custaria cota do free tier a
// cada gravação. Aqui a busca é por termo (com remoção de acento) + peso de recência — zero
// custo e previsível. Se um dia faltar recall, aí sim se avalia embedding local.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	memoriaMaxFatos   = 2000
	memoriaMaxEventos = 5000
	memoriaMaxTexto   = 4000 // bytes por fato
)

// Fato é uma informação durável que o agente decidiu guardar.
type Fato struct {
	ID        string    `json:"id"`
	Texto     string    `json:"texto"`
	Tags      []string  `json:"tags,omitempty"`
	Criado    time.Time `json:"criado"`
	UltimoUso time.Time `json:"ultimo_uso,omitempty"`
	Usos      int       `json:"usos,omitempty"`
}

// Evento é uma linha da trilha (o que aconteceu, quando).
type Evento struct {
	Quando time.Time `json:"quando"`
	Tipo   string    `json:"tipo"` // "pedido", "ferramenta", "resposta", "agenda"
	Texto  string    `json:"texto"`
	// Conversa separa o que é DIÁLOGO (pedido/resposta) do que é OPERAÇÃO (erro, cota).
	// Sem isso, o evento da conversa anterior entrava no prompt da conversa nova e o agente
	// "lembrava do que falou na outra" (04/11/2026).
	Conversa string `json:"conversa,omitempty"`
}

// Memoria é o armazenamento persistente (memoria.json na pasta do PetAgent).
type Memoria struct {
	mu      sync.Mutex
	Fat     []Fato   `json:"fatos"`
	Eve     []Evento `json:"eventos"`
	proxID  int
	arquivo string
}

// AbrirMemoria carrega (ou cria) a memória em %LOCALAPPDATA%\petagent\memoria.json.
func AbrirMemoria(caminho string) *Memoria {
	if caminho == "" {
		caminho = filepath.Join(pastaPetAgente(), "memoria.json")
	}
	m := &Memoria{arquivo: caminho}
	if b, err := os.ReadFile(caminho); err == nil {
		_ = json.Unmarshal(b, m)
	}
	m.proxID = len(m.Fat) + 1
	for _, f := range m.Fat {
		var n int
		if _, err := fmt.Sscanf(f.ID, "f%d", &n); err == nil && n >= m.proxID {
			m.proxID = n + 1
		}
	}
	return m
}

// salvar grava de forma atômica (tmp + Rename), como conversas.go. Chamar com m.mu travado.
func (m *Memoria) salvar() error {
	if m.arquivo == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.arquivo), 0o755); err != nil {
		return err
	}
	if len(m.Fat) > memoriaMaxFatos {
		m.podar()
	}
	if len(m.Eve) > memoriaMaxEventos {
		m.Eve = m.Eve[len(m.Eve)-memoriaMaxEventos:]
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.arquivo + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.arquivo)
}

// podar descarta os fatos menos usados e mais antigos quando passa do teto.
func (m *Memoria) podar() {
	ordem := make([]Fato, len(m.Fat))
	copy(ordem, m.Fat)
	sort.SliceStable(ordem, func(i, j int) bool {
		pi := float64(ordem[i].Usos)*10 + recencia(ordem[i].Criado)
		pj := float64(ordem[j].Usos)*10 + recencia(ordem[j].Criado)
		return pi > pj
	})
	m.Fat = ordem[:memoriaMaxFatos]
}

// recencia devolve 1.0 para hoje caindo pela metade a cada 30 dias.
func recencia(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	dias := time.Since(t).Hours() / 24
	return math.Pow(0.5, dias/30)
}

// normalizar deixa o texto comparável: minúsculo, sem acento, sem pontuação.
func normalizar(s string) string {
	s = strings.ToLower(s)
	troca := strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n",
	)
	s = troca.Replace(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// termos devolve os termos úteis (fora de uma lista curta de palavras vazias).
func termos(s string) []string {
	vazias := map[string]bool{"de": true, "da": true, "do": true, "que": true, "e": true,
		"o": true, "a": true, "os": true, "as": true, "um": true, "uma": true, "para": true,
		"com": true, "no": true, "na": true, "em": true, "por": true, "se": true, "ao": true}
	var out []string
	for _, t := range strings.Fields(normalizar(s)) {
		if len(t) < 2 || vazias[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// Memorizar grava um fato. Se já existir fato muito parecido, atualiza em vez de duplicar.
func (m *Memoria) Memorizar(texto string, tags []string) (string, error) {
	texto = strings.TrimSpace(texto)
	if texto == "" {
		return "", errors.New("texto vazio")
	}
	if len(texto) > memoriaMaxTexto {
		texto = texto[:memoriaMaxTexto]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	alvo := normalizar(texto)
	for i := range m.Fat {
		if normalizar(m.Fat[i].Texto) == alvo {
			m.Fat[i].UltimoUso = time.Now()
			m.Fat[i].Usos++
			if len(tags) > 0 {
				m.Fat[i].Tags = unirTags(m.Fat[i].Tags, tags)
			}
			_ = m.salvar()
			return m.Fat[i].ID, nil
		}
	}
	m.proxID++
	f := Fato{ID: fmt.Sprintf("f%d", m.proxID), Texto: texto, Tags: tags,
		Criado: time.Now(), UltimoUso: time.Now(), Usos: 1}
	m.Fat = append(m.Fat, f)
	if err := m.salvar(); err != nil {
		return f.ID, err
	}
	return f.ID, nil
}

func unirTags(a, b []string) []string {
	visto := map[string]bool{}
	for _, t := range a {
		visto[strings.ToLower(t)] = true
	}
	for _, t := range b {
		if !visto[strings.ToLower(t)] {
			a = append(a, t)
			visto[strings.ToLower(t)] = true
		}
	}
	return a
}

// Evento registra uma linha na trilha do que aconteceu.
func (m *Memoria) Evento(tipo, texto string) {
	m.EventoEm("", tipo, texto)
}

// EventoEm registra o evento amarrado a uma conversa ("" = evento global de operação).
func (m *Memoria) EventoEm(conversa, tipo, texto string) {
	if len(texto) > 500 {
		texto = texto[:500]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Eve = append(m.Eve, Evento{Quando: time.Now(), Tipo: tipo, Texto: texto, Conversa: conversa})
	_ = m.salvar()
}

// ResultadoBusca é um fato encontrado, com a nota de relevância.
type ResultadoBusca struct {
	Fato Fato
	Nota float64
}

// Buscar procura fatos por termos, ordenando por relevância + recência + uso.
func (m *Memoria) Buscar(consulta string, limite int) []ResultadoBusca {
	if limite <= 0 {
		limite = 8
	}
	q := termos(consulta)
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []ResultadoBusca
	for i := range m.Fat {
		f := m.Fat[i]
		corpo := normalizar(f.Texto + " " + strings.Join(f.Tags, " "))
		acertos := 0
		for _, t := range q {
			if strings.Contains(corpo, t) {
				acertos++
			}
		}
		if acertos == 0 {
			continue
		}
		nota := float64(acertos)/float64(len(q)) + 0.5*recencia(f.Criado) + 0.1*math.Log1p(float64(f.Usos))
		res = append(res, ResultadoBusca{Fato: f, Nota: nota})
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Nota > res[j].Nota })
	if len(res) > limite {
		res = res[:limite]
	}
	// marca uso: fato lembrado é fato útil
	for _, r := range res {
		for i := range m.Fat {
			if m.Fat[i].ID == r.Fato.ID {
				m.Fat[i].UltimoUso = time.Now()
				m.Fat[i].Usos++
			}
		}
	}
	if len(res) > 0 {
		_ = m.salvar()
	}
	return res
}

// Esquecer remove um fato pelo ID.
func (m *Memoria) Esquecer(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, f := range m.Fat {
		if f.ID == id {
			m.Fat = append(m.Fat[:i], m.Fat[i+1:]...)
			_ = m.salvar()
			return true
		}
	}
	return false
}

// Quantos devolve (fatos, eventos).
func (m *Memoria) Quantos() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Fat), len(m.Eve)
}

// ContextoParaPrompt monta o bloco que entra no prompt de sistema: memória relevante
// para o pedido atual + o que aconteceu recentemente. É isso que dá continuidade ao
// agente entre sessões sem reenviar conversa inteira.
func (m *Memoria) ContextoParaPrompt(consulta string, maxFatos int, maxEventos int) string {
	return m.ContextoParaPromptDe("", consulta, maxFatos, maxEventos)
}

// ContextoParaPromptDe é a versão com conversa: eventos de diálogo só entram se forem DESTA
// conversa. Eventos de operação (erro, cota, ferramenta sem conversa) entram sempre, porque
// não carregam conteúdo de conversa.
func (m *Memoria) ContextoParaPromptDe(conversa, consulta string, maxFatos int, maxEventos int) string {
	var b strings.Builder
	if maxFatos <= 0 {
		maxFatos = 8
	}
	if maxEventos <= 0 {
		maxEventos = 6
	}
	res := m.Buscar(consulta, maxFatos)
	m.mu.Lock()
	var ultimos []Evento
	for i := len(m.Eve) - 1; i >= 0 && len(ultimos) < maxEventos; i-- {
		e := m.Eve[i]
		// diálogo de outra conversa NUNCA entra; operação global entra sempre
		if e.Conversa != "" && e.Conversa != conversa {
			continue
		}
		ultimos = append([]Evento{e}, ultimos...)
	}
	m.mu.Unlock()
	if len(res) > 0 {
		b.WriteString("## O que eu já sei (memória)\n")
		for _, r := range res {
			b.WriteString("- [" + r.Fato.ID + "] " + r.Fato.Texto)
			if len(r.Fato.Tags) > 0 {
				b.WriteString("  (tags: " + strings.Join(r.Fato.Tags, ", ") + ")")
			}
			b.WriteString("\n")
		}
	}
	if len(ultimos) > 0 {
		b.WriteString("\n## O que aconteceu recentemente\n")
		for _, e := range ultimos {
			b.WriteString("- " + e.Quando.Format("02/01 15:04") + " [" + e.Tipo + "] " + e.Texto + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// RegistrarFerramentasMemoria registra as ferramentas de memória no registro.
func RegistrarFerramentasMemoria(r *Registro, m *Memoria) {
	r.Registrar(&Ferramenta{
		Nome:      "memorizar",
		Descricao: "Guarda um fato durável sobre o usuário ou o projeto (ex.: nome do cliente, onde fica um arquivo, decisão tomada).",
		Parametros: schemaObjeto(map[string]any{
			"texto": schemaString("O fato, em uma frase objetiva"),
			"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "Assuntos do fato (ex.: cliente, projeto-x)"},
		}, "texto"),
		Executar: func(args map[string]any) (string, error) {
			var tags []string
			if v, ok := args["tags"].([]any); ok {
				for _, t := range v {
					if s, ok := t.(string); ok && strings.TrimSpace(s) != "" {
						tags = append(tags, strings.TrimSpace(s))
					}
				}
			}
			id, err := m.Memorizar(argTexto(args, "texto"), tags)
			if err != nil {
				return "", err
			}
			return "ok: memorizado como " + id, nil
		},
	})

	r.Registrar(&Ferramenta{
		Nome:      "buscar_memoria",
		Descricao: "Procura na memória fatos relacionados a um assunto.",
		Parametros: schemaObjeto(map[string]any{
			"consulta": schemaString("Assunto a procurar"),
			"limite":   map[string]any{"type": "integer", "description": "Máximo de fatos (padrão 8)"},
		}, "consulta"),
		Executar: func(args map[string]any) (string, error) {
			lim := argInt(args, "limite", 8)
			if lim < 1 {
				lim = 8
			}
			res := m.Buscar(argTexto(args, "consulta"), lim)
			if len(res) == 0 {
				return "nada encontrado na memória sobre isso", nil
			}
			var b strings.Builder
			for _, r := range res {
				b.WriteString(fmt.Sprintf("- [%s] %s\n", r.Fato.ID, r.Fato.Texto))
			}
			return b.String(), nil
		},
	})

	r.Registrar(&Ferramenta{
		Nome:      "esquecer",
		Descricao: "Remove um fato da memória pelo ID (use quando o fato estiver errado ou vencido).",
		Parametros: schemaObjeto(map[string]any{
			"id": schemaString("ID do fato (ex.: f12)"),
		}, "id"),
		Executar: func(args map[string]any) (string, error) {
			if m.Esquecer(argTexto(args, "id")) {
				return "ok: esquecido", nil
			}
			return "", errors.New("não achei esse ID")
		},
	})
}
