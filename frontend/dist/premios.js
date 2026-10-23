/* ────────────────────────────────────────────────────────────────────────────
   PRÊMIOS — os presentes do bicho (galeria + moedeiro).

   Regras do dono (04/10/2026):
     1. ~2000 presentes DIFERENTES, "de tudo um pouco" (boné, mochila, chapéu,
        sabre de luz, espada viking, itens históricos…) — criatividade livre.
     2. Sorteio TOTALMENTE aleatório, mas raridade faz o item ser raro:
        alguns com 0,031% de chance.
     3. Item raro ganho fica EM DESTAQUE na galeria.
     4. Repetido → vira MOEDA. Exercício também dá moeda. (Trocar moeda por
        algo fica para uma versão futura.)

   COMO o 0,031% é garantido (isto é o coração do arquivo): o sorteio é em DOIS
   ESTÁGIOS — primeiro sorteia a RARIDADE pelos pesos da tabela (que somam 100%),
   depois sorteia o item uniformemente DENTRO daquela raridade. Assim, ter 960
   itens comuns e 12 ultra-raros NÃO muda a chance do ultra: ela é exatamente o
   peso dele (0,031% = 1 em ~3.226).

   Carregado ANTES do bicho.js. Usa do main.js: toast() · explosao() · reportarHit()
   Exporta: window.premiosAchar / premiosGaleria / premiosMoedas / premiosSortear /
            premiosCatalogo / premiosInfo / premiosRender
   ──────────────────────────────────────────────────────────────────────────── */
(() => {
  "use strict";

  const CHAVE = "bicho.premios";

  /* ── tabela de raridade: os pesos somam 100% ─────────────────────────────── */
  const RAR = {
    comum:    { nome: "Comum",      pct: 60.000, moedas: 1,    cor: "#9aa4b2" },
    incomum:  { nome: "Incomum",    pct: 25.000, moedas: 3,    cor: "#4ade80" },
    raro:     { nome: "Raro",       pct: 10.000, moedas: 10,   cor: "#38bdf8" },
    epico:    { nome: "Épico",      pct: 4.000,  moedas: 40,   cor: "#a78bfa" },
    lendario: { nome: "Lendário",   pct: 0.900,  moedas: 200,  cor: "#fbbf24" },
    mitico:   { nome: "Mítico",     pct: 0.069,  moedas: 900,  cor: "#f472b6" },
    ultra:    { nome: "ULTRA-RARO", pct: 0.031,  moedas: 5000, cor: "#f43f5e" },
  };
  const ORDEM = ["comum", "incomum", "raro", "epico", "lendario", "mitico", "ultra"];
  const RARO_DE_VERDADE = ["raro", "epico", "lendario", "mitico", "ultra"]; // destaque na galeria

  /* ── bases: "emoji nome" (o emoji é o primeiro pedaço) ───────────────────── */
  const BASES = {
    comum: [
      "🧢 boné", "🎒 mochila", "👕 camiseta", "🧦 meia", "👟 tênis", "🩴 chinelo",
      "🧴 protetor solar", "☕ caneca", "🥤 copo", "🍫 chocolate", "🍬 bala",
      "🍪 biscoito", "🍭 pirulito", "🧃 suco", "🍎 maçã", "🍌 banana",
      "🥪 sanduíche", "🍕 fatia de pizza", "🌭 cachorro-quente", "🍿 pipoca",
      "🥨 pretzel", "🪥 escova de dentes", "🧼 sabonete", "🧽 esponja", "🪣 balde",
      "🧹 vassoura", "🧺 cesta", "🪑 cadeira", "🛋 almofada", "🖊 caneta",
      "✏️ lápis", "🖍 giz de cera", "📏 régua", "📐 esquadro", "📎 clipe",
      "📌 tachinha", "🗂 pasta", "📔 caderno", "📕 livro", "📰 jornal",
      "🧩 quebra-cabeça", "🎲 dado", "🃏 baralho", "♟ peão de xadrez", "🪁 pipa",
      "🎈 balão", "🪃 bumerangue", "🧸 ursinho", "⚽ bola", "🏀 bola de basquete",
      "🏐 bola de vôlei", "🏓 raquete", "🏸 peteca", "🪀 ioiô", "🥁 tambor",
      "🎤 microfone", "🎸 violão", "🧭 bússola", "🔦 lanterna", "🕯 vela",
      "🪛 chave de fenda", "🔧 chave inglesa", "🔨 martelo", "🪚 serrote",
      "🧰 caixa de ferramentas", "🪴 plantinha", "🌻 girassol", "🌵 cactinho",
      "🍀 trevo", "🐚 concha", "🪨 pedra bonita", "🎃 abóbora", "🥕 cenoura",
      "🌽 milho", "🍇 uva", "🍓 morango", "🥑 abacate", "🧊 cubo de gelo",
    ],
    incomum: [
      "🧢 boné de couro", "🎒 mochila impermeável", "👒 chapéu de palha",
      "🕶 óculos escuros", "🧣 cachecol de lã", "🧤 luvas de couro",
      "👞 sapato social", "👔 gravata de seda", "🥾 bota de trilha",
      "⌚ relógio de pulso", "💍 anel de prata", "📿 colar de contas",
      "🎩 cartola", "🖋 caneta-tinteiro", "📓 diário de capa dura", "🔭 luneta",
      "🎯 alvo de dardos", "🎱 bola 8", "🀄 jogo de mahjong", "🎼 partitura antiga",
      "🪗 sanfona", "🎺 trompete", "🎷 saxofone", "🪕 banjo", "🧷 alfinete de ouro",
      "🗝 chave antiga", "⚖ balança de latão", "🕰 relógio de pêndulo",
      "🛡 escudo de madeira", "⚔ espada de treino", "🏹 arco de bambu",
      "🪓 machadinha", "🛶 canoa de madeira", "🐴 ferradura", "🧲 ímã forte",
      "🔮 bola de vidro", "🪔 lamparina", "🧿 olho grego", "📜 pergaminho",
      "🗺 mapa antigo", "🧵 novelo de linha", "🪡 agulha de cetim", "🪆 boneca russa",
      "🎎 bonecas japonesas", "🪅 pinhata", "🎏 bandeirinha", "🧧 envelope da sorte",
      "🪭 leque de seda", "🍯 pote de mel",
    ],
    raro: [
      "💡 sabre de luz de brinquedo", "⚔ espada de aço temperado", "🏹 arco composto",
      "🛡 escudo de aço", "🪖 capacete de piloto", "🥽 óculos de aviador",
      "🎧 fone de estúdio", "📷 câmera analógica", "🎥 projetor de cinema 8mm",
      "🕹 fliperama de bolso", "🎮 joystick arcade", "📼 videocassete",
      "💾 disquete de 1985", "📟 pager", "☎ telefone de disco", "📻 rádio valvulado",
      "🧭 bússola de latão", "🔭 telescópio refletor", "🧪 kit de química",
      "⚗ alambique de cobre", "🛰 maquete de satélite", "🚁 drone pequeno",
      "🛴 patinete elétrico", "🎿 esquis antigos", "🏄 prancha de surf",
      "🥋 faixa preta de judô", "🎽 camisa de time histórico", "🪖 capacete militar",
      "🗿 máscara de pedra", "🪘 tambor cerimonial",
    ],
    epico: [
      "⚔ espada viking de Drakkar", "🪓 machado de guerra viking",
      "🛡 escudo redondo nórdico", "🪖 elmo de samurai (kabuto)", "🗡 katana forjada",
      "⚔ florete de duelo", "🏹 arco longo de teixo", "🔱 tridente de bronze",
      "🪄 cajado de carvalho", "🧙 chapéu de mago", "🔮 orbe de cristal",
      "🧝 arco élfico", "🛡 escudo de dragão", "🐉 ovo de dragão petrificado",
      "🦴 fêmur de gigante", "🪶 pena de grifo", "🧿 talismã antigo",
      "🗿 moai de pedra", "🏺 ânfora romana", "⚱ sarcófago em miniatura",
    ],
    lendario: [
      "👑 Coroa de Carlos Magno", "🎭 Máscara de Tutancâmon",
      "🗡 Excalibur, a réplica sagrada", "🏆 Cálice do Torneio",
      "🛡 Escudo de Aquiles", "🪖 Elmo de Alexandre", "📜 Manuscrito de Alexandria",
      "🏺 Ânfora de Pompeia", "💎 Diamante do Rajá", "🪙 Moeda de César",
      "🕰 Relógio de Versalhes", "🗿 Moai do Pacífico",
    ],
    mitico: [
      "🔨 Mjölnir, o martelo de Thor", "🔱 Tridente de Poseidon",
      "⚡ Raio de Zeus", "🪄 Varinha de Merlin", "🛡 Égide de Atena",
      "🏹 Arco de Ártemis", "💎 Pedra Filosofal", "🔥 Fênix de âmbar",
    ],
    // 12 peças ÚNICAS: o topo da coleção (0,031% cada sorteio de raridade ultra)
    ultra: [
      "🗡 Sabre de Luz do Primeiro Mestre", "🔨 Mjölnir, o Martelo Esquecido",
      "🏆 O Graal Verdadeiro", "👑 Coroa do Primeiro Rei", "💎 Coração do Mundo",
      "🗝 A Chave de Atlantis", "🕯 A Lâmpada de Aladim", "🧿 O Olho da Eternidade",
      "⚗ O Elixir Dourado", "🪶 A Pena da Fênix Eterna", "⌛ A Ampulheta do Tempo",
      "⚔ A Lâmina de Ragnarök",
    ],
  };

  /* ── cores: o MESMO tipo em cor diferente é item DIFERENTE (pedido do dono,
     04/10/2026). Só cores de gênero INVARIÁVEL ("azul", "verde", "cinza", "rosa",
     "lilás", "vinho"…) — assim "boné azul" e "mochila azul" concordam sozinhos,
     sem eu precisar saber o gênero de cada uma das 209 bases. */
  const CORES = ["azul", "verde", "cinza", "rosa", "lilás", "vinho", "turquesa",
                 "âmbar", "jade", "marfim"];
  const CORES_RARAS = ["azul-cobalto", "verde-esmeralda", "violeta", "carmim"];
  // peças NOMEADAS não levam cor nem variante: são únicas, e o nome é a graça
  const SEM_VARIANTE = { lendario: 1, mitico: 1, ultra: 1 };
  /* ── adjetivos por raridade (poucos agora: a variedade vem das cores) ─────── */
  const ADJ = {
    comum: ["usado", "antigo"],
    incomum: ["de couro", "artesanal"],
    raro: ["vintage", "com runas"],
    epico: ["ancestral", "de guerra"],
    lendario: [],
    mitico: [],
    ultra: [],
  };

  /* ── catálogo (montado uma vez) ──────────────────────────────────────────── */
  let CATALOGO = null;
  let POR_RAR = null;
  function monta() {
    if (CATALOGO) return;
    CATALOGO = [];
    POR_RAR = {};
    for (const rar of ORDEM) {
      const lista = [];
      const cores = (rar === "raro" || rar === "epico")
        ? CORES.concat(CORES_RARAS)
        : CORES;
      (BASES[rar] || []).forEach((base, ib) => {
        const corte = base.indexOf(" ");
        const ico = base.slice(0, corte);
        const nome = base.slice(corte + 1);
        // sufixos: o item puro ("") + as CORES + os adjetivos da raridade.
        // Peça nomeada não ganha nenhum: é uma só — rara pelo peso, não pela cor.
        const sufixos = SEM_VARIANTE[rar]
          ? [""]
          : [""].concat(cores.map((c) => " " + c))
                  .concat((ADJ[rar] || []).map((a) => " " + a));
        sufixos.forEach((v, iv) => {
          const item = {
            id: rar + ":" + ib + ":" + iv,
            ico: ico,
            nome: nome + v,
            raridade: rar,
            moedas: RAR[rar].moedas,
          };
          CATALOGO.push(item);
          lista.push(item);
        });
      });
      POR_RAR[rar] = lista;
    }
  }
  monta();

  /* ── estado: {dono:{id:quando}, moedas:n, achadoDia:{dia,n}} ─────────────── */
  const padrao = () => ({ dono: {}, moedas: 0, achadoDia: { dia: "", n: 0 }, totalGanho: 0 });
  function estado() {
    try { return Object.assign(padrao(), JSON.parse(localStorage.getItem(CHAVE) || "{}")); }
    catch (e) { return padrao(); }
  }
  function salva(e) { try { localStorage.setItem(CHAVE, JSON.stringify(e)); } catch (err) { } }

  /* ── SORTEIO: raridade por peso, depois item uniforme dentro dela ────────── */
  function sortear() {
    let r = Math.random() * 100, acc = 0, tier = "comum";
    for (const t of ORDEM) {
      acc += RAR[t].pct;
      if (r < acc) { tier = t; break; }
    }
    const lista = POR_RAR[tier];
    return lista[Math.floor(Math.random() * lista.length)];
  }

  /* ── ganhar: novo entra na coleção; repetido vira moeda ─────────────────── */
  function ganhar(item) {
    const e = estado();
    if (e.dono[item.id]) {
      const m = RAR[item.raridade].moedas;
      e.moedas += m;
      salva(e);
      return { item: item, novo: false, moedas: m, total: e.moedas };
    }
    e.dono[item.id] = Date.now();
    e.totalGanho = (e.totalGanho || 0) + 1;
    salva(e);
    return { item: item, novo: true, moedas: 0, total: e.moedas };
  }

  function porMoedas(n) {
    const e = estado();
    e.moedas += n;
    salva(e);
    return e.moedas;
  }

  function resumo() {
    const e = estado();
    const dono = e.dono || {};
    const porRar = {};
    ORDEM.forEach((r) => { porRar[r] = 0; });
    const raros = [];
    Object.keys(dono).forEach((id) => {
      const it = CATALOGO.find((x) => x.id === id);
      if (!it) return;
      porRar[it.raridade] += 1;
      if (raridadeBoa(it.raridade)) raros.push(it);
    });
    const ordemRar = (a, b) => ORDEM.indexOf(b.raridade) - ORDEM.indexOf(a.raridade);
    raros.sort(ordemRar);
    return {
      moedas: e.moedas || 0,
      dono: dono,
      qtd: Object.keys(dono).length,
      total: CATALOGO.length,
      porRar: porRar,
      raros: raros,
    };
  }
  function raridadeBoa(r) { return RARO_DE_VERDADE.indexOf(r) >= 0; }

  /* ── A ENTREGA: cartão com a cor e o brilho da raridade ─────────────────────
     22/10/2026 (prancha "Presente achado"): nasce EM CIMA DO BONECO (não mais no meio
     da tela, interrompendo o trabalho), mostra a fita da raridade, o item grande e um
     selo "novo na coleção" / "repetido · +N moedas". Clicar nele abre a Coleção. */
  let cardTimer = null;
  function revelar(res) {
    const it = res.item;
    const r = RAR[it.raridade];
    const bom = raridadeBoa(it.raridade);
    let c = document.getElementById("premioCard");
    if (!c) {
      c = document.createElement("div");
      c.id = "premioCard";
      c.className = "premio oculto";
      c.setAttribute("role", "status");
      c.onclick = () => { c.classList.add("oculto"); try { reportarHit(); } catch (e) { } galeria(); };
      document.body.appendChild(c);
    }
    const selo = res.novo
      ? '<span class="pc-selo novo">Novo na coleção</span>'
      : '<span class="pc-selo rep">Repetido · +' + res.moedas + " moeda" + (res.moedas > 1 ? "s" : "") + "</span>";
    c.className = "premio " + it.raridade + (bom ? " brilha" : "");
    c.style.setProperty("--cor", r.cor);
    c.innerHTML =
      '<span class="pc-fita">' + r.nome + "</span>" +
      '<span class="pc-ico">' + it.ico + "</span>" +
      '<span class="pc-nome">' + maiuscula(it.nome) + "</span>" + selo +
      '<span class="pc-rodape">' + res.total + (res.total === 1 ? " moeda" : " moedas") + " · Coleção: " + resumo().qtd + " de " + CATALOGO.length.toLocaleString("pt-BR") + "</span>";
    c.classList.remove("oculto");
    if (typeof posicionaPertoDoPet === "function") posicionaPertoDoPet(c);
    // tira a classe e repõe: reinicia a animação mesmo em achados seguidos
    c.classList.remove("entrando");
    void c.offsetWidth;
    c.classList.add("entrando");
    try { explosao(bom ? [it.ico, "✨", "🌟", "💫"] : [it.ico, "✨"], bom ? 10 : 5); } catch (e) { }
    try { reportarHit(); } catch (e) { }
    clearTimeout(cardTimer);
    cardTimer = setTimeout(() => { c.classList.add("oculto"); c.classList.remove("entrando"); try { reportarHit(); } catch (e) { } },
      bom ? 5000 : 3000);
  }

  /* ── o achado do dia (mesma regra de antes: até 4 por dia) ───────────────── */
  function achar(ignoraLimite) {
    const e = estado();
    const h = new Date().toISOString().slice(0, 10);
    if (!e.achadoDia || e.achadoDia.dia !== h) e.achadoDia = { dia: h, n: 0 };
    if (!ignoraLimite && e.achadoDia.n >= 4) return null;
    if (!ignoraLimite) e.achadoDia.n += 1;
    salva(e);
    const res = ganhar(sortear());
    revelar(res);
    return res;
  }

  /* ── a Coleção: moedas, progresso, filtro por raridade, raros em destaque ────
     22/10/2026 (prancha "Coleção"). Os itens continuam emoji: eles SÃO o presente. */
  let filtroRar = "todos";
  function galeria() {
    const s = resumo();
    let p = document.getElementById("bichoCol");
    if (!p) {
      p = document.createElement("div");
      p.className = "bicho col";
      p.id = "bichoCol";
      document.body.appendChild(p);
    }
    const e = estado();
    const hoje = new Date().toISOString().slice(0, 10);
    const achadosHoje = e.achadoDia && e.achadoDia.dia === hoje ? e.achadoDia.n : 0;
    const nome = cfgNome() || "Xodó";
    const pct = (s.qtd / s.total) * 100;
    const chip = (k, rotulo, cor, n) =>
      '<button class="col-chip' + (filtroRar === k ? " sel" : "") + '" data-rar="' + k + '" style="--cor:' + cor + '" aria-pressed="' + (filtroRar === k) + '">' +
      '<i></i>' + rotulo + " <span>" + n + "</span></button>";
    const chips = chip("todos", "Todos", "var(--acento)", s.qtd) +
      ORDEM.slice().reverse().filter((r) => s.porRar[r]).map((r) => chip(r, RAR[r].nome, RAR[r].cor, s.porRar[r])).join("");
    const item = (it) => '<span class="col-item ' + it.raridade + '" title="' + maiuscula(it.nome) + " (" + RAR[it.raridade].nome + ")" +
      '" style="--cor:' + RAR[it.raridade].cor + '">' + it.ico + "</span>";
    const raros = s.raros.slice(0, 8).map(item).join("");
    const ids = Object.keys(s.dono).sort((a, b) => (s.dono[b] || 0) - (s.dono[a] || 0));
    const itens = ids.map((id) => CATALOGO.find((x) => x.id === id))
      .filter((it) => it && (filtroRar === "todos" || it.raridade === filtroRar));
    const titulo = filtroRar === "todos" ? "Tudo o que " + nome + " já ganhou" : "Itens da categoria " + RAR[filtroRar].nome;
    p.innerHTML =
      '<div class="pp-topo"><span class="pp-quem"><b>Coleção de ' + nome + "</b><span>Presentes encontrados nos baús</span></span>" +
      '<span class="col-moedas" title="Moedas">' + (typeof icone === "function" ? icone("moeda", 15) : "") + s.moedas + " moedas</span>" +
      '<button class="botao-x" id="col-x" aria-label="Fechar">' + (typeof icone === "function" ? icone("fechar", 18) : "✕") + "</button></div>" +
      '<div class="col-prog"><span><span>Coleção</span><span><b>' + s.qtd + "</b> de " + s.total.toLocaleString("pt-BR") + " · " +
      pct.toFixed(1).replace(".", ",") + '%</span></span><i><b style="width:' + Math.max(1.5, pct) + '%"></b></i></div>' +
      '<div class="col-chips">' + chips + "</div>" +
      (raros ? '<div class="col-raros"><span>Raros em destaque</span><div>' + raros + "</div></div>" : "") +
      '<div class="col-sec">' + titulo + "</div>" +
      '<div class="col-grade">' + (itens.slice(0, 160).map(item).join("") ||
        '<span class="pn-nota">Nenhum presente ainda. Os baús aparecem de vez em quando, até 4 por dia.</span>') + "</div>" +
      '<div class="col-rodape"><span>Baús hoje: ' + achadosHoje + " de 4 · Repetidos viram moedas</span>" +
      '<button class="pn-bt" id="col-achar">Procurar um baú</button></div>';
    p.classList.remove("oculto");
    p.querySelector("#col-x").onclick = () => { p.classList.add("oculto"); reportarHit(); };
    p.querySelectorAll("[data-rar]").forEach((b) => { b.onclick = () => { filtroRar = b.dataset.rar; galeria(); }; });
    p.querySelector("#col-achar").onclick = () => {
      const r = achar(false);
      if (!r) toast("colecao", "Já achei 4 baús hoje!", "Volto a procurar amanhã.", 3000, true);
      galeria();
    };
    if (typeof posicionaPertoDoPet === "function") {
      posicionaPertoDoPet(p);   // encostada no boneco, como os outros painéis (main.js)
    } else {
      const r0 = (window.pet || document.getElementById("pet")).getBoundingClientRect();
      p.style.left = Math.max(8, Math.min(innerWidth - 340, r0.left - 40)) + "px";
      p.style.top = Math.max(8, r0.top - 380) + "px";
    }
    try { reportarHit(); } catch (e) { }
  }

  // nome do item com inicial maiúscula para rótulos (o catálogo fica em minúsculas: ele também entra no meio das falas)
  const maiuscula = (t) => { t = String(t || ""); return t.charAt(0).toUpperCase() + t.slice(1); };
  const cfgNome = () => (window.bicho && window.bicho.cfg && window.bicho.cfg.nome) || "Xodó";

  /* ── ponte com o bicho.js ────────────────────────────────────────────────── */
  window.premiosAchar = achar;
  window.premiosGaleria = galeria;
  window.premiosMoedas = porMoedas;
  window.premiosSortear = sortear;
  window.premiosCatalogo = () => CATALOGO;
  window.premiosInfo = resumo;
  window.premiosRaridade = (r) => RAR[r];
})();
