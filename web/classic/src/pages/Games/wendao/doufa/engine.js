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

// 斗法的规则，与 pkg/doufa(fight.go、ai.go)逐行对应：只用整数，60 帧一秒，64 个方向，32 位异或移位随机数，实体按固定
// 顺序更新。Go 的整数除法向零取整，这里一律用 div；位运算都加括号(JS 里 & 比减号优先级低，和 Go 不一样)。改这里的
// 任何一处都要同样改 Go，再跑 pkg/doufa/testdata/generate_golden.mjs 重新生成黄金回放。
import content from './content.json';

export const FPS = 60;
export const SCALE = 100;
export const TILE_PX = 32;
export const TILE = TILE_PX * SCALE;
export const MAX_TICKS = 5 * 60 * FPS;
export const DIRS = 64;
const TRIG = 1024;

export const BTN_ATTACK = 1;
export const BTN_SKILL = 2;
export const BTN_DODGE = 4;
export const BTN_SWAP = 8;

const MARK_TICKS = 50;
const AUTO_AIM_RANGE = 420;
const TOUCH_COOL = 30;
const HASH_START = 2166136261;
const HASH_PRIME = 16777619;
const SHOT_LIFE = 400;

const COS = [
  1024, 1019, 1004, 980, 946, 903, 851, 792, 724, 650, 569, 483, 392, 297, 200,
  100, 0, -100, -200, -297, -392, -483, -569, -650, -724, -792, -851, -903,
  -946, -980, -1004, -1019, -1024, -1019, -1004, -980, -946, -903, -851, -792,
  -724, -650, -569, -483, -392, -297, -200, -100, 0, 100, 200, 297, 392, 483,
  569, 650, 724, 792, 851, 903, 946, 980, 1004, 1019,
];

export { content };

export const div = (a, b) => Math.trunc(a / b);
const fdiv = (a, b) => Math.floor(a / b);
export const cosOf = (a) => COS[a & (DIRS - 1)];
export const sinOf = (a) => COS[(a + 48) & (DIRS - 1)];
const px = (v) => v * SCALE;
const pxs = (v) => div(v * SCALE, FPS);

export function angleOf(dx, dy) {
  let best = 0;
  let bestDot = -Infinity;
  for (let i = 0; i < DIRS; i++) {
    const d = dx * COS[i] + dy * COS[(i + 48) & (DIRS - 1)];
    if (d > bestDot) {
      best = i;
      bestDot = d;
    }
  }
  return best;
}

const angleGap = (a, b) => {
  let d = (a - b) & (DIRS - 1);
  if (d > DIRS / 2) d = DIRS - d;
  return d;
};

const near = (ax, ay, bx, by, d) => {
  const dx = ax - bx;
  const dy = ay - by;
  return dx * dx + dy * dy <= d * d;
};

const dist2 = (ax, ay, bx, by) => {
  const dx = ax - bx;
  const dy = ay - by;
  return dx * dx + dy * dy;
};

// pack 把一帧的操作压成一个整数：移动方向(-1 不动)、瞄准方向(-1 自动瞄最近的敌人)与按住的键。
export const pack = (move, aim, buttons) =>
  (move + 1) | ((aim + 1) << 7) | ((buttons & 15) << 14);

// MODS 是一轮里祝福叠出来的加成，全是 0 就是没有加成(与 Go 的 Mods 同义)。
const MODS = {
  damage: 0,
  speed: 0,
  haste: 0,
  pierce: 0,
  regen: 0,
  vigor: 0,
  dodge: 0,
  focus: 0,
  reach: 0,
  ward: 0,
};

const modsValid = (m) =>
  [m.damage, m.speed, m.regen, m.reach].every((v) => v >= 0 && v <= 300) &&
  [m.haste, m.dodge, m.focus].every((v) => v >= 0 && v <= 80) &&
  m.pierce >= 0 &&
  m.pierce <= 5 &&
  m.vigor >= 0 &&
  m.vigor <= 500 &&
  m.ward >= 0 &&
  m.ward <= 5;

const unpack = (input) => [
  (input & 127) - 1,
  ((input >> 7) & 127) - 1,
  (input >> 14) & 15,
];

// trialSetup 是试剑台的那一间房。
export function trialSetup(cls) {
  const spec = content.classes[cls];
  return {
    cls,
    weapons: spec ? spec.weapons.slice() : [],
    room: content.trial.room,
    waves: content.trial.waves,
    hp: 0,
    mp: 0,
  };
}

// createFight 开一间房，setup 不合法时返回 null。
export function createFight(seed, setup) {
  const cls = content.classes[setup.cls];
  const rows = content.rooms[setup.room];
  const mods = { ...MODS, ...(setup.mods || {}) };
  const scale = setup.scale || {};
  const foeHp = scale.hp || 0;
  const foeDmg = scale.dmg || 0;
  if (
    !cls ||
    !rows ||
    setup.weapons.length < 1 ||
    setup.weapons.length > 2 ||
    setup.waves.length < 1 ||
    !modsValid(mods) ||
    foeHp < 0 ||
    foeHp > 1000 ||
    foeDmg < 0 ||
    foeDmg > 1000
  ) {
    return null;
  }
  if (setup.weapons.some((w) => !content.weapons[w])) return null;
  for (const wave of setup.waves) {
    if (wave.length < 1) return null;
    for (const g of wave) {
      if (!content.foes[g.foe] || g.n < 1 || g.n > 12) return null;
    }
  }
  const f = {
    tick: 0,
    rng: seed === 0 ? 1 : seed >>> 0,
    h: rows.length,
    w: rows[0].length,
    solid: [],
    rocks: [],
    bossX: 0,
    bossY: 0,
    p: null,
    foes: [],
    shots: [],
    drops: [],
    room: setup.room,
    waves: setup.waves,
    wave: 0,
    marks: [],
    markT: 0,
    nextId: 1,
    kills: 0,
    stones: 0,
    taken: 0,
    dealt: 0,
    prev: 0,
    over: false,
    won: false,
    hash: HASH_START,
    events: [],
    mods,
    foeHp: foeHp > 0 ? foeHp : 100,
    foeDmg: foeDmg > 0 ? foeDmg : 100,
  };
  let px0 = -1;
  let py0 = -1;
  for (let y = 0; y < rows.length; y++) {
    const row = rows[y];
    if (row.length !== f.w) return null;
    for (let x = 0; x < f.w; x++) {
      const c = row[x];
      f.solid.push(c === '#' || c === 'o');
      f.rocks.push(c === 'o');
      if (c === 'P') {
        px0 = x;
        py0 = y;
      } else if (c === 'B') {
        f.bossX = x * TILE + TILE / 2;
        f.bossY = y * TILE + TILE / 2;
      }
    }
  }
  if (px0 < 0) return null;
  const maxHp = cls.hp + mods.vigor;
  f.p = {
    x: px0 * TILE + TILE / 2,
    y: py0 * TILE + TILE / 2,
    r: px(content.player.radius),
    hp: maxHp,
    maxHp,
    mp: cls.mp * 100,
    maxMp: cls.mp * 100,
    speed: div(pxs(cls.speed) * (100 + mods.speed), 100),
    face: 48,
    cls: setup.cls,
    weapons: setup.weapons.slice(),
    slot: 0,
    cool: 0,
    skillCool: 0,
    dodgeCool: 0,
    dash: 0,
    dashDir: 0,
    hurt: 0,
    swing: 0,
    blades: 0,
    spin: 0,
    regen: div(
      div(content.player.mp_regen * 100, FPS) * (100 + mods.regen),
      100,
    ),
    ward: mods.ward,
  };
  if (setup.hp > 0 && setup.hp < maxHp) f.p.hp = setup.hp;
  if (setup.mp > 0 && setup.mp < cls.mp) f.p.mp = setup.mp * 100;
  markWave(f);
  return f;
}

function rand(f, n) {
  let s = f.rng;
  s ^= s << 13;
  s ^= s >>> 17;
  s ^= s << 5;
  s >>>= 0;
  f.rng = s;
  return s % n;
}

const ev = (f, k, x, y, n) => f.events.push({ k, x, y, n });

function solidAt(f, tx, ty) {
  if (tx < 0 || ty < 0 || tx >= f.w || ty >= f.h) return true;
  return f.solid[ty * f.w + tx];
}

export function blocked(f, x, y, r) {
  for (let ty = fdiv(y - r, TILE); ty <= fdiv(y + r, TILE); ty++) {
    for (let tx = fdiv(x - r, TILE); tx <= fdiv(x + r, TILE); tx++) {
      if (!solidAt(f, tx, ty)) continue;
      const cx = Math.min(Math.max(x, tx * TILE), tx * TILE + TILE);
      const cy = Math.min(Math.max(y, ty * TILE), ty * TILE + TILE);
      if (near(x, y, cx, cy, r - 1)) return true;
    }
  }
  return false;
}

function move(f, x, y, r, dx, dy) {
  let hit = false;
  for (let i = 0, step = dx; i < 3 && step !== 0; i++, step = div(step, 2)) {
    if (!blocked(f, x + step, y, r)) {
      x += step;
      break;
    }
    hit = true;
  }
  for (let i = 0, step = dy; i < 3 && step !== 0; i++, step = div(step, 2)) {
    if (!blocked(f, x, y + step, r)) {
      y += step;
      break;
    }
    hit = true;
  }
  return [x, y, hit];
}

function markWave(f) {
  if (f.wave >= f.waves.length) return;
  f.marks = [];
  for (const g of f.waves[f.wave]) {
    for (let n = 0; n < g.n; n++) {
      if (content.foes[g.foe].ai === 'boss') {
        f.marks.push({ kind: g.foe, x: f.bossX, y: f.bossY });
        continue;
      }
      const [x, y] = freeSpot(f);
      f.marks.push({ kind: g.foe, x, y });
    }
  }
  f.markT = MARK_TICKS;
}

function freeSpot(f) {
  let x = f.p.x;
  let y = f.p.y;
  for (let tries = 0; tries < 200; tries++) {
    const tx = 1 + rand(f, f.w - 2);
    const ty = 1 + rand(f, f.h - 2);
    if (solidAt(f, tx, ty)) continue;
    x = tx * TILE + TILE / 2;
    y = ty * TILE + TILE / 2;
    if (near(x, y, f.p.x, f.p.y, 5 * TILE)) continue;
    let crowded = false;
    for (const m of f.marks) {
      if (near(x, y, m.x, m.y, div(TILE * 3, 2))) {
        crowded = true;
        break;
      }
    }
    if (!crowded) return [x, y];
  }
  return [x, y];
}

function spawn(f, kind, x, y) {
  const spec = content.foes[kind];
  const hp = Math.max(1, div(spec.hp * f.foeHp, 100));
  const e = {
    id: f.nextId,
    kind,
    x,
    y,
    r: px(spec.radius),
    hp,
    maxHp: hp,
    state: 0,
    t: 0,
    a: 0,
    n: 0,
    cool: 0,
    touch: 0,
    bladeHit: 0,
    kx: 0,
    ky: 0,
    flash: 0,
    phase: 0,
    pat: 0,
  };
  f.nextId++;
  e.n = e.id % 2;
  e.cool = 40 + rand(f, 60);
  f.foes.push(e);
  ev(f, 'spawn', x, y, 0);
  return e;
}

// step 走一帧；打完以后什么都不做。
export function step(f, input) {
  if (f.over) return;
  f.events = [];
  f.tick++;
  stepPlayer(f, input);
  const n = f.foes.length;
  for (let i = 0; i < n; i++) {
    const e = f.foes[i];
    if (e.hp > 0) stepFoe(f, e);
  }
  stepShots(f);
  stepDrops(f);
  reap(f);
  if (f.markT > 0) {
    f.markT--;
    if (f.markT === 0) {
      for (const m of f.marks) spawn(f, m.kind, m.x, m.y);
      f.marks = [];
      f.wave++;
    }
  } else if (f.foes.length === 0) {
    if (f.wave < f.waves.length) {
      markWave(f);
    } else {
      for (const d of f.drops) pick(f, d);
      f.drops = [];
      f.won = true;
      f.over = true;
    }
  }
  if (f.p.hp <= 0 || f.tick >= MAX_TICKS) f.over = true;
  mixHash(f);
}

function mixHash(f) {
  let h = f.hash;
  const mix = (v) => {
    h = Math.imul(h ^ v, HASH_PRIME) >>> 0;
  };
  mix(f.tick);
  mix(f.p.x);
  mix(f.p.y);
  mix(f.p.hp);
  mix(f.p.mp);
  for (const e of f.foes) {
    mix(e.x);
    mix(e.y);
    mix(e.hp);
  }
  mix(f.shots.length);
  f.hash = h;
}

function stepPlayer(f, input) {
  const p = f.p;
  const [moveDir, aim, buttons] = unpack(input);
  const pressed = buttons & ~f.prev;
  f.prev = buttons;
  if (p.cool > 0) p.cool--;
  if (p.skillCool > 0) p.skillCool--;
  if (p.dodgeCool > 0) p.dodgeCool--;
  if (p.hurt > 0) p.hurt--;
  if (p.swing > 0) p.swing--;
  p.mp = Math.min(p.maxMp, p.mp + p.regen);
  if (aim >= 0) {
    p.face = aim;
  } else {
    const e = nearestFoe(f);
    if (e) p.face = angleOf(e.x - p.x, e.y - p.y);
    else if (moveDir >= 0) p.face = moveDir;
  }
  const dodge = content.player.dodge;
  if ((pressed & BTN_DODGE) !== 0 && p.dodgeCool === 0 && p.dash === 0) {
    p.dash = dodge.ticks;
    p.dashDir = p.face;
    p.dodgeCool = div(dodge.cooldown * (100 - f.mods.dodge), 100);
    if (moveDir >= 0) p.dashDir = moveDir;
    ev(f, 'dodge', p.x, p.y, p.dashDir);
  }
  if (p.dash > 0) {
    const sp = pxs(dodge.speed);
    [p.x, p.y] = move(
      f,
      p.x,
      p.y,
      p.r,
      div(sp * cosOf(p.dashDir), TRIG),
      div(sp * sinOf(p.dashDir), TRIG),
    );
    p.dash--;
  } else if (moveDir >= 0) {
    [p.x, p.y] = move(
      f,
      p.x,
      p.y,
      p.r,
      div(p.speed * cosOf(moveDir), TRIG),
      div(p.speed * sinOf(moveDir), TRIG),
    );
  }
  if ((pressed & BTN_SWAP) !== 0 && p.weapons.length > 1) {
    p.slot = 1 - p.slot;
    p.cool = Math.max(p.cool, 8);
    ev(f, 'swap', p.x, p.y, p.slot);
  }
  if ((buttons & BTN_ATTACK) !== 0 && p.cool === 0 && p.dash === 0) fire(f);
  const skill = content.classes[p.cls].skill;
  if ((pressed & BTN_SKILL) !== 0 && p.skillCool === 0 && p.dash === 0) {
    if (p.mp >= skill.cost * 100) cast(f, skill);
    else ev(f, 'dry', p.x, p.y, 0);
  }
  if (p.blades > 0) stepBlades(f, skill);
}

export function nearestFoe(f) {
  let best = null;
  let bestD = px(AUTO_AIM_RANGE) * px(AUTO_AIM_RANGE);
  for (const e of f.foes) {
    if (e.hp <= 0) continue;
    const d = dist2(e.x, e.y, f.p.x, f.p.y);
    if (d < bestD) {
      best = e;
      bestD = d;
    }
  }
  return best;
}

function fire(f) {
  const p = f.p;
  const w = content.weapons[p.weapons[p.slot]];
  const dmg = div(w.damage * (100 + f.mods.damage), 100);
  const cool = Math.max(4, div(w.cooldown * (100 - f.mods.haste), 100));
  if (w.kind === 'melee') {
    p.cool = cool;
    p.swing = 10;
    ev(f, 'swing', p.x, p.y, p.face);
    const reach = div(px(w.range) * (100 + f.mods.reach), 100);
    for (const e of f.foes) {
      if (e.hp <= 0 || !near(e.x, e.y, p.x, p.y, reach + e.r)) continue;
      const a = angleOf(e.x - p.x, e.y - p.y);
      if (angleGap(a, p.face) <= w.arc || near(e.x, e.y, p.x, p.y, p.r + e.r)) {
        hitFoe(f, e, dmg, a, w.knock);
      }
    }
    const kept = [];
    for (const s of f.shots) {
      if (
        s.hostile &&
        near(s.x, s.y, p.x, p.y, reach + s.r) &&
        angleGap(angleOf(s.x - p.x, s.y - p.y), p.face) <= w.arc
      ) {
        ev(f, 'deflect', s.x, s.y, 0);
        continue;
      }
      kept.push(s);
    }
    f.shots = kept;
    return;
  }
  if (p.mp < w.cost * 100) {
    p.cool = 10;
    ev(f, 'dry', p.x, p.y, 0);
    return;
  }
  p.mp -= w.cost * 100;
  p.cool = cool;
  const kind = p.weapons[p.slot];
  for (let i = 0; i < w.count; i++) {
    const a =
      (p.face + div((2 * i - (w.count - 1)) * w.spread, 2)) & (DIRS - 1);
    shoot(
      f,
      p.x,
      p.y,
      p.r,
      a,
      w.speed,
      w.radius,
      dmg,
      w.life,
      w.pierce + f.mods.pierce,
      false,
      kind,
    ).splash = px(w.splash || 0);
  }
  ev(f, 'shoot', p.x, p.y, p.face);
}

function shoot(f, x, y, r, a, speed, radius, dmg, life, pierce, hostile, kind) {
  const sp = pxs(speed);
  const off = r + px(2);
  const shot = {
    id: f.nextId,
    x: x + div(off * cosOf(a), TRIG),
    y: y + div(off * sinOf(a), TRIG),
    vx: div(sp * cosOf(a), TRIG),
    vy: div(sp * sinOf(a), TRIG),
    r: px(radius),
    a,
    dmg,
    hostile,
    life,
    pierce,
    hit: [],
    kind,
    splash: 0,
  };
  f.shots.push(shot);
  f.nextId++;
  return shot;
}

function cast(f, s) {
  const p = f.p;
  p.mp -= s.cost * 100;
  p.skillCool = div(s.cooldown * (100 - f.mods.focus), 100);
  const dmg = div(s.damage * (100 + f.mods.damage), 100);
  ev(f, 'cast', p.x, p.y, 0);
  if (s.kind === 'quake') {
    const reach = px(s.radius);
    for (const e of f.foes) {
      if (e.hp > 0 && near(e.x, e.y, p.x, p.y, reach + e.r)) {
        hitFoe(f, e, dmg, angleOf(e.x - p.x, e.y - p.y), s.knock);
      }
    }
    f.shots = f.shots.filter(
      (sh) => !(sh.hostile && near(sh.x, sh.y, p.x, p.y, reach)),
    );
    p.hurt = Math.max(p.hurt, 20);
  } else if (s.kind === 'nova') {
    for (let i = 0; i < s.count; i++) {
      shoot(
        f,
        p.x,
        p.y,
        p.r,
        div(i * DIRS, s.count),
        s.speed,
        s.radius,
        dmg,
        s.life,
        0,
        false,
        'fire',
      );
    }
  } else if (s.kind === 'blades') {
    p.blades = s.ticks;
  }
}

// bladePoints 是剑阵几把飞剑现在的位置(画面也用)。
export function bladePoints(f) {
  const p = f.p;
  const s = content.classes[p.cls].skill;
  const orbit = px(s.orbit || 0);
  const points = [];
  for (let k = 0; k < (s.count || 0); k++) {
    const a = (p.spin + div(k * DIRS, s.count)) & (DIRS - 1);
    points.push([
      p.x + div(orbit * cosOf(a), TRIG),
      p.y + div(orbit * sinOf(a), TRIG),
      a,
    ]);
  }
  return points;
}

function stepBlades(f, s) {
  const p = f.p;
  p.blades--;
  p.spin = (p.spin + 2) & (DIRS - 1);
  const orbit = px(s.orbit);
  const br = px(s.radius);
  const dmg = div(s.damage * (100 + f.mods.damage), 100);
  for (let k = 0; k < s.count; k++) {
    const a = (p.spin + div(k * DIRS, s.count)) & (DIRS - 1);
    const bx = p.x + div(orbit * cosOf(a), TRIG);
    const by = p.y + div(orbit * sinOf(a), TRIG);
    for (const e of f.foes) {
      if (e.hp > 0 && e.bladeHit === 0 && near(bx, by, e.x, e.y, br + e.r)) {
        hitFoe(f, e, dmg, a, 0);
        e.bladeHit = s.rehit;
      }
    }
    f.shots = f.shots.filter(
      (sh) => !(sh.hostile && near(sh.x, sh.y, bx, by, br + sh.r)),
    );
  }
}

function hitFoe(f, e, dmg, a, knock) {
  if (e.hp <= 0) return;
  e.hp -= dmg;
  f.dealt += dmg;
  e.flash = 6;
  ev(f, 'hit', e.x, e.y, dmg);
  const spec = content.foes[e.kind];
  if (knock > 0 && spec.ai !== 'boss' && spec.ai !== 'turret') {
    e.kx = div(pxs(knock) * cosOf(a), TRIG);
    e.ky = div(pxs(knock) * sinOf(a), TRIG);
  }
}

function hurt(f, dmg) {
  const p = f.p;
  if (p.hurt > 0 || p.dash > 0) return false;
  if (p.ward > 0) {
    p.ward--;
    p.hurt = content.player.iframes;
    ev(f, 'ward', p.x, p.y, 0);
    return true;
  }
  p.hp = Math.max(p.hp - dmg, 0);
  f.taken += dmg;
  p.hurt = content.player.iframes;
  ev(f, 'hurt', p.x, p.y, dmg);
  return true;
}

function stepShots(f) {
  const p = f.p;
  const kept = [];
  for (const s of f.shots) {
    s.x += s.vx;
    s.y += s.vy;
    s.life--;
    if (s.life <= 0 || solidAt(f, fdiv(s.x, TILE), fdiv(s.y, TILE))) {
      ev(f, 'fizzle', s.x, s.y, 0);
      continue;
    }
    if (s.hostile) {
      if (near(s.x, s.y, p.x, p.y, s.r + p.r - px(2)) && hurt(f, s.dmg))
        continue;
      kept.push(s);
      continue;
    }
    let alive = true;
    for (const e of f.foes) {
      if (
        e.hp <= 0 ||
        !near(s.x, s.y, e.x, e.y, s.r + e.r) ||
        s.hit.includes(e.id)
      ) {
        continue;
      }
      hitFoe(f, e, s.dmg, s.a, 0);
      if (s.splash > 0) {
        for (const o of f.foes) {
          if (o !== e && o.hp > 0 && near(s.x, s.y, o.x, o.y, s.splash + o.r)) {
            hitFoe(f, o, div(s.dmg, 2), s.a, 0);
          }
        }
        ev(f, 'blast', s.x, s.y, 0);
      }
      if (s.pierce <= 0) {
        alive = false;
        break;
      }
      s.pierce--;
      s.hit.push(e.id);
    }
    if (alive) kept.push(s);
  }
  f.shots = kept;
}

function stepDrops(f) {
  const reach = f.p.r + px(content.player.pickup);
  const kept = [];
  for (const d of f.drops) {
    if (near(d.x, d.y, f.p.x, f.p.y, reach)) {
      pick(f, d);
      continue;
    }
    kept.push(d);
  }
  f.drops = kept;
}

function pick(f, d) {
  const p = f.p;
  if (d.kind === 'stone') f.stones += d.n;
  else if (d.kind === 'heal') p.hp = Math.min(p.maxHp, p.hp + d.n);
  else if (d.kind === 'mana') p.mp = Math.min(p.maxMp, p.mp + d.n * 100);
  ev(f, 'pick', d.x, d.y, d.n);
}

function reap(f) {
  const kept = [];
  for (const e of f.foes) {
    if (e.hp > 0) {
      kept.push(e);
      continue;
    }
    f.kills++;
    ev(f, 'die', e.x, e.y, 0);
    if (content.foes[e.kind].ai === 'boss') {
      drop(f, 'stone', e.x, e.y, 10);
      continue;
    }
    const r = rand(f, 100);
    if (r < 35) drop(f, 'stone', e.x, e.y, 1 + rand(f, 2));
    else if (r < 45) drop(f, 'heal', e.x, e.y, 12);
    else if (r < 58) drop(f, 'mana', e.x, e.y, 20);
  }
  f.foes = kept;
}

function drop(f, kind, x, y, n) {
  f.drops.push({ id: f.nextId, kind, x, y, n });
  f.nextId++;
}

// ---------- 敌人(对应 ai.go) ----------

function stepFoe(f, e) {
  const spec = content.foes[e.kind];
  if (e.touch > 0) e.touch--;
  if (e.bladeHit > 0) e.bladeHit--;
  if (e.flash > 0) e.flash--;
  if (e.kx !== 0 || e.ky !== 0) {
    [e.x, e.y] = move(f, e.x, e.y, e.r, e.kx, e.ky);
    e.kx = div(e.kx * 7, 10);
    e.ky = div(e.ky * 7, 10);
    if (Math.abs(e.kx) < 20 && Math.abs(e.ky) < 20) {
      e.kx = 0;
      e.ky = 0;
    }
    touch(f, e, spec);
    return;
  }
  if (spec.ai === 'chase') {
    chaser(f, e, spec);
  } else if (spec.ai === 'shoot') {
    shooter(f, e, spec);
  } else if (spec.ai === 'charge') {
    charger(f, e, spec);
  } else if (spec.ai === 'turret') {
    turret(f, e, spec);
  } else if (spec.ai === 'boss') {
    boss(f, e, spec);
  }
  touch(f, e, spec);
}

function touch(f, e, spec) {
  if (
    e.touch === 0 &&
    spec.touch > 0 &&
    near(e.x, e.y, f.p.x, f.p.y, e.r + f.p.r) &&
    hurt(f, div(spec.touch * f.foeDmg, 100))
  ) {
    e.touch = TOUCH_COOL;
  }
}

function chaser(f, e, spec) {
  const p = f.p;
  if (near(e.x, e.y, p.x, p.y, e.r + p.r)) return;
  let a = angleOf(p.x - e.x, p.y - e.y);
  if (e.t > 0) {
    e.t--;
    a = (a + 16 - 32 * e.n) & (DIRS - 1);
  }
  const x = e.x;
  const y = e.y;
  const sp = pxs(spec.speed);
  if (
    walk(f, e, a, sp) &&
    Math.abs(e.x - x) + Math.abs(e.y - y) < div(sp, 2) &&
    e.t === 0
  ) {
    e.t = 30;
    e.n ^= 1;
  }
}

function walk(f, e, a, sp) {
  const [x, y, hit] = move(
    f,
    e.x,
    e.y,
    e.r,
    div(sp * cosOf(a), TRIG),
    div(sp * sinOf(a), TRIG),
  );
  e.x = x;
  e.y = y;
  return hit;
}

function volley(f, e, s, a) {
  for (let i = 0; i < s.count; i++) {
    const dir = (a + div((2 * i - (s.count - 1)) * s.spread, 2)) & (DIRS - 1);
    shoot(
      f,
      e.x,
      e.y,
      e.r,
      dir,
      s.speed,
      s.radius,
      div(s.damage * f.foeDmg, 100),
      SHOT_LIFE,
      0,
      true,
      e.kind,
    );
  }
}

function ring(f, e, count, offset, speed, radius, dmg) {
  for (let i = 0; i < count; i++) {
    const dir = (offset + div(i * DIRS, count)) & (DIRS - 1);
    shoot(
      f,
      e.x,
      e.y,
      e.r,
      dir,
      speed,
      radius,
      div(dmg * f.foeDmg, 100),
      SHOT_LIFE,
      0,
      true,
      e.kind,
    );
  }
}

function shooter(f, e, spec) {
  const p = f.p;
  if (e.state === 1) {
    e.t--;
    if (e.t <= 0) {
      volley(f, e, spec.shot, e.a);
      e.state = 0;
      e.cool = spec.cooldown + rand(f, 40);
    }
    return;
  }
  if (e.cool > 0) e.cool--;
  const a = angleOf(p.x - e.x, p.y - e.y);
  const d2 = dist2(p.x, p.y, e.x, e.y);
  const reach = px(spec.range);
  const far = reach + px(30);
  const close = reach - px(60);
  let dir = (a + 16 - 32 * e.n) & (DIRS - 1);
  if (d2 > far * far) dir = a;
  else if (d2 < close * close) dir = (a + 32) & (DIRS - 1);
  if (walk(f, e, dir, pxs(spec.speed)) && dir !== a) e.n ^= 1;
  const alert = reach + px(120);
  if (e.cool === 0 && d2 < alert * alert) {
    e.state = 1;
    e.t = spec.windup;
    e.a = a;
  }
}

function charger(f, e, spec) {
  const p = f.p;
  if (e.state === 0) {
    if (e.cool > 0) e.cool--;
    const a = angleOf(p.x - e.x, p.y - e.y);
    if (!near(e.x, e.y, p.x, p.y, e.r + p.r)) walk(f, e, a, pxs(spec.speed));
    if (e.cool === 0 && near(e.x, e.y, p.x, p.y, px(spec.range))) {
      e.state = 1;
      e.t = spec.windup;
      e.a = a;
    }
  } else if (e.state === 1) {
    e.t--;
    if (e.t <= 0) {
      e.state = 2;
      e.t = spec.dash_ticks;
    }
  } else if (e.state === 2) {
    const hit = walk(f, e, e.a, pxs(spec.dash));
    e.t--;
    if (hit || e.t <= 0) {
      e.state = 3;
      e.t = spec.rest;
    }
  } else {
    e.t--;
    if (e.t <= 0) {
      e.state = 0;
      e.cool = 50;
    }
  }
}

function turret(f, e, spec) {
  if (e.cool > 0) {
    e.cool--;
    return;
  }
  ring(
    f,
    e,
    spec.shot.count,
    e.a,
    spec.shot.speed,
    spec.shot.radius,
    spec.shot.damage,
  );
  e.a = (e.a + spec.spin) & (DIRS - 1);
  e.cool = spec.cooldown;
}

function boss(f, e, spec) {
  const b = spec.boss;
  const p = f.p;
  if (e.phase === 0 && e.hp * 2 <= e.maxHp) {
    e.phase = 1;
    ev(f, 'phase', e.x, e.y, 1);
  }
  let gap = b.gap;
  let spray = b.spray.count;
  let ringN = b.ring.count;
  if (e.phase > 0) {
    gap = b.gap2;
    spray = b.spray.count2;
    ringN = b.ring.count2;
  }
  const a = angleOf(p.x - e.x, p.y - e.y);
  if (e.state === 0) {
    if (!near(e.x, e.y, p.x, p.y, e.r + p.r + px(40)))
      walk(f, e, a, pxs(spec.speed));
    if (e.cool > 0) {
      e.cool--;
      return;
    }
    if (e.pat === 0) {
      e.state = 1;
      e.n = b.spray.volleys;
      e.t = 1;
    } else if (e.pat === 1) {
      e.state = 2;
      e.n = b.ring.volleys;
      e.t = 1;
    } else if (e.pat === 2) {
      summon(f, e, b);
      e.cool = gap;
    } else {
      e.state = 3;
      e.t = b.charge.windup;
      e.a = a;
    }
    e.pat = (e.pat + 1) % 4;
  } else if (e.state === 1) {
    e.t--;
    if (e.t > 0) return;
    volley(
      f,
      e,
      {
        damage: b.spray.damage,
        speed: b.spray.speed,
        radius: b.spray.radius,
        count: spray,
        spread: b.spray.spread,
      },
      a,
    );
    e.n--;
    if (e.n > 0) e.t = b.spray.every;
    else {
      e.state = 0;
      e.cool = gap;
    }
  } else if (e.state === 2) {
    e.t--;
    if (e.t > 0) return;
    ring(f, e, ringN, e.a, b.ring.speed, b.ring.radius, b.ring.damage);
    e.a = (e.a + b.ring.turn) & (DIRS - 1);
    e.n--;
    if (e.n > 0) e.t = b.ring.every;
    else {
      e.state = 0;
      e.cool = gap;
    }
  } else if (e.state === 3) {
    e.t--;
    if (e.t <= 0) {
      e.state = 4;
      e.t = b.charge.ticks;
    }
  } else {
    const hit = walk(f, e, e.a, pxs(b.charge.dash));
    e.t--;
    if (hit || e.t <= 0) {
      e.state = 0;
      e.cool = gap;
    }
  }
}

function summon(f, e, b) {
  const r = px(content.foes[b.summon.kind].radius);
  for (let i = 0; i < b.summon.count && f.foes.length < b.summon.cap; i++) {
    const start = rand(f, DIRS);
    for (let k = 0; k < 8; k++) {
      const a = (start + k * 8) & (DIRS - 1);
      const d = e.r + r + px(8);
      const x = e.x + div(d * cosOf(a), TRIG);
      const y = e.y + div(d * sinOf(a), TRIG);
      if (!blocked(f, x, y, r)) {
        spawn(f, b.summon.kind, x, y).cool = 40;
        break;
      }
    }
  }
}

// encodeInputs 与 Go 的 EncodeInputs 写出同样的字符串：相邻相同的输入合成一段，(输入值, 帧数)各写成无符号变长整数，
// 整串 base64url 不补等号。
export function encodeInputs(inputs) {
  const bytes = [];
  const put = (v) => {
    while (v >= 0x80) {
      bytes.push((v & 0x7f) | 0x80);
      v = Math.floor(v / 128);
    }
    bytes.push(v);
  };
  for (let i = 0; i < inputs.length; ) {
    let j = i + 1;
    while (j < inputs.length && inputs[j] === inputs[i]) j++;
    put(inputs[i]);
    put(j - i);
    i = j;
  }
  let binary = '';
  for (let i = 0; i < bytes.length; i += 4096) {
    binary += String.fromCharCode(...bytes.slice(i, i + 4096));
  }
  return btoa(binary)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
}

// decodeInputs 还原逐帧输入(测试用；格式不对时返回 null)。
export function decodeInputs(text) {
  const binary = atob(text.replace(/-/g, '+').replace(/_/g, '/'));
  const inputs = [];
  let pos = 0;
  const get = () => {
    let v = 0;
    let mul = 1;
    while (pos < binary.length) {
      const b = binary.charCodeAt(pos++);
      v += (b & 0x7f) * mul;
      if (b < 0x80) return v;
      mul *= 128;
    }
    return -1;
  };
  while (pos < binary.length) {
    const value = get();
    const run = get();
    if (value < 0 || run < 1) return null;
    for (let k = 0; k < run; k++) inputs.push(value);
  }
  return inputs;
}

// summary 是黄金回放核对的要点，与 Go 的 Summary 同名同义。
export function summary(f) {
  return {
    tick: f.tick,
    hp: f.p.hp,
    mp: f.p.mp,
    x: f.p.x,
    y: f.p.y,
    kills: f.kills,
    stones: f.stones,
    taken: f.taken,
    dealt: f.dealt,
    wave: f.wave,
    foes: f.foes.length,
    over: f.over,
    won: f.won,
    hash: f.hash,
  };
}
