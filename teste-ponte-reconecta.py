#!/usr/bin/env python
# teste-ponte-reconecta.py - reproduz o incidente de 05/10/2026 17:36 e exige o conserto.
#
# Cadeia real do incidente:
#   17:36:53  ponte: erro do agente (ConnectionClosedError ... keepalive ping timeout)
#             -> reserva lenta (hermes -z, que respondeu em 2+ minutos)
#   17:39:08  chat: reserva (hermes -z) respondeu 553 chars
#
# O que o teste faz:
#   1) sobe um backend próprio pelo MESMO caminho de produção (chat_bridge.Backend)
#   2) pergunta uma vez  -> prova que o trilho quente funciona
#   3) MATA o processo do backend (simula o serve morto/travado que derrubou o WS)
#   4) pergunta DE NOVO -> exige resposta quente, sem cair na reserva lenta
#   5) exige que seja um backend NOVO (pid diferente) = reconexão de verdade
#
# ANTES do conserto: o passo 4 estoura ConnectionClosedError -> FALHA.
# DEPOIS: a ponte reconecta sozinha -> PASSA.
#
# Nao toca no boneco que esta rodando: usa porta livre e token próprios.
import asyncio
import os
import sys

AQUI = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, AQUI)
import chat_bridge as cb  # noqa: E402


def limpar(bk):
    try:
        if bk.proc and bk.proc.poll() is None:
            bk.proc.terminate()
    except Exception:
        pass


async def principal() -> int:
    porta = cb.porta_livre()
    bk = cb.Backend(porta)
    print(f"[teste] subindo backend próprio na porta {porta} (mesmo caminho de produção)")
    if not await bk.subir():
        print("[teste] FALHA: nem o backend inicial subiu (problema anterior ao escopo do teste)")
        return 2

    pid_antes = bk.proc.pid if bk.proc else None
    print(f"[teste] backend no ar: pid {pid_antes}")

    # ---- 1) trilho quente funcionando ----
    try:
        resp1, seg1 = await bk.perguntar("responda apenas com a palavra: um")
    except Exception as e:
        print(f"[teste] FALHA no passo 1 (pergunta inicial): {type(e).__name__}: {e}")
        limpar(bk)
        return 1
    print(f"[teste] 1ª resposta quente em {seg1:.1f}s: {resp1[:60]!r}")
    if not resp1.strip():
        print("[teste] FALHA: 1ª resposta veio vazia")
        limpar(bk)
        return 1

    # ---- 2) mata o backend (o que aconteceu às 17:36) ----
    print(f"[teste] matando o backend (pid {pid_antes}) para simular o serve travado…")
    try:
        bk.proc.kill()
        bk.proc.wait(timeout=20)
    except Exception:
        pass
    await asyncio.sleep(3)

    # ---- 3) pergunta de novo: TEM de reconectar sozinho ----
    try:
        resp2, seg2 = await bk.perguntar("responda apenas com a palavra: dois")
    except Exception as e:
        print(f"[teste] FALHA: o canal morreu e a ponte NÃO reconectou "
              f"({type(e).__name__}: {e})")
        print("[teste] Este é exatamente o defeito de 05/10: cai para a reserva lenta.")
        limpar(bk)
        return 1

    pid_depois = bk.proc.pid if bk.proc else None
    print(f"[teste] 2ª resposta em {seg2:.1f}s: {resp2[:60]!r}  (backend agora pid {pid_depois})")

    falhas = []
    if not resp2.strip():
        falhas.append("2ª resposta veio vazia")
    if pid_depois == pid_antes:
        falhas.append(f"backend NÃO foi renovado (mesmo pid {pid_antes}) = não reconectou de verdade")
    if "reserva" in resp2.lower():
        falhas.append("resposta veio marcada como reserva lenta")

    limpar(bk)
    if falhas:
        print("[teste] FALHA:")
        for f in falhas:
            print("   - " + f)
        return 1

    print(f"[teste] PASSOU: reconectou sozinho e respondeu quente em {seg2:.1f}s "
          f"(backend {pid_antes} -> {pid_depois})")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(asyncio.run(principal()))
    except KeyboardInterrupt:
        raise SystemExit(130)
