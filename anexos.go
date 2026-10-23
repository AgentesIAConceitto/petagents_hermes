package main

// Anexos no chat (22/10/2026). Pedido da dona: "uma forma de anexar arquivos no chat, para
// a IA analisar — ver imagem etc.". Telas na prancha "PetAgent — Conversas e Anexos".
//
// O CUSTO (explicado e aceito antes de implementar):
//   - imagem: o modelo VÊ a imagem, ~1.000 a 1.600 tokens cada. Ela fica no histórico da
//     conversa e volta em cada mensagem seguinte (o cache do provedor barateia, não zera);
//   - PDF: vai como TEXTO (escolha da dona — barato e sem instalar nada), ~500 a 800 tokens
//     por página. O app extrai o texto aqui; PDF escaneado (só foto) não tem texto e avisa;
//   - texto (txt, csv, md, json, código): o tamanho do texto.
//
// O caminho: o frontend lê o arquivo e chama AnexoPreparar (valida, extrai e estima os tokens
// na hora — o aviso aparece ao escolher, não ao enviar). O pronto fica guardado aqui pelo id;
// SendChatEm leva os ids e a ponte entrega ao Hermes (chat_bridge.py, _anexar).

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

const (
	maxAnexoBytes = 20 << 20 // por arquivo
	maxAnexosMsg  = 5        // por mensagem
	// acima disto o chat pergunta antes de mandar (o Hermes, por conta própria, não põe na
	// mensagem um arquivo maior que metade do contexto: o agente lê por partes)
	tokensAvisar = 50_000
)

var (
	extImagem = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true}
	extTexto  = map[string]bool{".txt": true, ".csv": true, ".tsv": true, ".md": true, ".json": true, ".xml": true,
		".html": true, ".htm": true, ".css": true, ".js": true, ".ts": true, ".go": true, ".py": true, ".java": true,
		".c": true, ".h": true, ".cpp": true, ".cs": true, ".sql": true, ".yaml": true, ".yml": true, ".toml": true,
		".ini": true, ".log": true, ".sh": true, ".ps1": true, ".bat": true, ".php": true, ".rb": true, ".rs": true,
		".kt": true, ".swift": true, ".vue": true, ".jsx": true, ".tsx": true, ".env.example": true, ".srt": true}
	// formatos que a pessoa vai tentar e que ainda não vão: o recado diz o que fazer
	extRecado = map[string]string{
		".doc":  "Arquivos do Word ainda não são aceitos. Salve como PDF e anexe novamente.",
		".docx": "Arquivos do Word ainda não são aceitos. Salve como PDF e anexe novamente.",
		".xls":  "Planilhas do Excel ainda não são aceitas. Salve como CSV e anexe novamente.",
		".xlsx": "Planilhas do Excel ainda não são aceitas. Salve como CSV e anexe novamente.",
		".ppt":  "Apresentações do PowerPoint ainda não são aceitas. Salve como PDF e anexe novamente.",
		".pptx": "Apresentações do PowerPoint ainda não são aceitas. Salve como PDF e anexe novamente.",
		".mp4":  "Vídeos ainda não são aceitos. Envie uma captura de tela ou grave um áudio pelo microfone do chat.",
		".mov":  "Vídeos ainda não são aceitos. Envie uma captura de tela ou grave um áudio pelo microfone do chat.",
		".mp3":  "Arquivos de áudio ainda não são aceitos. Use o microfone do chat.",
		".zip":  "Arquivos compactados não são aceitos. Descompacte e anexe somente o necessário.",
	}
)

// anexoPronto — um arquivo já validado, à espera do envio.
type anexoPronto struct {
	ID      string `json:"id"`
	Nome    string `json:"nome"`
	Tipo    string `json:"tipo"` // imagem | pdf | texto
	Tokens  int    `json:"tokens"`
	Paginas int    `json:"paginas,omitempty"`
	Bytes   int    `json:"bytes"`
	envio   string // nome com que vai ao Hermes (PDF vira .txt)
	b64     string // o conteúdo que vai (o PDF já como texto)
}

var (
	anexosMu      sync.Mutex
	anexosProntos = map[string]*anexoPronto{}
)

// AnexoPreparar — valida o arquivo escolhido (nome + conteúdo em base64) e devolve
// {ok, id, nome, tipo, tokens, paginas, grande, semVisao} ou {ok:false, erro}.
func (a *App) AnexoPreparar(nome, b64 string) string {
	dados, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return respostaJSON(map[string]any{"ok": false, "erro": "Não foi possível ler o arquivo " + nome + "."})
	}
	p, err := preparaAnexo(nome, dados)
	if err != nil {
		return respostaJSON(map[string]any{"ok": false, "nome": nome, "erro": err.Error()})
	}
	anexosMu.Lock()
	if len(anexosProntos) > 50 { // escolhidos e nunca enviados: não acumula para sempre
		anexosProntos = map[string]*anexoPronto{}
	}
	anexosProntos[p.ID] = p
	anexosMu.Unlock()
	ef := iaAgora()
	return respostaJSON(map[string]any{"ok": true, "id": p.ID, "nome": p.Nome, "tipo": p.Tipo,
		"tokens": p.Tokens, "paginas": p.Paginas, "bytes": p.Bytes, "grande": p.Tokens > tokensAvisar,
		"semVisao": p.Tipo == "imagem" && modeloSemVisao(ef.Provedor, modeloEmUso())})
}

// AnexoDescartar — tirou o arquivo antes de enviar.
func (a *App) AnexoDescartar(id string) {
	anexosMu.Lock()
	delete(anexosProntos, id)
	anexosMu.Unlock()
}

// pegaAnexos — os prontos destes ids, já saindo da espera (cada um vai uma vez só).
func pegaAnexos(ids []string) ([]map[string]any, error) {
	if len(ids) > maxAnexosMsg {
		return nil, fmt.Errorf("Envie no máximo %d arquivos por mensagem.", maxAnexosMsg)
	}
	anexosMu.Lock()
	defer anexosMu.Unlock()
	saida := []map[string]any{}
	for _, id := range ids {
		p, ok := anexosProntos[id]
		if !ok {
			return nil, fmt.Errorf("Um dos anexos expirou. Anexe o arquivo novamente.")
		}
		saida = append(saida, map[string]any{"nome": p.envio, "tipo": map[bool]string{true: "imagem", false: "texto"}[p.Tipo == "imagem"], "base64": p.b64})
	}
	for _, id := range ids {
		delete(anexosProntos, id)
	}
	return saida, nil
}

// preparaAnexo — o miolo de AnexoPreparar, sem App (testável).
func preparaAnexo(nome string, dados []byte) (*anexoPronto, error) {
	nome = filepath.Base(strings.TrimSpace(nome))
	if nome == "" || nome == "." {
		nome = "anexo"
	}
	if len(dados) == 0 {
		return nil, fmt.Errorf("O arquivo %s está vazio.", nome)
	}
	if len(dados) > maxAnexoBytes {
		return nil, fmt.Errorf("O arquivo %s tem %.0f MB, e o limite é de %d MB por arquivo.", nome,
			float64(len(dados))/(1<<20), maxAnexoBytes>>20)
	}
	ext := strings.ToLower(filepath.Ext(nome))
	p := &anexoPronto{ID: idAnexo(), Nome: nome, Bytes: len(dados), envio: nome}
	switch {
	case extImagem[ext]:
		p.Tipo = "imagem"
		p.Tokens = tokensImagem(dados)
		p.b64 = base64.StdEncoding.EncodeToString(dados)
	case ext == ".pdf":
		texto, paginas, err := textoDoPDF(dados)
		if err != nil {
			return nil, fmt.Errorf("Não foi possível ler o PDF %s (%v).", nome, err)
		}
		if len(strings.Join(strings.Fields(texto), "")) < 40 {
			return nil, fmt.Errorf("O PDF %s não tem texto: é um documento escaneado (imagem das páginas). Envie capturas de tela das páginas importantes, pois a IA consegue ler imagens.", nome)
		}
		p.Tipo, p.Paginas = "pdf", paginas
		corpo := fmt.Sprintf("Texto extraído do PDF %s (%d página(s)).\n\n%s", nome, paginas, texto)
		p.Tokens = tokensTexto(corpo)
		p.envio = nome + ".txt"
		p.b64 = base64.StdEncoding.EncodeToString([]byte(corpo))
	case extRecado[ext] != "":
		return nil, fmt.Errorf("Não foi possível anexar %s. %s", nome, extRecado[ext])
	case extTexto[ext] || pareceTexto(dados):
		texto := comoUTF8(dados)
		p.Tipo = "texto"
		p.Tokens = tokensTexto(texto)
		p.b64 = base64.StdEncoding.EncodeToString([]byte(texto))
	default:
		return nil, fmt.Errorf("Não foi possível anexar %s: este tipo de arquivo ainda não é aceito. Envie imagem, PDF ou texto.", nome)
	}
	return p, nil
}

func idAnexo() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// tokensImagem — a conta da Anthropic (largura × altura / 750, com a imagem reduzida para
// caber em ~1,15 megapixel); serve de ordem de grandeza para os outros provedores. Formato
// que o Go não mede (webp, bmp): o teto.
func tokensImagem(dados []byte) int {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(dados))
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		return 1600
	}
	px := float64(cfg.Width) * float64(cfg.Height)
	if px > 1_150_000 {
		px = 1_150_000
	}
	t := int(px / 750)
	if t < 50 {
		t = 50
	}
	if t > 1600 {
		t = 1600
	}
	return t
}

// tokensTexto — ~4 caracteres por token (português fica perto disso).
func tokensTexto(t string) int { return utf8.RuneCountInString(t)/4 + 1 }

// pareceTexto — sem extensão conhecida: é texto se for UTF-8 e não tiver byte nulo.
func pareceTexto(dados []byte) bool {
	amostra := dados
	if len(amostra) > 8192 {
		amostra = amostra[:8192]
	}
	return !bytes.Contains(amostra, []byte{0}) && utf8.Valid(dados)
}

// comoUTF8 — texto salvo no Windows costuma vir em ANSI (Windows-1252): acento quebrado
// vira "�" para a IA. Se não é UTF-8 válido, lê como Latin-1 (cobre os acentos do português).
func comoUTF8(dados []byte) string {
	dados = bytes.TrimPrefix(dados, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(dados) {
		return string(dados)
	}
	r := make([]rune, len(dados))
	for i, b := range dados {
		r[i] = rune(b)
	}
	return string(r)
}

// textoDoPDF — o texto de cada página. A biblioteca entra em pânico com PDF estranho
// (protegido, quebrado): vira erro.
func textoDoPDF(dados []byte) (texto string, paginas int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("arquivo protegido ou corrompido")
		}
	}()
	rd, err := pdf.NewReader(bytes.NewReader(dados), int64(len(dados)))
	if err != nil {
		return "", 0, err
	}
	paginas = rd.NumPage()
	var b strings.Builder
	for i := 1; i <= paginas; i++ {
		pg := rd.Page(i)
		if pg.V.IsNull() {
			continue
		}
		t, errPg := pg.GetPlainText(nil)
		if errPg != nil {
			continue
		}
		if t = strings.TrimSpace(t); t != "" {
			fmt.Fprintf(&b, "--- página %d ---\n%s\n\n", i, t)
		}
	}
	return strings.TrimSpace(b.String()), paginas, nil
}

// modeloSemVisao — modelos que não enxergam imagem (a imagem seria cobrada e ignorada, ou
// recusada). Lista curta e conhecida; o resto se presume com visão.
func modeloSemVisao(provedor, modelo string) bool {
	m := strings.ToLower(provedor + " " + modelo)
	if strings.Contains(m, "deepseek") && !strings.Contains(m, "vl") {
		return true
	}
	for _, s := range []string{"o1-mini", "o3-mini", "qwq", "gpt-3.5"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// modeloEmUso — o que a ponte diz (o mais fiel); antes de ela subir, o configurado.
func modeloEmUso() string {
	if m := modeloPonte(); m != "" {
		return m
	}
	return iaAgora().Modelo
}
