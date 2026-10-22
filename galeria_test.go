package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// erroDaResposta — a resposta tem de ser JSON válido com ok=false; devolve o "erro".
func erroDaResposta(t *testing.T, nome, resp string) string {
	t.Helper()
	var d struct {
		OK   bool   `json:"ok"`
		Erro string `json:"erro"`
	}
	if err := json.Unmarshal([]byte(resp), &d); err != nil {
		t.Fatalf("%s devolveu JSON inválido (%v):\n%s", nome, err, resp)
	}
	if d.OK {
		t.Fatalf("%s devolveu ok=true, queria erro: %s", nome, resp)
	}
	return d.Erro
}

// TestGaleriaErroComCaminhoEJSONValido — erro do sistema traz caminho do Windows
// (C:\Users\...). Colado na mão dentro do JSON, "\U" é escape inválido: o painel não
// conseguia ler a resposta e mostrava "sem rede?" no lugar do motivo de verdade.
func TestGaleriaErroComCaminhoEJSONValido(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	a := NewApp()

	t.Run("PetDexBaixar", func(t *testing.T) {
		// catálogo em cache com o boneco "abc", e um ARQUIVO onde a pasta dele iria
		cat := filepath.Join(pastaPetAgente(), "galeria.json")
		if err := os.WriteFile(cat, []byte(`{"pets":[{"slug":"abc"}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(pastaPetsHermes(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pastaPetsHermes(), "abc"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		erro := erroDaResposta(t, "PetDexBaixar", a.PetDexBaixar("abc"))
		if !strings.Contains(erro, base) {
			t.Errorf("o erro perdeu o caminho: %q", erro)
		}
	})

	t.Run("PetDexEscolher", func(t *testing.T) {
		// uma PASTA onde o petdex.txt iria: gravar a escolha falha
		if err := os.MkdirAll(filepath.Join(pastaPetAgente(), "petdex.txt"), 0o755); err != nil {
			t.Fatal(err)
		}
		erro := erroDaResposta(t, "PetDexEscolher", a.PetDexEscolher("abc"))
		if !strings.Contains(erro, base) {
			t.Errorf("o erro perdeu o caminho: %q", erro)
		}
	})
}
