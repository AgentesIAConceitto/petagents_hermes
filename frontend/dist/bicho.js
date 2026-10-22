/* ────────────────────────────────────────────────────────────────────────────
   BICHO — o comportamento "tamagotchi" do PetAgent.
   Regras de ouro:
     1. Ele NUNCA morre. As necessidades chegam a 0 e param ali.
     2. Pedidos são de leve: intervalo ALEATÓRIO (não é metralhadora de balão).
     3. Fora do horário de trabalho ele quase não fala; no modo foco, só água.
   Este arquivo é carregado DEPOIS do main.js e usa as funções dele:
     msg(texto, quem) · abrirBalao() · pet · menu
   ──────────────────────────────────────────────────────────────────────────── */
(() => {
  "use strict";

  const CHAVE_CFG = "bicho.config";
  const CHAVE_EST = "bicho.estado";
  const MIN = 60 * 1000;

  const CFG_PADRAO = {
    nome: "Xodó",
    trabIni: 8,        // hora do dia
    trabFim: 18,
    // ── agenda biológica (04/10/2026) ─────────────────────────────────────────
    // ⚠️ REGRA DURA DO DONO (04/10/2026): o PC fica ligado 24/7, então o horário
    // de trabalho que ele informa é o ÚNICO recorte válido — TODO aviso (água,
    // comida, caminhada, atenção) acontece dentro dele, e nada fora. Por isso os
    // horários de refeição NÃO são fixos no relógio: são DERIVADOS do expediente
    // (ver eventosDoDia). Quem trabalha 7h-17h não pode receber aviso de jantar
    // das 19h30.
    aguaCadaMin: 120,        // água de 2 em 2 h, do começo ao fim do expediente
    caminhadaCadaMin: 90,    // levantar/caminhar a cada 1h30
    atencaoCadaMin: 180,     // carinho/atenção a cada 3 h
    cafeDepoisIniMin: 30,    // café da manhã: 30 min depois de começar
    refeicaoAntesFimMin: 45, // última refeição: 45 min antes de terminar
    folgaMax: 45,      // evento atrasado mais que isto não vale (PC estava desligado)
    intervaloMin: 6,   // distância mínima entre dois balões
    tetoDia: 20,       // rede de segurança: a agenda cheia tem ~17 avisos/dia. Com 12
                       // eu cortava o fim do dia — o JANTAR nunca chegava (medido)
    passeioMin: 6,
    passeioMax: 22,
    maxVoltasDia: 2,   // no máximo duas voltas completas por dia
    dormirAte: 0,      // timestamp (0 = acordado)
    foco: false,
  };

  const EST_PADRAO = {
    fome: 80, sede: 80, carinho: 80, exercicio: 80,
    proxPasseio: 0, voltaHoje: 0, dia: "",
    ultimo: 0,
    // agenda: o que já foi avisado hoje, quando e quantos
    feitos: {}, ultimoAviso: 0, avisosHoje: 0, pendente: {},
  };

  // quanto cada necessidade cai por minuto — AGORA casado com a agenda:
  // a barra chega baixa exatamente quando o aviso daquela necessidade chega.
  const DECAIMENTO = {
    sede: 1 / 2,       // 100 em 2 h   → aviso de água
    fome: 1 / 5,       // 100 em 5 h   → refeições/lanches
    carinho: 1 / 3,    // 100 em 3 h   → aviso de atenção
    exercicio: 1 / 2.5, // 100 em 2h30 → caminhadas
  };

  const ler = (chave, padrao) => {
    try {
      const bruto = JSON.parse(localStorage.getItem(chave) || "null");
      return Object.assign({}, padrao, bruto || {});
    } catch (e) { return Object.assign({}, padrao); }
  };
  const gravar = (chave, valor) => {
    try { localStorage.setItem(chave, JSON.stringify(valor)); } catch (e) { /* sem storage */ }
  };

  let cfg = ler(CHAVE_CFG, CFG_PADRAO);
  let est = ler(CHAVE_EST, EST_PADRAO);
  const salvar = () => { gravar(CHAVE_CFG, cfg); gravar(CHAVE_EST, est); };

  const agora = () => Date.now();
  const entre = (a, b) => a + Math.random() * (b - a);
  const hora = () => new Date().getHours();
  const hoje = () => new Date().toISOString().slice(0, 10);

  const estaDormindo = () => cfg.dormirAte > agora();
  const estaEmFoco = () => !!cfg.foco;
  const emExpediente = () => {
    const h = hora();
    return cfg.trabIni <= cfg.trabFim
      ? (h >= cfg.trabIni && h < cfg.trabFim)
      : (h >= cfg.trabIni || h < cfg.trabFim); // turno que cruza a meia-noite
  };

  // ── recado do bicho: balãozinho (toast), SEM abrir o chat ──────────────────
  // Regra do dono (03/10/2026): só o tile "Conversar" e as respostas do agente
  // abrem a conversa. Pedido/carinho/achado do bicho é RECADO — não sequestra o chat.
  function fala(texto) {
    try {
      // prova externa: todo recado do bicho fica também no petagent.log (canal G.Log
      // que o front já tem para o Go). Acabou o "será que o balão saiu?".
      try {
        if (window.go && window.go.main && window.go.main.App && window.go.main.App.Log) {
          window.go.main.App.Log("bicho fala: " + texto);
        }
      } catch (e) { }
      if (window.petBipe) window.petBipe();      // sininho suave (pedido do dono)
      if (typeof toast === "function") toast("🐾", cfg.nome || "Bicho", texto, 7000, true);
      else if (typeof msg === "function") msg(texto, "ele");
    } catch (e) { /* nunca deixa o bicho derrubar o app */ }
  }
  // ── esconder/mostrar o boneco ───────────────────────────────────────────────
  function sumir(sim) {
    try {
      if (!pet) return;
      pet.classList.toggle("escondido", sim);
      if (typeof reportarHit === "function") reportarHit();
    } catch (e) { /* ignora */ }
  }

  function dormir(horas) {
    cfg.dormirAte = agora() + Math.round(horas * 60) * MIN;
    salvar();
    fala(`Boa noite! Vou dormir por ${horas} hora${horas > 1 ? "s" : ""} 😴`);
    setTimeout(() => sumir(true), 1200);
    pintaPainel();
  }

  function acordar() {
    cfg.dormirAte = 0;
    salvar();
    sumir(false);
    fala("Bom dia! Dormi bem. 💤");
    pintaPainel();
  }

  // ── ações ───────────────────────────────────────────────────────────────────
  // alimentar/água/exercício moraram aqui e viraram DUPLICATA do bloco 2: os
  // tiles da linha anexada abriam o balão sem nenhuma reação visível no boneco.
  // Removidas em 03/10/2026 — a única implementação agora é
  // window.bichoAlimentar / bichoAgua / bichoExercicio (bloco 2), que os tiles usam.
  function carinho(quanto = 30) {
    est.carinho = Math.min(100, est.carinho + quanto); salvar(); pintaPainel();
    // pedido do dono (04/10/2026): carinho também toca o sininho. Precisa ser AQUI
    // e não só no fala(): o duplo clique vem com quanto=0 e não passa pelo fala(),
    // então ficaria mudo. O antirrajada do petBipe evita toque duplo quando o
    // carinho grande também fala.
    if (window.petBipe) window.petBipe();
    if (quanto >= 25) fala("Aaaah, carinho! 🥰");
  }
  function alternarFoco() {
    cfg.foco = !cfg.foco; salvar(); pintaPainel();
    fala(cfg.foco
      ? "Modo foco ligado 🎯 — só te lembro da água e fico quietinho."
      : "Modo foco desligado. Pode falar comigo. 🙂");
  }

  // ── movimento ───────────────────────────────────────────────────────────────
  // ⚠️ 13/10/2026: o passeio andava aqui mexendo só no `left` — numa borda lateral o
  // boneco saía andando para o meio da tela, descolado da borda. Quem sabe em que
  // borda ele está é o main.js: window.petCaminhar anda AO LONGO dela (e cuida da
  // linha do sprite e do sentido, que já eram de lá — window.petAndar).
  // 📌 parado (padrão desde 13/10/2026, pedido do dono): fica onde foi solto — sem
  // passeio nem volta sozinho. petParado() mora no main.js (o `cfg` de lá).
  const parado = () => typeof petParado === "function" && petParado();
  function passeio() {
    // durante a volta na tela o passeio fica de fora: ele mexeria no left/top do
    // boneco e a volta deixaria de terminar no lugar de origem
    if (parado() || estaDormindo() || !pet || pet.classList.contains("oculto") || pet.classList.contains("volta")) return;
    if (window.petCaminhar) window.petCaminhar(entre(-320, 320), entre(3000, 6500));
  }
  /* corta um passeio em andamento: a volta na tela precisa da pista livre, senão o
     passeio continua mexendo no left/top por baixo dela */
  function pararAndar() {
    try { if (window.petPararCaminhada) window.petPararCaminhada(); } catch (e) { }
  }
  function voltaCompleta() {
    if (parado() || estaDormindo() || !pet || pet.classList.contains("volta")) return;
    // ⚠️ a volta vem com recado ("você já fez o seu cárdio?") = pedido. Fora do
    // expediente o bicho anda pela tela, mas não cobra exercício de ninguém.
    if (!emExpediente()) return;
    if (est.dia !== hoje()) { est.dia = hoje(); est.voltaHoje = 0; }
    if (est.voltaHoje >= cfg.maxVoltasDia) return;
    est.voltaHoje += 1; salvar();
    pet.classList.add("volta");             // keyframe: contorna a tela
    fala("Já fiz meu cárdio, você já fez o seu? 🏃\u200d♂️");
    setTimeout(() => pet.classList.remove("volta"), 22000);
  }

  // ── AGENDA BIOLÓGICA ───────────────────────────────────────────────────────
  // O que ele fala em cada tipo de aviso. Ele pede pra si E cutuca o dono: é essa
  // dupla leitura que faz o bicho virar assistente.
  // ⚠️ Nada de hora escrita na frase ("das 15h", "passou das 19h"): a agenda é
  // derivada do expediente do dono, então a frase tem de servir em qualquer turno.
  const FRASES = {
    agua: ["💧 Hora da água! Eu já bebi a minha — e você?",
           "💧 Bora encher o copo? Já faz um tempinho…",
           "Sede! 💧 Bebe uma água comigo?",
           "💧 Pausa pro copo d'água — eu espero você."],
    caminhada: ["🚶 Levanta e estica as pernas comigo (2 min bastam)",
                "Minhas patinhas pedem movimento — e as suas costas também 👀",
                "🚶 Que tal uma voltinha? A gente volta rapidinho",
                "Pausa de caminhada! Levanta da cadeira e vai 🚶‍♂️"],
    cafe: ["☕ Começando o dia! Café da manhã na mesa?",
           "☕ Acordei com fome — e você, já tomou café?"],
    almoco: ["🍲 Metade do dia: hora de comer de verdade, não de beliscar",
             "🍲 Tô com uma fome de almoço… você também, né?"],
    jantar: ["🍽 Fim de expediente chegando… bora comer algo de verdade?",
             "🍽 Hora da última refeição do dia! Meu pratinho e o seu 👀"],
    lancheM: ["🍎 Meio da manhã: uma fruta cai bem",
              "🍎 Belisquei minha ração… e você, um lanchinho?"],
    lancheT: ["🥪 Um lanchinho leve agora? (eu aceito migalhas 🐾)",
              "🥪 Aquela fominha da tarde chegou — pra nós dois"],
    atencao: ["🥰 Um carinho rapidinho?",
              "Ei, olha pra mim um instante 🐾",
              "🥰 Tô aqui do seu lado — só queria atenção",
              "Um cafuné e eu volto a trabalhar feliz 🥰"],
  };
  // quem manda quando dois avisos caem perto: refeição > lanche > água > caminhada > atenção
  const PESO = { refeicao: 5, lanche: 4, agua: 3, caminhada: 2, atencao: 1 };
  const minHora = (m) => String(Math.floor(m / 60)).padStart(2, "0") + ":" + String(m % 60).padStart(2, "0");

  // eventosDoDia — a agenda de HOJE (pura: não toca em estado nem em DOM).
  // TUDO derivado do expediente informado; nada fora dele. Horas fixas proibidas:
  // o mesmo código tem de servir para quem trabalha 7h-17h e para quem trabalha
  // 13h-22h — e o bicho tem de ficar calado fora da janela.
  function eventosDoDia(c, trab) {
    const ev = [];
    const ini = trab.ini * 60, fim = trab.fim * 60;
    // turno em minutos, com o caso do turno que CRUZA a meia-noite (22h→6h)
    const turno = fim > ini ? (fim - ini) : ((1440 - ini) + fim);
    // ⚠️ a regra do dono, num lugar só: nada fora do expediente entra na agenda
    const dentro = (m) => (fim > ini ? (m >= ini && m <= fim) : (m >= ini || m <= fim));
    const add = (min, tipo, frases) => {
      const m = ((Math.round(min) % 1440) + 1440) % 1440;   // volta pro relógio do dia
      if (dentro(m)) ev.push({ min: m, tipo: tipo, frases: frases });
    };
    for (let m = ini; m <= ini + turno; m += c.aguaCadaMin) add(m, "agua", FRASES.agua);
    for (let m = ini; m < ini + turno; m += c.caminhadaCadaMin) add(m, "caminhada", FRASES.caminhada);
    for (let m = ini + 60; m <= ini + turno; m += c.atencaoCadaMin) add(m, "atencao", FRASES.atencao);
    // refeições derivadas: começo, 1/4, metade, 3/4 e fim do expediente
    add(ini + c.cafeDepoisIniMin, "refeicao", FRASES.cafe);
    add(ini + turno * 0.25, "lanche", FRASES.lancheM);
    add(ini + turno * 0.5, "refeicao", FRASES.almoco);
    add(ini + turno * 0.75, "lanche", FRASES.lancheT);
    add(ini + turno - c.refeicaoAntesFimMin, "refeicao", FRASES.jantar);
    // dois avisos em cima um do outro (±20 min) viram um só: o mais pesado fica.
    // ⚠️ A ORDEM IMPORTA: ordenar por minuto primeiro fazia o leve (que caiu 15 min
    // antes) roubar a vez do pesado — medido: a caminhada das 17:00 engolia o
    // JANTAR das 17:15. Por isso quem manda é o peso, e só depois o horário.
    const ordem = ev.slice().sort((a, b) =>
      ((PESO[b.tipo] || 0) - (PESO[a.tipo] || 0)) || (a.min - b.min));
    const fica = [];
    for (const e of ordem) {
      if (fica.some((f) => Math.abs(f.min - e.min) <= 20)) continue;
      fica.push(e);
    }
    return fica.sort((a, b) => a.min - b.min);
  }

  const chaveEv = (e) => e.tipo + "@" + e.min;
  // ⚠️ `sorteia` NÃO existe neste bloco (ele vive no bloco 2, lá embaixo). Usar o
  // dele aqui era ReferenceError silencioso: o catch do tique engolia e o bicho
  // simplesmente não avisava nada (medido em 04/10/2026 no harness).
  const escolhe = (l) => l[Math.floor(Math.random() * l.length)];

  // decide — PURA: dado o estado e a hora (minutos do dia), diz QUAL aviso sai
  // agora (ou null). O tique só executa; é isso que permite eu SIMULAR o dia.
  function decide(estado, minAgora, c, trab) {
    if ((estado.avisosHoje || 0) >= c.tetoDia) return null;
    if (estado.ultimoAviso && minAgora - estado.ultimoAviso < c.intervaloMin) return null;
    const feitos = estado.feitos || {};
    const cands = eventosDoDia(c, trab)
      .filter((e) => !feitos[chaveEv(e)])
      .filter((e) => e.min <= minAgora && (minAgora - e.min) <= c.folgaMax);
    if (!cands.length) return null;
    cands.sort((a, b) => ((PESO[b.tipo] || 0) - (PESO[a.tipo] || 0)) || (a.min - b.min));
    return cands[0];
  }

  // ── ganchos de MEDIÇÃO (rodam no harness headless, não tocam em DOM) ───────
  // bichoAgenda(cfg)     → a agenda do dia (horários + tipos)
  // bichoSimulaDia(cfg, {de, ate, foco}) → simula o dia minuto a minuto e devolve
  //                        a lista de avisos que SAIRIAM (mesma decisão do tique)
  window.bichoPasso = tique;   // MEDIÇÃO: roda o passo (aceita hora injetada)
  window.bichoAgenda = (c) => {
    const cc = c || cfg;
    return eventosDoDia(cc, { ini: cc.trabIni, fim: cc.trabFim })
      .map((e) => ({ min: e.min, hora: minHora(e.min), tipo: e.tipo }));
  };
  window.bichoSimulaDia = (c, opts) => {
    opts = opts || {};
    const cc = c || cfg;
    const trab = { ini: cc.trabIni, fim: cc.trabFim };
    const st = { feitos: {}, avisosHoje: 0, ultimoAviso: 0 };
    const avisos = [];
    const de = opts.de || 0;
    const ate = opts.ate === undefined ? 1439 : opts.ate;
    for (let m = de; m <= ate; m++) {
      const ev = decide(st, m, cc, trab);
      if (ev && (!opts.foco || ev.tipo === "agua")) {
        avisos.push({ min: ev.min, hora: minHora(ev.min), tipo: ev.tipo });
        st.feitos[chaveEv(ev)] = 1;
        st.ultimoAviso = m;
        st.avisosHoje++;
      }
    }
    return avisos;
  };

  // passeio (o andar aleatório pela tela) é independente da agenda biológica
  function agendaProximoPasseio() {
    est.proxPasseio = agora() + entre(cfg.passeioMin, cfg.passeioMax) * MIN;
  }
  // registra que o aviso daquela necessidade saiu agora (para o auto-cuidado)
  function pend(necessidade, min) {
    est.pendente = est.pendente || {};
    est.pendente[necessidade] = min;
  }

  // tique — o passo de verdade do bicho (roda a cada 20 s e no boot).
  // `minFake`/`minutosFake` existem para MEDIÇÃO: injetam a hora do dia (minutos) e
  // os minutos acumulados, para testar o caminho de produção sem esperar o relógio.
  // (O estado vive em MEMÓRIA; mexer no localStorage por fora não afeta o tique.)
  let tiqueAvisouErro = false;
  function tique(minFake, minutosFake) {
    try {
      const t = agora();
      const minutos = (minutosFake !== undefined && minutosFake !== null)
        ? minutosFake
        : (est.ultimo ? Math.min(10, (t - est.ultimo) / MIN) : 0.4);
      est.ultimo = t;

      // sono
      if (cfg.dormirAte && cfg.dormirAte <= t) { acordar(); }
      if (estaDormindo()) { salvar(); pintaPainel(); return; }

      // necessidades caem devagar, parando em 0 (ele nunca morre)
      for (const k of Object.keys(DECAIMENTO)) {
        est[k] = Math.max(0, est[k] - DECAIMENTO[k] * minutos);
      }

      // ── AGENDA DO DIA (04/10/2026): avisa no horário, um aviso por vez ──────
      if (est.dia !== hoje()) {
        est.dia = hoje(); est.voltaHoje = 0;
        est.feitos = {}; est.avisosHoje = 0; est.ultimoAviso = 0; est.pendente = {};
      }
      // ⚠️ estado vindo de versão anterior (ou corrompido) pode não ter estes campos.
      // `feitos` nulo faria `est.feitos[chave] = 1` estourar, e o catch engoliria:
      // a agenda morreria CALADA. Blindagem barata, aprendeu-se na marra.
      est.feitos = est.feitos || {}; est.pendente = est.pendente || {};
      est.avisosHoje = est.avisosHoje || 0; est.ultimoAviso = est.ultimoAviso || 0;
      const d = new Date();
      const agoraMin = (minFake === undefined || minFake === null)
        ? d.getHours() * 60 + d.getMinutes()
        : minFake;
      const ev = decide(est, agoraMin, cfg, { ini: cfg.trabIni, fim: cfg.trabFim });
      if (ev && (!estaEmFoco() || ev.tipo === "agua")) {   // no foco, só água
        fala(escolhe(ev.frases));
        est.feitos[chaveEv(ev)] = 1;
        est.ultimoAviso = agoraMin;
        est.avisosHoje = (est.avisosHoje || 0) + 1;
        // o aviso BAIXA a barra correspondente: a barra passa a mostrar a
        // necessidade que ele acabou de pedir, e volta a subir quando o dono atende
        if (ev.tipo === "agua") { est.sede = Math.min(est.sede, 22); pend("sede", agoraMin); }
        if (ev.tipo === "refeicao") { est.fome = Math.min(est.fome, 18); pend("fome", agoraMin); }
        if (ev.tipo === "lanche") { est.fome = Math.min(est.fome, 35); pend("fome", agoraMin); }
        if (ev.tipo === "caminhada") { est.exercicio = Math.min(est.exercicio, 25); pend("exercicio", agoraMin); }
        if (ev.tipo === "atencao") { est.carinho = Math.min(est.carinho, 25); pend("carinho", agoraMin); }
      }
      // pedido antigo sem resposta: o bicho se cuida sozinho e a barra se recupera
      // até 55 — ele NUNCA morre, e não fica sofrendo por causa do dono
      const piso = { sede: 55, fome: 55, carinho: 55, exercicio: 55 };
      est.pendente = est.pendente || {};
      for (const k of Object.keys(piso)) {
        const quando = est.pendente[k];
        if (quando === undefined) continue;
        if (agoraMin - quando <= 45) continue;   // recente: ainda dá tempo do dono agir
        // ⚠️ o guarda do `est[k] >= piso[k]` é essencial: sem ele o auto-cuidado
        // puxaria para BAIXO uma barra que o dono já tinha enchido
        if (est[k] >= piso[k]) { delete est.pendente[k]; continue; }
        est[k] = Math.min(piso[k], est[k] + 0.6 * minutos);
      }

      // passeios
      if (!est.proxPasseio) agendaProximoPasseio();
      if (t >= est.proxPasseio) {
        if (Math.random() < 0.28) voltaCompleta(); else passeio();
        agendaProximoPasseio();
      }

      salvar();
      pintaPainel();
    } catch (e) {
      // ⚠️ engolir em silêncio já custou caro (um ReferenceError deixou a agenda
      // muda sem ninguém perceber). O bicho nunca derruba o app — mas o primeiro
      // erro de cada sessão aparece, uma vez só, para não virar spam.
      try {
        if (!tiqueAvisouErro) {
          tiqueAvisouErro = true;
          console.error("[bicho] o passo falhou (e foi engolido):", e && e.message);
        }
      } catch (_) { }
    }
  }

  // ── painel 🐾 Bicho ────────────────────────────────────────────────────────
  let painel = null;
  const barra = (rotulo, valor) => {
    const p = Math.round(valor);
    const cor = p < 30 ? "#ff8a65" : p < 60 ? "#ffc94a" : "#35d07f";
    return `<div class="bicho-lin"><span>${rotulo}</span>
      <i><b style="width:${p}%;background:${cor}"></b></i><em>${p}</em></div>`;
  };
  function pintaPainel() {
    if (!painel || painel.classList.contains("oculto")) return;
    const alvo = painel.querySelector("#bicho-status");
    if (!alvo) return;
    const estado = estaDormindo()
      ? `dormindo até ${new Date(cfg.dormirAte).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" })}`
      : (estaEmFoco() ? "em modo foco 🎯" : (emExpediente() ? "em expediente" : "fora do expediente"));
    alvo.innerHTML =
      barra("💧 hidratação", est.sede) + barra("🍖 alimentação", est.fome) +
      barra("🥰 atenção", est.carinho) + barra("🏃 movimento", est.exercicio) +
      `<div class="bicho-est">${estado} · avisos hoje: ${est.avisosHoje || 0} · voltas: ${est.voltaHoje}/${cfg.maxVoltasDia}</div>`;
  }
  function abrirPainel() {
    if (!painel) {
      painel = document.createElement("div");
      painel.className = "bicho oculto";
      painel.innerHTML = `
        <div class="bicho-topo"><b>🐾 ${cfg.nome}</b>
          <span class="bicho-x" id="bicho-x">✕</span></div>
        <label>nome<input id="bicho-nome" maxlength="18" /></label>
        <div class="bicho-dupla">
          <label>trabalho das<input id="bicho-ini" type="number" min="0" max="23" /></label>
          <label>às<input id="bicho-fim" type="number" min="0" max="23" /></label>
        </div>
        <div class="bicho-dupla">
          <label>água a cada (h)<input id="bicho-agua" type="number" min="1" max="6" /></label>
          <label>caminhar a cada (min)<input id="bicho-caminha" type="number" min="30" max="240" /></label>
        </div>
        <div class="bicho-linha-bt">
          <button class="bicho-bt" id="bicho-foco">🎯 modo foco</button>
          <button class="bicho-bt" id="bicho-sono">😴 dormir</button>
        </div>
        <div class="bicho-linha-bt">
          <button class="bicho-bt" data-sono="1">1 h</button>
          <button class="bicho-bt" data-sono="2">2 h</button>
          <button class="bicho-bt" data-sono="3">3 h</button>
          <button class="bicho-bt" data-sono="4">4 h</button>
        </div>
        <div id="bicho-status"></div>`;
      document.body.appendChild(painel);

      const q = (s) => painel.querySelector(s);
      q("#bicho-x").onclick = () => { painel.classList.add("oculto"); reportarHit(); };
      q("#bicho-nome").onchange = (e) => { cfg.nome = e.target.value.trim() || "Xodó"; salvar(); };
      q("#bicho-ini").onchange = (e) => { cfg.trabIni = +e.target.value; salvar(); };
      q("#bicho-fim").onchange = (e) => { cfg.trabFim = +e.target.value; salvar(); };
      q("#bicho-agua").onchange = (e) => { cfg.aguaCadaMin = Math.max(30, (+e.target.value || 2) * 60); salvar(); };
      q("#bicho-caminha").onchange = (e) => { cfg.caminhadaCadaMin = Math.max(30, +e.target.value || 90); salvar(); };
      q("#bicho-foco").onclick = alternarFoco;
      q("#bicho-sono").onclick = () => { dormir(2); painel.classList.add("oculto"); };
      painel.querySelectorAll("[data-sono]").forEach((b) => {
        b.onclick = () => { dormir(+b.dataset.sono); painel.classList.add("oculto"); };
      });
    }
    const q = (s) => painel.querySelector(s);
    q("#bicho-nome").value = cfg.nome;
    q("#bicho-ini").value = cfg.trabIni;
    q("#bicho-fim").value = cfg.trabFim;
    q("#bicho-agua").value = Math.round(cfg.aguaCadaMin / 60);
    q("#bicho-caminha").value = cfg.caminhadaCadaMin;
    painel.querySelector(".bicho-topo b").textContent = "🐾 " + cfg.nome;
    painel.classList.remove("oculto");
    const r = pet ? pet.getBoundingClientRect() : { top: innerHeight - 140, left: 40 };
    painel.style.left = Math.max(8, Math.min(innerWidth - 320, r.left - 60)) + "px";
    painel.style.top = Math.max(8, r.top - 330) + "px";
    pintaPainel();
    reportarHit();
  }

  // O MENU tem UM dono: montarMenu() (main.js) + repintaMenu() (bloco 2 abaixo).
  // Existia aqui um enfeitaMenu() que ANEXAVA uma 7ª linha em #menu com
  // alimentar/água/exercício: duplicava os tiles (e escapava por baixo da
  // moldura) e os tiles dele só abriam o balão. Removido em 03/10/2026.
  // Regra daqui pra frente: uma ação, um botão, uma implementação.

  // ── ligação ────────────────────────────────────────────────────────────────
  try {
    if (pet) pet.addEventListener("dblclick", () => { if (interagindo()) carinho(0); });
    // primeira vez: ele se apresenta. Os avisos seguem a agenda biológica do dia
    // (água de 2 em 2 h, refeições nos horários, caminhada no expediente)
    try {
      if (!localStorage.getItem("bicho.oi")) {
        localStorage.setItem("bicho.oi", "1");
        setTimeout(() => fala(`Oi! Sou o ${cfg.nome}. 🐾 Me alimenta, me dá água e faz carinho de vez em quando — eu aviso quando precisar. E fica tranquilo: eu NUNCA morro.`), 4000);
      }
    } catch (e) { /* sem storage */ }
    if (!est.proxPasseio) agendaProximoPasseio();
    // 04/10/2026: o bicho NÃO veste mais os presentes (pedido do dono — ficava
    // feio no boneco atual; quem vai vestir são os bonecos próprios do PetAgent,
    // que ainda não existem). Aqui só limpa adereço que tenha ficado colado antes.
    try {
      const a = document.getElementById("aces");
      if (a) a.textContent = "";
      if (est.equipado) { est.equipado = ""; if (salvar) salvar(); }
    } catch (e) { /* sem DOM ainda */ }
    if (estaDormindo()) sumir(true);
    setInterval(tique, 20000);
    setTimeout(tique, 3000);
    window.bicho = { cfg, est, carinho, dormir, acordar, alternarFoco, passeio, pararAndar,
                     voltaCompleta, abrirPainel,
                     // compat: apontam para a ÚNICA implementação (bloco 2)
                     get alimentar() { return window.bichoAlimentar; },
                     get darAgua() { return window.bichoAgua; },
                     get exercicio() { return window.bichoExercicio; } };
  } catch (e) { /* nada aqui pode impedir o app de subir */ }
})();

/* ────────────────────────────────────────────────────────────────────────────
   CUIDADOS DO BICHO (3ª rodada, 03/10/2026) — substitui os botões do menu:
     🍖 Alimentar (frases variadas) · 🎾 Brincar (30 s pulando e dando cambalhota)
     😴 Dormir (com escolha de horas) · 📊 Como ele está (gráfico do pet)
     💎 Coleção = ACHADOS (ele acha baús com brinquedos e acessórios de verdade)
   "Refeição"/"Lanche" saíram do menu (não tinham função); "Missões" saiu porque
   tudo agora se resolve no chat — o que ficou no lugar é o aviso de tarefa
   concluída, que aparece quando o chat está fechado.
   ──────────────────────────────────────────────────────────────────────────── */
(() => {
  "use strict";
  const MIN = 60000;
  const sorteia = (l) => l[Math.floor(Math.random() * l.length)];
  const est = () => (window.bicho && window.bicho.est) || {};
  const cfgB = () => (window.bicho && window.bicho.cfg) || {};
  const salvaEst = () => {
    try { localStorage.setItem("bicho.estado", JSON.stringify(est())); } catch (e) { /* sem storage */ }
  };
  // recado do bicho = balãozinho, SEM abrir o chat (regra do dono, 03/10/2026)
  const fala = (t) => {
    try {
      try {
        if (window.go && window.go.main && window.go.main.App && window.go.main.App.Log) {
          window.go.main.App.Log("bicho fala: " + t);
        }
      } catch (e) { }
      if (window.petBipe) window.petBipe();      // sininho suave (pedido do dono)
      if (typeof toast === "function") toast("🐾", cfgB().nome || "Bicho", t, 7000, true);
      else if (typeof msg === "function") msg(t, "ele");
    } catch (e) { /* o bicho nunca derruba o app */ }
  };
  /* Reação VISÍVEL no boneco. Sem isto o clique só abre o balão e parece que
     nada aconteceu — foi o bug relatado pelo dono em 03/10/2026. */
  const fx = (emojis, n) => {
    try { if (typeof explosao === "function") explosao(emojis, n || 6); } catch (e) { }
  };
  let reageTimer = null;
  const reage = (estadoPet, ms) => {
    try {
      if (typeof aplicarEstado !== "function") return;
      // classePet() reescreve pet.className — então: (a) não cortar a
      // brincadeira/volta que estiver rolando, (b) a reação MAIS NOVA manda,
      // senão o timeout da anterior apaga a animação seguinte (bug visto 03/10).
      if (pet && (pet.classList.contains("brincando") || pet.classList.contains("volta"))) return;
      clearTimeout(reageTimer);
      aplicarEstado({ state: estadoPet });
      reageTimer = setTimeout(() => aplicarEstado({ state: "idle" }), ms || 1600);
    } catch (e) { }
  };
  function garante() {
    const e = est();
    if (e.humor === undefined) e.humor = 80;
    if (e.tarefas === undefined) e.tarefas = 0;
    if (!Array.isArray(e.achados)) e.achados = [];
    if (!e.achadoDia) e.achadoDia = { dia: "", n: 0 };
    if (!e.equipado) e.equipado = "";
    return e;
  }
  const feliz = (q) => { const e = garante(); e.humor = Math.min(100, (e.humor || 80) + q); };

  /* ── 🍖 alimentar: fecha o menu (o tile já fecha) e fala variado ─────────── */
  const FRASES_FOME = ["Hummm… 😋", "Gostoso!!!", "Delícia!", "Isso estava ótimo 🍖",
    "Nham nham nham…", "Que refeição, hein!", "Mais um pouquinho? 😍", "Barriga satisfeita!"];
  const FRASES_AGUA = ["Glub glub… 💧", "Água geladinha, que delícia!", "Ai que refresco!",
    "Obrigado, estava com sede!", "💧💧💧"];
  /* ── 🍖 alimentar / 💧 água / 🏃 exercício / 🎾 brincar: SILENCIOSOS ─────────
     Pedido do dono (03/10/2026): estas ações do menu NÃO inserem mensagem
     nenhuma — nem no chat, nem no balãozinho. O feedback é o EFEITO no boneco:
     emoji subindo + animação (e o estado do bicho muda, visível no painel 🐾).
     Era a mensagem inserida que fazia o mini chat abrir sozinho.
     (FRASES_* ficam guardadas para o dia em que ele quiser a voz de volta.) */
  window.bichoAlimentar = () => {
    const e = garante();
    e.fome = Math.min(100, e.fome + 35); feliz(8); salvaEst();
    if (window.petBipe) window.petBipe();                   // sininho (pedido do dono)
    fx(["🍖", "😋", "✨"], 7); reage("comemorando", 1800);   // ele COME: pula e brilha
  };
  window.bichoAgua = () => {
    const e = garante();
    e.sede = Math.min(100, e.sede + 40); feliz(6); salvaEst();
    if (window.petBipe) window.petBipe();                   // sininho (pedido do dono)
    fx(["💧", "💦", "✨"], 6); reage("carinho", 1400);       // bebe: balança de gosto
  };
  /* 🏃 exercício — o dono pediu no menu e não existia: só "Brincar" mexia nisto */
  const FRASES_EXERCICIO = ["Ufa! Alonguei tudo 🏃", "Hora do treino!", "Vou dar umas voltas!",
    "Corpo em movimento! 💪", "Alonga aqui, alonga ali…"];
  /* volta completa CONTORNANDO AS BORDAS em 22 s, no sentido horário (o movimento é
     do `voltaNaTela`, no main.js — o keyframe antigo saía da tela em multi-monitor).
     A classe `volta` continua sendo posta: é ela que segura `classePet`, `vagueia` e
     `passeio` enquanto o boneco está fora do lugar (ver EFEITOS/emEfeito).
     `frases` = o balãozinho que ele solta QUANDO TERMINA (pedido do dono). */
  const VOLTA_MS = 22000;
  function darVolta(comAndar, frases) {
    if (!pet || pet.classList.contains("volta")) return;
    try { if (window.bicho && window.bicho.pararAndar) window.bicho.pararAndar(); } catch (e) { }
    pet.classList.add("volta");
    if (comAndar) pet.classList.add("andando");
    voltaNaTela(VOLTA_MS, () => {
      pet.classList.remove("volta", "andando");
      if (frases && frases.length) fala(sorteia(frases));
    });
  }

  const FRASES_VOLTA_CORRIDA = ["Ufa! Gostei da corrida! 😄", "Cansei… mas valeu! 🥵",
    "Que volta boa! 🏃", "Corri bastante! 💪", "Adorei! Bora de novo? 😅"];

  window.bichoExercicio = () => {
    const e = garante();
    e.exercicio = Math.min(100, e.exercicio + 40); feliz(10); salvaEst();
    // 🪙 moedeiro (regra do dono, 04/10/2026): exercício rende moeda.
    // Sem mensagem: o exercício é ação SILENCIOSA — a moeda aparece no efeito
    // (🪙 subindo) e no contador da galeria.
    if (window.premiosMoedas) { try { window.premiosMoedas(1); fx(["🪙"], 1); } catch (err) { } }
    fx(["🏃", "💨", "💪"], 7);
    darVolta(true, FRASES_VOLTA_CORRIDA);   // contorna a tela e volta pro lugar
  };

  /* ── 🎾 brincar: dá a volta na tela PULANDO e dando cambalhota (22 s, o mesmo
     tempo do keyframe voltaTela — os dois terminam juntos, no lugar de origem) ─ */
  const FRASES_BRINCAR = ["Vou brincar! 🎾", "Uhuu! Hora de brincar!", "Pega-pega comigo? 🎾",
    "Olha eu pulando!", "Quero cambalhota! 🤸"];
  const FRASES_FIM = ["Ufa! Que brincadeira boa 🥵", "Cansado, mas feliz! 😄", "Isso foi divertido!"];
  const FRASES_VOLTA_BRINCAR = ["Ufa! Que brincadeira boa! 🥵", "Cansei, mas me diverti! 😄",
    "Gostei! Pega-pega de novo? 🎾", "Isso foi divertido! ✨"];
  window.bichoBrincar = () => {
    const e = garante();
    e.exercicio = Math.min(100, e.exercicio + 28);
    e.carinho = Math.min(100, e.carinho + 8);
    feliz(12); salvaEst();
    fx(["🎾", "✨", "🐾"], 6);
    if (!pet) return;
    pet.classList.add("brincando");          // pula/cambalhota no filho — soma com a volta
    darVolta(true, FRASES_VOLTA_BRINCAR);    // a volta na tela + o recado no fim
    setTimeout(() => pet.classList.remove("brincando"), VOLTA_MS);
  };

  /* ── 😴 dormir: escolhe o tempo (o balão pergunta e o painel tem as opções) ─ */
  window.bichoDormirMenu = () => {
    fala("Quanto tempo de soneca? Escolhe no painel 🐾: 1 h, 2 h, 3 h ou 4 h. 😴");
    if (window.bicho && window.bicho.abrirPainel) window.bicho.abrirPainel();
  };

  /* ── 📊 como ele está: gráfico do PET ────────────────────────────────────── */
  window.bichoGrafico = () => {
    const e = garante();
    const b = (rotulo, valor, ico) => {
      const p = Math.max(0, Math.min(100, Math.round(valor)));
      const cor = p < 30 ? "#ff8a65" : p < 60 ? "#ffc94a" : "#35d07f";
      return `<div class="bicho-lin"><span>${ico} ${rotulo}</span>
        <i><b style="width:${p}%;background:${cor}"></b></i><em>${p}</em></div>`;
    };
    let p = document.getElementById("bichoGraf");
    if (!p) {
      p = document.createElement("div");
      p.className = "bicho";
      p.id = "bichoGraf";
      document.body.appendChild(p);
    }
    p.innerHTML = `<div class="bicho-topo"><b>📊 ${cfgB().nome || "Xodó"} — como estou</b>
        <span class="bicho-x" id="graf-x">✕</span></div>
      ${b("alimentado", e.fome, "🍖")}
      ${b("tarefa", Math.min(100, (e.tarefas || 0) * 12), "📋")}
      ${b("cansaço", 100 - e.exercicio, "🥵")}
      ${b("felicidade", e.humor || 80, "😊")}
      ${b("sede", e.sede, "💧")}
      ${b("carinho", e.carinho, "🥰")}
      <div class="bicho-est">tarefas no chat hoje: ${e.tarefas || 0} · achados: ${(e.achados || []).length}
      </div>`;
    p.classList.remove("oculto");
    p.querySelector("#graf-x").onclick = () => { p.classList.add("oculto"); reportarHit(); };
    const r = pet ? pet.getBoundingClientRect() : { top: innerHeight - 160, left: 40 };
    p.style.left = Math.max(8, Math.min(innerWidth - 320, r.left - 40)) + "px";
    p.style.top = Math.max(8, r.top - 360) + "px";
    reportarHit();
  };

  /* ── 💎 achados: baú com presentes (catálogo e sorteio moram em premios.js) ──
     04/10/2026: os 8 itens fixos sorteados de forma uniforme (daí as repetições)
     viraram um catálogo de milhares de presentes (tipo + COR = item diferente),
     com raridade de 0,031% no topo, repetido → moeda e galeria com os raros em
     destaque.
     ⚠️ O BICHO NÃO VESTE NADA (pedido do dono, 04/10/2026): visualmente ficava
     feio no boneco atual. Ele guarda e comemora; quem vai vestir os presentes são
     os bonecos próprios do PetAgent — que ainda não existem. Não reintroduzir
     `vestir()` sem isso. */
  window.bichoAchar = () => {
    if (!window.premiosAchar) return;              // premios.js não carregou
    const r = window.premiosAchar(false);
    if (!r) { fala("Já achei 4 baús hoje — amanhã eu procuro mais! 🎁"); return; }
    const it = r.item, nomes = { ultra: "ULTRA-RARO", mitico: "MÍTICO",
      lendario: "LENDÁRIO", epico: "ÉPICO", raro: "RARO" };
    if (r.novo) {
      const e = garante();
      e.humor = Math.min(100, (e.humor || 80) + 8);
      salvaEst();
      if (nomes[it.raridade]) {
        // achado grande: a voz do bicho dá o tom (a entrega visual é o cartão)
        fala(`${it.ico} ${it.nome}?! Isso é ${nomes[it.raridade]}! 🤩`);
      } else {
        fala(sorteia([`Achei um baú! 🎁 Olha: ${it.ico} ${it.nome}!`,
                      `Um baú apareceu por aqui… 🎁 ${it.ico} ${it.nome} pra mim!`,
                      `Ganhei: ${it.ico} ${it.nome}! Que sorte!`]));
      }
    } else {
      fala(`Outro ${it.ico} ${it.nome}… repetido! Virou 🪙 ${r.moedas} moeda${r.moedas > 1 ? "s" : ""} 💰`);
    }
  };
  // 💎 a galeria (moedeiro + raros em destaque + coleção) vive em premios.js
  window.bichoColecao = () => {
    if (window.premiosGaleria) { window.premiosGaleria(); return; }
    fala("Minha galeria não carregou… 🥺");
  };

  /* ── menu: o bicho dirige o menu — UMA ação, UM botão ───────────────────── */
  function repintaMenu() {
    try {
      if (typeof TILES === "undefined") return;
      // linha 1 — os cuidados que ele pede, todos com reação visível no boneco
      TILES.linha1 = [
        ["🍖", "Alimentar", () => window.bichoAlimentar()],
        ["💧", "Água", () => window.bichoAgua()],
        ["🏃", "Exercício", () => window.bichoExercicio()],
        ["🎾", "Brincar", () => window.bichoBrincar()],
      ];
      // linha 2 (destaque, botão grande) — as duas portas de entrada
      TILES.linha2 = [
        ["🐾", "Cuidar dele", () => window.bicho.abrirPainel()],
        ["💬", "Conversar", () => { abrirBalao(); txt.focus(); }],
      ];
      // linha 3 — estado e sistema. Água saiu daqui: agora mora na linha 1.
      TILES.linha3 = [
        ["📊", "Como ele está", () => window.bichoGrafico()],
        ["💎", "Coleção", () => window.bichoColecao()],
        ["😴", "Dormir", () => window.bichoDormirMenu()],
        ["📧", "E-mail", () => abrirEmail()],
      ];
      // linha 6 — ZERADA: era depósito de resto ("Testar balãozinho" = debug)
      TILES.linha6 = [];
    } catch (e) { /* menu ainda não existe */ }
  }

  /* ── tarefa concluída: balão quando o chat está fechado ──────────────────── */
  try {
    const msgOriginal = window.msg;
    if (typeof msgOriginal === "function") {
      window.msg = function (texto, quem) {
        try {
          const eraTarefa = quem === "ele" && typeof conversa !== "undefined" &&
            conversa.length && conversa[conversa.length - 1].quem === "eu";
          if (eraTarefa && String(texto).length > 4) {
            const e = garante();
            e.tarefas = (e.tarefas || 0) + 1; feliz(4); salvaEst();
            const fechado = balao && balao.classList.contains("oculto");
            msgOriginal.call(null, "✅ tarefa concluída", "meta");
            if (fechado) window.bichoAchar();   // chat fechado? ele aparece com o aviso
          }
        } catch (e) { /* nunca quebra o chat */ }
        return msgOriginal.apply(null, arguments);
      };
    }
  } catch (e) { /* ignora */ }

  // achados: verifica de vez em quando se apareceu um baú
  try {
    garante(); repintaMenu();
    setInterval(() => {
      try {
        repintaMenu();
        const e = garante();
        if (cfgB().dormirAte > Date.now()) return;
        const h = new Date().toISOString().slice(0, 10);
        if (e.achadoDia.dia !== h) e.achadoDia = { dia: h, n: 0 };
        if (e.achadoDia.n < 4 && Math.random() < 0.06) window.bichoAchar();
      } catch (err) { /* ignora */ }
    }, 5 * MIN);
  } catch (e) { /* ignora */ }
})();
