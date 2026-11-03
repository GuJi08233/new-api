// 在仓库根目录运行 bun pkg/doufa/testdata/generate_golden.mjs，用浏览器那份斗法规则跑几段机器人操作，记下操作记录与
// 最后的要点(含逐帧校验和)。Go 的 TestReplayMatchesBrowser 与前端的 engine.test.js 都按这份记录核对。
// 引擎里 import 了 JSON，node 不带导入属性读不了，所以用 bun 跑。
import { writeFileSync } from 'node:fs';
import {
  BTN_ATTACK,
  BTN_DODGE,
  BTN_SKILL,
  BTN_SWAP,
  MAX_TICKS,
  angleOf,
  content,
  createFight,
  encodeInputs,
  nearestFoe,
  pack,
  step,
  summary,
  trialSetup,
} from '../../../web/classic/src/pages/Games/wendao/doufa/engine.js';

// 机器人：躲飞弹(冷却好了就侧向冲刺，否则侧移)，按流派保持距离，一直攻击，敌人扎堆或有首领时放法术，远程灵力不够换近战。
export function makeBot(style, seed) {
  let s = seed >>> 0 || 1;
  const rnd = () => {
    s ^= s << 13;
    s ^= s >>> 17;
    s ^= s << 5;
    s >>>= 0;
    return s / 4294967296;
  };
  let lastX = -1;
  let lastY = -1;
  let wander = 0;
  let wanderDir = 0;
  let side = 1;
  let prevButtons = 0;
  return (f) => {
    const p = f.p;
    const target = nearestFoe(f);
    let move = -1;
    let aim = -1;
    let buttons = 0;
    let threat = null;
    let threatD = Infinity;
    for (const sh of f.shots) {
      if (!sh.hostile) continue;
      const dx = p.x - sh.x;
      const dy = p.y - sh.y;
      const d = dx * dx + dy * dy;
      if (d < 9000 * 9000 && dx * sh.vx + dy * sh.vy > 0 && d < threatD) {
        threat = sh;
        threatD = d;
      }
    }
    for (const e of f.foes) {
      const spec = content.foes[e.kind];
      if ((spec.ai === 'charge' && (e.state === 1 || e.state === 2)) || (spec.ai === 'boss' && e.state >= 3)) {
        const d = (p.x - e.x) ** 2 + (p.y - e.y) ** 2;
        if (d < 22000 * 22000 && d < threatD) {
          threat = { vx: Math.cos((e.a * Math.PI) / 32), vy: Math.sin((e.a * Math.PI) / 32) };
          threatD = d;
        }
      }
    }
    const stuck = p.x === lastX && p.y === lastY;
    lastX = p.x;
    lastY = p.y;
    if (wander > 0) {
      wander--;
      move = wanderDir;
    } else if (threat) {
      const across = (angleOf(Math.round(threat.vx * 1000), Math.round(threat.vy * 1000)) + 16 * side) & 63;
      move = across;
      if (p.dodgeCool === 0 && rnd() < style.dodge) buttons |= BTN_DODGE;
      if (rnd() < 0.02) side = -side;
    } else if (target) {
      const dx = target.x - p.x;
      const dy = target.y - p.y;
      const d = Math.sqrt(dx * dx + dy * dy) / 100;
      const a = angleOf(dx, dy);
      const range = content.weapons[p.weapons[p.slot]].kind === 'melee' ? 30 : style.range;
      if (d > range + 30) move = a;
      else if (d < range - 30) move = (a + 32) & 63;
      else move = (a + 16 * side) & 63;
      if (rnd() < 0.01) side = -side;
    } else if (f.drops.length > 0) {
      const d = f.drops[0];
      move = angleOf(d.x - p.x, d.y - p.y);
    }
    if (stuck && move >= 0 && rnd() < 0.3) {
      wander = 20 + Math.floor(rnd() * 20);
      wanderDir = Math.floor(rnd() * 64);
    }
    if (target) {
      buttons |= BTN_ATTACK;
      if (style.manualAim) aim = angleOf(target.x - p.x, target.y - p.y);
      const crowd = f.foes.filter((e) => (e.x - p.x) ** 2 + (e.y - p.y) ** 2 < 15000 * 15000).length;
      const bossNear = f.foes.some((e) => content.foes[e.kind].ai === 'boss');
      if (p.skillCool === 0 && (crowd >= 2 || bossNear) && !(prevButtons & BTN_SKILL)) buttons |= BTN_SKILL;
    }
    const weapon = content.weapons[p.weapons[p.slot]];
    const other = p.weapons.length > 1 ? content.weapons[p.weapons[1 - p.slot]] : null;
    const wantMelee = weapon.kind === 'shot' && p.mp < weapon.cost * 100 * 2;
    const wantRanged = weapon.kind === 'melee' && other && p.mp > p.maxMp * 0.7 && style.range > 60;
    if (other && (wantMelee || wantRanged) && !(prevButtons & BTN_SWAP)) buttons |= BTN_SWAP;
    prevButtons = buttons;
    return pack(move, aim, buttons);
  };
}

export function play(cls, seed, style, botSeed, limit = MAX_TICKS, setup = trialSetup(cls)) {
  const f = createFight(seed, setup);
  const bot = makeBot(style, botSeed);
  const inputs = [];
  while (!f.over && inputs.length < limit) {
    const input = bot(f);
    inputs.push(input);
    step(f, input);
  }
  return { f, inputs };
}

const runs = [
  { name: 'warrior-melee', cls: 'warrior', seed: 1, style: { range: 36, dodge: 0.6 }, botSeed: 7 },
  { name: 'mage-manual-aim', cls: 'mage', seed: 20261103, style: { range: 150, dodge: 0.7, manualAim: true }, botSeed: 3 },
  { name: 'ranger-unsigned-seed', cls: 'ranger', seed: 4294967295, style: { range: 140, dodge: 0.8 }, botSeed: 2 },
  { name: 'warrior-careless', cls: 'warrior', seed: 99, style: { range: 20, dodge: 0 }, botSeed: 17 },
  // 秘境斗法第二重的一间房：祝福全上、敌人加强、长枪与雷印(溅射)、护心挡第一下、赤炎魔君。
  {
    name: 'rift-floor-two',
    cls: 'mage',
    seed: 777,
    style: { range: 140, dodge: 0.8 },
    botSeed: 5,
    setup: {
      cls: 'mage',
      weapons: ['thunder', 'spear'],
      room: 'cavern',
      waves: [
        [{ foe: 'werewolf', n: 2 }, { foe: 'shade_mage', n: 1 }],
        [{ foe: 'beetle', n: 1 }, { foe: 'lava_elemental', n: 1 }, { foe: 'troll', n: 1 }],
        [{ foe: 'flame_lord', n: 1 }],
      ],
      hp: 90,
      mp: 100,
      mods: { damage: 20, speed: 12, haste: 18, pierce: 1, regen: 60, vigor: 25, dodge: 35, focus: 30, reach: 25, ward: 1 },
      scale: { hp: 130, dmg: 112 },
    },
  },
];

if (import.meta.main) {
  const records = runs.map(({ name, cls, seed, style, botSeed, setup = trialSetup(cls) }) => {
    const { f, inputs } = play(cls, seed, style, botSeed, MAX_TICKS, setup);
    return { name, seed, setup, inputs: encodeInputs(inputs), summary: summary(f) };
  });
  writeFileSync(new URL('./golden.json', import.meta.url), JSON.stringify(records, null, 1) + '\n');
  console.log(records.map(({ name, summary: s }) => ({ name, won: s.won, seconds: Math.round(s.tick / 60), hp: s.hp, kills: s.kills, taken: s.taken })));
}
