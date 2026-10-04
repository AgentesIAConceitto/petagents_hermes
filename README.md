# PetAgent 🐾

> Um **boneco vivo no seu desktop** que é a segunda porta de entrada para o
> [Hermes](https://hermes-agent.nousresearch.com): conversa com o agente de verdade
> (**com ferramentas**), cuida de você (água, comida, caminhada, foco) e **não
> bloqueia nada** — fora do boneco, o clique atravessa a janela.

**[Português](#-português) · [English](#-english)**

---

## 🇧🇷 Português

### O que é

O PetAgent nasceu de uma tese simples: **se o boneco não tiver os mesmos poderes e a
mesma velocidade do Hermes, ninguém usa.** Ele não é um enfeite que responde "oi" —
é uma segunda interface para o *seu* agente, com a sua memória, a sua personalidade,
as suas skills e as suas ferramentas. Quem não quer abrir o Telegram nem o navegador
conversa com o agente clicando num bicho na borda da tela.

E ele é **vivo**: tem fome, sede, sono, humor, vontade de brincar — e uma agenda
ligada à biologia humana, porque um bicho de estimação que só pede é chato, e um
bicho que *também* cuida de você é útil.

### O que ele faz hoje

**Conversa de verdade**
- Chat no balãozinho (cresce para cima, até ~30% da tela) e **Chat Estendido** (janela
  arrastável, redimensionável, com botão de copiar por mensagem).
- O agente responde **com as ferramentas dele** — roda comando, lê arquivo, olha o
  sistema. A ponte sobe um `hermes serve` **próprio** (mesmo perfil do dono, mesma
  memória), então o boneco conversa com o mesmo agente que você usa no resto do dia.
- Mas só o que é conversa abre o chat: **recado do bicho é balão**, nunca sequestra a
  conversa. Um clique no boneco é carinho; dois cliques abrem o chat.

**Agenda biológica (dentro do seu horário de trabalho)**
- 💧 água de 2 em 2 h · 🚶 levantar e caminhar a cada 1h30 · ☕ refeições e 🥪 lanchinhos
  nos horários · 🥰 carinho de 3 em 3 h.
- As refeições são **derivadas do seu expediente** (começo, ¼, metade, ¾ e 45 min antes
  do fim), não de horas fixas: quem trabalha 7h–17h não recebe aviso de jantar às 19h30.
- **Nada fora do horário informado.** O PC pode ficar ligado 24/7: o bicho respeita a
  janela.
- Atrasado mais de 45 min, o aviso é descartado (ligar o PC à tarde não despeja a
  manhã inteira de uma vez).

**As barras (hidratação, alimentação, atenção, movimento)**
- O aviso **baixa** a barra da necessidade da vez — e atender (alimentar, dar água,
  carinho, exercício) enche de volta.
- Se você ignorar, ele se cuida sozinho depois de 45 min (+6/h, teto 55): **o bicho
  nunca morre e nunca fica travado sofrendo.**

**Animações, som e companhia**
- Sprite sheets do **PetDex** (galeria com milhares de bonecos, busca e paginação),
  com animação **por sentido** de caminhada, comemoração, carinho, sono e fala.
- **Sininho suave** a cada recado (dois toques de seno sintetizados, sem arquivo de
  áudio) — para você saber que chegou mensagem mesmo sem olhar a tela.
- Passeios sozinho pela linha da barra, volta completa contornando a tela, dormir de
  1 a 4 h, modo foco 🎯 (só água), "esconder por 15 min/1 h".
- Encaixa na borda mais próxima quando você solta, e a janela é **click-through**: fora
  do boneco, seus cliques vão para o que está atrás.
- Bandeja do sistema, atalho `Ctrl+Alt+Q`, menu com o botão DESLIGAR.

**Presentes e coleção**
- O bicho acha baús e ganha presentes: **~2.500 itens** com raridade de 60% (comum) a
  **0,031%** (ultra). Repetido vira moeda; exercício rende moeda.
- Ele **não veste** os presentes (decisão do dono): guarda, comemora e mostra na
  galeria 💎.

**E-mail (opcional)**
- Lê a caixa do Outlook por COM **não-elevado**, avisa só o que é importante e
  **aprende** com você (marca como importante/lixo pelo painel).
- Desenhado para nunca atrapalhar o Outlook do dono: se o Outlook estiver fechado, ele
  **adia** a leitura em vez de abrir uma instância invisível.

### Arquitetura

```
       ┌──────────────────────── PetAgent (Go + Wails v2) ────────────────────────┐
       │  janela transparente (WebView2)   ·  bandeja  ·  hit-test click-through  │
       │  frontend: index.html + main.js + bicho.js + premios.js + style.css      │
       └───────────────┬───────────────────────────────┬──────────────────────────┘
                       │ stdin/stdout (JSON por linha) │ HTTP local
                       ▼                               ▼
        ┌──────────────────────────┐        ┌──────────────────────────┐
        │  chat_bridge.py          │        │  servidor de sprites     │
        │  (ponte do chat)         │        │  (127.0.0.1, só local)   │
        └────────────┬─────────────┘        └──────────────────────────┘
                     │ WebSocket /api/ws
                     ▼
        ┌──────────────────────────────────────────────────────────────┐
        │  hermes serve  (processo PRÓPRIO, mesmo perfil do dono)      │
        │  = o agente de verdade, com as ferramentas dele              │
        └──────────────────────────────────────────────────────────────┘
```

- A ponte protege o Hermes do dono: sobe o backend dela com um **home espelhado**
  (mesmas skills/memória/plugins, `config.yaml` próprio) para não carregar MCPs que
  criam overlays de tela.
- Todo o estado do bicho (fome, sede, agenda, presentes) vive no `localStorage` do
  WebView2 — nada de banco de dados para o bicho.

### Requisitos

| Para | O que precisa |
|---|---|
| Rodar | Windows 10/11 (x64), WebView2 (já vem no Windows 11 e no Edge), **Hermes** instalado |
| Compilar | Go 1.22+, [Wails v2](https://wails.io), Node só para checagem de sintaxe |
| E-mail (opcional) | Outlook desktop + os scripts em `mail/` rodando **não-elevados** |

### Instalação

```bash
# via plugin do Hermes (recomendado)
hermes plugins install AgentesIAConceitto/petagents_hermes --enable
hermes petagent instalar        # baixa/configura e sobe o overlay
hermes petagent iniciar
```

Comandos do dia a dia:

```bash
hermes petagent status
hermes petagent parar
hermes petagent instalar --origem "caminho/para/petagent.exe"
```

E-mail (opcional): copie `mail/poll.ps1` e `mail/poll_silencioso.vbs` para
`C:\petagent_mail\`, ajuste a conta no topo do `poll.ps1` e o app cuida do resto.
**O leitor de e-mail roda não-elevado de propósito** — o COM do Outlook não enxerga a
instância do usuário quando o processo é elevado.

### Compilar do código

```bash
git clone https://github.com/AgentesIAConceitto/petagents_hermes
cd petagents_hermes
go build ./... && go vet ./...      # tem de passar limpo
wails build -s                      # gera build/bin/petagent.exe
```

### Configuração (painel 🐾)

Nome do bicho, horário de trabalho (início/fim), **água a cada (h)**, **caminhar a
cada (min)**, modo foco, dormir (1–4 h), perfil de animação, escolha de boneco na
galeria, gráfico do bicho e a coleção 💎. Tudo pelo painel — sem editar arquivo.

### Decisões de projeto (o porquê das regras)

- **Clique = carinho, duplo clique = chat.** Clicar no bicho não significa "quero
  conversar": é um agrado.
- **Recado é balão; chat é chat.** Alimentar, dar água, brincar e exercitar são
  **silenciosos** (sem mensagem): o retorno é o efeito no boneco + o som. Só o tile
  "Conversar", o duplo clique e as respostas do agente escrevem no histórico.
- **O bicho nunca morre** e nunca fica travado numa necessidade baixa.
- **Nada fora do expediente** — e nada de hora fixa no código: tudo derivado do
  horário informado.
- **Nunca mexer no Hermes do dono**: as correções vivem no PetAgent.

### Limitações conhecidas (honestidade primeiro)

- **Windows apenas** (o overlay usa WebView2 e APIs Win32).
- A ponte do chat usa **ACP**: perguntas que precisam de ferramenta são lentas e podem
  esbarrar no timeout de keepalive. A migração para `/api/ws` direto está no roadmap.
- O áudio do WebView2 só é liberado depois do primeiro clique/tecla na página: o
  primeiro recado após subir o app pode sair mudo.
- O leitor de e-mail depende do Outlook desktop e de um caminho fixo
  (`C:\petagent_mail`) — parametrizar está no roadmap.
- O sprite vem do PetDex em tempo de execução; a arte é dos autores originais.

### Licença

MIT — veja [LICENSE](LICENSE).

---

## 🇺🇸 English

### What it is

PetAgent started from a simple thesis: **if the pet doesn't have the same powers and
the same speed as Hermes, nobody uses it.** It is not a decoration that replies "hi" —
it is a second interface to *your* agent, with your memory, your personality, your
skills and your tools. If you don't want to open Telegram or a browser, you talk to
your agent by clicking a creature at the edge of your screen.

And it is **alive**: hungry, thirsty, sleepy, moody, playful — with an agenda tied to
human biology, because a pet that only asks for things is annoying, and a pet that
*also* takes care of you is useful.

### What it does today

**Real conversation**
- Chat in the bubble (grows upward, up to ~30% of the screen) and an **Extended Chat**
  window (draggable, resizable, with a copy button per message).
- The agent answers **with its tools** — runs commands, reads files, inspects the
  system. The bridge spins up its **own** `hermes serve` (same profile as the owner,
  same memory), so the pet talks to the same agent you use everywhere else.
- Only real conversation opens the chat: **pet chatter is a bubble**, it never hijacks
  the conversation. One click pets it; two clicks open the chat.

**Biological agenda (inside your working hours)**
- 💧 water every 2 h · 🚶 stand up and walk every 90 min · ☕ meals and 🥪 snacks at the
  right times · 🥰 affection every 3 h.
- Meals are **derived from your shift** (start, ¼, half, ¾ and 45 min before the end),
  never hard-coded: someone working 7am–5pm never gets a "dinner" reminder at 7:30pm.
- **Nothing outside the configured window.** The PC can stay on 24/7: the pet respects
  the schedule.
- Anything more than 45 min late is dropped (booting at 2pm does not dump the whole
  morning at once).

**The bars (hydration, food, affection, movement)**
- A reminder **lowers** the corresponding bar — and attending to it (feed, water,
  pet, exercise) fills it back up.
- If you ignore it, the pet takes care of itself after 45 min (+6/h, capped at 55):
  **it never dies and never gets stuck suffering.**

**Animations, sound and company**
- **PetDex** sprite sheets (a gallery with thousands of characters, search and
  pagination), with per-direction walking animation, celebration, affection, sleep and
  talking.
- A **soft chime** on every message (two synthesized sine notes, no audio file) so you
  know something arrived even if you are not looking at the screen.
- Wanders along the taskbar line, full lap around the screen, sleep 1–4 h, focus mode
  🎯 (water only), "hide for 15 min/1 h".
- Snaps to the nearest edge when you drop it, and the window is **click-through**:
  outside the pet, your clicks go to whatever is behind.

**Gifts and collection**
- The pet finds chests and receives gifts: **~2,500 items** with rarities from 60%
  (common) down to **0.031%** (ultra). Duplicates turn into coins; exercise earns coins.
- It does **not** wear the gifts: it keeps them, celebrates and shows them in the
  gallery 💎.

**E-mail (optional)**
- Reads the Outlook mailbox over COM **non-elevated**, notifies only about what matters
  and **learns** from you (mark things important/junk in the panel).
- Built to never disturb the owner's Outlook: if Outlook is closed, it **defers** the
  read instead of launching an invisible instance.

### Architecture

```
       ┌──────────────────────── PetAgent (Go + Wails v2) ────────────────────────┐
       │  transparent window (WebView2)  ·  tray  ·  click-through hit-testing    │
       │  frontend: index.html + main.js + bicho.js + premios.js + style.css      │
       └───────────────┬───────────────────────────────┬──────────────────────────┘
                       │ stdin/stdout (one JSON/line)  │ local HTTP
                       ▼                               ▼
        ┌──────────────────────────┐        ┌──────────────────────────┐
        │  chat_bridge.py          │        │  sprite server           │
        │  (the chat bridge)       │        │  (127.0.0.1, local only) │
        └────────────┬─────────────┘        └──────────────────────────┘
                     │ WebSocket /api/ws
                     ▼
        ┌──────────────────────────────────────────────────────────────┐
        │  hermes serve  (its OWN process, same profile as the owner)  │
        │  = the real agent, with all of its tools                     │
        └──────────────────────────────────────────────────────────────┘
```

- The bridge protects the owner's Hermes: it boots its backend with a **mirrored home**
  (same skills/memory/plugins, its own `config.yaml`) so it does not load MCP servers
  that create screen overlays.
- All pet state (hunger, thirst, agenda, gifts) lives in the WebView2 `localStorage` —
  no database for the pet.

### Requirements

| For | You need |
|---|---|
| Running | Windows 10/11 (x64), WebView2 (bundled with Windows 11 and Edge), **Hermes** installed |
| Building | Go 1.22+, [Wails v2](https://wails.io), Node only for syntax checks |
| E-mail (optional) | Outlook desktop + the scripts in `mail/` running **non-elevated** |

### Install

```bash
# via the Hermes plugin system (recommended)
hermes plugins install AgentesIAConceitto/petagents_hermes --enable
hermes petagent instalar        # downloads/configure and brings up the overlay
hermes petagent iniciar
```

Everyday commands:

```bash
hermes petagent status
hermes petagent parar
hermes petagent instalar --origem "path/to/petagent.exe"
```

E-mail (optional): copy `mail/poll.ps1` and `mail/poll_silencioso.vbs` to
`C:\petagent_mail\`, set your account at the top of `poll.ps1`, and the app does the
rest. **The mail reader runs non-elevated on purpose** — Outlook's COM cannot see the
user's instance when the caller is elevated.

### Build from source

```bash
git clone https://github.com/AgentesIAConceitto/petagents_hermes
cd petagents_hermes
go build ./... && go vet ./...      # must pass clean
wails build -s                      # produces build/bin/petagent.exe
```

### Configuration (panel 🐾)

Pet name, working hours (start/end), **water every (h)**, **walk every (min)**, focus
mode, sleep (1–4 h), animation profile, character gallery, pet stats graph and the 💎
collection. Everything from the panel — no file editing.

### Design decisions (why the rules exist)

- **One click = affection, double click = chat.** Clicking the pet does not mean "I
  want to talk": it is a pat.
- **Chatter is a bubble; chat is chat.** Feeding, watering, playing and exercising are
  **silent** (no message): the feedback is the animation + the sound. Only the "Chat"
  tile, a double click and the agent's replies write to the history.
- **The pet never dies** and never gets stuck with a low need.
- **Nothing outside working hours** — and no hard-coded clock times: everything is
  derived from the configured shift.
- **Never touch the owner's Hermes**: fixes live inside PetAgent.

### Known limitations (honesty first)

- **Windows only** (the overlay uses WebView2 and Win32 APIs).
- The chat bridge uses **ACP**: prompts that need tools are slow and can hit the
  keepalive timeout. Migrating to `/api/ws` directly is on the roadmap.
- WebView2 only unlocks audio after the first click/keypress on the page: the very
  first message after startup may be silent.
- The e-mail reader depends on Outlook desktop and a fixed path
  (`C:\petagent_mail`) — making it configurable is on the roadmap.
- Sprites come from PetDex at runtime; the artwork belongs to its original authors.

### License

MIT — see [LICENSE](LICENSE).
