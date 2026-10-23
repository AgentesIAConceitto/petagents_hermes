/* PetAgent — conversas do chat (22/10/2026). Pedido da dona: "criar conversas novas sobre
   assuntos diferentes e a anterior fica salva — como no Claude —, e poder renomear, pôr
   etiqueta e categoria". Telas aprovadas na prancha "PetAgent — Conversas e Anexos".

   No chat estendido, à esquerda: + Nova conversa, busca, filtro por pasta e por etiqueta e a
   lista agrupada por pasta. No cabeçalho: o nome da conversa (✎ renomeia) e o ⋯ (nome,
   pasta, etiquetas, excluir). No balãozinho: o nome da conversa (clicar abre o chat grande)
   e o + de nova conversa.

   O histórico mora no Hermes; quem fala com ele é o Go (conversas.go → a ponte). Aqui fica
   a tela. `conversa` (main.js) continua sendo a lista VISÍVEL: trocar de conversa a
   substitui pelas mensagens que o Hermes devolve. */

const conv = {
  atual: "",          // id guardado da conversa aberta (o Hermes conhece por ele)
  lista: [],          // [{id, titulo, previa, inicio, mensagens, pasta, etiquetas}]
  pastas: [],
  pronta: false,      // a ponte respondeu a lista (antes disso o agente está ligando)
  busca: "", pasta: "*", etiqueta: "",
  fechadas: {},       // grupos recolhidos na lista
  menuDe: "",         // conversa cujo menu ⋯ está aberto (a linha dela fica destacada)
  novaPasta: false,   // menu mostrando o campo de pasta nova
};
try { conv.fechadas = JSON.parse(localStorage.getItem("conversas.fechadas") || "{}"); } catch (e) { }
try { conv.atual = localStorage.getItem("conversa.id") || ""; } catch (e) { }

const ladoEl = document.getElementById("cg-lado");
const menuConvEl = document.getElementById("conv-menu");

/* ───────── o que o main.js pergunta ───────── */

function conversaAberta() { return conv.atual; }

function dadosDaAtual() { return conv.lista.find((c) => c.id === conv.atual); }

function tituloDe(c) {
  if (!c) return "";
  if (c.titulo) return c.titulo;
  if (c.previa) return c.previa.length > 42 ? c.previa.slice(0, 40).trim() + "…" : c.previa;
  return "Nova conversa";
}

// título do cabeçalho: vazio enquanto a lista não chegou (o main.js mostra o nome do pet).
// Conversa nova SEM mensagem ainda não está na lista (o Hermes só grava a conversa na 1ª
// resposta): é "Nova conversa", não o nome do pet (visto pela dona em 23/10/2026).
function tituloConversa() {
  const c = dadosDaAtual();
  if (c) return tituloDe(c);
  return conv.pronta && conv.atual ? "Nova conversa" : "";
}

// "Clientes · #orçamento #urgente" — a linha de baixo do cabeçalho do chat grande
function rotuloOrganizacao() {
  const c = dadosDaAtual();
  if (!c) return "";
  return [c.pasta || "", (c.etiquetas || []).map((e) => "#" + e).join(" ")].filter(Boolean).join(" · ");
}

function guardaAtual(id) {
  conv.atual = id || "";
  try { localStorage.setItem("conversa.id", conv.atual); } catch (e) { }
}

function salvaVisivel() {
  try { localStorage.setItem("conversa", JSON.stringify(conversa.slice(-60))); } catch (e) { }
}

const espereResposta = () =>
  toast("relogio", "Espere a resposta", nomeDoPet() + " ainda está respondendo — depois dá para trocar de conversa.", 5000, true);

/* ───────── falar com o Go ───────── */

async function chamaConv(fn, ...args) {
  if (!G || !G[fn]) return { ok: false, erro: "fora do app" };
  try { return JSON.parse(await G[fn](...args)); } catch (e) { return { ok: false, erro: String(e) }; }
}

function avisaErroConv(r, titulo) {
  if (r.erro === "ocupado") return espereResposta();
  const txt = /pronto|não respondeu/.test(r.erro || "") ? "O agente ainda está ligando — tente em instantes." : (r.erro || "");
  toast("alerta", titulo, txt, 7000, true);
}

let _tentativasLista = 0;
async function carregarConversas() {
  const r = await chamaConv("ConversasListar");
  if (r.pastas) conv.pastas = r.pastas;
  if (r.ok || (r.conversas && r.conversas.length)) conv.lista = r.conversas || [];
  conv.pronta = !!r.ok;
  // a ponte está em OUTRA conversa que a tela (subiu retomando a última, /new, outra
  // janela): a tela segue a ponte — é nela que a próxima mensagem vai cair
  if (r.ok && r.atual && r.atual !== conv.atual) {
    guardaAtual(r.atual);
    await carregaMensagens(r.atual);
  }
  pintaLado();
  pintaCabecalhoChat();
  // ainda ligando: tenta de novo (a ponte leva ~10-20 s para subir com o app) — uma
  // tentativa pendente por vez, por mais que outras chamadas cheguem no meio
  if (!r.ok && !r.ocupado && !_retryLista && _tentativasLista++ < 60) {
    _retryLista = setTimeout(() => { _retryLista = null; carregarConversas(); }, 5000);
  }
  return r.ok;
}
let _retryLista = null;

function horaDe(seg) {
  return new Date(seg * 1000).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });
}

async function carregaMensagens(id) {
  const r = await chamaConv("ConversaAbrir", id);
  if (!r.ok) { avisaErroConv(r, "Não abri a conversa"); return false; }
  conversa.length = 0;  // ⚠️ conversa é const no main.js: esvazia, não reatribui
  for (const m of r.mensagens || []) {
    conversa.push(Object.assign({ quem: m.quem, texto: m.texto },
      m.quem === "ele" && m.quando ? { hora: horaDe(m.quando) } : {}));
  }
  salvaVisivel();
  pintaConversa();
  return true;
}

async function abrirConversa(id) {
  if (!id || id === conv.atual) return;
  if (ocupado) return espereResposta();
  fechaMenuConv();
  ladoEl.classList.add("carregando");
  const ok = await carregaMensagens(id);
  ladoEl.classList.remove("carregando");
  if (!ok) return;
  guardaAtual(id);
  pintaLado();
  pintaCabecalhoChat();
  const input = document.getElementById(cg && !cg.classList.contains("oculto") ? "cg-txt" : "txt");
  if (input) input.focus();
}

async function novaConversa() {
  if (ocupado) return espereResposta();
  const r = await chamaConv("ConversaNova");
  if (!r.ok) return avisaErroConv(r, "Não criei a conversa");
  guardaAtual(r.atual);
  conversa.length = 0;
  salvaVisivel();
  pintaConversa();
  fechaMenuConv();
  await carregarConversas();
  const input = document.getElementById(cg && !cg.classList.contains("oculto") ? "cg-txt" : "txt");
  if (input) input.focus();
}

// o main.js chama depois de cada resposta: a conversa subiu para o topo, e o Hermes dá o
// título sozinho logo depois da primeira resposta (por isso a segunda leitura)
function conversaRespondida(id) {
  if (id && id !== conv.atual) guardaAtual(id);
  carregarConversas();
  setTimeout(carregarConversas, 6000);
}

function aoAbrirChatGrande() {
  if (!cg) return;
  let lado = null;
  try { lado = localStorage.getItem("conversas.lado"); } catch (e) { }
  // padrão: lista aberta se o chat grande tem espaço para ela
  cg.classList.toggle("sem-lado", lado === "0" || (lado === null && cg.offsetWidth < 640));
  carregarConversas();
}

/* ───────── a lista (lado esquerdo do chat grande) ───────── */

function dataCurta(seg) {
  if (!seg) return "";
  const d = new Date(seg * 1000), hoje = new Date();
  const dia = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const dif = Math.round((dia(hoje) - dia(d)) / 86400000);
  if (dif === 0) return horaDe(seg);
  if (dif === 1) return "ontem";
  if (dif < 7) return d.toLocaleDateString("pt-BR", { weekday: "short" }).replace(".", "");
  return d.toLocaleDateString("pt-BR", { day: "2-digit", month: "2-digit" });
}

function montaLado() {
  if (!ladoEl || ladoEl.dataset.montado) return;
  ladoEl.dataset.montado = "1";
  ladoEl.innerHTML = `
    <div class="lado-topo">
      <button class="cg-bt lado-nova" id="lado-nova">${icone("adicionar", 16)}Nova conversa</button>
      <label class="lado-busca">${icone("buscar", 15)}
        <input id="lado-busca" type="text" placeholder="Buscar conversas…" autocomplete="off" aria-label="Buscar conversas" /></label>
      <div class="lado-filtros" id="lado-filtros">
        <button class="lado-chip" id="lado-pasta-bt" aria-haspopup="listbox" aria-expanded="false" title="Filtrar por pasta">
          ${icone("pasta", 13)}<span></span>${icone("abaixo", 12)}</button>
        <button class="lado-chip" id="lado-tag-bt" aria-haspopup="listbox" aria-expanded="false" title="Filtrar por etiqueta">
          ${icone("etiqueta", 13)}<span></span>${icone("abaixo", 12)}</button>
      </div>
      <div class="lado-drop oculto" id="lado-drop" role="listbox"></div>
    </div>
    <div class="lado-lista" id="lado-lista"></div>`;
  document.getElementById("lado-nova").onclick = () => novaConversa();
  const busca = document.getElementById("lado-busca");
  busca.oninput = () => { conv.busca = busca.value; pintaLista(); };
  document.getElementById("lado-pasta-bt").onclick = () => alternaDrop("pasta");
  document.getElementById("lado-tag-bt").onclick = () => alternaDrop("tag");
}

/* O filtro abre um MENU DO APP, não o <select> do Windows (dona, 23/10/2026: a lista nativa
   saía branca, com fonte do sistema, fora do tema). Cada opção mostra quantas conversas tem. */
let _drop = "";   // "pasta" | "tag" | "" (fechado)

function alternaDrop(tipo) {
  if (_drop === tipo) return fechaDrop();
  _drop = tipo;
  const drop = document.getElementById("lado-drop");
  const bt = document.getElementById(tipo === "pasta" ? "lado-pasta-bt" : "lado-tag-bt");
  const conta = (f) => conv.lista.filter(f).length;
  const itens = tipo === "pasta"
    ? [["*", "Todas as pastas", conv.lista.length, "lista"],
       ...conv.pastas.map((p) => [p, p, conta((c) => c.pasta === p), "pasta"]),
       ["", "Sem pasta", conta((c) => !c.pasta), "pasta"]]
    : [["", "Todas as etiquetas", conv.lista.length, "lista"],
       ...todasEtiquetas().map((t) => [t, "#" + t, conta((c) => (c.etiquetas || []).includes(t)), "etiqueta"])];
  const atual = tipo === "pasta" ? conv.pasta : conv.etiqueta;
  drop.innerHTML = "";
  drop.setAttribute("aria-label", tipo === "pasta" ? "Filtrar por pasta" : "Filtrar por etiqueta");
  for (const [valor, rotulo, qtd, ic] of itens) {
    const o = document.createElement("button");
    o.className = "lado-op" + (valor === atual ? " sel" : "");
    o.setAttribute("role", "option");
    o.setAttribute("aria-selected", String(valor === atual));
    o.innerHTML = icone(ic, 14) + "<span></span><em></em>" + (valor === atual ? icone("ok", 14) : "");
    o.querySelector("span").textContent = rotulo;
    o.querySelector("em").textContent = qtd;
    o.onclick = () => {
      if (tipo === "pasta") conv.pasta = valor; else conv.etiqueta = valor;
      fechaDrop();
      pintaLado();
    };
    drop.appendChild(o);
  }
  // debaixo do botão que abriu, sem passar da borda direita da lista
  drop.classList.remove("oculto");
  const topo = bt.parentElement.parentElement;
  drop.style.top = (bt.offsetTop + bt.offsetHeight + 4) + "px";
  drop.style.left = Math.max(10, Math.min(bt.offsetLeft, topo.clientWidth - drop.offsetWidth - 10)) + "px";
  bt.setAttribute("aria-expanded", "true");
  const sel = drop.querySelector(".sel") || drop.firstChild;
  if (sel) sel.focus();
}

function fechaDrop() {
  if (!_drop) return;
  _drop = "";
  const drop = document.getElementById("lado-drop");
  if (drop) drop.classList.add("oculto");
  for (const id of ["lado-pasta-bt", "lado-tag-bt"]) {
    const b = document.getElementById(id);
    if (b) b.setAttribute("aria-expanded", "false");
  }
}

function todasEtiquetas() {
  return [...new Set(conv.lista.flatMap((c) => c.etiquetas || []))].sort((a, b) => a.localeCompare(b, "pt-BR"));
}

function pintaLado() {
  if (!ladoEl) return;
  montaLado();
  // rótulos curtos (a escolha atual, ou "Pastas"/"Etiquetas" = todas); sem nada para filtrar,
  // o filtro some
  if (conv.pasta !== "*" && conv.pasta !== "" && !conv.pastas.includes(conv.pasta)) conv.pasta = "*";
  const tags = todasEtiquetas();
  if (conv.etiqueta && !tags.includes(conv.etiqueta)) conv.etiqueta = "";
  const btP = document.getElementById("lado-pasta-bt"), btT = document.getElementById("lado-tag-bt");
  btP.querySelector("span").textContent = conv.pasta === "*" ? "Pastas" : (conv.pasta || "Sem pasta");
  btT.querySelector("span").textContent = conv.etiqueta ? "#" + conv.etiqueta : "Etiquetas";
  const temPasta = conv.pastas.length > 0 || conv.pasta !== "*", temTag = tags.length > 0;
  btP.classList.toggle("oculto", !temPasta);
  btT.classList.toggle("oculto", !temTag);
  document.getElementById("lado-filtros").classList.toggle("oculto", !temPasta && !temTag);
  btP.classList.toggle("ativa", conv.pasta !== "*");
  btT.classList.toggle("ativa", !!conv.etiqueta);
  pintaLista();
}

function pintaLista() {
  const box = document.getElementById("lado-lista");
  if (!box) return;
  const termo = conv.busca.trim().toLowerCase();
  const itens = conv.lista.filter((c) =>
    (conv.pasta === "*" || (c.pasta || "") === conv.pasta) &&
    (!conv.etiqueta || (c.etiquetas || []).includes(conv.etiqueta)) &&
    (!termo || [tituloDe(c), c.previa, ...(c.etiquetas || [])].join(" ").toLowerCase().includes(termo)));
  const rolagem = box.scrollTop;   // redesenhar não pode jogar a lista para o topo
  box.innerHTML = "";
  if (!itens.length) {
    const v = document.createElement("p");
    v.className = "lado-vazio";
    v.textContent = !conv.pronta && !conv.lista.length ? "Ligando o agente…"
      : conv.lista.length ? "Nada encontrado." : "Nenhuma conversa ainda.";
    box.appendChild(v);
    return;
  }
  const grupos = [...conv.pastas, ""].map((p) => [p, itens.filter((c) => (c.pasta || "") === p)])
    .filter(([, l]) => l.length);
  // pasta de uma conversa que não está mais na lista de pastas (não some da tela)
  for (const c of itens) if (c.pasta && !conv.pastas.includes(c.pasta) && !grupos.some(([p]) => p === c.pasta)) grupos.unshift([c.pasta, itens.filter((x) => x.pasta === c.pasta)]);
  const soUmGrupoSemPasta = grupos.length === 1 && grupos[0][0] === "";
  for (const [pasta, lista] of grupos) {
    const chave = pasta || "∅";
    if (!soUmGrupoSemPasta) {
      const g = document.createElement("button");
      g.className = "lado-grupo" + (conv.fechadas[chave] ? " fechado" : "");
      g.innerHTML = icone("abaixo", 13) + "<span></span><em></em>";
      g.querySelector("span").textContent = pasta || "Sem pasta";
      g.querySelector("em").textContent = lista.length;
      g.setAttribute("aria-expanded", String(!conv.fechadas[chave]));
      g.onclick = () => {
        conv.fechadas[chave] = !conv.fechadas[chave];
        try { localStorage.setItem("conversas.fechadas", JSON.stringify(conv.fechadas)); } catch (e) { }
        pintaLista();
      };
      box.appendChild(g);
      if (conv.fechadas[chave] && !termo) continue;
    }
    for (const c of lista) box.appendChild(itemLado(c));
  }
  box.scrollTop = rolagem;
}

function itemLado(c) {
  const linha = document.createElement("div");
  linha.className = "lado-item" + (c.id === conv.atual ? " ativa" : "") + (c.id === conv.menuDe ? " menu-de" : "");
  linha.dataset.id = c.id;
  const bt = document.createElement("button");
  bt.className = "lado-abre";
  bt.innerHTML = `<span class="li-topo"><span class="li-tit"></span><span class="li-data"></span></span>
    <span class="li-previa"></span><span class="li-tags"></span>`;
  bt.querySelector(".li-tit").textContent = tituloDe(c);
  bt.querySelector(".li-data").textContent = dataCurta(c.inicio);
  bt.querySelector(".li-previa").textContent = c.titulo ? (c.previa || "") : "";
  const tags = bt.querySelector(".li-tags");
  for (const e of c.etiquetas || []) {
    const t = document.createElement("span");
    t.textContent = "#" + e;
    tags.appendChild(t);
  }
  bt.onclick = () => abrirConversa(c.id);
  const mais = document.createElement("button");
  mais.className = "lado-mais botao-x";
  mais.innerHTML = icone("reticencias", 16);
  mais.title = "Renomear, pasta, etiquetas, excluir";
  mais.setAttribute("aria-label", "Opções de " + tituloDe(c));
  mais.onclick = (ev) => {
    ev.stopPropagation();
    if (conv.menuDe === c.id) return fechaMenuConv();
    abreMenuConv(c.id, false, true);
  };
  linha.append(bt, mais);
  return linha;
}

/* ───────── menu ⋯ da conversa ───────── */

// daLista=true: aberto pelo ⋯ de uma linha — o menu abre AO LADO dela (e não no canto do
// cabeçalho, longe da conversa: pedido da dona, 23/10/2026), e a linha fica destacada
function abreMenuConv(id, focarNome, daLista) {
  if (!menuConvEl || !id) return;
  if (cg && cg.classList.contains("oculto")) abrirChatGrande();
  conv.menuDe = id;
  conv.novaPasta = false;
  if (daLista) menuConvEl.dataset.daLista = "1";
  else delete menuConvEl.dataset.daLista;
  pintaLista();   // destaca a linha da conversa do menu
  menuConvEl.classList.remove("oculto");
  pintaMenuConv(); // já visível: mede a altura e se posiciona
  const alvo = document.getElementById(focarNome ? "cm-nome" : "cm-tag");
  if (focarNome && alvo) { alvo.focus(); alvo.select(); }
  reportarHit();
}

function posicionaMenuConv() {
  const s = menuConvEl.style;
  s.left = s.top = s.right = "";
  const linha = menuConvEl.dataset.daLista && ladoEl.querySelector('.lado-item[data-id="' + CSS.escape(conv.menuDe) + '"]');
  if (!linha || !cg) return;           // do cabeçalho: o canto de cima (CSS)
  const rc = cg.getBoundingClientRect(), rl = linha.getBoundingClientRect();
  const alto = menuConvEl.offsetHeight;
  s.right = "auto";
  s.left = Math.round(rl.right - rc.left + 8) + "px";
  // alinhado com a linha; se não couber para baixo, sobe o que faltar
  s.top = Math.round(Math.max(8, Math.min(rl.top - rc.top - 6, cg.clientHeight - alto - 8))) + "px";
}

function fechaMenuConv() {
  if (!menuConvEl || menuConvEl.classList.contains("oculto")) return;
  menuConvEl.classList.add("oculto");
  delete menuConvEl.dataset.daLista;
  conv.menuDe = "";
  pintaLista();
  reportarHit();
}

/* ───────── modal de excluir ─────────
   Era uma caixa DENTRO do menu, que crescia e criava rolagem (dona, 23/10/2026): agora é um
   modal no meio do chat grande, com o fundo escurecido. */
function abreModalExcluir(c) {
  fechaMenuConv();
  let m = document.getElementById("conv-modal");
  if (!m) {
    m = document.createElement("div");
    m.id = "conv-modal";
    m.className = "conv-modal oculto";
    m.innerHTML = `<div class="cmod-caixa" role="alertdialog" aria-modal="true" aria-labelledby="cmod-tit" aria-describedby="cmod-txt">
      <div class="cmod-ic">${icone("lixeira", 22)}</div>
      <b id="cmod-tit"></b>
      <span id="cmod-txt">A conversa e os anexos dela somem do PetAgent e do histórico da IA. Não dá para desfazer.</span>
      <div class="cmod-acoes"><button class="cm-bt" id="cmod-nao">Cancelar</button><button class="cm-bt perigo" id="cmod-sim">Excluir</button></div>
    </div>`;
    cg.appendChild(m);
    m.addEventListener("mousedown", (e) => { if (e.target === m) fechaModalExcluir(); }); // clicar no fundo
    document.getElementById("cmod-nao").onclick = fechaModalExcluir;
  }
  document.getElementById("cmod-tit").textContent = "Excluir “" + tituloDe(c) + "”?";
  document.getElementById("cmod-sim").onclick = () => { fechaModalExcluir(); excluiConversa(c.id); };
  m.classList.remove("oculto");
  document.getElementById("cmod-nao").focus();   // o padrão seguro: Enter cancela
  reportarHit();
}

function fechaModalExcluir() {
  const m = document.getElementById("conv-modal");
  if (m && !m.classList.contains("oculto")) { m.classList.add("oculto"); reportarHit(); }
}
const modalAberto = () => { const m = document.getElementById("conv-modal"); return m && !m.classList.contains("oculto"); };

function pintaMenuConv() {
  const c = conv.lista.find((x) => x.id === conv.menuDe) || { id: conv.menuDe, titulo: "", etiquetas: [], pasta: "" };
  const et = c.etiquetas || [];
  menuConvEl.innerHTML = `
    <label class="cm-rot" for="cm-nome">Nome da conversa</label>
    <input id="cm-nome" type="text" maxlength="80" autocomplete="off" />
    <span class="cm-dica">Enter salva · o nome vem sozinho depois da 1ª resposta</span>
    <hr />
    <span class="cm-rot">Pasta</span>
    <div class="cm-pastas" id="cm-pastas"></div>
    <hr />
    <span class="cm-rot">Etiquetas</span>
    <div class="cm-tags" id="cm-tags"></div>
    <input id="cm-tag" type="text" placeholder="adicionar etiqueta… (Enter)" autocomplete="off" aria-label="Adicionar etiqueta" />
    <div class="cm-sug" id="cm-sug"></div>
    <hr />
    <button class="cm-excluir" id="cm-excluir">${icone("lixeira", 16)}Excluir conversa</button>`;
  const nome = document.getElementById("cm-nome");
  nome.value = c.titulo || tituloDe(c);
  const salvaNome = async () => {
    const v = nome.value.trim();
    if (!v || v === (c.titulo || tituloDe(c))) return;
    const r = await chamaConv("ConversaRenomear", c.id, v);
    if (!r.ok) return avisaErroConv(r, "Não renomeei");
    c.titulo = r.titulo;
    pintaLado();
    pintaCabecalhoChat();
  };
  nome.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); salvaNome(); nome.blur(); } };
  nome.onblur = salvaNome;

  // pastas: as que existem + "Sem pasta" + criar nova (+ apagar uma pasta: as conversas
  // dela vão para "Sem pasta", nada é apagado)
  const boxP = document.getElementById("cm-pastas");
  for (const p of [...conv.pastas, ""]) {
    const linha = document.createElement("div");
    linha.className = "cm-pasta" + ((c.pasta || "") === p ? " sel" : "");
    const bt = document.createElement("button");
    bt.className = "cm-pasta-bt";
    bt.innerHTML = icone("pasta", 15) + "<span></span>" + ((c.pasta || "") === p ? icone("ok", 15) : "");
    bt.querySelector("span").textContent = p || "Sem pasta";
    bt.onclick = () => organiza(c, p, et);
    linha.appendChild(bt);
    if (p) {
      const x = document.createElement("button");
      x.className = "cm-pasta-x botao-x mini";
      x.innerHTML = icone("fechar", 13);
      x.title = "Apagar a pasta (as conversas dela vão para Sem pasta)";
      x.setAttribute("aria-label", "Apagar a pasta " + p);
      x.onclick = async () => {
        const r = await chamaConv("PastaExcluir", p);
        if (!r.ok) return;
        conv.pastas = r.pastas || [];
        for (const k of conv.lista) if (k.pasta === p) k.pasta = "";
        pintaLado(); pintaMenuConv(); pintaCabecalhoChat();
      };
      linha.appendChild(x);
    }
    boxP.appendChild(linha);
  }
  if (conv.novaPasta) {
    const campo = document.createElement("input");
    campo.type = "text";
    campo.placeholder = "nome da pasta (Enter)";
    campo.className = "cm-nova-pasta";
    campo.setAttribute("aria-label", "Nome da pasta nova");
    campo.onkeydown = (e) => {
      if (e.key === "Enter" && campo.value.trim()) { conv.novaPasta = false; organiza(c, campo.value.trim(), et); }
      if (e.key === "Escape") { e.stopPropagation(); conv.novaPasta = false; pintaMenuConv(); }
    };
    boxP.appendChild(campo);
    setTimeout(() => campo.focus(), 0);
  } else {
    const nova = document.createElement("button");
    nova.className = "cm-pasta-bt sub";
    nova.innerHTML = icone("adicionar", 15) + "<span>Nova pasta…</span>";
    nova.onclick = () => { conv.novaPasta = true; pintaMenuConv(); };
    boxP.appendChild(nova);
  }

  // etiquetas: chips com ✕, campo para adicionar (Enter) e as já usadas como sugestão
  const boxT = document.getElementById("cm-tags");
  for (const e of et) {
    const chip = document.createElement("span");
    chip.className = "cm-chip";
    chip.textContent = "#" + e;
    const x = document.createElement("button");
    x.innerHTML = icone("fechar", 12);
    x.setAttribute("aria-label", "Tirar a etiqueta " + e);
    x.onclick = () => organiza(c, c.pasta || "", et.filter((t) => t !== e));
    chip.appendChild(x);
    boxT.appendChild(chip);
  }
  const campoTag = document.getElementById("cm-tag");
  const sug = document.getElementById("cm-sug");
  const pintaSug = () => {
    const termo = campoTag.value.trim().toLowerCase().replace(/^#/, "");
    const opc = todasEtiquetas().filter((t) => !et.includes(t) && (!termo || t.includes(termo))).slice(0, 8);
    sug.innerHTML = opc.length ? '<span class="cm-dica">já usadas:</span>' : "";
    for (const t of opc) {
      const b = document.createElement("button");
      b.className = "cm-sug-bt";
      b.textContent = "#" + t;
      b.onclick = () => organiza(c, c.pasta || "", [...et, t]);
      sug.appendChild(b);
    }
  };
  campoTag.oninput = pintaSug;
  campoTag.onkeydown = (e) => {
    if (e.key === "Enter" && campoTag.value.trim()) organiza(c, c.pasta || "", [...et, campoTag.value]);
  };
  pintaSug();

  document.getElementById("cm-excluir").onclick = () => abreModalExcluir(c);
  if (!menuConvEl.classList.contains("oculto")) posicionaMenuConv(); // a altura pode ter mudado
}

async function organiza(c, pasta, etiquetas) {
  const r = await chamaConv("ConversaOrganizar", c.id, pasta, JSON.stringify(etiquetas));
  if (!r.ok) return avisaErroConv(r, "Não salvei");
  c.pasta = r.pasta;
  c.etiquetas = r.etiquetas || [];
  if (r.pasta && !conv.pastas.includes(r.pasta)) conv.pastas = [...conv.pastas, r.pasta].sort((a, b) => a.localeCompare(b, "pt-BR"));
  pintaLado();
  pintaMenuConv();
  pintaCabecalhoChat();
  const campo = document.getElementById("cm-tag");
  if (campo) campo.focus();
}

async function excluiConversa(id) {
  if (ocupado) return espereResposta();
  const r = await chamaConv("ConversaExcluir", id);
  if (!r.ok) return avisaErroConv(r, "Não excluí");
  fechaMenuConv();
  conv.lista = conv.lista.filter((c) => c.id !== id);
  if (id === conv.atual) {   // apagou a aberta: a ponte já passou para uma nova
    guardaAtual(r.atual);
    conversa.length = 0;
    salvaVisivel();
    pintaConversa();
  }
  await carregarConversas();
  toast("lixeira", "Conversa excluída", "", 3000, true);
}

/* ───────── botões e teclado ───────── */

const _bt = (id, fn) => { const b = document.getElementById(id); if (b) b.onclick = fn; };
_bt("balao-nova", () => novaConversa());
_bt("balao-quem", () => abrirChatGrande());
_bt("cg-conv-menu", () => (menuConvEl.classList.contains("oculto") ? abreMenuConv(conv.atual) : fechaMenuConv()));
_bt("cg-renomear", () => abreMenuConv(conv.atual, true));
_bt("cg-lado-bt", () => {
  const sem = !cg.classList.toggle("sem-lado") ? "1" : "0";
  try { localStorage.setItem("conversas.lado", sem); } catch (e) { }
  reportarHit();
});
// clicar fora fecha o menu ⋯ (o ⋯ do cabeçalho e o das linhas têm o próprio clique)
document.addEventListener("mousedown", (e) => {
  if (!menuConvEl || menuConvEl.classList.contains("oculto")) return;
  if (menuConvEl.contains(e.target) || e.target.closest("#cg-conv-menu, .lado-mais, #cg-renomear")) return;
  fechaMenuConv();
}, true);
// clicar fora fecha o menu do filtro (o botão do filtro tem o próprio clique: abre/fecha)
document.addEventListener("mousedown", (e) => {
  if (_drop && !e.target.closest("#lado-drop, #lado-pasta-bt, #lado-tag-bt")) fechaDrop();
}, true);
// menu do filtro: ↑ ↓ andam pelas opções (Enter escolhe: são botões)
document.addEventListener("keydown", (e) => {
  if (!_drop || (e.key !== "ArrowDown" && e.key !== "ArrowUp")) return;
  const ops = [...document.querySelectorAll("#lado-drop .lado-op")];
  const i = ops.indexOf(document.activeElement);
  if (!ops.length) return;
  e.preventDefault();
  ops[(i + (e.key === "ArrowDown" ? 1 : -1) + ops.length) % ops.length].focus();
});
// Esc fecha o modal ou o menu antes de fechar o chat (fase de captura: antes do Esc do main.js)
document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  if (_drop) {
    e.stopImmediatePropagation();
    const bt = document.getElementById(_drop === "pasta" ? "lado-pasta-bt" : "lado-tag-bt");
    fechaDrop();
    if (bt) bt.focus();
  } else if (modalAberto()) { e.stopImmediatePropagation(); fechaModalExcluir(); }
  else if (menuConvEl && !menuConvEl.classList.contains("oculto")) { e.stopImmediatePropagation(); fechaMenuConv(); }
}, true);
// rolar a lista com o menu aberto ao lado de uma linha: o menu acompanha a linha; se ela
// saiu da área visível da lista, ele fecha (apontaria para o nada)
document.addEventListener("scroll", (e) => {
  if (!e.target || e.target.id !== "lado-lista" || !menuConvEl.dataset.daLista) return;
  const linha = ladoEl.querySelector('.lado-item[data-id="' + CSS.escape(conv.menuDe) + '"]');
  const rb = e.target.getBoundingClientRect(), rl = linha && linha.getBoundingClientRect();
  if (!rl || rl.bottom < rb.top || rl.top > rb.bottom) fechaMenuConv();
  else posicionaMenuConv();
}, true);

pintaLado();
setTimeout(carregarConversas, 3000); // a ponte sobe junto com o app: a lista vem quando ela estiver pronta
