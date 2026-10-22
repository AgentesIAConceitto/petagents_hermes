// teste-painel-email.js - o painel de e-mail NAO pode interpretar HTML vindo do e-mail.
//
// Por que existe: remetente, assunto e endereco sao escritos por QUEM MANDA o e-mail.
// O painel montava o cartao com innerHTML direto, entao um remetente chamado
//   <img src=x onerror="...">
// rodava JavaScript dentro do overlay - que enxerga `window.go.main.App.SendChat`,
// ou seja, manda texto para o agente COM ferramentas (terminal). E-mail -> comando.
//
// O que o teste faz: tira `renderEmail` (e os ajudantes dela) do main.js REAL, roda
// num DOM de mentira com um e-mail malicioso e exige que nada vire tag.
//
//   node teste-painel-email.js      (sai 0 = passou, 1 = falhou)
const fs = require("fs");
const path = require("path");
const vm = require("vm");

const fonte = fs.readFileSync(path.join(__dirname, "frontend", "dist", "main.js"), "utf8");

// recorta `function nome(...) { ... }` contando chaves (basta para estas funcoes)
function extrai(nome) {
  const m = new RegExp("(async\\s+)?function\\s+" + nome + "\\s*\\(").exec(fonte);
  if (!m) return "";
  let i = fonte.indexOf("{", m.index), prof = 0;
  for (; i < fonte.length; i++) {
    if (fonte[i] === "{") prof++;
    else if (fonte[i] === "}" && --prof === 0) return fonte.slice(m.index, i + 1);
  }
  throw new Error("funcao sem fim: " + nome);
}

const ATAQUE = '<img src=x onerror="window.go.main.App.SendChat(1)">';
const caixa = {
  conta: ATAQUE,
  verificado_em: "2026-10-13T10:00:00",
  desconhecidos: [{
    de_nome: ATAQUE,
    de_email: "x@ev<img/src/onerror=alert(1)//",
    assunto: ATAQUE,
    nao_lida: true,
  }],
};

const criados = [];
function elemento() {
  const el = { innerHTML: "", textContent: "", className: "", title: "", filhos: [],
    appendChild(f) { this.filhos.push(f); return f; } };
  criados.push(el);
  return el;
}
const fixos = { "email-lista": elemento(), "email-resumo": elemento() };
const ctx = vm.createContext({
  document: { getElementById: (id) => fixos[id], createElement: () => elemento() },
  G: { EmailResumo: async () => JSON.stringify(caixa) },
  reportarHit: () => {},
  marcarEmail: () => {},
});
for (const nome of ["escHtml", "dominioDe", "renderEmail"]) {
  const src = extrai(nome);
  if (src) vm.runInContext(src, ctx);
}

(async () => {
  await vm.runInContext("renderEmail()", ctx);
  const html = criados.map((e) => e.innerHTML).join("\n");
  const falhas = [];
  if (/<img|<script/i.test(html)) falhas.push("HTML do e-mail virou tag de verdade");
  if (!html.includes("&lt;img")) falhas.push("o texto do e-mail sumiu (deveria aparecer escapado)");
  if (falhas.length) {
    console.log("FALHOU:\n  - " + falhas.join("\n  - ") + "\n\nHTML gerado:\n" + html);
    process.exit(1);
  }
  console.log("OK: remetente, assunto, dominio e conta aparecem como texto");
})().catch((e) => { console.log("ERRO: " + e.stack); process.exit(1); });
