#!/usr/bin/env python
# teste-espelho.py - o home espelhado do boneco nao pode LIGAR os bancos SQLite do dono.
#
# O espelho liga cada ARQUIVO da raiz do home do Hermes por hardlink. Para um banco
# SQLite isso e perigoso: o gateway do dono abre hermes\state.db e o backend do boneco
# abre hermes-home\state.db - o mesmo arquivo por dois caminhos. O SQLite deriva o
# -wal/-shm do CAMINHO, entao os dois acabam com WAL separado para o mesmo banco ->
# risco de corromper o historico do dono. (Pastas vao por junction: ali o -wal cai no
# mesmo lugar, sem problema.)
#
# Tambem exige que o link de arquivo seja REFEITO a cada subida: o Hermes regrava o
# .env trocando o arquivo, e o hardlink antigo seguia apontando para a chave velha.
#
#   python teste-espelho.py      (sai 0 = passou, 1 = falhou)
import os
import sys
import tempfile

AQUI = os.path.dirname(os.path.abspath(__file__))
base = tempfile.mkdtemp(prefix="espelho-")
os.environ["LOCALAPPDATA"] = base          # antes do import: o modulo le na carga
os.environ.pop("HERMES_HOME", None)
real = os.path.join(base, "hermes")
espelho = os.path.join(base, "petagent", "hermes-home")


def escreve(caminho, texto):
    os.makedirs(os.path.dirname(caminho), exist_ok=True)
    with open(caminho, "w", encoding="utf-8") as fh:
        fh.write(texto)


escreve(os.path.join(real, "config.yaml"), "agent:\n  reasoning_effort: medium\n")
escreve(os.path.join(real, ".env"), "OPENROUTER_API_KEY=velha\n")
escreve(os.path.join(real, "SOUL.md"), "personalidade\n")
for nome in ("state.db", "state.db-wal", "state.db-shm", "kanban.db", "gateway.pid", "auth.lock"):
    escreve(os.path.join(real, nome), "x")
escreve(os.path.join(real, "memories", "MEMORY.md"), "memoria\n")

# espelho de versao antiga: o state.db ja estava LIGADO ao do dono
os.makedirs(espelho, exist_ok=True)
os.link(os.path.join(real, "state.db"), os.path.join(espelho, "state.db"))

sys.path.insert(0, AQUI)
import chat_bridge as cb  # noqa: E402

falhas = []


def ligado(nome):
    e, r = os.path.join(espelho, nome), os.path.join(real, nome)
    return os.path.exists(e) and os.path.samefile(e, r)


if cb._home_do_backend() != espelho:
    falhas.append("o espelho nao foi montado")
for nome in ("state.db", "state.db-wal", "state.db-shm", "kanban.db", "gateway.pid", "auth.lock"):
    if ligado(nome):
        falhas.append(f"{nome} ficou ligado ao do dono")
for nome in (".env", "SOUL.md"):
    if not ligado(nome):
        falhas.append(f"{nome} deveria estar ligado ao do dono")
if not os.path.exists(os.path.join(espelho, "memories", "MEMORY.md")):
    falhas.append("memories/ nao foi espelhada")

# o Hermes regrava o .env TROCANDO o arquivo; a proxima subida tem de ver a chave nova
tmp = os.path.join(real, ".env.tmp")
escreve(tmp, "OPENROUTER_API_KEY=nova\n")
os.replace(tmp, os.path.join(real, ".env"))
cb._home_do_backend()
with open(os.path.join(espelho, ".env"), encoding="utf-8") as fh:
    if "nova" not in fh.read():
        falhas.append(".env do espelho ficou com a chave VELHA depois de o dono trocar")

if falhas:
    print("FALHOU:\n  - " + "\n  - ".join(falhas))
    raise SystemExit(1)
print("OK: bancos e travas fora do espelho; .env/SOUL.md ligados e atualizados")
