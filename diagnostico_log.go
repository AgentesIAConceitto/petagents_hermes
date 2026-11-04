package main

// diagnostico_log.go — linha de log para investigar comportamento em uso (04/11/2026).
// O gravaDiagnostico() tira uma foto completa e sobrescreve; este aqui ACUMULA, com hora,
// para provar coisas do tipo "a conversa nova está levando contexto da anterior?".

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func registraDiag(msg string) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return
	}
	dir := filepath.Join(base, "petagent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	arq := filepath.Join(dir, "diag.txt")
	f, err := os.OpenFile(arq, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("02/01 15:04:05"), msg)

	// não deixa o arquivo crescer sem limite
	if st, err := f.Stat(); err == nil && st.Size() > 512*1024 {
		_ = os.Remove(arq)
	}
}
