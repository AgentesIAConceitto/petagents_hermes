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
    # Hermes 0.21+ (Windows): não há mais venv fixo. O lançador `bin\hermes.exe` escolhe
    # o python e o venv da instalação, tira PYTHONPATH/PYTHONHOME e respeita HERMES_HOME
    # (o home espelhado) — a armadilha 2 acima era do lançador antigo.
    novo = os.path.join(LOCALAPPDATA, "hermes", "bin", "hermes.exe")
    if os.path.exists(novo):
        return [novo]
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
# entrada `mcp_servers.cua-driver`, regerada a cada subida (não envelhece). Os bancos
# SQLite e as travas da raiz ficam DE FORA (ver _fora_do_espelho).
HOME_ESPELHO = os.path.join(os.environ.get("LOCALAPPDATA", AQUI), "petagent", "hermes-home")
MCP_A_REMOVER = ("cua-driver",)
BLOCO_CLARIFY = '\n\n# PetAgent: o boneco NAO tem tela de escolhas e o `clarify` espera 3600 s por padrao\n# (medido 10/10/2026: o agente travava em clarify.request e a ponte estourava aos 300 s).\n# Com 8 s ele pergunta, ninguem responde e ele SEGUE SOZINHO, respondendo em texto normal.\n# NAO usar 0: no Hermes 0 = espera ILIMITADA.\nclarify:\n  timeout: 8\n'


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


def _fora_do_espelho(nome: str) -> bool:
    """Arquivos da raiz que NÃO entram no espelho por hardlink.

    Bancos SQLite (com o -wal/-shm/-journal deles) e travas/pids de processo. O SQLite
    acha o -wal/-shm pelo CAMINHO do banco: com o mesmo arquivo aberto por dois caminhos
    (hermes\\state.db no gateway do dono, hermes-home\\state.db no backend do boneco), cada
    processo pode acabar com o próprio WAL para o MESMO banco — receita de corrupção do
    histórico do dono. Fora do espelho, o boneco cria os bancos dele; memórias, skills,
    plugins e personalidade continuam os do dono. (Pastas vão por junction: lá dentro o
    caminho real é um só e o -wal cai no mesmo lugar.)
    """
    n = nome.lower()
    return n in ("config.yaml", "config.yml") or n.endswith(
        (".db", ".sqlite", ".sqlite3", "-wal", "-shm", "-journal", ".lock", ".pid"))


def _home_do_backend() -> str:
    """Prepara o home espelhado e devolve o caminho (vai em HERMES_HOME)."""
    real = _home_real()
    try:
        os.makedirs(HOME_ESPELHO, exist_ok=True)
        # 1) espelha o resto (junction para pasta, hardlink para arquivo)
        for nome in os.listdir(real):
            origem = os.path.join(real, nome)
            destino = os.path.join(HOME_ESPELHO, nome)
            if _fora_do_espelho(nome):
                # espelho de versão anterior: o banco/trava estava LIGADO ao do dono. Desfaz
                # o link (some só o nome no espelho; o arquivo do dono fica intacto).
                if (nome.lower() not in ("config.yaml", "config.yml") and os.path.isfile(destino)
                        and os.path.samefile(destino, origem)):
                    try:
                        os.remove(destino)
                        log(f"espelho: {nome} desligado do home do dono")
                    except OSError as e:
                        log(f"AVISO: {nome} segue ligado ao do dono ({e})")
                continue
            if os.path.isdir(origem):
                if not os.path.exists(destino):
                    subprocess.run(["cmd", "/c", "mklink", "/J", destino, origem],
                                   capture_output=True)
                continue
            # arquivo: link REFEITO a cada subida. O Hermes regrava alguns TROCANDO o arquivo
            # (o .env ao mudar a chave, por exemplo) e o hardlink antigo seguia apontando para
            # a versão velha.
            try:
                if os.path.lexists(destino):
                    os.remove(destino)
            except OSError as e:
                log(f"AVISO: não consegui renovar o link de {nome} ({e})")
                continue
            subprocess.run(["cmd", "/c", "mklink", "/H", destino, origem],
                           capture_output=True)
        # 2) config.yaml: cópia do dele sem os MCPs que poluem a tela
        with open(os.path.join(real, "config.yaml"), "r", encoding="utf-8") as fh:
            bruto = fh.read()
        limpo = _tira_mcp(bruto, MCP_A_REMOVER)
        # MENOS ESFORÇO DE RACIOCÍNIO NO BONECO (10/10/2026).
        #
        # Medido: uma pergunta simples levou 224 s e o log mostrou 3.802 eventos de
        # "pensamento" (reasoning.delta) + 5 chamadas de ferramenta (3× terminal, 2×
        # session_search). O `agent.reasoning_effort` estava em `medium` — herdado do dono.
        # O boneco é CHAT: `low` responde muito mais rápido. Valores aceitos: none, minimal,
        # low, medium, high. Isto muda SÓ o home do boneco; o config do dono não é tocado.
        limpo, trocou = _troca_reasoning(limpo)
        log("config do backend: reasoning_effort=%s (era medium)"
            % ("low" if trocou else "ja estava fora do padrao"))
        # O BONECO NÃO TEM TELA DE ESCOLHAS — e o `clarify` espera 1 HORA por padrão.
        #
        # Medido em 10/10/2026 (dono: "pergunta simples levou 205 s" e depois 300 s): o agente
        # chamou a ferramenta `clarify` (que mostra OPÇÕES para o usuário clicar), o chat do
        # boneco não tem como exibir as opções e a ponte não responde à requisição — e
        # `tools/clarify_gateway.py` espera `clarify.timeout`, cujo padrão é 3600 s (1 hora).
        # O agente ficava travado esperando; a ponte estourava aos 300 s e o app caía na reserva
        # lenta, que repetia a mesma pergunta. Log real: "tool.generating name=clarify" →
        # "clarify.request" → 300 s de silêncio.
        #
        # Com 8 s o agente pergunta, ninguém responde e ele SEGUE SOZINHO, respondendo em texto
        # normal — que o balão do boneco mostra. ⚠️ NÃO usar 0: no Hermes 0 significa espera
        # ILIMITADA (o oposto do que parece) e travaria para sempre.
        if not any(l.strip().startswith("clarify:") for l in limpo.splitlines()):
            limpo = limpo.rstrip() + BLOCO_CLARIFY
            log("config do backend: clarify.timeout=8 (o boneco nao tem tela de escolhas)")
        else:
            log("AVISO: o config do dono ja tem bloco clarify: — nao mexi nele")
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
    # Medido 10/10/2026: estes NÃO estavam na lista e cada token virava uma linha no
    # ponte.log — 3.802 linhas num único turno de 224 s (e o disco pagando por isso).
    # "reasoning.delta" é o pensamento token a token; não serve para diagnosticar nada.
    "reasoning.delta", "tool.generating", "message.interim",
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


# ---------------------------------------------------------------------------
# Progresso em tempo real (10/10/2026)
#
# O dono: "eu não fico nunca no vácuo aqui no chat". Medido: no Telegram CADA passo do agente
# vira uma mensagem (11 ferramentas = 11 mensagens recebidas); no boneco eram 9 ferramentas
# virando UMA mensagem no fim — 224 s olhando um balão parado. Aqui a ponte emite o que está
# acontecendo enquanto acontece, e o app repassa para a tela.
# ---------------------------------------------------------------------------
EM_TESTE = False            # --teste espera UM json no stdout: não emitir progresso
_pedido_atual = None        # id do pedido em andamento: vai em cada linha (o app descarta o de outro)
_ultimo_progresso = 0.0
_INTERVALO_PROGRESSO = 0.5  # freio: sem isto, cada token viraria uma linha (3.802 num turno)


def _progresso(tipo: str, texto: str) -> None:
    """Emite uma linha de progresso para o app (não é a resposta). Com freio de tempo."""
    global _ultimo_progresso
    if EM_TESTE:
        return
    agora = time.time()
    # ⚠️ O freio vale SÓ para texto: se o "passo" (ex.: "pensando") resetasse o relógio, o
    # primeiro trecho da resposta caía no freio e não aparecia — medido em 10/10/2026.
    if tipo == "texto":
        if agora - _ultimo_progresso < _INTERVALO_PROGRESSO:
            return
        _ultimo_progresso = agora
    try:
        print(json.dumps({"ok": True, "id": _pedido_atual, "progresso": tipo,
                          "texto": str(texto)[:800]}, ensure_ascii=False), flush=True)
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
        self.modelo = ""
        self.seq = 0

    # ---------- subida ----------
    def subir_processo(self) -> bool:
        if not os.path.exists(HERMES_CMD[0]):
            log(f"hermes não encontrado em {HERMES_CMD[0]}")
            return False
        env = _env_do_backend(self.token)
        cmd = HERMES_CMD + ["serve", "--port", str(self.porta), "--host", "127.0.0.1", "--skip-build"]
        log(f"backend: {' '.join(HERMES_CMD)} serve (PYTHONPATH herdado fora)")
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
        self.modelo = str(info.get('model') or info.get('model_name') or "")
        log(f"sessão {self.sessao} criada · modelo={self.modelo} · cwd={info.get('cwd')}")
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
            # backend que já estava na porta mas não entrega canal (travado/órfão):
            # antes a ponte só registrava "reusando" e seguia com um serve surdo (05/10)
            return await self.reconectar(f"subida inicial: {type(e).__name__}")

    # ---------- reconexão (conserto de 05/10/2026) ----------
    async def _fechar_canal(self) -> None:
        try:
            if self.ws is not None:
                await asyncio.wait_for(self.ws.close(), timeout=5)
        except Exception:  # noqa: BLE001
            pass
        self.ws = None

    async def reconectar(self, motivo: str = "") -> bool:
        """Revive o trilho quente quando o canal morre.

        Antes (incidente 05/10/2026 17:36): o WebSocket caía por `keepalive ping
        timeout`, a ponte ia para a reserva lenta (`hermes -z`, 2+ min por resposta) e
        ficava lá ATÉ ALGUÉM REINICIAR O APP. Agora: fecha o canal morto, derruba o
        backend que parou de responder (um `hermes serve` travado não volta sozinho) e
        sobe/conecta de novo, criando sessão nova.
        """
        log(f"canal caiu ({motivo or 'sem detalhe'}) — reconectando")
        await self._fechar_canal()
        if self.proc is not None and self.proc.poll() is None:
            try:
                self.proc.terminate()
                try:
                    self.proc.wait(timeout=10)
                except Exception:  # noqa: BLE001
                    self.proc.kill()
            except Exception:  # noqa: BLE001
                pass
        # espera a porta soltar; se algum processo alheio/travado a segurar, pega outra
        t0 = time.time()
        while porta_aberta(self.porta) and time.time() - t0 < 20:
            await asyncio.sleep(1)
        if porta_aberta(self.porta):
            self.porta = porta_livre()
            log(f"porta antiga ainda ocupada — passando para {self.porta}")
        if not self.subir_processo():
            log("reconexão: não consegui subir o backend de novo")
            return False
        try:
            if not await self.conectar():
                return False
        except Exception as e:  # noqa: BLE001
            log(f"reconexão falhou no canal: {type(e).__name__}: {e}")
            return False
        log("canal quente reconectado")
        return True

    # ---------- perguntar ----------
    async def perguntar(self, texto: str, timeout: float = 300) -> tuple[str, float]:
        """Pergunta com uma rede de segurança: se o canal estiver morto, reconecta
        UMA vez e repete — em vez de devolver erro e o app cair na reserva lenta."""
        # COMANDOS DE BARRA (pedido do dono, 10/10/2026): o chat do boneco passa a aceitar
        # /new, /compact, /reset e /status — e, por encaminhamento ao `slash.exec`, todos os
        # demais comandos que o Hermes já tem. Sem isto, digitar "/new" ia como TEXTO para o
        # modelo, que respondia sobre a barra em vez de executar o comando.
        if texto.strip().startswith("/"):
            return await self._comando(texto.strip())

        # INSTRUMENTAÇÃO (10/10/2026): o dono mediu 205 s numa pergunta simples e o log só
        # mostrava "tempo esgotado (150s)" DO LADO DO APP — que desistia antes da ponte. Sem
        # registrar o que acontece DURANTE a espera, a causa primeira fica invisível e eu
        # passei a chutar hipóteses (cheguei a medir tamanho de sessão errada). Este vigia
        # apenas ESCREVE NO LOG: não muda comportamento nem prazo.
        vigia = asyncio.create_task(self._vigiaDeDemora())
        t0 = time.time()
        try:
            try:
                resp = await self._perguntar_uma(texto, timeout)
            except (websockets.exceptions.WebSocketException, OSError) as e:
                log(f"pergunta falhou por canal morto ({type(e).__name__}: {e})")
                if not await self.reconectar(type(e).__name__):
                    raise
                resp = await self._perguntar_uma(texto, timeout)
            log(f"pergunta respondida em {time.time() - t0:.1f}s ({len(resp[0])} chars)")
            return resp
        finally:
            vigia.cancel()

    async def _comando(self, texto: str) -> tuple[str, float]:
        """Executa um comando de barra do chat do boneco.

        ⚠️ TETO DURO (10/10/2026): o laço da ponte é SERIAL — um comando pendurado faz o
        PRÓXIMO esperar. Foi o que o dono viu: o `/help` travou no `commands.catalog` (uma
        chamada de rede) e o `/clear` digitado depois ficou ~2 min na fila, atrás dele. Agora
        TODO comando tem teto de 90 s e devolve erro em vez de segurar o chat.
        """
        t0 = time.time()
        nome = texto.split()[0].lower()
        log(f"comando: {nome}")
        try:
            return await asyncio.wait_for(self._comando_impl(texto, nome), timeout=90)
        except asyncio.TimeoutError:
            log(f"comando: {nome} ESTOUROU 90s — interrompido para nao travar a fila")
            return (f"o comando `{nome}` demorou mais de 90 s e eu o interrompi, "
                    "para não travar os próximos.\n(Detalhe no ponte.log.)"), time.time() - t0

    async def _comando_impl(self, texto: str, nome: str) -> tuple[str, float]:
        """O corpo dos comandos (chamado sob o teto de 90 s de `_comando`)."""
        t0 = time.time()

        if nome in ("/new", "/nova", "/reset", "/clear", "/limpar"):
            anterior = self.sessao
            ok = await self._nova_sessao()
            if not ok:
                return "não consegui criar a sessão nova — veja o ponte.log", time.time() - t0
            # Pedido do dono (10/10/2026): `/clear` limpa o CHAT e o contexto; `/new` também
            # pode limpar o chat. A tela limpa sozinha ao ver estes comandos (main.js,
            # `enviarTexto`) — a ponte só avisa o que aconteceu com o contexto.
            if nome in ("/reset", "/clear", "/limpar"):
                return ("chat LIMPO e contexto zerado.\n\n"
                        "• sessão nova: `%s`\n• anterior: `%s`\n\n"
                        "O agente começa sem lembrar desta conversa. Memórias e personalidade "
                        "continuam — são do perfil, não da sessão."
                        % (self.sessao[:8], str(anterior)[:8])), time.time() - t0
            return ("sessão NOVA criada — e o chat foi limpo.\n\n"
                    "• sessão: `%s`\n• anterior: `%s`\n\n"
                    "A conversa anterior sai do contexto; memórias e personalidade continuam."
                    % (self.sessao[:8], str(anterior)[:8])), time.time() - t0

        if nome in ("/help", "/ajuda", "/comandos"):
            return await self._ajuda(), time.time() - t0

        if nome in ("/status", "/usage", "/contexto"):
            return await self._status(), time.time() - t0

        # Qualquer outro comando: o Hermes executa (é o mesmo catálogo do CLI/Telegram).
        try:
            r = await self.rpc("slash.exec", {"session_id": self.sessao, "command": texto},
                               timeout=240)
            saida = _texto_do_rpc(r)
            if not saida:
                saida = (f"o comando `{nome}` rodou, mas não devolveu texto.\n"
                         "(Se era /compact, veja o ponte.log.)")
            return saida, time.time() - t0
        except Exception as e:  # noqa: BLE001
            return (f"não consegui executar `{nome}`: {type(e).__name__}: {e}\n\n"
                    "Comandos tratados pela própria ponte: /new, /reset, /status."
                    ), time.time() - t0

    async def _ajuda(self) -> str:
        """Lista os comandos de barra (pedido do dono, 10/10/2026).

        ⚠️ DEFEITO CORRIGIDO no mesmo dia: a primeira versão chamava `commands.catalog` e
        essa chamada PENDUROU no backend do app (funcionava só com o backend recém-subido).
        Como o laço da ponte é serial, o `/help` travado fez o `/clear` seguinte esperar ~2 min.
        Agora a lista do boneco é ESTÁTICA (sempre instantânea) e o catálogo do Hermes é só um
        extra, com prazo CURTO: se não vier em 8 s, a resposta sai sem ele.
        """
        base = """COMANDOS DO CHAT

Deste chat (sempre funcionam):
  /new      sessão nova — limpa o chat e o contexto
  /clear    limpa o chat e o contexto
  /reset    zera a sessão e o chat
  /status   modelo em uso, sessão, chamadas e tamanho do contexto
  /compact  compacta o contexto (resume o histórico)
  /help     esta lista"""
        try:
            r = await self.rpc("commands.catalog", {}, timeout=8)
            pares = _paresDoCatalogo(r)
        except Exception as e:  # noqa: BLE001
            log(f"catalogo do Hermes nao veio a tempo ({type(e).__name__})")
            pares = []
        if pares:
            base += "\n\nDo Hermes (o boneco executa os mesmos):"
            base += "".join(f"\n  /{n.lstrip('/')}" + (f"  {d}" if d else "")
                            for n, d in pares[:60])
        else:
            base += ("\n\nDo Hermes: o boneco executa os mesmos comandos do CLI/Telegram "
                     "(/new, /clear, /compact, /history, /save, /retry, …). Digitar um comando "
                     "desconhecido mostra a lista do próprio Hermes.")
        return base

    async def _status(self) -> str:
        """O que o dono pediu no /status: TAMANHO DO CONTEXTO e a LLM em uso."""
        linhas = ["STATUS DA SESSÃO", ""]
        linhas.append(f"• modelo: {self.modelo or '(não informado)'}")
        linhas.append(f"• sessão: {self.sessao[:8] or '(sem sessão)'}")
        try:
            u = await self.rpc("session.usage", {"session_id": self.sessao}, timeout=30)
            uso = _dados_do_rpc(u) or {}
            ent = uso.get("input") or 0
            sai = uso.get("output") or 0
            tot = uso.get("total") or (ent + sai)
            chamadas = uso.get("calls") or 0
            linhas.append(f"• chamadas nesta sessão: {chamadas}")
            linhas.append(f"• tokens: entrada {ent:,} · saída {sai:,} · total {tot:,}".replace(",", "."))
            if ent:
                linhas.append("")
                linhas.append(_barraDeContexto(ent))
        except Exception as e:  # noqa: BLE001
            linhas.append(f"• uso: não consegui medir ({type(e).__name__})")
        linhas.append("")
        linhas.append("Comandos: /new (sessão nova) · /reset (zerar) · /compact (compactar) · /status")
        return "\n".join(linhas)

    async def _nova_sessao(self) -> bool:
        """Cria uma sessão nova no backend e passa a usá-la."""
        try:
            r = await self.rpc("session.create", {"cwd": CWD_PADRAO, "cols": 80,
                                                  "source": "petagent"}, timeout=60)
        except Exception as e:  # noqa: BLE001
            log(f"session.create falhou no comando: {type(e).__name__}: {e}")
            return False
        dados = _dados_do_rpc(r) or {}
        sid = str(dados.get("session_id") or dados.get("id") or "")
        if not sid:
            log(f"session.create sem id: {json.dumps(r)[:200]}")
            return False
        self.sessao = sid
        log(f"comando: sessão nova {sid}")
        return True

    async def _vigiaDeDemora(self):
        """Marca no log o canal quente ficando lento. Só observa — não interfere."""
        try:
            await asyncio.sleep(45)
            log("canal quente: 45s sem resposta (fila de turno anterior? canal morto?)")
            await asyncio.sleep(60)
            log("canal quente: 105s sem resposta — se nada voltar, a reserva e' o sintoma")
        except asyncio.CancelledError:
            raise

    async def _perguntar_uma(self, texto: str, timeout: float = 300) -> tuple[str, float]:
        t0 = time.time()
        # AVISO IMEDIATO (10/10/2026): medido — o modelo passa o tempo PENSANDO e o texto sai
        # em rajada no fim, então só transmitir o texto deixaria o balão parado de novo. Este
        # primeiro aviso garante que a tela mostra movimento desde o primeiro instante.
        _progresso("passo", "pensando")
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
                    _progresso("texto", "".join(pedacos))
            elif tipo == "message.interim":
                carga = p.get("payload") if isinstance(p.get("payload"), dict) else p
                t = carga.get("text")
                if isinstance(t, str) and t.strip():
                    _progresso("texto", t)
            elif tipo in ("tool.generating", "tool.start"):
                carga = p.get("payload") if isinstance(p.get("payload"), dict) else p
                nome = str(carga.get("name") or carga.get("tool") or "ferramenta")
                _progresso("passo", nome)
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
    global EM_TESTE
    ap = argparse.ArgumentParser()
    ap.add_argument("--teste", default="")
    ap.add_argument("--porta", type=int, default=0)
    args = ap.parse_args()
    EM_TESTE = bool(args.teste)   # --teste: stdout tem de ser UM json só (sem progresso)

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
    global _pedido_atual
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
        # o id volta em toda linha desta pergunta: se o app já desistiu dela (prazo), a
        # resposta atrasada não pode ser lida como resposta da pergunta seguinte
        _pedido_atual = pedido.get("id")
        try:
            resp, seg = await bk.perguntar(p)
            print(json.dumps({"ok": True, "id": _pedido_atual, "resposta": resp,
                              "segundos": round(seg, 1)}, ensure_ascii=False), flush=True)
        except Exception as e:  # noqa: BLE001
            print(json.dumps({"ok": False, "id": _pedido_atual,
                              "erro": f"{type(e).__name__}: {e}"}), flush=True)
    # o app fechou: leva o backend junto (sem órfãos)
    if bk.proc and bk.proc.poll() is None:
        try:
            bk.proc.terminate()
        except Exception:  # noqa: BLE001
            pass
    return 0


# ---------------------------------------------------------------------------
# Helpers dos comandos de barra (10/10/2026)
# ---------------------------------------------------------------------------

def _dados_do_rpc(r) -> dict:
    """Extrai o dicionario util de uma resposta JSON-RPC do gateway.

    O gateway responde em {result: {...}} ou ja no formato plano, e alguns metodos
    aninham em {result: {data: {...}}}. Tentar as tres formas evita "nao consegui medir".
    """
    if not isinstance(r, dict):
        return {}
    for caminho in (("result", "data"), ("result",), ("data",), ()):
        d = r
        for chave in caminho:
            d = d.get(chave) if isinstance(d, dict) else None
        if isinstance(d, dict) and d:
            return d
    return {}


def _texto_do_rpc(r) -> str:
    """Extrai TEXTO de uma resposta de `slash.exec` (o formato varia por comando)."""
    d = _dados_do_rpc(r)
    if not d:
        return ""
    for chave in ("output", "text", "message", "result_text", "lines", "content"):
        v = d.get(chave)
        if isinstance(v, str) and v.strip():
            return v.strip()
        if isinstance(v, list):
            juntas = "\n".join(str(x) for x in v if x is not None)
            if juntas.strip():
                return juntas.strip()
    # ultimo recurso: o proprio dicionario serializado, para o dono ver algo util
    return ""


def _troca_reasoning(texto: str) -> tuple:
    """Troca `agent.reasoning_effort` para `low` no config do BONECO (10/10/2026).

    Medido: com `medium` (herdado do dono), uma pergunta simples levou 224 s — o log mostrou
    3.802 eventos de pensamento. O boneco é CHAT: `low` responde muito mais rápido.

    ⚠️ Troca APENAS a linha dentro do bloco `agent:` — o config do dono também tem
    `reasoning_effort` sob `agent:` e um replace global atingiria a seção errada.
    Devolve (texto, trocou).
    """
    linhas = texto.split("\n")
    dentro = False
    for i, l in enumerate(linhas):
        if l.startswith("agent:"):
            dentro = True
            continue
        if dentro and l and not l[0].isspace() and not l.startswith("#"):
            dentro = False                      # saiu do bloco agent:
        if dentro and l.strip().startswith("reasoning_effort:"):
            if "medium" in l:
                linhas[i] = l.replace("medium", "low")
                return "\n".join(linhas), True
            return "\n".join(linhas), False     # já está em outro valor: não mexo
    return texto, False


def _paresDoCatalogo(r) -> list:
    """Extrai pares (comando, descrição) do `commands.catalog` do Hermes.

    O formato do gateway varia (categorias aninhadas, pares, dicts). Em vez de adivinhar a
    forma exata, varre a resposta inteira atrás de listas de dois textos — é o que o catálogo
    devolve para cada comando — e mantém a ordem em que aparecem.
    """
    achados: list = []
    vistos: set = set()

    def _anda(no):
        if len(achados) >= 80:
            return
        if isinstance(no, dict):
            for v in no.values():
                _anda(v)
        elif isinstance(no, list):
            if (len(no) == 2 and isinstance(no[0], str) and isinstance(no[1], str)
                    and no[0].strip() and no[0].strip() not in vistos):
                vistos.add(no[0].strip())
                achados.append((no[0].strip(), no[1].strip()))
            else:
                for v in no:
                    _anda(v)

    _anda(_dados_do_rpc(r))
    return achados


def _barraDeContexto(tokens: int) -> str:
    """Barra visual do tamanho do contexto — o dono pediu 'tamanho do contexto' no /status.

    A janela de referencia e a do modelo em uso; sem saber o limite exato, 128k e o piso
    com que o Hermes trabalha para os modelos atuais. O numero exato vem sempre acima.
    """
    limite = 128_000
    pct = min(100.0, tokens * 100.0 / limite)
    cheios = int(pct / 5)
    barra = "#" * cheios + "." * (20 - cheios)
    return f"[{barra}] ~{pct:.0f}% de {limite // 1000}k de contexto"


if __name__ == "__main__":
    try:
        raise SystemExit(principal())
    except KeyboardInterrupt:
        raise SystemExit(0)
