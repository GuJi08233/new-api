/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// 斗法的画面：水墨淡彩，颜色全取 wendao.css 的 --wd-* 变量，浅色深色跟着主题走。只读规则的状态，不改它；
// 飞溅、飘字、拖影这些只在画面里，和规则无关。房间比画面大时镜头跟着主角。
import { DIRS, SCALE, TILE_PX, bladePoints, content } from './engine';

const rad = (a) => (a * Math.PI * 2) / DIRS;
const toPx = (v) => v / SCALE;

export function readPalette(el) {
  const cs = getComputedStyle(el);
  const v = (name, fallback) => cs.getPropertyValue(name).trim() || fallback;
  return {
    paper: v('--wd-paper', '#f5f0e5'),
    ink: v('--wd-ink', '#2b2824'),
    red: v('--wd-red', '#b23a2a'),
    green: v('--wd-acc-green', '#6f8f5a'),
    fire: v('--wd-acc-fire', '#c2542f'),
    ice: v('--wd-acc-ice', '#6c97b3'),
    violet: v('--wd-acc-violet', '#6c5a8e'),
    stone: v('--wd-acc-stone', '#7b7d78'),
    bone: v('--wd-bone', '#e8e0cf'),
    skin: v('--wd-skin', '#ecdcc3'),
    tint: v('--wd-tint', '#6e8f78'),
    brown: v('--wd-robe-brown', '#8a7356'),
    robe: {
      warrior: v('--wd-robe-red', '#8e3326'),
      mage: v('--wd-robe-blue', '#4d6577'),
      ranger: v('--wd-robe-green', '#5f7d63'),
    },
    serif: v('--wd-serif', 'serif'),
    dark: document.body.getAttribute('theme-mode') === 'dark',
  };
}

const foeColor = (pal, kind) =>
  ({
    slime: pal.green,
    goblin: pal.brown,
    goblin_chief: pal.brown,
    skeleton: pal.bone,
    moth: pal.violet,
    gargoyle: pal.stone,
    slime_king: pal.tint,
  })[kind] || pal.stone;

// 同一格子每次画出同样的笔触。
const tileNoise = (x, y) => {
  let h = (x * 374761393 + y * 668265263) >>> 0;
  h = Math.imul(h ^ (h >>> 13), 1274126177) >>> 0;
  return (h ^ (h >>> 16)) / 4294967296;
};

export function createRenderer(canvas, options) {
  const ctx = canvas.getContext('2d');
  let pal = readPalette(canvas);
  let labels = options.labels;
  const reduced = options.reduced;
  const view = { w: 1, h: 1, dpr: 1, zoom: 1, camX: 0, camY: 0, follow: false };
  let room = null;
  let roomZoom = 0;
  const bits = [];
  const floats = [];
  const trail = [];
  let shake = 0;
  let hurtFlash = 0;
  let clock = 0;

  function resize(w, h) {
    view.w = Math.max(1, w);
    view.h = Math.max(1, h);
    view.dpr = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = Math.round(view.w * view.dpr);
    canvas.height = Math.round(view.h * view.dpr);
    room = null;
  }

  function layout(f) {
    const worldW = f.w * TILE_PX;
    const worldH = f.h * TILE_PX;
    const fit = Math.min(view.w / worldW, view.h / worldH);
    // 整间房缩到六成以上还看得清就整间放下(电脑上一般如此)；再小就放大到约十五格宽、镜头跟着主角，屏外的敌人在边上标方向。
    view.follow = fit < 0.6;
    view.zoom = view.follow
      ? Math.min(1, Math.max(0.62, Math.min(view.w, view.h) / (15 * TILE_PX)))
      : fit;
    const vw = view.w / view.zoom;
    const vh = view.h / view.zoom;
    const clamp = (v, lo, hi) =>
      hi < lo ? (lo + hi) / 2 : Math.min(Math.max(v, lo), hi);
    // 跟随时镜头可以越过房间边一点，主角贴着墙时也不会缩到画面角上(手机右下角是按键)。
    const padX = view.follow ? vw * 0.2 : 0;
    const padY = view.follow ? vh * 0.2 : 0;
    const tx = clamp(toPx(f.p.x) - vw / 2, -padX, worldW - vw + padX);
    const ty = clamp(toPx(f.p.y) - vh / 2, -padY, worldH - vh + padY);
    if (!view.follow || room === null) {
      view.camX = tx;
      view.camY = ty;
    } else {
      view.camX += (tx - view.camX) * 0.18;
      view.camY += (ty - view.camY) * 0.18;
    }
  }

  // 房间不动的部分(地面、墙、柱、石头)先画到一张离屏画布上，缩放变了才重画。
  function paintRoom(f) {
    const scale = view.zoom * view.dpr;
    const layer = document.createElement('canvas');
    layer.width = Math.ceil(f.w * TILE_PX * scale);
    layer.height = Math.ceil(f.h * TILE_PX * scale);
    const g = layer.getContext('2d');
    g.scale(scale, scale);
    g.fillStyle = pal.paper;
    g.fillRect(0, 0, f.w * TILE_PX, f.h * TILE_PX);
    g.strokeStyle = pal.ink;
    g.lineCap = 'round';
    for (let y = 0; y < f.h; y++) {
      for (let x = 0; x < f.w; x++) {
        const i = y * f.w + x;
        const cx = x * TILE_PX;
        const cy = y * TILE_PX;
        if (!f.solid[i]) {
          const n = tileNoise(x, y);
          if (n < 0.28) {
            g.globalAlpha = 0.07 + n * 0.12;
            g.lineWidth = 1.2;
            g.beginPath();
            const ox = cx + 6 + n * 18;
            const oy = cy + 10 + ((n * 97) % 1) * 14;
            g.moveTo(ox, oy);
            g.quadraticCurveTo(ox + 4, oy - 5, ox + 9, oy - 2);
            g.stroke();
          }
          continue;
        }
        if (f.rocks[i]) {
          g.globalAlpha = 0.45;
          g.fillStyle = pal.stone;
          g.beginPath();
          g.ellipse(cx + 16, cy + 17, 14, 12, 0.2, 0, Math.PI * 2);
          g.fill();
          g.globalAlpha = 0.9;
          g.lineWidth = 1.6;
          g.stroke();
          g.globalAlpha = 0.35;
          g.beginPath();
          g.moveTo(cx + 9, cy + 13);
          g.quadraticCurveTo(cx + 14, cy + 8, cx + 21, cy + 10);
          g.stroke();
          continue;
        }
        const edge = x === 0 || y === 0 || x === f.w - 1 || y === f.h - 1;
        // 深色主题的墨是浅色，墙照浅色铺满会太亮，压淡一些。
        g.globalAlpha = (edge ? 0.86 : 0.8) * (pal.dark ? 0.5 : 1);
        g.fillStyle = pal.ink;
        g.fillRect(cx, cy, TILE_PX, TILE_PX);
        if (!edge) {
          g.globalAlpha = 0.22;
          g.fillStyle = pal.paper;
          g.fillRect(cx + 6, cy + 6, TILE_PX - 12, TILE_PX - 12);
        } else {
          const n = tileNoise(x + 31, y + 17);
          g.globalAlpha = 0.18;
          g.fillStyle = pal.paper;
          g.fillRect(cx + 4 + n * 16, cy + 6 + ((n * 53) % 1) * 16, 6, 2);
        }
      }
    }
    // 墙脚一道淡墨，让地面和墙分开。
    g.globalAlpha = 0.16;
    g.lineWidth = 3;
    g.strokeStyle = pal.ink;
    g.strokeRect(
      TILE_PX + 1.5,
      TILE_PX + 1.5,
      (f.w - 2) * TILE_PX - 3,
      (f.h - 2) * TILE_PX - 3,
    );
    g.globalAlpha = 1;
    room = layer;
    roomZoom = view.zoom;
  }

  function burst(x, y, n, color, speed, life) {
    if (reduced) n = Math.min(n, 2);
    for (let i = 0; i < n; i++) {
      const a = Math.random() * Math.PI * 2;
      const s = speed * (0.4 + Math.random() * 0.8);
      bits.push({
        x,
        y,
        vx: Math.cos(a) * s,
        vy: Math.sin(a) * s,
        life,
        max: life,
        color,
        size: 1.5 + Math.random() * 2,
      });
    }
  }

  function float(x, y, text, color) {
    floats.push({ x, y, text, color, life: 40 });
  }

  function absorb(f, events) {
    for (const e of events) {
      const x = toPx(e.x);
      const y = toPx(e.y);
      switch (e.k) {
        case 'hit':
          burst(x, y, 4, pal.ink, 1.6, 14);
          float(x, y - 12, String(e.n), pal.ink);
          break;
        case 'die':
          bits.push({ blot: true, x, y, life: 26, max: 26, size: 12 });
          burst(x, y, 9, pal.ink, 2.2, 22);
          break;
        case 'pick':
          burst(x, y, 5, pal.green, 1.2, 16);
          break;
        case 'hurt':
          hurtFlash = 1;
          if (!reduced) shake = 8;
          float(x, y - 16, `-${e.n}`, pal.red);
          break;
        case 'deflect':
          burst(x, y, 3, pal.paper, 1.8, 10);
          break;
        case 'cast':
          bits.push({
            ring: true,
            x,
            y,
            life: 22,
            max: 22,
            size: 10,
            color: pal.robe[f.p.cls],
          });
          break;
        case 'phase':
          if (!reduced) shake = 14;
          bits.push({
            ring: true,
            x,
            y,
            life: 30,
            max: 30,
            size: 30,
            color: pal.red,
          });
          break;
        case 'dry':
          float(x, y - 22, labels.noMana, pal.ice);
          break;
        case 'spawn':
          burst(x, y, 6, pal.ink, 1.4, 18);
          break;
        default:
      }
    }
  }

  function drawFoe(e, f) {
    const spec = content.foes[e.kind];
    const x = toPx(e.x);
    const y = toPx(e.y);
    const r = toPx(e.r);
    const color = foeColor(pal, e.kind);
    const look = Math.atan2(f.p.y - e.y, f.p.x - e.x);
    // 出手的预兆：蓄力放弹画一截朝向的朱砂线，冲撞画出要冲的路线，炮台快放时外圈收紧。
    ctx.save();
    ctx.strokeStyle = pal.red;
    if (spec.ai === 'shoot' && e.state === 1) {
      ctx.globalAlpha = 0.55;
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(x, y);
      const len = r + 10 + (1 - e.t / spec.windup) * 26;
      ctx.lineTo(x + Math.cos(rad(e.a)) * len, y + Math.sin(rad(e.a)) * len);
      ctx.stroke();
    }
    const charging =
      (spec.ai === 'charge' && e.state === 1) ||
      (spec.ai === 'boss' && e.state === 3);
    if (charging) {
      const dash =
        spec.ai === 'boss'
          ? spec.boss.charge
          : { dash: spec.dash, ticks: spec.dash_ticks };
      const len = (dash.dash / 60) * dash.ticks;
      ctx.globalAlpha = 0.35;
      ctx.lineWidth = r * 1.4;
      ctx.lineCap = 'round';
      ctx.setLineDash([8, 10]);
      ctx.beginPath();
      ctx.moveTo(x, y);
      ctx.lineTo(x + Math.cos(rad(e.a)) * len, y + Math.sin(rad(e.a)) * len);
      ctx.stroke();
      ctx.setLineDash([]);
    }
    if (spec.ai === 'turret' && e.cool < 30) {
      ctx.globalAlpha = 0.5;
      ctx.lineWidth = 1.5;
      ctx.beginPath();
      ctx.arc(x, y, r + 4 + e.cool * 0.5, 0, Math.PI * 2);
      ctx.stroke();
    }
    ctx.restore();

    const bob =
      spec.ai === 'chase' || spec.ai === 'boss'
        ? Math.sin(clock / 9 + e.id) * 1.2
        : 0;
    ctx.save();
    ctx.translate(x, y + bob);
    ctx.lineWidth = spec.ai === 'boss' ? 2.4 : 1.6;
    ctx.strokeStyle = pal.ink;
    ctx.fillStyle = color;
    if (e.kind === 'moth') {
      const flap = reduced
        ? 0.8
        : 0.6 + Math.abs(Math.sin(clock / 4 + e.id)) * 0.5;
      ctx.globalAlpha = 0.55;
      for (const side of [-1, 1]) {
        ctx.beginPath();
        ctx.ellipse(
          side * r * 0.9,
          -2,
          r * flap,
          r * 0.7,
          side * 0.5,
          0,
          Math.PI * 2,
        );
        ctx.fill();
        ctx.stroke();
      }
    }
    ctx.globalAlpha = 0.88;
    ctx.beginPath();
    if (spec.ai === 'turret') {
      ctx.rect(-r, -r, r * 2, r * 2);
    } else if (spec.ai === 'chase' || spec.ai === 'boss') {
      ctx.ellipse(0, 0, r * 1.08, r * 0.92, 0, 0, Math.PI * 2);
    } else {
      ctx.arc(0, 0, r, 0, Math.PI * 2);
    }
    ctx.fill();
    ctx.globalAlpha = 1;
    ctx.stroke();
    if (e.kind === 'goblin' || e.kind === 'goblin_chief') {
      ctx.beginPath();
      ctx.moveTo(-r * 0.6, -r * 0.7);
      ctx.lineTo(-r * 0.9, -r * 1.35);
      ctx.moveTo(r * 0.6, -r * 0.7);
      ctx.lineTo(r * 0.9, -r * 1.35);
      ctx.stroke();
    }
    if (spec.ai === 'boss') {
      ctx.beginPath();
      for (let k = -1; k <= 1; k++) {
        ctx.moveTo(k * r * 0.45, -r * 0.82);
        ctx.lineTo(k * r * 0.45 + k * 2, -r * 1.25 + Math.abs(k) * 4);
      }
      ctx.stroke();
      if (e.phase > 0) {
        ctx.strokeStyle = pal.red;
        ctx.globalAlpha = 0.8;
        ctx.beginPath();
        ctx.ellipse(0, 0, r * 1.22, r * 1.06, 0, 0, Math.PI * 2);
        ctx.stroke();
      }
    }
    // 眼睛看着主角。
    const ex = Math.cos(look) * r * 0.35;
    const ey = Math.sin(look) * r * 0.35;
    ctx.globalAlpha = 1;
    ctx.fillStyle = e.kind === 'skeleton' ? pal.ink : pal.paper;
    for (const side of [-1, 1]) {
      ctx.beginPath();
      ctx.arc(
        ex + side * r * 0.32 * Math.cos(look + Math.PI / 2),
        ey + side * r * 0.32 * Math.sin(look + Math.PI / 2),
        Math.max(1.6, r * 0.16),
        0,
        Math.PI * 2,
      );
      ctx.fill();
    }
    if (e.flash > 0) {
      ctx.globalAlpha = (e.flash / 6) * 0.6;
      ctx.fillStyle = pal.paper;
      ctx.beginPath();
      ctx.arc(0, 0, r * 1.05, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.restore();
    if (e.hp < e.maxHp && spec.ai !== 'boss') {
      ctx.save();
      ctx.globalAlpha = 0.25;
      ctx.fillStyle = pal.ink;
      ctx.fillRect(x - r, y - r - 7, r * 2, 3);
      ctx.globalAlpha = 0.9;
      ctx.fillStyle = pal.red;
      ctx.fillRect(x - r, y - r - 7, (r * 2 * Math.max(e.hp, 0)) / e.maxHp, 3);
      ctx.restore();
    }
  }

  function drawShot(s) {
    const x = toPx(s.x);
    const y = toPx(s.y);
    const r = toPx(s.r);
    ctx.save();
    if (s.hostile) {
      ctx.fillStyle = pal.red;
      ctx.globalAlpha = 0.95;
      ctx.beginPath();
      ctx.arc(x, y, r, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = pal.paper;
      ctx.globalAlpha = 0.85;
      ctx.beginPath();
      ctx.arc(x, y, r * 0.42, 0, Math.PI * 2);
      ctx.fill();
    } else if (s.kind === 'fire') {
      ctx.fillStyle = pal.fire;
      ctx.globalAlpha = 0.85;
      ctx.beginPath();
      ctx.arc(x, y, r, 0, Math.PI * 2);
      ctx.fill();
    } else if (s.kind === 'talisman') {
      ctx.translate(x, y);
      ctx.rotate(rad(s.a));
      ctx.fillStyle = pal.skin;
      ctx.strokeStyle = pal.red;
      ctx.lineWidth = 1.2;
      ctx.fillRect(-5, -3.5, 10, 7);
      ctx.strokeRect(-5, -3.5, 10, 7);
      ctx.beginPath();
      ctx.moveTo(-2, 0);
      ctx.lineTo(3, 0);
      ctx.stroke();
    } else {
      ctx.translate(x, y);
      ctx.rotate(rad(s.a));
      ctx.strokeStyle = pal.ink;
      ctx.lineCap = 'round';
      ctx.lineWidth = 2.2;
      ctx.beginPath();
      ctx.moveTo(-9, 0);
      ctx.lineTo(7, 0);
      ctx.stroke();
      ctx.lineWidth = 1;
      ctx.globalAlpha = 0.35;
      ctx.beginPath();
      ctx.moveTo(-18, 0);
      ctx.lineTo(-9, 0);
      ctx.stroke();
    }
    ctx.restore();
  }

  function drawDrop(d) {
    const x = toPx(d.x);
    const y = toPx(d.y) + Math.sin(clock / 10 + d.id) * (reduced ? 0 : 1.5);
    ctx.save();
    ctx.lineWidth = 1.2;
    ctx.strokeStyle = pal.ink;
    if (d.kind === 'stone') {
      ctx.fillStyle = pal.green;
      ctx.beginPath();
      ctx.moveTo(x, y - 6);
      ctx.lineTo(x + 4.5, y);
      ctx.lineTo(x, y + 6);
      ctx.lineTo(x - 4.5, y);
      ctx.closePath();
      ctx.fill();
      ctx.stroke();
    } else {
      ctx.fillStyle = d.kind === 'heal' ? pal.red : pal.ice;
      ctx.beginPath();
      ctx.arc(x, y, 4.5, 0, Math.PI * 2);
      ctx.fill();
      ctx.stroke();
    }
    ctx.restore();
  }

  function drawPlayer(f) {
    const p = f.p;
    const x = toPx(p.x);
    const y = toPx(p.y);
    const r = toPx(p.r);
    const face = rad(p.face);
    const robe = pal.robe[p.cls] || pal.ink;
    if (p.dash > 0) trail.push({ x, y, life: 12 });
    for (const t of trail) {
      ctx.globalAlpha = (t.life / 12) * 0.25;
      ctx.fillStyle = pal.ink;
      ctx.beginPath();
      ctx.arc(t.x, t.y, r, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.globalAlpha = 1;
    const weapon = content.weapons[p.weapons[p.slot]];
    if (p.swing > 0) {
      const w = content.weapons[p.weapons[p.slot]];
      const reach = w.range || 50;
      const arc = rad(w.arc || 8);
      ctx.save();
      ctx.globalAlpha = (p.swing / 10) * 0.55;
      ctx.strokeStyle = pal.ink;
      ctx.lineWidth = 7;
      ctx.lineCap = 'round';
      ctx.beginPath();
      ctx.arc(x, y, reach * 0.78, face - arc, face + arc);
      ctx.stroke();
      ctx.restore();
    }
    const blink = p.hurt > 0 && !reduced && Math.floor(p.hurt / 4) % 2 === 0;
    ctx.save();
    ctx.globalAlpha = blink ? 0.35 : 1;
    // 手里的法宝：剑是一道墨线，符箓是一张黄纸。
    ctx.strokeStyle = pal.ink;
    ctx.lineCap = 'round';
    if (weapon.kind === 'melee' && p.swing === 0) {
      ctx.lineWidth = 2.4;
      ctx.beginPath();
      ctx.moveTo(x + Math.cos(face) * (r - 2), y + Math.sin(face) * (r - 2));
      ctx.lineTo(x + Math.cos(face) * (r + 15), y + Math.sin(face) * (r + 15));
      ctx.stroke();
    } else if (weapon.kind === 'shot') {
      ctx.fillStyle = p.weapons[p.slot] === 'talisman' ? pal.skin : pal.ink;
      ctx.beginPath();
      ctx.arc(
        x + Math.cos(face) * (r + 5),
        y + Math.sin(face) * (r + 5),
        3,
        0,
        Math.PI * 2,
      );
      ctx.fill();
    }
    ctx.fillStyle = robe;
    ctx.lineWidth = 1.8;
    ctx.beginPath();
    ctx.arc(x, y, r, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();
    ctx.fillStyle = pal.skin;
    ctx.beginPath();
    ctx.arc(
      x + Math.cos(face) * 3,
      y + Math.sin(face) * 3,
      r * 0.48,
      0,
      Math.PI * 2,
    );
    ctx.fill();
    ctx.stroke();
    ctx.fillStyle = pal.ink;
    ctx.beginPath();
    ctx.arc(
      x + Math.cos(face) * 1.5,
      y + Math.sin(face) * 1.5,
      r * 0.26,
      0,
      Math.PI * 2,
    );
    ctx.fill();
    if (p.hurt > 0 && reduced) {
      ctx.setLineDash([3, 3]);
      ctx.beginPath();
      ctx.arc(x, y, r + 3, 0, Math.PI * 2);
      ctx.stroke();
      ctx.setLineDash([]);
    }
    ctx.restore();
    if (p.blades > 0) {
      for (const [bx, by, a] of bladePoints(f)) {
        ctx.save();
        ctx.translate(toPx(bx), toPx(by));
        ctx.rotate(rad(a) + Math.PI / 2);
        ctx.strokeStyle = pal.ink;
        ctx.lineWidth = 2.2;
        ctx.lineCap = 'round';
        ctx.beginPath();
        ctx.moveTo(-8, 0);
        ctx.lineTo(8, 0);
        ctx.stroke();
        ctx.restore();
      }
    }
  }

  function drawMarks(f) {
    for (const m of f.marks) {
      const x = toPx(m.x);
      const y = toPx(m.y);
      const k = 1 - f.markT / 50;
      const r = toPx(content.foes[m.kind].radius * SCALE);
      ctx.save();
      ctx.strokeStyle = pal.red;
      ctx.globalAlpha = 0.35 + k * 0.4;
      ctx.lineWidth = 1.6;
      ctx.beginPath();
      ctx.arc(x, y, r * (0.4 + k * 0.8), 0, Math.PI * 2);
      ctx.stroke();
      ctx.beginPath();
      ctx.moveTo(x - 4, y);
      ctx.lineTo(x + 4, y);
      ctx.moveTo(x, y - 4);
      ctx.lineTo(x, y + 4);
      ctx.stroke();
      ctx.restore();
    }
  }

  function drawBits() {
    for (let i = bits.length - 1; i >= 0; i--) {
      const b = bits[i];
      b.life--;
      if (b.life <= 0) {
        bits.splice(i, 1);
        continue;
      }
      const k = b.life / b.max;
      ctx.save();
      if (b.blot) {
        ctx.globalAlpha = k * 0.35;
        ctx.fillStyle = pal.ink;
        ctx.beginPath();
        ctx.arc(b.x, b.y, b.size * (1.6 - k * 0.6), 0, Math.PI * 2);
        ctx.fill();
      } else if (b.ring) {
        ctx.globalAlpha = k * 0.6;
        ctx.strokeStyle = b.color;
        ctx.lineWidth = 3;
        ctx.beginPath();
        ctx.arc(b.x, b.y, b.size + (1 - k) * 70, 0, Math.PI * 2);
        ctx.stroke();
      } else {
        b.x += b.vx;
        b.y += b.vy;
        b.vx *= 0.9;
        b.vy *= 0.9;
        ctx.globalAlpha = k * 0.8;
        ctx.fillStyle = b.color;
        ctx.beginPath();
        ctx.arc(b.x, b.y, b.size * k + 0.5, 0, Math.PI * 2);
        ctx.fill();
      }
      ctx.restore();
    }
    for (let i = floats.length - 1; i >= 0; i--) {
      const t = floats[i];
      t.life--;
      if (t.life <= 0) {
        floats.splice(i, 1);
        continue;
      }
      if (!reduced) t.y -= 0.45;
      ctx.save();
      ctx.globalAlpha = Math.min(1, t.life / 15);
      ctx.fillStyle = t.color;
      ctx.font = `600 11px ${pal.serif}`;
      ctx.textAlign = 'center';
      ctx.fillText(t.text, t.x, t.y);
      ctx.restore();
    }
    for (let i = trail.length - 1; i >= 0; i--) {
      if (--trail[i].life <= 0) trail.splice(i, 1);
    }
  }

  function bar(x, y, w, h, value, max, color, text) {
    ctx.globalAlpha = 0.18;
    ctx.fillStyle = pal.ink;
    ctx.fillRect(x, y, w, h);
    ctx.globalAlpha = 0.9;
    ctx.fillStyle = color;
    ctx.fillRect(x, y, (w * Math.max(0, value)) / max, h);
    ctx.globalAlpha = 1;
    ctx.strokeStyle = pal.ink;
    ctx.lineWidth = 1;
    ctx.strokeRect(x + 0.5, y + 0.5, w - 1, h - 1);
    if (text) {
      ctx.fillStyle = pal.ink;
      ctx.font = `600 11px ${pal.serif}`;
      ctx.textAlign = 'left';
      ctx.fillText(text, x + 4, y + h - 3);
    }
  }

  function drawHud(f) {
    const p = f.p;
    ctx.save();
    ctx.setTransform(view.dpr, 0, 0, view.dpr, 0, 0);
    const w = Math.min(170, view.w * 0.42);
    ctx.globalAlpha = 0.72;
    ctx.fillStyle = pal.paper;
    ctx.fillRect(6, 6, w + 12, 40);
    bar(12, 11, w, 14, p.hp, p.maxHp, pal.red, labels.hp(p.hp, p.maxHp));
    bar(
      12,
      28,
      w,
      12,
      p.mp,
      p.maxMp,
      pal.ice,
      labels.mp(Math.floor(p.mp / 100)),
    );
    const boss = f.foes.find((e) => content.foes[e.kind].ai === 'boss');
    ctx.font = `600 12px ${pal.serif}`;
    ctx.textAlign = 'right';
    ctx.fillStyle = pal.ink;
    if (boss) {
      const bw = Math.min(260, view.w - 40);
      const bx = (view.w - bw) / 2;
      const by = view.h - 22;
      ctx.globalAlpha = 0.72;
      ctx.fillStyle = pal.paper;
      ctx.fillRect(bx - 4, by - 18, bw + 8, 34);
      ctx.globalAlpha = 1;
      ctx.fillStyle = pal.ink;
      ctx.textAlign = 'center';
      ctx.fillText(labels.foe(boss.kind), view.w / 2, by - 5);
      bar(bx, by, bw, 9, boss.hp, boss.maxHp, pal.red, '');
    } else {
      const shown = Math.min(
        f.wave + (f.marks.length > 0 ? 1 : 0),
        f.waves.length,
      );
      const text = labels.wave(Math.max(shown, 1), f.waves.length);
      ctx.globalAlpha = 0.72;
      ctx.fillStyle = pal.paper;
      const tw = ctx.measureText(text).width;
      ctx.fillRect(view.w - tw - 18, 6, tw + 12, 22);
      ctx.globalAlpha = 1;
      ctx.fillStyle = pal.ink;
      ctx.fillText(text, view.w - 12, 22);
    }
    if (view.follow) {
      // 屏外的敌人：在画面边上朝它的方向画一个朱砂小三角。
      const cx = view.w / 2;
      const cy = view.h / 2;
      ctx.fillStyle = pal.red;
      for (const e of f.foes) {
        const sx = (toPx(e.x) - view.camX) * view.zoom;
        const sy = (toPx(e.y) - view.camY) * view.zoom;
        if (sx >= 0 && sx <= view.w && sy >= 0 && sy <= view.h) continue;
        const a = Math.atan2(sy - cy, sx - cx);
        const k = Math.min(
          Math.abs((cx - 14) / (Math.cos(a) || 1e-6)),
          Math.abs((cy - 14) / (Math.sin(a) || 1e-6)),
        );
        const ex = cx + Math.cos(a) * k;
        const ey = cy + Math.sin(a) * k;
        ctx.globalAlpha = 0.85;
        ctx.beginPath();
        ctx.moveTo(ex + Math.cos(a) * 8, ey + Math.sin(a) * 8);
        ctx.lineTo(ex + Math.cos(a + 2.4) * 7, ey + Math.sin(a + 2.4) * 7);
        ctx.lineTo(ex + Math.cos(a - 2.4) * 7, ey + Math.sin(a - 2.4) * 7);
        ctx.closePath();
        ctx.fill();
      }
      ctx.globalAlpha = 1;
    }
    if (hurtFlash > 0) {
      ctx.globalAlpha = hurtFlash * 0.35;
      ctx.strokeStyle = pal.red;
      ctx.lineWidth = 10;
      ctx.strokeRect(5, 5, view.w - 10, view.h - 10);
      hurtFlash = Math.max(0, hurtFlash - 0.08);
    }
    ctx.restore();
  }

  function draw(f, events) {
    clock++;
    if (events.length) absorb(f, events);
    layout(f);
    if (room === null || roomZoom !== view.zoom) paintRoom(f);
    const sx = shake > 0 ? (Math.random() - 0.5) * shake : 0;
    const sy = shake > 0 ? (Math.random() - 0.5) * shake : 0;
    shake = Math.max(0, shake - 1);
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.fillStyle = pal.paper;
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    const s = view.zoom * view.dpr;
    ctx.setTransform(s, 0, 0, s, (-view.camX + sx) * s, (-view.camY + sy) * s);
    ctx.save();
    ctx.setTransform(view.dpr, 0, 0, view.dpr, 0, 0);
    ctx.drawImage(
      room,
      (-view.camX + sx) * view.zoom,
      (-view.camY + sy) * view.zoom,
      room.width / view.dpr,
      room.height / view.dpr,
    );
    ctx.restore();
    drawMarks(f);
    for (const d of f.drops) drawDrop(d);
    for (const e of f.foes) drawFoe(e, f);
    drawPlayer(f);
    for (const sh of f.shots) drawShot(sh);
    drawBits();
    drawHud(f);
  }

  return {
    view,
    resize,
    draw,
    setLabels(next) {
      labels = next;
    },
    refreshPalette() {
      pal = readPalette(canvas);
      room = null;
    },
  };
}
