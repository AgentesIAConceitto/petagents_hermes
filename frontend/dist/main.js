/* PetAgent — lógica do boneco (MVP v0.1) */
/* Bindings do Wails chegam DEPOIS do parse deste script — por isso são preenchidos
   em esperarBindings(). Ler aqui no topo (como estava) deixava G=null e nenhum clique
   funcionava: sem retângulos, o Go mantinha a janela em click-through total. */
let G = null;
let R = null;

const pet = document.getElementById("pet");
const balao = document.getElementById("balao");
const msgs = document.getElementById("msgs");
const txt = document.getElementById("txt");
const menu = document.getElementById("menu");
const telaPanel = document.getElementById("tela");
const dot = document.getElementById("dot");
const dotSub = document.getElementById("balao-sub");

let estado = { state: "idle", detail: "", heartbeat: -1, balance: "", agent: "hermes" };
let hideTimer = null, balaoTimer = null, arrastando = false, ultimoMenu = 0;

/* ───────── util ───────── */
const DPR = () => window.devicePixelRatio || 1;

function log(m) { try { G && G.LogMsg(m); } catch (e) {} }

/* informa ao Go quais retângulos NÃO devem deixar o clique atravessar (pixels físicos).
   Era uma LISTA FIXA de elementos e os painéis do bicho (#bicho, #bichoGraf,
   #bichoCol) ficaram de fora: o clique neles atravessava para a área de trabalho,
   então o X não fechava nada — só funcionava quando outro painel registrado (o
   balão, que abre por cima) caía sobre o X (relatado pelo dono, 03/10/2026).
   Agora varre o DOM pelas classes de superfície: painel novo entra sozinho. */
const SUPERFICIES = ".balao, .menu, .bicho, .pets, .email, .tela, .cg, .toast, .premio";
function reportarHit() {
  if (!G) return;
  const d = DPR(), rs = [];
  for (const el of [pet, ...document.querySelectorAll(SUPERFICIES)]) {
    if (!el || el.classList.contains("oculto") || el.classList.contains("escondido")) continue;
    // o boneco fica SEMPRE registrado: com a interação desligada o clique esquerdo
    // é ignorado pelos handlers, mas o botão direito precisa continuar vivo
    const r = el.getBoundingClientRect();
    if (r.width < 2 || r.height < 2) continue;
    rs.push({
      L: Math.round(r.left * d), T: Math.round(r.top * d),
      R: Math.round(r.right * d), B: Math.round(r.bottom * d),
    });
  }
  try { G.SetHitRects(JSON.stringify(rs)); } catch (e) {}
}

/* ───────── posição (borda + offset), perfil e interação ─────────
   A janela agora COBRE a tela inteira: o boneco anda por ela toda e encaixa
   em qualquer borda. Posição guardada como {borda, offset} — não como x/y. */
const PERFIS = {
  calmo:      { respira: 3600, salto: 0.45, vagueia: 24000, andar: 0.5, rotulo: "😌 calmo" },
  agitado:    { respira: 1500, salto: 1.0,  vagueia: 7000,  andar: 1.0, rotulo: "⚡ agitado" },
  brincalhao: { respira: 2300, salto: 0.8,  vagueia: 12000, andar: 0.8, rotulo: "🤹 brincalhão" },
};
const cfg = { borda: "baixo", offset: 120, perfil: "calmo", interagir: true, monitor: 1, dexOn: true };
const ORDEM_PERFIL = ["calmo", "agitado", "brincalhao"];

function aplicaPerfil() {
  const p = PERFIS[cfg.perfil] || PERFIS.calmo;
  document.documentElement.style.setProperty("--respira", p.respira + "ms");
  document.documentElement.style.setProperty("--salto", String(p.salto));
  clearInterval(vagueiaTimer);
  vagueiaTimer = setInterval(vagueia, p.vagueia);
}

function aplicaInteracao() {
  document.body.classList.toggle("sem-interacao", !cfg.interagir);
  // NÃO zerar pointer-events: o BOTÃO DIREITO precisa continuar chegando aqui, senão
  // o menu — única porta de volta — fica inalcançável e só resta reiniciar o app
  // (o dono caiu nessa em 03/10/2026: "cliquei em interação e o botão direito sumiu").
  // O que muda é o comportamento: com interação desligada o boneco ignora clique
  // esquerdo, arrasto e duplo clique (ver os handlers mais abaixo).
  if (!cfg.interagir) { fecharBalao(); fecharMenu(); }
  reportarHit();
}

/* interagindo() é global de propósito: o bicho.js checa antes de reagir a clique
   (lá existe um `cfg` próprio, que sombrearia o do main.js). */
function interagindo() { return !!cfg.interagir; }

/* Volta completa CONTORNANDO AS BORDAS da tela, no sentido horário, terminando
   exatamente na base de onde saiu. Feito em JS de propósito: o keyframe `voltaTela`
   somava `100vw`/`100vh` À POSIÇÃO do boneco, o que só funciona se ele estiver no
   canto 0,0 — com dois monitores e DPI 1,5 ele saía da área visível ("some da tela
   e volta caindo de cima", relatado em 03/10/2026).
   Sentido horário a partir da base: canto inferior-esquerdo → sobe pela ESQUERDA →
   atravessa o TOPO → desce pela DIREITA → volta pela BASE.
   O tempo é distribuído por distância, então a velocidade fica parecida nos trechos. */
function voltaNaTela(ms, aoTerminar) {
  if (!pet) return;
  const pw = pet.offsetWidth || 120, ph = pet.offsetHeight || 120, m = 4;
  const x0 = pet.offsetLeft, y0 = pet.offsetTop;
  const xMax = Math.max(m, innerWidth - pw - m), yMax = Math.max(m, innerHeight - ph - m);
  const caminho = [
    { x: m, y: yMax }, { x: m, y: m }, { x: xMax, y: m }, { x: xMax, y: yMax }, { x: x0, y: y0 },
  ];
  const trechos = [];
  let total = 0, atual = { x: x0, y: y0 };
  for (const p of caminho) {
    const d = Math.abs(p.x - atual.x) + Math.abs(p.y - atual.y);
    trechos.push({ de: atual, para: p, d });
    total += d; atual = p;
  }
  if (total < 8) { if (aoTerminar) aoTerminar(); return; }
  const dur = ms || 22000, t0 = performance.now();
  andar(true);
  const passo = () => {
    const k = Math.min(1, (performance.now() - t0) / dur);
    let sobra = total * k, ponto = { x: x0, y: y0 }, dirX = 1;
    for (const s of trechos) {
      if (sobra <= s.d || s === trechos[trechos.length - 1]) {
        const f = s.d ? Math.min(1, sobra / s.d) : 1;
        ponto = { x: s.de.x + (s.para.x - s.de.x) * f, y: s.de.y + (s.para.y - s.de.y) * f };
        dirX = s.para.x - s.de.x;      // sentido do trecho atual (o boneco contorna a tela)
        break;
      }
      sobra -= s.d;
    }
    andar(true, dirX < 0 ? -1 : 1);    // troca a LINHA conforme o sentido (dexToca não reinicia à toa)
    pet.style.left = Math.round(ponto.x) + "px";
    pet.style.top = Math.round(ponto.y) + "px";
    if (k < 1) { requestAnimationFrame(passo); return; }
    pet.style.left = x0 + "px"; pet.style.top = y0 + "px";   // fecha exato na origem
    andar(false);
    if (aoTerminar) { try { aoTerminar(); } catch (e) { } }
  };
  requestAnimationFrame(passo);
}

// posição a partir de {borda, offset}
function aplicaPosicao() {
  const W = innerWidth, H = innerHeight, m = 6;
  const pw = pet.offsetWidth || 120, ph = pet.offsetHeight || 120;
  let left = cfg.offset, top = H - ph - m; // padrão: borda de baixo (acima da barra)
  if (cfg.borda === "cima")          { left = cfg.offset; top = m; }
  else if (cfg.borda === "esquerda") { left = m; top = cfg.offset; }
  else if (cfg.borda === "direita")  { left = W - pw - m; top = cfg.offset; }
  // encaixa em pixel físico inteiro: posição sub-pixel faz o navegador reamostrar
  // o sprite e volta a serrilhar (mesmo com o desenho 1:1)
  const dpr = window.devicePixelRatio || 1;
  const snap = (v) => Math.round(v * dpr) / dpr;
  pet.style.left = snap(Math.max(0, Math.min(W - pw, left))) + "px";
  pet.style.top  = snap(Math.max(0, Math.min(H - ph, top))) + "px";
  reportarHit();
}

// ao soltar o arrasto: encaixa na borda mais próxima (regra do dono)
function encaixa() {
  const W = innerWidth, H = innerHeight;
  const pw = pet.offsetWidth, ph = pet.offsetHeight;
  const cx = pet.offsetLeft + pw / 2, cy = pet.offsetTop + ph / 2;
  const d = { baixo: H - cy, cima: cy, esquerda: cx, direita: W - cx };
  let borda = "baixo";
  for (const k in d) { if (d[k] < d[borda]) borda = k; }
  cfg.borda = borda;
  cfg.offset = Math.round((borda === "cima" || borda === "baixo") ? cx - pw / 2 : cy - ph / 2);
  cfg.offset = Math.max(6, cfg.offset);
  pet.style.transition = "";
  aplicaPosicao(); salvarPet();
  toast("📌", "Encaixado", "borda " + borda + " · " + cfg.offset + " px", 2000, true);
}

// vagueia — anda um pouco e para; ritmo depende do PERFIL
function vagueia() {
  // NÃO vagueia durante a volta na tela / brincadeira: vaguear mexe na BASE
  // (pet.left/top), e aí a volta deixaria de terminar no lugar de origem
  // (medido 03/10/2026 — o perfil "calmo" vagueia a cada 24 s e atropelava os 22 s
  // da volta).
  if (emEfeito()) return;
  const p = PERFIS[cfg.perfil] || PERFIS.calmo;
  const passo = Math.round((40 + Math.random() * 90) * p.andar) * (Math.random() < 0.5 ? -1 : 1);
  const W = innerWidth, H = innerHeight, pw = pet.offsetWidth, ph = pet.offsetHeight;
  pet.classList.add("andando");
  if (cfg.borda === "cima" || cfg.borda === "baixo") {
    const novo = Math.max(6, Math.min(W - pw - 6, pet.offsetLeft + passo));
    pet.style.transition = "left 1.7s ease-in-out"; pet.style.left = novo + "px"; cfg.offset = novo;
  } else {
    const novo = Math.max(6, Math.min(H - ph - 6, pet.offsetTop + passo));
    pet.style.transition = "top 1.7s ease-in-out"; pet.style.top = novo + "px"; cfg.offset = novo;
  }
  setTimeout(() => {
    pet.classList.remove("andando"); pet.style.transition = "";
    salvarPet(); reportarHit();
  }, 1900);
}

function salvarPet() {
  try { G && G.SavePet(JSON.stringify({ agent: "hermes", cfg: cfg })); } catch (e) {}
}
async function carregarPet() {
  try {
    const s = G ? await G.LoadPet() : "{}";
    const d = JSON.parse(s || "{}");
    if (d && d.cfg) {
      Object.assign(cfg, d.cfg);
    } else if (d && typeof d.left === "number") {
      // formato antigo (x/y da janelinha) → migra para borda+offset
      cfg.borda = d.top > innerHeight / 2 ? "baixo" : "cima";
      cfg.offset = Math.max(6, d.left);
    }
  } catch (e) {}
  aplicaPerfil(); aplicaInteracao(); aplicaPosicao();
  carregarPetDex(); // boneco da galeria (Petdex), se houver um instalado
}

/* ───────── PERFIL e INTERAÇÃO ───────── */
function cicloPerfil() {
  const i = ORDEM_PERFIL.indexOf(cfg.perfil);
  cfg.perfil = ORDEM_PERFIL[(i + 1) % ORDEM_PERFIL.length];
  aplicaPerfil(); salvarPet();
  toast("🎭", "Perfil: " + PERFIS[cfg.perfil].rotulo, "ritmo das animações ajustado", 2600, true);
}

function alternaInteracao() {
  const ligando = !cfg.interagir;
  // avisa ANTES de desligar: toast() obedece cfg.interagir e não apareceria depois
  if (!ligando) {
    toast("🖱", "Interação desligada",
      "o boneco ignora cliques; botão direito nele reabre este menu", 5000, true);
  }
  cfg.interagir = ligando;
  aplicaInteracao(); salvarPet();
  if (ligando) {
    toast("🖱", "Interação ligada", "pode me clicar, arrastar e conversar", 2600, true);
  }
}

/* ───────── boneco do Petdex (galeria pública do Hermes) ─────────
   Formato medido: pets/<slug>/{pet.json, spritesheet.webp} = folha 8x9 de
   frames 192x208. Linhas: 0=repouso, 1/2=andando, 4=pulinho, 5=caído, 6=falando. */
let dex = null, dexTimer = null, dexLinha = 0, dexFrame = 0, dexFrames = [];

// Mede, célula por célula, quais frames têm desenho de verdade. Várias folhas do
// Petdex têm células vazias no meio da linha — e aí o boneco "pisca"/some quando
// o ciclo chega nelas. Aqui a gente só anda pelos frames que existem.
async function medirFrames(url, fw, fh, cols, linhas) {
  const img = new Image();
  img.src = url;
  await img.decode();
  const cv = document.createElement("canvas");
  cv.width = img.width; cv.height = img.height;
  const cx = cv.getContext("2d", { willReadFrequently: true });
  cx.drawImage(img, 0, 0);
  const mapa = [];
  for (let l = 0; l < linhas; l++) {
    const validos = [];
    for (let c = 0; c < cols; c++) {
      const d = cx.getImageData(c * fw, l * fh, fw, fh).data;
      let soma = 0, n = 0;
      for (let i = 3; i < d.length; i += 4 * 5) { soma += d[i]; n++; }  // amostra do alfa
      if (n && (soma / n) > 6) validos.push(c);                        // célula com conteúdo
    }
    mapa.push(validos.length ? validos : [0]);
  }
  return mapa;
}
const DEX_LINHA = { idle: 0, dormindo: 0, andando: 1, andando_esq: 2, comemorando: 4, carinho: 4, erro: 5, falando: 6, pensando: 6 };
const dexEl = document.getElementById("petdex");

// classes de EFEITO com vida própria (movimento longo). O Go emite `pet-state` a
// cada 15 s, e classePet reescreve o className INTEIRO — sem esta guarda, a volta
// na tela de 22 s morreria no meio (medido: o loop de estado dorme 15 s em app.go).
const EFEITOS = ["volta", "brincando"];
// um efeito de movimento longo está no comando (volta na tela / brincadeira)
const emEfeito = () => EFEITOS.some((c) => pet.classList.contains(c));
// toda troca de estado passa por aqui: classe + animação do boneco
function classePet(est) {
  const e = est || "idle";
  if (e === "idle" && emEfeito()) return; // efeito manda
  pet.className = "pet " + e + (dex && cfg.dexOn ? " dex" : "");
  if (dex && cfg.dexOn) dexToca(e);
}

async function carregarPetDex() {
  if (!G || !G.PetDexAtual) return;
  let d = null;
  try { d = JSON.parse(await G.PetDexAtual()); } catch (e) { return; }
  if (!d || !d.ok) { if (d && d.erro) console.warn("petdex:", d.erro); return; }
  dex = d;
  // A folha vai direto (sem reprocessar): o suavizado é feito por CSS (filter no
  // #petdex). Gerar um PNG em base64 aqui estourava com folhas grandes (homelander,
  // 2,1 MB) e o boneco simplesmente não aparecia.
  dexEl.style.backgroundImage = "url(" + d.sheet + ")";
  if (cfg.dexOn) {
    try { dexFrames = await medirFrames(d.sheet, d.fw, d.fh, d.cols, d.linhas); } catch (e) { dexFrames = []; }
    pet.classList.add("dex");
    dexLinha = -1; clearInterval(dexTimer); dexTimer = null; // força reiniciar o ciclo
    dexToca(estado.state || "idle");
    aplicaPosicao();
  }
  console.log("petdex pronto:", d.slug, "|", d.nome, "| frame", d.fw + "x" + d.fh, "| frames por linha", JSON.stringify(dexFrames));
  if (G && G.Log) {
    try {
      G.Log("petdex frames por linha: " + JSON.stringify(dexFrames) +
            " | dpr=" + (window.devicePixelRatio || 1) +
            " | cliente css=" + innerWidth + "x" + innerHeight +
            " | tela fisica=" + screen.width + "x" + screen.height);
    } catch (e) {}
  }
}

// (o suavizado da arte é feito por CSS no #petdex — ver style.css. Reprocessar a
// folha em canvas/PNG estourava com folhas grandes e o boneco sumia.)

function dexToca(est) {
  if (!dex) return;
  const linha = DEX_LINHA[est] != null ? DEX_LINHA[est] : 0;
  // mesma linha: NÃO reinicia o ciclo (reiniciar a cada atualização de estado
  // fazia o boneco engasgar/piscar no meio da animação)
  if (linha === dexLinha && dexTimer) return;
  dexLinha = linha; dexFrame = 0; dexPinta();
  clearInterval(dexTimer);
  const ms = (est === "idle" || est === "dormindo") ? 170 : 115;
  dexTimer = setInterval(dexAvanca, ms);
}

function dexAvanca() {
  const cols = (dexFrames[dexLinha] && dexFrames[dexLinha].length) ? dexFrames[dexLinha] : null;
  if (cols) {
    const i = cols.indexOf(dexFrame);
    dexFrame = cols[(i + 1) % cols.length];
  } else {
    dexFrame = (dexFrame + 1) % dex.cols;
  }
  dexPinta();
}

function dexPinta() {
  if (!dex) return;
  // pixel exato: 1 pixel do sprite = 1 pixel físico da tela (senão reamostra e serrilha)
  const dpr = window.devicePixelRatio || 1;
  const cw = dex.fw / dpr, ch = dex.fh / dpr;
  pet.style.width = cw + "px"; pet.style.height = ch + "px";
  dexEl.style.backgroundSize = (dex.cols * cw) + "px " + (dex.linhas * ch) + "px";
  dexEl.style.backgroundPosition = (-dexFrame * cw) + "px " + (-dexLinha * ch) + "px";
}

/* andar(sim, dir) — liga/desliga a animação de CAMINHAR, no SENTIDO certo.
   ⚠️ 04/10/2026, dois defeitos medidos aqui:
   1) `pet.classList.add("andando")` sozinho NÃO troca a linha do sprite — quem
      troca é o dexToca. O boneco atravessava a tela DESLIZANDO na pose parada.
   2) A folha tem UMA LINHA POR SENTIDO: olhando a folha do batmeme, a linha 1
      corre inclinada para a direita e a linha 2 é o mesmo ciclo para a esquerda.
      Travar na linha 1 fazia ele correr "de costas" indo para a esquerda.
   3) dir < 0 = indo para a esquerda. Se a folha do boneco não tiver a linha 2
      (alguns não têm), cai de volta na linha 1 em vez de sumir. */
let ultimoAndarLog = "";
function andar(sim, dir) {
  try {
    const esq = (dir !== undefined && dir !== null && dir < 0);
    const temLinhaEsq = !!(dex && dexFrames[2] && dexFrames[2].length);
    if (sim) {
      pet.classList.add("andando");
      if (dex && cfg.dexOn) dexToca((esq && temLinhaEsq) ? "andando_esq" : "andando");
    } else {
      pet.classList.remove("andando");
      if (dex && cfg.dexOn) dexToca((estado && estado.state) || "idle");
    }
    // prova externa (uma linha por MUDANÇA, não a cada frame): fica no petagent.log
    const marca = (sim ? "ligado" : "desligado") + "|" + (esq ? "esq" : "dir");
    if (marca !== ultimoAndarLog) {
      ultimoAndarLog = marca;
      try {
        if (window.go && window.go.main && window.go.main.App && window.go.main.App.Log) {
          window.go.main.App.Log("andar: " + marca + " | petdex=" + (dex ? "sim" : "nao") +
            " | linha=" + dexLinha + " | classe=" + pet.className);
        }
      } catch (e) { }
    }
  } catch (e) { }
}
window.petAndar = andar;   // o bicho (bicho.js) usa nos passeios e nas voltas

/* petBipe() — sininho suave quando o bicho fala (pedido do dono, 04/10/2026):
   "quase um beep, mas mais suave", para ele saber que chegou recado mesmo sem
   olhar a tela. Sintezizado aqui (Web Audio): nada de arquivo de áudio para
   empacotar, e dá para afinar o timbre no código.
   ⚠️ O navegador/WebView2 só libera áudio depois do primeiro toque/clique na
   página (política de autoplay). Por isso: (a) guardo um AudioContext só meu e
   (b) retomo ele no primeiro pointerdown. Se o app acabou de subir e ninguém
   clicou nada ainda, o primeiro recado pode sair mudo — do segundo em diante toca. */
let somCtx = null, somUltimo = 0;
function petBipe(volume) {
  try {
    const AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return;
    // antirrajada (350 ms): um duplo clique dispara click + click + dblclick em
    // ~250 ms — com 120 ms ele sairia com 3 toques. As ações de verdade (comer,
    // beber, carinho) estão a segundos de distância, então não perdem nada.
    const agoraMs = (window.performance && performance.now) ? performance.now() : Date.now();
    if (agoraMs - somUltimo < 350) return;
    somUltimo = agoraMs;
    // rastro: diz no petagent.log que o som saiu (senão "não ouvi" é indecidível)
    try {
      if (window.go && window.go.main && window.go.main.App && window.go.main.App.Log) {
        window.go.main.App.Log("bipe: 2 toques (880/1175 Hz)");
      }
    } catch (e) { }
    if (!somCtx) somCtx = new AC();
    if (somCtx.state === "suspended") { try { somCtx.resume(); } catch (e) { } }
    const vol = (volume === undefined || volume === null) ? 0.07 : volume;
    const t0 = somCtx.currentTime;
    // dois toques curtos (880 → 1174 Hz): intervalo suave, sem estridência
    const notas = [[880, 0], [1174.66, 0.115]];
    for (const n of notas) {
      const o = somCtx.createOscillator(), g = somCtx.createGain();
      o.type = "sine";
      o.frequency.value = n[0];
      const t = t0 + n[1];
      g.gain.setValueAtTime(0.0001, t);
      g.gain.exponentialRampToValueAtTime(vol, t + 0.018);     // ataque macio
      g.gain.exponentialRampToValueAtTime(0.0001, t + 0.17);   // cauda curta
      o.connect(g); g.connect(somCtx.destination);
      o.start(t); o.stop(t + 0.19);
    }
  } catch (e) { /* sem áudio: o balão continua aparecendo */ }
}
window.petBipe = petBipe;
try {
  document.addEventListener("pointerdown", function () {
    try { if (somCtx && somCtx.state === "suspended") somCtx.resume(); } catch (e) { }
  });
  document.addEventListener("keydown", function () {
    try { if (somCtx && somCtx.state === "suspended") somCtx.resume(); } catch (e) { }
  });
} catch (e) { }

function alternaBoneco() {
  if (!dex) { carregarPetDex(); toast("🐾", "Buscando boneco da galeria…", "Petdex", 2400, true); return; }
  cfg.dexOn = !cfg.dexOn;
  pet.classList.toggle("dex", !!cfg.dexOn);
  if (cfg.dexOn) { toast("🐾", "Boneco da galeria: " + dex.nome, "as linhas viram as emoções dele", 2600, true); dexToca(estado.state || "idle"); }
  else { clearInterval(dexTimer); pet.style.width = ""; pet.style.height = ""; toast("🐾", "Boneco vetor (nosso)", "", 2400, true); }
  salvarPet(); reportarHit();
}

/* ───────── painel de bonecos (galeria Petdex) ─────────
   Escolhe o boneco sem sair do app: mostra os instalados (miniatura servida pelo
   próprio app em 127.0.0.1) e busca no catálogo do Petdex; clicar num que ainda
   não está na máquina baixa os 2 arquivos e já aplica. */
const petPanelEl = document.getElementById("petPanel");
let petsBase = "", petPanelTermo = "", petPanelOffset = 0;

function posicionaJunto(el, largura) {
  const r = pet.getBoundingClientRect();
  const larg = el.offsetWidth || largura, alt = el.offsetHeight || 420;
  let x = r.left + 14, y = r.top - alt - 14;
  if (y < 10) y = r.bottom + 14;
  if (x + larg > innerWidth - 10) x = Math.max(10, innerWidth - larg - 10);
  if (y + alt > innerHeight - 10) y = Math.max(10, innerHeight - alt - 10);
  el.style.left = x + "px"; el.style.top = y + "px";
}

async function abrirPainelBonecos(termo) {
  petPanelEl.classList.remove("oculto");
  posicionaJunto(petPanelEl, 430);
  await listaBonecos(termo === undefined ? "" : termo); // abre mostrando os meus
  reportarHit();
}
function fecharPainelBonecos() { petPanelEl.classList.add("oculto"); reportarHit(); }

async function listaBonecos(termo, offset) {
  if (termo !== undefined) { petPanelTermo = termo || ""; petPanelOffset = 0; }
  if (offset !== undefined) petPanelOffset = offset;
  document.getElementById("pets-termo").value = petPanelTermo === "*" ? "" : petPanelTermo;
  const alvo = document.getElementById("pets-lista");
  alvo.innerHTML = '<div class="aviso">carregando… (na primeira busca o catálogo da galeria pode levar alguns segundos)</div>';
  let d = null;
  try { d = JSON.parse(await G.PetGaleria(petPanelTermo, petPanelOffset)); } catch (e) {}
  if (!d || !d.ok) {
    alvo.innerHTML = '<div class="aviso">' + ((d && d.erro) || "sem resposta do app") + '</div>';
    document.getElementById("pets-conta").textContent = "";
    return;
  }
  petsBase = d.base || petsBase;
  document.getElementById("pets-atual").textContent =
    "· usando: " + ((dex && cfg.dexOn) ? dex.slug : "vetor (nosso)");
  const n = (d.itens || []).length;
  document.getElementById("pets-conta").textContent =
    petPanelTermo === "" ? (d.total + " bonecos na sua máquina · a galeria tem 4.873")
    : petPanelTermo === "*" ? ("galeria completa · " + d.total + " bonecos · mostrando de " + (petPanelOffset + 1) + " a " + (petPanelOffset + n))
    : (d.total + " encontrados · " + (n ? "mostrando " + n : "nenhum"));
  document.getElementById("pets-mais").style.display = d.tem_mais ? "" : "none";
  alvo.innerHTML = "";
  const cartao = (slug, nome, tipo, url, sel, local) => {
    const c = document.createElement("div");
    c.className = "card" + (sel ? " sel" : "");
    c.title = nome + (tipo ? " · " + tipo : "") + (local ? "" : " — será baixado da galeria");
    const t = document.createElement("div");
    t.className = "thumb";
    if (url) t.style.backgroundImage = "url(" + url + ")";
    const nn = document.createElement("div");
    nn.className = "nome"; nn.textContent = nome;
    const u = document.createElement("div");
    u.className = "usar";
    u.textContent = local ? (sel ? "em uso" : "usar") : "baixar e usar";
    c.appendChild(t); c.appendChild(nn);
    if (tipo) {
      const ty = document.createElement("div");
      ty.className = "tipo"; ty.textContent = tipo;
      c.appendChild(ty);
    }
    c.appendChild(u);
    c.onclick = () => usarBoneco(slug, local);
    alvo.appendChild(c);
  };
  if (!petPanelTermo) cartao("__vetor__", "vetor (nosso)", "sem download", "", !cfg.dexOn, true);
  for (const it of (d.itens || [])) {
    const local = !!it.local && !!petsBase;
    const url = local ? (petsBase + "/sheet/" + it.slug) : (it.sprite || "");
    cartao(it.slug, it.nome || it.slug, it.tipo, url,
           !!(dex && cfg.dexOn && dex.slug === it.slug), local);
  }
  if (!alvo.children.length) alvo.innerHTML = '<div class="aviso">nenhum boneco encontrado</div>';
}

async function usarBoneco(slug, local) {
  if (!G) return;
  if (slug === "__vetor__") {
    cfg.dexOn = false; clearInterval(dexTimer);
    pet.classList.remove("dex"); pet.style.width = ""; pet.style.height = "";
    salvarPet(); aplicaPosicao();
    toast("🐾", "Boneco vetor (nosso)", "", 2400, true);
    fecharPainelBonecos(); // volta a mostrar o boneco, como o usuário espera
    return;
  }
  if (!local) {
    toast("⬇", "Baixando " + slug + "…", "da galeria Petdex", 3200, true);
    let r = null;
    try { r = JSON.parse(await G.PetDexBaixar(slug)); } catch (e) {}
    if (!r || !r.ok) { toast("⚠️", "Download falhou", (r && r.erro) || "sem rede?", 3600, true); return; }
  }
  try { await G.PetDexEscolher(slug); } catch (e) {}
  cfg.dexOn = true; salvarPet();
  await carregarPetDex();
  toast("🐾", "Boneco: " + slug, "aplicado", 2200, true);
  fecharPainelBonecos(); // escolheu → fecha e mostra o boneco novo
}

document.getElementById("pets-buscar").onclick = () => listaBonecos(document.getElementById("pets-termo").value);
document.getElementById("pets-termo").addEventListener("keydown", (e) => {
  if (e.key === "Enter") listaBonecos(e.target.value);
});
document.getElementById("pets-instalados").onclick = () => listaBonecos("");
document.getElementById("pets-galeria").onclick = () => listaBonecos("*");
document.getElementById("pets-bichinhos").onclick = () => listaBonecos("creature");
document.getElementById("pets-personagens").onclick = () => listaBonecos("character");
document.getElementById("pets-mais").onclick = () => listaBonecos(undefined, petPanelOffset + 24);
document.getElementById("pets-vetor").onclick = () => usarBoneco("__vetor__", true);
document.getElementById("pets-fechar").onclick = () => fecharPainelBonecos();
document.getElementById("pets-x").onclick = () => fecharPainelBonecos();

/* ───────── estado do boneco ───────── */
function aplicarEstado(s) {
  estado = Object.assign(estado, s || {});
  classePet(estado.state || "idle");
  const mapa = { idle: "", pensando: "pensando", dormindo: "dormindo", erro: "erro", falando: "pensando" };
  dot.className = "dot " + (mapa[estado.state] || "");
  dotSub.textContent = [estado.detail, estado.balance].filter(Boolean).join(" · ");
  destacarPendencia();
  reportarHit();
}

/* ───────── balão (conversa) ─────────
   A conversa é UMA só: o balão e o chat estendido desenham a mesma lista. */
const conversa = [];
let ocupado = false; // true enquanto o Hermes está pensando

// O agente responde em markdown; aqui vai um subconjunto seguro (escapa antes)
function formata(t) {
  let s = (t || "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  s = s.replace(/\*\*([^*\n]+)\*\*/g, "<b>$1</b>");
  s = s.replace(/`([^`\n]+)`/g, "<code>$1</code>");
  s = s.replace(/^#{1,4} (.*)$/gm, "<b>$1</b>");
  return s;
}

function msg(texto, quem = "ele") {
  conversa.push({ quem: quem, texto: texto });
  // a conversa sobrevive ao reiniciar o app (o Hermes pode responder depois de ele
  // já ter sido fechado/reaberto — foi o que aconteceu em 02/10/2026)
  try { localStorage.setItem("conversa", JSON.stringify(conversa.slice(-60))); } catch (e) {}
  pintaConversa();
}

/* Levar a conversa SEMPRE para a última mensagem — no balão E no chat estendido.
   Detalhe que fazia o chat abrir no INÍCIO (relatado pelo dono, 03/10/2026):
   enquanto o elemento está `display:none`, `scrollHeight` é 0, então rolar durante
   a pintura não pega. Aqui rolamos de novo logo depois de o balão aparecer (o
   callback do frame e o timeout pegam o layout já pronto). */
function rolarAoFim() {
  const alvos = [msgs, document.getElementById("cg-msgs")];
  const fazer = () => { for (const a of alvos) if (a) a.scrollTop = a.scrollHeight; };
  fazer();
  requestAnimationFrame(fazer);
  setTimeout(fazer, 60);
}

function pintaConversa() {
  const alvos = [msgs, document.getElementById("cg-msgs")];
  for (const alvo of alvos) {
    if (!alvo) continue;
    alvo.innerHTML = "";
    for (const m of conversa) {
      const el = document.createElement("div");
      el.className = "m " + m.quem;
      el.innerHTML = formata(m.texto);
      if (m.quem !== "meta") { // linhas de status não ganham botão de copiar
        const cp = document.createElement("span");
        cp.className = "copiar";
        cp.textContent = "⧉";
        cp.title = "copiar esta mensagem";
        cp.onclick = (ev) => { ev.stopPropagation(); copiarTexto(m.texto, cp); };
        el.appendChild(cp);
      }
      alvo.appendChild(el);
    }
  }
  rolarAoFim();   // antes era `alvo.scrollTop` por alvo: não pegava com o balão oculto
  ancoraBalao();
}

/* Copiar mensagem: a API de clipboard exige contexto seguro e no WebView2 nem
   sempre está disponível, então há reserva com textarea + execCommand. */
function copiarTexto(t, el) {
  const ok = () => {
    const antes = el.textContent;
    el.textContent = "✓";
    el.classList.add("copiado");
    setTimeout(() => { el.textContent = antes; el.classList.remove("copiado"); }, 1200);
  };
  const reserva = () => {
    const ta = document.createElement("textarea");
    ta.value = t;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand("copy"); ok(); } catch (e) { /* sem clipboard */ }
    document.body.removeChild(ta);
  };
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(t).then(ok).catch(reserva);
  } else {
    reserva();
  }
}

// o balão é ancorado pelo RODAPÉ: cresce para cima, nunca por cima do pet
function ancoraBalao() {
  if (!balao || balao.classList.contains("oculto")) return;
  const r = pet.getBoundingClientRect();
  const larg = balao.offsetWidth || 420;
  balao.style.top = "";
  balao.style.bottom = Math.max(10, innerHeight - r.top + 18) + "px";
  let x = r.left + 20;
  if (x + larg > innerWidth - 10) {
    x = Math.max(10, innerWidth - larg - 10);
    balao.classList.add("flip");
  }
  balao.style.left = x + "px";
}

function abrirBalao(posicionar = true) {
  balao.classList.remove("oculto");
  if (posicionar) ancoraBalao();
  rolarAoFim();   // abrir = cair na ÚLTIMA mensagem, sem arrastar até o fim
  txt.focus();
  reiniciarAuto();
  reportarHit();
}
function fecharBalao() { balao.classList.add("oculto"); reportarHit(); }
function reiniciarAuto() {
  clearTimeout(balaoTimer);
  if (ocupado) return; // esperando o Hermes: NÃO esconde (a resposta chega depois)
  balaoTimer = setTimeout(() => fecharBalao(), 20000); // auto-esconde: 20 s sem interação
}

async function enviarTexto(texto, input) {
  texto = (texto || "").trim();
  if (!texto || ocupado) return;
  input.value = "";
  msg(texto, "eu");
  classePet("pensando");
  msg("⏳ consultando o Hermes…", "meta");
  ocupado = true;                       // esperando resposta: o balão NÃO se esconde
  input.disabled = true;
  input.placeholder = "o Hermes está pensando…";
  reiniciarAuto();
  let resp = "(sem resposta)";
  const t0 = Date.now();
  try { resp = G ? await G.SendChat(texto) : "(fora do Wails)"; } catch (e) { resp = "erro: " + e; }
  const levou = ((Date.now() - t0) / 1000).toFixed(1);
  if (conversa.length && conversa[conversa.length - 1].quem === "meta") conversa.pop();
  ocupado = false;
  input.disabled = false;
  input.placeholder = "Escreva e dê Enter…";
  msg(resp, "ele");
  // marcador honesto: mostra quanto tempo levou (quente = segundos / reserva = ~1 min)
  msg("via Hermes · " + levou + " s", "meta");
  classePet(estado.state || "idle");
  abrirBalao(false);                    // garante o balão aberto para mostrar a resposta
  reiniciarAuto();
  reportarHit();
}

function enviar() { return enviarTexto(txt.value, txt); }

/* ───────── chat estendido (janela arrastável e redimensionável) ─────────
   Abre no centro da tela atual, arrasta pela barra de título e redimensiona pelo
   canto. É a MESMA conversa do balão — só maior. */
const cg = document.getElementById("chatGrande");

function abrirChatGrande() {
  if (!cg) return;
  fecharBalao(); // pedido do dono (02/10/2026): o balão pequeno SAI de cena
  cg.classList.remove("oculto");
  rolarAoFim();   // idem: o chat estendido abre no fim da conversa
  if (!cg.dataset.pos) {
    const larg = Math.max(420, Math.round(innerWidth * 0.62));
    const alt = Math.max(300, Math.round(innerHeight * 0.6));
    cg.style.width = larg + "px";
    cg.style.height = alt + "px";
    cg.style.left = Math.round((innerWidth - larg) / 2) + "px";
    cg.style.top = Math.round((innerHeight - alt) / 2) + "px";
    cg.dataset.pos = "1";
  }
  document.getElementById("cg-txt").focus();
  reportarHit();
}
function fecharChatGrande() {
  if (cg) { cg.classList.add("oculto"); reportarHit(); }
}

(function arrastarChat() { // arrastar pela barra de título
  const topo = document.getElementById("cg-topo");
  if (!topo || !cg) return;
  let d = null;
  topo.addEventListener("mousedown", (e) => {
    if (e.target.id === "cg-x" || e.target.id === "cg-min") return; // botões não arrastam
    d = { x: e.clientX - cg.offsetLeft, y: e.clientY - cg.offsetTop };
    e.preventDefault();
  });
  window.addEventListener("mousemove", (e) => {
    if (!d) return;
    cg.style.left = Math.max(-40, Math.min(innerWidth - 120, e.clientX - d.x)) + "px";
    cg.style.top = Math.max(0, Math.min(innerHeight - 40, e.clientY - d.y)) + "px";
    reportarHit();
  });
  window.addEventListener("mouseup", () => { d = null; });
})();

(function redimensionarChat() { // redimensionar pelo canto
  const puxa = document.getElementById("cg-puxa");
  if (!puxa || !cg) return;
  let p = null;
  puxa.addEventListener("mousedown", (e) => {
    p = { x: e.clientX, y: e.clientY, w: cg.offsetWidth, h: cg.offsetHeight };
    e.preventDefault();
    e.stopPropagation();
  });
  window.addEventListener("mousemove", (e) => {
    if (!p) return;
    cg.style.width = Math.max(380, p.w + (e.clientX - p.x)) + "px";
    cg.style.height = Math.max(280, p.h + (e.clientY - p.y)) + "px";
    reportarHit();
  });
  window.addEventListener("mouseup", () => { p = null; });
})();

document.getElementById("balao-estender").onclick = () => abrirChatGrande();
// voltar ao chat pequeno: fecha o grande e reabre o balão (mesma conversa)
const btMin = document.getElementById("cg-min");
if (btMin) btMin.onclick = () => { fecharChatGrande(); abrirBalao(); };
document.getElementById("cg-x").onclick = () => fecharChatGrande();
document.getElementById("cg-enviar").onclick = () =>
  enviarTexto(document.getElementById("cg-txt").value, document.getElementById("cg-txt"));
document.getElementById("cg-txt").addEventListener("keydown", (e) => {
  if (e.key === "Enter") enviarTexto(e.target.value, e.target);
});
(async () => { // deixa escrito na tela se o chat está mesmo ligado no Hermes
  try {
    const i = JSON.parse(await G.ChatIntegrado());
    document.getElementById("cg-sub").textContent =
      i.ok ? "ligado ao Hermes · sessão " + i.sessao : "Hermes não encontrado";
  } catch (e) {}
})();
try { // recupera a conversa anterior (inclusive resposta que chegou com o balão fechado)
  const c = JSON.parse(localStorage.getItem("conversa") || "[]");
  if (Array.isArray(c) && c.length) {
    for (const m of c) conversa.push(m);
    pintaConversa();
  }
} catch (e) {}

/* ───────── menu (tiles, dirigido por estado) ─────────
   Padrão ANTES do bicho.js entrar: ele sobrescreve as linhas 1/2/3/6 em
   repintaMenu(). Regra: cada ação aparece em UM botão só. */
const TILES = {
  linha1: [
    ["🍖", "Alimentar", () => subAlimentar()],
    ["🎾", "Brincar", () => reagir("Uhu! Vou procurar um tesouro…", "pensando")],
    ["💬", "Conversar", () => { abrirBalao(); txt.focus(); }],
    ["😴", "Dormir", () => dormir(30)],
  ],
  linha2: [
    ["🐾", "Cuidar dele", () => { if (window.bicho) window.bicho.abrirPainel(); }],
    ["💬", "Conversar", () => { abrirBalao(); txt.focus(); }],
  ],
  linha3: [
    ["📊", "Como ele está", () => status()],
    ["💎", "Coleção", () => { if (window.premiosGaleria) window.premiosGaleria(); else msg("A coleção do bicho mora aqui.", "ele"); }],
    ["🖐", "Gestos", () => reagir("Olha eu me mexendo! 🎩", "pensando")],
    ["📧", "E-mail", () => abrirEmail()],
  ],
  linha4: [
    ["🐾", "Boneco", () => abrirPainelBonecos()],
    ["🎭", "Perfil", () => cicloPerfil()],
    ["🖱", "Interação", () => alternaInteracao()],
    ["🖥", "Tela e borda", () => abrirSeletorTela()],
    ["👁", "Esconder 1h", () => esconder(60)],
  ],
  // Linha obrigatória de controle: DESLIGAR sempre visível, sempre no mesmo lugar.
  linha5: [
    ["⏻", "DESLIGAR", () => desligar()],
    ["👁", "Esconder 15min", () => esconder(15)],
    ["⌄", "Recolher janela", () => esconderJanela()],
    ["↺", "Recolher balão", () => fecharBalao()],
  ],
  // linha6 ficou vazia de propósito: era depósito de resto (incl. o botão de
  // debug "Testar balãozinho"). Linha vazia não é renderizada.
  linha6: [],
};

function montarMenu() {
  for (const id of ["linha1", "linha3", "linha4", "linha5", "linha6"]) {
    const box = document.getElementById(id);
    if (!box) continue;
    box.innerHTML = "";
    const lista = TILES[id] || [];
    // linha vazia não ocupa espaço (era o buraco onde sobrava menu velho)
    box.style.display = lista.length ? "" : "none";
    for (const [ico, rotulo, acao] of lista) {
      const b = document.createElement("div");
      b.className = "tile"; b.dataset.acao = rotulo;
      b.innerHTML = `<span class="ico">${ico}</span>${rotulo}`;
      b.onclick = () => { fecharMenu(); acao(); };
      box.appendChild(b);
    }
  }
  montarDestaque();
}

function montarDestaque() {
  const botoes = document.querySelector(".menu-linha.destaque");
  if (!botoes) return;
  botoes.innerHTML = "";
  const lista = TILES.linha2 || [];
  botoes.style.display = lista.length ? "" : "none";
  for (const [ico, rot, fn] of lista) {
    const b = document.createElement("div");
    b.className = "tile grande"; b.dataset.acao = rot;
    b.innerHTML = `<span class="ico">${ico}</span>${rot}`;
    b.onclick = () => { fecharMenu(); fn(); };
    botoes.appendChild(b);
  }
}

function destacarPendencia() {
  document.querySelectorAll(".tile").forEach(t => t.classList.remove("alerta"));
  // aponta só para tiles que EXISTEM (antes mandava para "Missões", que saiu do
  // menu — o destaque amarelo de estado nunca acendia) — 03/10/2026
  const alvo = { erro: "Como ele está", pensando: "Conversar", dormindo: "Dormir" }[estado.state];
  if (!alvo) return;
  const t = [...document.querySelectorAll(".tile")].find(e => (e.dataset.acao || "").startsWith(alvo));
  if (t) t.classList.add("alerta");
}

function abrirMenu(x, y) {
  montarMenu();
  menu.classList.remove("oculto");
  const w = menu.offsetWidth, h = menu.offsetHeight;
  menu.style.left = Math.min(x, innerWidth - w - 8) + "px";
  menu.style.top = Math.max(8, Math.min(y - h - 10, innerHeight - h - 8)) + "px";
  ultimoMenu = Date.now();
  reportarHit();
}
function fecharMenu() { menu.classList.add("oculto"); reportarHit(); }

/* ───────── ações ───────── */
function reagir(texto, novoEstado) {
  abrirBalao(); msg(texto, "ele");
  if (novoEstado) { aplicarEstado({ state: novoEstado }); setTimeout(() => aplicarEstado({ state: "idle" }), 3500); }
}
function subAlimentar(tipo) {
  abrirBalao();
  msg(tipo === "lanche" ? "Lanche enfileirado (tarefa pequena). Etapa 2: vai para o agente."
                        : "Refeição enfileirada (tarefa grande). Etapa 2: vai para o agente.", "ele");
}
async function status() {
  abrirBalao();
  try { G && await G.ShowStatus(); } catch (e) { msg("status indisponível", "ele"); }
}
function dormir(min) {
  aplicarEstado({ state: "dormindo", detail: `dormindo ${min} min` });
  abrirBalao(); msg(`Vou tirar um cochilo de ${min} minutos. zZz`, "ele");
  setTimeout(() => aplicarEstado({ state: "idle", detail: "acordei" }), min * 60000);
  setTimeout(() => fecharBalao(), 6000);
}
// DESLIGAR — fecha o PetAgent de vez (obrigatório no menu).
function desligar() {
  fecharMenu(); fecharBalao();
  try { G && G.QuitApp(); } catch (e) {}
}
// Recolher a janela inteira (volta pelo systray ou pelo Ctrl+Alt+Q para sair).
function esconderJanela() {
  fecharMenu(); fecharBalao();
  try { G && G.HideAll(); } catch (e) {}
}
/* Esconder o boneco por N minutos — esconde a JANELA de verdade, não só o CSS.
   Motivo (pedido do dono, 03/10/2026): a janela cobre a tela toda e, mesmo invisível,
   CONTINUA presente no Windows — e atrapalhava QUALQUER software que capture ou grave
   UMA janela específica (não só a Ferramenta de Captura). Com a janela escondida, o
   Windows deixa de vê-la e a captura de qualquer outra janela funciona.
   O relógio e a volta ficam no Go (`EsconderPor`): com a janela escondida os timers
   do WebView2 podem ser estrangulados, e o boneco não voltaria na hora certa.
   A volta chega pelo evento "volta-escondido". */
function esconder(min) {
  fecharMenu(); fecharBalao();
  try { G && G.EsconderPor(min); } catch (e) { }
}

/* ───────── seletor de TELA e BORDA ───────── */
const telaSel = { monitor: 1, canto: "baixo-esquerda" };

function rotuloCanto(c) {
  return ({
    "baixo-esquerda": "◣ baixo-esq", "baixo-direita": "◢ baixo-dir",
    "cima-esquerda": "◤ cima-esq", "cima-direita": "◥ cima-dir",
  })[c] || c;
}

async function abrirSeletorTela() {
  fecharMenu();
  telaPanel.classList.remove("oculto");
  const r = pet.getBoundingClientRect();
  telaPanel.style.left = Math.max(10, Math.min(r.left + 20, innerWidth - 320)) + "px";
  telaPanel.style.top = Math.max(10, r.top - 210) + "px";

  const boxM = document.getElementById("tela-mon");
  boxM.innerHTML = "";
  let ms = [];
  try { ms = JSON.parse((G ? await G.ListarMonitores() : "[]") || "[]"); } catch (e) {}
  if (!ms.length) {
    boxM.innerHTML = '<div class="b">nao consegui listar as telas</div>';
  }
  const d = DPR();
  ms.forEach((m) => {
    const b = document.createElement("div");
    b.className = "b" + (m.indice === telaSel.monitor ? " sel" : "");
    b.innerHTML = `Tela ${m.indice}${m.primario ? " ★" : ""}<br><span style="opacity:.6">${Math.round(m.w / d)}x${Math.round(m.h / d)}</span>`;
    b.onclick = async () => {
      telaSel.monitor = m.indice; cfg.monitor = m.indice;
      try { G && await G.ConfigurarTela(m.indice, cfg.borda); } catch (e) {}
      abrirSeletorTela();
    };
    boxM.appendChild(b);
  });

  const boxC = document.getElementById("tela-canto");
  boxC.innerHTML = "";
  [["baixo", "▼ baixo"], ["cima", "▲ cima"], ["esquerda", "◀ esq"], ["direita", "▶ dir"]].forEach(([k, rot]) => {
    const b = document.createElement("div");
    b.className = "b" + (k === cfg.borda ? " sel" : "");
    b.textContent = rot;
    b.onclick = async () => {
      cfg.borda = k; cfg.offset = 120; aplicaPosicao(); salvarPet();
      try { G && await G.ConfigurarTela(telaSel.monitor, k); } catch (e) {}
      fecharSeletorTela();
    };
    boxC.appendChild(b);
  });

  document.getElementById("tela-atual").textContent =
    "Tela " + telaSel.monitor + " · borda " + cfg.borda + " · " + cfg.offset + "px — " +
    "ou arraste o boneco para onde quiser: ele encaixa na borda mais próxima.";
  reportarHit();
}

async function aplicarTela() {
  try { G && await G.ConfigurarTela(telaSel.monitor, telaSel.canto); } catch (e) {}
  fecharSeletorTela();
}
function fecharSeletorTela() { telaPanel.classList.add("oculto"); reportarHit(); }

/* ───────── painel de E-MAIL (triagem que aprende) ───────── */
const emailPanel = document.getElementById("email");

function dominioDe(e) {
  const m = String(e || "").match(/@([^@>\s]+)$/);
  return m ? "@" + m[1].toLowerCase() : String(e || "").toLowerCase();
}

async function abrirEmail() {
  fecharMenu(); fecharSeletorTela();
  emailPanel.classList.remove("oculto");
  const r = pet.getBoundingClientRect();
  emailPanel.style.left = Math.max(8, Math.min(r.left + 20, innerWidth - 410)) + "px";
  emailPanel.style.top = Math.max(8, Math.min(r.top - 380, innerHeight - 470)) + "px";
  await renderEmail();
  reportarHit();
}

async function renderEmail() {
  const box = document.getElementById("email-lista");
  const res = document.getElementById("email-resumo");
  box.innerHTML = "";
  let d = {};
  try { d = JSON.parse((G ? await G.EmailResumo() : "{}") || "{}"); } catch (e) {}
  if (d.erro) {
    res.innerHTML = "⚠ " + d.erro;
    box.innerHTML = '<div class="email-vazio">Clique em "↻ Verificar agora" para ler o Outlook.</div>';
    return;
  }
  const imp = d.importantes || [], des = d.desconhecidos || [];
  res.innerHTML =
    "Conta <b>" + (d.conta || "?") + "</b> · lida em " + String(d.verificado_em || "?").replace("T", " ") +
    "<br>caixa: " + (d.total_pasta || 0) + " mensagens · " + (d.nao_lidas || 0) + " não lidas · amostra: " + (d.lidas_agora || 0) +
    "<br><b>" + imp.length + " importantes</b> · " + des.length + " desconhecidos · " + (d.spam_n || 0) + " spam";
  if (!des.length) {
    box.innerHTML = '<div class="email-vazio">Nenhum remetente desconhecido na última leitura. 🎉</div>';
    return;
  }
  // agrupa por domínio: 1 cartão por domínio, com contagem
  const porDom = {};
  des.forEach((m) => { const k = dominioDe(m.de_email); (porDom[k] = porDom[k] || []).push(m); });
  Object.keys(porDom).slice(0, 14).forEach((k) => {
    const ms = porDom[k], m = ms[0];
    const el = document.createElement("div");
    el.className = "email-item";
    el.innerHTML = '<div class="txt"><b>' + (m.nao_lida ? "🔵 " : "") + k + "</b><span>" + ms.length + "x · " +
      (m.de_nome || "") + " — " + String(m.assunto || "(sem assunto)").slice(0, 60) + "</span></div>";
    const bi = document.createElement("div");
    bi.className = "bt"; bi.textContent = "✅"; bi.title = "sempre importante: " + k;
    bi.onclick = () => marcarEmail(k, "importante");
    const bs = document.createElement("div");
    bs.className = "bt"; bs.textContent = "🚫"; bs.title = "sempre lixo: " + k;
    bs.onclick = () => marcarEmail(k, "spam");
    el.appendChild(bi); el.appendChild(bs);
    box.appendChild(el);
  });
  reportarHit();
}

async function marcarEmail(alvo, tipo) {
  try { G && await G.EmailMarcar(alvo, tipo); } catch (e) {}
  toast(tipo === "importante" ? "✅" : "🚫",
        tipo === "importante" ? "Importante" : "Lixo",
        alvo + " → " + (tipo === "importante" ? "sempre me avise" : "silêncio total"), 2500, true);
  await renderEmail();
}

function fecharEmail() { emailPanel.classList.add("oculto"); reportarHit(); }

/* ───────── carinho, efeitos, balãozinho e lembretes ───────── */
const fxLayer = document.getElementById("fx");
const toastEl = document.getElementById("toast");
let toastTimer = null, aguaTimer = null, arrastouAgora = false, vagueiaTimer = null;

// explosao — emojis subindo a partir do boneco
function explosao(emojis, n) {
  const r = pet.getBoundingClientRect();
  for (let i = 0; i < (n || 5); i++) {
    const e = document.createElement("div");
    e.className = "fx-e";
    e.textContent = emojis[Math.floor(Math.random() * emojis.length)];
    e.style.left = (r.left + 18 + Math.random() * (r.width - 36)) + "px";
    e.style.top = (r.top + 14 + Math.random() * (r.height - 34)) + "px";
    e.style.animationDelay = (Math.random() * 0.35) + "s";
    fxLayer.appendChild(e);
    setTimeout(() => e.remove(), 2400);
  }
}

// carinho — CLIQUE no boneco é agrado, NÃO conversa (regra do dono);
// a conversa abre no duplo clique ou no tile "Conversar".
function carinho() {
  const anterior = (pet.className.match(/pet (\w+)/) || [, "idle"])[1];
  aplicarEstado({ state: "carinho" });
  explosao(["💚", "🩷", "✨"], 4);
  // ⚠️ 04/10/2026: o agrado tem DUAS metades — o visual (acima) e o BICHO (barra de
  // atenção + sininho). Aqui só existia a metade visual: medido no harness, clicar
  // no boneco NÃO subia a barra e não tocava nada, porque o carinho do bicho vivia
  // em OUTRA função (bicho.js -> window.bicho.carinho). 20 pontos = agrado de
  // verdade, mas abaixo do limiar de 25 que faz ele falar (senão cada clique virava
  // um balão).
  try { if (window.bicho && window.bicho.carinho) window.bicho.carinho(20); } catch (e) { }
  setTimeout(() => aplicarEstado({ state: anterior === "carinho" ? "idle" : anterior }), 1700);
}

// toast — balãozinho na tela; posiciona acima do boneco (ou abaixo, se não couber)
function toast(ico, titulo, texto, ms, mini) {
  if (!cfg.interagir) return; // modo "não me interaja": nenhum balãozinho
  document.getElementById("toast-ico").textContent = ico;
  document.getElementById("toast-tit").textContent = titulo;
  document.getElementById("toast-sub").textContent = texto || "";
  toastEl.classList.toggle("mini", !!mini);
  toastEl.classList.remove("oculto");
  const r = pet.getBoundingClientRect();
  const w = toastEl.offsetWidth || 300, h = toastEl.offsetHeight || 60;
  let top = r.top - h - 12;
  if (top < 8) top = r.bottom + 12;
  toastEl.style.left = Math.max(8, Math.min(r.left - 10, innerWidth - w - 8)) + "px";
  toastEl.style.top = Math.max(8, Math.min(top, innerHeight - h - 8)) + "px";
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toastEl.classList.add("oculto"); reportarHit(); }, ms || 7000);
  reportarHit();
}
toastEl.onclick = () => { toastEl.classList.add("oculto"); reportarHit(); };

// tarefa concluída — é o momento "ele termina e avisa na tela"
function tarefaConcluida(titulo, texto) {
  toast("✅", titulo, texto, 8000);
  aplicarEstado({ state: "comemorando" });
  explosao(["✅", "⭐", "🎉"], 6);
  setTimeout(() => aplicarEstado({ state: "idle" }), 2200);
}
function testeAviso() {
  tarefaConcluida("Tarefa concluída", "É assim que eu apareço quando termino algo no PC.");
}

// lembrete de água (local, já funciona)
function agua(min) {
  toast("💧", "Lembrete marcado", "Te chamo em " + min + " min para beber água.", 3500, true);
  clearTimeout(aguaTimer);
  aguaTimer = setTimeout(() => {
    toast("💧", "Hora da água!", "Bebe um copo e volta pro foco. 💪", 15000);
    explosao(["💧", "🚰", "🥤"], 5);
  }, min * 60000);
}

/* ───────── interação ───────── */
pet.addEventListener("mousedown", (e) => {
  // diagnóstico (04/10/2026): o dono relatou "clico e não ouço o carinho". Este log
  // diz se o clique CHEGA no boneco e se a interação está ligada — sem ele, seria
  // adivinhação (a janela é click-through e depende do hit-test do Go).
  try { if (G && G.Log) G.Log("clique: mousedown no boneco | interagir=" + cfg.interagir + " | botao=" + e.button); } catch (e2) { }
  if (e.button !== 0 || !cfg.interagir) return;   // interação desligada: não arrasta
  e.preventDefault();
  const sx = e.clientX, sy = e.clientY, ox = pet.offsetLeft, oy = pet.offsetTop;
  arrastando = true; pet.classList.add("arrastando");
  const move = (ev) => {
    if (!arrastando) return;
    if (Math.abs(ev.clientX - sx) + Math.abs(ev.clientY - sy) > 6) arrastouAgora = true;
    // limite: o boneco nunca sai da janela (senão ele desaparece e você não acha mais)
    const maxX = Math.max(0, innerWidth - pet.offsetWidth);
    const maxY = Math.max(0, innerHeight - pet.offsetHeight);
    pet.style.left = Math.min(maxX, Math.max(0, ox + (ev.clientX - sx))) + "px";
    pet.style.top = Math.min(maxY, Math.max(0, oy + (ev.clientY - sy))) + "px";
    reportarHit();
  };
  const up = () => {
    if (!arrastando) return;
    arrastando = false; pet.classList.remove("arrastando");
    window.removeEventListener("mousemove", move);
    window.removeEventListener("mouseup", up);
    if (arrastouAgora) encaixa(); // solta → encaixa na borda mais próxima
    else salvarPet();
  };
  window.addEventListener("mousemove", move);
  window.addEventListener("mouseup", up);
});

/* CLIQUE = AGRADO (carinho), nunca chat. A conversa é no DUPLO CLIQUE ou no
   tile "Conversar" — regra do dono: "clicar nele não quer dizer que eu
   quero um chat com ele, é só um agrado". */
pet.addEventListener("click", () => {
  fecharMenu();
  try { if (G && G.Log) G.Log("clique: click no boneco | interagir=" + cfg.interagir + " | arrastouAgora=" + arrastouAgora); } catch (e2) { }
  if (!cfg.interagir) return;                           // interação desligada: nem carinho
  if (arrastouAgora) { arrastouAgora = false; return; } // foi arrasto, não clique
  carinho();
});
pet.addEventListener("dblclick", () => {
  fecharMenu();
  try { if (G && G.Log) G.Log("clique: DUPLO clique no boneco | interagir=" + cfg.interagir); } catch (e2) { }
  if (!cfg.interagir) return;                           // interação desligada: sem chat
  abrirBalao(); txt.focus();
});
// BOTÃO DIREITO SEMPRE: é a porta de volta quando a interação está desligada.
pet.addEventListener("contextmenu", (e) => { e.preventDefault(); abrirMenu(e.clientX, e.clientY); });

document.addEventListener("click", (e) => {
  if (!menu.contains(e.target) && e.target !== pet && !pet.contains(e.target)) {
    if (Date.now() - ultimoMenu > 300) fecharMenu();
  }
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") { fecharMenu(); fecharBalao(); fecharSeletorTela(); fecharEmail(); }
  reiniciarAuto();
});
document.getElementById("enviar").onclick = enviar;
document.getElementById("email-fechar").onclick = fecharEmail;
document.getElementById("email-verificar").onclick = async () => {
  toast("📧", "Lendo o Outlook…", "aguarde ~40 s (o JSON é regravado)", 4000, true);
  try { G && await G.EmailVerificarAgora(); } catch (e) {}
  setTimeout(() => renderEmail(), 45000);
};
document.getElementById("balao-min").onclick = fecharBalao;
txt.addEventListener("keydown", (e) => { if (e.key === "Enter") enviar(); });
["mousemove", "keydown", "wheel"].forEach(ev => balao.addEventListener(ev, reiniciarAuto));

function ligarEventos() {
  R = window.runtime || null;
  if (!(R && R.EventsOn)) return false;
  R.EventsOn("pet-state", (s) => aplicarEstado(s));
  // avisos do Go (gateway voltou, saldo baixo, tarefa concluída) → balãozinho na tela
  R.EventsOn("aviso", (a) => {
    toast(a.icone || "🔔", a.titulo || "Aviso", a.texto || "", 9000);
    explosao([a.icone || "🔔"], 3);
  });
  R.EventsOn("status-panel", (texto) => { abrirBalao(); msg(texto, "ele"); });
  // volta do "esconder N min": quem conta o tempo é o Go — aqui só damos o recado
  // (balãozinho, que é a voz do bicho: não abre chat nem entra no histórico)
  R.EventsOn("volta-escondido", (min) => {
    toast("👋", "Voltei!", "fiquei " + min + " min fora, capturas liberadas", 3200, true);
  });

  R.EventsOn("tela-aplicada", (t) => {
    // payload: {borda, w, h, dpi, monitor} — a janela passa a cobrir a tela escolhida
    if (t && typeof t === "object") {
      cfg.monitor = t.monitor || cfg.monitor;
      if (t.borda) cfg.borda = t.borda;
    }
    setTimeout(() => { aplicaPosicao(); salvarPet(); }, 300); // deixa o Windows redimensionar
  });
  return true;
}

// esperarBindings — sem isto, nada funciona: é o pulo do gato desta versão.
async function esperarBindings() {
  for (let i = 0; i < 60; i++) {
    if (window.go && window.go.main && window.go.main.App) {
      G = window.go.main.App;
      ligarEventos();
      log("bindings do Wails prontos em " + (i * 150) + " ms");
      return true;
    }
    await new Promise((r) => setTimeout(r, 150));
  }
  log("FALHA: bindings do Wails nao apareceram em 9s");
  return false;
}

// a janela muda de tamanho quando troca de tela: reposiciona o boneco junto
window.addEventListener("resize", () => { aplicaPosicao(); });

/* rede de segurança: reenvia os retângulos a cada 500 ms.
   Se o Go perder o rastro (janela movida, layout mudou), o clique volta a atravessar. */
setInterval(reportarHit, 500);

/* ───────── boot ───────── */
(async function boot() {
  montarMenu();
  await esperarBindings(); // ← antes de tudo: sem bindings, nada é clicável
  // lê a tela/canto salvos e já alinha o boneco
  try {
    const c = JSON.parse((G ? await G.TelaAtual() : "{}") || "{}");
    telaSel.monitor = c.monitor || 1;
    if (c.borda) cfg.borda = c.borda;
  } catch (e) {}
  await carregarPet();
  reportarHit();
  // (a saudação "casco v0.2" era resto do stub antigo: ela aparecia sozinha no boot
  // e parecia resposta do agente — saiu daqui em 02/10/2026)
  // Pronto: recado pelo BALÃOZINHO — NÃO abre o chat (regra do dono, 03/10/2026).
  // Antes isto abria o balão sozinho no boot E deixava um "Pronto." DENTRO da
  // conversa, que parecia resposta do agente e poluía o histórico.
  setTimeout(() => toast("🐾", "PetAgent",
    "Pronto. Duplo clique em mim para conversar — eu respondo pelo Hermes.", 8000, true), 800);
  log("frontend boot ok");
})();
