package main

// Conversas do chat (22/10/2026). Pedido da dona: "eu crio conversas novas sobre assuntos
// diferentes e a anterior fica salva — como no Claude —, e dá para renomear, pôr etiqueta e
// categoria". Telas aprovadas na prancha "PetAgent — Conversas e Anexos".
//
// O HISTÓRICO mora no Hermes (o state.db do home do boneco), e quem fala com ele é a ponte
// (chat_bridge.py, as operações {"op": ...}). Aqui fica só o que o Hermes não tem: a PASTA e
// as ETIQUETAS de cada conversa, a lista de pastas e a última conversa aberta — em
// conversas.json, na pasta do PetAgent. O título é do Hermes (ele dá um sozinho depois da
// primeira resposta; renomear grava lá).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type metaConversa struct {
	Pasta     string   `json:"pasta,omitempty"`
	Etiquetas []string `json:"etiquetas,omitempty"`
}

type dadosConversas struct {
	Atual     string                  `json:"atual"`
	Pastas    []string                `json:"pastas"`
	Conversas map[string]metaConversa `json:"conversas"`
}

// conversaHermes — uma linha da lista que a ponte devolve (session.list do Hermes).
type conversaHermes struct {
	ID        string  `json:"id"`
	Titulo    string  `json:"titulo"`
	Previa    string  `json:"previa"`
	Inicio    float64 `json:"inicio"`
	Mensagens int     `json:"mensagens"`
}

// conversaTela — o que o painel lateral desenha.
type conversaTela struct {
	conversaHermes
	Pasta     string   `json:"pasta"`
	Etiquetas []string `json:"etiquetas"`
}

var (
	conversasMu    sync.Mutex
	conversasUltim []conversaHermes // a última lista que veio da ponte (para quando ela está ocupada)
)

func arquivoConversas() string { return filepath.Join(pastaPetAgente(), "conversas.json") }

func leConversas() dadosConversas {
	d := dadosConversas{}
	if b, err := os.ReadFile(arquivoConversas()); err == nil {
		_ = json.Unmarshal(b, &d)
	}
	if d.Conversas == nil {
		d.Conversas = map[string]metaConversa{}
	}
	if d.Pastas == nil {
		d.Pastas = []string{}
	}
	return d
}

func gravaConversas(d dadosConversas) {
	b, _ := json.MarshalIndent(d, "", "  ")
	tmp := arquivoConversas() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, arquivoConversas())
	}
}

// conversaAtual — a conversa aberta por último (vai para a ponte ao subir: ela retoma esta).
func conversaAtual() string {
	conversasMu.Lock()
	defer conversasMu.Unlock()
	return leConversas().Atual
}

// guardaConversa — a ponte disse em que conversa está (vazio = não disse; não apaga).
func guardaConversa(id string) {
	if id = strings.TrimSpace(id); id == "" {
		return
	}
	conversasMu.Lock()
	defer conversasMu.Unlock()
	d := leConversas()
	if d.Atual != id {
		d.Atual = id
		gravaConversas(d)
	}
}

// normalizaEtiquetas — sem "#", minúsculas, sem repetir; no máximo 10, de até 24 letras.
func normalizaEtiquetas(lista []string) []string {
	saida, vistas := []string{}, map[string]bool{}
	for _, e := range lista {
		e = strings.ToLower(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(e), "#")))
		e = strings.Join(strings.Fields(e), "-")
		if r := []rune(e); len(r) > 24 {
			e = string(r[:24])
		}
		if e == "" || vistas[e] {
			continue
		}
		vistas[e] = true
		saida = append(saida, e)
		if len(saida) == 10 {
			break
		}
	}
	return saida
}

// mesclaConversas — a lista do Hermes + a pasta e as etiquetas de cada uma, mais recente
// primeiro. Conversa sem mensagem nenhuma (aberta e largada) fica de fora, menos a atual.
func mesclaConversas(lista []conversaHermes, d dadosConversas, atual string) []conversaTela {
	saida := []conversaTela{}
	for _, c := range lista {
		if c.Mensagens == 0 && c.ID != atual {
			continue
		}
		m := d.Conversas[c.ID]
		et := m.Etiquetas
		if et == nil {
			et = []string{}
		}
		saida = append(saida, conversaTela{conversaHermes: c, Pasta: m.Pasta, Etiquetas: et})
	}
	sort.SliceStable(saida, func(i, j int) bool { return saida[i].Inicio > saida[j].Inicio })
	return saida
}

// opPonte — uma operação de conversa na ponte. esperar=false: com a ponte ocupada
// (pensando numa resposta), devolve ocupado na hora em vez de ficar na fila.
func (a *App) opPonte(pedido map[string]any, esperar bool) (map[string]any, string) {
	r, ok := a.pedidoPonte(pedido, esperar)
	if !ok {
		if r.Erro == "ocupado" {
			return nil, "ocupado"
		}
		if r.Erro == "" {
			r.Erro = "O agente não respondeu."
		}
		return nil, r.Erro
	}
	dados := map[string]any{}
	if len(r.Dados) > 0 {
		_ = json.Unmarshal(r.Dados, &dados)
	}
	if e, _ := dados["erro"].(string); e != "" {
		return nil, e
	}
	if r.Erro != "" && len(r.Dados) == 0 {
		return nil, r.Erro
	}
	if atual, _ := dados["atual"].(string); atual != "" {
		guardaConversa(atual)
	}
	return dados, ""
}

// ConversasListar — a lista do painel lateral: {ok, atual, pastas, conversas, ocupado?}.
func (a *App) ConversasListar() string {
	dados, erro := a.opPonte(map[string]any{"op": "listar"}, false)
	conversasMu.Lock()
	defer conversasMu.Unlock()
	var lista []conversaHermes
	if erro == "" {
		b, _ := json.Marshal(dados["conversas"])
		_ = json.Unmarshal(b, &lista)
		conversasUltim = lista
	} else {
		lista = conversasUltim // ocupada ou fora do ar: a última lista que se viu
	}
	d := leConversas()
	return respostaJSON(map[string]any{"ok": erro == "", "erro": erro, "ocupado": erro == "ocupado",
		"atual": d.Atual, "pastas": d.Pastas, "conversas": mesclaConversas(lista, d, d.Atual)})
}

// ConversaAbrir — passa a conversar nesta e devolve as mensagens dela.
func (a *App) ConversaAbrir(id string) string {
	dados, erro := a.opPonte(map[string]any{"op": "abrir", "conversa": id}, false)
	if erro != "" {
		return respostaJSON(map[string]any{"ok": false, "erro": erro})
	}
	return respostaJSON(map[string]any{"ok": true, "atual": dados["atual"], "mensagens": dados["mensagens"]})
}

// ConversaNova — começa uma conversa do zero (a anterior fica guardada).
func (a *App) ConversaNova() string {
	dados, erro := a.opPonte(map[string]any{"op": "nova"}, false)
	if erro != "" {
		return respostaJSON(map[string]any{"ok": false, "erro": erro})
	}
	return respostaJSON(map[string]any{"ok": true, "atual": dados["atual"]})
}

// ConversaRenomear — o título fica no Hermes (o mesmo que ele mostra no CLI e no app dele).
func (a *App) ConversaRenomear(id, titulo string) string {
	titulo = strings.TrimSpace(titulo)
	if r := []rune(titulo); len(r) > 80 {
		titulo = string(r[:80])
	}
	if titulo == "" {
		return respostaJSON(map[string]any{"ok": false, "erro": "Dê um nome para a conversa."})
	}
	_, erro := a.opPonte(map[string]any{"op": "renomear", "conversa": id, "titulo": titulo}, false)
	return respostaJSON(map[string]any{"ok": erro == "", "erro": erro, "titulo": titulo})
}

// ConversaExcluir — apaga do Hermes (histórico e anexos) e daqui (pasta e etiquetas).
func (a *App) ConversaExcluir(id string) string {
	dados, erro := a.opPonte(map[string]any{"op": "excluir", "conversa": id}, false)
	if erro != "" {
		return respostaJSON(map[string]any{"ok": false, "erro": erro})
	}
	conversasMu.Lock()
	d := leConversas()
	delete(d.Conversas, id)
	gravaConversas(d)
	conversasMu.Unlock()
	return respostaJSON(map[string]any{"ok": true, "atual": dados["atual"]})
}

// ConversaOrganizar — pasta ("" = sem pasta) e etiquetas (JSON: ["orçamento", ...]).
// Pasta nova entra sozinha na lista de pastas.
func (a *App) ConversaOrganizar(id, pasta, etiquetasJSON string) string {
	var et []string
	_ = json.Unmarshal([]byte(etiquetasJSON), &et)
	pasta = strings.TrimSpace(pasta)
	conversasMu.Lock()
	defer conversasMu.Unlock()
	d := leConversas()
	for _, p := range d.Pastas { // "clientes" digitado vai para a "Clientes" que já existe
		if strings.EqualFold(p, pasta) {
			pasta = p
		}
	}
	m := metaConversa{Pasta: pasta, Etiquetas: normalizaEtiquetas(et)}
	if m.Pasta == "" && len(m.Etiquetas) == 0 {
		delete(d.Conversas, id)
	} else {
		d.Conversas[id] = m
	}
	d.Pastas = comPasta(d.Pastas, pasta)
	gravaConversas(d)
	return respostaJSON(map[string]any{"ok": true, "pasta": m.Pasta, "etiquetas": m.Etiquetas})
}

// PastaCriar — pasta vazia (para já aparecer no menu de mover).
func (a *App) PastaCriar(nome string) string {
	nome = strings.TrimSpace(nome)
	if nome == "" {
		return respostaJSON(map[string]any{"ok": false, "erro": "Dê um nome para a pasta."})
	}
	conversasMu.Lock()
	defer conversasMu.Unlock()
	d := leConversas()
	d.Pastas = comPasta(d.Pastas, nome)
	gravaConversas(d)
	return respostaJSON(map[string]any{"ok": true, "pastas": d.Pastas})
}

// PastaExcluir — some a pasta; as conversas dela vão para "Sem pasta" (nada é apagado).
func (a *App) PastaExcluir(nome string) string {
	conversasMu.Lock()
	defer conversasMu.Unlock()
	d := leConversas()
	novas := []string{}
	for _, p := range d.Pastas {
		if p != nome {
			novas = append(novas, p)
		}
	}
	d.Pastas = novas
	for id, m := range d.Conversas {
		if m.Pasta == nome {
			m.Pasta = ""
			d.Conversas[id] = m
		}
	}
	gravaConversas(d)
	return respostaJSON(map[string]any{"ok": true, "pastas": d.Pastas})
}

// comPasta — a lista de pastas com `nome` (sem repetir, ignorando maiúsculas), em ordem alfabética.
func comPasta(pastas []string, nome string) []string {
	if nome == "" {
		return pastas
	}
	for _, p := range pastas {
		if strings.EqualFold(p, nome) {
			return pastas
		}
	}
	pastas = append(pastas, nome)
	sort.Slice(pastas, func(i, j int) bool { return strings.ToLower(pastas[i]) < strings.ToLower(pastas[j]) })
	return pastas
}
