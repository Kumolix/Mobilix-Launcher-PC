"use strict";
// ---------- базовое ----------
const KEY = new URLSearchParams(location.search).get("k") || sessionStorage.getItem("k") || "";
try { const fxp = new URLSearchParams(location.search).get("fx"); if (fxp) sessionStorage.setItem("fxForce", fxp); } catch (e) {}
try { sessionStorage.setItem("k", KEY); history.replaceState(null, "", location.pathname); } catch (e) {}
const $ = s => document.querySelector(s);
let S = null;                       // состояние с сервера
let STATUS = { busy: false };
let page = null;                    // открытая страница
const t = (ru, en) => (S && S.settings.lang === "en") ? en : ru;

function h(tag, attrs, ...kids) {
  const e = document.createElement(tag);
  if (attrs) for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") e.className = v;
    else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
    else if (v !== false && v != null) e.setAttribute(k, v === true ? "" : v);
  }
  for (const c of kids.flat()) if (c != null && c !== false) e.append(c.nodeType ? c : document.createTextNode(c));
  return e;
}

async function api(path, body) {
  const r = await fetch("/api/" + path, {
    method: body !== undefined ? "POST" : "GET",
    headers: { "X-K": KEY, "Content-Type": "application/json" },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  let j = null;
  try { j = await r.json(); } catch (e) {}
  if (!r.ok) throw new Error((j && j.error) || r.statusText);
  return j;
}

let toastT;
function toast(msg) {
  document.querySelectorAll("#toast").forEach(x => x.remove());
  const x = h("div", { id: "toast" }, msg);
  document.body.append(x);
  clearTimeout(toastT); toastT = setTimeout(() => x.remove(), 3600);
}

// ---------- оформление ----------
const PAL = {
  "new-dark":  { bg: "#0C0D0F", bar: "#141518", drawer: "#111214", skinBg: "#131519", accent: "#4ADE80", text: "#F2F4F7", text2: "#C9CED6", dim: "#8A919C", line: "#25282D", line2: "#1B1D21", chip: "#24282E", chip2: "#1B1E23", accentDk: "#1F7A45", hint: "#5C636E" },
  "new-light": { bg: "#F4F5F7", bar: "#E9EBEE", drawer: "#FFFFFF", skinBg: "#E3E6EA", accent: "#1B7F3A", text: "#14171B", text2: "#2F353D", dim: "#626A75", line: "#D6DAE0", line2: "#E3E6EB", chip: "#DADDE3", chip2: "#CBCFD6", accentDk: "#1F7A45", hint: "#8E96A1" },
  "old-dark":  { bg: "#0B0F0C", bar: "#121A15", drawer: "#0F1712", skinBg: "#0E1F14", accent: "#43E06A", text: "#FFFFFF", text2: "#B8E6C4", dim: "#7FA08A", line: "#22332A", line2: "#1B2A20", chip: "#14301D", chip2: "#0F2416", accentDk: "#1B6E34", hint: "#58705F" },
  "old-light": { bg: "#F1F6F2", bar: "#E2EDE5", drawer: "#FFFFFF", skinBg: "#DCEBDF", accent: "#1B7F3A", text: "#10200F", text2: "#2B4B33", dim: "#5A7A62", line: "#CADBCE", line2: "#DCE8DF", chip: "#CFE3D4", chip2: "#BFD6C5", accentDk: "#1B6E34", hint: "#8AA391" },
};
// шесть базовых цветов своей темы; остальные оттенки считаются из них
const BASE = [["bg", "Фон", "Background"], ["drawer", "Панели", "Panels"], ["skinBg", "Окно скина", "Skin window"], ["accent", "Акцент", "Accent"], ["text", "Текст", "Text"], ["chip", "Кнопки", "Buttons"]];

function mix(a, b, k) {
  const p = x => [1, 3, 5].map(i => parseInt(x.slice(i, i + 2), 16));
  const A = p(a), B = p(b);
  return "#" + A.map((v, i) => Math.round(v + (B[i] - v) * k).toString(16).padStart(2, "0")).join("");
}
function themeFrom(c) {
  return { bg: c.bg, bar: mix(c.bg, c.drawer, .5), drawer: c.drawer, skinBg: c.skinBg, accent: c.accent, text: c.text, text2: mix(c.text, c.bg, .2),
    dim: mix(c.text, c.bg, .5), line: mix(c.chip, c.bg, .5), line2: mix(c.chip, c.bg, .7), chip: c.chip, chip2: mix(c.chip, c.bg, .45),
    accentDk: mix(c.accent, c.bg, .5), hint: mix(c.text, c.bg, .65) };
}
function curCustom() { const s = S.settings; return s.cSel >= 0 && s.cThemes && s.cThemes[s.cSel] ? s.cThemes[s.cSel] : null; }

function applyTheme() {
  const s = S.settings, cu = curCustom();
  const p = cu ? themeFrom(cu.colors) : PAL[(s.style === "old" ? "old" : "new") + "-" + (s.light ? "light" : "dark")];
  const r = document.documentElement.style;
  for (const [k, v] of Object.entries(p)) r.setProperty("--" + k, v);
  const act = cu ? mix(p.accent, "#000000", .28) : "#2EA84F";
  r.setProperty("--act", act);
  r.setProperty("--hi", mix(act, p.accent, 0.55));
  const light = cu ? lum(p.bg) > .5 : s.light;
  r.setProperty("--shadow", light ? "rgba(0,0,0,.22)" : "rgba(0,0,0,.45)");
  $("#aurora").classList.toggle("off", !s.aurora);
  const tint = cu ? p.accent : null;
  if (window.ICON_TINT !== tint) { window.ICON_TINT = tint; try { buildMenus(); renderFxBtns(); } catch (e) {} }
}
function lum(hex) { const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255); return .2126 * r + .7152 * g + .0722 * b; }

async function save(patch) {
  const r = await api("settings", patch);
  S.settings = r.settings;
  return r.settings;
}

// ---------- меню ----------
const MENU = [
  { id: "account", ru: "Аккаунт", en: "Account", ico: "USER" },
  { id: "downloads", ru: "Загрузки", en: "Downloads", ico: "VERS" },
  { id: "net", ru: "Сеть", en: "Network", ico: "NET" },
  { id: "social", ru: "Соцсети", en: "Social", ico: "CHAT" },
  { id: "settings", ru: "Настройки", en: "Settings", ico: "GEAR" },
  { id: "wipe", ru: "Удалить скачанное", en: "Delete downloads", ico: "TRASH", pal: TRASH_PAL },
  { id: "lang", ru: "Язык", en: "Language", ico: "GLOBE", badge: true },
];

function menuAction(id) {
  setDrawer(false);
  if (id === "wipe") return confirmWipe();
  if (id === "lang") return chooseLang();
  openPage(id);
}

let closeT = 0;
function setDrawer(o) {
  $("#drawer").classList.toggle("open", o);
  $("#scrim").classList.toggle("on", o);
}

function buildMenus() {
  const side = S.settings.menuType === "side";
  document.body.classList.toggle("side", side);
  const dr = $("#drawer"); dr.innerHTML = "";
  dr.append(h("div", { class: "dt" }, h("div", { class: "logo" }, "Mobilix")));
  for (const m of MENU) {
    const it = h("div", { class: "mi", onclick: () => menuAction(m.id) },
      h("div", { class: "ic" }, pixIcon(m.ico, m.pal || MENU_PAL, 3)), h("span", null, t(m.ru, m.en)));
    if (m.badge) it.append(h("span", { class: "badge" }, S.settings.lang.toUpperCase()));
    dr.append(it);
  }
  if (!side) setDrawer(false);
  const slot = $("#dockSlot"); slot.innerHTML = "";
  if (!side) {
    const dock = h("div", { class: "dock" });
    for (const m of MENU) {
      dock.append(h("div", { class: "cell" + (page === m.id ? " cur" : ""), "data-l": t(m.ru, m.en), onclick: () => menuAction(m.id) },
        pixIcon(m.ico, m.pal || MENU_PAL, 3)));
    }
    slot.append(dock);
  }
  $("#pageLayer").classList.toggle("dockpad", !side);
}

function initHover() {
  const dr = $("#drawer");
  dr.addEventListener("mouseenter", () => { if (S.settings.menuType === "side") { clearTimeout(closeT); setDrawer(true); } });
  dr.addEventListener("mouseleave", () => { closeT = setTimeout(() => setDrawer(false), 160); });
  document.documentElement.addEventListener("mouseleave", () => setDrawer(false));
}

// ---------- шапка, версия, кнопка «Играть» ----------
const LOADERS = [["vanilla", "Vanilla"], ["fabric", "Fabric"], ["quilt", "Quilt"], ["forge", "Forge"], ["neoforge", "NeoForge"]];
const loaderName = l => (LOADERS.find(x => x[0] === l) || LOADERS[0])[1];
const instTitle = (mc, l) => mc + (l && l !== "vanilla" ? " • " + loaderName(l) : "");

function verTitle() {
  const s = S.settings;
  if (s.build) { const b = (s.builds || []).find(x => x.id === s.build); if (b) return b.name + " · " + instTitle(b.mc, b.loader); }
  if (!s.selected) return t("Выберите версию", "Pick a version");
  return instTitle(s.selected, s.loader);
}
const hasSel = () => !!(S.settings.selected || S.settings.build);

function loginLabel() {
  const a = S.account || {};
  return a.type === "ely" ? "Ely.by" : t("Автономный", "Offline");
}

function renderChrome() {
  const s = S.settings;
  $("#btnLog").textContent = t("Лог", "Log");
  $("#btnExit").title = t("Выход", "Exit");
  $("#nickLabel").textContent = (S.account && S.account.name) || s.nick;
  $("#loginTag").textContent = loginLabel();
  $("#skinLoad").textContent = t("загрузка скина…", "loading skin…");
  const vl = $("#verLabel");
  vl.textContent = "PC " + S.version + " · Kumolix" + (S.update ? " · " + t("доступно обновление ", "update available ") + S.update : "");
  vl.classList.toggle("upd", !!S.update);
  $("#verBtn").textContent = verTitle();
  $("#tabLatest").textContent = t("Последний", "Latest");
  $("#tabPrev").textContent = t("Предыдущий", "Previous");
  $("#tabCopy").textContent = t("Копировать", "Copy");
  $("#tabStop").textContent = t("Остановить игру", "Stop game");
  renderStatus();
  renderFxBtns();
}

function renderStatus() {
  const st = STATUS, pb = $("#playBtn"), pr = $("#progress");
  pb.classList.toggle("cancel", st.busy);
  pb.disabled = !!st.running && !st.busy;
  pb.textContent = st.busy ? t("Отмена", "Cancel") : st.running ? t("Игра идёт", "Running") : t("Играть", "Play");
  pr.classList.toggle("ind", st.busy && st.percent < 0);
  pr.firstElementChild.style.width = st.busy && st.percent >= 0 ? st.percent + "%" : (st.busy ? "" : "0");
  const txt = $("#stText");
  txt.classList.toggle("err", !!st.error && !st.busy);
  txt.textContent = st.busy ? (st.stage || "…") : (st.error ? st.error : (st.running ? t("Игра запущена", "Game is running") : ""));
  txt.title = txt.textContent;
  $("#stPct").textContent = st.busy && st.percent >= 0 ? st.percent + "%" : "";
  $("#tabStop").classList.toggle("hidden", !st.running);
}

async function onPlay() {
  if (STATUS.busy) { api("cancel", {}); return; }
  if (!hasSel()) { openPage("downloads"); toast(t("Сначала выберите версию игры", "Pick a game version first")); return; }
  try { await api("play", {}); STATUS = { ...STATUS, busy: true, error: "", percent: -1, stage: "…" }; renderStatus(); pollSoon(); }
  catch (e) { toast(e.message); }
}

let pollT = 0, wasBusy = false;
function pollSoon() { clearTimeout(pollT); pollT = setTimeout(poll, 200); }
async function poll() {
  try {
    STATUS = await api("status");
    if ((STATUS.update || "") !== (S.update || "")) { S.update = STATUS.update || ""; renderChrome(); if (page === "upd" || page === "settings") renderPage(); }
    if (wasBusy && !STATUS.busy) {
      const st = await api("state"); S.installed = st.installed; S.settings = st.settings;
      renderChrome();
      if (!STATUS.error && hasSel()) toast(t("Готово", "Done"));
      if (page === "downloads") renderPage();
    }
    wasBusy = STATUS.busy;
    renderStatus();
  } catch (e) {}
  pollT = setTimeout(poll, STATUS.busy ? 400 : 1500);
}

// выбор установки снизу: каждая связка «версия + загрузчик» и каждая сборка — отдельная установка
async function toggleVerPopup(force) {
  const p = $("#verPopup");
  const open = force !== undefined ? force : p.classList.contains("hidden");
  p.classList.toggle("hidden", !open);
  if (!open) return;
  p.innerHTML = "";
  const s = S.settings;
  let list = [];
  try { list = (await api("installed")).list; } catch (e) {}
  if (!list.length && !(s.builds || []).length) p.append(h("div", { class: "it", style: "color:var(--dim);cursor:default" }, t("Скачанных версий пока нет", "No downloaded versions yet")));
  for (const x of list) {
    const cur = !s.build && x.mc === s.selected && x.loader === (s.loader || "vanilla");
    p.append(h("div", { class: "it" + (cur ? " cur" : ""), onclick: async () => { await save({ selected: x.mc, loader: x.loader, loaderVer: "", build: "" }); renderChrome(); toggleVerPopup(false); } }, instTitle(x.mc, x.loader)));
  }
  if ((s.builds || []).length) {
    p.append(h("div", { class: "sepl" }));
    for (const b of s.builds) p.append(h("div", { class: "it" + (s.build === b.id ? " cur" : ""), onclick: async () => { await save({ build: b.id }); renderChrome(); toggleVerPopup(false); } }, "▣ " + b.name + " · " + instTitle(b.mc, b.loader)));
  }
  p.append(h("div", { class: "sepl" }));
  p.append(h("div", { class: "it", onclick: () => { toggleVerPopup(false); openPage("downloads"); } }, t("Все версии и загрузчики…", "All versions & loaders…")));
}

// ---------- лог (окно вместо скина) ----------
let logOpen = false, logTab = "live", logNext = 0, logLines = [], logTimer = 0;

function showLog(on) {
  logOpen = on;
  $("#logWin").classList.toggle("hidden", !on);
  $("#btnLog").classList.toggle("on", on);
  clearTimeout(logTimer);
  if (on) { setLogTab("live"); }
}

async function setLogTab(tab) {
  logTab = tab;
  $("#tabLatest").classList.toggle("on", tab === "live");
  $("#tabPrev").classList.toggle("on", tab === "prev");
  const pre = $("#logText");
  clearTimeout(logTimer);
  if (tab === "prev") {
    const r = await fetch("/api/log/file?prev=1", { headers: { "X-K": KEY } });
    pre.textContent = (await r.text()) || t("Лога пока нет.", "No log yet.");
    pre.scrollTop = pre.scrollHeight;
    return;
  }
  logLines = []; logNext = 0;
  const first = await api("log?from=0");
  logLines = first.lines; logNext = first.next;
  if (!logLines.length) {
    const r = await fetch("/api/log/file", { headers: { "X-K": KEY } });
    const txt = await r.text();
    if (txt) logLines = txt.split("\n");
  }
  pre.textContent = logLines.join("\n") || t("Лога пока нет.", "No log yet.");
  pre.scrollTop = pre.scrollHeight;
  const loop = async () => {
    if (!logOpen || logTab !== "live") return;
    try {
      const r = await api("log?from=" + logNext);
      if (r.lines.length) {
        const nearEnd = pre.scrollHeight - pre.scrollTop - pre.clientHeight < 60;
        logLines.push(...r.lines); if (logLines.length > 3000) logLines = logLines.slice(-2500);
        logNext = r.next;
        pre.textContent = logLines.join("\n");
        if (nearEnd) pre.scrollTop = pre.scrollHeight;
      }
    } catch (e) {}
    logTimer = setTimeout(loop, 600);
  };
  logTimer = setTimeout(loop, 600);
}

// ---------- модальные окна ----------
function modal(title, body, buttons) {
  const L = $("#modalLayer");
  L.innerHTML = ""; L.classList.remove("hidden");
  const close = () => { L.classList.add("hidden"); L.innerHTML = ""; };
  L.onclick = e => { if (e.target === L) close(); };
  const bt = h("div", { class: "mbtns" });
  for (const b of buttons) bt.append(h("button", { class: "btn " + (b.cls || ""), onclick: () => { close(); b.fn && b.fn(); } }, b.text));
  L.append(h("div", { class: "modal" }, h("h3", null, title), body, bt));
  return close;
}

function confirmWipe() {
  modal(t("Удалить скачанное?", "Delete downloads?"),
    h("p", null, t("Будут удалены версии, библиотеки, ресурсы игры и скачанная Java. Миры, моды и настройки останутся. Потом всё скачается заново при запуске.",
      "Versions, libraries, game assets and the downloaded Java will be removed. Worlds, mods and settings stay. Everything is re-downloaded on the next launch.")),
    [{ text: t("Отмена", "Cancel") }, { text: t("Удалить", "Delete"), cls: "dng", fn: async () => {
      try { await api("wipe", {}); const st = await api("state"); S.installed = st.installed; toast(t("Удалено", "Deleted")); } catch (e) { toast(e.message); } } }]);
}

function chooseLang() {
  const cur = S.settings.lang;
  modal(t("Язык", "Language"), h("div", { class: "seg", style: "justify-content:center" },
    ["ru", "en"].map(l => h("button", { class: l === cur ? "on" : "", onclick: async () => { await save({ lang: l }); $("#modalLayer").classList.add("hidden"); relang(); } }, l === "ru" ? "Русский" : "English"))), [{ text: t("Закрыть", "Close") }]);
}

function relang() { document.documentElement.lang = S.settings.lang; buildMenus(); renderChrome(); if (page) renderPage(); }

// ---------- скин ----------
let skinTok = 0;
async function loadSkin() {
  const tok = ++skinTok, s = S.settings, card = $("#skinCard");
  card.classList.add("loading");
  const done = () => { if (tok === skinTok) card.classList.remove("loading"); };
  try {
    const r = await fetch("/api/skin", { headers: { "X-K": KEY } });
    if (!r.ok) throw new Error("no skin");
    const hint = r.headers.get("X-Skin-Model") || "";
    const url = URL.createObjectURL(await r.blob());
    const img = new Image();
    await new Promise((ok, bad) => { img.onload = ok; img.onerror = bad; img.src = url; });
    if (tok === skinTok) Skin3D.setSkin(img, s.skinModel, hint);
  } catch (e) { if (tok === skinTok) Skin3D.setSkin(null, 0, ""); }
  done();
}

// ---------- страницы ----------
const TITLES = { net: ["Игра по сети", "Online over LAN"], buildcat: ["Каталог сборки", "Build catalog"], account: ["Аккаунт", "Account"], settings: ["Настройки", "Settings"], downloads: ["Загрузки", "Downloads"], social: ["Соцсети", "Social"] };

function openPage(id) { page = id; buildMenus(); renderPage(); }
function closePage() { page = null; catBuild = ""; $("#pageLayer").classList.add("hidden"); $("#pageLayer").innerHTML = ""; buildMenus(); }

function renderPage() {
  if (!page) return;
  const L = $("#pageLayer");
  L.classList.remove("hidden");
  const old = L.querySelector(".pbody"), scroll = old ? old.scrollTop : 0;
  const body = h("div", { class: "pbody" }, h("div", { class: "pin" }, PAGES[page]()));
  L.innerHTML = "";
  L.append(h("div", { class: "page" },
    h("div", { class: "phead" },
      h("button", { class: "back", onclick: closePage, title: t("Назад", "Back") }, svg("M15 5 8 12l7 7")),
      h("h2", null, t(...TITLES[page]))),
    body));
  body.scrollTop = scroll;
}

function svg(d) {
  const n = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  n.setAttribute("viewBox", "0 0 24 24");
  const p = document.createElementNS("http://www.w3.org/2000/svg", "path");
  p.setAttribute("d", d); n.append(p); return n;
}

function row(label, hint, ...ctl) {
  return h("div", { class: "row" }, h("div", { class: "l" }, h("b", null, label), hint ? h("small", null, hint) : null), h("div", { class: "c" }, ...ctl));
}
function sw(on, fn) {
  const e = h("div", { class: "sw" + (on ? " on" : ""), onclick: () => { on = !on; e.classList.toggle("on", on); fn(on); } });
  return e;
}
function seg(opts, val, fn) {
  const box = h("div", { class: "seg" });
  for (const [v, label] of opts) {
    box.append(h("button", { class: v === val ? "on" : "", onclick: () => { [...box.children].forEach(c => c.classList.remove("on")); box.children[opts.findIndex(o => o[0] === v)].classList.add("on"); fn(v); } }, label));
  }
  return box;
}
function tabs(opts, val, fn) {
  const box = h("div", { class: "tabs" });
  for (const [v, label] of opts) box.append(h("button", { class: v === val ? "on" : "", onclick: () => fn(v) }, label));
  return box;
}
function group(title, open, ...rows) {
  const g = h("div", { class: "grp" + (open ? "" : " closed") });
  g.append(h("div", { class: "gh", onclick: () => g.classList.toggle("closed") }, h("span", null, title), h("span", { class: "car" }, "▾")));
  g.append(h("div", { class: "gb" }, ...rows));
  return g;
}
function plain(...rows) { return h("div", { class: "grp" }, h("div", { class: "gb", style: "padding-top:6px" }, ...rows)); }

async function upload(type, file, extra) {
  const r = await fetch("/api/import?type=" + type + "&name=" + encodeURIComponent(file.name) + (extra || ""), { method: "POST", headers: { "X-K": KEY }, body: file });
  let j = {}; try { j = await r.json(); } catch (e) {}
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
}
function pickFile(accept, fn) {
  const f = h("input", { type: "file", accept, class: "hidden", onchange: () => { if (f.files[0]) fn(f.files[0]); f.remove(); } });
  document.body.append(f); f.click();
}

const PAGES = {};

// --- аккаунт ---
PAGES.account = () => {
  const s = S.settings, acc = S.account || {}, ely = acc.type === "ely";
  const nick = h("input", { type: "text", value: s.nick, maxlength: 16, spellcheck: "false", style: "width:220px", disabled: ely });
  const out = [];
  out.push(group(t("Вход", "Sign in"), true,
    row(t("Текущий вход", "Current sign-in"), ely ? t("Игра запускается с вашим аккаунтом Ely.by, скин берётся с Ely.by.", "The game starts with your Ely.by account; the skin comes from Ely.by.") : t("Автономный режим: игра запускается без аккаунта.", "Offline mode: the game starts without an account."),
      h("span", { class: "tag" }, loginLabel())),
    row(t("Ник", "Nickname"), ely ? acc.name : t("3–16 символов: латиница, цифры и _.", "3–16 characters: Latin letters, digits and _."),
      nick, h("button", { class: "btn pri", disabled: ely, onclick: async () => { await save({ nick: nick.value }); renderChrome(); renderPage(); loadSkin(); } }, t("Применить", "Apply"))),
    ely
      ? row("Ely.by", t("Выйти из аккаунта", "Sign out"), h("button", { class: "btn dng", onclick: async () => { await api("account/logout", {}); await refresh(); renderChrome(); renderPage(); loadSkin(); } }, t("Выйти", "Sign out")))
      : row("Ely.by", t("Войти через аккаунт Ely.by", "Sign in with your Ely.by account"), h("button", { class: "btn pri", onclick: elyForm }, t("Войти", "Sign in"))),
    row("Microsoft", t("Вход через Microsoft пока недоступен в этой версии.", "Microsoft sign-in is not available in this version yet."), h("button", { class: "btn", disabled: true }, t("Недоступно", "Unavailable")))));

  // скин
  const src = ely ? "ely" : s.skinSrc;
  const rows = [row(t("Источник скина", "Skin source"),
    ely ? t("Заблокирован: при входе через Ely.by используется скин вашего аккаунта.", "Locked: with an Ely.by sign-in your account skin is used.") : t("Скин показывается в лаунчере. В автономной игре он не применяется.", "Shown in the launcher. It is not applied in offline play."),
    (() => { const g = seg([["mojang", "Mojang"], ["ely", "Ely.by"], ["file", t("Файл", "File")]], src, async v => { await save({ skinSrc: v }); renderPage(); loadSkin(); }); if (ely) g.querySelectorAll("button").forEach(b => b.disabled = true); return g; })())];
  if (src === "file") {
    rows.push(row(t("Свой скин", "Custom skin"), t("PNG 64×64 или 64×32", "PNG 64×64 or 64×32"), h("button", { class: "btn", onclick: () => pickFile("image/png", async f => {
      const r = await fetch("/api/skin/import", { method: "POST", headers: { "X-K": KEY }, body: await f.arrayBuffer() });
      if (!r.ok) { try { toast((await r.json()).error); } catch (x) {} return; }
      await refresh(); renderPage(); loadSkin(); toast(t("Скин загружен", "Skin imported"));
    }) }, t("Выбрать файл…", "Choose file…"))));
  } else {
    const sn = h("input", { type: "text", value: ely ? acc.name : s.skinNick, placeholder: t("ник другого игрока", "another player's nickname"), maxlength: 16, spellcheck: "false", style: "width:220px", disabled: ely });
    rows.push(row(t("Ник для скина", "Skin nickname"), ely ? null : t("Пусто — скин вашего ника. Можно вписать ник любого игрока.", "Empty — your own nickname's skin. You can use any player's nickname."),
      sn, h("button", { class: "btn pri", disabled: ely, onclick: async () => { await save({ skinNick: sn.value.trim() }); loadSkin(); toast(t("Готово", "Done")); } }, t("Показать", "Show"))));
  }
  rows.push(row(t("Модель рук", "Arm model"), null,
    seg([[0, t("Авто", "Auto")], [1, t("Широкие", "Wide")], [2, t("Тонкие", "Slim")]], s.skinModel, async v => { await save({ skinModel: v }); loadSkin(); })));
  out.push(group(t("Скин", "Skin"), true, ...rows));
  return out;
};

function elyForm() {
  const u = h("input", { type: "text", placeholder: t("Логин или e-mail", "Login or e-mail"), style: "width:100%;margin-bottom:8px", spellcheck: "false" });
  const p = h("input", { type: "password", placeholder: t("Пароль", "Password"), style: "width:100%;margin-bottom:8px; background:var(--chip);border:1px solid var(--line);border-radius:12px;padding:9px 13px;color:inherit" });
  const c = h("input", { type: "text", placeholder: t("Код двухфакторной защиты", "Two-factor code"), style: "width:100%;display:none", spellcheck: "false" });
  const msg = h("p", { style: "color:#ff7d80;margin-top:6px" });
  const body = h("div", null, h("p", null, t("Пароль передаётся только на сервер Ely.by и нигде не сохраняется.", "The password goes only to Ely.by and is never stored.")), u, p, c, msg);
  const close = modal(t("Вход через Ely.by", "Sign in with Ely.by"), body, [{ text: t("Отмена", "Cancel") }]);
  const btn = h("button", { class: "btn pri", onclick: async () => {
    btn.disabled = true; msg.textContent = "";
    try {
      await api("account/ely", { user: u.value.trim(), pass: p.value, code: c.value.trim() });
      close(); await refresh(); renderChrome(); if (page) renderPage(); loadSkin(); toast(t("Вход выполнен", "Signed in"));
    } catch (e) {
      if (e.message === "2FA") { c.style.display = "block"; msg.textContent = t("Введите код из приложения", "Enter the code from your app"); }
      else msg.textContent = e.message;
      btn.disabled = false;
    }
  } }, t("Войти", "Sign in"));
  body.parentElement.querySelector(".mbtns").append(btn);
}

// --- настройки ---
let setTab = "launcher";
PAGES.settings = () => {
  const s = S.settings;
  const T = [["launcher", t("Лаунчер", "Launcher")], ["game", t("Игра", "Game")], ["memory", t("Память и Java", "Memory & Java")], ["gfx", t("Графика", "Graphics")], ["net", t("Загрузка", "Network")], ["upd", t("Обновления", "Updates")], ["special", t("Специальное", "Special")]];
  const out = [tabs(T, setTab, v => { setTab = v; renderPage(); })];
  const maxMb = Math.max(2048, Math.min(32768, (S.ramMb || 16384) - 1024));
  const sel = (opts, val, fn, w) => { const e = h("select", { style: w ? "width:" + w : "", onchange: () => fn(e.value) }, opts.map(([v, l]) => h("option", { value: v, selected: String(v) === String(val) }, l))); return e; };
  if (setTab === "launcher") {
    const thumbs = h("div", { class: "themes" });
    const mk = (label, colors, on, fn) => h("div", { class: "tcard" + (on ? " on" : ""), onclick: fn }, h("i", { style: `background:linear-gradient(90deg,${colors.bg} 0 45%,${colors.accent} 45% 70%,${colors.chip} 70%)` }), label);
    for (const [st, li, lbl] of [["new", false, t("Графит", "Graphite")], ["new", true, t("Графит светл.", "Graphite light")], ["old", false, t("Классика", "Classic")], ["old", true, t("Классика светл.", "Classic light")]]) {
      const p = PAL[st + "-" + (li ? "light" : "dark")];
      thumbs.append(mk(lbl, p, s.cSel < 0 && s.style === st && s.light === li, async () => { await save({ style: st, light: li, cSel: -1 }); applyTheme(); renderPage(); }));
    }
    (s.cThemes || []).forEach((ct, i) => thumbs.append(mk(ct.name || t("Своя ", "Custom ") + (i + 1), themeFrom(ct.colors), s.cSel === i, async () => { await save({ cSel: i }); applyTheme(); renderPage(); })));
    out.push(plain(
      row(t("Тема", "Theme"), t("Свои темы — до 4. Нажмите «Создать», чтобы выбрать шесть базовых цветов.", "Up to 4 custom themes. Press “Create” to pick six base colors."), h("div")),
      thumbs,
      h("div", { class: "tools", style: "margin-top:10px" },
        h("button", { class: "btn pri", disabled: (s.cThemes || []).length >= 4, onclick: () => themeEditor(-1) }, t("Создать тему", "Create theme")),
        curCustom() ? h("button", { class: "btn", onclick: () => themeEditor(s.cSel) }, t("Изменить", "Edit")) : null,
        curCustom() ? h("button", { class: "btn dng", onclick: async () => { const l = s.cThemes.filter((_, i) => i !== s.cSel); await save({ cThemes: l, cSel: -1 }); applyTheme(); renderPage(); } }, t("Удалить", "Delete")) : null),
      row(t("Меню", "Menu"), t("Боковое — панель иконок слева, раскрывается при наведении. Нижнее — панель по центру внизу.", "Side — an icon rail on the left that expands on hover. Bottom — a dock in the centre."),
        seg([["bottom", t("Нижнее", "Bottom")], ["side", t("Боковое", "Side")]], s.menuType, async v => { await save({ menuType: v }); buildMenus(); renderPage(); })),
      row(t("Анимированный фон", "Animated background"), null, sw(s.aurora, async v => { await save({ aurora: v }); applyTheme(); })),
      row(t("Язык", "Language"), null, seg([["ru", "Русский"], ["en", "English"]], s.lang, async v => { await save({ lang: v }); relang(); }))));
  }
  if (setTab === "game") {
    out.push(plain(
      row(t("Полный экран", "Fullscreen"), null, sw(s.fullscreen, v => save({ fullscreen: v }))),
      row(t("Отдельная папка для каждой установки", "Separate folder per installation"), t("Моды и миры не смешиваются между версиями и загрузчиками.", "Mods and worlds stay separate between versions and loaders."), sw(s.isolate, v => save({ isolate: v }))),
      row("Corelix", t("Оптимизирующее ядро. Работает только на чистой Vanilla 1.21.3 (без загрузчика).", "Optimising core. Works only on plain Vanilla 1.21.3 (no loader)."), sw(s.corelix, v => save({ corelix: v }))),
      row(t("Резервные копии миров", "World backups"), t("После закрытия игры сохраняются изменённые миры (последние 3).", "Changed worlds are saved after the game closes (last 3)."), sw(s.worldBackup, v => save({ worldBackup: v })), h("button", { class: "btn", onclick: backupsModal }, t("Копии…", "Backups…"))),
      row(t("Закрывать лаунчер после запуска", "Close launcher after start"), null, sw(s.closeOnPlay, v => save({ closeOnPlay: v })))));
  }
  if (setTab === "memory") {
    const ramLbl = h("span", { style: "min-width:64px;text-align:right" }, (s.ramMb / 1024).toFixed(1) + " " + t("ГБ", "GB"));
    const ram = h("input", { type: "range", min: 1024, max: maxMb, step: 256, value: Math.min(s.ramMb, maxMb), disabled: s.memAuto, oninput: () => { ramLbl.textContent = (ram.value / 1024).toFixed(1) + " " + t("ГБ", "GB"); }, onchange: () => save({ ramMb: +ram.value }) });
    const java = h("input", { type: "text", value: s.javaPath, placeholder: t("авто", "auto"), spellcheck: "false", style: "width:300px", onchange: () => save({ javaPath: java.value.trim() }) });
    const jvm = h("input", { type: "text", value: s.jvmArgs, placeholder: "-XX:+UseZGC", spellcheck: "false", style: "width:300px", onchange: () => save({ jvmArgs: jvm.value }) });
    out.push(plain(
      row(t("Память автоматически", "Automatic memory"), t("Половина оперативной памяти, от 2 до 8 ГБ.", "Half of your RAM, between 2 and 8 GB."), sw(s.memAuto, async v => { await save({ memAuto: v }); renderPage(); })),
      row(t("Память игры", "Game memory"), t("Не ставьте больше половины оперативной памяти.", "Don't use more than half of your RAM."), ram, ramLbl),
      row(t("Сборщик мусора", "Garbage collector"), null, sel([["", "G1 (" + t("по умолчанию", "default") + ")"], ["parallel", "Parallel"], ["serial", "Serial"], ["zgc", "ZGC"]], s.gc || "", v => save({ gc: v }))),
      row(t("Путь к Java", "Java path"), t("Пусто — подходящая Java найдётся или скачается сама.", "Empty — a suitable Java is found or downloaded automatically."), java),
      row(t("Аргументы JVM", "JVM arguments"), null, jvm)));
  }
  if (setTab === "gfx") {
    const PR = [[0, t("Не менять", "Keep")], [1, t("Минимальные", "Minimal")], [2, t("Низкие", "Low")], [3, t("Средние", "Medium")], [4, t("Высокие", "High")]];
    out.push(plain(
      row(t("Быстрые настройки", "Quick settings"), t("Один раз записываются в options.txt выбранной установки. Рендереры не затрагиваются.", "Written once to options.txt of the selected installation. Renderers are untouched."),
        sel(PR, s.preset, async v => { await save({ preset: +v, presetStamp: (s.presetStamp || 0) + 1 }); }, "170px")),
      row(t("Лимит FPS", "FPS limit"), t("0 — не менять", "0 — keep"), sel([[0, "—"], [30, "30"], [60, "60"], [120, "120"], [144, "144"], [240, "240"]], s.maxFps, v => save({ maxFps: +v }), "110px")),
      row(t("Мипмапы", "Mipmap levels"), null, sel([[-1, "—"], [0, "0"], [1, "1"], [2, "2"], [3, "3"], [4, "4"]], s.mip, v => save({ mip: +v }), "110px"))));
  }
  if (setTab === "net") {
    const api2 = h("input", { type: "text", value: s.mirrorApi, spellcheck: "false", style: "width:300px", onchange: () => save({ mirrorApi: api2.value.trim() }) });
    const thr = h("input", { type: "range", min: 1, max: 16, step: 1, value: s.threads, onchange: () => save({ threads: +thr.value }) });
    out.push(plain(
      row(t("Зеркала загрузки", "Download mirrors"), t("Если основной сервер недоступен, файлы качаются через зеркала (BMCLAPI).", "If the main server is unreachable, files are fetched through mirrors (BMCLAPI)."), sw(s.mirrors, v => save({ mirrors: v }))),
      row(t("Зеркало API Modrinth", "Modrinth API mirror"), t("Пробуется первым, затем официальный API.", "Tried first, then the official API."), api2),
      row(t("Источник каталога", "Catalog source"), S.cf ? null : t("CurseForge недоступен в этой сборке.", "CurseForge is not available in this build."), seg([["modrinth", "Modrinth"], ["curseforge", "CurseForge"]], s.modSource, v => save({ modSource: v }))),
      row(t("Потоки загрузки", "Download threads"), null, thr)));
  }
  if (setTab === "upd") {
    out.push(plain(
      row(t("Обновления лаунчера", "Launcher updates"), S.update ? t("Доступна версия ", "Version available: ") + S.update : t("Установлена последняя версия", "You have the latest version"),
        seg([[0, t("Выкл", "Off")], [1, t("Сообщать", "Notify")]], s.updMode, v => save({ updMode: v })),
        S.update ? h("button", { class: "btn pri", onclick: applyUpdate }, t("Обновить", "Update")) : null)));
  }
  if (setTab === "special") {
    out.push(plain(
      row(t("Папка игры", "Game folder"), null, h("button", { class: "btn", onclick: () => api("openfolder?which=game", {}) }, t("Открыть", "Open"))),
      row(t("Папка лаунчера", "Launcher folder"), S.dataDir, h("button", { class: "btn", onclick: () => api("openfolder?which=data", {}) }, t("Открыть", "Open"))),
      row(t("Скачанные файлы", "Downloaded files"), null, h("button", { class: "btn dng", onclick: confirmWipe }, t("Удалить скачанное", "Delete downloads")))));
  }
  return out;
};

async function applyUpdate() {
  try { await api("update/apply", {}); pollSoon(); toast(t("Обновление… лаунчер перезапустится", "Updating… the launcher will restart")); } catch (e) { toast(e.message); }
}

async function backupsModal() {
  let list = [];
  try { list = (await api("backups")).backups; } catch (e) { toast(e.message); return; }
  const body = h("div", { style: "max-height:320px;overflow:auto" });
  if (!list.length) body.append(h("p", null, t("Копий пока нет.", "No backups yet.")));
  for (const b of list) {
    body.append(h("div", { class: "note", style: "margin-top:10px" }, b.time + " · " + (b.size / 1048576).toFixed(1) + " MB"));
    for (const w of b.worlds) body.append(h("div", { class: "row", style: "padding:6px 0" }, h("div", { class: "l" }, h("b", null, w)),
      h("button", { class: "btn", onclick: async () => { try { await api("backups/restore", { key: b.key, file: b.file, world: w }); toast(t("Мир восстановлен", "World restored")); } catch (e) { toast(e.message); } } }, t("Восстановить", "Restore"))));
  }
  modal(t("Копии миров", "World backups"), body, [{ text: t("Закрыть", "Close") }]);
}

// --- редактор своей темы: 6 базовых цветов и цветовое колесо ---
function hsv2hex(H, Sa, V) {
  const f = n => { const k = (n + H / 60) % 6; return V - V * Sa * Math.max(0, Math.min(k, 4 - k, 1)); };
  return "#" + [f(5), f(3), f(1)].map(x => Math.round(x * 255).toString(16).padStart(2, "0")).join("");
}
function hex2hsv(hex) {
  const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255);
  const mx = Math.max(r, g, b), mn = Math.min(r, g, b), d = mx - mn;
  let H = 0;
  if (d) H = mx === r ? ((g - b) / d + 6) % 6 : mx === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return [H * 60, mx ? d / mx : 0, mx];
}
function colorWheel(hex, onChange) {
  const SZ = 170, R = SZ / 2, c = h("canvas", { width: SZ, height: SZ }), ctx = c.getContext("2d");
  let [H, Sa, V] = hex2hsv(hex);
  const vr = h("input", { type: "range", min: 0, max: 100, value: Math.round(V * 100), style: "width:170px;margin-top:8px" });
  function draw() {
    const im = ctx.createImageData(SZ, SZ);
    for (let y = 0; y < SZ; y++) for (let x = 0; x < SZ; x++) {
      const dx = x - R, dy = y - R, r = Math.hypot(dx, dy), i = (y * SZ + x) * 4;
      if (r > R) { im.data[i + 3] = 0; continue; }
      const col = hsv2hex((Math.atan2(dy, dx) * 180 / Math.PI + 360) % 360, r / R, V);
      im.data[i] = parseInt(col.slice(1, 3), 16); im.data[i + 1] = parseInt(col.slice(3, 5), 16); im.data[i + 2] = parseInt(col.slice(5, 7), 16); im.data[i + 3] = 255;
    }
    ctx.putImageData(im, 0, 0);
    const a = H * Math.PI / 180;
    ctx.beginPath(); ctx.arc(R + Math.cos(a) * Sa * R, R + Math.sin(a) * Sa * R, 6, 0, 7); ctx.lineWidth = 2; ctx.strokeStyle = "#fff"; ctx.stroke();
    ctx.beginPath(); ctx.arc(R + Math.cos(a) * Sa * R, R + Math.sin(a) * Sa * R, 7.5, 0, 7); ctx.lineWidth = 1; ctx.strokeStyle = "#000"; ctx.stroke();
  }
  const emit = () => onChange(hsv2hex(H, Sa, V));
  const pick = e => {
    const b = c.getBoundingClientRect(), dx = e.clientX - b.left - R, dy = e.clientY - b.top - R;
    H = (Math.atan2(dy, dx) * 180 / Math.PI + 360) % 360; Sa = Math.min(1, Math.hypot(dx, dy) / R); draw(); emit();
  };
  let down = false;
  c.addEventListener("mousedown", e => { down = true; pick(e); });
  window.addEventListener("mousemove", e => { if (down) pick(e); });
  window.addEventListener("mouseup", () => { down = false; });
  vr.oninput = () => { V = vr.value / 100; draw(); emit(); };
  draw();
  return { el: h("div", { style: "display:flex;flex-direction:column;align-items:center" }, c, vr),
    set(x) { [H, Sa, V] = hex2hsv(x); vr.value = Math.round(V * 100); draw(); } };
}

function themeEditor(idx) {
  const s = S.settings, ex = idx >= 0 ? s.cThemes[idx] : null;
  const cols = { ...(ex ? ex.colors : PAL[s.light ? "new-light" : "new-dark"]) };
  for (const k of Object.keys(cols)) if (!BASE.find(b => b[0] === k)) delete cols[k];
  let cur = "accent";
  const name = h("input", { type: "text", value: ex ? ex.name : "", placeholder: t("Название", "Name"), maxlength: 20, style: "width:100%" });
  const grid = h("div", { class: "swatches" });
  const wheel = colorWheel(cols[cur], v => { cols[cur] = v; paint(); previewTheme(); });
  const dots = {};
  function paint() {
    grid.innerHTML = "";
    for (const [k, ru, en] of BASE) {
      const dot = h("i", { style: "background:" + cols[k] });
      grid.append(h("div", { class: "swatch" + (k === cur ? " on" : ""), onclick: () => { cur = k; wheel.set(cols[k]); paint(); } }, dot, t(ru, en)));
    }
  }
  const prev = document.documentElement.style.cssText;
  function previewTheme() { const p = themeFrom(cols), r = document.documentElement.style; for (const [k, v] of Object.entries(p)) r.setProperty("--" + k, v); r.setProperty("--act", mix(p.accent, "#000000", .28)); }
  paint();
  const body = h("div", null, name, grid, h("div", { class: "wheelbox" }, wheel.el));
  const restore = () => { applyTheme(); };
  const close = modal(idx >= 0 ? t("Изменить тему", "Edit theme") : t("Новая тема", "New theme"), body, [
    { text: t("Отмена", "Cancel"), fn: restore },
    { text: t("Сохранить", "Save"), cls: "pri", fn: async () => {
      const list = [...(s.cThemes || [])], item = { name: name.value.trim() || t("Своя ", "Custom ") + (idx >= 0 ? idx + 1 : list.length + 1), colors: cols };
      if (idx >= 0) list[idx] = item; else list.push(item);
      await save({ cThemes: list, cSel: idx >= 0 ? idx : list.length - 1 }); applyTheme(); renderPage();
    } }]);
  previewTheme();
}

// --- загрузки: версии, каталог, сборки ---
let dlTab = "versions";
PAGES.downloads = () => {
  const T = [["versions", t("Версии", "Versions")], ["catalog", t("Каталог", "Catalog")], ["builds", t("Сборки", "Builds")]];
  const tb = tabs(T, dlTab, v => { dlTab = v; renderPage(); }); tb.classList.add("top");
  return [tb, TABS[dlTab]()];
};
const TABS = {};

let VERS = null, verFilter = "release", verQuery = "";
TABS.versions = () => {
  const s = S.settings, root = h("div"), listBox = h("div", { class: "list" });
  const q = h("input", { type: "search", placeholder: t("Поиск версии…", "Search version…"), value: verQuery, oninput: () => { verQuery = q.value; fill(); } });
  const types = [["release", t("Релизы", "Releases")], ["snapshot", t("Снапшоты", "Snapshots")], ["old", t("Старые", "Old")]];
  let instSet = new Set();
  const fill = () => {
    listBox.innerHTML = "";
    if (!VERS) return;
    let arr = VERS.versions.filter(v => verFilter === "old" ? v.type.startsWith("old_") : v.type === verFilter);
    if (verQuery) arr = arr.filter(v => v.id.toLowerCase().includes(verQuery.toLowerCase()));
    if (!arr.length) listBox.append(h("div", { class: "empty" }, t("Ничего не найдено", "Nothing found")));
    for (const v of arr.slice(0, 250)) {
      const cur = !S.settings.build && v.id === S.settings.selected;
      const loaders = [...instSet].filter(k => k.startsWith(v.id + "|")).map(k => loaderName(k.split("|")[1]));
      listBox.append(h("div", { class: "item" + (cur ? " sel" : ""), onclick: () => installModal(v.id) },
        h("span", { class: "nm" }, v.id), ...loaders.map(l => h("span", { class: "tag" }, l)), h("span", { class: "meta" }, v.date)));
    }
  };
  root.append(plain(row(verTitle(), hasSel() ? t("Выбранная установка", "Selected installation") : t("Выберите версию из списка ниже", "Pick a version from the list below"),
    h("button", { class: "btn pri", disabled: STATUS.busy || !hasSel(), onclick: async () => { try { await api("install", {}); pollSoon(); toast(t("Скачивание началось", "Download started")); } catch (e) { toast(e.message); } } }, t("Скачать без запуска", "Download only")))));
  root.append(h("div", { class: "tools" }, seg(types, verFilter, v => { verFilter = v; fill(); }), q), listBox);
  api("installed").then(r => { instSet = new Set(r.list.map(x => x.mc + "|" + x.loader)); fill(); }).catch(() => {});
  if (VERS) fill();
  else {
    listBox.append(h("div", { class: "empty" }, t("Загрузка списка…", "Loading list…")));
    api("versions").then(v => { VERS = v; if (page === "downloads" && dlTab === "versions") fill(); }).catch(e => { listBox.innerHTML = ""; listBox.append(h("div", { class: "empty" }, e.message)); });
  }
  return root;
};

// выбор загрузчика для версии: каждая связка — отдельная установка со своими модами и мирами
function installModal(mc, forBuild) {
  let loader = !forBuild && S.settings.selected === mc ? (S.settings.loader || "vanilla") : "vanilla", lv = "";
  const body = h("div"), box = h("div"), sel = h("select", { style: "width:100%;margin-top:10px", onchange: () => { lv = sel.value; } });
  const info = h("p", { class: "note" });
  async function drawLoader() {
    box.innerHTML = "";
    box.append(h("div", { class: "seg", style: "flex-wrap:wrap" }, LOADERS.map(([id, nm]) => h("button", { class: id === loader ? "on" : "", onclick: () => { loader = id; lv = ""; drawLoader(); } }, nm))));
    sel.innerHTML = ""; info.textContent = "";
    if (loader === "vanilla") { sel.classList.add("hidden"); if (S.settings.corelix && mc === "1.21.3") info.textContent = t("Для этой версии будет подключён Corelix.", "Corelix will be used for this version."); return; }
    sel.classList.remove("hidden");
    sel.append(h("option", { value: "" }, t("Загрузка списка…", "Loading list…")));
    try {
      const r = await api("loaders?loader=" + loader + "&mc=" + encodeURIComponent(mc));
      sel.innerHTML = ""; sel.append(h("option", { value: "" }, t("Авто (последняя)", "Auto (latest)")));
      for (const v of r.versions) sel.append(h("option", { value: v }, v));
      if (loader === "forge" || loader === "neoforge") info.textContent = t("Forge и NeoForge ставятся официальным установщиком — это может занять несколько минут.", "Forge and NeoForge use the official installer — it may take a few minutes.");
    } catch (e) { sel.innerHTML = ""; sel.classList.add("hidden"); info.textContent = e.message; }
  }
  body.append(box, sel, info); drawLoader();
  const done = forBuild || (() => {});
  modal(mc, body, [
    { text: t("Отмена", "Cancel") },
    { text: t("Выбрать", "Select"), fn: async () => {
      if (forBuild) return forBuild({ mc, loader, loaderVer: lv });
      await save({ selected: mc, loader, loaderVer: lv, build: "" }); renderChrome(); renderPage(); } },
    ...(forBuild ? [] : [{ text: t("Выбрать и скачать", "Select & download"), cls: "pri", fn: async () => {
      await save({ selected: mc, loader, loaderVer: lv, build: "" }); renderChrome();
      try { await api("install", {}); pollSoon(); toast(t("Скачивание началось", "Download started")); } catch (e) { toast(e.message); }
      renderPage(); } }]),
  ]);
}

// каталог
let catType = "mod", catQ = "", catFilter = true, catSrc = null, catBuild = "";
const bq = () => catBuild ? "&build=" + encodeURIComponent(catBuild) : "";
TABS.catalog = () => {
  const s = S.settings;
  if (!catSrc) catSrc = s.modSource === "curseforge" && S.cf ? "curseforge" : "modrinth";
  const root = h("div"), res = h("div"), more = h("button", { class: "btn hidden", style: "margin:12px auto;display:block" }, t("Показать ещё", "Show more"));
  const types = [["mod", t("Моды", "Mods")], ["modpack", t("Сборки", "Modpacks")], ["resourcepack", t("Ресурспаки", "Resource packs")], ["shader", t("Шейдеры", "Shaders")], ["datapack", t("Датапаки", "Datapacks")], ["installed", t("Установлено", "Installed")]];
  const srcs = [["modrinth", "Modrinth"], ["curseforge", "CurseForge"]];
  const q = h("input", { type: "search", placeholder: t("Поиск…", "Search…"), value: catQ, onkeydown: e => { if (e.key === "Enter") { catQ = q.value; run(true); } } });
  const e0 = (catBuild || s.build) ? (s.builds || []).find(b => b.id === (catBuild || s.build)) : null;
  const target = e0 ? e0.name : (s.selected ? instTitle(s.selected, s.loader) : t("выбранной версии", "the selected version"));
  const filt = h("div", { class: "row", style: "border:0;padding:4px 0" }, h("div", { class: "l" }, t("Только для ", "Only for ") + target), h("div", { class: "c" }, sw(catFilter, v => { catFilter = v; run(true); })));
  let off = 0;
  const instOnly = catType === "installed";
  async function run(reset) {
    if (reset) { off = 0; res.innerHTML = ""; }
    more.classList.add("hidden");
    if (catType === "installed") return runInstalled();
    if (catSrc === "curseforge" && !S.cf) { res.append(h("div", { class: "empty" }, t("CurseForge недоступен в этой сборке лаунчера.", "CurseForge is not available in this launcher build."))); return; }
    res.append(h("div", { class: "empty", id: "cload" }, "…"));
    try {
      const r = await api("catalog/search?src=" + catSrc + "&type=" + catType + "&q=" + encodeURIComponent(catQ) + "&filter=" + (catFilter ? 1 : 0) + "&offset=" + off + bq());
      $("#cload") && $("#cload").remove();
      if (!r.hits.length && !off) res.append(h("div", { class: "empty" }, t("Ничего не найдено", "Nothing found")));
      for (const x of r.hits) {
        const lbl = catType === "modpack" ? t("Скачать сборку", "Get pack") : t("Установить", "Install");
        const btn = h("button", { class: "btn " + (x.installed ? "ok" : "pri"), disabled: x.installed || x.installing }, x.installed ? "✓ " + t("Установлено", "Installed") : x.installing ? "…" : lbl);
        const key = catSrc + ":" + catType + ":" + x.id;
        if (x.installing) watchCat(key, btn, lbl);
        btn.onclick = async () => {
          let world = "";
          if (catType === "datapack") { world = await chooseWorld(); if (!world) return; }
          btn.disabled = true; btn.textContent = "…";
          try {
            const j = await api("catalog/install", { id: x.id, type: catType, src: catSrc, world, name: x.title, icon: x.icon, build: catBuild });
            if (j.job) { pollSoon(); toast(t("Сборка скачивается — прогресс внизу, можно переходить по вкладкам", "Downloading the pack — progress is at the bottom, you can browse freely")); btn.textContent = "✓ " + t("Запущено", "Started"); }
            else watchCat(key + (world ? ":" + world : ""), btn, lbl);
          } catch (e) { btn.disabled = false; btn.textContent = lbl; toast(e.message); }
        };
        res.append(h("div", { class: "cat" }, x.icon ? h("img", { src: x.icon, loading: "lazy" }) : h("div", { class: "noimg" }),
          h("div", { class: "ti" }, h("b", null, x.title), h("span", null, x.description), h("span", null, x.author)),
          h("div", { class: "dl" }, fmtN(x.downloads) + " " + t("скач.", "dl")), btn));
      }
      off += r.hits.length;
      more.classList.toggle("hidden", off >= r.total || !r.hits.length);
    } catch (e) { $("#cload") && $("#cload").remove(); res.append(h("div", { class: "empty" }, e.message)); }
  }
  async function runInstalled() {
    res.innerHTML = "";
    for (const [ty, label, acc] of [["mod", t("Моды", "Mods"), ".jar,.zip"], ["resourcepack", t("Ресурспаки", "Resource packs"), ".zip"], ["shader", t("Шейдеры", "Shaders"), ".zip"], ["datapack", t("Датапаки", "Datapacks"), ".zip"]]) {
      const r = await api("catalog/installed?type=" + ty + bq());
      res.append(h("div", { style: "display:flex;align-items:center;justify-content:space-between" }, h("h3", { class: "sec" }, label),
        h("button", { class: "btn", onclick: async () => {
          let world = ""; if (ty === "datapack") { world = await chooseWorld(); if (!world) return; }
          pickFile(acc, async f => { try { await upload(ty, f, (world ? "&world=" + encodeURIComponent(world) : "") + bq()); toast(t("Файл добавлен", "File added")); runInstalled(); } catch (e) { toast(e.message); } });
        } }, "+ " + t("Добавить файл", "Add file"))));
      if (!r.files.length) res.append(h("div", { class: "note" }, t("Пусто", "Empty")));
      for (const f of r.files) {
        res.append(h("div", { class: "cat" }, h("div", { class: "ti" }, h("b", null, f.name), h("span", null, (f.world ? f.world + " · " : "") + (f.size / 1048576).toFixed(2) + " MB")),
          h("button", { class: "btn dng", onclick: async () => { await api("catalog/remove", { type: ty, name: f.name, world: f.world, build: catBuild }); runInstalled(); } }, t("Удалить", "Remove"))));
      }
    }
    res.append(h("h3", { class: "sec" }, t("Миры", "Worlds")), h("button", { class: "btn", onclick: () => pickFile(".zip", async f => { try { await upload("world", f, bq()); toast(t("Мир добавлен", "World added")); } catch (e) { toast(e.message); } }) }, "+ " + t("Импорт мира (zip)", "Import a world (zip)")));
  }
  more.onclick = () => run(false);
  const setVis = () => { const inst = catType === "installed"; filt.classList.toggle("hidden", inst || catType === "modpack"); q.classList.toggle("hidden", inst); srcBox.classList.toggle("hidden", inst); };
  const srcBox = h("div");
  srcBox.append(seg(srcs, catSrc, v => { catSrc = v; run(true); }));
  const tb = tabs(types, catType, v => { catType = v; renderPage(); });
  root.append(tb, h("div", { class: "tools" }, srcBox, q), filt, res, more);
  setVis(); run(true);
  return root;
};
// установки из каталога идут в фоне на стороне лаунчера; кнопки обновляются по опросу, даже если вкладку закрывали
const catWatch = new Map();
let catTimer = 0;
function watchCat(key, btn, lbl) {
  catWatch.set(key, { btn, lbl });
  if (!catTimer) catTimer = setInterval(async () => {
    try {
      const r = await api("catalog/active");
      for (const [k, v] of Object.entries(r)) {
        const w = catWatch.get(k); if (!w) continue;
        catWatch.delete(k);
        if (v === "done") { w.btn.className = "btn ok"; w.btn.disabled = true; w.btn.textContent = "✓ " + t("Установлено", "Installed"); toast(t("Установлено", "Installed")); }
        else { w.btn.disabled = false; w.btn.textContent = w.lbl; toast(v.replace(/^error:/, "")); }
      }
    } catch (e) {}
    if (!catWatch.size) { clearInterval(catTimer); catTimer = 0; }
  }, 1000);
}
const fmtN = n => n >= 1e6 ? (n / 1e6).toFixed(1) + "M" : n >= 1e3 ? (n / 1e3).toFixed(1) + "K" : String(n);

function chooseWorld() {
  return new Promise(async ok => {
    let ws = [];
    try { ws = (await api("worlds" + (catBuild ? "?build=" + encodeURIComponent(catBuild) : ""))).worlds; } catch (e) {}
    if (!ws.length) { toast(t("В этой установке ещё нет миров", "This installation has no worlds yet")); return ok(""); }
    const sel = h("select", { style: "width:100%" }, ws.map(w => h("option", { value: w }, w)));
    modal(t("Выберите мир", "Pick a world"), h("div", null, h("p", null, t("Датапак ставится в выбранный мир.", "The datapack goes into the chosen world.")), sel),
      [{ text: t("Отмена", "Cancel"), fn: () => ok("") }, { text: t("Выбрать", "Select"), cls: "pri", fn: () => ok(sel.value) }]);
  });
}

// сборки
TABS.builds = () => {
  const s = S.settings, root = h("div");
  root.append(h("div", { class: "tools" },
    h("button", { class: "btn pri", onclick: createBuild }, "+ " + t("Добавить сборку", "Add build")),
    h("button", { class: "btn", onclick: () => pickFile(".zip,.mrpack", async f => { try { await upload("pack", f); pollSoon(); toast(t("Сборка импортируется…", "Importing the pack…")); } catch (e) { toast(e.message); } }) }, t("Импорт (.mrpack, CurseForge, Mobilix)", "Import (.mrpack, CurseForge, Mobilix)"))));
  if (!(s.builds || []).length) root.append(h("p", { class: "note" }, t("Сборок пока нет. У каждой сборки своя папка: моды, миры и настройки не смешиваются с обычной игрой. Карандаш открывает каталог для этой сборки; чтобы играть, выберите её внизу в списке версий на главном экране.", "No builds yet. Each build has its own folder: mods, worlds and settings stay apart from the regular game. The pencil opens the catalog for that build; to play it, pick it in the version list on the main screen.")));
  for (const b of s.builds || []) {
    const on = s.build === b.id;
    const ic = h("img", { class: "bic", src: "/api/build/icon?id=" + b.id + "&k=" + encodeURIComponent(KEY) + "&r=" + (b.iconV || 0), title: t("Сменить иконку", "Change icon"),
      onerror: () => { const ph = h("div", { class: "bic", title: t("Сменить иконку", "Change icon"), onclick: setIcon }, "▣"); ic.replaceWith(ph); }, onclick: setIcon });
    function setIcon() {
      pickFile("image/png,image/jpeg", async f => { try { const r = await fetch("/api/build/icon?id=" + b.id, { method: "POST", headers: { "X-K": KEY }, body: f }); if (!r.ok) throw new Error(r.statusText); b.iconV = Date.now(); renderPage(); } catch (e) { toast(e.message); } });
    }
    root.append(h("div", { class: "brow" + (on ? " on" : "") }, ic,
      h("div", { class: "bt", onclick: async () => { await save({ build: on ? "" : b.id }); renderChrome(); renderPage(); }, style: "cursor:pointer" }, h("b", null, (on ? "● " : "") + b.name), h("span", null, instTitle(b.mc, b.loader))),
      h("button", { class: "ib", title: t("Поделиться", "Share"), onclick: () => exportBuild(b) }, pixIcon("SHARE", MENU_PAL, 2)),
      h("button", { class: "ib", title: t("Каталог сборки", "Build catalog"), onclick: () => { catBuild = b.id; catType = "mod"; openPage("buildcat"); } }, pixIcon("PENCIL", MENU_PAL, 2)),
      h("button", { class: "ib", title: t("Копия", "Copy"), onclick: async () => { try { await api("build/dup", { id: b.id }); await refresh(); renderPage(); } catch (e) { toast(e.message); } } }, "⧉"),
      h("button", { class: "ib", title: t("Удалить", "Delete"), onclick: () => modal(t("Удалить сборку?", "Delete the build?"), h("p", null, "«" + b.name + "» — " + t("моды, миры и настройки сборки будут удалены.", "its mods, worlds and settings will be deleted.")), [{ text: t("Отмена", "Cancel") }, { text: t("Удалить", "Delete"), cls: "dng", fn: async () => { try { await api("build/delete", { id: b.id }); await refresh(); renderChrome(); renderPage(); } catch (e) { toast(e.message); } } }]) }, pixIcon("TRASH", TRASH_PAL, 2))));
  }
  return root;
};

PAGES.buildcat = () => [TABS.catalog()];

async function exportBuild(b) {
  try {
    const r = await fetch("/api/build/export?id=" + b.id, { headers: { "X-K": KEY } });
    if (!r.ok) throw new Error(r.statusText);
    const a = h("a", { href: URL.createObjectURL(await r.blob()), download: b.name.replace(/[^\w.\- ]/g, "_") + ".mobilix.zip" });
    document.body.append(a); a.click(); a.remove();
  } catch (e) { toast(e.message); }
}

async function createBuild() {
  if (!VERS) { try { VERS = await api("versions"); } catch (e) { toast(e.message); return; } }
  const rel = VERS.versions.filter(v => v.type === "release");
  const name = h("input", { type: "text", placeholder: t("Название сборки", "Build name"), maxlength: 32, style: "width:100%;margin-bottom:8px" });
  const sel = h("select", { style: "width:100%" }, rel.map(v => h("option", { value: v.id }, v.id)));
  modal(t("Новая сборка", "New build"), h("div", null, name, sel, h("p", { class: "note" }, t("Загрузчик выберете на следующем шаге.", "You pick the loader on the next step."))),
    [{ text: t("Отмена", "Cancel") }, { text: t("Далее", "Next"), cls: "pri", fn: () => installModal(sel.value, async c => {
      try { const j = await api("build/create", { name: name.value.trim(), mc: c.mc, loader: c.loader, loaderVer: c.loaderVer }); await save({ build: j.build.id }); renderChrome(); await api("install", {}); pollSoon(); renderPage(); toast(t("Сборка создана, скачиваю…", "Build created, downloading…")); }
      catch (e) { toast(e.message); } }) }]);
}

// --- сеть: ZeroTier One ---
let ZT = null, ztTimer = 0;
// Статус опрашивается тихо: страница и меню перерисовываются только если статус реально изменился (или по явному действию).
async function ztRefresh(force) {
  let n; try { n = await api("zt/status"); } catch (e) { n = { installed: false }; }
  const changed = JSON.stringify(n) !== JSON.stringify(ZT);
  ZT = n;
  if (changed || force === true) { if (page === "net") renderPage(); renderFxBtns(); }
}
function ztSchedule() { clearTimeout(ztTimer); ztTimer = setTimeout(async () => { if (page === "net") { await ztRefresh(); ztSchedule(); } else ztTimer = 0; }, 5000); }
PAGES.net = () => {
  if (!ZT) { ztRefresh(true).then(ztSchedule); return [h("div", { class: "empty" }, "…")]; }
  if (!ztTimer) ztSchedule();
  const s = S.settings, out = [];
  if (!ZT.installed) {
    out.push(plain(row("ZeroTier One", t("Игра по сети работает поверх приложения ZeroTier One (виртуальная локальная сеть, как Radmin). Установите его, запустите и вернитесь сюда.", "Online play runs on top of ZeroTier One (a virtual LAN, like Radmin). Install it, start it and come back here."),
      h("button", { class: "btn pri", onclick: () => api("openurl", { url: "https://www.zerotier.com/download/" }) }, t("Скачать", "Download")),
      h("button", { class: "btn", onclick: () => ztRefresh(true) }, t("Проверить", "Check")))));
    return out;
  }
  out.push(plain(
    row(ZT.running ? (ZT.online ? "● " + t("Узел ZeroTier в сети", "ZeroTier node online") : "… " + t("Узел подключается", "Node is connecting")) : "○ " + t("ZeroTier One не запущен", "ZeroTier One is not running"),
      ZT.running ? t("ID вашего узла: ", "Your node ID: ") + ZT.address : t("Запустите ZeroTier One (служба или значок в трее).", "Start ZeroTier One (the service or the tray icon)."),
      h("button", { class: "btn", onclick: () => ztRefresh(true) }, t("Обновить", "Refresh"))),
    row(t("Кнопка на главном экране", "Button on the main screen"), t("Быстрое включение сохранённых сетей рядом со скином.", "Quickly switch saved networks on/off next to the skin."), sw(s.ztButton, async v => { await save({ ztButton: v }); renderFxBtns(); }))));
  const id = h("input", { type: "text", placeholder: t("ID сети (16 символов)", "Network ID (16 characters)"), maxlength: 16, spellcheck: "false", style: "width:210px" });
  const nm = h("input", { type: "text", placeholder: t("Название", "Name"), maxlength: 24, style: "width:150px" });
  out.push(plain(row(t("Добавить сеть", "Add a network"), t("ID создаёт хозяин сети на my.zerotier.com. После входа хозяин должен разрешить ваш узел.", "The network owner creates the ID on my.zerotier.com. After joining, the owner must authorise your node."),
    id, nm, h("button", { class: "btn pri", onclick: async () => { try { await api("zt/join", { id: id.value.trim(), name: nm.value.trim() }); await refresh(); ztRefresh(true); toast(t("Готово", "Done")); } catch (e) { toast(e.message); } } }, t("Войти", "Join")))));
  const joined = new Map((ZT.nets || []).map(n => [n.id, n]));
  const list = h("div", { style: "margin-top:10px" });
  for (const n of s.ztNets || []) {
    const j = joined.get(n.id), ip = j && j.ips && j.ips[0];
    list.append(h("div", { class: "netnode" }, h("div", { class: "st" + (j && j.status === "OK" ? " ok" : "") }),
      h("div", { class: "ti" }, h("b", null, n.name || n.id), h("span", null, n.id + " · " + (j ? (j.status === "OK" ? (ip || "") : j.status === "ACCESS_DENIED" ? t("хозяин ещё не разрешил вход", "owner has not authorised you yet") : j.status) : t("не подключён", "not joined")))),
      ip ? h("button", { class: "btn", onclick: () => navigator.clipboard.writeText(ip).then(() => toast(t("IP скопирован", "IP copied"))) }, t("Копировать IP", "Copy IP")) : null,
      j ? h("button", { class: "btn", onclick: async () => { await api("zt/leave", { id: n.id }); ztRefresh(true); } }, t("Выйти", "Leave")) : h("button", { class: "btn pri", onclick: async () => { try { await api("zt/join", { id: n.id, name: n.name }); ztRefresh(true); } catch (e) { toast(e.message); } } }, t("Войти", "Join")),
      h("button", { class: "ib", title: t("Забыть", "Forget"), onclick: async () => { await api("zt/leave", { id: n.id, forget: true }); await refresh(); ztRefresh(true); } }, "✕")));
  }
  out.push(list);
  out.push(plain(row(t("Раздать мир друзьям", "Share my world"), t("В игре откройте мир для сети (Esc → Открыть для сети), затем скопируйте адрес и отправьте друзьям. Порт определяется по журналу игры.", "In the game choose Open to LAN, then copy the address and send it to friends. The port is read from the game log."),
    h("button", { class: "btn pri", onclick: async () => { try { const r = await api("zt/share"); await navigator.clipboard.writeText(r.text); toast(r.text + " — " + t("скопировано", "copied")); } catch (e) { toast(e.message); } } }, t("Копировать адрес", "Copy address")))));
  return out;
};

// --- праздничные эффекты (как в мобайл-версии): тыквы и летучие мыши, снег, криперы, курицы ---
function season() {
  const q = new URLSearchParams(location.search).get("fx") || (() => { try { return sessionStorage.getItem("fxForce"); } catch (e) { return null; } })();
  if (q) return q;
  const d = new Date(), m = d.getMonth(), day = d.getDate();
  if ((m === 9 && day >= 24) || (m === 10 && day <= 7)) return "halloween";
  if ((m === 11 && day >= 25) || (m === 0 && day <= 8)) return "newyear";
  if (m === 4 && day >= 10 && day <= 24) return "mcbday";
  if ((m === 2 && day >= 25) || (m === 3 && day <= 8)) return "aprilfools";
  return null;
}
const SPR = {};
function sprite(kind, name) {
  const k = kind + name;
  if (!SPR[k]) { const pal = FX_PAL[kind][name]; pal.__fx = 1; SPR[k] = pixIcon(name, pal, 1); }
  return SPR[k];
}
const FX = { ps: [], kind: null, raf: 0, last: 0 };
function fxSpawn(kind, w, h, top) {
  const r = Math.random, x = r() * w, y = top ? -24 : r() * h, ph = r() * 6;
  const mk = (name, size, vx, vy, bat) => ({ x, y, vx, vy, ph, size, bat: !!bat, c: sprite(kind, name) });
  if (kind === "newyear") return mk("SNOW", r() < .25 ? 14 : 8 + r() * 3, (r() - .5) * 14, 22 + r() * 28);
  if (kind === "mcbday") return mk("CREEPER", 13 + r() * 7, (r() - .5) * 8, 70 + r() * 60);
  if (kind === "aprilfools") return mk("CHICKEN", 18 + r() * 6, (r() - .5) * 20, 14 + r() * 14);
  if (r() < .33) { const p = mk("BAT", 20, (r() < .5 ? 1 : -1) * (30 + r() * 30), 0, true); p.y = r() * h * .7; return p; }
  return mk("PUMPKIN", 16 + r() * 8, (r() - .5) * 10, 16 + r() * 20);
}
function fxLoop(now) {
  const cv = $("#fx"), kind = FX.kind;
  if (!kind || !S.settings.fxOn) { cv.getContext("2d").clearRect(0, 0, cv.width, cv.height); FX.raf = 0; return; }
  const w = innerWidth, hh = innerHeight;
  if (cv.width !== w || cv.height !== hh || !FX.ps.length) {
    cv.width = w; cv.height = hh;
    const per = kind === "newyear" ? 9000 : kind === "mcbday" ? 14000 : 20000;
    FX.ps = Array.from({ length: Math.max(6, Math.min(36, Math.round(w * hh / per / 3))) }, () => fxSpawn(kind, w, hh, false));
  }
  const dt = FX.last && now - FX.last < 200 ? (now - FX.last) / 1000 : .016; FX.last = now;
  const x = cv.getContext("2d"); x.clearRect(0, 0, w, hh); x.imageSmoothingEnabled = false;
  x.globalAlpha = kind === "newyear" ? .82 : .92;
  FX.ps.forEach((p, i) => {
    p.ph += dt * (p.bat ? 5 : 1.5);
    p.x += p.vx * dt + (p.bat ? 0 : Math.sin(p.ph) * 8 * dt);
    p.y += p.vy * dt + (p.bat ? Math.sin(p.ph) * 30 * dt : 0);
    if (p.bat) { if (p.x < -30) p.x = w + 20; else if (p.x > w + 30) p.x = -20; }
    else if (p.y > hh + 24) FX.ps[i] = fxSpawn(kind, w, hh, true);
    x.drawImage(p.c, p.x, p.y, p.size, p.size * p.c.height / p.c.width);
  });
  FX.raf = requestAnimationFrame(fxLoop);
}
function fxStart() {
  FX.kind = season();
  if (FX.kind && S.settings.fxOn && !FX.raf) { FX.ps = []; FX.raf = requestAnimationFrame(fxLoop); }
}
// кнопки у скина: эффекты (только в праздничную неделю) и ZeroTier (если включена в настройках сети)
function renderFxBtns() {
  if (!S) return;
  const s = S.settings, fb = $("#fxBtn"), zb = $("#ztBtn"), sn = season();
  fb.classList.toggle("hidden", !sn);
  if (sn) {
    fb.classList.toggle("off", !s.fxOn); fb.innerHTML = "";
    const nm = { halloween: "PUMPKIN", newyear: "SNOW", mcbday: "CREEPER", aprilfools: "CHICKEN" }[sn];
    const pal = FX_PAL[sn][nm]; pal.__fx = 1; fb.append(pixIcon(nm, pal, 3));
    fb.onclick = async () => { await save({ fxOn: !s.fxOn }); renderFxBtns(); fxStart(); };
  }
  zb.classList.toggle("hidden", !s.ztButton);
  if (s.ztButton) {
    zb.classList.toggle("off", !s.ztOn); zb.classList.toggle("ztok", !!s.ztOn); zb.innerHTML = ""; zb.append(pixIcon("NET", MENU_PAL, 2));
    zb.title = s.ztOn ? t("ZeroTier включён — нажмите, чтобы выключить", "ZeroTier is on — click to turn off") : t("Включить ZeroTier", "Turn ZeroTier on");
    zb.onclick = async () => {
      if (!s.ztNets || !s.ztNets.length) { toast(t("Сначала добавьте сеть на странице «Сеть»", "Add a network on the Network page first")); openPage("net"); return; }
      try { await api("zt/toggle", { on: !s.ztOn }); await refresh(); renderFxBtns(); toast(S.settings.ztOn ? t("ZeroTier включён", "ZeroTier on") : t("ZeroTier выключен", "ZeroTier off")); }
      catch (e) { toast(e.message); }
    };
  }
}

// --- соцсети ---
PAGES.social = () => {
  const link = (name, sub, url) => row(name, sub, h("button", { class: "btn pri", onclick: () => api("openurl", { url }).catch(e => toast(e.message)) }, t("Открыть", "Open")));
  return [
    group(t("Mobilix в сети", "Mobilix online"), true,
      link(t("Сайт", "Website"), "kumolix.github.io/Mobilix-Launcher-for-Minecraft", "https://kumolix.github.io/Mobilix-Launcher-for-Minecraft/"),
      link("GitHub · PC", "Kumolix/Mobilix-Launcher-PC", "https://github.com/Kumolix/Mobilix-Launcher-PC"),
      link("GitHub · Android", "Kumolix/Mobilix-Launcher-for-Minecraft", "https://github.com/Kumolix/Mobilix-Launcher-for-Minecraft"),
      link("Telegram", "@MOBILIX_LAUNCHER", "https://t.me/mobilix_launcher"),
      link("TikTok", "@mobilix_launcher", "https://www.tiktok.com/@mobilix_launcher")),
    h("p", { class: "note" }, t("Разработчик: Kumolix. Основано на Mobilix Launcher for Minecraft.", "Developer: Kumolix. Based on Mobilix Launcher for Minecraft.")),
  ];
};

// ---------- запуск ----------
async function refresh() { const st = await api("state"); S = st; return st; }

async function init() {
  try { await refresh(); } catch (e) { document.body.textContent = "Mobilix: " + e.message; return; }
  applyTheme();
  Skin3D.mount($("#skinView"));
  buildMenus(); renderChrome(); initHover(); loadSkin();
  requestAnimationFrame(() => Skin3D.fit());
  fxStart();
  new ResizeObserver(() => Skin3D.fit()).observe($("#skinCard"));
  STATUS = S.status || STATUS; renderStatus();
  $("#btnExit").onclick = () => { api("exit", {}).finally(() => { try { window.close(); } catch (e) {} }); };
  $("#btnLog").onclick = () => showLog(!logOpen);
  $("#tabLatest").onclick = () => setLogTab("live");
  $("#tabPrev").onclick = () => setLogTab("prev");
  $("#tabClose").onclick = () => showLog(false);
  $("#tabStop").onclick = () => api("stop", {});
  $("#tabCopy").onclick = async () => {
    try { await navigator.clipboard.writeText($("#logText").textContent); toast(t("Лог скопирован", "Log copied")); } catch (e) { toast(e.message); }
  };
  $("#playBtn").onclick = onPlay;
  $("#verBtn").onclick = () => toggleVerPopup();
  $("#verArrow").onclick = () => toggleVerPopup();
  document.addEventListener("mousedown", e => { if (!e.target.closest(".vrow")) toggleVerPopup(false); });
  $("#verLabel").onclick = () => { if (S.update) modal(t("Обновить лаунчер?", "Update the launcher?"), h("p", null, t("Новая версия ", "New version ") + S.update + t(". Лаунчер скачает её, заменит файл и перезапустится.", ". The launcher will download it, replace itself and restart.")), [{ text: t("Позже", "Later") }, { text: t("Обновить", "Update"), cls: "pri", fn: applyUpdate }]); };
  document.addEventListener("keydown", e => { if (e.key === "Escape") { if (!$("#modalLayer").classList.contains("hidden")) { $("#modalLayer").classList.add("hidden"); $("#modalLayer").innerHTML = ""; } else if (page) closePage(); } });
  document.addEventListener("contextmenu", e => { if (!e.target.closest("#logText,input")) e.preventDefault(); });
  let fails = 0;
  setInterval(() => api("ping").then(() => { fails = 0; }).catch(() => { if (++fails >= 4) { try { window.close(); } catch (e) {} } }), 4000);
  api("ping").catch(() => {});
  poll();
  if (!hasSel()) { dlTab = "versions"; openPage("downloads"); }
}
init();
