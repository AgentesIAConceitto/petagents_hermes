package main

// Painel "Inteligência" (22/10/2026): escolher provedor, chave e modelo, testar e trocar
// — sem terminal. Pedido da dona: a PESSOA escolhe onde a escolha vale:
//
//   - "hermes": grava no Hermes de verdade (config.yaml + .env do dono). Vale para o boneco,
//     o Telegram, o terminal — tudo que usa aquele Hermes.
//   - "boneco": grava SÓ aqui (%LOCALAPPDATA%\petagent\inteligencia.json, chave cifrada por
//     DPAPI). A ponte monta o home espelhado do boneco com esse modelo e essa chave
//     (chat_bridge.py, _ia_do_boneco); o Hermes do dono não é tocado.
//
// ⚠️ NÃO usar `hermes config set` para gravar: o lançador do Hermes trata HERMES_HOME também
// como pasta de INSTALAÇÃO (medido 22/10/2026: com um home de teste ele tentou reinstalar as
// dependências lá dentro) e é lento. Aqui se troca só as linhas necessárias, com cópia de
// segurança e troca atômica do arquivo — o resto (comentários, outras chaves) fica intacto.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type provedorIA struct {
	ID      string `json:"id"`      // model.provider no Hermes
	Nome    string `json:"nome"`    // o que a pessoa lê
	Var     string `json:"var"`     // variável da chave no .env
	BaseURL string `json:"baseUrl"` // model.base_url
	Pagina  string `json:"pagina"`  // onde se cria a chave
	Prefixo string `json:"prefixo"` // começo típico da chave (só dica no campo)
	Saldo   bool   `json:"saldo"`   // o provedor informa saldo?
	// Complemento — extra opcional do Hermes sem o qual o provedor não funciona (ver
	// garanteComplemento). Vazio = só a biblioteca que já vem com o Hermes.
	Complemento string `json:"complemento,omitempty"`
	modelos     string // rota que lista os modelos
	teste   string // rota que valida a chave sem gastar token
}

// lista curta de propósito: os provedores de chave de API mais usados. Ids e variáveis
// são os do próprio Hermes (hermes_cli/auth.py, _REGISTRY_ROWS; a OpenRouter é tratada à
// parte lá, em runtime_provider_backends.py).
var provedoresIA = []provedorIA{
	{ID: "openrouter", Nome: "OpenRouter", Var: "OPENROUTER_API_KEY", BaseURL: "https://openrouter.ai/api/v1",
		Pagina: "https://openrouter.ai/settings/keys", Prefixo: "sk-or-", Saldo: true,
		modelos: "https://openrouter.ai/api/v1/models", teste: "https://openrouter.ai/api/v1/key"},
	{ID: "openai-api", Nome: "OpenAI", Var: "OPENAI_API_KEY", BaseURL: "https://api.openai.com/v1",
		Pagina: "https://platform.openai.com/api-keys", Prefixo: "sk-",
		modelos: "https://api.openai.com/v1/models"},
	{ID: "anthropic", Nome: "Anthropic (Claude)", Var: "ANTHROPIC_API_KEY", BaseURL: "https://api.anthropic.com",
		Pagina: "https://console.anthropic.com/settings/keys", Prefixo: "sk-ant-", Complemento: "anthropic",
		modelos: "https://api.anthropic.com/v1/models"},
	{ID: "gemini", Nome: "Google Gemini", Var: "GEMINI_API_KEY", BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		Pagina:  "https://aistudio.google.com/app/apikey",
		modelos: "https://generativelanguage.googleapis.com/v1beta/models"},
	{ID: "deepseek", Nome: "DeepSeek", Var: "DEEPSEEK_API_KEY", BaseURL: "https://api.deepseek.com/v1",
		Pagina: "https://platform.deepseek.com/api_keys", Prefixo: "sk-", Saldo: true,
		modelos: "https://api.deepseek.com/models"},
	{ID: "xai", Nome: "xAI (Grok)", Var: "XAI_API_KEY", BaseURL: "https://api.x.ai/v1",
		Pagina: "https://console.x.ai", Prefixo: "xai-",
		modelos: "https://api.x.ai/v1/models"},
}

func provedorPorID(id string) (provedorIA, bool) {
	for _, p := range provedoresIA {
		if p.ID == id {
			return p, true
		}
	}
	return provedorIA{}, false
}

// mascara — a chave NUNCA vai inteira para a tela: "sk-or-…a3f9".
func mascara(chave string) string {
	chave = strings.TrimSpace(chave)
	if chave == "" {
		return ""
	}
	if len(chave) < 12 {
		return "••••"
	}
	ini := 6
	if i := strings.LastIndex(chave[:10], "-"); i > 0 {
		ini = i + 1
	}
	return chave[:ini] + "…" + chave[len(chave)-4:]
}

// ── DPAPI: a chave do boneco fica cifrada no disco; só o mesmo usuário do Windows abre ──

var (
	crypt32              = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtect     = crypt32.NewProc("CryptProtectData")
	procCryptUnprotect   = crypt32.NewProc("CryptUnprotectData")
	procLocalFreeDPAPI   = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
	errDPAPI             = errors.New("DPAPI falhou")
	cryptprotectUIForbid = uintptr(0x1)
)

type dataBlob struct {
	tam  uint32
	dado *byte
}

func dpapi(proc *syscall.LazyProc, entrada []byte) ([]byte, error) {
	if len(entrada) == 0 {
		return nil, nil
	}
	in := dataBlob{uint32(len(entrada)), &entrada[0]}
	var out dataBlob
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, cryptprotectUIForbid, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, errDPAPI
	}
	defer procLocalFreeDPAPI.Call(uintptr(unsafe.Pointer(out.dado)))
	return append([]byte(nil), unsafe.Slice(out.dado, out.tam)...), nil
}

func cifra(texto string) (string, error) {
	b, err := dpapi(procCryptProtect, []byte(texto))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func decifra(b64 string) (string, error) {
	if b64 == "" {
		return "", nil
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	claro, err := dpapi(procCryptUnprotect, b)
	return string(claro), err
}

// ── a escolha guardada ──────────────────────────────────────────────────────────────

type iaSalva struct {
	Modo     string `json:"modo"` // "hermes" (padrão) | "boneco"
	Provedor string `json:"provedor,omitempty"`
	Modelo   string `json:"modelo,omitempty"`
	Chave    string `json:"chave,omitempty"` // DPAPI + base64
}

func arquivoIA() string { return filepath.Join(pastaPetAgente(), "inteligencia.json") }

func leIA() iaSalva {
	var s iaSalva
	if b, err := os.ReadFile(arquivoIA()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Modo != "boneco" {
		s.Modo = "hermes"
	}
	return s
}

func gravaIA(s iaSalva) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.MkdirAll(pastaPetAgente(), 0o755)
	return os.WriteFile(arquivoIA(), b, 0o600)
}

// ── o Hermes do dono: ler e trocar só as linhas necessárias ─────────────────────────

var reChaveModelo = regexp.MustCompile(`^(\s+)(default|model|provider|base_url|api_mode|api_key|api)\s*:\s*(.*)$`)

func semAspas(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return strings.Trim(v, `"'`)
}

// leModeloHermes — model.default e model.provider do config.yaml (aceita `model: "x"`).
func leModeloHermes(texto string) (modelo, provedor string) {
	dentro := false
	for _, l := range strings.Split(strings.ReplaceAll(texto, "\r\n", "\n"), "\n") {
		if l != "" && l[0] != ' ' && l[0] != '\t' && !strings.HasPrefix(l, "#") {
			nome, resto, _ := strings.Cut(l, ":")
			dentro = strings.TrimSpace(nome) == "model"
			if dentro && semAspas(resto) != "" {
				return semAspas(resto), ""
			}
			continue
		}
		if !dentro {
			continue
		}
		if m := reChaveModelo.FindStringSubmatch(l); m != nil && len(m[1]) <= 2 {
			switch m[2] {
			case "default", "model":
				if modelo == "" {
					modelo = semAspas(m[3])
				}
			case "provider":
				provedor = semAspas(m[3])
			}
		}
	}
	return modelo, provedor
}

// trocaModeloNoConfig — no bloco `model:`, põe default/provider/base_url novos e, SE O
// PROVEDOR MUDOU, tira o que prende ao antigo (api_mode, api_key/api fixos no config).
// Mesmo provedor = só o modelo troca. Comentários e o resto do arquivo ficam como estão. A ponte faz o MESMO no espelho (chat_bridge.py,
// _troca_modelo) — os dois andam juntos.
func trocaModeloNoConfig(texto, provedor, modelo, baseURL string) string {
	crlf := strings.Contains(texto, "\r\n")
	linhas := strings.Split(strings.ReplaceAll(texto, "\r\n", "\n"), "\n")
	novos := [][2]string{{"default", modelo}, {"provider", provedor}, {"base_url", baseURL}}
	_, provAntes := leModeloHermes(texto)
	trocouProvedor := provAntes != provedor
	var saida []string
	dentro, achou := false, false
	vistos := map[string]bool{}
	falta := func() {
		for _, kv := range novos {
			if !vistos[kv[0]] && kv[1] != "" {
				saida = append(saida, fmt.Sprintf("  %s: %q", kv[0], kv[1]))
			}
		}
	}
	for _, l := range linhas {
		if l != "" && l[0] != ' ' && l[0] != '\t' && !strings.HasPrefix(l, "#") {
			if dentro {
				falta()
			}
			nome, resto, _ := strings.Cut(l, ":")
			dentro = strings.TrimSpace(nome) == "model"
			if dentro {
				achou = true
				if semAspas(resto) != "" { // forma curta `model: "x"` vira bloco
					saida = append(saida, "model:")
					continue
				}
			}
			saida = append(saida, l)
			continue
		}
		if dentro {
			if m := reChaveModelo.FindStringSubmatch(l); m != nil && len(m[1]) <= 2 {
				k := m[2]
				if k == "model" {
					k = "default"
				}
				if k == "api_mode" || k == "api_key" || k == "api" {
					if !trocouProvedor {
						saida = append(saida, l)
					}
					continue
				}
				if vistos[k] {
					continue
				}
				vistos[k] = true
				for _, kv := range novos {
					if kv[0] == k && kv[1] != "" {
						saida = append(saida, fmt.Sprintf("%s%s: %q", m[1], k, kv[1]))
					}
				}
				continue
			}
		}
		saida = append(saida, l)
	}
	if dentro {
		falta()
	}
	if !achou {
		saida = append(saida, "", "model:")
		falta()
	}
	r := strings.Join(saida, "\n")
	if crlf {
		r = strings.ReplaceAll(r, "\n", "\r\n")
	}
	return r
}

// trocaChaveNoEnv — troca (ou acrescenta) só a linha `VAR=...` do .env.
func trocaChaveNoEnv(texto, variavel, chave string) string {
	crlf := strings.Contains(texto, "\r\n")
	linhas := strings.Split(strings.ReplaceAll(texto, "\r\n", "\n"), "\n")
	re := regexp.MustCompile(`^\s*(export\s+)?` + regexp.QuoteMeta(variavel) + `\s*=`)
	feito := false
	for i, l := range linhas {
		if re.MatchString(l) {
			if feito {
				linhas[i] = "\x00" // duplicada: sai
				continue
			}
			linhas[i], feito = variavel+"="+chave, true
		}
	}
	var saida []string
	for _, l := range linhas {
		if l != "\x00" {
			saida = append(saida, l)
		}
	}
	if !feito {
		if len(saida) > 0 && saida[len(saida)-1] == "" {
			saida = saida[:len(saida)-1]
		}
		saida = append(saida, variavel+"="+chave, "")
	}
	r := strings.Join(saida, "\n")
	if crlf {
		r = strings.ReplaceAll(r, "\n", "\r\n")
	}
	return r
}

// gravaComBackup — cópia .bak-petagent do arquivo anterior + troca atômica (temp + rename):
// quem estiver lendo (o Hermes) vê o arquivo velho ou o novo, nunca a metade.
func gravaComBackup(caminho, conteudo string) error {
	if antigo, err := os.ReadFile(caminho); err == nil {
		_ = os.WriteFile(caminho+".bak-petagent", antigo, 0o600)
	}
	tmp := caminho + ".petagent-tmp"
	if err := os.WriteFile(tmp, []byte(conteudo), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, caminho)
}

// ── o que vale agora ────────────────────────────────────────────────────────────────

type iaEfetiva struct {
	Modo     string
	Provedor string // id
	Modelo   string
	Chave    string
}

func lerArquivo(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// iaAgora — a configuração que o chat do boneco usa de fato.
func iaAgora() iaEfetiva {
	s := leIA()
	if s.Modo == "boneco" && s.Provedor != "" {
		chave, _ := decifra(s.Chave)
		return iaEfetiva{"boneco", s.Provedor, s.Modelo, chave}
	}
	modelo, prov := leModeloHermes(lerArquivo(filepath.Join(hermesDir(), "config.yaml")))
	e := iaEfetiva{Modo: "hermes", Provedor: prov, Modelo: modelo}
	if p, ok := provedorPorID(prov); ok {
		e.Chave = envKey(filepath.Join(hermesDir(), ".env"), p.Var)
	}
	return e
}

// envPonte — o que a ponte precisa para montar o espelho com a IA só do boneco. A chave
// vai por VARIÁVEL DE AMBIENTE do processo (nunca em linha de comando).
func envPonte() []string {
	e := iaAgora()
	if e.Modo != "boneco" || e.Chave == "" {
		return nil
	}
	p, ok := provedorPorID(e.Provedor)
	if !ok {
		return nil
	}
	cfg, _ := json.Marshal(map[string]string{"provedor": p.ID, "modelo": e.Modelo, "base_url": p.BaseURL, "var": p.Var})
	return []string{"PETAGENT_IA=" + string(cfg), "PETAGENT_IA_CHAVE=" + e.Chave}
}

// ── complemento do Hermes que o provedor exige ─────────────────────────────────────
//
// Medido em 22/10/2026: com a Anthropic escolhida no painel, o "Testar acesso" passava (a
// chave vale) e o chat respondia "(sem texto)" em 0,1 s. O Hermes só fala com a Anthropic
// pelo pacote `anthropic`, que é um EXTRA OPCIONAL dele — e a instalação automática que ele
// tenta no primeiro uso falha dentro do backend do boneco (HERMES_HOME espelhado: o lançador
// trata essa pasta como a instalação, ver o aviso no topo deste arquivo). Quem escolheu a
// Anthropic quer usá-la: o app instala o extra pelo comando oficial, no Hermes de verdade.
// Já instalado, o comando volta em ~1 s sem mexer em nada.

var complementoMu sync.Mutex // um `pm install` por vez (salvar + subir a ponte)

// instalaComplemento — `hermes pm install --extra <extra>` no Hermes do dono. instalou=true
// quando o extra não estava lá (processos do Hermes já abertos só o veem ao reiniciar).
// Variável para o teste trocar.
var instalaComplemento = func(extra string) (instalou bool, err error) {
	exe := caminhoHermes()
	if exe == "" {
		return false, errors.New("não encontrei o Hermes nesta máquina")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "pm", "install", "--extra", extra)
	cmd.Dir = hermesDir()
	// SEM o HERMES_HOME do espelho (se herdado): é ele que fazia a instalação falhar
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(v), "HERMES_HOME=") {
			env = append(env, v)
		}
	}
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	saida, err := cmd.CombinedOutput()
	txt := strings.TrimSpace(string(saida))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return false, errors.New("a instalação passou de 5 min")
		}
		if len(txt) > 300 {
			txt = "…" + txt[len(txt)-300:]
		}
		if txt == "" {
			txt = err.Error()
		}
		return false, errors.New(txt)
	}
	return strings.Contains(txt, "Installing"), nil
}

// garanteComplemento — instala o extra do provedor, se ele exigir um.
func garanteComplemento(p provedorIA) (instalou bool, err error) {
	if p.Complemento == "" {
		return false, nil
	}
	complementoMu.Lock()
	defer complementoMu.Unlock()
	t0 := time.Now()
	instalou, err = instalaComplemento(p.Complemento)
	switch {
	case err != nil:
		logMsg(fmt.Sprintf("ia: complemento %q do Hermes NÃO instalou: %v", p.Complemento, err))
	case instalou:
		logMsg(fmt.Sprintf("ia: complemento %q instalado no Hermes em %.0fs", p.Complemento, time.Since(t0).Seconds()))
	}
	return instalou, err
}

// ── rede: lista de modelos e teste de acesso (sem gastar token) ────────────────────

func pedidoIA(p provedorIA, url, chave string) (int, []byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, nil, err
	}
	switch p.ID {
	case "anthropic":
		req.Header.Set("x-api-key", chave)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		req.Header.Set("x-goog-api-key", chave)
	default:
		if chave != "" {
			req.Header.Set("Authorization", "Bearer "+chave)
		}
	}
	req.Header.Set("Accept", "application/json")
	cli := &http.Client{Timeout: 25 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, b, nil
}

// lerModelos — {"data":[{"id"}]} (OpenAI, OpenRouter, Anthropic, DeepSeek, xAI) ou
// {"models":[{"name":"models/x"}]} (Gemini).
func lerModelos(corpo []byte) []string {
	var d struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(corpo, &d) != nil {
		return nil
	}
	var ids []string
	for _, m := range d.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	for _, m := range d.Models {
		if m.Name != "" {
			ids = append(ids, strings.TrimPrefix(m.Name, "models/"))
		}
	}
	return ids
}

func respostaJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// chaveParaTeste — a chave digitada; vazia = a que já está salva para esse provedor.
func chaveParaTeste(prov provedorIA, digitada string) string {
	if c := strings.TrimSpace(digitada); c != "" {
		return c
	}
	s := leIA()
	if s.Modo == "boneco" && s.Provedor == prov.ID {
		if c, _ := decifra(s.Chave); c != "" {
			return c
		}
	}
	return envKey(filepath.Join(hermesDir(), ".env"), prov.Var)
}

// ── bindings (o painel) ────────────────────────────────────────────────────────────

// IAProvedores — a lista do seletor.
func (a *App) IAProvedores() string { return respostaJSON(provedoresIA) }

// IAAtual — o que o painel mostra ao abrir. Chaves só mascaradas.
func (a *App) IAAtual() string {
	s := leIA()
	modeloH, provH := leModeloHermes(lerArquivo(filepath.Join(hermesDir(), "config.yaml")))
	env := filepath.Join(hermesDir(), ".env")
	chavesH := map[string]string{}
	algumaChave := false
	for _, p := range provedoresIA {
		if c := envKey(env, p.Var); c != "" {
			chavesH[p.ID] = mascara(c)
			algumaChave = true
		}
	}
	chaveB, _ := decifra(s.Chave)
	if chaveB != "" {
		algumaChave = true
	}
	ef := iaAgora()
	nome := ef.Provedor
	if p, ok := provedorPorID(ef.Provedor); ok {
		nome = p.Nome
	}
	return respostaJSON(map[string]any{
		"modo":        s.Modo,
		"hermes":      map[string]any{"provedor": provH, "modelo": modeloH, "chaves": chavesH},
		"boneco":      map[string]any{"provedor": s.Provedor, "modelo": s.Modelo, "chave": mascara(chaveB)},
		"efetivo":     map[string]any{"provedor": ef.Provedor, "nome": nome, "modelo": ef.Modelo, "modo": ef.Modo},
		"temChave":    algumaChave,
		"hermesAchou": caminhoHermes() != "",
	})
}

// IAModelos — modelos do provedor (a OpenRouter lista sem chave).
func (a *App) IAModelos(provedor, chave string) string {
	p, ok := provedorPorID(provedor)
	if !ok {
		return respostaJSON(map[string]any{"ok": false, "erro": "provedor desconhecido"})
	}
	c := chaveParaTeste(p, chave)
	if c == "" && p.ID != "openrouter" {
		return respostaJSON(map[string]any{"ok": false, "erro": "informe a chave para ver os modelos"})
	}
	st, corpo, err := pedidoIA(p, p.modelos, c)
	if err != nil {
		return respostaJSON(map[string]any{"ok": false, "erro": "sem rede"})
	}
	if st != 200 {
		return respostaJSON(map[string]any{"ok": false, "erro": fmt.Sprintf("o provedor respondeu %d", st)})
	}
	return respostaJSON(map[string]any{"ok": true, "modelos": lerModelos(corpo)})
}

// IATestar — a chave funciona? Não gasta token: rota de listar modelos (ou /key na OpenRouter).
func (a *App) IATestar(provedor, chave string) string {
	p, ok := provedorPorID(provedor)
	if !ok {
		return respostaJSON(map[string]any{"ok": false, "msg": "provedor desconhecido"})
	}
	c := chaveParaTeste(p, chave)
	if c == "" {
		return respostaJSON(map[string]any{"ok": false, "msg": "falta a chave"})
	}
	url := p.teste
	if url == "" {
		url = p.modelos
	}
	st, _, err := pedidoIA(p, url, c)
	switch {
	case err != nil:
		return respostaJSON(map[string]any{"ok": false, "msg": "sem rede — confira a internet"})
	case st == 401 || st == 403 || st == 400:
		return respostaJSON(map[string]any{"ok": false, "msg": "chave recusada pelo " + p.Nome})
	case st != 200:
		return respostaJSON(map[string]any{"ok": false, "msg": fmt.Sprintf("o %s respondeu %d", p.Nome, st)})
	}
	saldo := ""
	if p.Saldo {
		saldo = saldoDoProvedor(p.ID, c)
	}
	return respostaJSON(map[string]any{"ok": true, "msg": "funcionando", "saldo": saldo})
}

// IASalvar — grava a escolha e reinicia a ponte. Chave vazia = manter a que já existe.
func (a *App) IASalvar(js string) string {
	var d struct {
		Modo     string `json:"modo"`
		Provedor string `json:"provedor"`
		Modelo   string `json:"modelo"`
		Chave    string `json:"chave"`
	}
	falha := func(m string) string { return respostaJSON(map[string]any{"ok": false, "erro": m}) }
	if json.Unmarshal([]byte(js), &d) != nil {
		return falha("pedido ilegível")
	}
	p, ok := provedorPorID(d.Provedor)
	if !ok {
		return falha("escolha um provedor")
	}
	d.Modelo = strings.TrimSpace(d.Modelo)
	if d.Modelo == "" {
		return falha("escolha um modelo")
	}
	chave := chaveParaTeste(p, d.Chave)
	if chave == "" {
		return falha("falta a chave do " + p.Nome)
	}
	// ANTES de gravar: se o complemento não instala, a IA que funciona hoje fica como está
	instalou, err := garanteComplemento(p)
	if err != nil {
		return falha("não consegui instalar no Hermes o complemento que o " + p.Nome +
			" exige (" + p.Complemento + "): " + err.Error())
	}
	salvo := respostaJSON(map[string]any{"ok": true, "instalou": instalou})
	if d.Modo == "boneco" {
		c, err := cifra(chave)
		if err != nil {
			return falha("não consegui proteger a chave (DPAPI)")
		}
		if err := gravaIA(iaSalva{Modo: "boneco", Provedor: p.ID, Modelo: d.Modelo, Chave: c}); err != nil {
			return falha("não consegui gravar: " + err.Error())
		}
		logMsg(fmt.Sprintf("ia: só o boneco agora usa %s / %s", p.ID, d.Modelo))
	} else {
		cfgPath, envPath := filepath.Join(hermesDir(), "config.yaml"), filepath.Join(hermesDir(), ".env")
		cfgTxt, err := os.ReadFile(cfgPath)
		if err != nil {
			return falha("não achei o config.yaml do Hermes")
		}
		modeloH, provH := leModeloHermes(string(cfgTxt))
		if modeloH == d.Modelo && provH == p.ID && strings.TrimSpace(d.Chave) == "" && envKey(envPath, p.Var) != "" {
			// nada muda no Hermes: só deixa de valer a escolha só do boneco (não regrava os arquivos dele)
			_ = gravaIA(iaSalva{Modo: "hermes"})
			logMsg("ia: o boneco voltou a usar a IA do Hermes (" + p.ID + " / " + d.Modelo + ")")
			depoisDeSalvarIA(a)
			return salvo
		}
		if err := gravaComBackup(cfgPath, trocaModeloNoConfig(string(cfgTxt), p.ID, d.Modelo, p.BaseURL)); err != nil {
			return falha("não consegui gravar o config.yaml do Hermes: " + err.Error())
		}
		// chave nova digitada — ou a do boneco, se o .env do Hermes ainda não tem a deste provedor
		if strings.TrimSpace(d.Chave) != "" || envKey(envPath, p.Var) == "" {
			if err := gravaComBackup(envPath, trocaChaveNoEnv(lerArquivo(envPath), p.Var, chave)); err != nil {
				return falha("não consegui gravar a chave no .env do Hermes: " + err.Error())
			}
		}
		_ = gravaIA(iaSalva{Modo: "hermes"}) // a escolha só do boneco deixa de valer
		logMsg(fmt.Sprintf("ia: Hermes inteiro agora usa %s / %s (backup em *.bak-petagent)", p.ID, d.Modelo))
	}
	depoisDeSalvarIA(a)
	return salvo
}

// depoisDeSalvarIA — relê o saldo e reinicia a ponte com a IA nova. Variável só para o
// teste trocar: reiniciar a ponte de verdade mexeria no estado global que outros testes usam.
var depoisDeSalvarIA = func(a *App) {
	go a.warmBalance()
	go a.reiniciaPonte()
}
