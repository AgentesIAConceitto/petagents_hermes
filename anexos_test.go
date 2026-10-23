package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
)

// pdfComTexto — um PDF mínimo de verdade (uma página por texto; "" = página sem texto,
// como a de um PDF escaneado), com a tabela xref nos deslocamentos certos.
func pdfComTexto(paginas ...string) []byte {
	var b bytes.Buffer
	offs := []int{}
	obj := func(corpo string) {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offs), corpo)
	}
	b.WriteString("%PDF-1.4\n")
	n := len(paginas)
	kids := []string{}
	for i := 0; i < n; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 4+2*i))
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i, t := range paginas {
		conteudo := ""
		if t != "" {
			conteudo = fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", t)
		}
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(conteudo), conteudo))
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, xref)
	return b.Bytes()
}

func TestAnexoPDFViraTexto(t *testing.T) {
	p, err := preparaAnexo("proposta.pdf", pdfComTexto("Preco unitario 21,10 no lote de 500", "Frete incluso no valor final"))
	if err != nil {
		t.Fatalf("PDF com texto deveria passar: %v", err)
	}
	if p.Tipo != "pdf" || p.Paginas != 2 || p.envio != "proposta.pdf.txt" {
		t.Fatalf("tipo/páginas/nome errados: %+v", p)
	}
	corpo, _ := base64.StdEncoding.DecodeString(p.b64)
	for _, quer := range []string{"Preco unitario 21,10", "Frete incluso", "página 2"} {
		if !strings.Contains(string(corpo), quer) {
			t.Errorf("o texto extraído deveria ter %q:\n%s", quer, corpo)
		}
	}
	if p.Tokens < 10 {
		t.Errorf("tokens estimados baixos demais: %d", p.Tokens)
	}
}

func TestAnexoPDFEscaneadoAvisa(t *testing.T) {
	_, err := preparaAnexo("contrato.pdf", pdfComTexto("", ""))
	if err == nil || !strings.Contains(err.Error(), "escaneado") {
		t.Fatalf("PDF sem texto deveria avisar que é escaneado, veio: %v", err)
	}
}

func TestAnexoPDFQuebradoNaoDerruba(t *testing.T) {
	if _, err := preparaAnexo("x.pdf", []byte("%PDF-1.4 lixo sem xref")); err == nil {
		t.Fatal("PDF quebrado deveria dar erro (e não pânico)")
	}
}

func TestAnexoImagemEstimaTokens(t *testing.T) {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 300, 250)))
	p, err := preparaAnexo("print.png", b.Bytes())
	if err != nil || p.Tipo != "imagem" {
		t.Fatalf("PNG deveria virar imagem: %+v %v", p, err)
	}
	if p.Tokens != 300*250/750 {
		t.Errorf("tokens = %d, esperava %d", p.Tokens, 300*250/750)
	}
	b.Reset()
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4000, 3000)))
	if p, _ = preparaAnexo("grande.png", b.Bytes()); p.Tokens != 1533 {
		t.Errorf("imagem grande é reduzida (~1,15 MP): tokens = %d, esperava 1533", p.Tokens)
	}
}

func TestAnexoTextoETamanho(t *testing.T) {
	p, err := preparaAnexo("vendas.csv", []byte("mes;total\nset;1200\n"))
	if err != nil || p.Tipo != "texto" {
		t.Fatalf("CSV deveria virar texto: %+v %v", p, err)
	}
	// ANSI (Windows-1252): "ção" não pode chegar quebrado
	p, _ = preparaAnexo("nota.txt", []byte{'a', 0xE7, 0xE3, 'o'})
	corpo, _ := base64.StdEncoding.DecodeString(p.b64)
	if string(corpo) != "ação" {
		t.Errorf("texto ANSI deveria virar UTF-8: %q", corpo)
	}
	if _, err = preparaAnexo("enorme.txt", make([]byte, maxAnexoBytes+1)); err == nil || !strings.Contains(err.Error(), "limite") {
		t.Errorf("acima de 20 MB deveria recusar: %v", err)
	}
	if _, err = preparaAnexo("relatorio.docx", []byte("PK\x03\x04")); err == nil || !strings.Contains(err.Error(), "PDF") {
		t.Errorf("Word deveria dizer para salvar como PDF: %v", err)
	}
	if _, err = preparaAnexo("prog.exe", []byte("MZ\x00\x00")); err == nil {
		t.Error("binário desconhecido não deveria passar")
	}
}

func TestAnexoSaiDaEsperaUmaVez(t *testing.T) {
	p, _ := preparaAnexo("a.txt", []byte("oi"))
	anexosMu.Lock()
	anexosProntos[p.ID] = p
	anexosMu.Unlock()
	lista, err := pegaAnexos([]string{p.ID})
	if err != nil || len(lista) != 1 || lista[0]["tipo"] != "texto" {
		t.Fatalf("pegaAnexos: %v %v", lista, err)
	}
	if _, err = pegaAnexos([]string{p.ID}); err == nil {
		t.Error("o mesmo anexo não pode ir duas vezes")
	}
	if _, err = pegaAnexos([]string{"1", "2", "3", "4", "5", "6"}); err == nil {
		t.Error("mais de 5 por mensagem deveria recusar")
	}
}

func TestModeloSemVisao(t *testing.T) {
	casos := map[[2]string]bool{
		{"deepseek", "deepseek-chat"}:              true,
		{"openrouter", "deepseek/deepseek-r1"}:     true,
		{"openrouter", "anthropic/claude-sonnet"}: false,
		{"openai-api", "gpt-5"}:                    false,
		{"openai-api", "o3-mini"}:                  true,
	}
	for c, quer := range casos {
		if got := modeloSemVisao(c[0], c[1]); got != quer {
			t.Errorf("modeloSemVisao(%q, %q) = %v, esperava %v", c[0], c[1], got, quer)
		}
	}
}
