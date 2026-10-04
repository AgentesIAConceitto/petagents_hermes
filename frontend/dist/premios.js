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

  /* ── A ENTREGA: cartão animado, com a cor e o brilho da raridade ─────────── */
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
      document.body.appendChild(c);
    }
    const linha = res.novo
      ? '<span class="pc-novo">✨ NOVO na coleção!</span>'
      : '<span class="pc-rep">repetido → 🪙 ' + res.moedas + ' moeda' + (res.moedas > 1 ? "s" : "") + '</span>';
    c.className = "premio " + it.raridade + (res.novo ? " novo" : " rep");
    c.innerHTML =
      '<div class="pc-fita" style="background:' + r.cor + '">' + r.nome + "</div>" +
      '<div class="pc-ico">' + it.ico + "</div>" +
      '<div class="pc-nome">' + it.nome + "</div>" +
      linha +
      '<div class="pc-rodape">🪙 ' + res.total + " moedas · coleção " + resumo().qtd + "/" + CATALOGO.length + "</div>";
    c.classList.remove("oculto");
    // tira a classe e repõe: reinicia a animação mesmo em achados seguidos
    void c.offsetWidth;
    c.classList.add("entrando");
    try { explosao(bom ? [it.ico, "✨", "🌟", "💫"] : [it.ico, "✨"], bom ? 10 : 5); } catch (e) { }
    try { reportarHit(); } catch (e) { }
    clearTimeout(cardTimer);
    cardTimer = setTimeout(() => { c.classList.add("oculto"); c.classList.remove("entrando"); try { reportarHit(); } catch (e) { } },
      bom ? 4200 : 2600);
    if (it.raridade === "ultra" || it.raridade === "mitico" || it.raridade === "lendario") {
      try { toast("🌟", r.nome.toUpperCase() + "!", it.ico + " " + it.nome, 4200, true); } catch (e) { }
    }
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

  /* ── a Galeria: moedeiro, progresso, raros em destaque, coleção ─────────── */
  function galeria() {
    const s = resumo();
    let p = document.getElementById("bichoCol");
    if (!p) {
      p = document.createElement("div");
      p.className = "bicho";
      p.id = "bichoCol";
      document.body.appendChild(p);
    }
    const contador = ORDEM.slice().reverse().map((r) => {
      const n = s.porRar[r];
      if (!n) return "";
      return '<span class="gl-cont" style="color:' + RAR[r].cor + '">' + RAR[r].nome + " " + n + "</span>";
    }).join("");
    const raros = s.raros.length
      ? s.raros.map((it) => '<span class="gl-item ' + it.raridade + '" title="' + it.nome + ' — ' + RAR[it.raridade].nome + '" style="--cor:' + RAR[it.raridade].cor + '">' + it.ico + "</span>").join("")
      : '<span class="bicho-est">nenhum ainda — os raros são 1 em cada ~3.226 sorteios…</span>';
    const ids = Object.keys(s.dono).sort((a, b) => (s.dono[b] || 0) - (s.dono[a] || 0));
    const todos = ids.slice(0, 120).map((id) => {
      const it = CATALOGO.find((x) => x.id === id);
      if (!it) return "";
      return '<span class="gl-item ' + it.raridade + '" title="' + it.nome + ' — ' + RAR[it.raridade].nome + '" style="--cor:' + RAR[it.raridade].cor + '">' + it.ico + "</span>";
    }).join("");
    const pct = ((s.qtd / s.total) * 100).toFixed(1);
    p.innerHTML =
      '<div class="bicho-topo"><b>💎 Galeria do ' + (cfgNome() || "Xodó") + '</b>' +
      '<span class="bicho-x" id="col-x">✕</span></div>' +
      '<div class="gl-moeda">🪙 <b>' + s.moedas + "</b> moedas" +
      '<span class="gl-prog">coleção ' + s.qtd + "/" + s.total + " (" + pct + "%)</span></div>" +
      '<div class="gl-raros"><div class="gl-tit">em destaque — raros que eu já tenho</div>' +
      '<div class="gl-grade">' + raros + "</div></div>" +
      '<div class="gl-contadores">' + contador + "</div>" +
      '<div class="gl-tit">tudo o que eu já ganhei</div>' +
      '<div class="gl-grade gl-todos">' + (todos || '<span class="bicho-est">ainda não ganhei nada… os baús aparecem de vez em quando (até 4 por dia)</span>') + "</div>" +
      (s.qtd > 120 ? '<div class="bicho-est">mostrando os 120 mais recentes</div>' : "") +
      '<div class="bicho-linha-bt">' +
      '<button class="bicho-bt" id="col-achar">🎁 procurar um baú</button>' +
      "</div>";
    p.classList.remove("oculto");
    p.querySelector("#col-x").onclick = () => { p.classList.add("oculto"); reportarHit(); };
    p.querySelector("#col-achar").onclick = () => { const r = achar(false); if (!r) toast("🎁", "Já achei 4 hoje!", "volto a procurar amanhã 🙂", 3000); galeria(); };
    const r0 = (window.pet || document.getElementById("pet")).getBoundingClientRect();
    p.style.left = Math.max(8, Math.min(innerWidth - 340, r0.left - 40)) + "px";
    p.style.top = Math.max(8, r0.top - 380) + "px";
    try { reportarHit(); } catch (e) { }
  }

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
