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
const SUPERFICIES = ".balao, .menu, .bicho, .pets, .email, .tela, .cg, .toast, .premio, .aparencia";
function reportarHit() {
  if (!G) return;
  const d = DPR(), rs = [];
  for (const el of [pet, ...document.querySelectorAll(SUPERFICIES)]) {
    if (!el || el.classList.contains("oculto") || el.classList.contains("escondido")) continue;
    // o boneco fica SEMPRE registrado: com a interação desligada o clique esquerdo
    // é ignorado pelos handlers, mas o botão direito precisa continuar vivo
    const r = el === pet ? retPet() : el.getBoundingClientRect();
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
// canto: "" | "ini" | "fim" — encostado também no começo/fim da borda (um canto da tela)
// parado: não anda sozinho (pedido do dono, 13/10/2026) · tamanho: escala do boneco
const cfg = { borda: "baixo", offset: 120, canto: "", perfil: "calmo", interagir: true, monitor: 1, dexOn: true,
              parado: true, tamanho: 1 };
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

/* ───────── colado na borda, parado e no tamanho escolhido ─────────
   Pedido do dono (13/10/2026): "quero que ele fique parado no canto que eu soltar,
   o mais grudado possível na tela, e poder reduzir o tamanho dele".
   O quadro do sprite tem transparente em volta do desenho (no yi-dog, 31 px embaixo
   na pose parada) e cada linha da folha centraliza o desenho de um jeito. Por isso:
   a CAIXA (.pet) encosta na borda com margem zero, e o DESENHO (folha ou svg) é
   empurrado o que sobra de transparente na linha atual, pelo `translate` — que soma
   com as animações de `transform` (brincar, gingado) em vez de brigar com elas. */
const TAM_MIN = 0.3, TAM_MAX = 1.5, TAMANHOS = [1, 0.85, 0.7, 0.55, 0.4];
const SVG_FOLGA = { l: 18, t: 0, r: 18, b: 8 }; // sobra do desenho vetor no viewBox 120x120
const escala = () => Math.max(TAM_MIN, Math.min(TAM_MAX, Number(cfg.tamanho) || 1));
const comDex = () => !!(dex && cfg.dexOn);
const petSvg = pet.querySelector("svg");
const desenho = () => comDex() ? dexEl : petSvg;

// transparente em volta do desenho ATUAL, em px CSS (a folha mede por linha)
function folga() {
  const r = pet.getBoundingClientRect();
  if (comDex()) {
    const c = dexCaixas[dexLinha] || dexCaixas[0];
    if (!c) return { l: 0, t: 0, r: 0, b: 0 };
    const kx = r.width / dex.fw, ky = r.height / dex.fh;
    return { l: c.l * kx, t: c.t * ky, r: c.r * kx, b: c.b * ky };
  }
  const kx = r.width / 120, ky = r.height / 120;
  return { l: SVG_FOLGA.l * kx, t: SVG_FOLGA.t * ky, r: SVG_FOLGA.r * kx, b: SVG_FOLGA.b * ky };
}

// lados da tela em que ele está encostado: a borda e, se houver, o canto
function encostado() {
  const horiz = cfg.borda === "cima" || cfg.borda === "baixo";
  const e = { l: cfg.borda === "esquerda", t: cfg.borda === "cima", r: cfg.borda === "direita", b: cfg.borda === "baixo" };
  if (cfg.canto === "ini") e[horiz ? "l" : "t"] = true;
  if (cfg.canto === "fim") e[horiz ? "r" : "b"] = true;
  return e;
}

// leva o desenho até a beirada da tela. `solto` (durante o arrasto) = sem empurrão,
// o desenho acompanha o cursor
function ajustaFolga(solto) {
  const el = desenho();
  for (const o of [dexEl, petSvg]) if (o && o !== el) o.style.translate = "";
  if (!el) return;
  let dx = 0, dy = 0;
  if (!solto && !(arrastando && arrastouAgora)) {
    const f = folga(), e = encostado();
    dx = (e.r ? f.r : 0) - (e.l ? f.l : 0);
    dy = (e.b ? f.b : 0) - (e.t ? f.t : 0);
  }
  const dpr = DPR(), snap = (v) => Math.round(v * dpr) / dpr;
  el.style.translate = (dx || dy) ? snap(dx) + "px " + snap(dy) + "px" : "";
}

// a caixa de clique do boneco = .pet + o desenho (que pode passar da caixa)
function retPet() {
  const a = pet.getBoundingClientRect(), d = desenho();
  const b = d ? d.getBoundingClientRect() : null;
  if (!b || b.width < 2) return a;
  const left = Math.min(a.left, b.left), top = Math.min(a.top, b.top);
  const right = Math.max(a.right, b.right), bottom = Math.max(a.bottom, b.bottom);
  return { left, top, right, bottom, width: right - left, height: bottom - top };
}

function aplicaTamanho() {
  if (comDex()) { dexPinta(); return; }
  const dpr = DPR(), lado = Math.round(120 * escala() * dpr) / dpr;
  pet.style.width = lado + "px"; pet.style.height = lado + "px";
}

// muda o tamanho mantendo o MEIO no mesmo ponto da borda (ou o canto, se estiver num)
function mudaTamanho(novo) {
  const s = Math.round(Math.max(TAM_MIN, Math.min(TAM_MAX, novo)) * 100) / 100;
  const horiz = cfg.borda === "cima" || cfg.borda === "baixo";
  const lado = () => { const r = pet.getBoundingClientRect(); return horiz ? r.width : r.height; };
  const antes = lado();
  cfg.tamanho = s;
  aplicaTamanho();
  if (!cfg.canto) cfg.offset = Math.round(cfg.offset + (antes - lado()) / 2);
  aplicaPosicao(); salvarPet();
  toast("📏", "Tamanho " + Math.round(s * 100) + "%", "gire a rodinha do mouse em cima de mim para ajustar", 1800, true);
}
// tile do menu: desce um degrau (100 → 85 → 70 → 55 → 40 → volta a 100)
function cicloTamanho() {
  const prox = TAMANHOS.find((t) => t < escala() - 0.01);
  mudaTamanho(prox !== undefined ? prox : 1);
}

function aplicaParado() { document.body.classList.toggle("parado", !!cfg.parado); }
/* petParado() é global de propósito (como interagindo()): o bicho.js consulta antes
   de passear sozinho — lá existe um `cfg` próprio, que sombrearia o daqui. */
function petParado() { return !!cfg.parado; }
function alternaParado() {
  cfg.parado = !cfg.parado;
  aplicaParado();
  if (cfg.parado) {
    try { if (window.bicho && window.bicho.pararAndar) window.bicho.pararAndar(); } catch (e) { }
    pet.style.transition = ""; aplicaPosicao();
  }
  salvarPet();
  toast(cfg.parado ? "📌" : "🚶", cfg.parado ? "Fico parado aqui" : "Vou passear de vez em quando",
    cfg.parado ? "só saio do lugar se você me arrastar (ou mandar brincar)" : "ando pela borda e volto a me mexer", 2600, true);
}

// posição a partir de {borda, offset, canto}: a caixa encosta na borda com margem
// zero e ajustaFolga() leva o desenho até a beirada
function aplicaPosicao() {
  const W = innerWidth, H = innerHeight;
  const r = pet.getBoundingClientRect(), pw = r.width || 120, ph = r.height || 120;
  const horiz = cfg.borda === "cima" || cfg.borda === "baixo";
  const max = Math.max(0, horiz ? W - pw : H - ph);
  let ao = Math.max(0, Math.min(max, Number(cfg.offset) || 0)); // posição AO LONGO da borda
  if (cfg.canto === "ini") ao = 0;
  else if (cfg.canto === "fim") ao = max;
  let left = ao, top = Math.max(0, H - ph); // padrão: borda de baixo (acima da barra)
  if (cfg.borda === "cima")          { top = 0; }
  else if (cfg.borda === "esquerda") { left = 0; top = ao; }
  else if (cfg.borda === "direita")  { left = Math.max(0, W - pw); top = ao; }
  // encaixa em pixel físico inteiro: posição sub-pixel faz o navegador reamostrar
  // o sprite e volta a serrilhar (mesmo com o desenho 1:1)
  const dpr = window.devicePixelRatio || 1;
  const snap = (v) => Math.round(v * dpr) / dpr;
  pet.style.left = snap(left) + "px";
  pet.style.top  = snap(top) + "px";
  ajustaFolga();
  reportarHit();
}

// ao soltar o arrasto: encaixa na borda mais próxima (regra do dono) e, se soltou
// perto de um canto, no canto — e fica ali, colado
const CANTO_IMA = 48; // px CSS
function encaixa() {
  const W = innerWidth, H = innerHeight;
  const r = pet.getBoundingClientRect();
  const cx = r.left + r.width / 2, cy = r.top + r.height / 2;
  const d = { baixo: H - cy, cima: cy, esquerda: cx, direita: W - cx };
  let borda = "baixo";
  for (const k in d) { if (d[k] < d[borda]) borda = k; }
  const horiz = borda === "cima" || borda === "baixo";
  const ao = horiz ? r.left : r.top, max = horiz ? W - r.width : H - r.height;
  cfg.borda = borda;
  cfg.canto = ao <= CANTO_IMA ? "ini" : (ao >= max - CANTO_IMA ? "fim" : "");
  cfg.offset = Math.round(Math.max(0, Math.min(max, ao)));
  pet.style.transition = "";
  aplicaPosicao(); salvarPet(); // sem aviso: soltar o boneco não precisa de confirmação (pedido da dona, 22/10/2026)
}

// vagueia — anda um pouco e para; ritmo depende do PERFIL
function vagueia() {
  // NÃO vagueia durante a volta na tela / brincadeira: vaguear mexe na BASE
  // (pet.left/top), e aí a volta deixaria de terminar no lugar de origem
  // (medido 03/10/2026 — o perfil "calmo" vagueia a cada 24 s e atropelava os 22 s
  // da volta). E parado (o padrão) ele não sai do lugar sozinho.
  if (cfg.parado || emEfeito()) return;
  const p = PERFIS[cfg.perfil] || PERFIS.calmo;
  const passo = Math.round((40 + Math.random() * 90) * p.andar) * (Math.random() < 0.5 ? -1 : 1);
  const W = innerWidth, H = innerHeight, pw = pet.offsetWidth, ph = pet.offsetHeight;
  pet.classList.add("andando");
  cfg.canto = ""; ajustaFolga();   // andando, ele sai do canto
  if (cfg.borda === "cima" || cfg.borda === "baixo") {
    const novo = Math.max(0, Math.min(W - pw, pet.offsetLeft + passo));
    pet.style.transition = "left 1.7s ease-in-out"; pet.style.left = novo + "px"; cfg.offset = novo;
  } else {
    const novo = Math.max(0, Math.min(H - ph, pet.offsetTop + passo));
    pet.style.transition = "top 1.7s ease-in-out"; pet.style.top = novo + "px"; cfg.offset = novo;
  }
  setTimeout(() => {
    pet.classList.remove("andando"); pet.style.transition = "";
    salvarPet(); reportarHit();
  }, 1900);
}

/* caminharNaBorda(delta, ms) — o PASSEIO do bicho.js: anda `delta` px em `ms` e para.
   ⚠️ 13/10/2026: o passeio mexia só no `left`, então numa borda lateral o boneco saía
   andando para o meio da tela, descolado da borda. Agora ele anda AO LONGO da borda
   em que está, e onde ele para vira o cfg.offset (antes o próximo aplicaPosicao o
   puxava de volta para o lugar de antes do passeio). */
let caminhadaTimer = null;
function caminharNaBorda(delta, ms) {
  if (arrastando || emEfeito()) return;
  pararCaminhada(true);
  const horiz = cfg.borda === "cima" || cfg.borda === "baixo";
  const r = pet.getBoundingClientRect();
  const de = horiz ? r.left : r.top;
  const fim = Math.max(0, Math.min(horiz ? innerWidth - r.width : innerHeight - r.height, de + delta));
  if (Math.abs(fim - de) < 8) return;
  cfg.canto = "";
  // sentido do sprite: na horizontal, para onde vai; nas laterais, de frente para a tela
  const dir = horiz ? (fim >= de ? 1 : -1) : (cfg.borda === "direita" ? -1 : 1);
  pet.style.transition = "";
  andar(true, dir);
  ajustaFolga();
  const t0 = performance.now();
  caminhadaTimer = setInterval(() => {
    const k = Math.min(1, (performance.now() - t0) / ms);
    pet.style[horiz ? "left" : "top"] = (de + (fim - de) * k) + "px";
    if (k >= 1) pararCaminhada();
  }, 30);
}
// para onde estiver; `semAssentar` = quem chamou já vai reposicionar (arrasto, nova caminhada)
function pararCaminhada(semAssentar) {
  if (!caminhadaTimer) return;
  clearInterval(caminhadaTimer); caminhadaTimer = null;
  andar(false);
  if (semAssentar) return;
  const horiz = cfg.borda === "cima" || cfg.borda === "baixo";
  cfg.offset = Math.round(horiz ? pet.offsetLeft : pet.offsetTop);
  aplicaPosicao(); salvarPet();
}
window.petCaminhar = caminharNaBorda;
window.petPararCaminhada = pararCaminhada;

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
  aplicaAparencia(cfg.aparencia); // tema.js: cores, fonte, ícones e cantos salvos
  aplicaPerfil(); aplicaInteracao(); aplicaParado(); aplicaTamanho(); aplicaPosicao();
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
let dex = null, dexTimer = null, dexLinha = 0, dexFrame = 0, dexFrames = [], dexCaixas = [];

// Mede, célula por célula, quais frames têm desenho de verdade. Várias folhas do
// Petdex têm células vazias no meio da linha — e aí o boneco "pisca"/some quando
// o ciclo chega nelas. Aqui a gente só anda pelos frames que existem.
// Mede também a CAIXA do desenho em cada linha (união dos frames): o transparente
// que sobra de cada lado, para ele encostar de verdade na borda (ver ajustaFolga).
async function medirFrames(url, fw, fh, cols, linhas) {
  const img = new Image();
  img.src = url;
  await img.decode();
  const cv = document.createElement("canvas");
  cv.width = img.width; cv.height = img.height;
  const cx = cv.getContext("2d", { willReadFrequently: true });
  cx.drawImage(img, 0, 0);
  const mapa = [], caixas = [];
  for (let l = 0; l < linhas; l++) {
    const validos = [];
    let x0 = fw, y0 = fh, x1 = -1, y1 = -1;
    for (let c = 0; c < cols; c++) {
      const d = cx.getImageData(c * fw, l * fh, fw, fh).data;
      let soma = 0, n = 0;
      for (let i = 3; i < d.length; i += 4 * 5) { soma += d[i]; n++; }  // amostra do alfa
      if (!(n && (soma / n) > 6)) continue;                            // célula vazia
      validos.push(c);
      for (let y = 0; y < fh; y++) {
        for (let x = 0, i = y * fw * 4 + 3; x < fw; x++, i += 4) {
          if (d[i] <= 16) continue;                                    // quase transparente
          if (x < x0) x0 = x;
          if (x > x1) x1 = x;
          if (y < y0) y0 = y;
          if (y > y1) y1 = y;
        }
      }
    }
    mapa.push(validos.length ? validos : [0]);
    caixas.push(x1 < 0 ? null : { l: x0, t: y0, r: fw - 1 - x1, b: fh - 1 - y1 });
  }
  return { mapa, caixas };
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
    try {
      const m = await medirFrames(d.sheet, d.fw, d.fh, d.cols, d.linhas);
      dexFrames = m.mapa; dexCaixas = m.caixas;
    } catch (e) { dexFrames = []; dexCaixas = []; }
    pet.classList.add("dex");
    dexLinha = -1; clearInterval(dexTimer); dexTimer = null; // força reiniciar o ciclo
    dexToca(estado.state || "idle");
    aplicaPosicao();
  }
  console.log("petdex pronto:", d.slug, "|", d.nome, "| frame", d.fw + "x" + d.fh, "| frames por linha", JSON.stringify(dexFrames));
  if (G && G.Log) {
    try {
      G.Log("petdex frames por linha: " + JSON.stringify(dexFrames) +
            " | folga da pose parada=" + JSON.stringify(dexCaixas[0] || null) +
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
  ajustaFolga();   // cada linha da folha tem a sua sobra de transparente
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
  // em 100%, pixel exato: 1 pixel do sprite = 1 pixel físico da tela (senão reamostra
  // e serrilha). Em outro tamanho o quadro é arredondado para pixel físico inteiro,
  // para a folha não escorregar de um frame para o vizinho.
  const dpr = window.devicePixelRatio || 1, s = escala();
  const cw = Math.max(1, Math.round(dex.fw * s)) / dpr, ch = Math.max(1, Math.round(dex.fh * s)) / dpr;
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
  else { clearInterval(dexTimer); aplicaTamanho(); toast("🐾", "Boneco vetor (nosso)", "", 2400, true); }
  aplicaPosicao(); salvarPet();
}

/* ───────── painel de bonecos (galeria Petdex) ─────────
   Escolhe o boneco sem sair do app: mostra os instalados (miniatura servida pelo
   próprio app em 127.0.0.1) e busca no catálogo do Petdex; clicar num que ainda
   não está na máquina baixa os 2 arquivos e já aplica. */
const petPanelEl = document.getElementById("petPanel");
let petsBase = "", petPanelTermo = "", petPanelOffset = 0;

// 22/10/2026: todos os painéis abrem ENCOSTADOS no boneco, pelo mesmo cálculo do menu
// (posicionaPertoDoPet: ancorado pela base, então a lista que carrega depois cresce
// para cima sem desgrudar). Antes cada um tinha a sua conta, com "380 px acima" fixo.
function posicionaJunto(el) { posicionaPertoDoPet(el); }

async function abrirPainelBonecos(termo) {
  petPanelEl.classList.remove("oculto");
  posicionaJunto(petPanelEl);
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
    pet.classList.remove("dex"); aplicaTamanho();
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
  atualizaInfoChat(); // cabeçalho do chat: nome, modelo, saldo e o ponto de status
  destacarPendencia();
  reportarHit();
}

/* ───────── balão (conversa) ─────────
   A conversa é UMA só: o balão e o chat estendido desenham a mesma lista. */
const conversa = [];
let ocupado = false; // true enquanto o agente está pensando

// O agente responde em markdown; aqui vai um subconjunto seguro (escapa antes)
function formata(t) {
  let s = (t || "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  s = s.replace(/\*\*([^*\n]+)\*\*/g, "<b>$1</b>");
  s = s.replace(/`([^`\n]+)`/g, "<code>$1</code>");
  s = s.replace(/^#{1,4} (.*)$/gm, "<b>$1</b>");
  return s;
}

// `extra` vai junto na mensagem guardada (ex.: { hora, seg } no rodapé da resposta)
function msg(texto, quem = "ele", extra) {
  conversa.push(Object.assign({ quem: quem, texto: texto }, extra || {}));
  // a conversa sobrevive ao reiniciar o app (o agente pode responder depois de ele
  // já ter sido fechado/reaberto — foi o que aconteceu em 02/10/2026)
  try { localStorage.setItem("conversa", JSON.stringify(conversa.slice(-60))); } catch (e) {}
  pintaConversa();
}

/* ───────── cabeçalho do chat: nome do pet · modelo · saldo · gasto de hoje ─────────
   Pedido da dona (22/10/2026): sai "Hermes · gateway ativo (4s) · saldo indisponível
   (chave não encontrada)". O ponto de status agora fala do AGENTE QUE RESPONDE O CHAT
   (a ponte), não do heartbeat do gateway — esse detalhe técnico vai para o tooltip. */
let infoChat = { modelo: "", saldo: "", pronta: false, erro: "" };

const numeroSaldo = (t) => { const m = /US\$\s*([\d.]+)/.exec(t || ""); return m ? parseFloat(m[1]) : null; };
const moeda = (v) => "US$ " + v.toLocaleString("pt-BR", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

// gasto de hoje = quanto o saldo caiu desde a primeira leitura do dia. Recarga (o saldo
// SOBE) recomeça a conta. É a conta inteira da chave: inclui o que se gastou fora do boneco.
function gastoHoje(saldo) {
  const d = new Date(), dia = d.getFullYear() + "-" + (d.getMonth() + 1) + "-" + d.getDate();
  let g = {};
  try { g = JSON.parse(localStorage.getItem("saldo.dia") || "{}"); } catch (e) { }
  if (g.dia !== dia || !(saldo <= g.inicial)) {
    g = { dia: dia, inicial: saldo };
    try { localStorage.setItem("saldo.dia", JSON.stringify(g)); } catch (e) { }
  }
  return g.inicial - saldo;
}

function nomeDoPet() {
  try { if (window.bichoResumo) return window.bichoResumo().nome || "Seu pet"; } catch (e) { }
  return "Seu pet";
}

function pintaCabecalhoChat() {
  const nome = nomeDoPet();
  const saldoTxt = infoChat.saldo || estado.balance || "";
  const saldo = numeroSaldo(saldoTxt);
  const partes = [];
  if (infoChat.modelo) partes.push(infoChat.modelo);
  if (saldo !== null) {
    partes.push(moeda(saldo));
    const g = gastoHoje(saldo);
    if (g >= 0.005) partes.push("hoje −" + moeda(g));
  } else if (/chave/.test(saldoTxt)) {
    partes.push("saldo: configure a chave");
  }
  // o ponto: pensando (esperando resposta) › fora do ar › saldo baixo › ok
  let cls = "", linha = partes.join(" · ");
  if (ocupado) { cls = "pensando"; linha = nome + " está pensando…"; }
  else if (!infoChat.pronta && infoChat.erro) { cls = "erro"; linha = "agente fora do ar — respondo pelo modo lento"; }
  else if (!infoChat.pronta) { cls = "aviso"; linha = "conectando ao agente…"; }
  else if (saldo !== null && saldo < 3) { cls = "aviso"; linha = "saldo baixo · " + linha; }
  // o detalhe técnico (gateway, motivo do saldo) continua acessível: passar o mouse
  const dica = [estado.detail, saldoTxt, infoChat.erro].filter(Boolean).join(" · ");
  for (const [d, t, s] of [["dot", "balao-titulo", "balao-sub"], ["cg-dot", "cg-tit", "cg-sub"]]) {
    const elD = document.getElementById(d), elT = document.getElementById(t), elS = document.getElementById(s);
    if (elD) { elD.className = "dot " + cls; elD.title = dica; }
    if (elT) elT.textContent = nome;
    if (elS) { elS.textContent = linha; elS.title = dica; }
  }
}

async function atualizaInfoChat() {
  try { if (G && G.InfoChat) infoChat = Object.assign(infoChat, JSON.parse(await G.InfoChat())); } catch (e) { }
  pintaCabecalhoChat();
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

/* BOLHA AO VIVO — o pet "digitando" enquanto o agente trabalha (10/10/2026; virou
   bolha com três pontinhos em 22/10/2026, no lugar de "⏳ consultando o Hermes…").
   O dono: "eu não fico nunca no vácuo aqui no chat".

   MEDIDO: o modelo passa o tempo PENSANDO e o texto só sai em rajada no fim — transmitir
   só o texto deixaria o balão parado de novo. Por isso há DOIS movimentos: os passos que a
   ponte manda (ferramentas) e um RELÓGIO que mantém a bolha viva a cada segundo.

   ⚠️ Não abre o balão: a regra do dono é que nada abre o chat sozinho.
   ⚠️ Não usa `conversa` (é const — reatribuir quebra a função): mexe só no DOM. */
let _vivoRotulo = "";
let _vivoTexto = "";
let _vivoInicio = 0;
let _vivoTimer = null;

function _pintaVivo() {
  if (!_vivoInicio) return;
  const seg = Math.round((Date.now() - _vivoInicio) / 1000);
  let html;
  if (_vivoTexto) {
    html = formata(_vivoTexto);   // o texto já está chegando: mostra o que veio
  } else {
    const rotulo = [_vivoRotulo ? formata(_vivoRotulo) : "", seg > 2 ? seg + " s" : ""].filter(Boolean).join(" · ");
    html = '<span class="digitando"><i></i><i></i><i></i></span>' + (rotulo ? '<span class="passo">' + rotulo + "</span>" : "");
  }
  for (const alvo of [msgs, document.getElementById("cg-msgs")]) {
    if (!alvo) continue;
    let el = alvo.querySelector(".ao-vivo");
    if (!el) {
      el = document.createElement("div");
      el.className = "m ele ao-vivo";
      alvo.appendChild(el);
    }
    el.innerHTML = html;
  }
  rolarAoFim();
}

function iniciaAoVivo() {
  _vivoRotulo = ""; _vivoTexto = ""; _vivoInicio = Date.now();
  _pintaVivo();
  if (!_vivoTimer) _vivoTimer = setInterval(_pintaVivo, 1000);
}

function progressoAoVivo(p) {
  if (!p || !p.tipo) return;
  if (p.tipo === "passo") _vivoRotulo = String(p.texto || "trabalhando");
  else _vivoTexto = String(p.texto || "");
  if (!_vivoInicio) _vivoInicio = Date.now();
  _pintaVivo();
  if (!_vivoTimer) _vivoTimer = setInterval(_pintaVivo, 1000);
}

function encerraAoVivo() {
  if (_vivoTimer) { clearInterval(_vivoTimer); _vivoTimer = null; }
  _vivoRotulo = ""; _vivoTexto = ""; _vivoInicio = 0;
  for (const alvo of [msgs, document.getElementById("cg-msgs")]) {
    if (!alvo) continue;
    const vivo = alvo.querySelector(".ao-vivo");
    if (vivo) vivo.remove();
  }
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
      // rodapé discreto da resposta: hora e quanto levou (era a linha "via Hermes · 13.9 s")
      if (m.hora || m.seg) {
        const rp = document.createElement("span");
        rp.className = "rodape-msg";
        rp.textContent = [m.hora, m.seg ? String(m.seg).replace(".", ",") + " s" : ""].filter(Boolean).join(" · ");
        el.appendChild(rp);
      }
      if (m.quem !== "meta") { // linhas de status não ganham botão de copiar
        const cp = document.createElement("span");
        cp.className = "copiar";
        cp.innerHTML = icone("copiar", 14);
        cp.title = "copiar esta mensagem";
        cp.onclick = (ev) => { ev.stopPropagation(); copiarTexto(m.texto, cp); };
        el.appendChild(cp);
      }
      alvo.appendChild(el);
    }
  }
  if (_vivoInicio) _pintaVivo(); // repintar a conversa não pode sumir com a bolha "digitando"
  rolarAoFim();   // antes era `alvo.scrollTop` por alvo: não pegava com o balão oculto
  ancoraBalao();
}

/* Copiar mensagem: a API de clipboard exige contexto seguro e no WebView2 nem
   sempre está disponível, então há reserva com textarea + execCommand. */
function copiarTexto(t, el) {
  const ok = () => {
    el.innerHTML = icone("ok", 14);
    el.classList.add("copiado");
    setTimeout(() => { el.innerHTML = icone("copiar", 14); el.classList.remove("copiado"); }, 1200);
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

/* o balão é ancorado pelo RODAPÉ: cresce para cima, nunca por cima do pet.
   Tamanho (22/10/2026): 460 × até 55% da tela por padrão, e a dona redimensiona pela
   alça (fica salvo em cfg.chat). Aqui só se garante que ele CABE acima do boneco. */
const BALAO_MIN = { w: 320, h: 260 };
function tamanhoBalao() {
  const c = cfg.chat || {};
  return { w: c.w || 460, h: c.h || Math.round(Math.min(innerHeight * 0.55, 560)) };
}
function ancoraBalao() {
  if (!balao || balao.classList.contains("oculto")) return;
  const r = pet.getBoundingClientRect();
  const t = tamanhoBalao();
  const larg = Math.max(BALAO_MIN.w, Math.min(t.w, innerWidth - 20));
  // boneco encostado em CIMA: não cabe o balão acima dele — abre embaixo (ancorado no topo)
  const embaixo = r.top - 28 < BALAO_MIN.h;
  balao.classList.toggle("embaixo", embaixo);
  balao.style.width = larg + "px";
  if (embaixo) {
    const topo = Math.round(r.bottom + 18);
    balao.style.height = Math.max(BALAO_MIN.h, Math.min(t.h, innerHeight - topo - 10)) + "px";
    balao.style.bottom = "";
    balao.style.top = topo + "px";
  } else {
    const base = Math.max(10, innerHeight - r.top + 18);
    balao.style.height = Math.max(BALAO_MIN.h, Math.min(t.h, innerHeight - base - 10)) + "px";
    balao.style.top = "";
    balao.style.bottom = base + "px";
  }
  let x = r.left + 20;
  const flip = x + larg > innerWidth - 10;
  if (flip) x = Math.max(10, innerWidth - larg - 10);
  balao.classList.toggle("flip", flip); // antes só ADICIONAVA: virado uma vez, ficava virado
  balao.style.left = x + "px";
}

(function redimensionarBalao() { // pela alça do canto de cima (oposto ao rabinho)
  const puxa = document.getElementById("balao-puxa");
  if (!puxa || !balao) return;
  let p = null;
  puxa.addEventListener("mousedown", (e) => {
    p = { x: e.clientX, y: e.clientY, w: balao.offsetWidth, h: balao.offsetHeight, flip: balao.classList.contains("flip"), embaixo: balao.classList.contains("embaixo") };
    e.preventDefault();
    e.stopPropagation();
  });
  window.addEventListener("mousemove", (e) => {
    if (!p) return;
    const dx = (e.clientX - p.x) * (p.flip ? -1 : 1);  // virado: a alça fica à esquerda
    cfg.chat = {
      w: Math.round(Math.max(BALAO_MIN.w, p.w + dx)),
      // ancorado embaixo: subir = crescer (aberto embaixo do boneco, a alça desce)
      h: Math.round(Math.max(BALAO_MIN.h, p.h + (e.clientY - p.y) * (p.embaixo ? 1 : -1))),
    };
    ancoraBalao();
    reportarHit();
    reiniciarAuto();
  });
  window.addEventListener("mouseup", () => { if (p) { p = null; salvarPet(); } });
})();

function abrirBalao(posicionar = true) {
  balao.classList.remove("oculto");
  if (posicionar) ancoraBalao();
  pintaCabecalhoChat();
  rolarAoFim();   // abrir = cair na ÚLTIMA mensagem, sem arrastar até o fim
  txt.focus();
  reiniciarAuto();
  reportarHit();
}
function fecharBalao() { balao.classList.add("oculto"); reportarHit(); }
function reiniciarAuto() {
  clearTimeout(balaoTimer);
  if (ocupado) return; // esperando o agente: NÃO esconde (a resposta chega depois)
  balaoTimer = setTimeout(() => fecharBalao(), 20000); // auto-esconde: 20 s sem interação
}

async function enviarTexto(texto, input) {
  texto = (texto || "").trim();
  if (!texto || ocupado) return;
  input.value = "";
  msg(texto, "eu");
  classePet("pensando");
  ocupado = true;                       // esperando resposta: o balão NÃO se esconde
  iniciaAoVivo();                       // a bolha "digitando" (era a linha "⏳ consultando…")
  input.disabled = true;
  input.placeholder = nomeDoPet() + " está pensando…";
  pintaCabecalhoChat();
  reiniciarAuto();
  let resp = "(sem resposta)";
  const t0 = Date.now();
  try { resp = G ? await G.SendChat(texto) : "(fora do Wails)"; } catch (e) { resp = "erro: " + e; }
  const levou = Math.round((Date.now() - t0) / 100) / 10;
  ocupado = false;
  input.disabled = false;
  input.placeholder = "Escreva e dê Enter…";
  // COMANDOS QUE LIMPAM A TELA (pedido do dono, 10/10/2026): /clear, /new e /reset apagam a
  // conversa VISÍVEL — senão o chat fica poluído e o dono rola por mãos antigas. A ponte cuida
  // do CONTEXTO (cria sessão nova); a tela é daqui. Apago ANTES de mostrar a resposta, para o
  // recado do comando ser a primeira linha do chat limpo.
  const _cmd = texto.toLowerCase().split(/\s+/)[0];
  if (["/clear", "/limpar", "/new", "/nova", "/reset"].includes(_cmd)) {
    // ⚠️ `conversa` é CONST: reatribuir (`conversa = []`) lança TypeError e QUEBRA
    // a função aqui — a resposta nunca aparecia e o chat não limpava. Defeito medido em
    // 10/10/2026: o /clear zerava o contexto no motor (log: 0,6 s) e na tela "não funcionava".
    // Limpar o MESMO array (length = 0) mantém a referência e o resto do código intacto.
    conversa.length = 0;
    try { localStorage.setItem("conversa", JSON.stringify([])); } catch (e) {}
  }
  // a bolha ao vivo sai de cena: entra a mensagem de verdade, com hora e tempo no rodapé
  encerraAoVivo();
  const hora = new Date().toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });
  msg(resp, "ele", { hora: hora, seg: levou });
  classePet(estado.state || "idle");
  abrirBalao(false);                    // garante o balão aberto para mostrar a resposta
  atualizaInfoChat();                   // o modelo pode ter chegado agora, com a resposta
  reiniciarAuto();
  reportarHit();
}

function enviar() { return enviarTexto(txt.value, txt); }

/* ───────── chat estendido (janela arrastável e redimensionável) ─────────
   Abre no centro da tela atual, arrasta pela barra de título e redimensiona pelo
   canto. É a MESMA conversa do balão — só maior. Posição e tamanho ficam salvos
   em cfg.chatGrande (22/10/2026): reabrir volta onde a dona deixou. */
const cg = document.getElementById("chatGrande");

function abrirChatGrande() {
  if (!cg) return;
  fecharBalao(); // pedido do dono (02/10/2026): o balão pequeno SAI de cena
  cg.classList.remove("oculto");
  pintaCabecalhoChat();
  rolarAoFim();   // idem: o chat estendido abre no fim da conversa
  if (!cg.dataset.pos) {
    const s = cfg.chatGrande || {};
    const larg = Math.min(innerWidth - 20, s.w || Math.max(420, Math.round(innerWidth * 0.62)));
    const alt = Math.min(innerHeight - 20, s.h || Math.max(300, Math.round(innerHeight * 0.6)));
    const x = s.x !== undefined ? s.x : Math.round((innerWidth - larg) / 2);
    const y = s.y !== undefined ? s.y : Math.round((innerHeight - alt) / 2);
    cg.style.width = larg + "px";
    cg.style.height = alt + "px";
    cg.style.left = Math.max(-40, Math.min(innerWidth - 120, x)) + "px";
    cg.style.top = Math.max(0, Math.min(innerHeight - 40, y)) + "px";
    cg.dataset.pos = "1";
  }
  document.getElementById("cg-txt").focus();
  reportarHit();
}
function fecharChatGrande() {
  if (cg) { cg.classList.add("oculto"); reportarHit(); }
}
function guardaChatGrande() {
  cfg.chatGrande = { x: cg.offsetLeft, y: cg.offsetTop, w: cg.offsetWidth, h: cg.offsetHeight };
  salvarPet();
}

(function arrastarChat() { // arrastar pela barra de título
  const topo = document.getElementById("cg-topo");
  if (!topo || !cg) return;
  let d = null;
  topo.addEventListener("mousedown", (e) => {
    if (e.target.closest("button")) return; // botões não arrastam (o alvo pode ser o ícone)
    d = { x: e.clientX - cg.offsetLeft, y: e.clientY - cg.offsetTop };
    e.preventDefault();
  });
  window.addEventListener("mousemove", (e) => {
    if (!d) return;
    cg.style.left = Math.max(-40, Math.min(innerWidth - 120, e.clientX - d.x)) + "px";
    cg.style.top = Math.max(0, Math.min(innerHeight - 40, e.clientY - d.y)) + "px";
    reportarHit();
  });
  window.addEventListener("mouseup", () => { if (d) { d = null; guardaChatGrande(); } });
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
  window.addEventListener("mouseup", () => { if (p) { p = null; guardaChatGrande(); } });
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
try { // recupera a conversa anterior (inclusive resposta que chegou com o balão fechado)
  const c = JSON.parse(localStorage.getItem("conversa") || "[]");
  if (Array.isArray(c) && c.length) {
    for (const m of c) {
      // conversa guardada antes de 22/10/2026: "via Hermes · 13.9 s" vira o rodapé da
      // resposta anterior; "⏳ consultando…" e "✅ tarefa concluída" não aparecem mais
      if (m.quem === "meta") {
        const via = /^via \S+ · ([\d.]+) s$/.exec(m.texto || "");
        const ant = conversa[conversa.length - 1];
        if (via && ant && ant.quem === "ele" && !ant.seg) { ant.seg = parseFloat(via[1]); continue; }
        if (via || /^⏳|tarefa concluída/.test(m.texto || "")) continue;
      }
      conversa.push(m);
    }
    pintaConversa();
  }
} catch (e) {}
pintaCabecalhoChat();

/* ───────── menu (dock com abas: Pet · Recursos · Sistema) ─────────
   Escolha da dona em 22/10/2026 (prancha "PetAgent — Menu novo", opção A): o
   que é DO PET fica separado do que é recurso DELA e do que é controle da janela.
   Cada item: [ícone, nome, ação, valor?, fica?] — nome/valor podem ser função
   (mostram o estado ATUAL a cada abertura); `fica` mantém o menu aberto (ajuste
   que só alterna um valor). O bicho.js troca MENU.pet pelas ações dele em
   repintaMenu(). Regra que continua: uma ação, um botão. */
const MENU = {
  pet: [
    ["alimentar", "Alimentar", () => subAlimentar()],
    ["brincar", "Brincar", () => reagir("Uhu! Vou procurar um tesouro…", "pensando")],
    ["dormir", "Dormir", () => dormir(30)],
    ["status", "Como está", () => status()],
    ["colecao", "Coleção", () => { if (window.premiosGaleria) window.premiosGaleria(); }],
    ["cuidar", "Cuidar dele", () => { if (window.bicho) window.bicho.abrirPainel(); }],
  ],
  boneco: [
    ["boneco", "Trocar boneco", () => abrirPainelBonecos()],
    ["tamanho", "Tamanho", () => cicloTamanho(), () => Math.round(escala() * 100) + "%", true],
    ["passear", "Passear sozinho", () => alternaParado(), () => cfg.parado ? "Parado" : "Ligado", true],
    ["perfil", "Perfil", () => cicloPerfil(), () => ({ calmo: "Calmo", agitado: "Agitado", brincalhao: "Brincalhão" })[cfg.perfil] || "", true],
    ["interacao", "Interação", () => alternaInteracao(), () => cfg.interagir ? "Ligada" : "Desligada", true],
  ],
  recursos: [
    ["conversar", "Conversar", () => { abrirBalao(); txt.focus(); }, "No balão, junto do boneco"],
    ["janela", "Chat estendido", () => abrirChatGrande(), "Janela grande, com histórico"],
    ["email", "E-mail", () => abrirEmail(), "O que chegou de importante"],
  ],
  sistema: [
    ["tela", "Tela e borda", () => abrirSeletorTela()],
    ["inteligencia", "Inteligência", () => abrirInteligencia()],
    ["ajustes", "Aparência", () => abrirAparencia()],
    ["esconder", "Esconder por 15 min", () => esconder(15)],
    ["relogio", "Esconder por 1 h", () => esconder(60)],
    ["recolher", "Recolher janela", () => esconderJanela()],
    ["balao", "Recolher balão", () => fecharBalao()],
  ],
};
const ABAS_MENU = [["pet", "Pet"], ["recursos", "Recursos"], ["sistema", "Sistema"]];
let abaMenu = "pet";

const valorDe = (v) => (typeof v === "function" ? v() : v) || "";
// texto no innerHTML do menu sempre escapado (o nome do pet é digitado pela dona)
const escMenu = (t) => String(t).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

function montarMenu() {
  // nome, humor e o que ele está pedindo vêm do bicho.js (se carregou)
  let r = { nome: "Hermes", linha: "", pendente: "" };
  try { if (window.bichoResumo) r = Object.assign(r, window.bichoResumo()); } catch (e) { }
  const linha = (it, cls) => {
    const [ico, nome, , valor] = it;
    return `<button class="mlinha ${cls || ""}" data-acao="${escMenu(valorDe(nome))}">${icone(ico, 18)}` +
      `<span class="nome">${escMenu(valorDe(nome))}</span><span class="valor">${escMenu(valorDe(valor))}</span></button>`;
  };
  let corpo = "";
  if (abaMenu === "pet") {
    corpo = `<div class="menu-grade">${MENU.pet.map(([ico, nome]) =>
      `<button class="mtile" data-acao="${escMenu(nome)}"><span class="mtile-ico">${icone(ico)}</span>` +
      `<span>${escMenu(nome)}</span>${r.pendente === ico ? '<i class="pendente" title="pedindo agora"></i>' : ""}</button>`).join("")}</div>
      <div class="mlista"><div class="menu-secao">Boneco</div>${MENU.boneco.map((it) => linha(it)).join("")}</div>`;
  } else if (abaMenu === "recursos") {
    corpo = `<div class="mlista largo">${MENU.recursos.map(([ico, nome, , desc]) =>
      `<button class="mcartao" data-acao="${escMenu(nome)}"><span class="mcartao-ico">${icone(ico)}</span>` +
      `<span class="txt"><b>${escMenu(nome)}</b><span>${escMenu(desc || "")}</span></span>${icone("seta", 16)}</button>`).join("")}</div>`;
  } else {
    corpo = `<div class="mlista">${MENU.sistema.map((it) => linha(it)).join("")}
      <div class="menu-sep"></div>${linha(["desligar", "Desligar"], "perigo")}</div>`;
  }
  menu.innerHTML = `
    <div class="menu-topo">
      <span class="menu-avatar">${icone("boneco")}</span>
      <span class="menu-quem"><b>${escMenu(r.nome)}</b><span>${escMenu(r.linha)}</span></span>
      <button class="botao-x" data-fechar aria-label="Fechar menu">${icone("fechar", 18)}</button>
    </div>
    <div class="menu-abas" role="tablist">${ABAS_MENU.map(([k, rot]) =>
      `<button role="tab" data-aba="${k}" class="${abaMenu === k ? "sel" : ""}" aria-selected="${abaMenu === k}">${rot}</button>`).join("")}</div>
    ${corpo}`;

  menu.querySelector("[data-fechar]").onclick = fecharMenu;
  menu.querySelectorAll("[data-aba]").forEach((b) => {
    b.onclick = () => { abaMenu = b.dataset.aba; montarMenu(); posicionaPertoDoPet(menu); reportarHit(); };
  });
  const itens = abaMenu === "pet" ? [...MENU.pet, ...MENU.boneco]
    : abaMenu === "recursos" ? MENU.recursos
    : [...MENU.sistema, ["desligar", "Desligar", () => desligar()]];
  menu.querySelectorAll("[data-acao]").forEach((b, i) => {
    const it = itens[i];
    if (!it) return;
    const fica = abaMenu !== "recursos" && it[4];
    b.onclick = () => {
      if (fica) { it[2](); if (!menu.classList.contains("oculto")) { montarMenu(); reportarHit(); } return; }
      fecharMenu(); it[2]();
    };
  });
  destacarPendencia();
}

function destacarPendencia() {
  document.querySelectorAll("#menu [data-acao]").forEach(t => t.classList.remove("alerta"));
  // aponta só para itens que EXISTEM (antes mandava para "Missões", que saiu do
  // menu — o destaque de estado nunca acendia) — 03/10/2026
  const alvo = { erro: "Como está", pensando: "Conversar", dormindo: "Dormir" }[estado.state];
  if (!alvo) return;
  const t = [...document.querySelectorAll("#menu [data-acao]")].find(e => (e.dataset.acao || "").startsWith(alvo));
  if (t) t.classList.add("alerta");
}

/* Painel encostado no boneco: em cima dele se o boneco está na metade de baixo da
   tela (o caso comum: canto inferior), embaixo se está em cima; alinhado pelo lado
   de dentro da tela. Ancora pela BASE quando fica em cima: assim trocar de aba
   (que muda a altura) cresce para cima sem desgrudar do boneco. */
function posicionaPertoDoPet(el) {
  const r = retPet(), m = 8, gap = 10;
  const w = el.offsetWidth;
  const cx = (r.left + r.right) / 2, cy = (r.top + r.bottom) / 2;
  const left = cx > innerWidth / 2 ? r.right - w : r.left;
  el.style.left = Math.round(Math.max(m, Math.min(left, innerWidth - w - m))) + "px";
  if (cy > innerHeight / 2) {
    el.style.top = "auto";
    el.style.bottom = Math.round(Math.max(m, innerHeight - r.top + gap)) + "px";
    el.style.maxHeight = Math.round(Math.max(200, r.top - gap - m)) + "px";
  } else {
    el.style.bottom = "auto";
    el.style.top = Math.round(Math.min(r.bottom + gap, innerHeight - 200)) + "px";
    el.style.maxHeight = Math.round(Math.max(200, innerHeight - r.bottom - gap - m)) + "px";
  }
}

function abrirMenu() {
  abaMenu = "pet";
  montarMenu();
  menu.classList.remove("oculto");
  posicionaPertoDoPet(menu);
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
  posicionaJunto(telaPanel);

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
      cfg.borda = k; cfg.offset = 120; cfg.canto = ""; aplicaPosicao(); salvarPet();
      try { G && await G.ConfigurarTela(telaSel.monitor, k); } catch (e) {}
      fecharSeletorTela();
    };
    boxC.appendChild(b);
  });

  document.getElementById("tela-atual").textContent =
    "Tela " + telaSel.monitor + " · borda " + cfg.borda + " · " + cfg.offset + "px — " +
    "ou arraste o boneco para onde quiser: ele gruda na borda (ou no canto) mais próximo.";
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
  posicionaJunto(emailPanel);
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
  pararCaminhada();   // pegou no meio de um passeio: ele para ali, e o clique/arrasto manda
  const sx = e.clientX, sy = e.clientY, ox = pet.offsetLeft, oy = pet.offsetTop;
  // zera aqui: se o último arrasto terminou fora do boneco, o click que limparia
  // isto não veio — e o próximo clique simples seria engolido como "arrasto"
  arrastouAgora = false;
  arrastando = true; pet.classList.add("arrastando");
  const move = (ev) => {
    if (!arrastando) return;
    if (!arrastouAgora && Math.abs(ev.clientX - sx) + Math.abs(ev.clientY - sy) > 6) {
      arrastouAgora = true;
      ajustaFolga(true);   // desgrudou: o desenho volta para dentro da caixa e segue o cursor
    }
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

// o bicho está dormindo? (ele fica na tela cochilando; um clique acorda — bicho.js)
const petDormindo = () => { try { return !!(window.bicho && window.bicho.estaDormindo && window.bicho.estaDormindo()); } catch (e) { return false; } };

/* CLIQUE = AGRADO (carinho), nunca chat. A conversa é no DUPLO CLIQUE ou no
   tile "Conversar" — regra do dono: "clicar nele não quer dizer que eu
   quero um chat com ele, é só um agrado". */
pet.addEventListener("click", () => {
  fecharMenu();
  try { if (G && G.Log) G.Log("clique: click no boneco | interagir=" + cfg.interagir + " | arrastouAgora=" + arrastouAgora); } catch (e2) { }
  if (!cfg.interagir) return;                           // interação desligada: nem carinho
  if (arrastouAgora) { arrastouAgora = false; return; } // foi arrasto, não clique
  if (petDormindo()) { window.bicho.acordar(); return; } // dormindo: o clique acorda
  carinho();
});
pet.addEventListener("dblclick", () => {
  fecharMenu();
  try { if (G && G.Log) G.Log("clique: DUPLO clique no boneco | interagir=" + cfg.interagir); } catch (e2) { }
  if (!cfg.interagir) return;                           // interação desligada: sem chat
  if (petDormindo()) return;                            // o clique simples já acordou
  abrirBalao(); txt.focus();
});
// BOTÃO DIREITO SEMPRE: é a porta de volta quando a interação está desligada.
pet.addEventListener("contextmenu", (e) => { e.preventDefault(); abrirMenu(e.clientX, e.clientY); });
// RODINHA em cima do boneco = tamanho (5% por passo). O freio de 90 ms segura o
// touchpad, que manda dezenas de eventos num gesto só.
let rodaUltima = 0;
pet.addEventListener("wheel", (e) => {
  e.preventDefault();
  if (!cfg.interagir) return;
  const agora = performance.now();
  if (agora - rodaUltima < 90 || !e.deltaY) return;
  rodaUltima = agora;
  mudaTamanho(escala() + (e.deltaY < 0 ? 0.05 : -0.05));
}, { passive: false });

document.addEventListener("click", (e) => {
  // composedPath: trocar de aba REFAZ o menu, e o alvo do clique já saiu do DOM
  if (!e.composedPath().includes(menu) && e.target !== pet && !pet.contains(e.target)) {
    if (Date.now() - ultimoMenu > 300) fecharMenu();
  }
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") { fecharMenu(); fecharBalao(); fecharSeletorTela(); fecharEmail(); fecharAparencia(); if (typeof fecharInteligencia === "function") fecharInteligencia(); if (window.bicho && window.bicho.fecharPainel) window.bicho.fecharPainel(); }
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
  // PROGRESSO AO VIVO (10/10/2026) — o dono: "eu não fico nunca no vácuo aqui no chat".
  // Medido: no Telegram cada passo do agente vira uma mensagem (11 ferramentas = 11
  // mensagens); aqui eram 9 ferramentas virando UMA no fim, com 224 s de balão parado.
  // ⚠️ NÃO abre o balão (regra do dono: nada abre o chat sozinho) — só pinta se já estiver
  // aberto. Ao chegar a resposta final, a linha ao vivo sai e entra a mensagem de verdade.
  R.EventsOn("chat-progresso", (p) => progressoAoVivo(p));
  // volta do "esconder N min": quem conta o tempo é o Go — aqui só damos o recado
  // (balãozinho, que é a voz do bicho: não abre chat nem entra no histórico)
  R.EventsOn("volta-escondido", (min) => {
    toast("👋", "Voltei!", "fiquei " + min + " min fora, capturas liberadas", 3200, true);
  });

  R.EventsOn("tela-aplicada", (t) => {
    // payload: {borda, w, h, dpi, monitor} — a janela passa a cobrir a tela escolhida.
    // A borda do payload NÃO manda: quem sabe onde o boneco foi solto é o cfg salvo
    // (pets.json). O config.json do Go só muda pelo seletor de tela — que já põe a
    // borda no cfg antes de chamar o Go —, então no boot ele pode vir velho e, depois
    // de um arrasto, devolver o boneco para a borda antiga ao reiniciar o app.
    if (t && typeof t === "object") {
      cfg.monitor = t.monitor || cfg.monitor;
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
  atualizaInfoChat();
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
    "Pronto. Duplo clique em mim para conversar.", 8000, true), 800);
  log("frontend boot ok");
})();
