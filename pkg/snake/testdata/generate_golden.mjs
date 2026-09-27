// 用浏览器端的贪吃蛇引擎生成跨端对照记录，供 pkg/snake 的测试确认服务端重放结果与之逐位一致。
// 改动任一端的规则后，在仓库根目录执行(Node 18 以上)：
//
//   node pkg/snake/testdata/generate_golden.mjs > pkg/snake/testdata/golden.json
//
// 也可以传入局数，批量生成更多对局做本地对照：node generate_golden.mjs 300 > /tmp/replays.json
// 记录里有两部分：浏览器引擎的规则常量原样导出，以及脚本里的机器人按固定种子打出的整局转向。
// 一个机器人沿最短路去吃食物、只走还能回到自己尾巴的路，吃到 45 个之后不再转向、一头撞死，
// 覆盖加速到最快步长、吃食物后立刻放下一个和撞墙；一个随机乱转，同向、反向的转向也照样去按；
// 还有一局按最短路吃了几口就主动结束。
import {
  DIRECTION,
  HEIGHT,
  NO_TURN,
  RULES,
  SnakeGame,
  TurnRecorder,
  WIDTH,
  isTurn,
} from '../../../web/classic/src/pages/Games/snake/engine.js';

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

const DIRECTIONS = [
  DIRECTION.UP,
  DIRECTION.RIGHT,
  DIRECTION.DOWN,
  DIRECTION.LEFT,
];
const STEP_X = [0, 1, 0, -1];
const STEP_Y = [-1, 0, 1, 0];

function neighbor(cell, direction) {
  const x = (cell % WIDTH) + STEP_X[direction];
  const y = Math.floor(cell / WIDTH) + STEP_Y[direction];
  if (x < 0 || x >= WIDTH || y < 0 || y >= HEIGHT) return -1;
  return y * WIDTH + x;
}

// 从 start 出发、不经过 blocked 的格子，到每个格子的步数(到不了为 -1)。
function distances(start, blocked) {
  const dist = new Array(WIDTH * HEIGHT).fill(-1);
  dist[start] = 0;
  const queue = [start];
  for (let i = 0; i < queue.length; i++) {
    for (const direction of DIRECTIONS) {
      const next = neighbor(queue[i], direction);
      if (next < 0 || blocked[next] || dist[next] >= 0) continue;
      dist[next] = dist[queue[i]] + 1;
      queue.push(next);
    }
  }
  return dist;
}

// 蛇头朝 direction 走一步之后的蛇身(从尾到头)，撞墙或撞到自己时为 null。
function moved(game, direction) {
  const head = game.body[game.body.length - 1];
  const next = neighbor(head, direction);
  if (next < 0) return null;
  const tail = game.body[0];
  if (game.occupied[next] && next !== tail) return null;
  const body = next === game.food ? game.body.slice() : game.body.slice(1);
  body.push(next);
  return body;
}

// 走完这一步之后头还能不能回到尾巴：能回到尾巴的蛇不会把自己困死。
function canReachTail(body) {
  const blocked = new Array(WIDTH * HEIGHT).fill(false);
  for (const cell of body) blocked[cell] = true;
  const tail = body[0];
  blocked[tail] = false;
  return distances(body[body.length - 1], blocked)[tail] > 0;
}

// 走完这一步之后头能到达的空格数。
function reachable(body) {
  const blocked = new Array(WIDTH * HEIGHT).fill(false);
  for (const cell of body) blocked[cell] = true;
  blocked[body[0]] = false;
  const head = body[body.length - 1];
  blocked[head] = false;
  return distances(head, blocked).filter((d) => d > 0).length;
}

// 沿最短路去吃食物，只走还能回到尾巴的路；没有这样的路时走能到达的空格最多的方向。
function smartBot(give) {
  return (game) => {
    if (give(game)) return NO_TURN;
    const candidates = [];
    for (const direction of DIRECTIONS) {
      if (direction !== game.direction && !isTurn(game.direction, direction)) {
        continue;
      }
      const body = moved(game, direction);
      if (body === null) continue;
      const blocked = new Array(WIDTH * HEIGHT).fill(false);
      for (const cell of body) blocked[cell] = true;
      const head = body[body.length - 1];
      blocked[head] = false;
      const food = game.food < 0 ? -1 : distances(head, blocked)[game.food];
      candidates.push({
        direction,
        food: head === game.food ? 0 : food,
        safe: canReachTail(body),
        room: reachable(body),
      });
    }
    if (candidates.length === 0) return NO_TURN;
    const safe = candidates.filter((c) => c.safe && c.food >= 0);
    if (safe.length > 0) {
      safe.sort((a, b) => a.food - b.food || a.direction - b.direction);
      return safe[0].direction;
    }
    candidates.sort(
      (a, b) =>
        Number(b.safe) - Number(a.safe) ||
        b.room - a.room ||
        a.direction - b.direction,
    );
    return candidates[0].direction;
  };
}

// 随机乱转：两成的步去按一个随机方向，同向、反向的照样按，只是不主动拐进墙或蛇身；
// 前面就是墙或蛇身时随便拐向一个还能走的方向，走投无路才撞上。
function chaosBot(random) {
  return (game) => {
    const alive = (direction) => moved(game, direction) !== null;
    if (!alive(game.direction)) {
      const escapes = DIRECTIONS.filter(
        (direction) => isTurn(game.direction, direction) && alive(direction),
      );
      if (escapes.length > 0) return escapes[random() % escapes.length];
    }
    if (random() % 10 >= 2) return NO_TURN;
    const press = DIRECTIONS[random() % DIRECTIONS.length];
    return isTurn(game.direction, press) && !alive(press) ? NO_TURN : press;
  };
}

// recordAll 为假时只记生效的转向(浏览器页面的做法)；为真时每一步按下的方向都记，
// 同向、反向的转向两端都必须不生效。
function play(name, seed, bot, maxSteps, recordAll) {
  const game = new SnakeGame(seed);
  const recorder = new TurnRecorder();
  while (!game.over && game.steps < maxSteps) {
    const step = game.steps;
    const turn = bot(game);
    const turned = game.step(turn);
    if (turned || (recordAll && turn !== NO_TURN)) {
      recorder.record(step, turn);
    }
  }
  return {
    name,
    seed,
    steps: game.steps,
    inputs: recorder.inputs,
    score: game.score,
    length: game.length,
    elapsedMs: game.elapsedMs,
    over: game.over,
    direction: game.direction,
    food: game.food,
    body: game.body,
  };
}

const games = [];
const count = Number(process.argv[2] || 0);
if (count > 0) {
  for (let i = 0; i < count; i++) {
    const random = createRandom(1000 + i);
    const seed = random();
    if (i % 2 === 0) {
      const limit = 10 + (random() % 150);
      games.push(
        play(
          `smart-${i}`,
          seed,
          smartBot((game) => game.score >= limit),
          RULES.maxSteps,
          false,
        ),
      );
    } else {
      games.push(
        play(`chaos-${i}`, seed, chaosBot(random), RULES.maxSteps, true),
      );
    }
  }
} else {
  games.push(
    play(
      'smart-then-crash',
      1414213562,
      smartBot((game) => game.score >= 45),
      RULES.maxSteps,
      false,
    ),
  );
  games.push(
    play(
      'chaos-with-idle-turns',
      1732050807,
      chaosBot(createRandom(11)),
      RULES.maxSteps,
      true,
    ),
  );
  // 吃了几口、走了 150 步就主动结束，蛇还活着。
  games.push(
    play(
      'smart-ended-early',
      2236067977,
      smartBot(() => false),
      150,
      false,
    ),
  );
}
process.stdout.write(`${JSON.stringify({ rules: RULES, games })}\n`);
