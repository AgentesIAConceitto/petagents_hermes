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
  // MODO GRÁTIS (03/11/2026): este painel é do sistema antigo e dizia "Nenhuma IA
  // configurada" com três chaves grátis gravadas. Aqui ele passa a saber que existe motor
  // próprio funcionando — senão a tela mente para o usuário.
  try { ia.motor = JSON.parse(await G.ModoMotorAtual()); } catch (e) { ia.motor = null; }
  try { ia.listaChaves = JSON.parse(await G.ChavesHarness()); } catch (e) { ia.listaChaves = []; }
  ia.gratis = { ativo: false, chaves: 0, provedores: [] };
  try {
    const bruto = await G.ChavesHarness();
    const lista = (typeof bruto === "string") ? JSON.parse(bruto) : bruto;
    const comChave = (lista || []).filter((x) => (x.quantas || 0) > 0);
    ia.gratis = { ativo: comChave.length > 0, chaves: comChave.reduce((s, x) => s + (x.quantas || 0), 0),
                  provedores: comChave.map((x) => x.nome) };
  } catch (e) { }
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


// ── a chave única do motor (pedido do dono, 03/11/2026) ─────────────────────────────
// Três opções, uma decisão. O corpo do painel muda conforme a escolha, para não existir
// campo que não vale (era o que deixava a tela confusa: chave de OpenRouter aparecendo
// para quem usa chaves grátis).
function descricaoMotor(m) {
  if (!m) return "";
  if (m.escolha === "hermes") {
    return m.hermesInstalado
      ? "Quem responde é o Hermes instalado, com a IA que ele já usa. Nada a configurar aqui."
      : "O Hermes não foi encontrado nesta máquina. Instale o Hermes ou escolha \"Chaves grátis\".";
  }
  if (m.escolha === "boneco") {
    return "O Hermes roda isolado com a SUA chave: só o boneco usa, o resto do seu Hermes continua como está.";
  }
  if ((m.chavesGratis || 0) > 0) {
    return "Motor próprio do PetAgent, sem Hermes. As chaves abaixo respondem pelo agente.";
  }
  return "Motor próprio do PetAgent, sem Hermes. Falta cadastrar uma chave grátis.";
}

function statusDoMotor() {
  if (!ia.status) return "";
  return `<div class="ia-status ${ia.status.tipo}">${escIA(ia.status.texto)}</div>`;
}

function seletorMotorIA() {
  // Lista vertical em vez de 3 botões lado a lado: com painel estreito, "Integrado ao
  // Hermes" quebrava em duas linhas e virava uma bagunça (reclamação de 04/11/2026).
  // Cada opção já traz a explicação, então some o parágrafo solto embaixo.
  const m = ia.motor || { escolha: "gratis", hermesInstalado: false, chavesGratis: 0 };
  const opcoes = [
    ["hermes", "Hermes", m.hermesInstalado
      ? "usa a IA que o seu Hermes já tem"
      : "indisponível: Hermes não instalado nesta máquina"],
    ["boneco", "Minha chave", "Hermes isolado com a sua chave (só o boneco usa)"],
    ["gratis", "Chaves grátis", (m.chavesGratis || 0) > 0
      ? "motor próprio, sem Hermes · " + m.chavesGratis + " chave(s) de modelo"
      : "motor próprio, sem Hermes · falta cadastrar chave"],
  ];
  const itens = opcoes.map(([v, nome, desc]) => {
    const sel = m.escolha === v;
    return `<button class="ia-motor-opc ${sel ? "sel" : ""}" data-motor="${v}" role="radio" aria-checked="${sel}">
        <span class="ia-motor-marca" aria-hidden="true"></span>
        <span class="ia-motor-texto">
          <b>${escIA(nome)}</b>
          <i>${escIA(desc)}</i>
        </span>
      </button>`;
  }).join("");
  return `<div class="ap-sec"><span>Quem responde o agente</span>
    <div class="ia-motor-lista" role="radiogroup" aria-label="Quem responde o agente">${itens}</div>
    ${statusDoMotor()}</div>`;
}

function corpoMotorIA(pv, salva) {
  const m = ia.motor || { escolha: "gratis" };
  if (m.escolha === "gratis") {
    // UMA TELA SÓ (03/11/2026): as chaves aparecem AQUI dentro. Antes havia um botão que
    // abria outro painel — duas telas para a mesma configuração, que era a confusão.
    const lista = ia.listaChaves || [];
    const blocos = lista.map((it) => {
      const chaves = (it.chaves || []).map((k, i) => `
        <div class="ia-chave-item">
          <code class="ia-chave-codigo">${escIA(k.mascara)}</code>
          <span class="${k.emEspera ? "ia-chave-espera" : "ia-chave-ok"}">${k.emEspera ? "em espera" : "em uso"}</span>
          <span class="ia-chave-acoes">
            <button class="ia-chave-bt" data-ch-test="${escIA(it.id)}" data-i="${i}" ${ia.ocupado ? "disabled" : ""}>Testar</button>
            <button class="ia-chave-bt" data-ch-del="${escIA(it.id)}" data-i="${i}" ${ia.ocupado ? "disabled" : ""}>Remover</button>
          </span>
        </div>`).join("") || `<p class="ia-chave-vazio">nenhuma chave cadastrada</p>`;
      return `<div class="ia-chave-bloco">
        <div class="ia-chave-titulo">${escIA(it.nome)}</div>
        ${chaves}
        <div class="ia-chave-nova">
          <input class="ia-campo" data-ch-campo="${escIA(it.id)}" type="password" autocomplete="off" spellcheck="false"
                 placeholder="${escIA(it.prefixo || "cole a chave aqui")}" value="${escIA((ia.chaveNovaPorProvedor || {})[it.id] || "")}">
          <button class="ia-bt sec" data-ch-add="${escIA(it.id)}" ${ia.ocupado ? "disabled" : ""}>Adicionar</button>
        </div>
        <a href="#" class="ia-link" data-ch-criar="${escIA(it.pagina)}">Criar a minha chave ↗</a>
      </div>`;
    }).join("");
    return `<div class="ap-sec"><span>Chaves grátis</span>
      <p class="ia-nota">Quando uma chave estoura a cota, o agente usa a próxima. Quanto mais chaves, mais ele aguenta.</p></div>
      ${blocos}`;
  }
  if (m.escolha === "hermes") {
    return `<div class="ap-sec"><span>Hermes</span>
      <p class="ia-nota">${m.hermesInstalado
        ? "Hermes encontrado" + (m.hermesVersao ? " · " + escIA(m.hermesVersao) : "") + ". O agente usa a IA dele."
        : "Hermes não encontrado nesta máquina."}</p></div>`;
  }
  // chave própria: aqui sim faz sentido pedir provedor, chave e modelo
  return `<div class="ap-sec"><span>Provedor</span>
      <select class="ia-campo" data-provedor>${provedoresIA.map((x) =>
        `<option value="${x.id}" ${x.id === ia.provedor ? "selected" : ""}>${escIA(x.nome)}</option>`).join("")}</select></div>
    <div class="ap-sec"><span>Chave</span>
      ${ia.trocandoChave || !salva
        ? `<input class="ia-campo" data-chave type="password" autocomplete="off" spellcheck="false"
             placeholder="${escIA(pv.prefixo ? pv.prefixo + "…" : "Cole a chave aqui")}" value="${escIA(ia.chaveNova)}">`
        : `<div class="ia-linha"><code class="ia-mascara">${escIA(salva)}</code>
             <button class="ia-bt sec" data-trocar>Trocar</button></div>`}
      <a href="#" class="ia-link" data-pagina>Como obter a minha chave ↗</a></div>
    <div class="ap-sec"><span>Modelo</span>
      <input class="ia-campo" data-modelo list="ia-modelos" autocomplete="off" spellcheck="false"
        placeholder="Busque ou digite o modelo…" value="${escIA(ia.modelo)}">
      <datalist id="ia-modelos">${ia.modelos.slice(0, 600).map((m2) => `<option value="${escIA(m2)}"></option>`).join("")}</datalist>
      <p class="ia-nota" data-nota-modelos>${notaModelosIA(pv)}</p></div>
    <div class="ia-acoes">
      <button class="ia-bt sec" data-testar ${ia.ocupado ? "disabled" : ""}>Testar acesso</button>
      <button class="ia-bt" data-salvar ${ia.ocupado ? "disabled" : ""}>Salvar</button>
    </div>`;
}

function pintaIA() {
  const p = painelIA;
  if (!p) return;
  const a = ia.atual, pv = provAtual();
  if (!a || !pv) {
    p.innerHTML = `<div class="ap-topo">${icone("inteligencia")}<b>Inteligência</b>
      <button class="botao-x" data-fechar aria-label="Fechar">${icone("fechar", 18)}</button></div>
      <p class="ap-dica">Não foi possível ler a configuração. Feche o painel e abra novamente.</p>`;
    p.querySelector("[data-fechar]").onclick = fecharInteligencia;
    return;
  }
  const salva = chaveSalvaIA();
  const ef = a.efetivo || {};
  const gratis = ia.gratis || { ativo: false, chaves: 0, provedores: [] };
  let agora;
  if (gratis.ativo) {
    // o motor próprio responde o chat. Nada de citar OpenRouter aqui: nesse modo o campo
    // dele nem aparece (era uma das contradições da tela, 04/11/2026).
    agora = `Em uso: <b>motor grátis</b> · ${escIA((gratis.provedores || []).join(", "))}.`;
  } else if (ef.modelo) {
    agora = `Em uso: <b>${escIA(ef.modelo)}</b> · ${escIA(ef.nome || ef.provedor)} · ${ef.modo === "boneco" ? "Só para o boneco" : "Igual ao Hermes"}`;
  } else {
    agora = "Nenhuma IA configurada ainda.";
  }
  const dicaModo = ia.modo === "hermes"
    ? "Vale para tudo o que usa o seu Hermes: o boneco, o Telegram e o terminal."
    : "Só o boneco usa esta IA. O resto do Hermes continua como está.";
  // O status fica DENTRO do bloco de seleção (o dono só o via rolando a tela, 04/11/2026)
  // e desaparece sozinho em 5 s. "info" (testando…) não some: é andamento, não aviso.
  const status = ia.status
    ? `<div class="ia-status ${ia.status.tipo}">${escIA(ia.status.texto)}</div>` : "";
  if (ia.status && (ia.status.tipo === "erro" || ia.status.tipo === "ok")) {
    clearTimeout(ia.statusTimer);
    ia.statusTimer = setTimeout(() => { ia.status = null; pintaIA(); }, 5000);
  }
  p.innerHTML = `
    <div class="ap-topo">${icone("inteligencia")}<b>Inteligência</b>
      <button class="botao-x" data-fechar aria-label="Fechar">${icone("fechar", 18)}</button></div>
    ${ia.boasVindas && !(ia.gratis && ia.gratis.ativo) ? `<p class="ia-boas">Oi! Para conversar de verdade, eu preciso de uma IA. Escolha o provedor e cole a sua chave para começar.</p>` : ""}
    <p class="ap-dica">${agora}</p>
    ${seletorMotorIA()}
    ${corpoMotorIA(pv, salva)}
    ${secaoVozIA()}`;

  const q = (s) => p.querySelector(s);
  q("[data-fechar]").onclick = fecharInteligencia;

  // a chave única do motor: grava e redesenha (o corpo muda com a escolha)
  p.querySelectorAll("[data-motor]").forEach((b) => b.onclick = async () => {
    const escolhido = b.dataset.motor;
    ia.ocupado = true; pintaIA();
    let erro = "";
    try { erro = await G.DefinirModoMotor(escolhido); } catch (e) { erro = String(e); }
    ia.ocupado = false;
    await carregaIA();          // recarrega ANTES: carregaIA() zera ia.status
    // o status entra DEPOIS do recarregamento, senão era apagado e o clique parecia não fazer nada
    if (erro) {
      ia.status = { tipo: "erro", texto: "✗ " + erro };
    } else {
      const m = ia.motor || {};
      ia.status = { tipo: "ok", texto: "✓ Motor definido: " + descricaoMotor(m) };
    }
    pintaIA();
  });
  // chaves grátis, agora dentro deste painel
  p.querySelectorAll("[data-ch-campo]").forEach((el) => {
    el.oninput = () => { ia.chaveNovaPorProvedor = ia.chaveNovaPorProvedor || {}; ia.chaveNovaPorProvedor[el.dataset.chCampo] = el.value; };
  });
  p.querySelectorAll("[data-ch-add]").forEach((b) => b.onclick = async () => {
    const id = b.dataset.chAdd;
    const valor = ((ia.chaveNovaPorProvedor || {})[id] || "").trim();
    if (!valor) { ia.status = { tipo: "erro", texto: "✗ Cole a chave antes de adicionar." }; pintaIA(); return; }
    ia.ocupado = true; pintaIA();
    let erro = "";
    try { erro = await G.AdicionarChaveHarness(id, valor); } catch (e) { erro = String(e); }
    ia.ocupado = false;
    if (!erro) ia.chaveNovaPorProvedor[id] = "";
    await carregaIA();
    ia.status = erro ? { tipo: "erro", texto: "✗ " + erro } : { tipo: "ok", texto: "✓ Chave adicionada. Clique em Testar para confirmar." };
    pintaIA();
  });
  p.querySelectorAll("[data-ch-test]").forEach((b) => b.onclick = async () => {
    ia.ocupado = true; ia.status = { tipo: "info", texto: "Testando com uma chamada real…" }; pintaIA();
    let r = "";
    try { r = await G.TestarChaveHarness(b.dataset.chTest, parseInt(b.dataset.i, 10)); } catch (e) { r = "erro: " + e; }
    ia.ocupado = false;
    const bom = /válida|funcionando/i.test(r);
    ia.status = { tipo: bom ? "ok" : "erro", texto: (bom ? "✓ " : "✗ ") + r };
    pintaIA();
  });
  p.querySelectorAll("[data-ch-del]").forEach((b) => b.onclick = async () => {
    ia.ocupado = true;
    let erro = "";
    try { erro = await G.RemoverChaveHarness(b.dataset.chDel, parseInt(b.dataset.i, 10)); } catch (e) { erro = String(e); }
    ia.ocupado = false;
    await carregaIA();
    ia.status = erro ? { tipo: "erro", texto: "✗ " + erro } : { tipo: "info", texto: "Chave removida." };
    pintaIA();
  });
  p.querySelectorAll("[data-ch-criar]").forEach((a) => a.onclick = (e) => {
    e.preventDefault();
    try { window.runtime.BrowserOpenURL(a.dataset.chCriar); } catch (err) { }
  });

  const prov = q("[data-provedor]");
  if (prov) prov.onchange = (e) => {
    ia.provedor = e.target.value; ia.modelo = ""; ia.modelos = []; ia.chaveNova = "";
    ia.trocandoChave = !chaveSalvaIA(); ia.status = null; pintaIA(); buscaModelosIA();
  };
  const ch = q("[data-chave]");
  if (ch) ch.oninput = () => { ia.chaveNova = ch.value; };
  if (q("[data-trocar]")) q("[data-trocar]").onclick = () => { ia.trocandoChave = true; pintaIA(); if (q("[data-chave]")) q("[data-chave]").focus(); };
  const mo = q("[data-modelo]");
  if (mo) mo.oninput = (e) => { ia.modelo = e.target.value; };
  const pag = q("[data-pagina]");
  if (pag) pag.onclick = (e) => {
    e.preventDefault();
    try { window.runtime.BrowserOpenURL(pv.pagina); } catch (err) { }
  };
  const btTestar = q("[data-testar]");
  if (btTestar) btTestar.onclick = testaIA;
  const btSalvar = q("[data-salvar]");
  if (btSalvar) btSalvar.onclick = salvaIA;
  ligaSecaoVozIA(q);
  if (typeof reportarHit === "function") reportarHit();
}

/* ───────── voz: a chave do Groq que transcreve o áudio do 🎤 (voz.js / voz.go) ───────── */

function secaoVozIA() {
  const v = ia.voz, a = v.atual;
  if (!a) return "";
  const status = v.status ? `<div class="ia-status ${v.status.tipo}">${escIA(v.status.texto)}</div>` : "";
  return `
    <div class="ap-sec"><span>Voz: falar pelo microfone do chat</span>
      ${v.trocando || !a.temChave
        ? `<input class="ia-campo" data-voz-chave type="password" autocomplete="off" spellcheck="false"
             placeholder="Chave do Groq (gsk_…)" value="${escIA(v.nova)}">`
        : `<div class="ia-linha"><code class="ia-mascara">${escIA(a.chave)}</code>
             <button class="ia-bt sec" data-voz-trocar>Trocar</button></div>`}
      <p class="ia-nota">O áudio é transcrito pelo Groq Whisper (conta gratuita, cerca de 1 s por frase).
        <a href="#" class="ia-link" data-voz-pagina>Criar a minha chave ↗</a></p>
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
  v.ocupado = true; v.status = { tipo: "info", texto: "Testando a chave…" }; pintaIA();
  let r = { ok: false, erro: "não foi possível salvar a chave" };
  try { r = JSON.parse(await G.VozSalvar(v.nova.trim())); } catch (e) { }
  v.ocupado = false;
  if (!r.ok) { v.status = { tipo: "erro", texto: "✗ " + fraseUI(r.erro) }; pintaIA(); return; }
  try { v.atual = JSON.parse(await G.VozAtual()); } catch (e) { }
  v.nova = ""; v.trocando = false;
  v.status = { tipo: "ok", texto: "✓ Voz configurada. Clique no microfone do chat e fale." };
  pintaIA();
}

const notaModelosIA = (pv) => ia.modelos.length
  ? ia.modelos.length + " modelos disponíveis · " + escIA(pv.nome) : "A lista de modelos aparece depois que a chave for validada.";

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
  ia.ocupado = true; ia.status = { tipo: "info", texto: "Testando…" }; pintaIA();
  let r = { ok: false, msg: "não foi possível testar o acesso" };
  try { r = JSON.parse(await G.IATestar(pv.id, ia.chaveNova.trim())); } catch (e) { }
  ia.ocupado = false;
  ia.status = r.ok
    ? { tipo: "ok", texto: "✓ " + fraseUI(r.msg) + (r.saldo && /US\$/.test(r.saldo) ? " Saldo: " + r.saldo.replace(".", ",") + "." : "") }
    : { tipo: "erro", texto: "✗ " + fraseUI(r.msg) };
  pintaIA();
  if (r.ok) buscaModelosIA();
}

async function salvaIA() {
  const pv = provAtual();
  if (!pv) return;
  if (!ia.modelo.trim()) { ia.status = { tipo: "erro", texto: "Escolha um modelo." }; pintaIA(); return; }
  ia.ocupado = true;
  // provedor que exige complemento do Hermes (ex.: Anthropic): o Go instala antes de gravar
  ia.status = { tipo: "info", texto: pv.complemento
    ? "Instalando o complemento do " + pv.nome + " no Hermes. Na primeira vez, isso leva cerca de 1 minuto."
    : "Salvando…" };
  pintaIA();
  let r = { ok: false, erro: "não foi possível salvar a configuração" };
  try {
    r = JSON.parse(await G.IASalvar(JSON.stringify({ modo: ia.modo, provedor: pv.id, modelo: ia.modelo.trim(), chave: ia.chaveNova.trim() })));
  } catch (e) { }
  if (!r.ok) { ia.ocupado = false; ia.status = { tipo: "erro", texto: "✗ " + fraseUI(r.erro) }; pintaIA(); return; }
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
      // complemento instalado agora: o Telegram/terminal do Hermes já abertos só o veem ao reiniciar
      const avisoHermes = r.instalou && ia.atual && ia.atual.modo === "hermes"
        ? " O complemento do " + pv.nome + " foi instalado no Hermes. Se o Telegram estiver aberto, reinicie o gateway." : "";
      ia.status = pronta
        ? { tipo: "ok", texto: "✓ Configuração salva. Modelo em uso: " + ((ia.atual && ia.atual.efetivo.modelo) || ia.modelo) + "." + avisoHermes }
        : { tipo: "erro", texto: "✗ O agente não respondeu em 90 s. Enquanto isso, as respostas usarão o modo lento." };
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
  // NÃO NAGUEAR QUEM JÁ CONFIGUROU O MODO GRÁTIS (03/11/2026).
  // Este painel é do sistema antigo (OpenRouter/Hermes). Se as chaves grátis do motor
  // próprio já estão configuradas, abrir isto aqui dizendo "Nenhuma IA configurada ainda"
  // é contraditório — o dono viu exatamente isso com as três chaves gravadas.
  try {
    if (typeof G.PrecisaConfigurarHarness === "function") {
      const bruto = await G.PrecisaConfigurarHarness();
      const p = (typeof bruto === "string") ? JSON.parse(bruto) : bruto;
      if (p && p.precisa === false) return;   // modo grátis configurado: não abrir
    }
  } catch (e) { }
  try {
    const a = JSON.parse(await G.IAAtual());
    if (!a.temChave) setTimeout(() => abrirInteligencia(true), 4000);
  } catch (e) { }
})();
