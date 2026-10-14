#!/usr/bin/env python
# teste-limite-ia.py - o backend do boneco tem TETO de iteracoes da IA por mensagem.
#
# O laco que gasta credito e o do agente: chama o modelo, roda ferramenta, chama de novo.
# O dono tem `agent.max_turns: 150` (e 250 por subagente em `delegation.max_iterations`);
# um agente em loop queimava os creditos de uma vez. O boneco sobe com teto de 20:
# pela variavel HERMES_TUI_MAX_TURNS (vence o config no `hermes serve`) e no config.yaml
# espelhado (cobre os subagentes). Cuidado: no Hermes 0, -1 e "none" = ILIMITADO.
#
#   python teste-limite-ia.py      (sai 0 = passou, 1 = falhou)
import os
import sys
import tempfile

AQUI = os.path.dirname(os.path.abspath(__file__))
base = tempfile.mkdtemp(prefix="limite-ia-")
os.environ["LOCALAPPDATA"] = base          # antes do import: o modulo le na carga
os.environ.pop("HERMES_HOME", None)
real = os.path.join(base, "hermes")
espelho = os.path.join(base, "petagent", "hermes-home")

sys.path.insert(0, AQUI)
import chat_bridge as cb  # noqa: E402

falhas = []


def valor(texto, bloco, chave):
    """Valor de `chave` dentro do bloco de topo `bloco` (None se nao achar)."""
    dentro = False
    for l in texto.split("\n"):
        if l.startswith(bloco + ":"):
            dentro = True
            continue
        if dentro and l and not l[0].isspace() and not l.startswith("#"):
            dentro = False
        if dentro and l.strip().startswith(chave + ":"):
            return l.split(":", 1)[1].split("#")[0].strip()
    return None


def confere(nome, entrada, esperado_agent, esperado_deleg="20"):
    saida, _ = cb._limita_iteracoes(entrada)
    a, d = valor(saida, "agent", "max_turns"), valor(saida, "delegation", "max_iterations")
    if a != esperado_agent:
        falhas.append(f"{nome}: agent.max_turns={a!r}, esperado {esperado_agent!r}")
    if d != esperado_deleg:
        falhas.append(f"{nome}: delegation.max_iterations={d!r}, esperado {esperado_deleg!r}")
    return saida


confere("150 do dono", "agent:\n  max_turns: 150\ndelegation:\n  max_iterations: 250\n", "20")
confere("10 ja e menor", "agent:\n  max_turns: 10\ndelegation:\n  max_iterations: 5\n", "10", "5")
for ilimitado in ("0", "-1", "none", '"unlimited"', "abc"):
    confere(f"max_turns={ilimitado}", f"agent:\n  max_turns: {ilimitado}\n", "20")
confere("comentario na linha", "agent:\n  max_turns: 150   # bastante\n", "20")
confere("chave ausente", "agent:\n  reasoning_effort: low\ndelegation:\n  model: x\n", "20")
confere("blocos ausentes", "model:\n  provider: openrouter\n", "20")

# chave de mesmo nome em OUTRO bloco nao pode ser tocada
outro = confere("outro bloco", "goals:\n  max_turns: 99\nagent:\n  max_turns: 150\n", "20")
if valor(outro, "goals", "max_turns") != "99":
    falhas.append("goals.max_turns foi alterado (so agent/delegation podem mudar)")
# linha comentada nao conta como a chave
comentada = confere("chave so comentada", "agent:\n  # max_turns: 500\n  reasoning_effort: low\n", "20")
if "# max_turns: 500" not in comentada:
    falhas.append("o comentario do dono sumiu")

# de ponta a ponta: o ambiente do backend e o config.yaml espelhado
os.makedirs(real, exist_ok=True)
with open(os.path.join(real, "config.yaml"), "w", encoding="utf-8") as fh:
    fh.write("agent:\n  max_turns: 150\n  reasoning_effort: medium\n"
             "delegation:\n  max_iterations: 250\n")
env = cb._env_do_backend("tok")
if env.get("HERMES_TUI_MAX_TURNS") != "20":
    falhas.append(f"HERMES_TUI_MAX_TURNS={env.get('HERMES_TUI_MAX_TURNS')!r}, esperado '20'")
with open(os.path.join(espelho, "config.yaml"), encoding="utf-8") as fh:
    cfg = fh.read()
if valor(cfg, "agent", "max_turns") != "20":
    falhas.append("config.yaml espelhado sem agent.max_turns: 20")
if valor(cfg, "delegation", "max_iterations") != "20":
    falhas.append("config.yaml espelhado sem delegation.max_iterations: 20")
with open(os.path.join(real, "config.yaml"), encoding="utf-8") as fh:
    if "max_turns: 150" not in fh.read():
        falhas.append("o config.yaml do DONO foi alterado (so o espelho pode mudar)")

if falhas:
    print("FALHOU:\n  - " + "\n  - ".join(falhas))
    raise SystemExit(1)
print("OK: teto de 20 iteracoes no ambiente e no config espelhado; config do dono intacto")
