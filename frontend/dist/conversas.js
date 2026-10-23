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
  menuDe: "",         // conversa cujo menu ⋯ está aberto
  confirmando: false, // menu mostrando "Excluir? Cancelar / Excluir"
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

// título do cabeçalho: vazio enquanto a lista não chegou (o main.js mostra o nome do pet)
function tituloConversa() { return tituloDe(dadosDaAtual()); }

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
      <div class="lado-filtros">
        <label class="lado-chip">${icone("pasta", 13)}<select id="lado-pasta" aria-label="Filtrar por pasta"></select></label>
        <label class="lado-chip tag" id="lado-tag-rot">${icone("etiqueta", 13)}<select id="lado-tag" aria-label="Filtrar por etiqueta"></select></label>
      </div>
    </div>
    <div class="lado-lista" id="lado-lista"></div>`;
  document.getElementById("lado-nova").onclick = () => novaConversa();
  const busca = document.getElementById("lado-busca");
  busca.oninput = () => { conv.busca = busca.value; pintaLista(); };
  document.getElementById("lado-pasta").onchange = (e) => { conv.pasta = e.target.value; pintaLado(); };
  document.getElementById("lado-tag").onchange = (e) => { conv.etiqueta = e.target.value; pintaLado(); };
}

function opcoes(sel, lista, valor) {
  sel.innerHTML = "";
  for (const [v, r] of lista) {
    const o = document.createElement("option");
    o.value = v;
    o.textContent = r;
    sel.appendChild(o);
  }
  sel.value = lista.some(([v]) => v === valor) ? valor : lista[0][0];
}

function todasEtiquetas() {
  return [...new Set(conv.lista.flatMap((c) => c.etiquetas || []))].sort((a, b) => a.localeCompare(b, "pt-BR"));
}

function pintaLado() {
  if (!ladoEl) return;
  montaLado();
  const pastas = [["*", "Todas as pastas"], ...conv.pastas.map((p) => [p, p]), ["", "Sem pasta"]];
  opcoes(document.getElementById("lado-pasta"), pastas, conv.pasta);
  conv.pasta = document.getElementById("lado-pasta").value;
  const tags = todasEtiquetas();
  if (conv.etiqueta && !tags.includes(conv.etiqueta)) conv.etiqueta = "";
  opcoes(document.getElementById("lado-tag"), [["", tags.length ? "Etiqueta" : "Sem etiquetas"], ...tags.map((t) => [t, "#" + t])], conv.etiqueta);
  document.getElementById("lado-tag-rot").classList.toggle("ativa", !!conv.etiqueta);
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
}

function itemLado(c) {
  const linha = document.createElement("div");
  linha.className = "lado-item" + (c.id === conv.atual ? " ativa" : "");
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
  mais.onclick = (ev) => { ev.stopPropagation(); abreMenuConv(c.id); };
  linha.append(bt, mais);
  return linha;
}

/* ───────── menu ⋯ da conversa ───────── */

function abreMenuConv(id, focarNome) {
  if (!menuConvEl || !id) return;
  if (cg && cg.classList.contains("oculto")) abrirChatGrande();
  conv.menuDe = id;
  conv.confirmando = false;
  conv.novaPasta = false;
  pintaMenuConv();
  menuConvEl.classList.remove("oculto");
  const alvo = document.getElementById(focarNome ? "cm-nome" : "cm-tag");
  if (focarNome && alvo) { alvo.focus(); alvo.select(); }
  reportarHit();
}

function fechaMenuConv() {
  if (!menuConvEl || menuConvEl.classList.contains("oculto")) return;
  menuConvEl.classList.add("oculto");
  conv.menuDe = "";
  reportarHit();
}

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
    <button class="cm-excluir" id="cm-excluir">${icone("lixeira", 16)}Excluir conversa</button>
    <div class="cm-confirma oculto" id="cm-confirma" role="alertdialog" aria-label="Confirmar exclusão">
      <b></b>
      <span>A conversa e os anexos dela somem do PetAgent e do histórico da IA. Não dá para desfazer.</span>
      <div><button class="cm-bt" id="cm-cancelar">Cancelar</button><button class="cm-bt perigo" id="cm-sim">Excluir</button></div>
    </div>`;
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

  const conf = document.getElementById("cm-confirma");
  conf.querySelector("b").textContent = "Excluir “" + tituloDe(c) + "”?";
  conf.classList.toggle("oculto", !conv.confirmando);
  document.getElementById("cm-excluir").onclick = () => { conv.confirmando = true; conf.classList.remove("oculto"); };
  document.getElementById("cm-cancelar").onclick = () => { conv.confirmando = false; conf.classList.add("oculto"); };
  document.getElementById("cm-sim").onclick = () => excluiConversa(c.id);
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
// Esc fecha o menu antes de fechar o chat (fase de captura: antes do Esc do main.js)
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && menuConvEl && !menuConvEl.classList.contains("oculto")) {
    e.stopImmediatePropagation();
    fechaMenuConv();
  }
}, true);

pintaLado();
setTimeout(carregarConversas, 3000); // a ponte sobe junto com o app: a lista vem quando ela estiver pronta
