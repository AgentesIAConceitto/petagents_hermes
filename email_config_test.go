package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// trocaPoll — aponta o lançador para `script` e troca o disparo por um registro.
// Devolve quantas vezes ele foi disparado (o explorer.exe nunca roda no teste).
func trocaPoll(t *testing.T, script string) *int {
	scriptAntes, disparaAntes := scriptPoll, disparaPoll
	t.Cleanup(func() { scriptPoll, disparaPoll = scriptAntes, disparaAntes })
	n := 0
	scriptPoll = script
	disparaPoll = func(string) error { n++; return nil }
	return &n
}

// TestEmailSemScriptNaoDispara — o e-mail é OPCIONAL. Sem o poll_silencioso.vbs
// instalado, o app não pode disparar o explorer.exe com um caminho que não existe (a
// cada ~10 min, para sempre) nem dizer "ok" ao botão "Verificar agora".
func TestEmailSemScriptNaoDispara(t *testing.T) {
	disparos := trocaPoll(t, filepath.Join(t.TempDir(), "poll_silencioso.vbs"))

	r := NewApp().EmailVerificarAgora()

	if *disparos != 0 {
		t.Errorf("disparou a leitura %d vez(es) sem o script instalado", *disparos)
	}
	if !strings.Contains(r, "não configurado") {
		t.Errorf("EmailVerificarAgora() = %q, queria aviso de e-mail não configurado", r)
	}
}

// TestEmailComScriptDispara — com o script no lugar, continua disparando como antes.
func TestEmailComScriptDispara(t *testing.T) {
	script := filepath.Join(t.TempDir(), "poll_silencioso.vbs")
	if err := os.WriteFile(script, []byte("' teste"), 0o644); err != nil {
		t.Fatal(err)
	}
	disparos := trocaPoll(t, script)

	if r := NewApp().EmailVerificarAgora(); r != "ok" || *disparos != 1 {
		t.Errorf("EmailVerificarAgora() = %q com %d disparo(s), queria \"ok\" com 1", r, *disparos)
	}
}
