package main

import (
	"os"
	"path/filepath"
	"testing"
)

// criaArquivo — arquivo vazio (com as pastas) para simular uma instalação do Hermes.
func criaArquivo(t *testing.T, partes ...string) string {
	t.Helper()
	p := filepath.Join(partes...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAchaHermesLayoutNovo — o Hermes 0.21+ no Windows não tem mais
// hermes-agent\venv: o comando é hermes\bin\hermes.exe e o venv (com websockets, que a
// ponte usa) mora em hermes\installs\<id>\environments\<hash>\venv. Sem achar os dois,
// o chat do boneco dizia "Não encontrei o Hermes" com o Hermes instalado.
func TestAchaHermesLayoutNovo(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("PATH", "") // nada de achar o python/hermes do sistema por acaso
	exe := criaArquivo(t, base, "hermes", "bin", "hermes.exe")
	py := criaArquivo(t, base, "hermes", "installs", "bb5e", "environments", "04b2", "venv", "Scripts", "python.exe")

	if got := caminhoHermes(); got != exe {
		t.Errorf("caminhoHermes() = %q, queria %q", got, exe)
	}
	if got := caminhoPythonHermes(); got != py {
		t.Errorf("caminhoPythonHermes() = %q, queria %q", got, py)
	}
}

// TestAchaHermesLayoutAntigo — quem ainda tem o hermes-agent\venv continua igual.
func TestAchaHermesLayoutAntigo(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("PATH", "")
	exe := criaArquivo(t, base, "hermes", "hermes-agent", "venv", "Scripts", "hermes.exe")
	py := criaArquivo(t, base, "hermes", "hermes-agent", "venv", "Scripts", "python.exe")
	criaArquivo(t, base, "hermes", "bin", "hermes.exe") // os dois layouts: o antigo manda

	if got := caminhoHermes(); got != exe {
		t.Errorf("caminhoHermes() = %q, queria %q", got, exe)
	}
	if got := caminhoPythonHermes(); got != py {
		t.Errorf("caminhoPythonHermes() = %q, queria %q", got, py)
	}
}
