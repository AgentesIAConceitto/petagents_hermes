/* PetAgent — painel Inteligência (menu › Sistema › Inteligência), 22/10/2026.
   Escolher provedor, chave e modelo, testar e trocar — sem terminal. A PESSOA escolhe
   onde vale: "igual ao Hermes" (grava no Hermes de verdade: vale para o boneco, o
   Telegram, o terminal) ou "só para o boneco" (o resto do Hermes não muda).
   A lógica e a gravação moram no Go (inteligencia.go); a chave NUNCA volta inteira para
   cá — só mascarada. Ela só vai daqui para o Go ao testar ou salvar.
   Usa o visual do painel Aparência (.aparencia / .ap-*). */

let painelIA = null;
let provedoresIA = [];
const ia = {
  atual: null,          // IAAtual()
  modo: "hermes",
  provedor: "",
  modelo: "",
  chaveNova: "",        // só o que a pessoa digitou agora (nunca vem do Go)
  trocandoChave: false,
  modelos: [],
  status: null,         // { tipo: "ok" | "erro" | "info", texto }
  boasVindas: false,
  ocupado: false,
  // voz (voz.go): a chave do Groq que transcreve o áudio do chat — salva à parte da IA
  voz: { atual: null, nova: "", trocando: false, status: null, ocupado: false },
};

const provAtual = () => provedoresIA.find((p) => p.id === ia.provedor) || null;

// a chave já salva para (modo, provedor) — mascarada
function chaveSalvaIA() {
  const a = ia.atual;
  if (!a) return "";
  if (ia.modo === "boneco") return a.boneco.provedor === ia.provedor ? a.boneco.chave : "";
  return (a.hermes.chaves || {})[ia.provedor] || "";
}

function escIA(t) {
  return String(t ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

async function carregaIA() {
  try {
    if (!provedoresIA.length) provedoresIA = JSON.parse(await G.IAProvedores());
    ia.atual = JSON.parse(await G.IAAtual());
  } catch (e) { ia.atual = null; }
  try { ia.voz.atual = JSON.parse(await G.VozAtual()); } catch (e) { ia.voz.atual = null; }
  ia.voz.nova = "";
  ia.voz.trocando = !(ia.voz.atual && ia.voz.atual.temChave);
  ia.voz.status = null;
  const a = ia.atual;
  if (!a) return;
  ia.modo = a.modo;
  const base = a.modo === "boneco" ? a.boneco : a.hermes;
  ia.provedor = provedoresIA.some((p) => p.id === base.provedor) ? base.provedor : (provedoresIA[0] || {}).id;
  ia.modelo = base.modelo || "";
  ia.chaveNova = "";
  ia.trocandoChave = !chaveSalvaIA();
  ia.status = null;
}

function pintaIA() {
  const p = painelIA;
  if (!p) return;
  const a = ia.atual, pv = provAtual();
  if (!a || !pv) {
    p.innerHTML = `<div class="ap-topo">${icone("inteligencia")}<b>Inteligência</b>
      <button class="botao-x" data-fechar aria-label="Fechar">${icone("fechar", 18)}</button></div>
      <p class="ap-dica">Não consegui ler a configuração agora. Feche e abra de novo.</p>`;
    p.querySelector("[data-fechar]").onclick = fecharInteligencia;
    return;
  }
  const salva = chaveSalvaIA();
  const ef = a.efetivo || {};
  const agora = ef.modelo
    ? `Agora: <b>${escIA(ef.modelo)}</b> · ${escIA(ef.nome || ef.provedor)} · ${ef.modo === "boneco" ? "só do boneco" : "igual ao Hermes"}`
    : "Ainda não há IA configurada.";
  const dicaModo = ia.modo === "hermes"
    ? "Vale para tudo que usa o seu Hermes: o boneco, o Telegram, o terminal."
    : "Só o boneco usa esta IA. O resto do Hermes continua como está.";
  const status = ia.status
    ? `<div class="ia-status ${ia.status.tipo}">${escIA(ia.status.texto)}</div>` : "";
  p.innerHTML = `
    <div class="ap-topo">${icone("inteligencia")}<b>Inteligência</b>
      <button class="botao-x" data-fechar aria-label="Fechar">${icone("fechar", 18)}</button></div>
    ${ia.boasVindas ? `<p class="ia-boas">Oi! Para eu conversar de verdade, preciso de uma IA. Escolha o provedor, cole a sua chave e pronto.</p>` : ""}
    <p class="ap-dica">${agora}</p>
    <div class="ap-sec"><span>Onde vale</span>
      <div class="ap-seg ia-2">${[["hermes", "Igual ao Hermes"], ["boneco", "Só para o boneco"]].map(([v, r]) =>
        `<button data-modo="${v}" class="${ia.modo === v ? "sel" : ""}" aria-pressed="${ia.modo === v}">${r}</button>`).join("")}</div>
      <p class="ia-nota">${dicaModo}</p></div>
    <div class="ap-sec"><span>Provedor</span>
      <select class="ia-campo" data-provedor>${provedoresIA.map((x) =>
        `<option value="${x.id}" ${x.id === ia.provedor ? "selected" : ""}>${escIA(x.nome)}</option>`).join("")}</select></div>
    <div class="ap-sec"><span>Chave</span>
      ${ia.trocandoChave || !salva
        ? `<input class="ia-campo" data-chave type="password" autocomplete="off" spellcheck="false"
             placeholder="${escIA(pv.prefixo ? pv.prefixo + "…" : "cole a chave aqui")}" value="${escIA(ia.chaveNova)}">`
        : `<div class="ia-linha"><code class="ia-mascara">${escIA(salva)}</code>
             <button class="ia-bt sec" data-trocar>Trocar</button></div>`}
      <a href="#" class="ia-link" data-pagina>onde pego a minha chave? ↗</a></div>
    <div class="ap-sec"><span>Modelo</span>
      <input class="ia-campo" data-modelo list="ia-modelos" autocomplete="off" spellcheck="false"
        placeholder="buscar ou digitar o modelo…" value="${escIA(ia.modelo)}">
      <datalist id="ia-modelos">${ia.modelos.slice(0, 600).map((m) => `<option value="${escIA(m)}"></option>`).join("")}</datalist>
      <p class="ia-nota" data-nota-modelos>${notaModelosIA(pv)}</p></div>
    ${status}
    <div class="ia-acoes">
      <button class="ia-bt sec" data-testar ${ia.ocupado ? "disabled" : ""}>Testar acesso</button>
      <button class="ia-bt" data-salvar ${ia.ocupado ? "disabled" : ""}>Salvar</button>
    </div>
    ${secaoVozIA()}`;

  const q = (s) => p.querySelector(s);
  q("[data-fechar]").onclick = fecharInteligencia;
  p.querySelectorAll("[data-modo]").forEach((b) => b.onclick = () => {
    ia.modo = b.dataset.modo; ia.trocandoChave = !chaveSalvaIA(); ia.status = null; pintaIA();
  });
  q("[data-provedor]").onchange = (e) => {
    ia.provedor = e.target.value; ia.modelo = ""; ia.modelos = []; ia.chaveNova = "";
    ia.trocandoChave = !chaveSalvaIA(); ia.status = null; pintaIA(); buscaModelosIA();
  };
  const ch = q("[data-chave]");
  if (ch) ch.oninput = () => { ia.chaveNova = ch.value; };
  if (q("[data-trocar]")) q("[data-trocar]").onclick = () => { ia.trocandoChave = true; pintaIA(); q("[data-chave]").focus(); };
  q("[data-modelo]").oninput = (e) => { ia.modelo = e.target.value; };
  q("[data-pagina]").onclick = (e) => {
    e.preventDefault();
    try { window.runtime.BrowserOpenURL(pv.pagina); } catch (err) { }
  };
  q("[data-testar]").onclick = testaIA;
  q("[data-salvar]").onclick = salvaIA;
  ligaSecaoVozIA(q);
  if (typeof reportarHit === "function") reportarHit();
}

/* ───────── voz: a chave do Groq que transcreve o áudio do 🎤 (voz.js / voz.go) ───────── */

function secaoVozIA() {
  const v = ia.voz, a = v.atual;
  if (!a) return "";
  const status = v.status ? `<div class="ia-status ${v.status.tipo}">${escIA(v.status.texto)}</div>` : "";
  return `
    <div class="ap-sec"><span>Voz — falar pelo microfone do chat</span>
      ${v.trocando || !a.temChave
        ? `<input class="ia-campo" data-voz-chave type="password" autocomplete="off" spellcheck="false"
             placeholder="chave do Groq (gsk_…)" value="${escIA(v.nova)}">`
        : `<div class="ia-linha"><code class="ia-mascara">${escIA(a.chave)}</code>
             <button class="ia-bt sec" data-voz-trocar>Trocar</button></div>`}
      <p class="ia-nota">O áudio vira texto pelo Groq Whisper (conta grátis, ~1 s por frase).
        <a href="#" class="ia-link" data-voz-pagina>criar a minha chave ↗</a></p>
      ${status}
      ${v.trocando || !a.temChave
        ? `<div class="ia-acoes"><button class="ia-bt" data-voz-salvar ${v.ocupado ? "disabled" : ""}>Testar e salvar a voz</button></div>`
        : ""}
    </div>`;
}

function ligaSecaoVozIA(q) {
  const v = ia.voz;
  const ch = q("[data-voz-chave]");
  if (ch) ch.oninput = () => { v.nova = ch.value; };
  if (q("[data-voz-trocar]")) q("[data-voz-trocar]").onclick = () => { v.trocando = true; pintaIA(); q("[data-voz-chave]").focus(); };
  if (q("[data-voz-pagina]")) q("[data-voz-pagina]").onclick = (e) => {
    e.preventDefault();
    try { window.runtime.BrowserOpenURL(v.atual.pagina); } catch (err) { }
  };
  if (q("[data-voz-salvar]")) q("[data-voz-salvar]").onclick = salvaVozIA;
}

async function salvaVozIA() {
  const v = ia.voz;
  if (!v.nova.trim()) { v.status = { tipo: "erro", texto: "Cole a chave do Groq." }; pintaIA(); return; }
  v.ocupado = true; v.status = { tipo: "info", texto: "testando a chave…" }; pintaIA();
  let r = { ok: false, erro: "não consegui salvar" };
  try { r = JSON.parse(await G.VozSalvar(v.nova.trim())); } catch (e) { }
  v.ocupado = false;
  if (!r.ok) { v.status = { tipo: "erro", texto: "✗ " + r.erro }; pintaIA(); return; }
  try { v.atual = JSON.parse(await G.VozAtual()); } catch (e) { }
  v.nova = ""; v.trocando = false;
  v.status = { tipo: "ok", texto: "✓ Voz pronta: clique no microfone do chat e fale." };
  pintaIA();
}

const notaModelosIA = (pv) => ia.modelos.length
  ? ia.modelos.length + " modelos do " + escIA(pv.nome) : "a lista aparece quando a chave funciona";

async function buscaModelosIA() {
  const pv = provAtual();
  if (!pv) return;
  try {
    const r = JSON.parse(await G.IAModelos(pv.id, ia.chaveNova.trim()));
    ia.modelos = r.ok ? (r.modelos || []).sort() : [];
  } catch (e) { ia.modelos = []; }
  // só a lista e a nota: redesenhar o painel tiraria o foco de quem está digitando a chave
  if (!painelIA || painelIA.classList.contains("oculto")) return;
  const dl = painelIA.querySelector("#ia-modelos"), nota = painelIA.querySelector("[data-nota-modelos]");
  if (dl) dl.innerHTML = ia.modelos.slice(0, 600).map((m) => `<option value="${escIA(m)}"></option>`).join("");
  if (nota) nota.innerHTML = notaModelosIA(pv);
}

async function testaIA() {
  const pv = provAtual();
  if (!pv) return;
  ia.ocupado = true; ia.status = { tipo: "info", texto: "testando…" }; pintaIA();
  let r = { ok: false, msg: "não consegui testar" };
  try { r = JSON.parse(await G.IATestar(pv.id, ia.chaveNova.trim())); } catch (e) { }
  ia.ocupado = false;
  ia.status = r.ok
    ? { tipo: "ok", texto: "✓ " + r.msg + (r.saldo && /US\$/.test(r.saldo) ? " · saldo " + r.saldo.replace(".", ",") : "") }
    : { tipo: "erro", texto: "✗ " + r.msg };
  pintaIA();
  if (r.ok) buscaModelosIA();
}

async function salvaIA() {
  const pv = provAtual();
  if (!pv) return;
  if (!ia.modelo.trim()) { ia.status = { tipo: "erro", texto: "Escolha um modelo." }; pintaIA(); return; }
  ia.ocupado = true; ia.status = { tipo: "info", texto: "salvando…" }; pintaIA();
  let r = { ok: false, erro: "não consegui salvar" };
  try {
    r = JSON.parse(await G.IASalvar(JSON.stringify({ modo: ia.modo, provedor: pv.id, modelo: ia.modelo.trim(), chave: ia.chaveNova.trim() })));
  } catch (e) { }
  if (!r.ok) { ia.ocupado = false; ia.status = { tipo: "erro", texto: "✗ " + r.erro }; pintaIA(); return; }
  ia.boasVindas = false;
  ia.status = { tipo: "info", texto: "Salvo. Reiniciando o agente…" };
  pintaIA();
  // a ponte leva ~10 s para subir com a IA nova: acompanha até ela dizer "pronta"
  const t0 = Date.now();
  const espera = setInterval(async () => {
    if (typeof atualizaInfoChat === "function") await atualizaInfoChat();
    const pronta = typeof infoChat !== "undefined" && infoChat.pronta;
    if (pronta || Date.now() - t0 > 90000) {
      clearInterval(espera);
      await carregaIA();
      ia.ocupado = false;
      ia.status = pronta
        ? { tipo: "ok", texto: "✓ Pronto: " + ((ia.atual && ia.atual.efetivo.modelo) || ia.modelo) }
        : { tipo: "erro", texto: "O agente não voltou em 90 s. Vou responder pelo modo lento enquanto isso." };
      pintaIA();
    }
  }, 2000);
}

async function abrirInteligencia(boasVindas) {
  if (!painelIA) {
    painelIA = document.createElement("div");
    painelIA.className = "aparencia ia oculto";
    document.body.appendChild(painelIA);
  }
  ia.boasVindas = !!boasVindas;
  await carregaIA();
  painelIA.classList.remove("oculto");
  pintaIA();
  if (typeof posicionaPertoDoPet === "function") posicionaPertoDoPet(painelIA);
  if (typeof reportarHit === "function") reportarHit();
  buscaModelosIA();
}
function fecharInteligencia() {
  if (!painelIA) return;
  painelIA.classList.add("oculto");
  if (typeof reportarHit === "function") reportarHit();
}

// primeira vez: sem chave de IA nenhuma (nem no Hermes nem do boneco), o painel abre sozinho
(async function boasVindasIA() {
  for (let i = 0; i < 60 && !G; i++) await new Promise((r) => setTimeout(r, 500));
  if (!G || !G.IAAtual) return;
  try {
    const a = JSON.parse(await G.IAAtual());
    if (!a.temChave) setTimeout(() => abrirInteligencia(true), 4000);
  } catch (e) { }
})();
