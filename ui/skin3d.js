// Объёмный скин на CSS 3D: каждая часть тела — параллелепипед из шести canvas-граней.
const Skin3D = (() => {
  const K = 10; // пикселей экрана на пиксель скина
  let root, rig, parts = [], yawBase = 0, yaw = 0, pitch = -8, drag = null, t0 = performance.now(), raf = 0, scale = 1;

  function mk(w, h) { const c = document.createElement("canvas"); c.width = w; c.height = h; return c; }

  function uv(x, y, w, h, d) {
    return {
      top: [x + d, y, w, d], bottom: [x + d + w, y, w, d], right: [x, y + d, d, h],
      front: [x + d, y + d, w, h], left: [x + d + w, y + d, d, h], back: [x + 2 * d + w, y + d, w, h],
    };
  }

  function flipDraw(ctx, src, sx, sy, w, h, dx, dy) {
    ctx.save(); ctx.translate(dx + w, dy); ctx.scale(-1, 1);
    ctx.drawImage(src, sx, sy, w, h, 0, 0, w, h); ctx.restore();
  }

  // старые скины 64x32: левые конечности — зеркало правых
  function mirrorBlock(ctx, src, sb, db, w, h, d) {
    const s = uv(sb[0], sb[1], w, h, d), t = uv(db[0], db[1], w, h, d);
    const fl = (a, b) => flipDraw(ctx, src, a[0], a[1], a[2], a[3], b[0], b[1]);
    fl(s.top, t.top); fl(s.bottom, t.bottom); fl(s.front, t.front); fl(s.back, t.back);
    fl(s.left, t.right); fl(s.right, t.left);
  }

  function normalize(img) {
    const c = mk(64, 64), x = c.getContext("2d");
    x.imageSmoothingEnabled = false;
    x.drawImage(img, 0, 0);
    if (img.height === 32) {
      mirrorBlock(x, c, [40, 16], [32, 48], 4, 12, 4);
      mirrorBlock(x, c, [0, 16], [16, 48], 4, 12, 4);
    }
    return c;
  }

  function isSlim(c) {
    const d = c.getContext("2d").getImageData(54, 20, 2, 12).data;
    for (let i = 3; i < d.length; i += 4) if (d[i] !== 0) return false;
    return true;
  }

  function defaultSkin() {
    const c = mk(64, 64), x = c.getContext("2d");
    const R = (col, a, b, w, h) => { x.fillStyle = col; x.fillRect(a, b, w, h); };
    R("#c58c6b", 0, 0, 32, 16); R("#4a2f1b", 8, 0, 8, 8); R("#4a2f1b", 0, 8, 32, 3); R("#4a2f1b", 24, 8, 8, 8);
    R("#ffffff", 9, 12, 2, 1); R("#ffffff", 13, 12, 2, 1); R("#3d3d9e", 10, 12, 1, 1); R("#3d3d9e", 13, 12, 1, 1);
    R("#7a4a33", 11, 14, 2, 1);
    R("#2fa3a8", 16, 16, 24, 16);
    R("#2fa3a8", 40, 16, 16, 4); R("#c58c6b", 40, 20, 16, 12); R("#c58c6b", 44, 16, 8, 4);
    R("#2fa3a8", 32, 48, 16, 4); R("#c58c6b", 32, 52, 16, 12); R("#c58c6b", 36, 48, 8, 4);
    R("#3b3bb0", 0, 16, 16, 16); R("#3b3bb0", 16, 48, 16, 16); R("#6b6b6b", 4, 28, 8, 4); R("#6b6b6b", 20, 60, 8, 4);
    return c;
  }

  function face(cub, src, r, w, h, tr) {
    const c = mk(r[2], r[3]);
    c.getContext("2d").drawImage(src, r[0], r[1], r[2], r[3], 0, 0, r[2], r[3]);
    c.style.cssText = `position:absolute;left:${-w / 2}px;top:${-h / 2}px;width:${w}px;height:${h}px;image-rendering:pixelated;backface-visibility:hidden;transform:${tr}`;
    cub.appendChild(c);
  }

  function cuboid(src, base, W, H, D, grow) {
    const g = grow ? K * 0.5 : 0, w = W * K + g * 2, h = H * K + g * 2, d = D * K + g * 2;
    const cub = document.createElement("div");
    cub.style.cssText = "position:absolute;left:0;top:0;width:0;height:0;transform-style:preserve-3d";
    const u = uv(base[0], base[1], W, H, D);
    face(cub, src, u.front, w, h, `translateZ(${d / 2}px)`);
    face(cub, src, u.back, w, h, `rotateY(180deg) translateZ(${d / 2}px)`);
    face(cub, src, u.right, d, h, `rotateY(-90deg) translateZ(${w / 2}px)`);
    face(cub, src, u.left, d, h, `rotateY(90deg) translateZ(${w / 2}px)`);
    face(cub, src, u.top, w, d, `rotateX(90deg) translateZ(${h / 2}px)`);
    face(cub, src, u.bottom, w, d, `rotateX(-90deg) translateZ(${h / 2}px)`);
    return cub;
  }

  function build(sk, slim) {
    rig.innerHTML = ""; parts = [];
    const aw = slim ? 3 : 4;
    const defs = [
      { n: "head", b: [0, 0], o: [32, 0], s: [8, 8, 8], pos: [0, -10], off: 0 },
      { n: "body", b: [16, 16], o: [16, 32], s: [8, 12, 4], pos: [0, 0], off: 0 },
      { n: "rarm", b: [40, 16], o: [40, 32], s: [aw, 12, 4], pos: [-(4 + aw / 2), -6], off: 6, swing: 1 },
      { n: "larm", b: [32, 48], o: [48, 48], s: [aw, 12, 4], pos: [4 + aw / 2, -6], off: 6, swing: -1 },
      { n: "rleg", b: [0, 16], o: [0, 32], s: [4, 12, 4], pos: [-2, 6], off: 6, swing: -1 },
      { n: "lleg", b: [16, 48], o: [0, 48], s: [4, 12, 4], pos: [2, 6], off: 6, swing: 1 },
    ];
    for (const p of defs) {
      const grp = document.createElement("div");
      grp.style.cssText = `position:absolute;left:0;top:0;width:0;height:0;transform-style:preserve-3d;transform:translate3d(${p.pos[0] * K}px,${p.pos[1] * K}px,0)`;
      const sw = document.createElement("div");
      sw.style.cssText = "position:absolute;left:0;top:0;width:0;height:0;transform-style:preserve-3d";
      const holder = document.createElement("div");
      holder.style.cssText = `position:absolute;left:0;top:0;width:0;height:0;transform-style:preserve-3d;transform:translateY(${p.off * K}px)`;
      holder.appendChild(cuboid(sk, p.b, p.s[0], p.s[1], p.s[2], false));
      holder.appendChild(cuboid(sk, p.o, p.s[0], p.s[1], p.s[2], true));
      sw.appendChild(holder); grp.appendChild(sw); rig.appendChild(grp);
      parts.push({ sw, swing: p.swing || 0 });
    }
  }

  function fit() {
    if (!root) return;
    const h = root.clientHeight || 400;
    scale = Math.max(0.4, Math.min(1.6, (h * 0.80) / (32 * K)));
  }

  function frame(now) {
    const t = (now - t0) / 1000;
    if (!drag) {
      yawBase *= 0.94;
      yaw = yawBase + Math.sin(t * 0.6) * 24 + 18;
    }
    const a = Math.sin(t * 2.2) * 14;
    for (const p of parts) if (p.swing) p.sw.style.transform = `rotateX(${a * p.swing}deg)`;
    rig.style.transform = `scale(${scale}) rotateX(${pitch}deg) rotateY(${yaw}deg)`;
    raf = requestAnimationFrame(frame);
  }

  function mount(el) {
    root = el;
    root.classList.add("s3d");
    rig = document.createElement("div");
    rig.className = "rig";
    root.appendChild(rig);
    root.addEventListener("mousedown", e => { drag = { x: e.clientX, y: e.clientY, yaw, pitch }; e.preventDefault(); });
    window.addEventListener("mousemove", e => {
      if (!drag) return;
      yaw = drag.yaw + (e.clientX - drag.x) * 0.6;
      pitch = Math.max(-40, Math.min(30, drag.pitch - (e.clientY - drag.y) * 0.3));
    });
    window.addEventListener("mouseup", () => { if (drag) { yawBase = yaw - 18; drag = null; } });
    window.addEventListener("resize", fit);
    fit(); build(normalize(defaultSkin()), false);
    if (!raf) raf = requestAnimationFrame(frame);
  }

  // img: загруженный Image или null (скин по умолчанию); slimMode: 0 авто, 1 широкий, 2 стройный; hint: "slim"/"default"
  function setSkin(img, slimMode, hint) {
    const sk = img ? normalize(img) : normalize(defaultSkin());
    let slim = slimMode === 2 || (slimMode === 0 && (hint === "slim" || (hint !== "default" && isSlim(sk))));
    build(sk, slim); fit();
  }

  return { mount, setSkin, fit };
})();
