package main

// harness_documentos_test.go — testes da ingestão de arquivos (30/10/2026).
//
// Os fixtures de DOCX e XLSX são montados no próprio teste com archive/zip: é ZIP + XML,
// então dá para provar a extração sem depender de arquivo binário no repositório.
//
// O que não pode regredir:
//   - DOCX devolve os parágrafos; XLSX devolve as células;
//   - caminho sensível (~/.ssh, AppData, .git) é RECUSADO, mesmo absoluto;
//   - arquivo fora da pasta do usuário é recusado;
//   - formato desconhecido devolve erro claro (não lixo).

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zipComArquivos(t *testing.T, mapa map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for nome, conteudo := range mapa {
		w, err := zw.Create(nome)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write([]byte(conteudo)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestTextoDeDOCX(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>Objetivo do projeto</w:t></w:r></w:p>
<w:p><w:r><w:t>Cliente: empresa Y</w:t></w:r></w:p>
<w:p><w:r><w:t>Prazo: 30 dias</w:t></w:r></w:p>
</w:body></w:document>`
	dados := zipComArquivos(t, map[string]string{"word/document.xml": doc})
	p := filepath.Join(t.TempDir(), "prd.docx")
	if err := os.WriteFile(p, dados, 0o644); err != nil {
		t.Fatal(err)
	}
	texto, tipo, err := TextoDeDocumento(p)
	if err != nil {
		t.Fatalf("extração falhou: %v", err)
	}
	if tipo != "docx" {
		t.Fatalf("tipo errado: %s", tipo)
	}
	for _, esperado := range []string{"Objetivo do projeto", "Cliente: empresa Y", "Prazo: 30 dias"} {
		if !strings.Contains(texto, esperado) {
			t.Fatalf("faltou %q no texto: %q", esperado, texto)
		}
	}
	if !strings.Contains(texto, "\n") {
		t.Fatalf("parágrafos não viraram linhas: %q", texto)
	}
}

func TestTextoDeXLSX(t *testing.T) {
	shared := `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3" uniqueCount="3">
<si><t>Nome</t></si><si><t>Valor</t></si><si><t>Aluguel</t></si></sst>`
	sheet := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row>
<row><c t="s"><v>2</v></c><c><v>1500</v></c></row>
</sheetData></worksheet>`
	dados := zipComArquivos(t, map[string]string{
		"xl/sharedStrings.xml":     shared,
		"xl/worksheets/sheet1.xml": sheet,
	})
	p := filepath.Join(t.TempDir(), "custos.xlsx")
	if err := os.WriteFile(p, dados, 0o644); err != nil {
		t.Fatal(err)
	}
	texto, tipo, err := TextoDeDocumento(p)
	if err != nil {
		t.Fatalf("extração falhou: %v", err)
	}
	if tipo != "xlsx" {
		t.Fatalf("tipo errado: %s", tipo)
	}
	if !strings.Contains(texto, "Nome\tValor") {
		t.Fatalf("cabeçalho não veio: %q", texto)
	}
	if !strings.Contains(texto, "Aluguel\t1500") {
		t.Fatalf("linha de dados não veio: %q", texto)
	}
}

func TestTextoPuroECodigo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notas.md")
	if err := os.WriteFile(p, []byte("# Notas\n- item um\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	texto, tipo, err := TextoDeDocumento(p)
	if err != nil || tipo != "texto" || !strings.Contains(texto, "item um") {
		t.Fatalf("texto puro falhou: %v %s %q", err, tipo, texto)
	}
}

func TestFormatoDesconhecido(t *testing.T) {
	p := filepath.Join(t.TempDir(), "foto.exe")
	if err := os.WriteFile(p, []byte{0x4d, 0x5a, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := TextoDeDocumento(p); err == nil {
		t.Fatal("deveria recusar formato desconhecido")
	}
}

func TestFormatoAntigoAvisa(t *testing.T) {
	p := filepath.Join(t.TempDir(), "velho.doc")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := TextoDeDocumento(p)
	if err == nil || !strings.Contains(err.Error(), "docx") {
		t.Fatalf("deveria orientar a salvar como .docx: %v", err)
	}
}

func TestCaminhoSensivelERecusado(t *testing.T) {
	raiz := t.TempDir()
	casa, _ := os.UserHomeDir()
	sensiveis := []string{
		filepath.Join(casa, ".ssh", "id_rsa"),
		filepath.Join(casa, "AppData", "Local", "Google", "Chrome", "Login Data"),
		filepath.Join(casa, "projeto", ".git", "config"),
		"../../.ssh/id_rsa",
	}
	for _, c := range sensiveis {
		if _, err := resolverDocumento(raiz, c); err == nil {
			t.Fatalf("deveria recusar caminho sensível: %s", c)
		}
	}
}

func TestCaminhoForaDaCasaERecusado(t *testing.T) {
	raiz := t.TempDir()
	if _, err := resolverDocumento(raiz, `C:\Windows\System32\drivers\etc\hosts`); err == nil {
		t.Fatal("deveria recusar caminho fora da pasta do usuário")
	}
}

func TestCaminhoRelativoUsaSandbox(t *testing.T) {
	raiz := t.TempDir()
	if err := os.WriteFile(filepath.Join(raiz, "rel.txt"), []byte("dentro da area"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := resolverDocumento(raiz, "rel.txt")
	if err != nil {
		t.Fatalf("deveria aceitar relativo: %v", err)
	}
	texto, _, err := TextoDeDocumento(abs)
	if err != nil || !strings.Contains(texto, "dentro da area") {
		t.Fatalf("leitura falhou: %v %q", err, texto)
	}
	if _, err := resolverDocumento(raiz, "../fora.txt"); err == nil {
		t.Fatal("deveria recusar escape da área de trabalho")
	}
}

func TestFerramentaLerDocumento(t *testing.T) {
	raiz := t.TempDir()
	doc := `<?xml version="1.0"?><w:document xmlns:w="w"><w:body>
<w:p><w:r><w:t>Contrato do cliente Y</w:t></w:r></w:p></w:body></w:document>`
	dados := zipComArquivos(t, map[string]string{"word/document.xml": doc})
	if err := os.WriteFile(filepath.Join(raiz, "contrato.docx"), dados, 0o644); err != nil {
		t.Fatal(err)
	}
	r := NovoRegistro()
	RegistrarFerramentasDocumento(r, raiz)
	saida, err := r.Executar("ler_documento", map[string]any{"caminho": "contrato.docx"})
	if err != nil {
		t.Fatalf("ferramenta falhou: %v", err)
	}
	if !strings.Contains(saida, "Contrato do cliente Y") || !strings.Contains(saida, "docx") {
		t.Fatalf("saída inesperada: %q", saida)
	}
	if _, err := r.Executar("ler_documento", map[string]any{"caminho": "naoexiste.pdf"}); err == nil {
		t.Fatal("arquivo inexistente deveria falhar")
	}
}

func TestXLSXGrandeMostraTeto(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0"?><worksheet xmlns="s"><sheetData>`)
	for i := 0; i < 620; i++ {
		sb.WriteString(fmt.Sprintf(`<row><c><v>%d</v></c></row>`, i))
	}
	sb.WriteString(`</sheetData></worksheet>`)
	dados := zipComArquivos(t, map[string]string{"xl/worksheets/sheet1.xml": sb.String()})
	texto, _, err := textoDeXLSXDeBytes(dados)
	if err != nil {
		t.Fatalf("extração: %v", err)
	}
	if !strings.Contains(texto, "500") {
		t.Fatalf("deveria avisar o teto de linhas: %q", texto[:200])
	}
}

// textoDeXLSXDeBytes só existe para o teste do teto (a função pública recebe caminho).
func textoDeXLSXDeBytes(dados []byte) (string, string, error) {
	p := filepath.Join(os.TempDir(), "teto_agente_test.xlsx")
	if err := os.WriteFile(p, dados, 0o644); err != nil {
		return "", "", err
	}
	defer os.Remove(p)
	return TextoDeDocumento(p)
}
