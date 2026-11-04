package main

// harness_ferramentas.go — bloco 1 do harness nativo do PetAgent (30/10/2026).
//
// POR QUE EXISTE: hoje o PetAgent delega o trabalho ao motor do Hermes (chat_bridge.py
// sobe hermes_cli.main com HERMES_HOME espelhado). Isso funciona, mas amarra o app a ter
// um Hermes instalado na máquina — o que impede distribuir o PetAgent para quem só quer
// usar chave grátis (Google/NVIDIA) sem instalar nada além do próprio app.
//
// Este arquivo NÃO substitui o Hermes. Ele é a base do "motor nativo": as ferramentas de
// arquivo que qualquer harness precisa, com raiz de trabalho isolada (sandbox), limites de
// tamanho e recusa de caminho que escape da raiz. O motor (loop de tool calling) vem depois
// e usa o Registro daqui.
//
// REGRAS QUE NÃO PODEM SER AFROUXADAS SEM TESTE:
//   - caminho nunca sai da raiz (inclusive via "..", caminho absoluto ou symlink);
//   - escrita é atômica (.tmp + Rename), como em conversas.go;
//   - arquivo binário não é lido como texto (o modelo receberia lixo);
//   - tamanho de leitura e escrita é limitado (protege contexto e disco).

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	// Limite de leitura: o conteúdo vai direto para o contexto do modelo. 256 KB de texto
	// já é muita coisa; acima disso o certo é o agente ler por partes.
	harnessMaxLeitura = 256 * 1024
	// Limite de escrita por chamada. Arquivo maior que isso deve ser escrito em partes
	// (o harness de saída longa faz a continuação).
	harnessMaxEscrita = 1024 * 1024
	// Quantos itens uma listagem devolve antes de resumir.
	harnessMaxItensListagem = 400
)

// Ferramenta é uma capacidade que o modelo pode pedir. Os campos de schema são os que
// vão no JSON enviado ao provedor (formato OpenAI: type=function / function.parameters).
type Ferramenta struct {
	Nome       string
	Descricao  string
	Parametros map[string]any // JSON Schema dos argumentos
	Executar   func(args map[string]any) (string, error)
}

// Registro guarda as ferramentas disponíveis para o motor nativo.
type Registro struct {
	mu      sync.RWMutex
	porNome map[string]*Ferrata
}

// Ferrata é o par ferramenta+estado (mantido simples: sem estado por enquanto).
type Ferrata struct {
	F *Ferramenta
}

// NovoRegistro cria um registro vazio.
func NovoRegistro() *Registro {
	return &Registro{porNome: map[string]*Ferrata{}}
}

// Registrar adiciona (ou substitui) uma ferramenta no registro.
func (r *Registro) Registrar(f *Ferramenta) {
	if f == nil || f.Nome == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.porNome[f.Nome] = &Ferrata{F: f}
}

// Nomes devolve os nomes registrados, em ordem estável (bom para log e para teste).
func (r *Registro) Nomes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ns := make([]string, 0, len(r.porNome))
	for n := range r.porNome {
		ns = append(ns, n)
	}
	sort.Strings(ns)
	return ns
}

// Executar roda uma ferramenta pelo nome, devolvendo o texto que o modelo vai ler.
func (r *Registro) Executar(nome string, args map[string]any) (string, error) {
	r.mu.RLock()
	fe, ok := r.porNome[nome]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("ferramenta desconhecida: %s", nome)
	}
	return fe.F.Executar(args)
}

// EsquemaOpenAI devolve as ferramentas no formato que o provedor espera
// (OpenAI-compatível: NVIDIA/Groq/OpenRouter/DeepSeek). O adaptador do Gemini
// converte este mesmo esquema para functionDeclarations.
func (r *Registro) EsquemaOpenAI() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nomes := make([]string, 0, len(r.porNome))
	for n := range r.porNome {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)
	out := make([]map[string]any, 0, len(nomes))
	for _, n := range nomes {
		f := r.porNome[n].F
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        f.Nome,
				"description": f.Descricao,
				"parameters":  f.Parametros,
			},
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Sandbox de caminho
// ---------------------------------------------------------------------------

// RaizTrabalho devolve a pasta onde o harness pode escrever. Fica separada da pasta de
// configuração do app (%LOCALAPPDATA%\petagent) para o agente não sobrescrever
// configuração nem memória por acidente.
func RaizTrabalho() string {
	return filepath.Join(pastaPetAgente(), "workspace")
}

// resolverCaminho transforma o argumento do modelo em caminho absoluto DENTRO da raiz.
// Devolve erro se o caminho escapar da raiz de qualquer forma.
func resolverCaminho(raiz, pedido string) (string, error) {
	if raiz == "" {
		return "", errors.New("raiz de trabalho vazia")
	}
	raizAbs, err := filepath.Abs(raiz)
	if err != nil {
		return "", fmt.Errorf("raiz inválida: %w", err)
	}
	pedido = strings.TrimSpace(pedido)
	if pedido == "" {
		return "", errors.New("caminho vazio")
	}
	// Tira barra inicial para tratar tudo como relativo à raiz.
	pedido = strings.TrimPrefix(pedido, "/")
	pedido = strings.TrimPrefix(pedido, `\`)
	alvo := filepath.Join(raizAbs, pedido)
	alvo, err = filepath.Abs(filepath.Clean(alvo))
	if err != nil {
		return "", err
	}
	// Comparação com separador: /raiz2 não pode passar por /raiz.
	if alvo != raizAbs && !strings.HasPrefix(alvo, raizAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("caminho fora da área de trabalho: %s", pedido)
	}
	// Symlink que aponta para fora também é recusado (quando o caminho já existe).
	//
	// ARMADILHA DO WINDOWS (30/10/2026): EvalSymlinks devolve o caminho LONGO, mas a raiz
	// pode vir em nome curto 8.3 (ex.: C:\Users\WINDOWS~1\AppData\Local\Temp). Comparar o
	// resolvido do alvo com a raiz NÃO resolvida acusava "fora da área" em toda escrita.
	// Por isso os dois lados são resolvidos antes de comparar.
	raizReal := raizAbs
	if r, err := filepath.EvalSymlinks(raizAbs); err == nil {
		if abs, err := filepath.Abs(r); err == nil {
			raizReal = abs
		}
	}
	if real, err := filepath.EvalSymlinks(alvo); err == nil {
		realAbs, _ := filepath.Abs(real)
		if realAbs != raizReal && !strings.HasPrefix(realAbs, raizReal+string(os.PathSeparator)) {
			return "", fmt.Errorf("caminho resolve para fora da área de trabalho: %s", pedido)
		}
	}
	return alvo, nil
}

// relativo devolve o caminho relativo à raiz, para a resposta ficar estável e sem
// vazar o caminho do usuário para o modelo.
func relativo(raiz, abs string) string {
	if r, err := filepath.Rel(raiz, abs); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(abs)
}

// ---------------------------------------------------------------------------
// Ferramentas de arquivo
// ---------------------------------------------------------------------------

// ehBinario detecta conteúdo que não é texto (NUL nos primeiros bytes ou UTF-8 inválido).
func ehBinario(b []byte) bool {
	amostra := b
	if len(amostra) > 8192 {
		amostra = amostra[:8192]
	}
	for _, c := range amostra {
		if c == 0 {
			return true
		}
	}
	return !utf8.Valid(amostra)
}

// escaparParaModelo devolve o conteúdo de um arquivo pronto para ir ao contexto.
func escaparParaModelo(caminho, conteudo string) string {
	conteudo = strings.ReplaceAll(conteudo, "\r\n", "\n")
	return fmt.Sprintf("arquivo: %s\n---\n%s", caminho, conteudo)
}

func argTexto(args map[string]any, chave string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[chave]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func argBool(args map[string]any, chave string) bool {
	if args == nil {
		return false
	}
	if v, ok := args[chave].(bool); ok {
		return v
	}
	return false
}

// argInt lê um inteiro do argumento aceitando o que o modelo (ou o Go) mandar:
// JSON decodificado vem float64, chamada interna pode vir int, e alguns modelos
// mandam número como string ("10"). Sem isso o parâmetro era ignorado em silêncio.
func argInt(args map[string]any, chave string, padrao int) int {
	if args == nil {
		return padrao
	}
	switch v := args[chave].(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil {
			return n
		}
	}
	return padrao
}

func schemaObjeto(props map[string]any, obrigatorios ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(obrigatorios) > 0 {
		s["required"] = obrigatorios
	}
	return s
}

func schemaString(descricao string) map[string]any {
	return map[string]any{"type": "string", "description": descricao}
}

// RegistrarFerramentasArquivo registra as ferramentas de arquivo no registro, todas
// presas à raiz informada.
func RegistrarFerramentasArquivo(r *Registro, raiz string) {
	r.Registrar(&Ferramenta{
		Nome:      "listar_pasta",
		Descricao: "Lista arquivos e subpastas de uma pasta da área de trabalho. Caminho vazio lista a raiz.",
		Parametros: schemaObjeto(map[string]any{
			"caminho": schemaString("Pasta relativa à área de trabalho (vazio = raiz)"),
		}),
		Executar: func(args map[string]any) (string, error) {
			abs, err := resolverCaminho(raiz, argTexto(args, "caminho"))
			if err != nil {
				return "", err
			}
			itens, err := os.ReadDir(abs)
			if err != nil {
				return "", fmt.Errorf("não consegui listar: %w", err)
			}
			if len(itens) == 0 {
				return "pasta vazia", nil
			}
			var b strings.Builder
			b.WriteString(fmt.Sprintf("%d item(ns) em %s:\n", len(itens), relativo(raiz, abs)))
			for i, it := range itens {
				if i >= harnessMaxItensListagem {
					b.WriteString(fmt.Sprintf("... (+%d itens não listados)\n", len(itens)-i))
					break
				}
				info, errInfo := it.Info()
				marca := "arquivo"
				tam := int64(0)
				if it.IsDir() {
					marca = "pasta"
				} else if errInfo == nil {
					tam = info.Size()
				}
				if it.IsDir() {
					b.WriteString(fmt.Sprintf("- [%s] %s\n", marca, it.Name()))
				} else {
					b.WriteString(fmt.Sprintf("- [%s] %s (%d bytes)\n", marca, it.Name(), tam))
				}
			}
			return b.String(), nil
		},
	})

	r.Registrar(&Ferramenta{
		Nome:      "ler_arquivo",
		Descricao: "Lê um arquivo de texto da área de trabalho e devolve o conteúdo.",
		Parametros: schemaObjeto(map[string]any{
			"caminho": schemaString("Arquivo relativo à área de trabalho"),
			"inicio":  map[string]any{"type": "integer", "description": "Linha inicial (1 = começo)"},
			"limite":  map[string]any{"type": "integer", "description": "Quantas linhas ler (padrão 400)"},
		}, "caminho"),
		Executar: func(args map[string]any) (string, error) {
			abs, err := resolverCaminho(raiz, argTexto(args, "caminho"))
			if err != nil {
				return "", err
			}
			f, err := os.Open(abs)
			if err != nil {
				return "", fmt.Errorf("não consegui abrir: %w", err)
			}
			defer f.Close()
			cabeca := make([]byte, 8192)
			n, _ := io.ReadFull(f, cabeca)
			if ehBinario(cabeca[:n]) {
				return "", fmt.Errorf("%s parece binário (não leio como texto)", argTexto(args, "caminho"))
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return "", err
			}
			dados, err := io.ReadAll(io.LimitReader(f, harnessMaxLeitura+1))
			if err != nil {
				return "", err
			}
			aviso := ""
			if len(dados) > harnessMaxLeitura {
				dados = dados[:harnessMaxLeitura]
				aviso = "\n[aviso: arquivo maior que o limite; leia por partes usando inicio/limite]"
			}
			linhas := strings.Split(strings.ReplaceAll(string(dados), "\r\n", "\n"), "\n")
			inicio := argInt(args, "inicio", 1)
			if inicio < 1 {
				inicio = 1
			}
			limite := argInt(args, "limite", 400)
			if limite < 1 {
				limite = 400
			}
			if inicio > len(linhas) {
				return fmt.Sprintf("arquivo tem %d linhas; início pedido (%d) passa do fim", len(linhas), inicio), nil
			}
			fim := inicio - 1 + limite
			if fim > len(linhas) {
				fim = len(linhas)
			}
			var b strings.Builder
			b.WriteString(fmt.Sprintf("%s (linhas %d-%d de %d)%s\n---\n", relativo(raiz, abs), inicio, fim, len(linhas), aviso))
			for i := inicio - 1; i < fim; i++ {
				b.WriteString(linhas[i])
				b.WriteString("\n")
			}
			return b.String(), nil
		},
	})

	r.Registrar(&Ferramenta{
		Nome:      "escrever_arquivo",
		Descricao: "Cria ou sobrescreve um arquivo de texto na área de trabalho. Cria as pastas necessárias.",
		Parametros: schemaObjeto(map[string]any{
			"caminho":  schemaString("Arquivo relativo à área de trabalho"),
			"conteudo": schemaString("Conteúdo completo do arquivo"),
			"anexar":   map[string]any{"type": "boolean", "description": "true adiciona ao fim em vez de sobrescrever"},
		}, "caminho", "conteudo"),
		Executar: func(args map[string]any) (string, error) {
			abs, err := resolverCaminho(raiz, argTexto(args, "caminho"))
			if err != nil {
				return "", err
			}
			conteudo := argTexto(args, "conteudo")
			if len(conteudo) > harnessMaxEscrita {
				return "", fmt.Errorf("conteúdo grande demais (%d bytes; limite %d) — escreva em partes", len(conteudo), harnessMaxEscrita)
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return "", fmt.Errorf("não consegui criar a pasta: %w", err)
			}
			modo := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
			if argBool(args, "anexar") {
				modo = os.O_CREATE | os.O_WRONLY | os.O_APPEND
			}
			tmp := abs + ".tmp"
			if !argBool(args, "anexar") {
				// escrita atômica: .tmp + Rename (mesmo padrão de conversas.go)
				if err := os.WriteFile(tmp, []byte(conteudo), 0o644); err != nil {
					return "", fmt.Errorf("não consegui escrever: %w", err)
				}
				if err := os.Rename(tmp, abs); err != nil {
					_ = os.Remove(tmp)
					return "", fmt.Errorf("não consegui gravar: %w", err)
				}
			} else {
				f, err := os.OpenFile(abs, modo, 0o644)
				if err != nil {
					return "", fmt.Errorf("não consegui abrir para anexar: %w", err)
				}
				if _, err := f.WriteString(conteudo); err != nil {
					f.Close()
					return "", fmt.Errorf("não consegui anexar: %w", err)
				}
				f.Close()
			}
			info, _ := os.Stat(abs)
			tam := int64(0)
			if info != nil {
				tam = info.Size()
			}
			return fmt.Sprintf("ok: %s gravado (%d bytes)", relativo(raiz, abs), tam), nil
		},
	})

	r.Registrar(&Ferramenta{
		Nome:      "criar_pasta",
		Descricao: "Cria uma pasta (e as pastas acima dela) na área de trabalho.",
		Parametros: schemaObjeto(map[string]any{
			"caminho": schemaString("Pasta relativa à área de trabalho"),
		}, "caminho"),
		Executar: func(args map[string]any) (string, error) {
			abs, err := resolverCaminho(raiz, argTexto(args, "caminho"))
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return "", fmt.Errorf("não consegui criar a pasta: %w", err)
			}
			return "ok: pasta pronta: " + relativo(raiz, abs), nil
		},
	})

	r.Registrar(&Ferramenta{
		Nome:      "substituir_trecho",
		Descricao: "Substitui um trecho exato de um arquivo existente. Use para editar código sem reescrever tudo.",
		Parametros: schemaObjeto(map[string]any{
			"caminho": schemaString("Arquivo relativo à área de trabalho"),
			"antigo":  schemaString("Texto exato que existe hoje no arquivo"),
			"novo":    schemaString("Texto que entra no lugar"),
		}, "caminho", "antigo", "novo"),
		Executar: func(args map[string]any) (string, error) {
			abs, err := resolverCaminho(raiz, argTexto(args, "caminho"))
			if err != nil {
				return "", err
			}
			antigo := argTexto(args, "antigo")
			if antigo == "" {
				return "", errors.New("trecho antigo vazio")
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return "", fmt.Errorf("não consegui ler: %w", err)
			}
			txt := string(b)
			n := strings.Count(txt, antigo)
			if n == 0 {
				return "", errors.New("trecho não encontrado no arquivo")
			}
			if n > 1 {
				return "", fmt.Errorf("trecho aparece %d vezes; seja mais específico", n)
			}
			novo := strings.Replace(txt, antigo, argTexto(args, "novo"), 1)
			tmp := abs + ".tmp"
			if err := os.WriteFile(tmp, []byte(novo), 0o644); err != nil {
				return "", err
			}
			if err := os.Rename(tmp, abs); err != nil {
				_ = os.Remove(tmp)
				return "", err
			}
			return "ok: trecho substituído em " + relativo(raiz, abs), nil
		},
	})
}

// CatalogoFerramentasJSON é util para log/diagnóstico (e para o wizard mostrar o que o
// agente sabe fazer).
func (r *Registro) CatalogoFerramentasJSON() string {
	b, _ := json.MarshalIndent(r.EsquemaOpenAI(), "", "  ")
	return string(b)
}
