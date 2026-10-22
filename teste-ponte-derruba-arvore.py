#!/usr/bin/env python
# teste-ponte-derruba-arvore.py - derrubar o backend tem de levar a ARVORE inteira.
#
# No Hermes 0.21 o `hermes serve` e uma cadeia: bin\hermes.exe (lancador) -> python, e
# quem escuta a porta e o python. A ponte fazia `proc.terminate()`, que no Windows mata
# so o processo do topo. Quase sempre o lancador leva o filho junto, mas quando o serve
# se re-executa o NETO escapa (medido 13/10/2026: escutando a porta antiga depois de o
# app fechar). E o 0.21 recusa subir outro serve no mesmo home enquanto o orfao vive
# (codigo 78): a reconexao e a proxima subida do app falhavam.
#
# Aqui a cadeia e de mentira (python -> python dormindo): sem Hermes, sem rede, sem gasto.
#
#   python teste-ponte-derruba-arvore.py      (sai 0 = passou, 1 = falhou)
import ctypes
import os
import subprocess
import sys
import tempfile
import time

AQUI = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, AQUI)
import chat_bridge as cb  # noqa: E402

falhas = []


def vivo(pid: int) -> bool:
    k = ctypes.windll.kernel32
    h = k.OpenProcess(0x00100000 | 0x1000, False, pid)  # SYNCHRONIZE | QUERY_LIMITED_INFORMATION
    if not h:
        return False
    try:
        return k.WaitForSingleObject(h, 0) == 0x102     # WAIT_TIMEOUT = ainda rodando
    finally:
        k.CloseHandle(h)


def morreu(pid: int, prazo: float = 10) -> bool:
    fim = time.time() + prazo
    while time.time() < fim:
        if not vivo(pid):
            return True
        time.sleep(0.2)
    return False


def cadeia():
    """Sobe topo -> neto (os dois dormindo); devolve (proc do topo, pid do neto)."""
    arq = tempfile.mktemp(prefix="arvore-", suffix=".pid")
    codigo = ("import subprocess, sys, time\n"
              "n = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(120)'])\n"
              f"open({arq!r}, 'w').write(str(n.pid))\n"
              "time.sleep(120)\n")
    topo = subprocess.Popen([sys.executable, "-c", codigo],
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    fim = time.time() + 15
    while time.time() < fim:
        try:
            with open(arq, encoding="utf-8") as fh:
                txt = fh.read().strip()
            if txt:
                return topo, int(txt)
        except (OSError, ValueError):
            pass
        time.sleep(0.1)
    topo.kill()
    raise SystemExit("a cadeia de mentira nao subiu")


# controle: o jeito antigo (so terminate) deixa o neto vivo - senao o teste nao prova nada
topo, neto = cadeia()
topo.terminate()
topo.wait(timeout=10)
if morreu(neto, prazo=2):
    falhas.append("controle: terminate() ja levou o neto - o teste nao reproduz o orfao")
else:
    subprocess.run(["taskkill", "/PID", str(neto), "/F"], capture_output=True)

# o conserto: a arvore inteira cai
topo, neto = cadeia()
cb._derruba_arvore(topo)
if topo.poll() is None:
    falhas.append("o topo da arvore seguiu vivo")
if not morreu(neto):
    falhas.append(f"o neto (pid {neto}) ficou ORFAO - e o `hermes serve` de verdade")
    subprocess.run(["taskkill", "/PID", str(neto), "/F"], capture_output=True)

# sem processo, ou ja encerrado: nao pode estourar
try:
    cb._derruba_arvore(None)
    cb._derruba_arvore(topo)
except Exception as e:  # noqa: BLE001
    falhas.append(f"_derruba_arvore estourou com processo ausente/encerrado: {e!r}")

if falhas:
    print("FALHOU:\n  - " + "\n  - ".join(falhas))
    raise SystemExit(1)
print("OK: derrubar o backend leva a arvore inteira (o jeito antigo deixava o neto orfao)")
