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
  // em MINUTOS: o painel aceita 08:30 (trabIni = 8.5). Antes só valia hora cheia.
  const emExpediente = () => {
    const d = new Date(), m = d.getHours() * 60 + d.getMinutes();
    const ini = Math.round(cfg.trabIni * 60), fim = Math.round(cfg.trabFim * 60);
    return ini <= fim
      ? (m >= ini && m < fim)
      : (m >= ini || m < fim); // turno que cruza a meia-noite
  };

  // ── recado do bicho: balãozinho (toast), SEM abrir o chat ──────────────────
  // Regra do dono (03/10/2026): só o tile "Conversar" e as respostas do agente
  // abrem a conversa. Pedido/carinho/achado do bicho é RECADO — não sequestra o chat.
  function fala(texto, acoes, ico) {
    try {
      // prova externa: todo recado do bicho fica também no petagent.log (canal G.Log
      // que o front já tem para o Go). Acabou o "será que o balão saiu?".
      try {
        if (window.go && window.go.main && window.go.main.App && window.go.main.App.Log) {
          window.go.main.App.Log("bicho fala: " + texto);
        }
      } catch (e) { }
      if (window.petBipe) window.petBipe();      // sininho suave (pedido do dono)
      if (typeof toast === "function") {
        // com ação, o aviso espera mais (45 s) — dá tempo de levantar e voltar
        const tem = acoes && acoes.length;
        toast(ico || "🐾", cfg.nome || "Bicho", texto, tem ? 45000 : 7000, !tem, { acoes: acoes || [] });
      }
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

  // DORMIR (22/10/2026): ele fica NA TELA cochilando (apagadinho, com zZ) e um clique
  // acorda. Antes ele SUMIA — e sumido não havia onde clicar: só restava esperar.
  const marcaSono = (sim) => { try { document.body.classList.toggle("pet-dormindo", sim); } catch (e) { } };
  function dormir(horas) {
    cfg.dormirAte = agora() + Math.round(horas * 60) * MIN;
    salvar();
    marcaSono(true);
    fala(horas < 1 ? `Boa noite! Vou cochilar por ${Math.round(horas * 60)} minutinhos. 😴`
                   : `Boa noite! Vou dormir por ${horas} hora${horas > 1 ? "s" : ""}. 😴`);
    pintaPainel();
  }

  function acordar() {
    cfg.dormirAte = 0;
    salvar();
    marcaSono(false);
    sumir(false);   // quem dormiu numa versão antiga ficou ESCONDIDO: volta
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
  // LEMBRETE COM AÇÃO (22/10/2026, prancha "Avisos"): o aviso traz o botão que resolve
  // — "Já bebi" conta como dar água, igual ao menu — e o "Daqui a 15 min", que repete o
  // mesmo lembrete depois (o tique cuida, ver est.adiado).
  const ACAO_LEMBRETE = {
    agua: ["Já bebi", "agua", () => window.bichoAgua && window.bichoAgua()],
    caminhada: ["Já caminhei", "exercicio", () => window.bichoExercicio && window.bichoExercicio()],
    refeicao: ["Comi", "alimentar", () => window.bichoAlimentar && window.bichoAlimentar()],
    lanche: ["Comi", "alimentar", () => window.bichoAlimentar && window.bichoAlimentar()],
    atencao: ["Carinho", "cuidar", () => carinho(30)],
  };
  function lembra(tipo, frases) {
    const a = ACAO_LEMBRETE[tipo];
    const acoes = [];
    if (a) acoes.push({ r: a[0], prim: true, fn: a[2] });
    acoes.push({ r: "Daqui a 15 min", fn: () => { est.adiado = { tipo: tipo, frases: frases, ate: agora() + 15 * MIN }; salvar(); } });
    fala(escolhe(frases), acoes, a ? a[1] : "");
  }

  function alternarFoco() {
    cfg.foco = !cfg.foco; salvar(); pintaPainel();
    fala(cfg.foco
      ? "Modo foco ligado. 🎯 Vou lembrar você só da água e ficar quietinho."
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
    agua: ["💧 Hora da água! Eu já bebi a minha. E você?",
           "💧 Vamos encher o copo? Já faz um tempinho.",
           "Que sede! 💧 Que tal beber uma água comigo?",
           "💧 Pausa para o copo d'água. Eu espero você."],
    caminhada: ["🚶 Levante e estique as pernas comigo. Dois minutos bastam.",
                "Minhas patinhas pedem movimento, e as suas costas também. 👀",
                "🚶 Que tal uma voltinha? É rapidinho.",
                "Pausa para caminhar! Levante da cadeira e mexa-se. 🚶‍♂️"],
    cafe: ["☕ Começando o dia! O café da manhã já está na mesa?",
           "☕ Acordei com fome. E você, já tomou café?"],
    almoco: ["🍲 Metade do dia: hora de comer de verdade, não só beliscar.",
             "🍲 Estou com uma fome de almoço… Você também?"],
    jantar: ["🍽 O fim do expediente está chegando. Que tal comer algo de verdade?",
             "🍽 Hora da última refeição do dia! O meu pratinho e o seu. 👀"],
    lancheM: ["🍎 Meio da manhã: uma fruta cai bem.",
              "🍎 Belisquei minha ração. E você, que tal um lanchinho?"],
    lancheT: ["🥪 Que tal um lanchinho leve agora? (Eu aceito migalhas. 🐾)",
              "🥪 Aquela fominha da tarde chegou para nós."],
    atencao: ["🥰 Um carinho rapidinho?",
              "Ei, olhe para mim um instante. 🐾",
              "🥰 Estou aqui do seu lado. Só queria um pouco de atenção.",
              "Um cafuné, e eu volto a trabalhar feliz. 🥰"],
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
      // humor: cai ~2 por hora e para em 30 (antes só subia — ficava sempre em 100).
      // Sobe com cada cuidado e com cada tarefa feita no chat (bloco 2, feliz()).
      if (typeof est.humor === "number") est.humor = Math.max(30, est.humor - minutos / 30);

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
        lembra(ev.tipo, ev.frases);
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
      // lembrete adiado ("Daqui a 15 min"): chegou a hora, avisa de novo (no foco, só água)
      if (est.adiado && t >= est.adiado.ate) {
        const ad = est.adiado;
        delete est.adiado;
        if (!estaEmFoco() || ad.tipo === "agua") lembra(ad.tipo, ad.frases && ad.frases.length ? ad.frases : [""]);
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

  // ── painel do pet (22/10/2026, opção C da prancha "Painel do pet") ────────────
  // "Como ele está" e "Cuidar dele" viraram UM painel encostado no boneco: 4 anéis no
  // mesmo sentido (cheio = bem) + cuidado rápido + o próximo lembrete. A engrenagem
  // abre, AO LADO, a rotina (nome, expediente com minutos, água, caminhada) e as pausas
  // (foco e dormir). Antes eram dois painéis com rótulos diferentes, "cansaço" invertido
  // pintado de vermelho quando estava ÓTIMO e "sede 63" querendo dizer hidratação 63.
  let painel = null, lado = null, ladoAberto = false, salvoTimer = null;
  const corDe = (v) => v >= 60 ? "#35D07F" : v >= 30 ? "#F2B33D" : "#FF7A6B";
  const ic = (n, t) => (typeof icone === "function" ? icone(n, t) : "");
  const hhmm = (h) => {             // 8.5 → "08:30"
    const m = Math.round((Number(h) || 0) * 60);
    return String(Math.floor(m / 60) % 24).padStart(2, "0") + ":" + String(m % 60).padStart(2, "0");
  };
  const deHHMM = (s) => {           // "08:30" → 8.5
    const [h, m] = String(s || "").split(":").map(Number);
    return (h || 0) + (m || 0) / 60;
  };
  const conta = (n, palavra) => `${n || 0} ${palavra}${(n || 0) === 1 ? "" : "s"}`;
  const NOME_EV = { agua: "água", caminhada: "caminhada", refeicao: "refeição", lanche: "lanche", atencao: "carinho" };

  // a linha de baixo dos anéis: o próximo lembrete (ou por que não há)
  function proximoLembrete() {
    const d = new Date(), agoraMin = d.getHours() * 60 + d.getMinutes();
    if (!emExpediente()) return `Fora do expediente: os lembretes voltam às <b>${hhmm(cfg.trabIni)}</b>`;
    const feitos = est.feitos || {};
    const prox = eventosDoDia(cfg, { ini: cfg.trabIni, fim: cfg.trabFim })
      .filter((e) => e.min > agoraMin && !feitos[chaveEv(e)] && (!estaEmFoco() || e.tipo === "agua"))[0];
    if (!prox) return "Não há mais lembretes hoje";
    return `Próximo lembrete: <b>${NOME_EV[prox.tipo] || prox.tipo} às ${minHora(prox.min)}</b>` +
      (estaEmFoco() ? " (modo foco ligado)" : "");
  }

  function anel(nome, icoNome, v) {
    const p = Math.max(0, Math.min(100, Math.round(v))), cor = corDe(p);
    return `<div class="pp-anel" title="${nome}: ${p}%">
      <div class="pp-anel-svg">
        <svg width="64" height="64" viewBox="0 0 64 64" aria-hidden="true">
          <circle cx="32" cy="32" r="26" class="pp-trilho"></circle>
          <circle cx="32" cy="32" r="26" class="pp-valor" style="stroke:${cor};stroke-dasharray:${(163.4 * p / 100).toFixed(1)} 164"></circle>
        </svg>
        <span style="color:${cor}">${ic(icoNome, 20)}</span>
      </div>
      <b>${nome}</b><span>${p}%</span></div>`;
  }

  function pintaPainel() {
    if (!painel || painel.classList.contains("oculto")) return;
    const e = est, r = window.bichoResumo ? window.bichoResumo() : { nome: cfg.nome, linha: "" };
    const esc = (t) => String(t ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
    let corpo;
    if (estaDormindo()) {
      const ate = new Date(cfg.dormirAte), falta = Math.max(0, Math.round((cfg.dormirAte - agora()) / MIN));
      const faltaTxt = falta >= 60 ? `${Math.floor(falta / 60)}h${String(falta % 60).padStart(2, "0")}` : `${falta} min`;
      corpo = `<div class="pp-sono">${ic("dormir", 20)}<span><b>Dormindo até ${ate.toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" })}</b>
          <span>Faltam ${faltaTxt}. Os lembretes ficam pausados e as barras não caem.</span></span></div>
        <div class="pp-acoes dois"><button class="pp-bt" data-acordar>Acordar agora</button><button class="pp-bt sec" data-mais1h>+1 h</button></div>`;
    } else {
      corpo = `<div class="pp-aneis">${anel("Água", "agua", e.sede)}${anel("Comida", "alimentar", e.fome)}${anel("Movimento", "exercicio", e.exercicio)}${anel("Carinho", "cuidar", e.carinho)}</div>
        <div class="pp-acoes">
          <button class="pp-bt sec" data-cuida="agua">Água</button><button class="pp-bt sec" data-cuida="comer">Comer</button>
          <button class="pp-bt sec" data-cuida="mexer">Mexer</button><button class="pp-bt sec" data-cuida="carinho">Carinho</button></div>
        <div class="pp-proximo">${ic("relogio", 16)}<span>${proximoLembrete()}</span></div>`;
    }
    painel.innerHTML = `
      <div class="pp-topo"><span class="pp-quem"><b>${esc(r.nome || cfg.nome)}</b><span>${esc(r.linha || "")}</span></span>
        <button class="botao-x ${ladoAberto ? "sel" : ""}" data-ajustes aria-expanded="${ladoAberto}" aria-label="Rotina e pausas" title="Rotina e pausas">${ic("ajustes", 18)}</button>
        <button class="botao-x" data-fechar aria-label="Fechar">${ic("fechar", 18)}</button></div>
      ${corpo}
      <div class="pp-rodape">Hoje: ${conta(e.tarefas, "tarefa")} no chat · ${conta(e.avisosHoje, "lembrete")} · ${conta((e.achados || []).length, "achado")}</div>`;
    const q = (s) => painel.querySelector(s);
    q("[data-fechar]").onclick = fecharPainel;
    q("[data-ajustes]").onclick = () => { ladoAberto = !ladoAberto; pintaPainel(); pintaLado(); };
    painel.querySelectorAll("[data-cuida]").forEach((b) => b.onclick = () => {
      const f = { agua: window.bichoAgua, comer: window.bichoAlimentar, mexer: window.bichoExercicio,
                  carinho: () => carinho(30) }[b.dataset.cuida];
      if (f) f();
      pintaPainel();
    });
    if (q("[data-acordar]")) q("[data-acordar]").onclick = () => acordar();
    if (q("[data-mais1h]")) q("[data-mais1h]").onclick = () => { cfg.dormirAte += 60 * MIN; salvar(); pintaPainel(); };
    posicionaPainel();
  }

  // a rotina e as pausas: só redesenha ao abrir ou ao mudar algo AQUI (o tique a cada
  // 20 s redesenharia por cima de quem está digitando)
  function pintaLado() {
    if (!ladoAberto) { if (lado) lado.classList.add("oculto"); posicionaPainel(); return; }
    if (!lado) {
      lado = document.createElement("div");
      lado.className = "bicho pp pp-lado oculto";
      document.body.appendChild(lado);
    }
    const opc = (sel, lista) => lista.map(([v, r]) => `<option value="${v}" ${Number(sel) === v ? "selected" : ""}>${r}</option>`).join("");
    const intervalos = [[60, "1 h"], [90, "1h30"], [120, "2 h"], [180, "3 h"]];
    lado.innerHTML = `
      <div class="pp-topo"><b class="pp-titulo">Rotina e pausas</b><span class="pp-salvo" aria-live="polite"></span></div>
      <label class="pp-campo-l">Nome<input class="pp-campo" data-nome maxlength="18" value="${String(cfg.nome).replace(/"/g, "&quot;")}"></label>
      <label class="pp-campo-l">Seu expediente
        <span class="pp-linha"><input class="pp-campo" type="time" data-ini value="${hhmm(cfg.trabIni)}">até
        <input class="pp-campo" type="time" data-fim value="${hhmm(cfg.trabFim)}"></span></label>
      <div class="pp-linha"><span>Água a cada</span><select class="pp-campo curto" data-agua>${opc(cfg.aguaCadaMin, intervalos)}</select></div>
      <div class="pp-linha"><span>Caminhar a cada</span><select class="pp-campo curto" data-caminha>${opc(cfg.caminhadaCadaMin, intervalos)}</select></div>
      <p class="pp-nota">Fora do expediente não há lembretes. Os horários das refeições são calculados a partir dele: café, lanche, almoço, lanche e a última refeição, 45 min antes do fim.</p>
      <div class="pp-sep"></div>
      <div class="pp-linha"><span class="pp-quem"><b>Modo foco</b><span>Apenas os lembretes de água. Não altera nada no Windows.</span></span>
        <button class="pp-switch" role="switch" aria-checked="${estaEmFoco()}" aria-label="Modo foco" data-foco><i></i></button></div>
      <div class="pp-quem"><b>Dormir</b><span>O boneco cochila no canto, sem lembretes. Clique nele para acordar.</span></div>
      <div class="pp-acoes">${[[0.5, "30 min"], [1, "1 h"], [2, "2 h"], [4, "4 h"]].map(([h, r]) =>
        `<button class="pp-bt sec" data-dormir="${h}">${r}</button>`).join("")}</div>`;
    lado.classList.remove("oculto");
    const q = (s) => lado.querySelector(s);
    const salvo = () => {
      salvar(); pintaPainel();
      const s = q(".pp-salvo");
      if (s) { s.textContent = "✓ Salvo"; clearTimeout(salvoTimer); salvoTimer = setTimeout(() => { s.textContent = ""; }, 1800); }
    };
    q("[data-nome]").onchange = (ev) => { cfg.nome = ev.target.value.trim() || "Xodó"; salvo(); };
    q("[data-ini]").onchange = (ev) => { if (ev.target.value) { cfg.trabIni = deHHMM(ev.target.value); salvo(); } };
    q("[data-fim]").onchange = (ev) => { if (ev.target.value) { cfg.trabFim = deHHMM(ev.target.value); salvo(); } };
    q("[data-agua]").onchange = (ev) => { cfg.aguaCadaMin = +ev.target.value; salvo(); };
    q("[data-caminha]").onchange = (ev) => { cfg.caminhadaCadaMin = +ev.target.value; salvo(); };
    q("[data-foco]").onclick = () => { alternarFoco(); pintaLado(); };
    lado.querySelectorAll("[data-dormir]").forEach((b) => b.onclick = () => { dormir(+b.dataset.dormir); pintaLado(); });
    posicionaPainel();
  }

  // encostado no boneco (posicionaPertoDoPet, main.js); a rotina vai AO LADO, do lado
  // de dentro da tela, com a base alinhada à do painel
  function posicionaPainel() {
    if (!painel || painel.classList.contains("oculto")) return;
    if (typeof posicionaPertoDoPet === "function") posicionaPertoDoPet(painel);
    if (lado && ladoAberto && !lado.classList.contains("oculto")) {
      const r = painel.getBoundingClientRect(), w = lado.offsetWidth || 320;
      const esquerda = r.left - 12 - w >= 8 ? r.left - 12 - w : Math.min(innerWidth - w - 8, r.right + 12);
      lado.style.left = Math.round(esquerda) + "px";
      lado.style.top = painel.style.top;
      lado.style.bottom = painel.style.bottom;
      lado.style.maxHeight = painel.style.maxHeight;
    }
    if (typeof reportarHit === "function") reportarHit();
  }

  // abrirPainel(comAjustes) — "Como está" abre só o painel; "Cuidar dele" e "Dormir"
  // abrem já com a rotina e as pausas ao lado
  function abrirPainel(comAjustes) {
    if (!painel) {
      painel = document.createElement("div");
      painel.className = "bicho pp oculto";
      document.body.appendChild(painel);
    }
    if (comAjustes !== undefined) ladoAberto = !!comAjustes;
    painel.classList.remove("oculto");
    pintaPainel();
    pintaLado();
  }
  function fecharPainel() {
    if (painel) painel.classList.add("oculto");
    if (lado) lado.classList.add("oculto");
    if (typeof reportarHit === "function") reportarHit();
  }

  // O MENU tem UM dono: montarMenu() (main.js) + repintaMenu() (bloco 2 abaixo, aba Pet).
  // Existia aqui um enfeitaMenu() que ANEXAVA uma 7ª linha em #menu com
  // alimentar/água/exercício: duplicava os tiles (e escapava por baixo da
  // moldura) e os tiles dele só abriam o balão. Removido em 03/10/2026.
  // Regra daqui pra frente: uma ação, um botão, uma implementação.

  // ── ligação ────────────────────────────────────────────────────────────────
  try {
    if (pet) pet.addEventListener("dblclick", () => { if (interagindo() && !estaDormindo()) carinho(0); });
    // primeira vez: ele se apresenta. Os avisos seguem a agenda biológica do dia
    // (água de 2 em 2 h, refeições nos horários, caminhada no expediente)
    try {
      if (!localStorage.getItem("bicho.oi")) {
        localStorage.setItem("bicho.oi", "1");
        setTimeout(() => fala(`Oi! Sou o ${cfg.nome}. 🐾 Dê comida, água e carinho de vez em quando; eu aviso quando precisar. E não se preocupe: eu nunca morro.`), 4000);
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
    if (estaDormindo()) marcaSono(true);
    setInterval(tique, 20000);
    setTimeout(tique, 3000);
    window.bicho = { cfg, est, carinho, dormir, acordar, alternarFoco, passeio, pararAndar,
                     voltaCompleta, abrirPainel, fecharPainel, estaDormindo, estaEmFoco,
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
  const FRASES_FOME = ["Hummm… 😋", "Que gostoso!", "Delícia!", "Isso estava ótimo! 🍖",
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
  const FRASES_EXERCICIO = ["Ufa! Alonguei tudo. 🏃", "Hora do treino!", "Vou dar umas voltas!",
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
    "Que volta boa! 🏃", "Corri bastante! 💪", "Adorei! Vamos de novo? 😅"];

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
  const FRASES_FIM = ["Ufa! Que brincadeira boa! 🥵", "Cansado, mas feliz! 😄", "Isso foi divertido!"];
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

  /* ── 😴 dormir / 📊 como ele está: os dois abrem o PAINEL DO PET (bloco 1) ─────
     22/10/2026: "Como ele está" e "Cuidar dele" viraram um painel só. "Dormir" abre
     com as pausas ao lado (30 min · 1 h · 2 h · 4 h) — antes ele só mandava "escolhe
     no painel" e parecia não fazer nada. */
  window.bichoDormirMenu = () => {
    if (window.bicho && window.bicho.abrirPainel) window.bicho.abrirPainel(true);
  };
  window.bichoGrafico = () => {
    if (window.bicho && window.bicho.abrirPainel) window.bicho.abrirPainel(false);
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
    if (!r) { fala("Já achei 4 baús hoje. Amanhã eu procuro mais! 🎁"); return; }
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
                      `Um baú apareceu por aqui… 🎁 ${it.ico} ${it.nome} para mim!`,
                      `Ganhei: ${it.ico} ${it.nome}! Que sorte!`]));
      }
    } else {
      fala(`Outro ${it.ico} ${it.nome}? Repetido! Virou 🪙 ${r.moedas} moeda${r.moedas > 1 ? "s" : ""}. 💰`);
    }
  };
  // 💎 a galeria (moedeiro + raros em destaque + coleção) vive em premios.js
  window.bichoColecao = () => {
    if (window.premiosGaleria) { window.premiosGaleria(); return; }
    fala("Minha galeria não carregou… 🥺");
  };

  /* ── menu: o bicho dirige a aba Pet — UMA ação, UM botão ─────────────────────
     22/10/2026: o menu virou dock com abas (main.js, MENU). Conversar e E-mail
     foram para a aba Recursos; aqui ficam só os cuidados e o estado DELE. */
  function repintaMenu() {
    try {
      if (typeof MENU === "undefined") return;
      MENU.pet = [
        ["alimentar", "Alimentar", () => window.bichoAlimentar()],
        ["agua", "Água", () => window.bichoAgua()],
        ["exercicio", "Exercício", () => window.bichoExercicio()],
        ["brincar", "Brincar", () => window.bichoBrincar()],
        ["dormir", "Dormir", () => window.bichoDormirMenu()],
        ["status", "Como está", () => window.bichoGrafico()],
        ["colecao", "Coleção", () => window.bichoColecao()],
        ["cuidar", "Cuidar dele", () => window.bicho.abrirPainel(true)],
      ];
    } catch (e) { /* menu ainda não existe */ }
  }

  /* ── cabeçalho do menu: nome, humor e o que ele está pedindo ─────────────────
     `pendente` é o ícone do botão que ganha o pontinho de "pedindo agora". */
  window.bichoResumo = () => {
    const e = garante(), c = cfgB(), nome = c.nome || "Xodó";
    if (c.dormirAte > Date.now()) {
      const ate = new Date(c.dormirAte).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });
      return { nome, linha: "Dormindo até " + ate, pendente: "" };
    }
    const h = e.humor || 80;
    const humor = h >= 70 ? "Feliz" : h >= 40 ? "Tranquilo" : "Meio para baixo";
    const nec = [
      ["agua", e.sede, "Com sede", "Com um pouco de sede"],
      ["alimentar", e.fome, "Com fome", "Com um pouco de fome"],
      ["", e.carinho, "Querendo carinho", "Com saudade de você"],
      ["exercicio", e.exercicio, "Precisando se mexer", "Meio parado"],
    ].sort((a, b) => a[1] - b[1]);
    const [ico, valor, forte, leve] = nec[0];
    if (valor < 40) return { nome, linha: humor + " · " + forte, pendente: ico };
    if (valor < 60) return { nome, linha: humor + " · " + leve, pendente: "" };
    return { nome, linha: humor + " · Tudo em dia", pendente: "" };
  };

  /* ── tarefa concluída: balão quando o chat está fechado ──────────────────── */
  // (a linha "✅ tarefa concluída" no chat saiu em 22/10/2026, pedido da dona: poluía a
  // conversa. A contagem de tarefas e o baú com o chat fechado continuam.)
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
        if (cfgB().dormirAte > Date.now() || cfgB().foco) return;   // dormindo ou em foco: sem baú
        const h = new Date().toISOString().slice(0, 10);
        if (e.achadoDia.dia !== h) e.achadoDia = { dia: h, n: 0 };
        if (e.achadoDia.n < 4 && Math.random() < 0.06) window.bichoAchar();
      } catch (err) { /* ignora */ }
    }, 5 * MIN);
  } catch (e) { /* ignora */ }
})();
