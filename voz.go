package main

// Conversar por áudio (22/10/2026). Pedido da dona: um botão no chat para gravar e mandar
// áudio — "a ideia é conversar por áudio com o chat".
//
// Caminho: o WebView grava (MediaRecorder; o Wails já libera o microfone sem diálogo —
// SetGlobalPermission(Allow) no frontend do Windows) → manda o áudio para cá → o Groq
// Whisper transcreve → o texto entra no chat como se tivesse sido digitado. A resposta é
// lida em voz alta pelo próprio WebView (vozes pt-BR do Windows), sem nada daqui.
//
// Por que o Groq e não o Hermes: o WebView2 não tem reconhecimento de voz (a Web Speech
// API não funciona nele) e o Hermes desta máquina não tem provedor de transcrição — o
// Whisper local instalaria pacotes no ambiente do Hermes do dono, o que o PetAgent não faz.
// A dona escolheu o Groq: chave grátis, ~1 s por frase, bom em português.
//
// A chave fica em %LOCALAPPDATA%\petagent\voz.json, cifrada por DPAPI. Arquivo PRÓPRIO de
// propósito: o painel Inteligência regrava o inteligencia.json inteiro a cada "Salvar" e
// levaria a chave de voz junto. Sem chave aqui, vale a GROQ_API_KEY do .env do Hermes.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	urlGroq          = "https://api.groq.com/openai/v1"
	modeloTranscreve = "whisper-large-v3-turbo"
	paginaChaveGroq  = "https://console.groq.com/keys"
	maxAudio         = 24 << 20 // o Groq recusa acima de 25 MB (≈ 25 min de opus)
)

type vozSalva struct {
	Chave string `json:"chave,omitempty"` // DPAPI + base64
}

func arquivoVoz() string { return filepath.Join(pastaPetAgente(), "voz.json") }

// chaveVoz — a chave do Groq: a do boneco, senão a do .env do Hermes.
func chaveVoz() string {
	var s vozSalva
	if b, err := os.ReadFile(arquivoVoz()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if c, _ := decifra(s.Chave); c != "" {
		return c
	}
	return envKey(filepath.Join(hermesDir(), ".env"), "GROQ_API_KEY")
}

// O Whisper "ouve" frases prontas no silêncio — as legendas com que foi treinado. Gravação
// muda que volta só com uma destas é tratada como "não ouvi nada", senão o boneco
// responderia a um "Obrigado." que ninguém disse.
var alucinacoesWhisper = []string{
	"legendas pela comunidade amara.org", "obrigado.", "obrigado por assistir.",
	"obrigada por assistir.", "tchau.", "tchau, tchau.", "e aí", "...", "você",
	"legenda adriana zanotto", "inscreva-se no canal",
}

func limpaTranscricao(t string) string {
	t = strings.TrimSpace(t)
	baixo := strings.ToLower(t)
	for _, a := range alucinacoesWhisper {
		if baixo == a || baixo == strings.TrimSuffix(a, ".") {
			return ""
		}
	}
	return t
}

// pedidoGroq — chamada autenticada ao Groq; o corpo da resposta volta inteiro.
func pedidoGroq(metodo, rota, chave, tipo string, corpo io.Reader) (int, []byte, error) {
	req, err := http.NewRequest(metodo, urlGroq+rota, corpo)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+chave)
	req.Header.Set("Accept", "application/json")
	if tipo != "" {
		req.Header.Set("Content-Type", tipo)
	}
	cli := &http.Client{Timeout: 60 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, b, nil
}

// erroGroq — o motivo que a pessoa lê, a partir do status do Groq.
func erroGroq(st int, corpo []byte) string {
	switch st {
	case 401, 403:
		return "A chave do Groq foi recusada. Confira a chave em Sistema › Inteligência."
	case 413:
		return "O áudio ficou grande demais. Grave um trecho mais curto."
	case 429:
		return "O Groq atingiu o limite de uso da conta gratuita. Tente novamente em instantes."
	}
	var d struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(corpo, &d) == nil && d.Error.Message != "" {
		return fmt.Sprintf("O Groq respondeu com erro (código %d): %s", st, d.Error.Message)
	}
	return fmt.Sprintf("O Groq respondeu com erro (código %d).", st)
}

// extensaoAudio — o Groq decide o formato pelo nome do arquivo.
func extensaoAudio(mime string) string {
	switch {
	case strings.Contains(mime, "ogg"):
		return "ogg"
	case strings.Contains(mime, "mp4"), strings.Contains(mime, "m4a"):
		return "m4a"
	case strings.Contains(mime, "wav"):
		return "wav"
	}
	return "webm" // o que o WebView2 grava (audio/webm;codecs=opus)
}

// ── bindings ──────────────────────────────────────────────────────────────────────

// VozAtual — o que o painel mostra: há chave? (só mascarada) e onde se cria uma.
func (a *App) VozAtual() string {
	c := chaveVoz()
	return respostaJSON(map[string]any{"temChave": c != "", "chave": mascara(c), "pagina": paginaChaveGroq})
}

// VozSalvar — testa a chave no Groq (rota de modelos: não gasta nada) e grava cifrada.
// Chave vazia = apagar a do boneco (volta a valer a do .env do Hermes, se houver).
func (a *App) VozSalvar(chave string) string {
	chave = strings.TrimSpace(chave)
	falha := func(m string) string { return respostaJSON(map[string]any{"ok": false, "erro": m}) }
	if chave == "" {
		_ = os.Remove(arquivoVoz())
		return respostaJSON(map[string]any{"ok": true})
	}
	st, corpo, err := pedidoGroq("GET", "/models", chave, "", nil)
	if err != nil {
		return falha("Sem conexão com a internet. Verifique a rede e tente novamente.")
	}
	if st != 200 {
		return falha(erroGroq(st, corpo))
	}
	c, err := cifra(chave)
	if err != nil {
		return falha("Não foi possível proteger a chave com a criptografia do Windows (DPAPI).")
	}
	b, _ := json.MarshalIndent(vozSalva{Chave: c}, "", "  ")
	_ = os.MkdirAll(pastaPetAgente(), 0o755)
	if err := os.WriteFile(arquivoVoz(), b, 0o600); err != nil {
		return falha("Não foi possível salvar a chave: " + err.Error())
	}
	logMsg("voz: chave do Groq salva")
	return respostaJSON(map[string]any{"ok": true})
}

// Transcrever — áudio gravado no WebView (base64) → texto. {"ok", "texto"} ou {"ok":false, "erro"}.
func (a *App) Transcrever(b64, mime string) string {
	falha := func(m string) string { return respostaJSON(map[string]any{"ok": false, "erro": m}) }
	chave := chaveVoz()
	if chave == "" {
		return respostaJSON(map[string]any{"ok": false, "semChave": true,
			"erro": "Para transcrever áudio, é preciso cadastrar uma chave do Groq."})
	}
	audio, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(audio) == 0 {
		return falha("O áudio chegou vazio. Grave novamente.")
	}
	if len(audio) > maxAudio {
		return falha(erroGroq(413, nil))
	}
	var corpo bytes.Buffer
	w := multipart.NewWriter(&corpo)
	arq, _ := w.CreateFormFile("file", "fala."+extensaoAudio(mime))
	_, _ = arq.Write(audio)
	_ = w.WriteField("model", modeloTranscreve)
	_ = w.WriteField("language", "pt")
	_ = w.WriteField("response_format", "json")
	_ = w.WriteField("temperature", "0")
	_ = w.Close()

	t0 := time.Now()
	st, resp, err := pedidoGroq("POST", "/audio/transcriptions", chave, w.FormDataContentType(), &corpo)
	if err != nil {
		logMsg("voz: Groq sem rede: " + err.Error())
		return falha("Sem conexão com a internet: não foi possível enviar o áudio.")
	}
	if st != 200 {
		logMsg(fmt.Sprintf("voz: Groq respondeu %d", st))
		return falha(erroGroq(st, resp))
	}
	var d struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(resp, &d)
	texto := limpaTranscricao(d.Text)
	logMsg(fmt.Sprintf("voz: %d KB transcritos em %.1fs (%d chars)", len(audio)>>10, time.Since(t0).Seconds(), len(texto)))
	return respostaJSON(map[string]any{"ok": true, "texto": texto})
}
