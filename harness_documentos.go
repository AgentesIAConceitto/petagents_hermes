package main

// harness_documentos.go — ingestão de arquivos no agente nativo (30/10/2026).
//
// REQUISITO: "receber arquivos e ler" é básico para tarefa real — contrato, planilha, PRD em
// Word. O PDF o app já sabia extrair (anexos.go, textoDoPDF); aqui entra o resto.
//
// SEM DEPENDÊNCIA NOVA: DOCX e XLSX são ZIP + XML. Dá para ler com archive/zip e
// encoding/xml da biblioteca padrão — menos peso e menos risco de supply chain do que
// trazer uma lib de Office.
//
// SEGURANÇA (o modelo escolhe o caminho):
//   - caminho relativo → dentro da área de trabalho do agente (sandbox);
//   - caminho absoluto → só dentro da pasta do usuário, e nunca em pasta sensível
//     (.ssh, .aws, .gnupg, AppData, .git, credenciais). Sem isso, "leia meu arquivo"
//     viraria "leia minhas chaves".

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	docMaxBytes = 25 << 20 // 25 MB: planilha/PDF grande ainda cabe
	docMaxChars = 60000    // texto devolvido ao modelo
)

// extensoesDeTexto são lidas como texto puro.
var extensoesDeTexto = map[string]bool{
	".txt": true, ".md": true, ".csv": true, ".tsv": true, ".json": true, ".yaml": true,
	".yml": true, ".xml": true, ".html": true, ".htm": true, ".log": true, ".ini": true,
	".toml": true, ".go": true, ".py": true, ".js": true, ".ts": true, ".css": true,
	".sql": true, ".sh": true, ".ps1": true, ".bat": true, ".c": true, ".h": true,
	".java": true, ".php": true, ".rb": true, ".rs": true,
}

// pastasSensiveis nunca são lidas pelo agente, nem com caminho absoluto.
var pastasSensiveis = []string{
	".ssh", ".aws", ".gnupg", ".kube", ".docker", ".git", "appdata",
	"cookies", "credentials", "credentials.json", "id_rsa", "id_ed25519",
	"login data", "web data", "local state",
}

// resolverDocumento valida o caminho pedido pelo modelo.
func resolverDocumento(raiz, pedido string) (string, error) {
	pedido = strings.TrimSpace(pedido)
	if pedido == "" {
		return "", errors.New("caminho vazio")
	}
	baixo := strings.ToLower(filepath.ToSlash(pedido))
	for _, s := range pastasSensiveis {
		if strings.Contains(baixo, "/"+s) || strings.Contains(baixo, "\\"+s) || strings.HasPrefix(baixo, s) {
			return "", fmt.Errorf("arquivo em local sensível não é lido pelo agente (%s)", s)
		}
	}
	if !filepath.IsAbs(pedido) {
		return resolverCaminho(raiz, pedido)
	}
	// absoluto: precisa estar dentro da pasta do usuário
	casa, err := os.UserHomeDir()
	if err != nil || casa == "" {
		return "", errors.New("não consegui descobrir a pasta do usuário")
	}
	abs, err := filepath.Abs(pedido)
	if err != nil {
		return "", err
	}
	casaAbs, _ := filepath.Abs(casa)
	if !strings.HasPrefix(strings.ToLower(abs), strings.ToLower(casaAbs)+string(os.PathSeparator)) {
		return "", errors.New("só leio arquivos dentro da sua pasta de usuário")
	}
	return abs, nil
}

// TextoDeDocumento devolve o texto de PDF, DOCX, XLSX ou arquivo de texto.
// tipo é "pdf", "docx", "xlsx", "texto" (para o chamador saber o que veio).
func TextoDeDocumento(caminho string) (texto string, tipo string, err error) {
	info, err := os.Stat(caminho)
	if err != nil {
		return "", "", fmt.Errorf("não achei o arquivo: %w", err)
	}
	if info.IsDir() {
		return "", "", errors.New("isso é uma pasta, não um arquivo")
	}
	if info.Size() > docMaxBytes {
		return "", "", fmt.Errorf("arquivo grande demais (%d MB; limite %d MB)", info.Size()>>20, docMaxBytes>>20)
	}
	dados, err := os.ReadFile(caminho)
	if err != nil {
		return "", "", fmt.Errorf("não consegui ler: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(caminho))

	switch ext {
	case ".pdf":
		t, paginas, err := textoDoPDF(dados)
		if err != nil {
			return "", "pdf", fmt.Errorf("PDF sem texto extraível (pode ser digitalizado; precisaria de OCR): %w", err)
		}
		cab := fmt.Sprintf("[PDF, %d página(s)]\n", paginas)
		return cortarTexto(cab+t, docMaxChars), "pdf", nil
	case ".docx":
		t, err := textoDeDOCX(dados)
		if err != nil {
			return "", "docx", err
		}
		return cortarTexto("[DOCX]\n"+t, docMaxChars), "docx", nil
	case ".xlsx":
		t, err := textoDeXLSX(dados)
		if err != nil {
			return "", "xlsx", err
		}
		return cortarTexto("[XLSX]\n"+t, docMaxChars), "xlsx", nil
	case ".doc", ".xls", ".ppt", ".pptx":
		return "", strings.TrimPrefix(ext, "."), fmt.Errorf("formato antigo/outro (%s): peça para salvar como .docx/.xlsx ou PDF", ext)
	}

	if extensoesDeTexto[ext] || pareceTexto(dados) {
		return cortarTexto(comoUTF8(dados), docMaxChars), "texto", nil
	}
	return "", "desconhecido", fmt.Errorf("não sei ler %s (aceito PDF, DOCX, XLSX, texto e código)", ext)
}

// textoDeDOCX lê word/document.xml e devolve os parágrafos.
func textoDeDOCX(dados []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(dados), int64(len(dados)))
	if err != nil {
		return "", fmt.Errorf("DOCX inválido: %w", err)
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", errors.New("DOCX sem word/document.xml")
	}
	rc, err := doc.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	corpo, err := io.ReadAll(io.LimitReader(rc, docMaxBytes))
	if err != nil {
		return "", err
	}

	// percorre o XML: texto em <w:t>, quebra de parágrafo em <w:p>, célula em <w:tc>
	var b strings.Builder
	dec := xml.NewDecoder(bytes.NewReader(corpo))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "tab" {
				b.WriteString("\t")
			} else if t.Name.Local == "br" {
				b.WriteString("\n")
			}
		case xml.CharData:
			// só dentro de <w:t> — para simplificar, o texto de outros nós é ignorado pelo
			// filtro de parágrafo abaixo; CharData fora de <w:t> costuma ser formatação.
			s := strings.TrimSpace(string(t))
			if s != "" {
				b.WriteString(string(t))
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				b.WriteString("\n")
			case "tc":
				b.WriteString("\t")
			}
		}
	}
	txt := strings.ReplaceAll(b.String(), "\r", "")
	txt = strings.Join(strings.Fields(strings.ReplaceAll(txt, "\n", " \n ")), " ")
	txt = strings.ReplaceAll(txt, " \n ", "\n")
	txt = strings.ReplaceAll(txt, "\t ", "\t")
	return strings.TrimSpace(txt), nil
}

// textoDeXLSX lê sharedStrings + a primeira planilha e devolve linhas com TAB.
func textoDeXLSX(dados []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(dados), int64(len(dados)))
	if err != nil {
		return "", fmt.Errorf("XLSX inválido: %w", err)
	}
	abrir := func(nome string) ([]byte, error) {
		for _, f := range zr.File {
			if f.Name == nome {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, docMaxBytes))
			}
		}
		return nil, fmt.Errorf("XLSX sem %s", nome)
	}

	// textos compartilhados: <si><t>texto</t></si>
	var compartilhados []string
	if b, err := abrir("xl/sharedStrings.xml"); err == nil {
		var ss struct {
			SI []struct {
				T string `xml:"t"`
				R []struct {
					T string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		if xml.Unmarshal(b, &ss) == nil {
			for _, si := range ss.SI {
				if si.T != "" {
					compartilhados = append(compartilhados, si.T)
				} else {
					var juntos strings.Builder
					for _, r := range si.R {
						juntos.WriteString(r.T)
					}
					compartilhados = append(compartilhados, juntos.String())
				}
			}
		}
	}

	// planilha: procura a primeira sheet
	nome := ""
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			nome = f.Name
			break
		}
	}
	if nome == "" {
		return "", errors.New("XLSX sem planilha")
	}
	b, err := abrir(nome)
	if err != nil {
		return "", err
	}
	var sheet struct {
		Rows []struct {
			C []struct {
				T  string `xml:"t,attr"`
				V  string `xml:"v"`
				IS struct {
					T string `xml:"t"`
				} `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(b, &sheet); err != nil {
		return "", fmt.Errorf("planilha ilegível: %w", err)
	}
	var out strings.Builder
	out.WriteString("(linhas da planilha, valores separados por TAB)\n")
	for i, linha := range sheet.Rows {
		if i >= 500 { // teto: planilha de 50 mil linhas não cabe no contexto
			out.WriteString(fmt.Sprintf("... (%d linhas no total; mostrando 500)\n", len(sheet.Rows)))
			break
		}
		var celulas []string
		for _, c := range linha.C {
			v := c.V
			if c.T == "s" { // índice na tabela de compartilhados
				var idx int
				if _, err := fmt.Sscanf(v, "%d", &idx); err == nil && idx >= 0 && idx < len(compartilhados) {
					v = compartilhados[idx]
				}
			} else if v == "" && c.IS.T != "" {
				v = c.IS.T
			}
			celulas = append(celulas, v)
		}
		if strings.TrimSpace(strings.Join(celulas, "")) == "" {
			continue
		}
		out.WriteString(strings.Join(celulas, "\t"))
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String()), nil
}

// RegistrarFerramentasDocumento dá ao agente a capacidade de ler arquivos enviados.
func RegistrarFerramentasDocumento(r *Registro, raiz string) {
	r.Registrar(&Ferramenta{
		Nome: "ler_documento",
		Descricao: "Lê um arquivo e devolve o texto: PDF, DOCX (Word), XLSX (Excel), CSV, TXT e " +
			"código. Use quando a pessoa enviar ou apontar um arquivo. Caminho relativo usa a " +
			"área de trabalho do agente; caminho completo só dentro da pasta do usuário.",
		Parametros: schemaObjeto(map[string]any{
			"caminho": schemaString("Arquivo (relativo à área de trabalho ou caminho completo)"),
		}, "caminho"),
		Executar: func(args map[string]any) (string, error) {
			abs, err := resolverDocumento(raiz, argTexto(args, "caminho"))
			if err != nil {
				return "", err
			}
			texto, tipo, err := TextoDeDocumento(abs)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("arquivo: %s (%s)\n---\n%s", filepath.Base(abs), tipo, texto), nil
		},
	})
}
