package main

// harness_tela.go — ligação das chaves do harness com a tela do PetAgent (30/10/2026).
//
// Métodos em *App ficam disponíveis ao frontend pelo Wails (são os mesmos que o painel de
// Inteligência já usa em inteligencia.go). Regras:
//   - a chave NUNCA volta inteira para a tela: só máscara;
//   - salvar cifra com DPAPI antes de tocar no disco;
//   - testar faz chamada REAL e devolve o resultado em texto, para a pessoa saber na hora
//     se colou a chave certa (foi assim que descobrimos chave errada e cota estourada).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// cofreDoApp abre o cofre de chaves do harness na pasta do app.
func cofreDoApp() *CofreChaves {
	return AbrirCofre("")
}

// ChavesHarness devolve o estado das chaves para a tela (id, nome, configurada, máscara,
// página onde criar, prefixo esperado, ajuda).
func (a *App) ChavesHarness() string {
	b, err := json.Marshal(cofreDoApp().EstadoChaves())
	if err != nil {
		return "[]"
	}
	return string(b)
}

// AdicionarChaveHarness inclui uma chave na lista do provedor. Devolve "" ou o erro.
func (a *App) AdicionarChaveHarness(id, valor string) string {
	if err := cofreDoApp().AdicionarChave(ChaveProvedor(strings.TrimSpace(id)), valor); err != nil {
		return err.Error()
	}
	return ""
}

// RemoverChaveHarness tira a chave na posição indicada (0-based).
func (a *App) RemoverChaveHarness(id string, indice int) string {
	if err := cofreDoApp().RemoverChave(ChaveProvedor(strings.TrimSpace(id)), indice); err != nil {
		return err.Error()
	}
	return ""
}

// ResumoChavesHarness devolve "3 chaves grátis · 1 em espera" para o cabeçalho.
func (a *App) ResumoChavesHarness() string {
	c := cofreDoApp()
	total := c.TotalChaves()
	if total == 0 {
		return "nenhuma chave configurada"
	}
	espera := 0
	for _, item := range c.EstadoChaves() {
		if n, ok := item["emEspera"].(int); ok {
			espera += n
		}
	}
	if espera == 0 {
		return fmt.Sprintf("%d chave(s) grátis em uso", total)
	}
	return fmt.Sprintf("%d chave(s) grátis · %d em espera (cota)", total, espera)
}

// SalvarChaveHarness grava uma chave cifrada. Devolve "" em caso de sucesso ou a mensagem
// de erro (o frontend mostra a string).
func (a *App) SalvarChaveHarness(id, valor string) string {
	c := cofreDoApp()
	if err := c.Definir(ChaveProvedor(strings.TrimSpace(id)), valor); err != nil {
		return err.Error()
	}
	return ""
}

// ApagarChaveHarness remove a chave (mesma string de retorno do salvar).
func (a *App) ApagarChaveHarness(id string) string {
	c := cofreDoApp()
	if err := c.Definir(ChaveProvedor(strings.TrimSpace(id)), ""); err != nil {
		return err.Error()
	}
	return ""
}

// SalvarSearxHarness grava a URL de um SearxNG próprio (opcional; sem chave).
func (a *App) SalvarSearxHarness(url string) string {
	if err := cofreDoApp().DefinirSearx(url); err != nil {
		return err.Error()
	}
	return ""
}

// SearxHarness devolve a URL configurada.
func (a *App) SearxHarness() string { return cofreDoApp().Searx() }

// ModeloHarness devolve o modelo configurado para o agente nativo.
func (a *App) ModeloHarness() string { return cofreDoApp().ModeloPadrao() }

// SalvarModeloHarness grava o modelo do agente nativo ("" volta ao padrão medido).
func (a *App) SalvarModeloHarness(modelo string) string {
	if err := cofreDoApp().DefinirModeloPadrao(modelo); err != nil {
		return err.Error()
	}
	return ""
}

// TestarChaveHarness faz uma chamada real e devolve o resultado legível.
// Não gasta token de geração: lista modelos (NVIDIA/Google) ou faz uma busca de 1 termo
// (Brave).
func (a *App) TestarChaveHarness(id string, indice int) string {
	c := cofreDoApp()
	p := ChaveProvedor(strings.TrimSpace(id))
	lista := c.Chaves(p)
	var chave string
	if indice >= 0 && indice < len(lista) {
		chave = lista[indice]
	}
	if chave == "" {
		return "essa chave ainda não foi configurada"
	}
	cli := &http.Client{Timeout: 20 * time.Second}
	switch p {
	case ChaveNVIDIA:
		return testaListaModelos(cli, "https://integrate.api.nvidia.com/v1/models", chave, "Bearer ", "nvapi-")
	case ChaveGemini:
		return testaListaModelos(cli, "https://generativelanguage.googleapis.com/v1beta/models?key="+chave, "", "", "AIza")
	case ChaveBrave:
		return testaBrave(cli, chave)
	}
	return "provedor desconhecido"
}

func testaListaModelos(cli *http.Client, url, chave, esquema, prefixo string) string {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "erro: " + err.Error()
	}
	if esquema != "" {
		req.Header.Set("Authorization", esquema+chave)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		return "não consegui conectar: " + err.Error()
	}
	defer resp.Body.Close()
	corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Sprintf("a chave foi recusada (HTTP %d). Confira se copiou inteira e se começa com %q.", resp.StatusCode, prefixo)
	}
	n := strings.Count(string(corpo), "\"id\"")
	if n == 0 {
		n = strings.Count(string(corpo), "\"name\"")
	}
	return fmt.Sprintf("chave válida — %d modelos disponíveis", n)
}

func testaBrave(cli *http.Client, chave string) string {
	return testaBraveComURL(cli, "https://api.search.brave.com/res/v1/web/search?q=teste&count=1", chave)
}

// testaBraveComURL permite apontar para servidor falso em teste.
func testaBraveComURL(cli *http.Client, url, chave string) string {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "erro: " + err.Error()
	}
	req.Header.Set("X-Subscription-Token", chave)
	req.Header.Set("Accept", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		return "não consegui conectar: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Sprintf("a chave foi recusada (HTTP %d)", resp.StatusCode)
	}
	restante := resp.Header.Get("x-ratelimit-remaining")
	if restante != "" {
		return "chave válida — busca funcionando (cota restante neste minuto: " + restante + ")"
	}
	return "chave válida — busca funcionando"
}
