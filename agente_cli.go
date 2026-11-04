package main

// agente_cli.go — modo headless do agente nativo (30/10/2026).
//
// PARA QUE SERVE: provar, sem abrir janela, que o agente funciona numa máquina SEM Python e
// SEM Hermes. É o teste que o modo grátis promete e que a interface sozinha não demonstra.
//
// Uso:
//   petagent.exe agente "qual é o preço do dólar hoje?"     (usa as chaves do app)
//   petagent.exe agente --dir C:\teste "pergunta"            (usa uma pasta isolada)
//
// Chaves: usa o cofre do app; se o cofre estiver vazio, aceita NVIDIA_API_KEY /
// GEMINI_API_KEY / BRAVE_SEARCH_API_KEY do ambiente (é o caso de uma máquina limpa, onde a
// pessoa ainda não configurou nada pela tela).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// rodarAgenteCLI responde uma pergunta no terminal. Devolve o código de saída.
func rodarAgenteCLI(args []string) int {
	dir := ""
	var partes []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--dir" && i+1 < len(args) {
			dir = args[i+1]
			i++
			continue
		}
		partes = append(partes, args[i])
	}
	pergunta := strings.TrimSpace(strings.Join(partes, " "))
	if pergunta == "" {
		fmt.Println("uso: petagent agente [--dir PASTA] \"a sua pergunta\"")
		return 2
	}

	base := dir
	if base == "" {
		base = pastaPetAgente()
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		fmt.Println("não consegui criar a pasta de trabalho:", err)
		return 1
	}

	// PROVA DO AMBIENTE: o que esta máquina tem (ou não tem)
	fmt.Println("== ambiente ==")
	fmt.Printf("  python no PATH: %v\n", temNoPath("python") || temNoPath("python3") || temNoPath("py"))
	fmt.Printf("  hermes no PATH: %v\n", temNoPath("hermes"))
	if h := DetectarHermes(); h.Instalado {
		fmt.Printf("  Hermes instalado: sim (%s)\n", h.Versao)
	} else {
		fmt.Printf("  Hermes instalado: não (%s)\n", h.Motivo)
	}

	mem := AbrirMemoria(filepath.Join(base, "memoria.json"))
	cofre := AbrirCofre(filepath.Join(base, "harness_chaves.json"))
	// máquina limpa: sem cofre, aceita as chaves do ambiente
	if cofre.Obter(ChaveNVIDIA) == "" && os.Getenv("NVIDIA_API_KEY") != "" {
		_ = cofre.Definir(ChaveNVIDIA, os.Getenv("NVIDIA_API_KEY"))
	}
	if cofre.Obter(ChaveGemini) == "" && os.Getenv("GEMINI_API_KEY") != "" {
		_ = cofre.Definir(ChaveGemini, os.Getenv("GEMINI_API_KEY"))
	}
	if cofre.Obter(ChaveBrave) == "" && os.Getenv("BRAVE_SEARCH_API_KEY") != "" {
		_ = cofre.Definir(ChaveBrave, os.Getenv("BRAVE_SEARCH_API_KEY"))
	}

	cli, modelo := cofre.ClientePadrao()
	if cli == nil {
		fmt.Println("  chave grátis: NENHUMA (defina NVIDIA_API_KEY ou configure pela tela)")
		return 1
	}
	fmt.Printf("  chave grátis: %s (%s)\n", cli.Nome(), modelo)

	h := &Harness{Registro: NovoRegistro(), Memoria: mem, Cofre: cofre, Agenda: AbrirAgenda(filepath.Join(base, "agenda.json")), Raiz: base}
	RegistrarFerramentasArquivo(h.Registro, h.Raiz)
	RegistrarFerramentasMemoria(h.Registro, mem)
	RegistrarFerramentasDocumento(h.Registro, h.Raiz)
	RegistrarFerramentasWeb(h.Registro, cofre.ConfigWeb())
	RegistrarFerramentasAgenda(h.Registro, h.Agenda, mem)

	cadeia := NovaCadeia(ProvedorEscolhido{Modelo: modelo, Cliente: cli})
	// Medição de latência NÃO pode bloquear o atendimento: medir custa uma chamada inteira
	// (19 s no free tier) e o usuário estaria pagando isso antes de qualquer resposta.
	// Aqui só faz sentido medir quando há mais de um provedor para comparar — e em segundo
	// plano, para a PRÓXIMA pergunta já usar a ordem certa.
	if elos := cadeia.Elos(); len(elos) > 1 {
		go func() {
			defer func() { _ = recover() }()
			MedirProvedores(context.Background(), elos)
		}()
	}
	motor := &Motor{Harness: h, Cadeia: cadeia}
	motor.AoProgresso = func(e Etapa) {
		if e.Tipo == "ferramenta" || e.Tipo == "troca" {
			fmt.Printf("  · %s: %s\n", e.Tipo, e.Detalhe)
		}
	}

	fmt.Println("== pergunta ==")
	fmt.Println(" ", pergunta)
	inicio := time.Now()
	ctx, cancelar := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancelar()
	res, err := motor.Responder(ctx, nil, pergunta)
	if err != nil {
		fmt.Println("== erro ==")
		fmt.Println(" ", err)
		return 1
	}
	fmt.Println("== resposta ==")
	fmt.Println(" ", res.Texto)
	fmt.Printf("\n[%d volta(s) · %d chamada(s) · %s · %.1fs]\n",
		res.Voltas, res.Chamadas, strings.Join(res.Ferramentas, ","), time.Since(inicio).Seconds())
	return 0
}

// temNoPath diz se um executável está no PATH (sem executá-lo).
func temNoPath(nome string) bool {
	_, err := exec.LookPath(nome)
	return err == nil
}
