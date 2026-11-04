package main

// harness_cache.go — resposta repetida não se paga duas vezes (03/11/2026).
//
// CONTEXTO: com o free tier a ~19 s por chamada, a mesma pergunta feita de novo custava
// outros 19 s. Guardar a resposta por alguns minutos é o ganho mais barato que existe.
//
// REGRA DE SEGURANÇA: só entra no cache resposta que NÃO MEXEU em nada. Ferramenta que lê
// (buscar_web, ler_pagina) pode; ferramenta que escreve (criar_pasta, escrever_arquivo,
// agendar, mandar e-mail) NUNCA — repetir isso por cache seria mentir para o usuário
// dizendo "criei a pasta" sem ter criado.

import (
	"hash/fnv"
	"strings"
	"sync"
	"time"
)

// validadeCache é quanto tempo uma resposta fica válida.
const validadeCache = 10 * time.Minute

type entradaCache struct {
	Texto       string
	Quando      time.Time
	Ferramentas []string
}

var (
	rcMu   sync.Mutex
	rcMapa = map[string]entradaCache{}
)

// ferramentasSomenteLeitura são as que não alteram nada no mundo.
var ferramentasSomenteLeitura = map[string]bool{
	"buscar_web":  true,
	"ler_pagina":  true,
	"ler_arquivo": true,
	"listar":      true,
}

// semAcento troca letras acentuadas pelas equivalentes — gente escreve "cotação" e
// "cotacao" para a mesma pergunta, e as duas têm de bater no cache.
var trocasAcento = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// chaveCache normaliza a pergunta (caixa, espaço, acento) para acertar repetição trivial.
func chaveCache(pergunta string) string {
	p := strings.ToLower(strings.TrimSpace(pergunta))
	p = trocasAcento.Replace(p)
	p = strings.Join(strings.Fields(p), " ")
	h := fnv.New64a()
	_, _ = h.Write([]byte(p))
	return p + "|" + itoaUint64(h.Sum64())
}

func itoaUint64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// cachePodeGuardar diz se a resposta pode ser reaproveitada.
func cachePodeGuardar(ferramentas []string) bool {
	for _, f := range ferramentas {
		if !ferramentasSomenteLeitura[f] {
			return false // mexeu no mundo: não guardar
		}
	}
	return true
}

// GuardarResposta põe a resposta no cache (se for seguro).
func GuardarResposta(pergunta, texto string, ferramentas []string) {
	if strings.TrimSpace(texto) == "" || !cachePodeGuardar(ferramentas) {
		return
	}
	rcMu.Lock()
	defer rcMu.Unlock()
	rcMapa[chaveCache(pergunta)] = entradaCache{Texto: texto, Quando: time.Now(), Ferramentas: ferramentas}
}

// BuscarResposta devolve a resposta guardada, se ainda válida.
func BuscarResposta(pergunta string) (string, bool) {
	rcMu.Lock()
	defer rcMu.Unlock()
	k := chaveCache(pergunta)
	e, ok := rcMapa[k]
	if !ok || time.Since(e.Quando) > validadeCache {
		delete(rcMapa, k)
		return "", false
	}
	return e.Texto, true
}

// LimparCacheRespostas zera o cache (testes e botão "esquecer").
func LimparCacheRespostas() {
	rcMu.Lock()
	defer rcMu.Unlock()
	rcMapa = map[string]entradaCache{}
}
