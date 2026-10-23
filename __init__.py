"""PetAgent — plugin do Hermes.

O que este plugin faz (Windows):

    hermes petagent instalar [--origem CAMINHO] [--boneco SLUG]
    hermes petagent iniciar | parar | status
    /petagent instalar | iniciar | parar | status      (o mesmo, no chat)

De onde ele pega o app, em ordem:
    1) --origem CAMINHO   → o .exe (ou a pasta dele) que você já tem: usado em dev
    2) Release do GitHub  → REPO_GITHUB abaixo (quando o repositório for publicado)
    3) %LOCALAPPDATA%\\petagent\\petagent.exe já instalado

Regras do projeto que este código respeita:
  * NUNCA abre janela de console (tudo com CREATE_NO_WINDOW) — o usuário trabalha
    enquanto o Hermes mexe nos bastidores;
  * não sobe o boneco sozinho no `instalar` — quem decide mostrar coisa na tela é
    o usuário (`iniciar`);
  * todo estado fica em arquivo texto simples, fácil de inspecionar e apagar.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import urllib.request
from datetime import datetime
from pathlib import Path

VERSAO = "0.1.0"
EXE = "petagent.exe"

# Repositório: "AgentesIAConceitto/petagents_hermes".
# Enquanto estiver vazio, `instalar` só funciona com --origem.
REPO_GITHUB = ""

NO_WINDOW = 0x08000000  # CREATE_NO_WINDOW
DETACHED = 0x00000008   # DETACHED_PROCESS


# ── caminhos ──────────────────────────────────────────────────────────

def _pasta() -> Path:
    """Pasta de instalação/config do PetAgent (%LOCALAPPDATA%\\petagent)."""
    base = os.environ.get("LOCALAPPDATA") or str(Path.home() / "AppData" / "Local")
    p = Path(base) / "petagent"
    p.mkdir(parents=True, exist_ok=True)
    return p


def _log(msg: str) -> None:
    try:
        with (_pasta() / "plugin.log").open("a", encoding="utf-8") as f:
            f.write(f"{datetime.now():%d/%m/%Y %H:%M:%S} {msg}\n")
    except OSError:
        pass


# ── estado ────────────────────────────────────────────────────────────

def _rodando() -> bool:
    # Sem text=True de propósito: o tasklist em Windows PT-BR sai em cp850 e
    # estoura UnicodeDecodeError. Comparamos bytes, sem decodificar.
    try:
        out = subprocess.run(
            ["tasklist", "/FI", f"IMAGENAME eq {EXE}"],
            capture_output=True, creationflags=NO_WINDOW, timeout=20,
        ).stdout or b""
        return EXE.lower().encode() in out.lower()
    except Exception:
        return False


def _boneco_do_hermes() -> str:
    """Lê display.pet.slug do config.yaml do Hermes (o pet que ele já usa)."""
    base = os.environ.get("LOCALAPPDATA")
    if not base:
        return ""
    cfg = Path(base) / "hermes" / "config.yaml"
    if not cfg.exists():
        return ""
    em_display = em_pet = False
    try:
        for linha in cfg.read_text(encoding="utf-8", errors="ignore").splitlines():
            t = linha.strip()
            recuo = len(linha) - len(linha.lstrip(" \t"))
            if t.startswith("display:"):
                em_display, em_pet = True, recuo == 0
            elif em_display and t.startswith("pet:"):
                em_pet = True
            elif em_display and em_pet and t.startswith("slug:"):
                v = t.split(":", 1)[1].strip().strip("\"'")
                if v and v != "null":
                    return v
            elif recuo == 0 and t and not t.startswith("display:"):
                em_display = em_pet = False
    except OSError:
        pass
    return ""


# ── passos ────────────────────────────────────────────────────────────

def _pega_exe(origem):
    destino = _pasta() / EXE
    if origem:
        src = Path(origem)
        if src.is_dir():
            src = src / EXE
        if not src.exists():
            return False, f"Executável não encontrado em {src}."
        shutil.copy2(src, destino)
        # a ponte do chat (agente quente via hermes acp) vai junto: sem ela o chat
        # do boneco cai na reserva lenta (~30 s por mensagem)
        ponte = src.parent / "chat_bridge.py"
        if ponte.exists():
            shutil.copy2(ponte, _pasta() / "chat_bridge.py")
        return True, f"Aplicativo copiado de {src}."
    if destino.exists():
        return True, f"O aplicativo já estava instalado ({destino})."
    if REPO_GITHUB:
        try:
            api = f"https://api.github.com/repos/{REPO_GITHUB}/releases/latest"
            with urllib.request.urlopen(api, timeout=40) as r:
                release = json.load(r)
            url = ""
            for a in release.get("assets", []):
                if a.get("name", "").lower().endswith(".exe"):
                    url = a.get("browser_download_url", "")
                    break
            if not url:
                return False, "A versão mais recente publicada no GitHub não tem um arquivo .exe anexado."
            with urllib.request.urlopen(url, timeout=180) as r, destino.open("wb") as f:
                shutil.copyfileobj(r, f)
            return True, f"Aplicativo baixado do GitHub ({release.get('tag_name', '?')})."
        except Exception as e:  # rede, 404, rate-limit...
            return False, f"Não foi possível baixar do GitHub: {e}"
    return False, ("O aplicativo ainda não foi publicado (REPO_GITHUB vazio). "
                   "Use --origem CAMINHO_PARA_petagent.exe (build local).")


def instalar(args) -> int:
    ok, msg = _pega_exe(getattr(args, "origem", None))
    print(("[ok] " if ok else "[x] ") + msg)
    if not ok:
        return 1
    # NÃO sobrescreve a escolha do usuário: o painel do app grava o boneco em
    # petdex.txt e o `instalar` apagava essa escolha (bug real, 02/10/2026).
    # Só define quando vier --boneco explícito ou quando não há escolha ainda.
    destino = _pasta() / "petdex.txt"
    boneco = (getattr(args, "boneco", None) or "").strip()
    if not boneco:
        try:
            boneco = destino.read_text(encoding="utf-8").strip()
        except OSError:
            boneco = ""
    if not boneco:
        boneco = _boneco_do_hermes() or "paperclip"
    destino.write_text(boneco, encoding="utf-8")
    (_pasta() / "modo.txt").write_text("cheio", encoding="utf-8")
    meta = {
        "versao": VERSAO,
        "instalado_em": datetime.now().isoformat(timespec="seconds"),
        "boneco": boneco,
        "exe": str(_pasta() / EXE),
    }
    (_pasta() / "plugin.json").write_text(
        json.dumps(meta, ensure_ascii=False, indent=2), encoding="utf-8")
    _log(f"instalar ok: boneco={boneco}")
    print(f"[ok] Boneco definido: {boneco}   (para trocar, use --boneco <slug>)")
    print(f"[ok] Configuração salva em: {_pasta()}")
    print("  Para colocar o boneco na tela:  hermes petagent iniciar")
    print("  Para tirar o boneco da tela:    hermes petagent parar")
    return 0


def iniciar(args=None) -> int:
    exe = _pasta() / EXE
    if not exe.exists():
        print("✗ O aplicativo não está instalado. Rode antes: hermes petagent instalar")
        return 1
    if _rodando():
        print("• O boneco já está na tela.")
        return 0
    # explorer.exe: sobe o app no contexto NÃO-elevado do usuário (janela elevada
    # não recebe arquivo arrastado do Explorer, e o anexo do chat depende disso)
    # e desanexa limpo.
    subprocess.Popen(["explorer.exe", str(exe)], creationflags=DETACHED)
    _log("iniciar")
    print("[ok] Abrindo o boneco na tela...")
    print("  Para encerrar: Ctrl+Alt+Q, opção DESLIGAR do menu ou hermes petagent parar")
    return 0


def parar(args=None) -> int:
    if not _rodando():
        print("• O boneco não está na tela.")
        return 0
    subprocess.run(["taskkill", "/IM", EXE, "/F"],
                   capture_output=True, creationflags=NO_WINDOW, timeout=30)
    _log("parar")
    print("[ok] O boneco saiu da tela.")
    return 0


def status(args=None) -> int:
    exe = _pasta() / EXE
    pet = ""
    try:
        pet = (_pasta() / "petdex.txt").read_text(encoding="utf-8").strip()
    except OSError:
        pass
    print(f"Pasta:          {_pasta()}")
    print(f"Aplicativo:     {'instalado' if exe.exists() else 'NÃO instalado'}")
    print(f"Na tela:        {'sim' if _rodando() else 'não'}")
    print(f"Boneco:         {pet or '(nenhum definido)'}")
    print(f"Pet do Hermes:  {_boneco_do_hermes() or '(não configurado)'}")
    return 0


# ── integração com o Hermes ───────────────────────────────────────────

def configura_subcomandos(sub) -> None:
    """Monta `hermes petagent <ação>`."""
    acoes = sub.add_subparsers(dest="acao")
    p = acoes.add_parser("instalar", help="baixa ou copia o aplicativo, define o boneco e salva a configuração")
    p.add_argument("--origem", help="caminho do petagent.exe (ou da pasta dele), para usar um build local")
    p.add_argument("--boneco", help="slug do boneco no Petdex (ex.: paperclip)")
    p.set_defaults(func=instalar)
    acoes.add_parser("iniciar", help="coloca o boneco na tela").set_defaults(func=iniciar)
    acoes.add_parser("parar", help="tira o boneco da tela").set_defaults(func=parar)
    acoes.add_parser("status", help="mostra se o aplicativo está instalado e se o boneco está na tela").set_defaults(func=status)


def comando_chat(raw_args: str):
    """`/petagent ...` no chat: responde em texto."""
    acao = (raw_args or "").strip().split(" ")[0] or "status"
    funcoes = {"instalar": instalar, "iniciar": iniciar, "parar": parar, "status": status}
    fn = funcoes.get(acao)
    if not fn:
        return "Uso: /petagent instalar | iniciar | parar | status"
    args = type("A", (), {"origem": None, "boneco": None})()
    if fn is instalar:
        return "A instalação não pode ser feita pelo chat, pois precisa do caminho do arquivo. No terminal, use:\n" \
               "  hermes petagent instalar --origem CAMINHO\\petagent.exe"
    fn(args)
    return {"iniciar": "Abrindo o boneco na tela.", "parar": "O boneco saiu da tela.",
            "status": "O status completo foi exibido no terminal."}[acao]


def ao_usar_ferramenta(**kw) -> None:
    _log("evento post_tool_call: " + str(kw.get("tool_name") or kw.get("name") or "?"))


def ao_encerrar_sessao(**kw) -> None:
    _log("evento on_session_end")


def register(ctx) -> None:
    ctx.register_cli_command(
        name="petagent",
        help="PetAgent: boneco animado na área de trabalho (instalar, iniciar, parar, status)",
        setup_fn=configura_subcomandos,
        handler_fn=lambda a: (a.func(a) if getattr(a, "func", None) else 0),
        description="Instala, configura e controla o boneco do PetAgent na tela.",
    )
    ctx.register_command(
        name="petagent",
        handler=comando_chat,
        description="Controla o boneco do PetAgent na tela",
        args_hint="iniciar|parar|status",
    )
    ctx.register_hook("post_tool_call", ao_usar_ferramenta)
    ctx.register_hook("on_session_end", ao_encerrar_sessao)
    _log(f"plugin carregado (v{VERSAO})")
