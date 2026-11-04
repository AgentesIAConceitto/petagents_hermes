/* PetAgent — painel Agendamentos (menu › Sistema › Agendamentos), 30/10/2026.
   Mostra o que o agente marcou para rodar depois: quando roda, quantas vezes já rodou, se
   falhou e por quê. Permite cancelar e criar na mão.
   O estado mora no Go (harness_agenda.go / harness_agendador_app.go); aqui é só a tela. */

let painelAgenda = null;
const agd = {
  itens: [],
  estado: null,     // EstadoAgendador(): { rodando, aviso }
  novoPedido: "",
  novoQuando: "todo dia 09:00",
  status: null,
  ocupado: false,
};

function escAgd(t) {
  return String(t ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

async function carregaAgenda() {
  try { agd.itens = JSON.parse(await G.AgendamentosHarness()); } catch (e) { agd.itens = []; }
  try { agd.estado = JSON.parse(await G.EstadoAgendador()); } catch (e) { agd.estado = null; }
}

function linhaAgenda(it) {
  const cancelado = it.estado !== "ativo";
  const erro = it.ultimoErro
    ? `<p class="ap-dica agd-erro">último erro: ${escAgd(it.ultimoErro)}</p>` : "";
  const rodou = it.execucoes > 0 ? ` · rodou ${it.execucoes}x` : "";
  return `<div class="agd-item ${cancelado ? "agd-off" : ""}">
      <div class="agd-topo">
        <b>${escAgd(it.proxima)}</b>
        <span class="ap-dica">${escAgd(it.regra)}${rodou}</span>
      </div>
      <p class="agd-pedido">${escAgd(it.pedido)}</p>
      ${erro}
      <div class="agd-acoes">
        <span class="ap-dica">${cancelado ? "cancelado" : "ativo"} · ${escAgd(it.id)}</span>
        ${cancelado ? "" : `<button class="ap-link" data-cancelar="${escAgd(it.id)}">Cancelar</button>`}
      </div>
    </div>`;
}

function pintaAgenda() {
  const p = painelAgenda;
  if (!p) return;
  const status = agd.status ? `<div class="ia-status ${agd.status.tipo}">${escAgd(agd.status.texto)}</div>` : "";
  const roda = agd.estado
    ? (agd.estado.rodando
        ? `<span class="chv-ok">✓ agendador ativo</span>`
        : `<span class="chv-falta">agendador parado</span>${agd.estado.aviso ? " — " + escAgd(agd.estado.aviso) : ""}`)
    : "";
  const lista = agd.itens.length
    ? agd.itens.map(linhaAgenda).join("")
    : `<p class="ap-dica">Nada agendado ainda. Você pode pedir no chat: "me lembre amanhã às 9h de revisar o PRD".</p>`;
  p.innerHTML = `
    <div class="ap-topo">${icone("inteligencia")}<b>Agendamentos</b>
      <button class="botao-x" data-fechar aria-label="Fechar">${icone("fechar", 18)}</button></div>
    <p class="ap-dica">${roda}</p>
    ${status}
    ${lista}
    <div class="ap-sec"><span>Criar na mão</span>
      <div class="agd-linha">
        <input class="chv-input" data-novo="pedido" type="text" placeholder="O que fazer (ex.: revisar o PRD do projeto X)"
               value="${escAgd(agd.novoPedido)}" />
      </div>
      <div class="agd-linha">
        <input class="chv-input" data-novo="quando" type="text" placeholder="todo dia 09:00 · a cada 2h · uma vez 31/10 09:00"
               value="${escAgd(agd.novoQuando)}" />
        <button class="ap-botao" data-criar ${agd.ocupado ? "disabled" : ""}>Agendar</button>
      </div>
    </div>`;

  p.querySelector("[data-fechar]").onclick = fecharAgenda;
  p.querySelectorAll("[data-novo]").forEach((el) => {
    el.oninput = () => { agd[el.dataset.novo === "pedido" ? "novoPedido" : "novoQuando"] = el.value; };
  });
  p.querySelectorAll("[data-cancelar]").forEach((b) => {
    b.onclick = () => cancelaAgenda(b.dataset.cancelar);
  });
  const criar = p.querySelector("[data-criar]");
  if (criar) criar.onclick = criaAgenda;
}

async function criaAgenda() {
  const pedido = (agd.novoPedido || "").trim();
  const quando = (agd.novoQuando || "").trim();
  if (!pedido) {
    agd.status = { tipo: "erro", texto: "✗ Escreva o que o agente deve fazer." };
    pintaAgenda();
    return;
  }
  agd.ocupado = true;
  let r = "";
  try { r = await G.AgendarHarness(pedido, quando); } catch (e) { r = "erro: " + e; }
  agd.ocupado = false;
  const bom = String(r).startsWith("ok");
  agd.status = { tipo: bom ? "ok" : "erro", texto: (bom ? "✓ " : "✗ ") + r };
  if (bom) agd.novoPedido = "";
  await carregaAgenda();
  pintaAgenda();
}

async function cancelaAgenda(id) {
  let erro = "";
  try { erro = await G.CancelarAgendamentoHarness(id); } catch (e) { erro = String(e); }
  await carregaAgenda();
  agd.status = erro ? { tipo: "erro", texto: "✗ " + erro } : { tipo: "info", texto: "Agendamento cancelado." };
  pintaAgenda();
}

async function abrirAgenda() {
  if (!painelAgenda) {
    painelAgenda = document.createElement("div");
    painelAgenda.className = "aparencia agenda oculto";
    document.body.appendChild(painelAgenda);
  }
  await carregaAgenda();
  painelAgenda.classList.remove("oculto");
  agd.status = null;
  pintaAgenda();
  if (typeof posicionaPertoDoPet === "function") posicionaPertoDoPet(painelAgenda);
  if (typeof reportarHit === "function") reportarHit();
}

function fecharAgenda() {
  if (!painelAgenda) return;
  painelAgenda.classList.add("oculto");
  if (typeof reportarHit === "function") reportarHit();
}
