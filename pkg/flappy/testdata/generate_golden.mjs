// 用浏览器端的像素鸟引擎生成跨端对照记录，供 pkg/flappy 的测试确认服务端重放结果与之逐位一致。
// 改动任一端的规则后，在仓库根目录执行(Node 18 以上)：
//
//   node pkg/flappy/testdata/generate_golden.mjs > pkg/flappy/testdata/golden.json
//
// 也可以传入局数，批量生成更多对局做本地对照：node generate_golden.mjs 300 > /tmp/replays.json
// 记录里有两部分：浏览器引擎的规则常量原样导出，以及脚本里的机器人按固定种子打出的整局操作。
// 一个机器人按人的节奏(两次拍翅膀至少隔 9 帧)对准缺口飞，穿过 40 根管道后不再拍翅膀、掉到地上；
// 一个随机乱拍，很快撞上；还有一个脚本一掉到线下就拍、每帧都能拍，1 秒内拍翅膀的次数远超人类，
// 供服务端的速度检查对照，它飞到 40 秒时主动结束。
import {
  BIRD_X,
  FlapRecorder,
  FlappyGame,
  PIPE_WIDTH,
  RULES,
  SCALE,
} from '../../../web/classic/src/pages/Games/flappy/engine.js';

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

// 鸟还没完全越过的第一根管道，就是接下来要穿过的缺口。
function nextPipe(game) {
  return game.pipes.find(
    (pipe) => pipe.x + PIPE_WIDTH * SCALE >= BIRD_X * SCALE,
  );
}

// 对准缺口飞：预计 lead 帧后落到缺口上沿以下 zone 像素(加一点抖动)时拍一下，两次拍翅膀至少隔 minGap 帧；
// give 返回真之后不再拍翅膀。
function aimBot(random, { minGap, zone, lead }, give) {
  let last = 0;
  return (game) => {
    if (!game.started) return true;
    if (give(game)) return false;
    const line = nextPipe(game).gapTop + (zone + (random() % 9) - 4) * SCALE;
    if (game.y + lead * game.vy <= line) return false;
    if (game.ticks - last < minGap) return false;
    last = game.ticks;
    return true;
  };
}

// 人的节奏：两次拍翅膀至少隔 9 帧，一秒最多六七次。
const HUMAN = { minGap: 9, zone: 72, lead: 2 };
// 脚本：一掉到线下就拍，每帧都可以拍，爬升时一秒能拍二三十次。
const SCRIPT = { minGap: 1, zone: 80, lead: 0 };

// 随机乱拍：平均每 12 帧拍一次。
function chaosBot(random) {
  return (game) => !game.started || random() % 12 === 0;
}

function play(name, seed, bot, maxTicks) {
  const game = new FlappyGame(seed);
  const recorder = new FlapRecorder();
  while (!game.over && game.ticks < maxTicks) {
    const flap = bot(game);
    if (flap) recorder.record(game.ticks);
    game.tick(flap);
  }
  return {
    name,
    seed,
    ticks: game.ticks,
    flaps: recorder.flaps,
    score: game.score,
    flapCount: game.flaps,
    over: game.over,
    y: game.y,
    vy: game.vy,
    pipes: game.pipes,
  };
}

const games = [];
const count = Number(process.argv[2] || 0);
if (count > 0) {
  for (let i = 0; i < count; i++) {
    const random = createRandom(1000 + i);
    const seed = random();
    if (i % 3 === 0) {
      const limit = 1 + (random() % 200);
      const bot = aimBot(random, HUMAN, (game) => game.score >= limit);
      games.push(play(`human-${i}`, seed, bot, RULES.maxTicks));
    } else if (i % 3 === 1) {
      const bot = aimBot(random, SCRIPT, () => false);
      games.push(play(`script-${i}`, seed, bot, 600 + (random() % 20000)));
    } else {
      games.push(play(`chaos-${i}`, seed, chaosBot(random), RULES.maxTicks));
    }
  }
} else {
  games.push(
    play(
      'human-then-fall',
      577215664,
      aimBot(createRandom(7), HUMAN, (game) => game.score >= 40),
      RULES.maxTicks,
    ),
  );
  games.push(
    play('chaos-crash', 1202056903, chaosBot(createRandom(11)), RULES.maxTicks),
  );
  // 脚本打 40 秒，主动结束时鸟还活着。
  games.push(
    play(
      'script',
      2685452001,
      aimBot(createRandom(13), SCRIPT, () => false),
      40 * RULES.ticksPerSecond,
    ),
  );
}
process.stdout.write(`${JSON.stringify({ rules: RULES, games })}\n`);
