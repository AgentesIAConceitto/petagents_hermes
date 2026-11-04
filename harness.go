package main

// harness.go — ponto de entrada do motor nativo do PetAgent (30/10/2026).
//
// Junta o que existe hoje (blocos 1 e 2) num objeto só, para o loop de tool calling
// (próximo bloco) não precisar saber como as peças são montadas:
//
//	h := NovoHarness()
//	tools := h.Ferramentas()                  // vai no pedido ao provedor
//	prompt := h.ContextoParaPrompt(pergunta)   // memória + eventos recentes
//	saida, err := h.Registro.Executar(nome, args)
//	h.Memoria.Evento("ferramenta", nome)
//
// IMPORTANTE: nada aqui substitui o motor do Hermes. O modo integrado continua sendo o
// padrão enquanto o loop nativo não estiver pronto; este pacote é a fundação do modo
// "chave grátis sem instalar Hermes" (Google/NVIDIA).

import (
	"os"
	"path/filepath"
)

// Harness é o conjunto ferramentas + memória, preso a uma raiz de trabalho.
type Harness struct {
	Registro *Registro
	Memoria  *Memoria
	Cofre    *CofreChaves
	Agenda   *Agenda
	Raiz     string
}

// NovoHarness monta o harness usando as pastas padrão do app:
//
//	%LOCALAPPDATA%\petagent\workspace   → arquivos que o agente cria e lê
//	%LOCALAPPDATA%\petagent\memoria.json → fatos e eventos
func NovoHarness() *Harness {
	raiz := RaizTrabalho()
	_ = os.MkdirAll(raiz, 0o755)
	mem := AbrirMemoria(filepath.Join(pastaPetAgente(), "memoria.json"))
	cofre := AbrirCofre("")
	reg := NovoRegistro()
	RegistrarFerramentasArquivo(reg, raiz)
	RegistrarFerramentasMemoria(reg, mem)
	// as chaves vêm do que o usuário configurou no app (cai para env se ainda não configurou)
	RegistrarFerramentasDocumento(reg, raiz)
	RegistrarFerramentasWeb(reg, cofre.ConfigWeb())
	agenda := AbrirAgenda("")
	RegistrarFerramentasAgenda(reg, agenda, mem)
	return &Harness{Registro: reg, Memoria: mem, Cofre: cofre, Agenda: agenda, Raiz: raiz}
}

// NovoHarnessEm permite apontar outra raiz e outro arquivo de memória (usado em teste,
// ou quando o usuário escolhe a pasta de trabalho).
func NovoHarnessEm(raiz, arquivoMemoria string) *Harness {
	if raiz == "" {
		raiz = RaizTrabalho()
	}
	_ = os.MkdirAll(raiz, 0o755)
	mem := AbrirMemoria(arquivoMemoria)
	reg := NovoRegistro()
	RegistrarFerramentasArquivo(reg, raiz)
	RegistrarFerramentasMemoria(reg, mem)
	RegistrarFerramentasDocumento(reg, raiz)
	RegistrarFerramentasWeb(reg, ConfigWeb{})
	agenda := AbrirAgenda(filepath.Join(filepath.Dir(arquivoMemoria), "agenda.json"))
	RegistrarFerramentasAgenda(reg, agenda, mem)
	return &Harness{Registro: reg, Memoria: mem, Agenda: agenda, Raiz: raiz}
}

// Ferramentas devolve o esquema no formato OpenAI (o adaptador do Gemini converte).
func (h *Harness) Ferramentas() []map[string]any {
	if h == nil || h.Registro == nil {
		return nil
	}
	return h.Registro.EsquemaOpenAI()
}

// ContextoParaPrompt devolve memória relevante + eventos recentes para o pedido atual.
func (h *Harness) ContextoParaPrompt(consulta string) string {
	if h == nil || h.Memoria == nil {
		return ""
	}
	return h.Memoria.ContextoParaPrompt(consulta, 8, 6)
}

// Executar roda uma ferramenta e registra o uso na trilha de eventos.
func (h *Harness) Executar(nome string, args map[string]any) (string, error) {
	if h == nil || h.Registro == nil {
		return "", os.ErrInvalid
	}
	saida, err := h.Registro.Executar(nome, args)
	if h.Memoria != nil {
		if err != nil {
			h.Memoria.Evento("ferramenta", nome+" falhou: "+err.Error())
		} else {
			h.Memoria.Evento("ferramenta", nome+" ok")
		}
	}
	return saida, err
}
