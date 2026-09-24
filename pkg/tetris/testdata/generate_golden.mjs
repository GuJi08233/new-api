// 用浏览器端的俄罗斯方块引擎生成跨端对照记录，供 pkg/tetris 的测试确认服务端重放结果与之逐位一致。
// 改动任一端的规则后，在仓库根目录执行：
//
//   node pkg/tetris/testdata/generate_golden.mjs > pkg/tetris/testdata/golden.json
//
// 也可以传入局数，批量生成更多对局做本地对照：node generate_golden.mjs 300 > /tmp/replays.json
// 记录里有两部分：浏览器引擎的规则表原样导出，以及脚本里两个机器人按固定种子打出的整局操作。
// 一个机器人按局面评估摆放方块、能连续消行升级，另一个随机乱按，覆盖踢墙、暂存、软降和锁定计时。
import {
  ACTION,
  HEIGHT,
  InputRecorder,
  PIECE_CELLS,
  RULE_TABLES,
  TetrisGame,
  WIDTH,
} from '../../../web/classic/src/pages/Games/tetris/engine.js';

function createRandom(seed) {
  let state = seed | 0;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let t = state;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t = (t + Math.imul(t ^ (t >>> 7), t | 61)) ^ t;
    return (t ^ (t >>> 14)) >>> 0;
  };
}

function fitsOn(board, piece, rotation, x, y) {
  for (const [cellX, cellY] of PIECE_CELLS[piece][rotation]) {
    const col = x + cellX;
    const row = y + cellY;
    if (col < 0 || col >= WIDTH || row < 0 || row >= HEIGHT) return false;
    if (board[row][col] !== 0) return false;
  }
  return true;
}

// 经典的四项局面评估：总高度、消行、空洞、表面起伏。
function evaluate(board, piece, rotation, x) {
  if (!fitsOn(board, piece, rotation, x, 0)) return null;
  let y = 0;
  while (fitsOn(board, piece, rotation, x, y + 1)) y++;
  const next = board.map((row) => row.slice());
  for (const [cellX, cellY] of PIECE_CELLS[piece][rotation]) {
    next[y + cellY][x + cellX] = piece + 1;
  }
  const kept = next.filter((row) => row.includes(0));
  const cleared = HEIGHT - kept.length;
  const heights = [];
  let holes = 0;
  for (let col = 0; col < WIDTH; col++) {
    let top = HEIGHT;
    for (let row = 0; row < kept.length; row++) {
      if (kept[row][col] !== 0) {
        top = Math.min(top, row);
      } else if (top < HEIGHT) {
        holes++;
      }
    }
    heights.push(top === HEIGHT ? 0 : kept.length - top);
  }
  let bumpiness = 0;
  for (let col = 1; col < WIDTH; col++) {
    bumpiness += Math.abs(heights[col] - heights[col - 1]);
  }
  const aggregate = heights.reduce((sum, h) => sum + h, 0);
  return -0.51 * aggregate + 0.76 * cleared - 0.36 * holes - 0.18 * bumpiness;
}

function bestPlacement(game) {
  let best = null;
  const candidates = [{ piece: game.piece, hold: false }];
  const alternative = game.hold >= 0 ? game.hold : game.queue[0];
  if (!game.holdUsed && alternative !== game.piece) {
    candidates.push({ piece: alternative, hold: true });
  }
  for (const candidate of candidates) {
    for (let rotation = 0; rotation < 4; rotation++) {
      for (let x = -2; x < WIDTH; x++) {
        const value = evaluate(game.board, candidate.piece, rotation, x);
        if (value !== null && (best === null || value > best.value)) {
          best = { value, rotation, x, hold: candidate.hold };
        }
      }
    }
  }
  return best;
}

// 按评估结果摆放：先暂存(如需)，再旋转、横移，最后随机选择硬降、软降或等重力落下。
function smartBot(random, reckless) {
  let plan = null;
  let planned = -1;
  let stalled = 0;
  return (game) => {
    if (planned !== game.pieces || plan === null) {
      planned = game.pieces;
      plan = reckless(game) ? null : bestPlacement(game);
      stalled = 0;
      if (plan) plan.finish = random() % 3;
    }
    if (plan === null) {
      return random() % 4 === 0 ? [ACTION.HARD_DROP] : [];
    }
    if (plan.hold) {
      plan.hold = false;
      return [ACTION.HOLD];
    }
    stalled++;
    if (stalled > 90) return [ACTION.HARD_DROP];
    if (random() % 5 === 0) return [];
    if (game.rotation !== plan.rotation) {
      return [
        (plan.rotation - game.rotation + 4) % 4 === 3
          ? ACTION.ROTATE_CCW
          : ACTION.ROTATE_CW,
      ];
    }
    if (game.x < plan.x) return [ACTION.RIGHT];
    if (game.x > plan.x) return [ACTION.LEFT];
    if (plan.finish === 0) return [ACTION.HARD_DROP];
    if (plan.finish === 1) return [ACTION.SOFT_DROP];
    return [];
  };
}

function chaosBot(random) {
  const pool = [
    ACTION.LEFT,
    ACTION.RIGHT,
    ACTION.SOFT_DROP,
    ACTION.ROTATE_CW,
    ACTION.ROTATE_CCW,
    ACTION.HOLD,
  ];
  return () => {
    const roll = random() % 100;
    if (roll < 45) return [];
    if (roll < 46) return [ACTION.HARD_DROP];
    const actions = [pool[random() % pool.length]];
    if (roll > 92) actions.push(pool[random() % pool.length]);
    return actions;
  };
}

function play(name, seed, bot, maxTicks) {
  const game = new TetrisGame(seed);
  const recorder = new InputRecorder();
  while (!game.over && game.ticks < maxTicks) {
    const actions = bot(game);
    for (const action of actions) recorder.record(game.ticks, action);
    game.tick(actions);
  }
  return {
    name,
    seed,
    ticks: game.ticks,
    inputs: recorder.inputs,
    score: game.score,
    lines: game.lines,
    level: game.level,
    pieces: game.pieces,
    over: game.over,
    board: game.board.map((row) => row.join('')),
  };
}

const games = [];
const count = Number(process.argv[2] || 0);
if (count > 0) {
  for (let i = 0; i < count; i++) {
    const random = createRandom(1000 + i);
    const seed = random();
    if (i % 2 === 0) {
      const limit = 60 + (random() % 200);
      games.push(
        play(
          `smart-${i}`,
          seed,
          smartBot(random, (game) => game.pieces >= limit),
          216000,
        ),
      );
    } else {
      games.push(
        play(`chaos-${i}`, seed, chaosBot(random), 600 + (random() % 20000)),
      );
    }
  }
} else {
  // 一局打到 15 级以上后故意堆高顶出，覆盖消行计分、升级与重力表；一局乱按到中途主动结束。
  games.push(
    play(
      'smart-then-top-out',
      2718281828,
      smartBot(createRandom(7), (game) => game.lines >= 150),
      216000,
    ),
  );
  games.push(
    play('chaos-ended-early', 3141592653, chaosBot(createRandom(11)), 1500),
  );
}
process.stdout.write(`${JSON.stringify({ rules: RULE_TABLES, games })}\n`);
