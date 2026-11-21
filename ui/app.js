"use strict";
// ---------- базовое ----------
const KEY = new URLSearchParams(location.search).get("k") || sessionStorage.getItem("k") || "";
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

function mix(a, b, k) {
  const p = x => [1, 3, 5].map(i => parseInt(x.slice(i, i + 2), 16));
  const A = p(a), B = p(b);
  return "#" + A.map((v, i) => Math.round(v + (B[i] - v) * k).toString(16).padStart(2, "0")).join("");
}

function applyTheme() {
  const s = S.settings, p = PAL[(s.style === "old" ? "old" : "new") + "-" + (s.light ? "light" : "dark")];
  const r = document.documentElement.style;
  for (const [k, v] of Object.entries(p)) r.setProperty("--" + k, v);
  r.setProperty("--act", "#2EA84F");
  r.setProperty("--hi", mix("#2EA84F", p.accent, 0.55));
  r.setProperty("--shadow", s.light ? "rgba(0,0,0,.22)" : "rgba(0,0,0,.45)");
  $("#aurora").classList.toggle("off", !s.aurora);
}

// ---------- сохранение настроек ----------
async function save(patch) {
  const r = await api("settings", patch);
  S.settings = r.settings;
  return r.settings;
}

// ---------- меню ----------
const MENU = [
  { id: "account", ru: "Аккаунт", en: "Account", ico: "USER" },
  { id: "settings", ru: "Настройки", en: "Settings", ico: "GEAR" },
  { id: "versions", ru: "Загрузчик версий", en: "Version loader", ico: "VERS" },
  { id: "catalog", ru: "Каталог", en: "Catalog", ico: "CAT" },
  { id: "wipe", ru: "Удалить скачанное", en: "Delete downloads", ico: "TRASH", pal: TRASH_PAL },
  { id: "social", ru: "Соцсети", en: "Social", ico: "CHAT" },
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
  $("#hint").classList.toggle("off", o);
}

function buildMenus() {
  const side = S.settings.menuType !== "bottom";
  const dr = $("#drawer"); dr.innerHTML = "";
  dr.append(h("div", { class: "dt" }, h("div", { class: "logo" }, "Mobilix")));
  for (const m of MENU) {
    const it = h("div", { class: "mi", onclick: () => menuAction(m.id) },
      h("div", { class: "ic" }, pixIcon(m.ico, m.pal || MENU_PAL, 2)), h("span", null, t(m.ru, m.en)));
    if (m.badge) it.append(h("span", { class: "badge" }, S.settings.lang.toUpperCase()));
    dr.append(it);
  }
  $("#hot").classList.toggle("hidden", !side);
  $("#hint").classList.toggle("hidden", !side);
  if (!side) setDrawer(false);
  const slot = $("#dockSlot"); slot.innerHTML = "";
  if (!side) {
    const dock = h("div", { class: "dock" });
    for (const m of MENU) {
      dock.append(h("div", { class: "cell" + (page === m.id ? " cur" : ""), "data-l": t(m.ru, m.en), onclick: () => menuAction(m.id) },
        pixIcon(m.ico, m.pal || MENU_PAL, 2)));
    }
    slot.append(dock);
  }
  $("#pageLayer").classList.toggle("dockpad", !side);
}

function initHover() {
  const dr = $("#drawer");
  $("#hot").addEventListener("mouseenter", () => { if (S.settings.menuType !== "bottom") { clearTimeout(closeT); setDrawer(true); } });
  $("#hot").addEventListener("mouseleave", () => { closeT = setTimeout(() => setDrawer(false), 220); });
  dr.addEventListener("mouseenter", () => clearTimeout(closeT));
  dr.addEventListener("mouseleave", () => { closeT = setTimeout(() => setDrawer(false), 160); });
  document.documentElement.addEventListener("mouseleave", () => setDrawer(false));
}

// ---------- шапка, версия, кнопка «Играть» ----------
function verTitle() {
  const s = S.settings;
  if (!s.selected) return t("Выберите версию", "Pick a version");
  return s.selected + (s.loader === "fabric" ? " • Fabric" : "");
}

function renderChrome() {
  const s = S.settings;
  $("#btnLog").textContent = t("Лог", "Log");
  $("#btnExit").title = t("Выход", "Exit");
  $("#nickLabel").textContent = s.nick;
  const vl = $("#verLabel");
  vl.textContent = "PC " + S.version + (S.update ? " · " + t("доступно обновление ", "update available ") + S.update : "");
  vl.classList.toggle("upd", !!S.update);
  $("#verBtn").textContent = verTitle();
  $("#tabLatest").textContent = t("Последний", "Latest");
  $("#tabPrev").textContent = t("Предыдущий", "Previous");
  $("#tabCopy").textContent = t("Копировать", "Copy");
  $("#tabStop").textContent = t("Остановить игру", "Stop game");
  renderStatus();
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
  $("#stPct").textContent = st.busy && st.percent >= 0 ? st.percent + "%" : "";
  $("#tabStop").classList.toggle("hidden", !st.running);
}

async function onPlay() {
  if (STATUS.busy) { api("cancel", {}); return; }
  if (!S.settings.selected) { openPage("versions"); toast(t("Сначала выберите версию игры", "Pick a game version first")); return; }
  try { await api("play", {}); STATUS = { ...STATUS, busy: true, error: "", percent: -1, stage: "…" }; renderStatus(); pollSoon(); }
  catch (e) { toast(e.message); }
}

let pollT = 0, wasBusy = false;
function pollSoon() { clearTimeout(pollT); pollT = setTimeout(poll, 200); }
async function poll() {
  try {
    STATUS = await api("status");
    if (wasBusy && !STATUS.busy) {
      const st = await api("state"); S.installed = st.installed;
      if (!STATUS.error && S.settings.selected) toast(t("Готово", "Done"));
      if (page === "versions") renderPage();
    }
    wasBusy = STATUS.busy;
    renderStatus();
  } catch (e) {}
  pollT = setTimeout(poll, STATUS.busy ? 400 : 1500);
}

// выбор версии снизу (список вверх)
function toggleVerPopup(force) {
  const p = $("#verPopup");
  const open = force !== undefined ? force : p.classList.contains("hidden");
  p.classList.toggle("hidden", !open);
  if (!open) return;
  p.innerHTML = "";
  const inst = (S.installed || []).filter(x => !x.startsWith("fabric-loader-"));
  if (!inst.length) p.append(h("div", { class: "it", style: "color:var(--dim);cursor:default" }, t("Скачанных версий пока нет", "No downloaded versions yet")));
  for (const id of inst) {
    p.append(h("div", { class: "it" + (id === S.settings.selected ? " cur" : ""), onclick: async () => { await save({ selected: id }); renderChrome(); toggleVerPopup(false); } }, id));
  }
  p.append(h("div", { class: "sepl" }));
  p.append(h("div", { class: "it", onclick: () => { toggleVerPopup(false); openPage("versions"); } }, t("Все версии и загрузчик…", "All versions & loader…")));
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
  const tok = ++skinTok, s = S.settings;
  try {
    const r = await fetch("/api/skin", { headers: { "X-K": KEY } });
    if (!r.ok) throw new Error("no skin");
    const hint = r.headers.get("X-Skin-Model") || "";
    const url = URL.createObjectURL(await r.blob());
    const img = new Image();
    await new Promise((ok, bad) => { img.onload = ok; img.onerror = bad; img.src = url; });
    if (tok === skinTok) Skin3D.setSkin(img, s.skinModel, hint);
  } catch (e) { if (tok === skinTok) Skin3D.setSkin(null, 0, ""); }
}

// ---------- страницы ----------
const TITLES = { account: ["Аккаунт", "Account"], settings: ["Настройки", "Settings"], versions: ["Загрузчик версий", "Version loader"], catalog: ["Каталог", "Catalog"], social: ["Соцсети", "Social"] };

function openPage(id) { page = id; buildMenus(); renderPage(); }
function closePage() { page = null; $("#pageLayer").classList.add("hidden"); $("#pageLayer").innerHTML = ""; buildMenus(); }

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

// элементы форм
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
function group(title, open, ...rows) {
  const g = h("div", { class: "grp" + (open ? "" : " closed") });
  g.append(h("div", { class: "gh", onclick: () => g.classList.toggle("closed") }, h("span", null, title), h("span", { class: "car" }, "▾")));
  g.append(h("div", { class: "gb" }, ...rows));
  return g;
}

const PAGES = {};

// --- аккаунт ---
PAGES.account = () => {
  const s = S.settings;
  const nick = h("input", { type: "text", value: s.nick, maxlength: 16, spellcheck: "false", style: "width:220px" });
  const file = h("input", { type: "file", accept: "image/png", class: "hidden", onchange: async e => {
    const f = e.target.files[0]; if (!f) return;
    const r = await fetch("/api/skin/import", { method: "POST", headers: { "X-K": KEY }, body: await f.arrayBuffer() });
    if (!r.ok) { try { toast((await r.json()).error); } catch (x) {} return; }
    await refresh(); renderPage(); loadSkin(); toast(t("Скин загружен", "Skin imported"));
  } });
  return [
    group(t("Игрок", "Player"), true,
      row(t("Ник", "Nickname"), t("3–16 символов: латиница, цифры и _. Игра запускается в офлайн-режиме.", "3–16 characters: Latin letters, digits and _. The game starts in offline mode."),
        nick, h("button", { class: "btn pri", onclick: async () => { await save({ nick: nick.value }); renderChrome(); renderPage(); loadSkin(); } }, t("Применить", "Apply")))),
    group(t("Скин", "Skin"), true,
      row(t("Источник скина", "Skin source"), t("Скин показывается в лаунчере по нику. Внутри игры в офлайн-режиме он не применяется.", "Shown in the launcher by nickname. It is not applied in-game in offline mode."),
        seg([["mojang", "Mojang"], ["ely", "Ely.by"], ["file", t("Файл", "File")]], s.skinSrc, async v => { await save({ skinSrc: v }); loadSkin(); })),
      row(t("Модель рук", "Arm model"), null,
        seg([[0, t("Авто", "Auto")], [1, t("Широкие", "Wide")], [2, t("Тонкие", "Slim")]], s.skinModel, async v => { await save({ skinModel: v }); loadSkin(); })),
      row(t("Свой скин", "Custom skin"), t("PNG 64×64 или 64×32", "PNG 64×64 or 64×32"), h("button", { class: "btn", onclick: () => file.click() }, t("Выбрать файл…", "Choose file…")), file)),
  ];
};

// --- настройки ---
PAGES.settings = () => {
  const s = S.settings;
  const maxMb = Math.max(2048, Math.min(32768, (S.ramMb || 16384) - 1024));
  const ramLbl = h("span", { style: "min-width:64px;text-align:right" }, (s.ramMb / 1024).toFixed(1) + " " + t("ГБ", "GB"));
  const ram = h("input", { type: "range", min: 1024, max: maxMb, step: 256, value: Math.min(s.ramMb, maxMb), oninput: () => { ramLbl.textContent = (ram.value / 1024).toFixed(1) + " " + t("ГБ", "GB"); }, onchange: () => save({ ramMb: +ram.value }) });
  const thrLbl = h("span", { style: "min-width:28px;text-align:right" }, s.threads);
  const thr = h("input", { type: "range", min: 1, max: 16, step: 1, value: s.threads, oninput: () => { thrLbl.textContent = thr.value; }, onchange: () => save({ threads: +thr.value }) });
  const java = h("input", { type: "text", value: s.javaPath, placeholder: t("авто", "auto"), spellcheck: "false", style: "width:300px", onchange: () => save({ javaPath: java.value.trim() }) });
  const jvm = h("input", { type: "text", value: s.jvmArgs, placeholder: "-XX:+UseZGC", spellcheck: "false", style: "width:300px", onchange: () => save({ jvmArgs: jvm.value }) });
  return [
    group(t("Лаунчер", "Launcher"), true,
      row(t("Оформление", "Style"), null, seg([["new", t("Графит", "Graphite")], ["old", t("Классика", "Classic")]], s.style, async v => { await save({ style: v }); applyTheme(); })),
      row(t("Тема", "Theme"), null, seg([[false, t("Тёмная", "Dark")], [true, t("Светлая", "Light")]], s.light, async v => { await save({ light: v }); applyTheme(); })),
      row(t("Меню", "Menu"), t("Боковое выезжает при наведении мыши на левый край. Нижнее — панель иконок.", "Side menu slides out when the mouse touches the left edge. Bottom is an icon dock."),
        seg([["side", t("Боковое", "Side")], ["bottom", t("Нижнее", "Bottom")]], s.menuType, async v => { await save({ menuType: v }); buildMenus(); })),
      row(t("Анимированный фон", "Animated background"), null, sw(s.aurora, async v => { await save({ aurora: v }); applyTheme(); })),
      row(t("Язык", "Language"), null, seg([["ru", "Русский"], ["en", "English"]], s.lang, async v => { await save({ lang: v }); relang(); }))),
    group(t("Игра", "Game"), false,
      row(t("Полный экран", "Fullscreen"), null, sw(s.fullscreen, v => save({ fullscreen: v }))),
      row(t("Отдельная папка для каждой версии", "Separate folder per version"), t("Моды и миры не смешиваются между версиями.", "Mods and worlds stay separate between versions."), sw(s.isolate, v => save({ isolate: v }))),
      row(t("Закрывать лаунчер после запуска", "Close launcher after start"), null, sw(s.closeOnPlay, v => save({ closeOnPlay: v }))),
      row(t("Потоки загрузки", "Download threads"), null, thr, thrLbl)),
    group(t("Память и Java", "Memory & Java"), false,
      row(t("Память игры", "Game memory"), t("Не ставьте больше половины оперативной памяти.", "Don't use more than half of your RAM."), ram, ramLbl),
      row(t("Путь к Java", "Java path"), t("Пусто — подходящая Java найдётся или скачается сама.", "Empty — a suitable Java is found or downloaded automatically."), java),
      row(t("Аргументы JVM", "JVM arguments"), null, jvm)),
    group(t("Специальное", "Special"), false,
      row(t("Папка игры", "Game folder"), null, h("button", { class: "btn", onclick: () => api("openfolder?which=game", {}) }, t("Открыть", "Open"))),
      row(t("Папка лаунчера", "Launcher folder"), S.dataDir, h("button", { class: "btn", onclick: () => api("openfolder?which=data", {}) }, t("Открыть", "Open"))),
      row(t("Скачанные файлы", "Downloaded files"), null, h("button", { class: "btn dng", onclick: confirmWipe }, t("Удалить скачанное", "Delete downloads")))),
  ];
};

// --- загрузчик версий ---
let VERS = null, verFilter = "release", verQuery = "", fabricList = [];
PAGES.versions = () => {
  const s = S.settings, root = h("div");
  const listBox = h("div", { class: "list" });
  const tools = h("div", { class: "tools" });
  const q = h("input", { type: "search", placeholder: t("Поиск версии…", "Search version…"), value: verQuery, oninput: () => { verQuery = q.value; fill(); } });
  const types = [["release", t("Релизы", "Releases")], ["snapshot", t("Снапшоты", "Snapshots")], ["old", t("Старые", "Old")]];
  tools.append(seg(types, verFilter, v => { verFilter = v; fill(); }), q);
  const loaderRow = h("div");
  const fillLoader = async () => {
    loaderRow.innerHTML = "";
    const sel = S.settings;
    const lv = h("select", { style: "max-width:200px", onchange: () => save({ loaderVer: lv.value }).then(renderChrome) });
    loaderRow.append(row(t("Загрузчик модов", "Mod loader"), t("Fabric скачается автоматически при запуске.", "Fabric is downloaded automatically on launch."),
      seg([["vanilla", "Vanilla"], ["fabric", "Fabric"]], sel.loader, async v => { await save({ loader: v, loaderVer: "" }); renderChrome(); fillLoader(); }),
      ...(sel.loader === "fabric" ? [lv] : [])));
    if (sel.loader === "fabric" && sel.selected) {
      lv.append(h("option", { value: "" }, t("Авто (последняя)", "Auto (latest)")));
      try { fabricList = (await api("fabric?mc=" + encodeURIComponent(sel.selected))).loaders; } catch (e) { fabricList = []; }
      for (const l of fabricList) lv.append(h("option", { value: l, selected: l === sel.loaderVer }, l));
    }
  };
  const fill = () => {
    listBox.innerHTML = "";
    if (!VERS) return;
    const want = verFilter;
    let arr = VERS.versions.filter(v => want === "old" ? v.type.startsWith("old_") : v.type === want);
    if (verQuery) arr = arr.filter(v => v.id.toLowerCase().includes(verQuery.toLowerCase()));
    const inst = new Set(S.installed || []);
    if (!arr.length) listBox.append(h("div", { class: "empty" }, t("Ничего не найдено", "Nothing found")));
    for (const v of arr.slice(0, 250)) {
      listBox.append(h("div", { class: "item" + (v.id === S.settings.selected ? " sel" : ""), onclick: async () => { await save({ selected: v.id, loaderVer: "" }); renderChrome(); fill(); fillLoader(); } },
        h("span", { class: "nm" }, v.id), inst.has(v.id) ? h("span", { class: "tag" }, t("скачана", "downloaded")) : null, h("span", { class: "meta" }, v.date)));
    }
  };
  root.append(group(t("Выбранная версия", "Selected version"), true,
    row(verTitle(), s.selected ? null : t("Выберите версию из списка ниже", "Pick a version from the list below"),
      h("button", { class: "btn pri", disabled: STATUS.busy || !s.selected, onclick: async () => { try { await api("install", {}); pollSoon(); toast(t("Скачивание началось", "Download started")); } catch (e) { toast(e.message); } } }, t("Скачать без запуска", "Download only"))),
    loaderRow));
  root.append(h("h3", { class: "sec" }, t("Все версии", "All versions")), tools, listBox);
  fillLoader();
  if (VERS) fill();
  else {
    listBox.append(h("div", { class: "empty" }, t("Загрузка списка…", "Loading list…")));
    api("versions").then(v => { VERS = v; if (page === "versions") fill(); }).catch(e => { listBox.innerHTML = ""; listBox.append(h("div", { class: "empty" }, e.message)); });
  }
  return root;
};

// --- каталог (Modrinth) ---
let catType = "mod", catQ = "", catFilter = true;
PAGES.catalog = () => {
  const root = h("div"), res = h("div"), more = h("button", { class: "btn hidden", style: "margin:12px auto;display:block" }, t("Показать ещё", "Show more"));
  const types = [["mod", t("Моды", "Mods")], ["resourcepack", t("Ресурспаки", "Resource packs")], ["shader", t("Шейдеры", "Shaders")], ["installed", t("Установлено", "Installed")]];
  const q = h("input", { type: "search", placeholder: t("Поиск на Modrinth…", "Search Modrinth…"), value: catQ, onkeydown: e => { if (e.key === "Enter") { catQ = q.value; run(true); } } });
  const filt = h("div", { class: "row", style: "border:0;padding:4px 0" }, h("div", { class: "l" }, t("Только для " + (S.settings.selected || "выбранной версии"), "Only for " + (S.settings.selected || "the selected version"))), h("div", { class: "c" }, sw(catFilter, v => { catFilter = v; run(true); })));
  let off = 0;
  async function run(reset) {
    if (reset) { off = 0; res.innerHTML = ""; }
    more.classList.add("hidden");
    if (catType === "installed") return runInstalled();
    res.append(h("div", { class: "empty", id: "cload" }, "…"));
    try {
      const r = await api("catalog/search?type=" + catType + "&q=" + encodeURIComponent(catQ) + "&filter=" + (catFilter ? 1 : 0) + "&offset=" + off);
      $("#cload") && $("#cload").remove();
      if (!r.hits.length && !off) res.append(h("div", { class: "empty" }, t("Ничего не найдено", "Nothing found")));
      for (const x of r.hits) {
        const btn = h("button", { class: "btn pri", onclick: async () => {
          btn.disabled = true; btn.textContent = "…";
          try { await api("catalog/install", { id: x.project_id, type: catType }); btn.textContent = t("Установлено", "Installed"); }
          catch (e) { btn.disabled = false; btn.textContent = t("Установить", "Install"); toast(e.message); }
        } }, t("Установить", "Install"));
        res.append(h("div", { class: "cat" }, x.icon_url ? h("img", { src: x.icon_url, loading: "lazy" }) : h("div", { class: "noimg" }),
          h("div", { class: "ti" }, h("b", null, x.title), h("span", null, x.description), h("span", null, x.author)),
          h("div", { class: "dl" }, fmtN(x.downloads) + " " + t("скач.", "dl")), btn));
      }
      off += r.hits.length;
      more.classList.toggle("hidden", off >= r.total_hits || !r.hits.length);
    } catch (e) { $("#cload") && $("#cload").remove(); res.append(h("div", { class: "empty" }, e.message)); }
  }
  async function runInstalled() {
    res.innerHTML = "";
    for (const [ty, label] of [["mod", t("Моды", "Mods")], ["resourcepack", t("Ресурспаки", "Resource packs")], ["shader", t("Шейдеры", "Shaders")]]) {
      const r = await api("catalog/installed?type=" + ty);
      res.append(h("h3", { class: "sec" }, label));
      if (!r.files.length) res.append(h("div", { class: "note" }, t("Пусто", "Empty")));
      for (const f of r.files) {
        res.append(h("div", { class: "cat" }, h("div", { class: "ti" }, h("b", null, f.name), h("span", null, (f.size / 1048576).toFixed(2) + " MB")),
          h("button", { class: "btn dng", onclick: async () => { await api("catalog/remove", { type: ty, name: f.name }); runInstalled(); } }, t("Удалить", "Remove"))));
      }
    }
  }
  more.onclick = () => run(false);
  const tabs = seg(types, catType, v => { catType = v; filt.classList.toggle("hidden", v === "installed"); q.classList.toggle("hidden", v === "installed"); run(true); });
  root.append(h("div", { class: "tools" }, tabs, q), filt, res, more);
  filt.classList.toggle("hidden", catType === "installed"); q.classList.toggle("hidden", catType === "installed");
  run(true);
  return root;
};
const fmtN = n => n >= 1e6 ? (n / 1e6).toFixed(1) + "M" : n >= 1e3 ? (n / 1e3).toFixed(1) + "K" : String(n);

// --- соцсети ---
PAGES.social = () => {
  const link = (name, sub, url) => row(name, sub, h("button", { class: "btn pri", onclick: () => api("openurl", { url }).catch(e => toast(e.message)) }, t("Открыть", "Open")));
  return [
    group(t("Mobilix в сети", "Mobilix online"), true,
      link("Telegram", "@MOBILIX_LAUNCHER", "https://t.me/mobilix_launcher"),
      link("GitHub · PC", "Kumolix/Mobilix-Launcher-PC", "https://github.com/Kumolix/Mobilix-Launcher-PC"),
      link("GitHub · Android", "Kumolix/Mobilix-Launcher-for-Minecraft", "https://github.com/Kumolix/Mobilix-Launcher-for-Minecraft")),
    h("p", { class: "note" }, t("Основано на Mobilix Launcher for Minecraft, автор Kumolix.", "Based on Mobilix Launcher for Minecraft by Kumolix.")),
  ];
};

// ---------- запуск ----------
async function refresh() { const st = await api("state"); S = st; return st; }

async function init() {
  try { await refresh(); } catch (e) { document.body.textContent = "Mobilix: " + e.message; return; }
  applyTheme();
  Skin3D.mount($("#skinView"));
  buildMenus(); renderChrome(); initHover(); loadSkin();
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
  $("#verLabel").onclick = () => { if (S.update) api("openurl", { url: "https://github.com/Kumolix/Mobilix-Launcher-PC/releases" }); };
  document.addEventListener("keydown", e => { if (e.key === "Escape") { if (!$("#modalLayer").classList.contains("hidden")) $("#modalLayer").classList.add("hidden"); else if (page) closePage(); } });
  document.addEventListener("contextmenu", e => { if (!e.target.closest("#logText,input")) e.preventDefault(); });
  let fails = 0;
  setInterval(() => api("ping").then(() => { fails = 0; }).catch(() => { if (++fails >= 4) { try { window.close(); } catch (e) {} } }), 4000);
  api("ping").catch(() => {});
  poll();
  if (!S.settings.selected) { openPage("versions"); }
}
init();
