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

// 贪吃蛇规则引擎，与服务端 pkg/snake/snake.go 逐条对应。浏览器按步记下每一次生效的转向，结束时上传，
// 服务端用同一个种子重放整局来结算成绩、游戏内时长和奖励。两边任何规则差异都会让结算结果和玩家看到的不一致：
// 改规则时必须两边一起改，并用 pkg/snake/testdata/generate_golden.mjs 重新生成跨端对照记录。
// 只用整数运算，随机数是与服务端逐位一致的 mulberry32。

export const WIDTH = 20;
export const HEIGHT = 20;
export const START_LENGTH = 3;
// 一局的步数上限：到了就要结束并提交，更长的记录服务端整局拒绝。
export const MAX_STEPS = 50000;

const START_X = 4;
const START_Y = 10;
// 步长(毫秒)从 BASE_STEP_MS 起，每得 1 分快 STEP_MS_PER_FOOD，最快 MIN_STEP_MS。
const BASE_STEP_MS = 150;
const STEP_MS_PER_FOOD = 4;
const MIN_STEP_MS = 70;

// 方向编码是上传记录的一部分，与服务端的 snake.Direction 一一对应，不能改动。
export const DIRECTION = {
  UP: 0,
  RIGHT: 1,
  DOWN: 2,
  LEFT: 3,
};
// 这一步不转向。
export const NO_TURN = -1;

const STEP_X = [0, 1, 0, -1];
const STEP_Y = [-1, 0, 1, 0];

// 规则常量原样导出，只给 generate_golden.mjs 写进跨端对照记录，服务端测试会逐项比对。
export const RULES = {
  width: WIDTH,
  height: HEIGHT,
  startLength: START_LENGTH,
  startX: START_X,
  startY: START_Y,
  maxSteps: MAX_STEPS,
  baseStepMs: BASE_STEP_MS,
  stepMsPerFood: STEP_MS_PER_FOOD,
  minStepMs: MIN_STEP_MS,
};

// mulberry32：Math.imul 与 >>> 让每一步都是 32 位整数运算，与服务端的 uint32 实现逐位一致。
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

// 从 current 转向 next 会不会生效：与当前方向相同或相反的转向不生效，蛇不能原地掉头咬到自己的脖子。
// 页面给玩家连按的方向排队时，也用它和队尾的方向比较。
export function isTurn(current, next) {
  return (
    Number.isInteger(next) &&
    next >= DIRECTION.UP &&
    next <= DIRECTION.LEFT &&
    next !== current &&
    next !== (current + 2) % 4
  );
}

export class SnakeGame {
  constructor(seed) {
    this.random = createRandom(seed);
    // 格子编号行优先(y × WIDTH + x)。body 从尾到头排列。
    this.body = [];
    this.occupied = new Array(WIDTH * HEIGHT).fill(false);
    this.direction = DIRECTION.RIGHT;
    // 场地被蛇占满时为 -1。
    this.food = -1;
    this.score = 0;
    // 已推进的步数，包括结束的那一步。
    this.steps = 0;
    // 游戏内时长：已推进的每一步的步长之和。
    this.elapsedMs = 0;
    this.over = false;
    for (let x = START_X - START_LENGTH + 1; x <= START_X; x++) {
      const cell = START_Y * WIDTH + x;
      this.body.push(cell);
      this.occupied[cell] = true;
    }
    this.placeFood();
  }

  // 下一步的步长(毫秒)：页面按它的节奏推进。
  stepMs() {
    return Math.max(MIN_STEP_MS, BASE_STEP_MS - STEP_MS_PER_FOOD * this.score);
  }

  get length() {
    return this.body.length;
  }

  // 把食物放在随机一个空格里：空格按行优先编号，用一个随机数选。场地被蛇占满时结束。
  placeFood() {
    const free = WIDTH * HEIGHT - this.body.length;
    if (free === 0) {
      this.food = -1;
      this.over = true;
      return;
    }
    let pick = this.random() % free;
    for (let cell = 0; cell < this.occupied.length; cell++) {
      if (this.occupied[cell]) continue;
      if (pick === 0) {
        this.food = cell;
        return;
      }
      pick--;
    }
  }

  // 推进一步：先转向 turn(NO_TURN 表示不转，不生效的转向忽略)，再让蛇前进一格。
  // 返回转向是否生效；生效的转向要按这一步开始前的 steps 记进上传的记录。
  step(turn = NO_TURN) {
    if (this.over) return false;
    const turned = isTurn(this.direction, turn);
    if (turned) this.direction = turn;
    this.elapsedMs += this.stepMs();
    this.steps++;
    const head = this.body[this.body.length - 1];
    const x = (head % WIDTH) + STEP_X[this.direction];
    const y = Math.floor(head / WIDTH) + STEP_Y[this.direction];
    if (x < 0 || x >= WIDTH || y < 0 || y >= HEIGHT) {
      this.over = true;
      return turned;
    }
    const next = y * WIDTH + x;
    // 没吃到食物时尾巴这一步会移走，头可以进入它空出的格子；食物不会放在蛇身上，吃到食物时头不可能是尾巴那一格。
    const tail = this.body[0];
    if (this.occupied[next] && next !== tail) {
      this.over = true;
      return turned;
    }
    const eating = next === this.food;
    if (!eating) {
      this.occupied[tail] = false;
      this.body.shift();
    }
    this.body.push(next);
    this.occupied[next] = true;
    if (eating) {
      this.score++;
      this.placeFood();
    }
    return turned;
  }

  // 以下只供渲染：蛇身各格从尾到头的坐标 [x, y]，食物的坐标(没有食物时为 null)。
  cells() {
    return this.body.map((cell) => [cell % WIDTH, Math.floor(cell / WIDTH)]);
  }

  foodCell() {
    if (this.food < 0) return null;
    return [this.food % WIDTH, Math.floor(this.food / WIDTH)];
  }
}

// 记录格式与服务端 snake.DecodeInputs 对应：[距上一次转向的步数, 方向] 成对排列，每一步最多一次。
// 只记 step 返回 true 的转向，step 是转向那一步开始前的 game.steps。
export class TurnRecorder {
  constructor() {
    this.inputs = [];
    this.lastStep = 0;
  }

  record(step, direction) {
    this.inputs.push(step - this.lastStep, direction);
    this.lastStep = step;
  }
}
