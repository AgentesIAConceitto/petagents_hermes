#!/usr/bin/env python
# teste-espelho-ia.py - a IA SO DO BONECO (painel Inteligencia) muda o ESPELHO, nunca o dono.
#
# Com PETAGENT_IA + PETAGENT_IA_CHAVE no ambiente da ponte, o home espelhado do boneco
# tem de dizer o modelo/provedor do boneco (config.yaml) e ter a chave dele num .env
# PROPRIO (o Hermes carrega o .env com override=True: so variavel de ambiente perderia
# para a chave do dono). O config.yaml e o .env do dono ficam intactos. Sem as variaveis,
# tudo como sempre: .env ligado ao do dono por hardlink.
#
#   python teste-espelho-ia.py      (sai 0 = passou, 1 = falhou)
import json
import os
import sys
import tempfile

AQUI = os.path.dirname(os.path.abspath(__file__))
base = tempfile.mkdtemp(prefix="espelho-ia-")
os.environ["LOCALAPPDATA"] = base          # antes do import: o modulo le na carga
os.environ.pop("HERMES_HOME", None)
os.environ.pop("PETAGENT_IA", None)
os.environ.pop("PETAGENT_IA_CHAVE", None)
real = os.path.join(base, "hermes")
espelho = os.path.join(base, "petagent", "hermes-home")

CONFIG_DONO = ("# config do dono\n"
               "model:\n"
               "  # modelo padrao (comentario do dono)\n"
               '  default: "openai/gpt-6-luna"\n'
               '  provider: "openrouter"\n'
               '  base_url: "https://openrouter.ai/api/v1"\n'
               "  api_mode: chat_completions\n"
               "agent:\n"
               "  reasoning_effort: medium\n")
ENV_DONO = "OPENROUTER_API_KEY=sk-or-do-dono\nTELEGRAM_BOT_TOKEN=tg-do-dono\n"


def escreve(caminho, texto):
    os.makedirs(os.path.dirname(caminho), exist_ok=True)
    with open(caminho, "w", encoding="utf-8") as fh:
        fh.write(texto)


def le(caminho):
    with open(caminho, encoding="utf-8") as fh:
        return fh.read()


escreve(os.path.join(real, "config.yaml"), CONFIG_DONO)
escreve(os.path.join(real, ".env"), ENV_DONO)

sys.path.insert(0, AQUI)
import chat_bridge as cb  # noqa: E402

falhas = []
env_espelho = os.path.join(espelho, ".env")

# 1) com a IA do boneco
os.environ["PETAGENT_IA"] = json.dumps({"provedor": "deepseek", "modelo": "deepseek-chat",
                                        "base_url": "https://api.deepseek.com/v1",
                                        "var": "DEEPSEEK_API_KEY"})
os.environ["PETAGENT_IA_CHAVE"] = "sk-do-boneco"
if cb._home_do_backend() != espelho:
    falhas.append("o espelho nao foi montado")
cfg = le(os.path.join(espelho, "config.yaml"))
for esperado in ('default: "deepseek-chat"', 'provider: "deepseek"', 'base_url: "https://api.deepseek.com/v1"',
                 "# modelo padrao (comentario do dono)", "reasoning_effort: low"):
    if esperado not in cfg:
        falhas.append(f"config do espelho sem: {esperado}")
for proibido in ("gpt-6-luna", "openrouter", "api_mode"):
    if proibido in cfg:
        falhas.append(f"config do espelho ainda tem: {proibido}")
if os.path.samefile(env_espelho, os.path.join(real, ".env")):
    falhas.append(".env do espelho continua LIGADO ao do dono (a chave do dono venceria)")
env = le(env_espelho)
if "DEEPSEEK_API_KEY=sk-do-boneco" not in env:
    falhas.append(".env do espelho sem a chave do boneco")
if "sk-or-do-dono" in env:
    falhas.append(".env do espelho ainda tem a chave de IA do dono")
if "TELEGRAM_BOT_TOKEN=tg-do-dono" not in env:
    falhas.append(".env do espelho perdeu o resto do .env do dono")
if "PETAGENT_IA_CHAVE" in cb._env_do_backend("t"):
    falhas.append("a chave foi solta no ambiente do backend")

# 2) o dono nao pode ter sido tocado
if le(os.path.join(real, "config.yaml")) != CONFIG_DONO:
    falhas.append("o config.yaml do DONO mudou")
if le(os.path.join(real, ".env")) != ENV_DONO:
    falhas.append("o .env do DONO mudou")

# 3) sem a IA do boneco: volta a ser como sempre (hardlink, modelo do dono)
os.environ.pop("PETAGENT_IA")
os.environ.pop("PETAGENT_IA_CHAVE")
cb._home_do_backend()
if not os.path.samefile(env_espelho, os.path.join(real, ".env")):
    falhas.append("sem IA do boneco, o .env do espelho deveria voltar a ser o do dono")
if 'default: "openai/gpt-6-luna"' not in le(os.path.join(espelho, "config.yaml")):
    falhas.append("sem IA do boneco, o config do espelho deveria ter o modelo do dono")

# 4) forma curta `model: "x"` e config sem bloco model
mini = cb._troca_modelo('model: "velho"\nagent:\n  x: 1\n', {"modelo": "m", "provedor": "p", "base_url": ""})
if 'default: "m"' not in mini or 'provider: "p"' not in mini or "velho" in mini:
    falhas.append("forma curta model: \"x\" nao virou bloco com o modelo do boneco")
vazio = cb._troca_modelo("agent:\n  x: 1\n", {"modelo": "m", "provedor": "p", "base_url": "u"})
if "model:" not in vazio or 'base_url: "u"' not in vazio:
    falhas.append("config sem bloco model: nao ganhou o bloco")

# 5) mesmo provedor do dono, outro modelo: so o modelo troca (o api_mode do provedor fica)
mesmo = cb._troca_modelo(CONFIG_DONO, {"modelo": "outro/modelo", "provedor": "openrouter",
                                       "base_url": "https://openrouter.ai/api/v1"})
if 'default: "outro/modelo"' not in mesmo or "api_mode: chat_completions" not in mesmo:
    falhas.append("mesmo provedor: deveria trocar so o modelo e manter o api_mode")

if falhas:
    print("FALHOU:\n  - " + "\n  - ".join(falhas))
    raise SystemExit(1)
print("OK: IA so do boneco vai para o espelho (config + .env proprio); o dono fica intacto")
