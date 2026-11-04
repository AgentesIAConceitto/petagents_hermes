package main

// motor_chat.go — liga o motor próprio ao chat do app (30/10/2026).
//
// ATÉ AQUI o chat ia SEMPRE para o Hermes (app.go: chamarHermes). Ou seja: quem não tem
// Hermes instalado configurava a chave, via os painéis, e continuava sem conseguir
// conversar — a última peça que faltava para o modo grátis funcionar de verdade.
//
// A troca é feita no único ponto por onde TODA mensagem passa (enviaChat), então
// SendChat e SendChatEm (com conversa e anexos) ganham o modo nativo sem tocar no frontend.

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	natMu        sync.Mutex
	natHistorico = map[string][]Mensagem{}
)

const natMaxHistorico = 12

// modoNativoAtivo diz se o chat deve ser respondido pelo motor próprio.
func modoNativoAtivo(h *Harness) bool {
	if h == nil || h.Cofre == nil {
		return false
	}
	temChave := h.Cofre.TotalChaves() > 0
	modo, _ := ResolverModo(h.Cofre.ModoEscolhido(), detectarHermes(), temChave)
	return modo == ModoNativo
}

// respondeNativo responde pela chave grátis do usuário. Devolve (texto, tratado):
// tratado=false significa "não é o modo nativo, siga para o Hermes".
func (a *App) respondeNativo(conversa, texto string, anexos []map[string]any) (string, bool) {
	return respondeNativoEm(NovoHarness(), conversa, texto, anexos)
}

// respondeNativoEm é a versão testável (recebe o harness pronto).
func respondeNativoEm(h *Harness, conversa, texto string, anexos []map[string]any) (string, bool) {
	if !modoNativoAtivo(h) {
		return "", false
	}
	cadeia := h.Cofre.CadeiaDeChaves(h.Cofre.ModeloPadrao(), h.Cofre.ModeloPadrao())
	if cadeia.Vazia() {
		return "Ainda não consigo conversar: falta uma chave grátis.\n" +
			"Abra o menu › Sistema › Chaves grátis e configure a NVIDIA ou o Google (as duas têm plano gratuito).", true
	}

	// anexos: no modo grátis não há visão, então só texto/PDF entram como texto.
	conteudo := strings.TrimSpace(texto)
	for _, an := range anexos {
		nome, _ := an["nome"].(string)
		tipo, _ := an["tipo"].(string)
		b64, _ := an["base64"].(string)
		if tipo == "imagem" {
			conteudo += "\n\n[O usuário anexou a imagem " + nome + ", mas o modo grátis não lê imagem.]"
			continue
		}
		claro := b64
		if dec, err := base64.StdEncoding.DecodeString(b64); err == nil {
			claro = string(dec)
		}
		conteudo += "\n\n[Arquivo anexado: " + nome + "]\n" + cortarTexto(claro, 20000)
	}

	chave := conversa
	if chave == "" {
		chave = "atual"
	}
	hist := historicoDaConversa(chave)
	registraDiag("chat: conversa=" + chave + " mensagens_no_contexto=" + fmt.Sprint(len(hist)))

	motor := &Motor{Harness: h, Cadeia: cadeia, Conversa: chave}
	ctx, cancelar := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancelar()
	res, err := motor.Responder(ctx, hist, conteudo)
	if err != nil {
		if h.Memoria != nil {
			h.Memoria.Evento("erro", "chat nativo: "+err.Error())
		}
		switch {
		case err == ErroSemChave:
			return "Ainda não consigo conversar: falta uma chave grátis (menu › Sistema › Chaves grátis).", true
		case strings.Contains(err.Error(), "nenhum provedor"):
			// DISTINGUIR causa: cota (429) não é a mesma coisa que provedor instável (503/timeout).
			// Em 03/11/2026 o app disse "sua cota acabou" quando o Gemini estava devolvendo 503 —
			// mensagem errada faz o usuário caçar problema no lugar errado.
			detalhe := resumirErro(err)
			if strings.Contains(detalhe, "429") || strings.Contains(strings.ToLower(detalhe), "quota") {
				return "A cota da chave grátis acabou agora. Se você tiver uma segunda chave, " +
					"cadastre em Chaves grátis que eu passo a usar ela automaticamente.", true
			}
			return "O provedor grátis não respondeu agora (instabilidade do serviço, não é a sua chave).\n" +
				"Detalhe: " + cortarTexto(detalhe, 200) + "\nTente de novo em alguns segundos.", true
		}
		return "Não consegui responder agora: " + err.Error(), true
	}

	natMu.Lock()
	natHistorico[chave] = append(natHistorico[chave],
		Mensagem{Role: "user", Content: cortarTexto(conteudo, 4000)},
		Mensagem{Role: "assistant", Content: res.Texto})
	if n := len(natHistorico[chave]); n > natMaxHistorico {
		natHistorico[chave] = natHistorico[chave][n-natMaxHistorico:]
	}
	natMu.Unlock()

	if strings.HasPrefix(chave, "local-") {
		GuardaMensagensLocal(chave, conteudo, res.Texto)
	}

	return res.Texto, true
}

// EsquecerConversaNativa limpa o histórico em memória de uma conversa.
func (a *App) EsquecerConversaNativa(id string) string {
	natMu.Lock()
	defer natMu.Unlock()
	if id == "" {
		natHistorico = map[string][]Mensagem{}
		return ""
	}
	delete(natHistorico, id)
	return ""
}

// ChatNativo status para o cabeçalho do chat (quem está respondendo).
func (a *App) ChatNativo() map[string]any {
	h := NovoHarness()
	ativo := modoNativoAtivo(h)
	info := map[string]any{"ativo": ativo}
	if !ativo {
		return info
	}
	cli, modelo := h.Cofre.ClientePadrao()
	if cli != nil {
		info["provedor"] = cli.Nome()
	}
	info["modelo"] = modelo
	return info
}

// historicoDaConversa devolve SÓ o histórico da conversa pedida (04/11/2026).
//
// REGRA QUE O DONO COBROU: "cada conversa com o seu contexto". O contexto do diálogo é
// chaveado pelo id da conversa — nunca por um balde global. Conversa nova nasce com zero
// mensagens. Conversa antiga é relida do arquivo dela (e só dela).
func historicoDaConversa(chave string) []Mensagem {
	natMu.Lock()
	hist := append([]Mensagem(nil), natHistorico[chave]...)
	natMu.Unlock()
	if len(hist) == 0 && strings.HasPrefix(chave, "local-") {
		if c := LeConversaLocal(chave); c != nil {
			for _, m := range c.Mensagens {
				hist = append(hist, Mensagem{Role: m.De, Content: m.Texto})
			}
		}
	}
	if n := len(hist); n > natMaxHistorico {
		hist = hist[n-natMaxHistorico:]
	}
	return hist
}
