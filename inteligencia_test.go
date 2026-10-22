package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMascaraNuncaMostraAChave — a tela só vê começo e fim; chave curta vira bolinhas.
func TestMascaraNuncaMostraAChave(t *testing.T) {
	casos := map[string]string{
		"sk-or-v1-0123456789abcdefa3f9": "sk-or-v1-…a3f9",
		"sk-ant-api03-xyzxyzxyz9f2c":    "sk-ant-…9f2c",
		"AIzaSyD0123456789abc":          "AIzaSy…9abc",
		"curta":                         "••••",
		"":                              "",
	}
	for chave, quer := range casos {
		if got := mascara(chave); got != quer {
			t.Errorf("mascara(%q) = %q, queria %q", chave, got, quer)
		}
	}
}

// TestDPAPIIdaEVolta — a chave do boneco é guardada cifrada e volta igual.
func TestDPAPIIdaEVolta(t *testing.T) {
	segredo := "sk-or-v1-segredo-de-teste"
	c, err := cifra(segredo)
	if err != nil {
		t.Fatalf("cifra: %v", err)
	}
	if strings.Contains(c, "segredo") {
		t.Fatalf("o texto cifrado contém a chave em claro: %s", c)
	}
	d, err := decifra(c)
	if err != nil || d != segredo {
		t.Fatalf("decifra = %q, %v; queria %q", d, err, segredo)
	}
}

const configDono = `# config do dono
model:
  # modelo padrão (comentário do dono)
  default: "openai/gpt-6-luna"
  provider: "openrouter"
  base_url: "https://openrouter.ai/api/v1"
  api_mode: chat_completions
agent:
  reasoning_effort: medium
`

func TestLeModeloHermes(t *testing.T) {
	m, p := leModeloHermes(configDono)
	if m != "openai/gpt-6-luna" || p != "openrouter" {
		t.Fatalf("leModeloHermes = %q, %q", m, p)
	}
	m, p = leModeloHermes("model: \"curto\"\n")
	if m != "curto" || p != "" {
		t.Fatalf("forma curta: %q, %q", m, p)
	}
}

// TestTrocaModeloNoConfig — troca só o necessário e preserva comentários e o resto.
func TestTrocaModeloNoConfig(t *testing.T) {
	novo := trocaModeloNoConfig(configDono, "deepseek", "deepseek-chat", "https://api.deepseek.com/v1")
	for _, quer := range []string{`default: "deepseek-chat"`, `provider: "deepseek"`,
		`base_url: "https://api.deepseek.com/v1"`, "# modelo padrão (comentário do dono)",
		"reasoning_effort: medium", "# config do dono"} {
		if !strings.Contains(novo, quer) {
			t.Errorf("faltou %q em:\n%s", quer, novo)
		}
	}
	for _, nao := range []string{"gpt-6-luna", "openrouter", "api_mode"} {
		if strings.Contains(novo, nao) {
			t.Errorf("sobrou %q em:\n%s", nao, novo)
		}
	}
	if m, p := leModeloHermes(novo); m != "deepseek-chat" || p != "deepseek" {
		t.Errorf("relido: %q, %q", m, p)
	}
	// mesmo provedor: só o modelo troca (o api_mode do provedor fica)
	if c := trocaModeloNoConfig(configDono, "openrouter", "outro/modelo", "https://openrouter.ai/api/v1"); !strings.Contains(c, "api_mode: chat_completions") ||
		!strings.Contains(c, `default: "outro/modelo"`) {
		t.Errorf("mesmo provedor:\n%s", c)
	}
	// CRLF continua CRLF
	if c := trocaModeloNoConfig(strings.ReplaceAll(configDono, "\n", "\r\n"), "x", "y", ""); strings.Count(c, "\n") != strings.Count(c, "\r\n") {
		t.Errorf("perdeu o CRLF")
	}
	// sem bloco model: ganha um
	if c := trocaModeloNoConfig("agent:\n  x: 1\n", "p", "m", "u"); !strings.Contains(c, "model:\n  default: \"m\"") {
		t.Errorf("sem bloco model:\n%s", c)
	}
}

// TestTrocaChaveNoEnv — troca a linha da variável (mesmo com export/duplicada) ou acrescenta.
func TestTrocaChaveNoEnv(t *testing.T) {
	env := "OPENROUTER_API_KEY=velha\nTELEGRAM_BOT_TOKEN=tg\nexport OPENROUTER_API_KEY=dup\n"
	got := trocaChaveNoEnv(env, "OPENROUTER_API_KEY", "nova")
	if strings.Count(got, "OPENROUTER_API_KEY=") != 1 || !strings.Contains(got, "OPENROUTER_API_KEY=nova") ||
		!strings.Contains(got, "TELEGRAM_BOT_TOKEN=tg") || strings.Contains(got, "velha") {
		t.Errorf("troca:\n%s", got)
	}
	got = trocaChaveNoEnv("TELEGRAM_BOT_TOKEN=tg\n", "DEEPSEEK_API_KEY", "sk-x")
	if got != "TELEGRAM_BOT_TOKEN=tg\nDEEPSEEK_API_KEY=sk-x\n" {
		t.Errorf("acréscimo: %q", got)
	}
	if got = trocaChaveNoEnv("", "XAI_API_KEY", "xai-1"); !strings.Contains(got, "XAI_API_KEY=xai-1") {
		t.Errorf(".env vazio: %q", got)
	}
}

func TestLerModelos(t *testing.T) {
	if ids := lerModelos([]byte(`{"data":[{"id":"a"},{"id":"b"}]}`)); strings.Join(ids, ",") != "a,b" {
		t.Errorf("formato data: %v", ids)
	}
	if ids := lerModelos([]byte(`{"models":[{"name":"models/gemini-x"}]}`)); strings.Join(ids, ",") != "gemini-x" {
		t.Errorf("formato gemini: %v", ids)
	}
}

// TestProvedoresSemRepeticao — ids e variáveis únicos, todos com rota de modelos e página da chave.
func TestProvedoresSemRepeticao(t *testing.T) {
	ids, vars := map[string]bool{}, map[string]bool{}
	for _, p := range provedoresIA {
		if ids[p.ID] || vars[p.Var] {
			t.Errorf("repetido: %s / %s", p.ID, p.Var)
		}
		ids[p.ID], vars[p.Var] = true, true
		if p.modelos == "" || p.Pagina == "" || p.BaseURL == "" {
			t.Errorf("%s incompleto", p.ID)
		}
	}
}

// TestIASalvarIgualAoHermes — "igual ao Hermes" grava no Hermes (num LOCALAPPDATA de
// teste): troca provedor/modelo no config.yaml, põe a chave no .env sem perder o resto,
// deixa backup — e "só para o boneco" NÃO toca nos arquivos do Hermes.
func TestIASalvarIgualAoHermes(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	antes := depoisDeSalvarIA
	depoisDeSalvarIA = func(*App) {} // sem reiniciar a ponte de verdade
	t.Cleanup(func() { depoisDeSalvarIA = antes })
	h := filepath.Join(base, "hermes")
	if err := os.MkdirAll(h, 0o755); err != nil {
		t.Fatal(err)
	}
	escreve := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	escreve(filepath.Join(h, "config.yaml"), configDono)
	escreve(filepath.Join(h, ".env"), "OPENROUTER_API_KEY=sk-or-do-dono\nTELEGRAM_BOT_TOKEN=tg\n")
	a := &App{}

	// só para o boneco: o Hermes fica intacto
	r := a.IASalvar(`{"modo":"boneco","provedor":"deepseek","modelo":"deepseek-chat","chave":"sk-do-boneco-123456"}`)
	if !strings.Contains(r, `"ok":true`) {
		t.Fatalf("salvar boneco: %s", r)
	}
	if lerArquivo(filepath.Join(h, "config.yaml")) != configDono {
		t.Fatal("modo boneco mexeu no config.yaml do Hermes")
	}
	if e := iaAgora(); e.Modo != "boneco" || e.Chave != "sk-do-boneco-123456" || e.Modelo != "deepseek-chat" {
		t.Fatalf("efetiva no modo boneco: %+v", e)
	}

	// igual ao Hermes: grava lá
	r = a.IASalvar(`{"modo":"hermes","provedor":"anthropic","modelo":"claude-sonnet-5","chave":"sk-ant-nova-123456"}`)
	if !strings.Contains(r, `"ok":true`) {
		t.Fatalf("salvar hermes: %s", r)
	}
	cfg, env := lerArquivo(filepath.Join(h, "config.yaml")), lerArquivo(filepath.Join(h, ".env"))
	if m, p := leModeloHermes(cfg); m != "claude-sonnet-5" || p != "anthropic" {
		t.Errorf("config do Hermes: %q / %q", m, p)
	}
	for _, quer := range []string{"ANTHROPIC_API_KEY=sk-ant-nova-123456", "OPENROUTER_API_KEY=sk-or-do-dono", "TELEGRAM_BOT_TOKEN=tg"} {
		if !strings.Contains(env, quer) {
			t.Errorf(".env do Hermes sem %q:\n%s", quer, env)
		}
	}
	if lerArquivo(filepath.Join(h, "config.yaml.bak-petagent")) != configDono {
		t.Error("faltou o backup do config.yaml")
	}
	if e := iaAgora(); e.Modo != "hermes" || e.Provedor != "anthropic" || e.Chave != "sk-ant-nova-123456" {
		t.Errorf("efetiva no modo hermes: %+v", e)
	}
	// sem chave nenhuma para o provedor: recusa
	if r = a.IASalvar(`{"modo":"hermes","provedor":"xai","modelo":"grok-5","chave":""}`); !strings.Contains(r, "falta a chave") {
		t.Errorf("sem chave deveria recusar: %s", r)
	}
}
