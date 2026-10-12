// 在仓库根目录运行 node pkg/breakout/testdata/generate_golden.mjs，生成真实浏览器引擎的确定性重放记录。
import { writeFileSync } from 'node:fs';
import { createGame, step, MAX_TICKS } from '../../../web/classic/src/pages/Games/breakout/engine.js';

const records = [];
for (const [name, seed, follow] of [['stationary', 1, false], ['multi-ball', 382910, true], ['unsigned-seed', 4294967295, true]]) {
  const game = createGame(seed);
  const inputs = [];
  while (!game.over && inputs.length < MAX_TICKS) {
    const target = follow ? Math.max(0, Math.min(420, game.balls[0].x + game.balls[0].vx * 2)) : 30;
    inputs.push(target); step(game, target);
  }
  records.push({ name, seed, inputs, game });
}
writeFileSync(new URL('./golden.json', import.meta.url), JSON.stringify(records) + '\n');
console.log(records.map(({ name, game }) => ({ name, score: game.score, ticks: game.ticks, broken: game.broken, won: game.won })));
