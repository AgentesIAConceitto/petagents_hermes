#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""Ponte do chat do PetAgent — BACKEND PRÓPRIO do boneco (`hermes serve` + /api/ws).

Arquitetura validada em 02/10/2026 (com números):
    * `hermes -z`        -> sobe agente novo a cada mensagem ......... ~30-290 s ✗
    * `hermes acp`       -> rápido para conversa, mas as FERRAMENTAS
                            travam ("Creating new local environment") ✗
    * `hermes serve`     -> processo PRÓPRIO, agente quente, e as ferramentas
                            FUNCIONAM (testado: executou terminal em 23,6 s) ✓

    app (Go) --stdin/stdout--> esta ponte --WebSocket--> hermes serve (próprio)

Por que isto é seguro para o usuário: o backend é um processo separado, do MESMO
perfil (mesma memória/personalidade/skills), sem reiniciar nem editar nada do
gateway que já está rodando.

Protocolo com o app (1 JSON por linha):
    app -> {"prompt": "texto"}
    app <- {"ok": true, "pronto": true, ...}          (handshake)
    app <- {"ok": true, "resposta": "...", "segundos": 3.2}
"""
from __future__ import annotations

import argparse
import asyncio
import json
import os
import secrets
import socket
import subprocess
import sys
import time

try:
    import websockets
except ImportError:  # pragma: no cover
    print(json.dumps({"ok": False, "erro": "websockets ausente no venv"}))
    raise SystemExit(2)

AQUI = os.path.dirname(os.path.abspath(__file__))
LOCALAPPDATA = os.environ.get("LOCALAPPDATA", "")


def _achar_hermes() -> list[str]:
    """Comando do backend próprio: um `hermes` que FUNCIONE — sem alterar a
    instalação do dono (regra do plugin: o PetAgent não pode exigir que se mexa no
    Hermes instalado).

    Duas armadilhas pagas em 03/10/2026, quando o chat do boneco caiu calado com
    "backend não subiu":

    1. O Hermes mantém mais de um venv, e o `venv-dashboard` é o que ELE MESMO
       montou com os pins certos do stack web (`fastapi==0.133.1` +
       `starlette==1.3.1`, iguais aos do `pyproject.toml` dele). O venv principal
       ficou com fastapi estale (0.115.0) e o `hermes serve` morria no import com
       `Router.__init__() got an unexpected keyword argument 'on_startup'`.
    2. Não adianta chamar `Scripts\\hermes.exe`: esse launcher amarra um
       interpretador que IGNORA o ambiente limpo e segue pegando o venv errado
       (medido: hermes.exe morria, `python.exe -m hermes_cli.main` subiu em ~6 s).
       Chamando o python do venv escolhido, o venv escolhido manda.

    E o PYTHONPATH herdado precisa sair do caminho — ver `_env_do_backend()`.
    """
    venvs = [
        os.environ.get("PETAGENT_HERMES_VENV", ""),
        os.path.join(LOCALAPPDATA, "hermes", "venv-dashboard"),
        os.path.join(LOCALAPPDATA, "hermes", "hermes-agent", "venv"),
    ]
    for v in venvs:
        if v and os.path.exists(os.path.join(v, "Scripts", "python.exe")):
            return [os.path.join(v, "Scripts", "python.exe"), "-m", "hermes_cli.main"]
    # nada encontrado: o log de "não encontrado" aponta onde eu olhei
    return [os.path.join(venvs[-1], "Scripts", "python.exe"), "-m", "hermes_cli.main"]


HERMES_CMD = _achar_hermes()


# ── home espelhado: herda o home do Hermes, mas sem o MCP que polui a tela ──────
#
# MEDIDO em 03/10/2026 (vigia por evento de criação de processo): o backend do chat
# sobe TODOS os MCPs do dono — inclusive `cua-driver` — e CADA instância dele abre uma
# janela `Cua.AgentCursorOverlay` de 3840x1440, isto é, as DUAS telas. Invisível, mas
# presente: atrapalha qualquer ferramenta que capture ou grave UMA janela no PC do dono
# (a "janela do topo" vira ela). O boneco não precisa de computer-use para conversar;
# o dono precisa da tela limpa.
#
# Como, sem tocar no Hermes do dono: `HERMES_HOME` é o que define qual `config.yaml` é
# lido — o próprio código do Hermes diz isso ("profile switches change HERMES_HOME and
# therefore get_config_path()"). Então o backend sobe com um HOME ESPELHADO: tudo do
# home real entra por junction (pasta) ou hardlink (arquivo) — skills, plugins,
# memórias, sessões, .env — e o `config.yaml` é um arquivo NOSSO: cópia do dele sem a
# entrada `mcp_servers.cua-driver`, regerada a cada subida (não envelhece).
HOME_ESPELHO = os.path.join(os.environ.get("LOCALAPPDATA", AQUI), "petagent", "hermes-home")
MCP_A_REMOVER = ("cua-driver",)


def _home_real() -> str:
    return os.environ.get("HERMES_HOME") or os.path.join(
        os.environ.get("LOCALAPPDATA", AQUI), "hermes")


def _tira_mcp(texto: str, nomes) -> str:
    """Remove, de um config.yaml em texto, o bloco de cada MCP em `nomes`.

    Textual de propósito: não depende de PyYAML no venv que roda a ponte, e o
    config.yaml do dono é legível. O bloco vai da linha `  nome:` até a primeira
    linha com indentação menor ou igual (ou o fim).
    """
    linhas = texto.splitlines(True)
    saida, i = [], 0
    while i < len(linhas):
        linha = linhas[i]
        chave = linha.strip()
        alvo = any(chave in (n + ":", '"' + n + '":') for n in nomes) and linha[:1] in (" ", "\t")
        if alvo:
            indent = len(linha) - len(linha.lstrip())
            i += 1
            while i < len(linhas):
                prox = linhas[i]
                if not prox.strip():
                    i += 1
                    continue
                if (len(prox) - len(prox.lstrip())) > indent:
                    i += 1
                    continue
                break
            continue
        saida.append(linha)
        i += 1
    return "".join(saida)


def _home_do_backend() -> str:
    """Prepara o home espelhado e devolve o caminho (vai em HERMES_HOME)."""
    real = _home_real()
    try:
        os.makedirs(HOME_ESPELHO, exist_ok=True)
        # 1) espelha o que NÃO é config.yaml (junction para pasta, hardlink para arquivo)
        for nome in os.listdir(real):
            if nome in ("config.yaml", "config.yml"):
                continue
            origem = os.path.join(real, nome)
            destino = os.path.join(HOME_ESPELHO, nome)
            if os.path.exists(destino):
                continue
            if os.path.isdir(origem):
                subprocess.run(["cmd", "/c", "mklink", "/J", destino, origem],
                               capture_output=True)
            else:
                subprocess.run(["cmd", "/c", "mklink", "/H", destino, origem],
                               capture_output=True)
        # 2) config.yaml: cópia do dele sem os MCPs que poluem a tela
        with open(os.path.join(real, "config.yaml"), "r", encoding="utf-8") as fh:
            bruto = fh.read()
        limpo = _tira_mcp(bruto, MCP_A_REMOVER)
        with open(os.path.join(HOME_ESPELHO, "config.yaml"), "w", encoding="utf-8") as fh:
            fh.write(limpo)
        removidos = len(bruto) - len(limpo)
        log("home espelhado pronto (%s): config.yaml sem %s (%d bytes a menos)"
            % (HOME_ESPELHO, ", ".join(MCP_A_REMOVER), removidos))
    except Exception as e:  # noqa: BLE001
        # degradado, mas não quebra o chat: segue com o home real (e o overlay volta)
        log("home espelhado FALHOU (%r) — seguindo com o home real" % (e,))
        return ""
    return HOME_ESPELHO


def _env_do_backend(token: str) -> dict:
    """Ambiente do backend próprio.

    PYTHONPATH SAI: herdado, ele põe `hermes-agent\\venv\\Lib\\site-packages` antes
    do site-packages do venv escolhido, e aí o fastapi velho do venv principal
    sombreia o fastapi correto do outro venv (um `import fastapi` ainda "existe",
    então ninguém percebe — só quebra na hora de construir o app).

    HERMES_HOME vai para o HOME ESPELHADO: herda skills/plugins/memórias/sessões/.env
    do dono por junction+hardlink, mas o config.yaml é nosso — sem o MCP `cua-driver`,
    que abria uma janela de 3840x1440 em cima de tudo (ver _home_do_backend).
    """
    env = dict(os.environ)
    env.pop("PYTHONPATH", None)
    espelho = _home_do_backend()
    if espelho:
        env["HERMES_HOME"] = espelho
    env["HERMES_DASHBOARD_SESSION_TOKEN"] = token  # token local, meu
    env["PYTHONIOENCODING"] = "utf-8"
    return env


def _motivo_backend() -> str:
    """Última linha do backend.log = o erro de verdade (a última linha de um traceback)."""
    try:
        with open(os.path.join(AQUI, "backend.log"), "rb") as fh:
            linhas = [l for l in fh.read().splitlines() if l.strip()]
        return linhas[-1].decode("utf-8", "replace")[:300] if linhas else "(backend.log vazio)"
    except Exception:  # noqa: BLE001
        return "(não consegui ler o backend.log)"


CWD_PADRAO = os.path.expanduser("~")
PORTA_PADRAO = int(os.environ.get("PETAGENT_PORTA_BACKEND", "9131"))

# Windows + stdio em PIPE: sem isto, qualquer acento derruba a ponte (02/10/2026)
for _fluxo in ("stdin", "stdout", "stderr"):
    try:
        getattr(sys, _fluxo).reconfigure(encoding="utf-8", errors="replace")
    except Exception:  # noqa: BLE001
        pass

ARQ_LOG = os.path.join(os.environ.get("LOCALAPPDATA", AQUI), "petagent", "ponte.log")

# eventos já conhecidos do backend (não precisam virar log; os NOVOS viram)
_EV_CONHECIDOS = {
    "gateway.ready", "session.info", "sessions.changed", "reasoning.available",
    "thinking.delta", "message.start", "message.delta", "message.complete",
    "tool.start", "tool.complete", "tool.delta",
}


def log(msg: str) -> None:
    linha = f"[ponte] {msg}"
    try:
        os.makedirs(os.path.dirname(ARQ_LOG), exist_ok=True)
        with open(ARQ_LOG, "a", encoding="utf-8") as fh:
            fh.write(time.strftime("%d/%m %H:%M:%S ") + linha + "\n")
    except OSError:
        pass
    try:
        print(linha, file=sys.stderr, flush=True)
    except Exception:  # noqa: BLE001
        pass


def porta_aberta(porta: int) -> bool:
    try:
        with socket.create_connection(("127.0.0.1", porta), timeout=2):
            return True
    except OSError:
        return False


class Backend:
    """O Hermes do boneco: processo próprio (`hermes serve`) + canal /api/ws."""

    def __init__(self, porta: int) -> None:
        self.porta = porta
        self.token = os.environ.get("PETAGENT_TOKEN_BACKEND") or secrets.token_urlsafe(24)
        self.proc: subprocess.Popen | None = None
        self.ws = None
        self.sessao = ""
        self.seq = 0

    # ---------- subida ----------
    def subir_processo(self) -> bool:
        if not os.path.exists(HERMES_CMD[0]):
            log(f"hermes não encontrado em {HERMES_CMD[0]}")
            return False
        env = _env_do_backend(self.token)
        cmd = HERMES_CMD + ["serve", "--port", str(self.porta), "--host", "127.0.0.1", "--skip-build"]
        log(f"backend: {HERMES_CMD[0]} -m hermes_cli.main serve (PYTHONPATH herdado fora)")
        kwargs: dict = {}
        if os.name == "nt":
            si = subprocess.STARTUPINFO()
            si.dwFlags |= subprocess.STARTF_USESHOWWINDOW
            si.wShowWindow = 0  # SW_HIDE: console oculto herdado, nada pipoca
            kwargs["startupinfo"] = si
        try:
            arq = open(os.path.join(AQUI, "backend.log"), "ab", buffering=0)
        except OSError:
            arq = subprocess.DEVNULL  # type: ignore[assignment]
        self.proc = subprocess.Popen(
            cmd,
            env=env, stdout=arq, stderr=arq, cwd=CWD_PADRAO, **kwargs,
        )
        t0 = time.time()
        while time.time() - t0 < 200:
            if porta_aberta(self.porta):
                log(f"backend próprio no ar em {time.time() - t0:.1f}s (porta {self.porta})")
                return True
            if self.proc.poll() is not None:
                # o motivo TEM de aparecer aqui: antes era só "backend não subiu" e
                # ninguém sabia que o hermes serve tinha morrido no import (03/10/2026)
                log(f"backend morreu ao subir (código {self.proc.returncode}) — motivo: {_motivo_backend()}")
                return False
            time.sleep(1.5)
        log("backend não subiu a tempo")
        return False

    async def rpc(self, metodo: str, params: dict, timeout: float = 240) -> dict:
        self.seq += 1
        meu = f"{metodo}#{self.seq}"
        await self.ws.send(json.dumps({"jsonrpc": "2.0", "id": meu, "method": metodo,
                                       "params": params}))
        limite = time.time() + timeout
        while time.time() < limite:
            raw = await asyncio.wait_for(self.ws.recv(), timeout=max(1, limite - time.time()))
            try:
                d = json.loads(raw)
            except (json.JSONDecodeError, TypeError):
                continue
            if not isinstance(d, dict):
                continue
            if str(d.get("id") or "") == meu:
                return d
            # eventos do gateway chegam antes: só registra os interessantes
            p = d.get("params") if isinstance(d.get("params"), dict) else {}
            if str(p.get("type") or "") not in ("", "gateway.ready"):
                log(f"evento: {str(p.get('type'))[:40]}")
        return {"erro": "tempo esgotado"}

    async def conectar(self) -> bool:
        url = f"ws://127.0.0.1:{self.porta}/api/ws?token={self.token}"
        self.ws = await websockets.connect(url, open_timeout=30, max_size=32 << 20)
        r = await self.rpc("session.create", {"cwd": CWD_PADRAO}, timeout=240)
        res = r.get("result") if isinstance(r.get("result"), dict) else {}
        self.sessao = str(res.get("session_id") or "")
        if not self.sessao:
            log(f"session.create falhou: {json.dumps(r)[:200]}")
            return False
        info = res.get("info") if isinstance(res.get("info"), dict) else {}
        log(f"sessão {self.sessao} criada · modelo={info.get('model')} · cwd={info.get('cwd')}")
        return True

    async def subir(self) -> bool:
        if not porta_aberta(self.porta):
            if not self.subir_processo():
                return False
        else:
            log("já havia backend nesta porta (reusando)")
        try:
            return await self.conectar()
        except Exception as e:  # noqa: BLE001
            log(f"canal falhou: {type(e).__name__}: {e}")
            return False

    # ---------- perguntar ----------
    async def perguntar(self, texto: str, timeout: float = 300) -> tuple[str, float]:
        t0 = time.time()
        await self.ws.send(json.dumps({"jsonrpc": "2.0", "id": f"p{self.seq + 1}",
                                       "method": "prompt.submit",
                                       "params": {"session_id": self.sessao, "text": texto}}))
        self.seq += 1
        pedacos: list[str] = []
        while time.time() - t0 < timeout:
            try:
                raw = await asyncio.wait_for(self.ws.recv(),
                                             timeout=max(1, timeout - (time.time() - t0)))
            except asyncio.TimeoutError:
                return "".join(pedacos).strip() or "(tempo esgotado)", time.time() - t0
            try:
                d = json.loads(raw)
            except (json.JSONDecodeError, TypeError):
                continue
            if not isinstance(d, dict):
                continue
            p = d.get("params") if isinstance(d.get("params"), dict) else {}
            tipo = str(p.get("type") or d.get("method") or "")
            if tipo and tipo not in _EV_CONHECIDOS:
                log(f"ev novo={tipo} chaves={sorted(p.keys())} "
                    f"payload={json.dumps(p.get('payload'), ensure_ascii=False)[:200]}")
            if tipo == "message.delta":
                carga = p.get("payload") if isinstance(p.get("payload"), dict) else p
                txt = carga.get("text") or carga.get("delta")
                if isinstance(txt, dict):
                    txt = txt.get("text")
                if isinstance(txt, str):
                    pedacos.append(txt)
            elif tipo == "message.complete":
                break
            # thinking.delta / reasoning.available são PENSAMENTO: não entram na resposta
            if "error" in d and not pedacos:
                return "erro: " + json.dumps(d.get("error"))[:300], time.time() - t0
        return "".join(pedacos).strip() or "(sem texto)", time.time() - t0


def porta_livre() -> int:
    """Porta livre escolhida na hora: evita colidir com backend órfão de outra
    execução (dois backends com tokens diferentes na mesma porta = confusão)."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def principal() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--teste", default="")
    ap.add_argument("--porta", type=int, default=0)
    args = ap.parse_args()

    porta = args.porta or int(os.environ.get("PETAGENT_PORTA_BACKEND") or 0) or porta_livre()
    bk = Backend(porta)
    try:
        return asyncio.run(servico(bk, args))
    except KeyboardInterrupt:
        return 0


async def servico(bk: "Backend", args) -> int:
    """TUDO num único laço de eventos: o WebSocket vive neste laço — chamar
    asyncio.run duas vezes derrubava a conexão ('Event loop is closed')."""
    t0 = time.time()
    if not await bk.subir():
        log("não consegui subir o backend próprio")
        print(json.dumps({"ok": False, "pronto": False, "erro": "backend não subiu"}), flush=True)
        return 3
    log(f"pronto em {time.time() - t0:.1f}s (porta {bk.porta})")

    if args.teste:
        resp, seg = await bk.perguntar(args.teste)
        print(json.dumps({"ok": True, "resposta": resp, "segundos": round(seg, 1)},
                         ensure_ascii=False), flush=True)
        if bk.proc and bk.proc.poll() is None:
            bk.proc.terminate()
        return 0

    # handshake: o app só pergunta depois disto
    print(json.dumps({"ok": True, "pronto": True, "sessao": bk.sessao}), flush=True)
    while True:
        linha = await asyncio.to_thread(sys.stdin.readline)
        if not linha:
            break
        try:
            pedido = json.loads(linha)
        except json.JSONDecodeError:
            continue
        p = str(pedido.get("prompt") or "").strip()
        if not p:
            continue
        try:
            resp, seg = await bk.perguntar(p)
            print(json.dumps({"ok": True, "resposta": resp, "segundos": round(seg, 1)},
                             ensure_ascii=False), flush=True)
        except Exception as e:  # noqa: BLE001
            print(json.dumps({"ok": False, "erro": f"{type(e).__name__}: {e}"}), flush=True)
    # o app fechou: leva o backend junto (sem órfãos)
    if bk.proc and bk.proc.poll() is None:
        try:
            bk.proc.terminate()
        except Exception:  # noqa: BLE001
            pass
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(principal())
    except KeyboardInterrupt:
        raise SystemExit(0)
