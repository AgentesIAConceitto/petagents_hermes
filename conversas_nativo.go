package main

// conversas_nativo.go — conversas do modo grátis, guardadas no próprio app (04/11/2026).
//
// PROBLEMA RELATADO: "quando eu tento iniciar uma nova conversa ele me diz que o agente não
// está iniciado". Causa: ConversaNova/ConversaAbrir/ConversasListar falam com a PONTE do
// Hermes (opPonte). No modo grátis não existe Hermes, então toda a gestão de conversas morria
// — inclusive começar uma conversa nova, que é o botão mais básico da tela.
//
// Solução: no modo nativo, as conversas vivem em %LOCALAPPDATA%\petagent\conversas\<id>.json.
// Um arquivo por conversa, com título, data e mensagens. Nada de depender do Hermes.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type mensagemLocal struct {
	De     string `json:"de"` // "user" ou "assistant"
	Texto  string `json:"texto"`
	Quando string `json:"quando"`
}

type conversaLocal struct {
	ID         string          `json:"id"`
	Titulo     string          `json:"titulo"`
	Criada     string          `json:"criada"`
	Atualizada string          `json:"atualizada"`
	Mensagens  []mensagemLocal `json:"mensagens"`
}

var convLocalMu sync.Mutex

func pastaConversasLocal() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "petagent", "conversas")
}

func caminhoConversaLocal(id string) string {
	return filepath.Join(pastaConversasLocal(), id+".json")
}

// NovaConversaLocal cria uma conversa vazia e devolve o id dela.
func NovaConversaLocal() string {
	agora := time.Now()
	// sufixo com microssegundos: sem ele, duas conversas criadas no mesmo segundo caíam no
	// mesmo arquivo e as mensagens se misturavam (pego pelo teste em 04/11/2026).
	id := fmt.Sprintf("local-%s-%06d", agora.Format("20060102-150405"), agora.Nanosecond()/1000)
	c := conversaLocal{
		ID:         id,
		Titulo:     "Conversa " + agora.Format("02/01 15:04"),
		Criada:     agora.Format(time.RFC3339),
		Atualizada: agora.Format(time.RFC3339),
		Mensagens:  []mensagemLocal{},
	}
	convLocalMu.Lock()
	defer convLocalMu.Unlock()
	if err := os.MkdirAll(pastaConversasLocal(), 0o700); err != nil {
		return ""
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(caminhoConversaLocal(id), b, 0o600); err != nil {
		return ""
	}
	return id
}

// LeConversaLocal devolve a conversa ou nil se não existir.
func LeConversaLocal(id string) *conversaLocal {
	if id == "" {
		return nil
	}
	convLocalMu.Lock()
	defer convLocalMu.Unlock()
	b, err := os.ReadFile(caminhoConversaLocal(id))
	if err != nil {
		return nil
	}
	var c conversaLocal
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return &c
}

// GuardaMensagensLocal grava o par pergunta/resposta no arquivo da conversa.
func GuardaMensagensLocal(id, pergunta, resposta string) {
	if id == "" {
		return
	}
	convLocalMu.Lock()
	defer convLocalMu.Unlock()
	c := conversaLocal{ID: id, Titulo: "Conversa", Criada: time.Now().Format(time.RFC3339)}
	if b, err := os.ReadFile(caminhoConversaLocal(id)); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	agora := time.Now().Format(time.RFC3339)
	c.ID = id
	c.Atualizada = agora
	// o título vem da primeira pergunta: melhor que "Conversa 04/11 16:20"
	if len(c.Mensagens) == 0 && strings.TrimSpace(pergunta) != "" {
		c.Titulo = cortarTexto(strings.TrimSpace(strings.Split(pergunta, "\n")[0]), 48)
	}
	c.Mensagens = append(c.Mensagens,
		mensagemLocal{De: "user", Texto: cortarTexto(pergunta, 4000), Quando: agora},
		mensagemLocal{De: "assistant", Texto: resposta, Quando: agora},
	)
	if len(c.Mensagens) > 40 {
		c.Mensagens = c.Mensagens[len(c.Mensagens)-40:]
	}
	_ = os.MkdirAll(pastaConversasLocal(), 0o700)
	b, _ := json.MarshalIndent(c, "", "  ")
	_ = os.WriteFile(caminhoConversaLocal(id), b, 0o600)
}

// ListaConversasLocal devolve as conversas do modo grátis, da mais recente para a mais antiga.
func ListaConversasLocal() []conversaLocal {
	convLocalMu.Lock()
	defer convLocalMu.Unlock()
	arqs, err := filepath.Glob(filepath.Join(pastaConversasLocal(), "*.json"))
	if err != nil {
		return nil
	}
	var saida []conversaLocal
	for _, a := range arqs {
		b, err := os.ReadFile(a)
		if err != nil {
			continue
		}
		var c conversaLocal
		if json.Unmarshal(b, &c) == nil && c.ID != "" {
			saida = append(saida, c)
		}
	}
	sort.Slice(saida, func(i, j int) bool { return saida[i].Atualizada > saida[j].Atualizada })
	return saida
}

// ApagaConversaLocal remove o arquivo da conversa.
func ApagaConversaLocal(id string) error {
	if id == "" {
		return fmt.Errorf("conversa vazia")
	}
	convLocalMu.Lock()
	defer convLocalMu.Unlock()
	return os.Remove(caminhoConversaLocal(id))
}

// RenomeiaConversaLocal troca o título.
func RenomeiaConversaLocal(id, titulo string) error {
	convLocalMu.Lock()
	defer convLocalMu.Unlock()
	b, err := os.ReadFile(caminhoConversaLocal(id))
	if err != nil {
		return err
	}
	var c conversaLocal
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	c.Titulo = cortarTexto(strings.TrimSpace(titulo), 60)
	out, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(caminhoConversaLocal(id), out, 0o600)
}

// MensagensParaTela converte o arquivo local no formato que a TELA espera:
// {quem: "eu"|"ele", texto, quando(segundos)}. O contrato é da interface (conversas.js
// usa m.quem e horaDe(m.quando) = new Date(seg*1000)), não do armazenamento.
// Devolver role/content fazia a conversa abrir vazia (04/11/2026).
func MensagensParaTela(c *conversaLocal) []map[string]any {
	if c == nil {
		return []map[string]any{}
	}
	msgs := make([]map[string]any, 0, len(c.Mensagens))
	for _, m := range c.Mensagens {
		quem := "ele"
		if m.De == "user" {
			quem = "eu"
		}
		var seg int64
		if ts, err := time.Parse(time.RFC3339, m.Quando); err == nil {
			seg = ts.Unix()
		}
		msgs = append(msgs, map[string]any{"quem": quem, "texto": m.Texto, "quando": seg})
	}
	return msgs
}
