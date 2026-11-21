// Пиксельные иконки меню — те же, что в мобильном Mobilix (Ui.kt), и та же палитра.
const ICONS = {
  USER: ["....gggg....", "...gGGGGg...", "...gGGGGg...", "...gGGGGg...", "....gggg....", "............", "..gggggggg..", ".gGGGGGGGGg.", ".gGGGGGGGGg.", ".gGGGGGGGGg.", ".gggggggggg."],
  GEAR: [".....ggg.....", "..gg.gGg.gg..", ".gGGgGGGgGGg.", "..gGGgggGGg..", "..gGg...gGg..", "ggGg.....gGgg", "gGGg.....gGGg", "ggGg.....gGgg", "..gGg...gGg..", "..gGGgggGGg..", ".gGGgGGGgGGg.", "..gg.gGg.gg..", ".....ggg....."],
  VERS: ["....g....", "....G....", "....G....", ".gggGggg.", "..gGGGg..", "...gGg...", "....g....", "g.......g", "ggggggggg"],
  CAT: ["gggg.gggg", "gGGg.gGGg", "gGGg.gGGg", "gggg.gggg", ".........", "gggg.gggg", "gGGg.gGGg", "gGGg.gGGg", "gggg.gggg"],
  CHAT: [".ggggggg.", "gGGGGGGGg", "gGWGWGWGg", "gGGGGGGGg", ".ggggggg.", "..gG.....", "..g......"],
  GLOBE: [".....GGG.....", "...GGGGGGG...", "..G.GG.GG.G..", ".G.GG...GG.G.", ".G.G.....G.G.", "G..G.....G..G", "GGGGGGGGGGGGG", "G..G.....G..G", ".G.G.....G.G.", ".G.GG...GG.G.", "..G.GG.GG.G..", "...GGGGGGG...", ".....GGG....."],
  TRASH: ["...rrr...", "rrrrrrrrr", ".RRRRRRR.", ".RrRrRrR.", ".RrRrRrR.", ".RrRrRrR.", ".RrRrRrR.", ".RrRrRrR.", ".RRRRRRR."],
};
const MENU_PAL = { g: "#2EA84F", G: "#5BD67B", W: "#E8F5EC" };
const TRASH_PAL = { r: "#E5484D", R: "#A3231C" };

// Рисует иконку в canvas (1 пиксель иконки = 1 пиксель canvas), увеличение делает CSS без сглаживания.
function pixIcon(name, pal, scale) {
  const rows = ICONS[name], h = rows.length, w = Math.max(...rows.map(r => r.length));
  const c = document.createElement("canvas");
  c.width = w; c.height = h;
  const x = c.getContext("2d");
  for (let j = 0; j < h; j++) for (let i = 0; i < rows[j].length; i++) {
    const col = pal[rows[j][i]];
    if (col) { x.fillStyle = col; x.fillRect(i, j, 1, 1); }
  }
  const s = scale || 2;
  c.style.width = w * s + "px"; c.style.height = h * s + "px";
  c.className = "pix";
  return c;
}
