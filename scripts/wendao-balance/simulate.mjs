// 在仓库根目录运行 bun scripts/wendao-balance/simulate.mjs [结果.csv] [模式...]，模式是 chapter、steps、weekly、rift，不给时全跑。
// 数值来自 Go 的真实 JourneySetup 与秘境的 Run；操作策略复用黄金回放机器人，补上射击被障碍挡住时的绕行。
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { makeBot } from '../../pkg/doufa/testdata/generate_golden.mjs';
import {
  angleOf,
  content,
  createFight,
  nearestFoe,
  pack,
  step,
} from '../../web/classic/src/pages/Games/wendao/doufa/engine.js';

const modes = ['chapter', 'steps', 'weekly', 'rift'];
const args = process.argv.slice(2);
const wanted = args.filter((arg) => modes.includes(arg));
const csvArg = args.find((arg) => !modes.includes(arg));
const scenarios = JSON.parse(
  execFileSync('go', ['run', './scripts/wendao-balance', ...wanted], {
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  }),
);
const seeds = [1, 7, 19, 41, 97, 211, 509, 1021, 4093, 8191];
const rows = [];
for (const scenario of scenarios) {
  let wins = 0;
  let timedOut = 0;
  let seconds = 0;
  let winSeconds = 0;
  let hp = 0;
  for (const seed of seeds) {
    const f = createFight(seed, scenario.setup);
    const bot = makeBot(
      {
        range: scenario.class === 'warrior' ? 36 : 140,
        dodge: 0.8,
        manualAim: true,
      },
      seed + 17,
    );
    let oldHealth = -1;
    let idle = 0;
    let flank = 0;
    while (!f.over) {
      let input = bot(f);
      const health = f.foes.reduce((sum, foe) => sum + foe.hp, 0);
      idle = health === oldHealth && f.foes.length ? idle + 1 : 0;
      oldHealth = health;
      if (idle >= 120) {
        flank = 100;
        idle = 0;
      }
      const target = nearestFoe(f);
      if (flank > 0 && target) {
        const aim = angleOf(target.x - f.p.x, target.y - f.p.y);
        input = pack(
          (aim + (seed % 2 ? 16 : 48)) & 63,
          aim,
          (input >> 14) & 15,
        );
        flank--;
      }
      step(f, input);
    }
    if (f.won) {
      wins++;
      winSeconds += f.tick / 60;
    }
    if (f.tick >= 18000) timedOut++;
    seconds += f.tick / 60;
    hp += f.p.hp;
  }
  rows.push({
    id: scenario.id,
    mode: scenario.mode,
    chapter: scenario.chapter,
    step: scenario.step || 0,
    floor: scenario.floor || 0,
    variant: scenario.variant || '',
    kind: scenario.kind || '',
    training: scenario.training,
    class: scenario.class,
    wins,
    attempts: seeds.length,
    timed_out: timedOut,
    mean_seconds: +(seconds / seeds.length).toFixed(1),
    win_seconds: wins ? +(winSeconds / wins).toFixed(1) : 0,
    mean_hp: +(hp / seeds.length).toFixed(1),
    hp_scale: scenario.setup.scale.hp,
    dmg_scale: scenario.setup.scale.dmg,
    vigor: scenario.setup.mods.vigor || 0,
    damage: scenario.setup.mods.damage || 0,
    ward: scenario.setup.mods.ward || 0,
  });
}
const path = resolve(csvArg || join(tmpdir(), 'wendao-balance.csv'));
const keys = Object.keys(rows[0]);
writeFileSync(
  path,
  keys.join(',') +
    '\n' +
    rows.map((row) => keys.map((key) => row[key]).join(',')).join('\n') +
    '\n',
);
console.log(
  `Wrote ${rows.length} scenarios × ${seeds.length} fixed seeds to ${path}`,
);
for (const cls of Object.keys(content.classes)) {
  for (const training of ['base', 'trained', 'graduate']) {
    const chapter = rows.filter(
      (r) => r.mode === 'chapter' && r.class === cls && r.training === training,
    );
    const stairs = rows.filter(
      (r) => r.mode === 'steps' && r.class === cls && r.training === training,
    );
    if (!chapter.length || !stairs.length) continue;
    const bands = [0, 10, 20, 30, 40].map((start) => {
      const group = stairs.slice(start, start + 10);
      return group.reduce((sum, row) => sum + row.wins, 0);
    });
    console.log(
      `${cls}/${training}: bosses ${chapter.map((r) => `${r.id}=${r.wins}/10`).join(' ')}; stairs 1–10/11–20/21–30/31–40/41–50 wins=${bands.join('/')} out of 100 each; step50=${stairs.at(-1).wins}/10`,
    );
  }
  const weekly = rows.filter((r) => r.mode === 'weekly' && r.class === cls);
  if (weekly.length) {
    console.log(
      `${cls}/weekly trained: ${weekly.reduce((sum, r) => sum + r.wins, 0)}/${weekly.length * seeds.length} wins; ${weekly.reduce((sum, r) => sum + r.timed_out, 0)} timeouts`,
    );
  }
  // 秘境：每重每种房间的胜场/次数与赢下来的平均秒数，old 与 new 是同一批开局。
  for (const training of ['fresh', 'blessed']) {
    const rift = rows.filter(
      (r) => r.mode === 'rift' && r.class === cls && r.training === training,
    );
    const groups = new Map();
    for (const r of rift) {
      const key = `F${r.floor} ${r.variant}`;
      const room = `${r.kind}${r.kind === 'boss' ? '' : r.step}`;
      if (!groups.has(key)) groups.set(key, new Map());
      const g = groups.get(key);
      const sum = g.get(room) || { wins: 0, attempts: 0, seconds: 0 };
      sum.wins += r.wins;
      sum.attempts += r.attempts;
      sum.seconds += r.win_seconds * r.wins;
      g.set(room, sum);
    }
    for (const [key, g] of groups) {
      const parts = [...g].map(
        ([room, s]) =>
          `${room} ${s.wins}/${s.attempts} ${s.wins ? (s.seconds / s.wins).toFixed(0) : '-'}s`,
      );
      console.log(`rift ${cls}/${training} ${key}: ${parts.join(', ')}`);
    }
  }
}
