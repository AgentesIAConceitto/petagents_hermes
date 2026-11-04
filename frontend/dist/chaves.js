/* PetAgent — painel "Chaves grátis" (menu › Sistema › Chaves grátis), 31/10/2026.
   O público do modo grátis não sabe o que é "modelo", "endpoint" ou "API". Então aqui só
   existe o essencial: colar a chave, testar, e ver quantas estão em uso.

   VÁRIAS CHAVES POR PROVEDOR: a cota de cada chave grátis é pequena; o app junta várias e
   distribui a carga (uma de castigo por cota, a próxima assume).
   O MODELO É AUTOMÁTICO — não aparece campo. Quem escolhe é o app, medindo.

   A chave nunca volta do Go em claro: só máscara. Ela só desce ao salvar/testar. */

let painelChaves = null;
const chv = {
  itens: [],          // [{id, nome, quantas, emEspera, chaves:[{rotulo,mascara,emEspera}], pagina, prefixo, ajuda}]
  modo: null,         // ModoAgenteHarness()
  cota: null,         // EstadoCotasHarness()
  bemVindo: false,
  passos: [],
  entrada: {},        // texto digitado por provedor
  status: null,
  ocupado: false,
  avancado: false,
};

function escChv(t) {
  return String(t ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

async function carregaChaves() {
  try { chv.itens = JSON.parse(await G.ChavesHarness()); } catch (e) { chv.itens = []; }
  try { chv.modo = JSON.parse(await G.ModoAgenteHarness()); } catch (e) { chv.modo = null; }
  try { chv.cota = JSON.parse(await G.EstadoCotasHarness()); } catch (e) { chv.cota = null; }
  if (chv.bemVindo && !chv.passos.length) {
    try { chv.passos = JSON.parse(await G.PassosBemVindo()); } catch (e) { chv.passos = []; }
  }
}

// ── roteiro de primeira execução ───────────────────────────────────────────────
function blocoBemVindo() {
  if (!chv.bemVindo || !chv.passos.length) return "";
  return `<div class="chv-bemvindo">
      <p class="chv-bemvindo-titulo">Vamos configurar em 3 passos</p>
      ${chv.passos.map((p) => `<div class="chv-passo">
          <span class="chv-passo-n">${escChv(p.n)}</span>
          <div><b>${escChv(p.titulo)}</b>
            <p class="ap-dica">${escChv(p.texto)}</p>
            ${p.dica ? `<p class="ap-dica chv-dica">${escChv(p.dica)}</p>` : ""}
          </div>
        </div>`).join("")}
    </div>`;
}

// ── aviso de cota (o motor trocou de chave/provedor sozinho) ───────────────────
function blocoCota() {
  const c = chv.cota;
  if (!c || !c.aviso) return "";
  return `<div class="ia-status info">${escChv(c.aviso)}${c.quando ? " (" + escChv(c.quando) + ")" : ""}</div>`;
}

// ── seletor de motor ───────────────────────────────────────────────────────────
function blocoModo() {
  const m = chv.modo;
  if (!m) return "";
  const hermes = m.hermesInstalado
    ? `<span class="chv-ok">✓ Hermes ${escChv(m.hermesVersao)}</span>`
    : `<span class="chv-falta">Hermes não encontrado</span>`;
  const opcoes = [["auto", "Automático"], ["hermes", "Hermes"], ["nativo", "Próprio (grátis)"]];
  const botoes = opcoes.map(([v, r]) =>
    `<button data-modo-motor="${v}" class="${m.escolhido === v ? "sel" : ""}" aria-pressed="${m.escolhido === v}">${r}</button>`).join("");
  const aviso = m.aviso ? `<div class="ia-status info">${escChv(m.aviso).replace(/\n/g, "<br>")}</div>` : "";
  return `<div class="ap-sec"><span>Motor do agente</span>
      <p class="ap-dica">Em uso: <b>${m.efetivo === "hermes" ? "motor do Hermes" : "motor próprio"}</b> · ${hermes}</p>
      <div class="ap-seg">${botoes}</div>
      ${aviso}
    </div>`;
}

// ── um provedor com a lista de chaves dele ─────────────────────────────────────
function blocoProvedor(it) {
  const lista = it.chaves || [];
  const itens = lista.map((k, i) => `<div class="chv-chave ${k.emEspera ? "chv-espera" : ""}">
      <code>${escChv(k.mascara)}</code>
      ${k.emEspera ? `<span class="chv-falta">em espera (cota)</span>` : `<span class="chv-ok">em uso</span>`}
      <button class="ap-link" data-testar="${escChv(it.id)}" data-indice="${i}" ${chv.ocupado ? "disabled" : ""}>Testar</button>
      <button class="ap-link" data-remover="${escChv(it.id)}" data-indice="${i}" ${chv.ocupado ? "disabled" : ""}>Remover</button>
    </div>`).join("");

  const resumo = it.quantas === 0
    ? `<span class="chv-falta">nenhuma chave</span>`
    : `<span class="chv-ok">✓ ${it.quantas} chave(s)</span>${it.emEspera ? ` · <span class="chv-falta">${it.emEspera} em espera</span>` : ""}`;

  return `<div class="chv-item">
      <div class="chv-topo"><b>${escChv(it.nome)}</b> ${resumo}</div>
      <p class="ap-dica">${escChv(it.ajuda)}</p>
      ${itens}
      <div class="chv-linha">
        <input class="chv-input" data-campo="${escChv(it.id)}" type="password"
               placeholder="${escChv(it.prefixo || "cole a chave aqui")}"
               value="${escChv(chv.entrada[it.id] || "")}" />
        <button class="ap-botao" data-salvar="${escChv(it.id)}" ${chv.ocupado ? "disabled" : ""}>Adicionar</button>
        <a class="ap-link" href="${escChv(it.pagina)}" target="_blank" rel="noopener">Criar ↗</a>
      </div>
    </div>`;
}

function pintaChavesInterno() {
  const p = painelChaves;
  if (!p) return;
  const status = chv.status ? `<div class="ia-status ${chv.status.tipo}">${escChv(chv.status.texto)}</div>` : "";
  const total = chv.itens.reduce((s, i) => s + (i.quantas || 0), 0);
  const emEspera = chv.itens.reduce((s, i) => s + (i.emEspera || 0), 0);
  const resumo = total === 0
    ? "Nenhuma chave ainda: o agente não consegue conversar sem uma."
    : `${total} chave(s) configurada(s)${emEspera ? ` · ${emEspera} em espera por cota` : ""}. Quanto mais chaves, mais o agente aguenta.`;

  p.innerHTML = `
    <div class="ap-topo">${icone("inteligencia")}<b>Chaves grátis</b>
      <button class="botao-x" data-fechar aria-label="Fechar">${icone("fechar", 18)}</button></div>
    <p class="ap-dica">${escChv(resumo)}</p>
    ${status}
    ${blocoBemVindo()}
    ${blocoCota()}
    ${blocoModo()}
    ${chv.itens.length ? chv.itens.map(blocoProvedor).join("") : `<div class="ia-status erro">
        Não consegui ler a lista de chaves do aplicativo. ${escChv(chv.diagnostico || "")}
        (se isso aparecer, é falha de comunicação entre tela e programa — me avise)</div>`}
    <div class="ap-sec">
      <button class="ap-link" data-avancado>${chv.avancado ? "▾" : "▸"} Avançado (busca sem chave)</button>
      ${chv.avancado ? `<p class="ap-dica">Se você tiver um SearxNG rodando, o agente busca sem chave nenhuma. Sem isso, ele usa a Wikipédia.</p>
        <div class="chv-linha">
          <input class="chv-input" data-campo="searx" type="text" placeholder="http://127.0.0.1:8888"
                 value="${escChv(chv.searx || "")}" />
          <button class="ap-botao" data-salvar="searx" ${chv.ocupado ? "disabled" : ""}>Salvar</button>
        </div>` : ""}
    </div>`;

  p.querySelector("[data-fechar]").onclick = fecharChaves;
  p.querySelectorAll("[data-campo]").forEach((el) => {
    el.oninput = () => { chv.entrada[el.dataset.campo] = el.value; };
  });
  const av = p.querySelector("[data-avancado]");
  if (av) av.onclick = () => { chv.avancado = !chv.avancado; pintaChaves(); };
  p.querySelectorAll("[data-salvar]").forEach((b) => {
    b.onclick = () => salvaChave(b.dataset.salvar);
  });
  p.querySelectorAll("[data-testar]").forEach((b) => {
    b.onclick = () => testaChave(b.dataset.testar, parseInt(b.dataset.indice, 10));
  });
  p.querySelectorAll("[data-remover]").forEach((b) => {
    b.onclick = () => removeChave(b.dataset.remover, parseInt(b.dataset.indice, 10));
  });
  p.querySelectorAll("[data-modo-motor]").forEach((b) => {
    b.onclick = () => salvaModoMotor(b.dataset.modoMotor);
  });
}

// Invólucro: qualquer erro ao desenhar aparece NA TELA (antes o painel ficava vazio e
// invisível, e não havia como descobrir o motivo em máquina de terceiro).
function pintaChaves() {
  try {
    pintaChavesInterno();
  } catch (e) {
    const p = painelChaves;
    if (p) {
      p.classList.remove("oculto");
      p.innerHTML = `<div class="ap-topo"><b>Chaves grátis</b></div>
        <div class="ia-status erro">Erro ao desenhar a tela: ${escChv(e.message)}</div>
        <div class="ap-dica">${escChv(String(e.stack || "").slice(0, 300))}</div>`;
    }
    try { document.title = "ERRO PAINEL: " + e.message; } catch (x) { }
  }
}

async function salvaChave(id) {
  if (id === "searx") {
    chv.ocupado = true;
    let erro = "";
    try { erro = await G.SalvarSearxHarness((chv.entrada.searx || "").trim()); } catch (e) { erro = String(e); }
    chv.ocupado = false;
    chv.status = erro ? { tipo: "erro", texto: "✗ " + erro } : { tipo: "ok", texto: "✓ Endereço salvo." };
    await carregaChaves();
    pintaChaves();
    return;
  }
  const valor = (chv.entrada[id] || "").trim();
  if (!valor) {
    chv.status = { tipo: "erro", texto: "✗ Cole a chave antes de adicionar." };
    pintaChaves();
    return;
  }
  chv.ocupado = true;
  chv.status = { tipo: "info", texto: "Salvando…" };
  pintaChaves();
  let erro = "";
  try { erro = await G.AdicionarChaveHarness(id, valor); } catch (e) { erro = String(e); }
  chv.ocupado = false;
  if (erro) {
    chv.status = { tipo: "erro", texto: "✗ " + erro };
    pintaChaves();
    return;
  }
  chv.entrada[id] = "";
  if (chv.bemVindo) chv.passos = [];
  await carregaChaves();
  chv.status = { tipo: "ok", texto: "✓ Chave adicionada. Clique em Testar para confirmar que ela vale." };
  pintaChaves();
}

async function testaChave(id, indice) {
  chv.ocupado = true;
  chv.status = { tipo: "info", texto: "Testando com uma chamada real…" };
  pintaChaves();
  let r = "";
  try { r = await G.TestarChaveHarness(id, indice); } catch (e) { r = "erro: " + e; }
  chv.ocupado = false;
  const bom = /válida|funcionando/i.test(r);
  chv.status = { tipo: bom ? "ok" : "erro", texto: (bom ? "✓ " : "✗ ") + r };
  await carregaChaves();
  pintaChaves();
}

async function removeChave(id, indice) {
  chv.ocupado = true;
  let erro = "";
  try { erro = await G.RemoverChaveHarness(id, indice); } catch (e) { erro = String(e); }
  chv.ocupado = false;
  await carregaChaves();
  chv.status = erro ? { tipo: "erro", texto: "✗ " + erro } : { tipo: "info", texto: "Chave removida." };
  pintaChaves();
}

async function salvaModoMotor(modo) {
  chv.ocupado = true;
  let erro = "";
  try { erro = await G.SalvarModoAgenteHarness(modo); } catch (e) { erro = String(e); }
  chv.ocupado = false;
  await carregaChaves();
  chv.status = erro ? { tipo: "erro", texto: "✗ " + erro } : { tipo: "ok", texto: "✓ Motor salvo." };
  pintaChaves();
}

async function abrirChaves(bemVindo) {
  chv.bemVindo = !!bemVindo;
  if (!painelChaves) {
    painelChaves = document.createElement("div");
    painelChaves.className = "aparencia chaves oculto";
    document.body.appendChild(painelChaves);
  }
  try { chv.searx = await G.SearxHarness(); } catch (e) { chv.searx = ""; }
  await carregaChaves();
  painelChaves.classList.remove("oculto");
  chv.status = null;
  pintaChaves();
  if (typeof posicionaPertoDoPet === "function") posicionaPertoDoPet(painelChaves);
  cabeNaTela(painelChaves);
  if (typeof reportarHit === "function") reportarHit();
}

// O painel cresceu (várias chaves por provedor) e o posicionamento ao lado do boneco pode
// jogá-lo para fora da tela. Aqui ele é trazido de volta e ganha rolagem.
function cabeNaTela(el) {
  if (!el) return;
  el.style.maxHeight = Math.floor(window.innerHeight * 0.8) + "px";
  el.style.overflowY = "auto";
  const r = el.getBoundingClientRect();
  let top = r.top, left = r.left;
  if (r.bottom > window.innerHeight - 8) top = Math.max(8, window.innerHeight - r.height - 8);
  if (r.right > window.innerWidth - 8) left = Math.max(8, window.innerWidth - r.width - 8);
  if (r.top < 8) top = 8;
  if (r.left < 8) left = 8;
  el.style.top = top + "px";
  el.style.left = left + "px";
}

function fecharChaves() {
  if (!painelChaves) return;
  painelChaves.classList.add("oculto");
  if (typeof reportarHit === "function") reportarHit();
}

// primeira execução sem chave e sem Hermes: abre o guia sozinho.
// IMPORTANTE: se a checagem com o app falhar, abre DE QUALQUER FORMA e mostra o motivo —
// antes ele saía calado e o usuário via o painel antigo sem entender.
(async function boasVindasChaves() {
  for (let i = 0; i < 60 && !G; i++) await new Promise((r) => setTimeout(r, 500));
  if (!G) return;
  let abre = true;
  chv.diagnostico = "";
  try {
    if (typeof G.PrecisaConfigurarHarness === "function") {
      const bruto = await G.PrecisaConfigurarHarness();
      const p = (typeof bruto === "string") ? JSON.parse(bruto) : bruto;
      abre = !!p.precisa;
      if (!abre) chv.diagnostico = "já existe configuração — painel não abre sozinho";
    } else {
      chv.diagnostico = "o app não expôs PrecisaConfigurarHarness (bindings antigos)";
    }
  } catch (e) {
    chv.diagnostico = "falha ao perguntar ao app: " + e.message;
    abre = true;
  }
  if (abre) {
    setTimeout(() => {
      try { if (typeof fecharInteligencia === "function") fecharInteligencia(); } catch (e) { }
      abrirInteligencia(true);   // uma tela só
    }, 6500);
  }
})();
