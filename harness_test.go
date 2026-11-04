package main

// harness_test.go — testes dos blocos 1 e 2 do harness nativo (30/10/2026).
//
// Tudo roda em pasta temporária: nenhum teste toca %LOCALAPPDATA%\petagent do usuário.
// O que está sendo protegido aqui, e que não pode regredir:
//   - o modelo não escapa da raiz de trabalho (.. / absoluto / symlink);
//   - arquivo binário não entra no contexto como texto;
//   - escrita é atômica e o conteúdo antigo não fica pela metade;
//   - memória não duplica fato e a busca acha por termo sem acento.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func registroTemp(t *testing.T) (*Registro, string) {
	t.Helper()
	raiz := t.TempDir()
	r := NovoRegistro()
	RegistrarFerramentasArquivo(r, raiz)
	return r, raiz
}

func TestSandboxRecusaEscapar(t *testing.T) {
	r, raiz := registroTemp(t)
	// sobe um nível acima da raiz
	if _, err := r.Executar("escrever_arquivo", map[string]any{
		"caminho": "../fora.txt", "conteudo": "x"}); err == nil {
		t.Fatal("deveria recusar caminho com ..")
	}
	// caminho absoluto de outro lugar
	alvo := filepath.Join(t.TempDir(), "absoluto.txt")
	if _, err := r.Executar("escrever_arquivo", map[string]any{
		"caminho": alvo, "conteudo": "x"}); err == nil {
		t.Fatal("deveria recusar caminho absoluto fora da raiz")
	}
	// nada foi criado fora
	if _, err := os.Stat(alvo); err == nil {
		t.Fatal("arquivo foi criado fora da raiz")
	}
	// dentro da raiz funciona
	if _, err := r.Executar("escrever_arquivo", map[string]any{
		"caminho": "ok/arquivo.txt", "conteudo": "conteudo"}); err != nil {
		t.Fatalf("deveria aceitar caminho dentro da raiz: %v", err)
	}
	if _, err := os.Stat(filepath.Join(raiz, "ok", "arquivo.txt")); err != nil {
		t.Fatalf("arquivo não foi criado: %v", err)
	}
}

func TestSymlinkParaForaERecusado(t *testing.T) {
	r, raiz := registroTemp(t)
	fora := t.TempDir()
	alvoExterno := filepath.Join(fora, "segredo.txt")
	if err := os.WriteFile(alvoExterno, []byte("segredo"), 0o644); err != nil {
		t.Skip("sem permissão para criar arquivo temporário")
	}
	link := filepath.Join(raiz, "atalho")
	if err := os.Symlink(alvoExterno, link); err != nil {
		t.Skip("Windows sem permissão para symlink (precisa de modo desenvolvedor): " + err.Error())
	}
	if _, err := r.Executar("ler_arquivo", map[string]any{"caminho": "atalho"}); err == nil {
		t.Fatal("deveria recusar symlink que aponta para fora da raiz")
	}
}

func TestEscreverLerListar(t *testing.T) {
	r, raiz := registroTemp(t)
	if _, err := r.Executar("criar_pasta", map[string]any{"caminho": "projeto-x"}); err != nil {
		t.Fatalf("criar_pasta: %v", err)
	}
	if _, err := r.Executar("escrever_arquivo", map[string]any{
		"caminho": "projeto-x/prd.md", "conteudo": "linha 1\nlinha 2\nlinha 3\n"}); err != nil {
		t.Fatalf("escrever: %v", err)
	}
	saida, err := r.Executar("ler_arquivo", map[string]any{"caminho": "projeto-x/prd.md"})
	if err != nil {
		t.Fatalf("ler: %v", err)
	}
	if !strings.Contains(saida, "linha 2") {
		t.Fatalf("conteúdo não veio na leitura: %q", saida)
	}
	lista, err := r.Executar("listar_pasta", map[string]any{"caminho": "projeto-x"})
	if err != nil {
		t.Fatalf("listar: %v", err)
	}
	if !strings.Contains(lista, "prd.md") {
		t.Fatalf("listagem não trouxe o arquivo: %q", lista)
	}
	// escrita atômica não deixa .tmp para trás
	if _, err := os.Stat(filepath.Join(raiz, "projeto-x", "prd.md.tmp")); err == nil {
		t.Fatal("sobrou arquivo .tmp da escrita atômica")
	}
}

func TestLerPorPartesEAnexar(t *testing.T) {
	r, _ := registroTemp(t)
	var b strings.Builder
	for i := 1; i <= 50; i++ {
		b.WriteString("linha ")
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString("\n")
	}
	if _, err := r.Executar("escrever_arquivo", map[string]any{"caminho": "grande.txt", "conteudo": b.String()}); err != nil {
		t.Fatalf("escrever: %v", err)
	}
	parte, err := r.Executar("ler_arquivo", map[string]any{"caminho": "grande.txt", "inicio": 10, "limite": 5})
	if err != nil {
		t.Fatalf("ler parte: %v", err)
	}
	if !strings.Contains(parte, "linhas 10-14") {
		t.Fatalf("cabeçalho de faixa inesperado: %q", parte)
	}
	if _, err := r.Executar("escrever_arquivo", map[string]any{
		"caminho": "grande.txt", "conteudo": "final\n", "anexar": true}); err != nil {
		t.Fatalf("anexar: %v", err)
	}
	tudo, _ := r.Executar("ler_arquivo", map[string]any{"caminho": "grande.txt", "limite": 1000})
	if !strings.Contains(tudo, "final") {
		t.Fatal("anexar não funcionou")
	}
}

func TestSubstituirTrecho(t *testing.T) {
	r, _ := registroTemp(t)
	if _, err := r.Executar("escrever_arquivo", map[string]any{
		"caminho": "codigo.go", "conteudo": "func a() { return 1 }\n"}); err != nil {
		t.Fatalf("escrever: %v", err)
	}
	if _, err := r.Executar("substituir_trecho", map[string]any{
		"caminho": "codigo.go", "antigo": "return 1", "novo": "return 2"}); err != nil {
		t.Fatalf("substituir: %v", err)
	}
	saida, _ := r.Executar("ler_arquivo", map[string]any{"caminho": "codigo.go"})
	if !strings.Contains(saida, "return 2") {
		t.Fatalf("substituição não aplicada: %q", saida)
	}
	// trecho inexistente devolve erro (o modelo precisa saber)
	if _, err := r.Executar("substituir_trecho", map[string]any{
		"caminho": "codigo.go", "antigo": "nao existe", "novo": "x"}); err == nil {
		t.Fatal("deveria falhar quando o trecho não existe")
	}
}

func TestRecusaBinario(t *testing.T) {
	r, raiz := registroTemp(t)
	bin := filepath.Join(raiz, "imagem.bin")
	if err := os.WriteFile(bin, []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Executar("ler_arquivo", map[string]any{"caminho": "imagem.bin"}); err == nil {
		t.Fatal("deveria recusar leitura de binário")
	}
}

func TestMemoriaMemorizarEBuscar(t *testing.T) {
	arq := filepath.Join(t.TempDir(), "memoria.json")
	m := AbrirMemoria(arq)

	id1, err := m.Memorizar("O cliente do projeto X é a empresa Y", []string{"cliente", "projeto-x"})
	if err != nil {
		t.Fatalf("memorizar: %v", err)
	}
	if id1 == "" {
		t.Fatal("id vazio")
	}
	// mesmo fato com acento/espaço diferente não duplica
	id2, _ := m.Memorizar("o cliente do projeto x e a empresa y", nil)
	if id2 != id1 {
		t.Fatalf("duplicou fato equivalente: %s != %s", id1, id2)
	}
	if n, _ := m.Quantos(); n != 1 {
		t.Fatalf("esperava 1 fato, tenho %d", n)
	}
	// busca sem acento acha
	res := m.Buscar("empresa cliente", 5)
	if len(res) == 0 {
		t.Fatal("busca não achou o fato")
	}
	if !strings.Contains(res[0].Fato.Texto, "empresa Y") {
		t.Fatalf("fato errado no topo: %q", res[0].Fato.Texto)
	}
	// persiste em disco e reabre
	m2 := AbrirMemoria(arq)
	if n, _ := m2.Quantos(); n != 1 {
		t.Fatalf("memória não persistiu: %d fatos", n)
	}
}

func TestMemoriaRankingPorTermo(t *testing.T) {
	m := AbrirMemoria(filepath.Join(t.TempDir(), "m.json"))
	_, _ = m.Memorizar("O PRD fica em projeto-x/prd.md", []string{"arquivo"})
	_, _ = m.Memorizar("O cliente prefere reuniões pela manhã", []string{"cliente"})
	_, _ = m.Memorizar("O servidor de produção é a VPS conceitto_nova", []string{"infra"})

	res := m.Buscar("onde fica o prd do projeto", 3)
	if len(res) == 0 || !strings.Contains(res[0].Fato.Texto, "prd.md") {
		t.Fatalf("ranking errado: %+v", res)
	}
	res2 := m.Buscar("servidor producao vps", 3)
	if len(res2) == 0 || !strings.Contains(res2[0].Fato.Texto, "conceitto_nova") {
		t.Fatalf("ranking errado para infra: %+v", res2)
	}
}

func TestMemoriaEsquecerEEventos(t *testing.T) {
	m := AbrirMemoria(filepath.Join(t.TempDir(), "m.json"))
	id, _ := m.Memorizar("Fato temporário", nil)
	if !m.Esquecer(id) {
		t.Fatal("deveria esquecer")
	}
	if n, _ := m.Quantos(); n != 0 {
		t.Fatalf("fato não foi removido: %d", n)
	}
	m.Evento("ferramenta", "escreveu projeto-x/prd.md")
	_, ev := m.Quantos()
	if ev != 1 {
		t.Fatalf("evento não registrado: %d", ev)
	}
	ctx := m.ContextoParaPrompt("prd", 3, 3)
	if !strings.Contains(ctx, "aconteceu recentemente") {
		t.Fatalf("contexto sem eventos: %q", ctx)
	}
}

func TestFerramentasMemoriaNoRegistro(t *testing.T) {
	m := AbrirMemoria(filepath.Join(t.TempDir(), "m.json"))
	r := NovoRegistro()
	RegistrarFerramentasMemoria(r, m)

	saida, err := r.Executar("memorizar", map[string]any{
		"texto": "O cliente é a empresa Y", "tags": []any{"cliente"}})
	if err != nil || !strings.Contains(saida, "ok") {
		t.Fatalf("memorizar via ferramenta: %v %q", err, saida)
	}
	busca, err := r.Executar("buscar_memoria", map[string]any{"consulta": "empresa"})
	if err != nil || !strings.Contains(busca, "empresa Y") {
		t.Fatalf("buscar via ferramenta: %v %q", err, busca)
	}
	esq := strings.TrimSpace(strings.TrimPrefix(busca, "- ["))
	id := esq[:strings.Index(esq, "]")]
	if _, err := r.Executar("esquecer", map[string]any{"id": id}); err != nil {
		t.Fatalf("esquecer via ferramenta: %v", err)
	}
}

func TestEsquemaOpenAIDasFerramentas(t *testing.T) {
	r, _ := registroTemp(t)
	esq := r.EsquemaOpenAI()
	if len(esq) < 5 {
		t.Fatalf("esperava as 5 ferramentas de arquivo, tenho %d", len(esq))
	}
	for _, f := range esq {
		fn, ok := f["function"].(map[string]any)
		if !ok {
			t.Fatal("formato fora do padrão OpenAI")
		}
		if fn["name"] == "" || fn["parameters"] == nil {
			t.Fatalf("ferramenta incompleta: %+v", fn)
		}
	}
	nomes := strings.Join(r.Nomes(), ",")
	for _, esperado := range []string{"listar_pasta", "ler_arquivo", "escrever_arquivo", "criar_pasta", "substituir_trecho"} {
		if !strings.Contains(nomes, esperado) {
			t.Fatalf("faltou a ferramenta %s (tenho %s)", esperado, nomes)
		}
	}
}
