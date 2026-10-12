// 用浏览器端的 2048 引擎生成跨端对照记录，供 pkg/g2048 的测试确认服务端重放结果与之逐位一致。
// 改动任一端的规则后，在仓库根目录执行(Node 18 以上)：
//
//   node pkg/g2048/testdata/generate_golden.mjs > pkg/g2048/testdata/golden.json
//
// 也可以传入局数，批量生成更多对局做本地对照：node generate_golden.mjs 300 > /tmp/replays.json
// 记录里有两部分：浏览器引擎的规则常量原样导出，以及脚本里的机器人按固定种子打出的整局移动。
// 一个机器人用两层期望搜索认真玩到走不动，一个随机乱按、连没让盘面变化的移动和结束之后的移动也照样记下，
// 还有一局随机按几十下就主动结束。
import {
  Game2048,
  MOVE,
  MoveRecorder,
  RULES,
  canMoveBoard,
  slideBoard,
} from '../../../web/classic/src/pages/Games/g2048/engine.js';

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

const DIRECTIONS = [MOVE.UP, MOVE.RIGHT, MOVE.DOWN, MOVE.LEFT];

// 蛇形权重：大方块沿着一条蛇形路线排在左上角，是 2048 常见的评估方式。
const WEIGHTS = [15, 14, 13, 12, 8, 9, 10, 11, 7, 6, 5, 4, 0, 1, 2, 3].map(
  (power) => 2 ** power,
);

function evaluate(board) {
  let value = 0;
  let empty = 0;
  for (let i = 0; i < board.length; i++) {
    value += WEIGHTS[i] * board[i];
    if (board[i] === 0) empty++;
  }
  return value + empty * 4096;
}

// 期望搜索：玩家这一层取最好的移动，随机这一层按 9:1 的 2、4 对所有空格取平均。
function bestValue(board, depth) {
  let best = -1;
  for (const direction of DIRECTIONS) {
    const { board: next, changed } = slideBoard(board, direction);
    if (!changed) continue;
    best = Math.max(
      best,
      depth <= 1 ? evaluate(next) : chanceValue(next, depth),
    );
  }
  return best < 0 ? evaluate(board) - 1e12 : best;
}

function chanceValue(board, depth) {
  const empty = [];
  for (let i = 0; i < board.length; i++) {
    if (board[i] === 0) empty.push(i);
  }
  if (empty.length === 0) return evaluate(board);
  let total = 0;
  for (const index of empty) {
    for (const [value, weight] of [
      [2, 0.9],
      [4, 0.1],
    ]) {
      const next = board.slice();
      next[index] = value;
      total += weight * bestValue(next, depth - 1);
    }
  }
  return total / empty.length;
}

function smartBot() {
  return (game) => {
    let best = null;
    for (const direction of DIRECTIONS) {
      const { board, changed } = slideBoard(game.board, direction);
      if (!changed) continue;
      const value = chanceValue(board, 2);
      if (best === null || value > best.value) best = { value, direction };
    }
    return best === null ? MOVE.UP : best.direction;
  };
}

function chaosBot(random) {
  return () => DIRECTIONS[random() % DIRECTIONS.length];
}

// 机器人每一步给出一个方向。recordAll 为假时只记生效的移动、走不动就停(浏览器页面的做法)，
// 为真时每一下都记，连结束之后的也记。
function play(name, seed, bot, attempts, recordAll) {
  const game = new Game2048(seed);
  const recorder = new MoveRecorder();
  for (let i = 0; i < attempts; i++) {
    if (!recordAll && game.over) break;
    const direction = bot(game);
    if (game.move(direction) || recordAll) recorder.record(direction);
  }
  if (game.over !== !canMoveBoard(game.board)) {
    throw new Error(`${name}: over flag disagrees with the board`);
  }
  return {
    name,
    seed,
    moves: recorder.moves,
    score: game.score,
    maxTile: game.maxTile,
    effectiveMoves: game.moves,
    over: game.over,
    board: game.board,
  };
}

const games = [];
const count = Number(process.argv[2] || 0);
if (count > 0) {
  for (let i = 0; i < count; i++) {
    const random = createRandom(1000 + i);
    const seed = random();
    if (i % 2 === 0) {
      games.push(play(`smart-${i}`, seed, smartBot(), 20000, false));
    } else {
      games.push(
        play(`chaos-${i}`, seed, chaosBot(random), random() % 3000, true),
      );
    }
  }
} else {
  // 认真玩到走不动：覆盖连续合并、大方块和结束判定。
  games.push(play('smart-to-game-over', 2718281828, smartBot(), 20000, false));
  // 随机乱按 1500 下：没让盘面变化的移动、结束之后的移动都记在里面，两端都必须不生效。
  games.push(
    play(
      'chaos-with-idle-moves',
      3141592653,
      chaosBot(createRandom(11)),
      1500,
      true,
    ),
  );
  // 按几十下就主动结束，盘面还能动。
  games.push(
    play('chaos-ended-early', 1618033988, chaosBot(createRandom(7)), 40, true),
  );
}
process.stdout.write(`${JSON.stringify({ rules: RULES, games })}\n`);
