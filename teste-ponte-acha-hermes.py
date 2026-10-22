#!/usr/bin/env python
# teste-ponte-acha-hermes.py - a ponte tem de achar o Hermes 0.21+ no Windows.
#
# O Hermes novo nao tem mais hermes-agent\venv (nem venv-dashboard): o comando e
# hermes\bin\hermes.exe, um lancador que ja escolhe o python e o venv certos. Sem
# isto a ponte logava "hermes nao encontrado" e o chat caia na reserva lenta.
#
#   python teste-ponte-acha-hermes.py      (sai 0 = passou, 1 = falhou)
import os
import sys
import tempfile

AQUI = os.path.dirname(os.path.abspath(__file__))


def cria(*partes):
    p = os.path.join(*partes)
    os.makedirs(os.path.dirname(p), exist_ok=True)
    open(p, "wb").close()
    return p


base = tempfile.mkdtemp(prefix="acha-hermes-")
os.environ["LOCALAPPDATA"] = base          # antes do import: o modulo le na carga
os.environ.pop("PETAGENT_HERMES_VENV", None)
exe = cria(base, "hermes", "bin", "hermes.exe")

sys.path.insert(0, AQUI)
import chat_bridge as cb  # noqa: E402

cmd = cb._achar_hermes()
if cmd != [exe]:
    print(f"FALHOU: _achar_hermes() = {cmd}\n        queria {[exe]}")
    raise SystemExit(1)
print(f"OK: achou o lancador do Hermes novo ({exe})")
