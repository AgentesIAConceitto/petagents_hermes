/* PetAgent — anexos no chat (22/10/2026). Pedido da dona: "uma forma de anexar arquivos no
   chat, para a IA analisar arquivos, ver imagem etc.". Telas na prancha "PetAgent — Conversas
   e Anexos" (rodapé com 📎, chips antes de enviar, avisos, "Solte para anexar").

   Três jeitos de anexar: o 📎 (abre o seletor), arrastar para o balão ou o chat grande, e
   colar um print (Ctrl+V) no campo de texto. Cada arquivo vai na hora para o Go
   (AnexoPreparar), que valida, extrai o texto do PDF e estima os tokens — o aviso aparece ao
   ESCOLHER, não ao enviar. A fila é uma só para o balão e o chat grande (a conversa é a
   mesma); o enviarTexto() do main.js leva os ids por anexosDaMensagem().

   Imagem grande é reduzida aqui antes (lado maior de 1568 px — o que os modelos usam de
   fato): sai mais leve e não cobra token por pixel que o modelo descartaria. */

const ANEXO_MAX = 5;
const ANEXO_MAX_MB = 20;
const IMG_LADO_MAX = 1568;
const ANEXO_BOTOES = ["anexo", "cg-anexo"];
const ANEXO_FILAS = ["balao-anexos", "cg-anexos"];

const filaAnexos = [];   // {chave, nome, tipo, estado: lendo|pronto|confirmar, id, tokens, paginas, bytes, thumb, semVisao}
const avisosAnexo = [];  // {chave, tom: erro|aviso, html, acoes: [{r, fn, prim}]}
let _chaveAnexo = 0;

const ehImagemNome = (n) => /\.(png|jpe?g|gif|webp|bmp)$/i.test(n || "");

function tamanhoLegivel(b) {
  if (b < 1024 * 1024) return Math.max(1, Math.round(b / 1024)) + " KB";
  return (b / 1048576).toLocaleString("pt-BR", { maximumFractionDigits: 1 }) + " MB";
}

function tokensLegivel(t) {
  if (!t) return "";
  if (t < 1000) return "~" + t + " tokens";
  const mil = t / 1000;
  return "~" + mil.toLocaleString("pt-BR", { maximumFractionDigits: mil < 10 ? 1 : 0 }) + " mil tokens";
}

/* ───────── entrar na fila ───────── */

async function adicionarArquivos(lista) {
  for (const f of Array.from(lista || [])) {
    if (filaAnexos.length >= ANEXO_MAX) {
      avisoAnexo("erro", `No máximo ${ANEXO_MAX} arquivos por mensagem — <b>${escHtml(f.name)}</b> ficou de fora.`);
      break;
    }
    if (f.size > ANEXO_MAX_MB * 1048576 && !ehImagemNome(f.name)) {
      avisoAnexo("erro", `<b>${escHtml(f.name)} não foi.</b> Tem ${tamanhoLegivel(f.size)} e o limite é ${ANEXO_MAX_MB} MB por arquivo.`);
      continue;
    }
    const item = { chave: ++_chaveAnexo, nome: f.name || "anexo", tipo: ehImagemNome(f.name) ? "imagem" : /\.pdf$/i.test(f.name) ? "pdf" : "texto",
      estado: "lendo", bytes: f.size };
    filaAnexos.push(item);
    pintaFilaAnexos();
    preparaAnexo(item, f);
  }
}

async function preparaAnexo(item, f) {
  let r;
  try {
    let blob = f, nome = item.nome;
    if (item.tipo === "imagem") {
      const red = await reduzImagem(f);
      blob = red.blob; nome = red.nome; item.thumb = red.thumb; item.bytes = blob.size;
    }
    r = JSON.parse(await G.AnexoPreparar(nome, await blobBase64(blob)));
  } catch (e) {
    r = { ok: false, erro: "não consegui ler " + item.nome + " (" + (e && e.message || e) + ")" };
  }
  const i = filaAnexos.indexOf(item);
  if (i < 0) { if (r.ok && G) G.AnexoDescartar(r.id); return; } // tirado enquanto lia
  if (!r.ok) {
    filaAnexos.splice(i, 1);
    avisoAnexo("erro", escHtml(r.erro || "não deu para anexar"));
    return pintaFilaAnexos();
  }
  Object.assign(item, { id: r.id, tipo: r.tipo, tokens: r.tokens, paginas: r.paginas, semVisao: !!r.semVisao,
    estado: r.grande ? "confirmar" : "pronto" });
  pintaFilaAnexos();
}

// lado maior até 1568 px; webp/bmp viram JPEG (o Go mede png/jpeg/gif). Miniatura de 160 px
// para o chip e para a bolha da mensagem.
async function reduzImagem(f) {
  const url = URL.createObjectURL(f);
  try {
    const img = await new Promise((ok, erro) => {
      const i = new Image();
      i.onload = () => ok(i);
      i.onerror = () => erro(new Error("imagem ilegível"));
      i.src = url;
    });
    const w = img.naturalWidth, h = img.naturalHeight;
    const desenha = (lado, tipo, q) => {
      const k = Math.min(1, lado / Math.max(w, h));
      const c = document.createElement("canvas");
      c.width = Math.max(1, Math.round(w * k));
      c.height = Math.max(1, Math.round(h * k));
      const ctx = c.getContext("2d");
      if (tipo === "image/jpeg") { ctx.fillStyle = "#fff"; ctx.fillRect(0, 0, c.width, c.height); }
      ctx.drawImage(img, 0, 0, c.width, c.height);
      return c;
    };
    const thumb = desenha(160, "image/jpeg").toDataURL("image/jpeg", 0.72);
    const nativo = /\.(png|jpe?g|gif)$/i.test(f.name);
    if (nativo && Math.max(w, h) <= IMG_LADO_MAX && f.size <= 4 * 1048576) return { blob: f, nome: f.name, thumb };
    const png = /\.png$/i.test(f.name);
    const tipo = png ? "image/png" : "image/jpeg";
    const c = desenha(IMG_LADO_MAX, tipo);
    let blob = await new Promise((ok) => c.toBlob(ok, tipo, 0.88));
    let nome = f.name.replace(/\.[^.]+$/, "") + (png ? ".png" : ".jpg");
    if (png && blob.size > 4 * 1048576) {   // print enorme em PNG: JPEG fica 5-10× menor
      blob = await new Promise((ok) => desenha(IMG_LADO_MAX, "image/jpeg").toBlob(ok, "image/jpeg", 0.88));
      nome = nome.replace(/\.png$/i, ".jpg");
    }
    return { blob, nome, thumb };
  } finally {
    URL.revokeObjectURL(url);
  }
}

function tiraAnexo(item) {
  const i = filaAnexos.indexOf(item);
  if (i >= 0) filaAnexos.splice(i, 1);
  if (item.id && G) G.AnexoDescartar(item.id);
  pintaFilaAnexos();
}

function avisoAnexo(tom, html, acoes) {
  const a = { chave: ++_chaveAnexo, tom, html, acoes: acoes || [] };
  avisosAnexo.push(a);
  if (avisosAnexo.length > 3) avisosAnexo.shift();
  pintaFilaAnexos();
  if (!a.acoes.length) setTimeout(() => dispensaAviso(a), 12000);
}

function dispensaAviso(a) {
  const i = avisosAnexo.indexOf(a);
  if (i >= 0) { avisosAnexo.splice(i, 1); pintaFilaAnexos(); }
}

/* ───────── o que o enviarTexto() leva ───────── */

// {ids, itens, custo} — ou false se ainda não dá para enviar (lendo / grande sem confirmar)
function anexosDaMensagem() {
  if (!filaAnexos.length) return { ids: [], itens: [], custo: "" };
  if (filaAnexos.some((i) => i.estado === "lendo")) {
    toast("relogio", "Ainda lendo os anexos", "Um instante — envio quando terminar de ler.", 4000, true);
    return false;
  }
  if (filaAnexos.some((i) => i.estado === "confirmar")) {
    toast("alerta", "Confirme o arquivo grande", "Escolha “Mandar assim mesmo” ou tire o arquivo.", 5000, true);
    return false;
  }
  const itens = filaAnexos.map((i) => ({ nome: i.nome, tipo: i.tipo, thumb: i.thumb || "" }));
  const tokens = filaAnexos.reduce((s, i) => s + (i.tokens || 0), 0);
  const r = {
    ids: filaAnexos.map((i) => i.id),
    itens,
    custo: (itens.length === 1 ? "1 anexo" : itens.length + " anexos") + (tokens ? " · " + tokensLegivel(tokens) : ""),
  };
  filaAnexos.length = 0;   // os avisos da fila (modelo cego, grande) somem junto
  pintaFilaAnexos();
  return r;
}

/* ───────── desenhar a fila (balão e chat grande) ───────── */

function avisosDaFila() {
  // os avisos que dependem da fila (imagem com modelo cego, arquivo grande) saem dela mesma
  const lista = [];
  const cegas = filaAnexos.filter((i) => i.semVisao && i.estado !== "lendo");
  if (cegas.length) {
    const modelo = (typeof infoChat !== "undefined" && infoChat.modelo) || "o modelo atual";
    lista.push({ tom: "aviso",
      html: `<b>${escHtml(modelo)} não enxerga imagens.</b> Para analisar ${cegas.length > 1 ? "as imagens" : "a imagem"}, troque para Claude, GPT ou Gemini, ou tire e mande só o texto.`,
      acoes: [{ r: "Trocar o modelo", prim: true, fn: () => { if (typeof abrirInteligencia === "function") abrirInteligencia(false); } },
        { r: cegas.length > 1 ? "Tirar as imagens" : "Tirar a imagem", fn: () => cegas.forEach(tiraAnexo) }] });
  }
  for (const i of filaAnexos.filter((x) => x.estado === "confirmar")) {
    lista.push({ tom: "aviso",
      html: `<b>${escHtml(i.nome)} é grande:</b> ${tokensLegivel(i.tokens)}${i.paginas ? " (" + i.paginas + " páginas)" : ""}. ` +
        "Ele volta junto em cada mensagem desta conversa. Mandar assim mesmo?",
      acoes: [{ r: "Mandar assim mesmo", prim: true, fn: () => { i.estado = "pronto"; pintaFilaAnexos(); } },
        { r: "Tirar", fn: () => tiraAnexo(i) }] });
  }
  return [...lista, ...avisosAnexo];
}

function pintaFilaAnexos() {
  const avisos = avisosDaFila();
  for (const id of ANEXO_FILAS) {
    const box = document.getElementById(id);
    if (!box) continue;
    box.innerHTML = "";
    box.classList.toggle("oculto", !filaAnexos.length && !avisos.length);
    for (const a of avisos) box.appendChild(bannerAnexo(a));
    if (filaAnexos.length) {
      const chips = document.createElement("div");
      chips.className = "anexos-chips";
      for (const i of filaAnexos) chips.appendChild(chipAnexo(i));
      box.appendChild(chips);
    }
    box.scrollTop = box.scrollHeight; // no balão estreito, os avisos não escondem os chips
  }
  if (typeof ancoraBalao === "function") ancoraBalao();
  reportarHit();
}

function chipAnexo(i) {
  const c = document.createElement("span");
  c.className = "anexo-chip " + i.estado;
  c.appendChild(seloAnexo(i));
  const txt = document.createElement("span");
  txt.className = "anexo-txt";
  const n = document.createElement("b");
  n.textContent = i.nome;
  const s = document.createElement("span");
  const tipo = i.tipo === "imagem" ? "imagem" : i.tipo === "pdf" ? (i.paginas ? i.paginas + " pág. · texto" : "PDF") : "texto";
  s.textContent = i.estado === "lendo" ? (i.tipo === "pdf" ? "lendo o PDF…" : "preparando…")
    : [tamanhoLegivel(i.bytes), tipo, tokensLegivel(i.tokens)].filter(Boolean).join(" · ");
  txt.append(n, s);
  c.appendChild(txt);
  const x = document.createElement("button");
  x.className = "anexo-x";
  x.innerHTML = icone("fechar", 13);
  x.title = "Tirar " + i.nome;
  x.setAttribute("aria-label", "Remover " + i.nome);
  x.onclick = () => tiraAnexo(i);
  c.appendChild(x);
  return c;
}

function bannerAnexo(a) {
  const b = document.createElement("div");
  b.className = "anexo-aviso " + a.tom;
  b.setAttribute("role", a.tom === "erro" ? "alert" : "status");
  b.innerHTML = icone("alerta", 17) + '<div class="aa-corpo"><span class="aa-txt"></span><div class="aa-acoes"></div></div>';
  b.querySelector(".aa-txt").innerHTML = a.html; // montado aqui com escHtml em todo texto de fora
  const acoes = b.querySelector(".aa-acoes");
  for (const ac of a.acoes || []) {
    const bt = document.createElement("button");
    bt.className = "aa-bt" + (ac.prim ? " prim" : "");
    bt.textContent = ac.r;
    bt.onclick = () => ac.fn();
    acoes.appendChild(bt);
  }
  if (!(a.acoes || []).length) {
    acoes.remove();
    const x = document.createElement("button");
    x.className = "anexo-x";
    x.innerHTML = icone("fechar", 13);
    x.setAttribute("aria-label", "Dispensar aviso");
    x.onclick = () => dispensaAviso(a);
    b.appendChild(x);
  }
  return b;
}

/* ───────── 📎, colar e arrastar ───────── */

const seletorArquivo = document.getElementById("anexo-arquivo");
if (seletorArquivo) {
  seletorArquivo.onchange = () => {
    adicionarArquivos(seletorArquivo.files);
    seletorArquivo.value = ""; // escolher o mesmo arquivo de novo tem de disparar outra vez
  };
}
ANEXO_BOTOES.forEach((id) => {
  const b = document.getElementById(id);
  if (b) b.onclick = () => { if (seletorArquivo) seletorArquivo.click(); };
});

// Ctrl+V com imagem (print) no campo de texto: vira anexo. Texto colado segue normal.
["txt", "cg-txt"].forEach((id) => {
  const input = document.getElementById(id);
  if (!input) return;
  input.addEventListener("paste", (e) => {
    const arqs = Array.from((e.clipboardData && e.clipboardData.files) || []);
    if (!arqs.length) return;
    e.preventDefault();
    const hora = new Date().toTimeString().slice(0, 5).replace(":", "h");
    adicionarArquivos(arqs.map((f) => (/^image\.(png|jpe?g|gif)$/i.test(f.name)
      ? new File([f], "print-" + hora + f.name.slice(5), { type: f.type }) : f)));
  });
});

// arrastar: a área do balão / do chat grande mostra "Solte para anexar"
function camadaSolta(sup) {
  let s = sup.querySelector(":scope > .solta");
  if (s) return s;
  s = document.createElement("div");
  s.className = "solta oculto";
  s.innerHTML = `<div class="solta-ic">${icone("subir", 28)}</div><b>Solte para anexar</b><span class="solta-qtd"></span>
    <span class="solta-tipos">Imagem · PDF · texto (TXT, CSV, MD, JSON, código) · até ${ANEXO_MAX_MB} MB cada</span>`;
  sup.appendChild(s);
  return s;
}
const temArquivo = (e) => e.dataTransfer && Array.from(e.dataTransfer.types || []).includes("Files");
[document.getElementById("balao"), document.getElementById("chatGrande")].forEach((sup) => {
  if (!sup) return;
  let dentro = 0;
  sup.addEventListener("dragenter", (e) => {
    if (!temArquivo(e)) return;
    e.preventDefault();
    dentro++;
    const s = camadaSolta(sup);
    const n = e.dataTransfer.items ? e.dataTransfer.items.length : 0;
    s.querySelector(".solta-qtd").textContent = n > 1 ? n + " arquivos — entram na próxima mensagem desta conversa" : "Entra na próxima mensagem desta conversa";
    s.classList.remove("oculto");
  });
  sup.addEventListener("dragover", (e) => { if (temArquivo(e)) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; } });
  sup.addEventListener("dragleave", () => { if (--dentro <= 0) { dentro = 0; camadaSolta(sup).classList.add("oculto"); } });
  sup.addEventListener("drop", (e) => {
    if (!temArquivo(e)) return;
    e.preventDefault();
    dentro = 0;
    camadaSolta(sup).classList.add("oculto");
    adicionarArquivos(e.dataTransfer.files);
  });
});
// solto fora do chat: o WebView abriria o arquivo no lugar do boneco — aqui, nada acontece
window.addEventListener("dragover", (e) => { if (temArquivo(e)) e.preventDefault(); });
window.addEventListener("drop", (e) => { if (temArquivo(e)) e.preventDefault(); });
