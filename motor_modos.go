package main

// motor_modos.go — os dois modos do agente (30/10/2026).
//
// DECISÃO DO DONO: o PetAgent tem que funcionar de duas formas, e a pessoa escolhe:
//
//	modo HERMES  — usa o motor do Hermes já instalado (melhor qualidade, ferramentas
//	               completas). Exige Hermes na máquina.
//	modo NATIVO  — o motor deste app (motor.go) com chave grátis. Não exige nada instalado.
//	modo AUTO    — Hermes se existir; senão nativo. É o padrão.
//
// A detecção NÃO pode ser otimista: rodar `hermes --version` nesta máquina TRAVOU por mais
// de 3 minutos (30/10/2026) porque o CLI abre o app quando não entende o argumento. Então a
// checagem roda python com `-c` curto e timeout, lendo a versão do pacote — não o CLI.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ModoAgente é o motor escolhido.
type ModoAgente string

const (
	ModoAuto   ModoAgente = "auto"
	ModoHermes ModoAgente = "hermes"
	ModoNativo ModoAgente = "nativo"
)

// DiagnosticoHermes é o resultado da detecção.
type DiagnosticoHermes struct {
	Instalado bool   `json:"instalado"`
	Caminho   string `json:"caminho,omitempty"` // python.exe do venv
	Versao    string `json:"versao,omitempty"`
	Motivo    string `json:"motivo,omitempty"` // por que não achou / por que falhou
}

// comoInstalarHermes é o texto mostrado quando o modo Hermes é escolhido sem Hermes.
const comoInstalarHermes = "Para usar o modo Hermes, instale em um terminal:\n" +
	"  1) pipx install hermes-agent   (ou: pip install hermes-agent)\n" +
	"  2) hermes setup\n" +
	"Depois feche e abra o PetAgent. Enquanto isso, o modo nativo funciona com a sua chave grátis."

// candidatosHermes são os lugares onde o venv pode estar.
func candidatosHermes() []string {
	var out []string
	if v := strings.TrimSpace(os.Getenv("PETAGENT_HERMES_VENV")); v != "" {
		out = append(out, filepath.Join(v, "Scripts", "python.exe"))
	}
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		out = append(out, filepath.Join(base, "hermes", "hermes-agent", "venv", "Scripts", "python.exe"))
	}
	if casa, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(casa, ".local", "share", "hermes", "hermes-agent", "venv", "Scripts", "python.exe"))
	}
	return out
}

// DetectarHermes procura a instalação e devolve a versão do pacote.
//
// Usa `python -c` com timeout de 25s: `hermes_cli.main --version` abre o app e trava
// (aconteceu nesta máquina, 30/10/2026). Aqui só importamos o metadado.
func DetectarHermes() DiagnosticoHermes {
	var d DiagnosticoHermes
	var pythons []string
	for _, c := range candidatosHermes() {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			pythons = append(pythons, c)
		}
	}
	if len(pythons) == 0 {
		d.Motivo = "não encontrei o Hermes instalado nesta máquina"
		return d
	}

	codigo := "import importlib.metadata as m,sys;sys.stdout.write(m.version('hermes-agent'))"
	for _, p := range pythons {
		ctx, cancelar := context.WithTimeout(context.Background(), 25*time.Second)
		cmd := exec.CommandContext(ctx, p, "-c", codigo)
		cmd.Env = append(os.Environ(), "PYTHONPATH=", "PYTHONHOME=")
		saida, err := cmd.Output()
		cancelar()
		if err != nil {
			d.Motivo = "achei o Python do Hermes mas não consegui ler a versão: " + err.Error()
			continue
		}
		versao := strings.TrimSpace(string(saida))
		if versao == "" {
			continue
		}
		d.Instalado = true
		d.Caminho = p
		d.Versao = versao
		d.Motivo = ""
		return d
	}
	return d
}

// ResolverModo decide o motor EFETIVO e devolve o aviso (quando o escolhido não é possível).
//
// Regras:
//   - hermes escolhido e instalado → hermes
//   - hermes escolhido e sem Hermes → nativo (se houver chave) com aviso de como instalar
//   - nativo escolhido → nativo (com aviso se falta chave)
//   - auto → hermes se instalado; senão nativo
func ResolverModo(escolhido ModoAgente, h DiagnosticoHermes, temChaveNativa bool) (ModoAgente, string) {
	switch escolhido {
	case ModoHermes:
		if h.Instalado {
			return ModoHermes, ""
		}
		aviso := "O Hermes não está instalado nesta máquina, então usei o modo nativo."
		if !temChaveNativa {
			aviso += "\nConfigure uma chave grátis (NVIDIA ou Google) em Chaves grátis para o agente funcionar."
		}
		aviso += "\n\n" + comoInstalarHermes
		return ModoNativo, aviso
	case ModoNativo:
		if !temChaveNativa {
			return ModoNativo, "O modo nativo precisa de uma chave grátis. Abra Chaves grátis e configure NVIDIA ou Google."
		}
		return ModoNativo, ""
	default: // auto
		if h.Instalado {
			return ModoHermes, ""
		}
		aviso := ""
		if !temChaveNativa {
			aviso = "Nenhum motor disponível: instale o Hermes ou configure uma chave grátis."
		}
		return ModoNativo, aviso
	}
}

// ModoEscolhido lê a preferência salva no cofre.
func (c *CofreChaves) ModoEscolhido() ModoAgente {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch strings.ToLower(strings.TrimSpace(c.dados.Modo)) {
	case string(ModoHermes):
		return ModoHermes
	case string(ModoNativo):
		return ModoNativo
	}
	return ModoAuto
}

// DefinirModo salva a preferência do motor.
func (c *CofreChaves) DefinirModo(modo ModoAgente) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch modo {
	case ModoAuto, ModoHermes, ModoNativo:
		c.dados.Modo = string(modo)
	default:
		return os.ErrInvalid
	}
	return c.salvar()
}

// ---------------------------------------------------------------------------
// Ligação com a tela
// ---------------------------------------------------------------------------

// ModoAgenteHarness devolve tudo que a tela precisa para mostrar a escolha do motor.
func (a *App) ModoAgenteHarness() map[string]any {
	cofre := cofreDoApp()
	escolhido := cofre.ModoEscolhido()
	temChave := cofre.TotalChaves() > 0
	h := DetectarHermes()
	efetivo, aviso := ResolverModo(escolhido, h, temChave)
	return map[string]any{
		"escolhido":       string(escolhido),
		"efetivo":         string(efetivo),
		"aviso":           aviso,
		"hermesInstalado": h.Instalado,
		"hermesVersao":    h.Versao,
		"hermesCaminho":   h.Caminho,
		"hermesMotivo":    h.Motivo,
		"temChaveNativa":  temChave,
		"comoInstalar":    comoInstalarHermes,
	}
}

// SalvarModoAgenteHarness grava a escolha (auto/hermes/nativo).
func (a *App) SalvarModoAgenteHarness(modo string) string {
	if err := cofreDoApp().DefinirModo(ModoAgente(strings.TrimSpace(modo))); err != nil {
		return "modo inválido: use auto, hermes ou nativo"
	}
	return ""
}

// DetectarHermesHarness força uma nova detecção (botão "procurar de novo").
func (a *App) DetectarHermesHarness() map[string]any {
	h := DetectarHermes()
	return map[string]any{
		"instalado": h.Instalado,
		"versao":    h.Versao,
		"caminho":   h.Caminho,
		"motivo":    h.Motivo,
	}
}
