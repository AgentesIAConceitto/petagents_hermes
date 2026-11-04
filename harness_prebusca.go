package main

// harness_prebusca.go — busca antes de perguntar ao modelo (03/11/2026).
//
// POR QUE: no free tier cada chamada ao modelo custa ~19 s. O laço normal gasta DUAS:
//   1ª o modelo decide buscar; 2ª o modelo lê os resultados e responde.
// Em pergunta de consulta ("qual a cotação do dólar") isso é o dobro do tempo sem ganho
// nenhum — a busca podia ter sido feita antes, pelo próprio app.
//
// REGRA: só dispara quando o pedido é CLARAMENTE consulta (pergunta + assunto do mundo).
// Pedido de ação ("crie", "escreva", "agende", "apague") NUNCA entra aqui — esses precisam
// do modelo decidindo ferramenta, e prender a busca só atrapalharia.
//
// Se a busca falhar ou voltar vazia, segue o caminho normal (o laço de ferramentas).

import (
	"regexp"
	"strings"
)

var (
	// sinais de que é pergunta, não ordem
	rePerguntaInicio = regexp.MustCompile(`(?i)^\s*(qual|quais|quanto|quantos|quanta|quantas|quem|quando|onde|como está|como esta|me diga|me diz|sabe|você sabe|voce sabe|o que é|o que e)\b`)
	// assunto que costuma exigir dado de fora
	reAssuntoExterno = regexp.MustCompile(`(?i)\b(cotação|cotacao|dólar|dolar|euro|bitcoin|preço|preco|valor|clima|temperatura|previsão|previsao|notícia|noticia|notícias|noticias|placar|jogo|resultado|eleição|eleicao|prefeito|governador|presidente|população|populacao|pib|inflação|inflacao|ibge|hoje|agora|atual|ontem)\b`)
	// sinais de AÇÃO — proíbem a pré-busca
	// radicais, não formas: "criei", "criada", "escreveu" também são ação.
	reAcao = regexp.MustCompile(`(?i)\b(cri(e|a|o|ou|ei|ei)\w*|criad\w*|escrev\w*|salv\w*|apagu\w*|delet\w*|mov\w*|renome\w*|agend\w*|lembr\w*|mand\w*|envi\w*|abr(a|ir|iu)\w*|instal\w*|execut\w*|rod(e|ar|ou)\w*|corrij\w*|corrig\w*|refator\w*|resum\w*|traduz\w*|organiz\w*)\b`)
)

// pareceConsultaDados diz se vale buscar antes de chamar o modelo.
func pareceConsultaDados(pedido string) bool {
	p := strings.TrimSpace(pedido)
	if p == "" || len(p) > 240 {
		return false // pedido longo costuma ser tarefa, não consulta
	}
	if reAcao.MatchString(p) {
		return false
	}
	return rePerguntaInicio.MatchString(p) && reAssuntoExterno.MatchString(p)
}

// consultaParaBusca reduz a pergunta ao termo útil para a busca.
func consultaParaBusca(pedido string) string {
	p := strings.TrimSpace(pedido)
	p = strings.TrimSuffix(p, "?")
	p = strings.TrimSuffix(p, ".")
	// corta a moldura da pergunta, o buscador não precisa dela
	for _, prefixo := range []string{"me diga ", "me diz ", "você sabe ", "voce sabe ", "sabe "} {
		if strings.HasPrefix(strings.ToLower(p), prefixo) {
			p = p[len(prefixo):]
		}
	}
	return strings.TrimSpace(p)
}

// reConversa reconhece saudação/conversa curta: nesses turnos, mandar 10+ ferramentas (27 KB
// de schema) custa ~10 s a mais no free tier e só aumenta a superfície de falha. Medido em
// 03/11/2026 na VM: "Bom dia" com 10 ferramentas = 30,8 s; sem ferramentas = 21,4 s.
var reConversa = regexp.MustCompile(`(?i)^\s*(bom dia|boa tarde|boa noite|oi+|ol[áa]|e a[íi]|tudo bem|tudo bom|obrigad[oa]|valeu|ok|certo|beleza|tchau|at[ée] (logo|mais)|como vai)[\s!.,?]*$`)

// pareceConversa diz se o turno não precisa de ferramenta nenhuma.
func pareceConversa(pedido string) bool {
	p := strings.TrimSpace(pedido)
	if p == "" {
		return true
	}
	if reConversa.MatchString(p) {
		return true
	}
	// frase curta, sem verbo de ação e sem assunto de consulta: é conversa
	if len(p) <= 32 && !reAcao.MatchString(p) && !reAssuntoExterno.MatchString(p) {
		return true
	}
	return false
}
