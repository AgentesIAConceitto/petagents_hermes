/* PetAgent — painel Contas de e-mail (painel 📧 › Contas), 22/10/2026.
   A pessoa adiciona Gmail, Hotmail, Microsoft 365, Yahoo ou o e-mail da empresa (IMAP),
   sem precisar de programa de e-mail instalado. A lógica e a gravação moram no Go
   (email_contas.go, email_imap.go, email_oauth.go); a senha só vai daqui para o Go ao
   testar e salvar, e NUNCA volta. Os da Microsoft entram pelo navegador (OAuth): o
   resultado chega pelo evento "email-oauth".
   Usa o visual dos painéis Aparência/Inteligência (.aparencia / .ap-* / .ia-*). */

let painelEC = null;
let provedoresEC = [];
const ec = {
  contas: [],
  prov: "gmail",
  usuario: "",
  senha: "",         // só o que a pessoa digitou agora (nunca vem do Go)
  host: "",
  porta: 993,
  status: null,      // { tipo: "ok" | "erro" | "info", texto }
  ocupado: false,
  esperandoMS: false,
  confirmaRemover: "",
  ouvindo: false,
};

const provEC = () => provedoresEC.find((p) => p.id === ec.prov) || provedoresEC[0] || null;

function escEC(t) {
  return String(t ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

async function carregaEC() {
  try {
    if (!provedoresEC.length) provedoresEC = JSON.parse(await G.EmailProvedores());
    ec.contas = JSON.parse(await G.EmailContas()) || [];
  } catch (e) { ec.contas = []; }
}

function estadoContaEC(c) {
  if (c.estado === "ok") return `<span class="ec-estado ok">Atualizada às ${escEC(String(c.verificado_em || "").slice(11, 16))}</span>`;
  if (c.estado === "precisa_entrar") return `<span class="ec-estado erro">É preciso entrar novamente</span>`;
  if (c.estado === "erro") return `<span class="ec-estado erro" title="${escEC(fraseUI(c.erro))}">Falha na leitura: ${escEC(fraseUI(c.erro))}</span>`;
  return `<span class="ec-estado">Aguardando a primeira leitura</span>`;
}

function pintaEC() {
  const p = painelEC;
  if (!p) return;
  const pv = provEC();
  const lista = ec.contas.length
    ? ec.contas.map((c) => `
      <div class="ec-conta">
        <div class="ec-info"><b title="${escEC(c.usuario)}">${escEC(c.usuario)}</b>
          <span>${escEC(c.provedor_nome)} · ${estadoContaEC(c)}</span></div>
        ${c.auth === "microsoft" && c.estado === "precisa_entrar"
          ? `<button class="ia-bt" data-reentrar="${escEC(c.id)}" ${ec.esperandoMS ? "disabled" : ""}>Entrar de novo</button>` : ""}
        <button class="ia-bt sec" data-remover="${escEC(c.id)}">${ec.confirmaRemover === c.id ? "Confirmar" : "Remover"}</button>
      </div>`).join("")
    : `<p class="ia-nota">Nenhuma conta conectada.</p>`;

  let campos = "";
  if (pv && pv.auth === "senha") {
    campos = `
      <input class="ia-campo" data-usuario type="email" autocomplete="off" spellcheck="false"
        placeholder="Endereço de e-mail" value="${escEC(ec.usuario)}">
      ${pv.id === "outro" ? `
      <div class="ec-2">
        <input class="ia-campo" data-host autocomplete="off" spellcheck="false"
          placeholder="Servidor IMAP (ex.: imap.suaempresa.com.br)" value="${escEC(ec.host)}">
        <input class="ia-campo" data-porta type="number" min="1" max="65535" value="${escEC(ec.porta)}" title="porta (993 = IMAP com TLS)">
      </div>` : ""}
      <input class="ia-campo" data-senha type="password" autocomplete="off" spellcheck="false"
        placeholder="${pv.id === "outro" ? "Senha" : "Senha de app"}" value="${escEC(ec.senha)}">
      <div class="ia-acoes"><button class="ia-bt" data-salvar ${ec.ocupado ? "disabled" : ""}>Testar e salvar</button></div>`;
  } else if (pv && pv.auth === "microsoft") {
    campos = pv.disponivel
      ? `<div class="ia-acoes"><button class="ia-bt" data-ms ${ec.esperandoMS ? "disabled" : ""}>Entrar com a Microsoft</button></div>`
      : `<div class="ia-status erro">${escEC(pv.motivo)}</div>`;
  }
  const status = ec.status ? `<div class="ia-status ${ec.status.tipo}">${escEC(ec.status.texto)}</div>` : "";

  p.innerHTML = `
    <div class="ap-topo">${icone("email")}<b>Contas de e-mail</b>
      <button class="botao-x" data-fechar aria-label="Fechar">${icone("fechar", 18)}</button></div>
    <p class="ap-dica">Somente leitura: nenhuma mensagem é apagada, movida ou marcada como lida. Os e-mails importantes são resumidos pela IA configurada em Inteligência.</p>
    <div class="ap-sec"><span>Suas contas</span>${lista}</div>
    <div class="ap-sec"><span>Adicionar conta</span>
      <select class="ia-campo" data-prov>${provedoresEC.map((x) =>
        `<option value="${escEC(x.id)}" ${x.id === ec.prov ? "selected" : ""}>${escEC(x.nome)}${x.disponivel ? "" : " (indisponível)"}</option>`).join("")}</select>
      ${pv ? `<p class="ia-nota">${escEC(pv.ajuda)}</p>` : ""}
      ${pv && pv.link_ajuda ? `<a href="#" class="ia-link" data-ajuda>Como criar a senha de app ↗</a>` : ""}
      ${campos}
    </div>
    ${status}`;

  const q = (s) => p.querySelector(s);
  q("[data-fechar]").onclick = fecharContasEmail;
  q("[data-prov]").onchange = (e) => {
    ec.prov = e.target.value; ec.senha = ""; ec.status = null; ec.esperandoMS = false; pintaEC();
  };
  const liga = (sel, campo) => { const el = q(sel); if (el) el.oninput = () => { ec[campo] = el.value; }; };
  liga("[data-usuario]", "usuario"); liga("[data-senha]", "senha"); liga("[data-host]", "host"); liga("[data-porta]", "porta");
  const senhaEl = q("[data-senha]");
  if (senhaEl) senhaEl.onkeydown = (e) => { if (e.key === "Enter") salvaContaEC(); };
  if (q("[data-ajuda]")) q("[data-ajuda]").onclick = (e) => {
    e.preventDefault();
    try { window.runtime.BrowserOpenURL(pv.link_ajuda); } catch (err) { }
  };
  if (q("[data-salvar]")) q("[data-salvar]").onclick = salvaContaEC;
  if (q("[data-ms]")) q("[data-ms]").onclick = () => entrarMicrosoftEC(pv.id, "");
  p.querySelectorAll("[data-reentrar]").forEach((b) => b.onclick = () => entrarMicrosoftEC("", b.dataset.reentrar));
  p.querySelectorAll("[data-remover]").forEach((b) => b.onclick = () => removeContaEC(b.dataset.remover));
  if (typeof reportarHit === "function") reportarHit();
}

async function salvaContaEC() {
  const pv = provEC();
  if (!pv || ec.ocupado) return;
  ec.ocupado = true; ec.status = { tipo: "info", texto: "Conectando e lendo a caixa de entrada…" }; pintaEC();
  let r = { ok: false, erro: "não consegui salvar" };
  try {
    r = JSON.parse(await G.EmailAdicionarConta(JSON.stringify({
      provedor: pv.id, usuario: ec.usuario.trim(), senha: ec.senha, host: ec.host.trim(), porta: Number(ec.porta) || 993,
    })));
  } catch (e) { }
  ec.ocupado = false;
  if (!r.ok) { ec.status = { tipo: "erro", texto: "✗ " + fraseUI(r.erro) }; pintaEC(); return; }
  ec.status = { tipo: "ok", texto: "✓ Conta " + ec.usuario.trim() + " conectada. " + r.mensagens + " mensagens lidas." };
  ec.usuario = ""; ec.senha = ""; ec.host = "";
  await carregaEC();
  pintaEC();
  atualizaPainelEmailEC();
}

async function entrarMicrosoftEC(provedorID, contaID) {
  ligaOuvinteEC();
  ec.esperandoMS = true;
  ec.status = { tipo: "info", texto: "O login da Microsoft foi aberto no navegador. Conclua o acesso e volte para esta tela." };
  pintaEC();
  let r = { ok: false, erro: "não consegui abrir o login" };
  try { r = JSON.parse(await G.EmailEntrarMicrosoft(provedorID, contaID)); } catch (e) { }
  if (!r.ok) { ec.esperandoMS = false; ec.status = { tipo: "erro", texto: "✗ " + fraseUI(r.erro) }; pintaEC(); }
}

// o login da Microsoft termina no navegador: o Go avisa por evento
function ligaOuvinteEC() {
  if (ec.ouvindo || !window.runtime || !window.runtime.EventsOn) return;
  ec.ouvindo = true;
  window.runtime.EventsOn("email-oauth", async (d) => {
    ec.esperandoMS = false;
    ec.status = d && d.ok
      ? { tipo: "ok", texto: "✓ Conta " + d.conta + " conectada." }
      : { tipo: "erro", texto: "✗ " + fraseUI((d && d.erro) || "o login não foi concluído") };
    await carregaEC();
    pintaEC();
    if (d && d.ok) atualizaPainelEmailEC();
  });
}

async function removeContaEC(id) {
  if (ec.confirmaRemover !== id) { ec.confirmaRemover = id; pintaEC(); return; }
  ec.confirmaRemover = "";
  let r = { ok: false, erro: "não consegui remover" };
  try { r = JSON.parse(await G.EmailRemoverConta(id)); } catch (e) { }
  ec.status = r.ok ? { tipo: "ok", texto: "✓ Conta removida. A senha guardada também foi apagada." } : { tipo: "erro", texto: "✗ " + fraseUI(r.erro) };
  await carregaEC();
  pintaEC();
  atualizaPainelEmailEC();
}

// o painel 📧 aberto ao lado mostra a mudança na hora
function atualizaPainelEmailEC() {
  const ep = document.getElementById("email");
  if (ep && !ep.classList.contains("oculto") && typeof renderEmail === "function") renderEmail();
}

async function abrirContasEmail() {
  if (!painelEC) {
    painelEC = document.createElement("div");
    painelEC.className = "aparencia ec oculto";
    document.body.appendChild(painelEC);
  }
  ligaOuvinteEC();
  ec.status = null; ec.confirmaRemover = "";
  await carregaEC();
  painelEC.classList.remove("oculto");
  pintaEC();
  if (typeof posicionaPertoDoPet === "function") posicionaPertoDoPet(painelEC);
  if (typeof reportarHit === "function") reportarHit();
}

function fecharContasEmail() {
  if (!painelEC) return;
  painelEC.classList.add("oculto");
  if (typeof reportarHit === "function") reportarHit();
}
