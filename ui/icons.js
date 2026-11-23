// Пиксельные иконки меню — те же, что в мобильном Mobilix (Ui.kt), и та же палитра.
const ICONS = {
  USER: ["....gggg....", "...gGGGGg...", "...gGGGGg...", "...gGGGGg...", "....gggg....", "............", "..gggggggg..", ".gGGGGGGGGg.", ".gGGGGGGGGg.", ".gGGGGGGGGg.", ".gggggggggg."],
  GEAR: [".....ggg.....", "..gg.gGg.gg..", ".gGGgGGGgGGg.", "..gGGgggGGg..", "..gGg...gGg..", "ggGg.....gGgg", "gGGg.....gGGg", "ggGg.....gGgg", "..gGg...gGg..", "..gGGgggGGg..", ".gGGgGGGgGGg.", "..gg.gGg.gg..", ".....ggg....."],
  VERS: ["....g....", "....G....", "....G....", ".gggGggg.", "..gGGGg..", "...gGg...", "....g....", "g.......g", "ggggggggg"],
  CAT: ["gggg.gggg", "gGGg.gGGg", "gGGg.gGGg", "gggg.gggg", ".........", "gggg.gggg", "gGGg.gGGg", "gGGg.gGGg", "gggg.gggg"],
  CHAT: [".ggggggg.", "gGGGGGGGg", "gGWGWGWGg", "gGGGGGGGg", ".ggggggg.", "..gG.....", "..g......"],
  GLOBE: [".....GGG.....", "...GGGGGGG...", "..G.GG.GG.G..", ".G.GG...GG.G.", ".G.G.....G.G.", "G..G.....G..G", "GGGGGGGGGGGGG", "G..G.....G..G", ".G.G.....G.G.", ".G.GG...GG.G.", "..G.GG.GG.G..", "...GGGGGGG...", ".....GGG....."],
  NET: ["....ggg....", "....gGg....", "....ggg....", ".....g.....", "ggggggggggg", "g....g....g", "ggg.ggg.ggg", "gGg.gGg.gGg", "ggg.ggg.ggg"],
  PENCIL: ["......gg.", ".....gGGg", "....gGGg.", "...gGGg..", "..gGGg...", ".gGGg....", "gGGg.....", "ggg......"],
  SHARE: ["....gg...", "...gGg...", "..gGGgggg", "...gGg..g", "....gg..g", "g.......g", "g.......g", "ggggggggg"],
  PUMPKIN: ["....uu...", "..oouoo..", ".ooooooo.", "ookoookoo", "ookkokkoo", "ooooooooo", "okokokoko", ".okkkkko.", "..OOOOO.."],
  SNOW: ["...w...", ".w.w.w.", "..www..", "wwwwwww", "..www..", ".w.w.w.", "...w..."],
  BAT: ["k.......k", "kk.k.k.kk", "kkkkkkkkk", ".kk.k.kk."],
  CREEPER: ["aaaaaaaa", "aAaaaaAa", "akkaakka", "akkaakka", "aaakkaaa", "aakkkkaa", "aakkkkaa", "aakaakaa"],
  CHICKEN: ["...rr...", "..wkwo..", "..wwwo..", ".wwww...", "wwwww...", "wwwww...", ".wwww...", "..o.o..."],
  TRASH: ["...rrr...", "rrrrrrrrr", ".RRRRRRR.", ".RrRrRrR.", ".RrRrRrR.", ".RrRrRrR.", ".RrRrRrR.", ".RrRrRrR.", ".RRRRRRR."],
};
const MENU_PAL = { g: "#2EA84F", G: "#5BD67B", W: "#E8F5EC" };
const TRASH_PAL = { r: "#E5484D", R: "#A3231C" };
const FX_PAL = {
  halloween: { PUMPKIN: { o: "#F28A1E", O: "#C25A0A", u: "#4C9A2A", k: "#2A1608" }, BAT: { k: "#2A1608" } },
  newyear: { SNOW: { w: "#BFE4FF" } },
  mcbday: { CREEPER: { a: "#4CAF50", A: "#2E7D32", k: "#0B0B0B" } },
  aprilfools: { CHICKEN: { w: "#F2F2F2", r: "#E5484D", o: "#F5A623", k: "#222222" } },
};
// иконки перекрашиваются в оттенок акцента своей темы (красная корзина остаётся красной)
window.ICON_TINT = null;
function tintHex(hex, acc) {
  const p = x => [1, 3, 5].map(i => parseInt(x.slice(i, i + 2), 16) / 255);
  const rgb2hsl = ([r, g, b]) => { const mx = Math.max(r, g, b), mn = Math.min(r, g, b), l = (mx + mn) / 2, d = mx - mn; let h = 0, s = 0;
    if (d) { s = d / (1 - Math.abs(2 * l - 1)); h = mx === r ? ((g - b) / d + 6) % 6 : mx === g ? (b - r) / d + 2 : (r - g) / d + 4; } return [h * 60, s, l]; };
  const [h, s] = rgb2hsl(p(acc)), l = rgb2hsl(p(hex))[2];
  const f = n => { const k = (n + h / 30) % 12, a = s * Math.min(l, 1 - l); return l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1)); };
  return "#" + [f(0), f(8), f(4)].map(v => Math.round(v * 255).toString(16).padStart(2, "0")).join("");
}

// Рисует иконку в canvas (1 пиксель иконки = 1 пиксель canvas), увеличение делает CSS без сглаживания.
function pixIcon(name, pal, scale) {
  const rows = ICONS[name], h = rows.length, w = Math.max(...rows.map(r => r.length));
  const c = document.createElement("canvas");
  c.width = w; c.height = h;
  const x = c.getContext("2d");
  for (let j = 0; j < h; j++) for (let i = 0; i < rows[j].length; i++) {
    let col = pal[rows[j][i]];
    if (col && window.ICON_TINT && pal !== TRASH_PAL && pal.__fx !== 1) col = tintHex(col, window.ICON_TINT);
    if (col) { x.fillStyle = col; x.fillRect(i, j, 1, 1); }
  }
  const s = scale || 2;
  c.style.width = w * s + "px"; c.style.height = h * s + "px";
  c.className = "pix";
  return c;
}
