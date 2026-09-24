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

// 俄罗斯方块规则引擎，与服务端 pkg/tetris/tetris.go 逐条对应。浏览器按帧记录操作，
// 结束时上传，服务端用同一个种子重放整局来结算成绩和奖励。两边任何规则差异都会让结算结果
// 和玩家看到的不一致：改规则时必须两边一起改，并用 pkg/tetris/testdata/generate_golden.mjs
// 重新生成跨端对照记录。只用整数运算，随机数是与服务端逐位一致的 mulberry32。

export const WIDTH = 10;
export const HEIGHT = 20;
export const TICKS_PER_SECOND = 60;
// 一局的游戏内时长上限(1 小时)，到点自动结束。
export const MAX_TICKS = 60 * 60 * TICKS_PER_SECOND;
export const PREVIEW_SIZE = 5;

const LOCK_DELAY = 30;
const MAX_LOCK_RESETS = 15;
const LINES_PER_LEVEL = 10;

// 操作编码是上传记录的一部分，与服务端的 tetris.Action 一一对应，不能改动。
export const ACTION = {
  LEFT: 1,
  RIGHT: 2,
  SOFT_DROP: 3,
  HARD_DROP: 4,
  ROTATE_CW: 5,
  ROTATE_CCW: 6,
  HOLD: 7,
};

const PIECE_I = 0;
const PIECE_O = 1;

// 方块种类依次为 I O T S Z J L。出生朝向下各格在旋转方框内的坐标(x 向右，y 向下)，
// 按 x、y 成对排列；其余朝向由方框内顺时针旋转得到。
const PIECE_BOXES = [4, 2, 3, 3, 3, 3, 3];
const SPAWN_CELLS = [
  [0, 1, 1, 1, 2, 1, 3, 1],
  [0, 0, 1, 0, 0, 1, 1, 1],
  [1, 0, 0, 1, 1, 1, 2, 1],
  [1, 0, 2, 0, 0, 1, 1, 1],
  [0, 0, 1, 0, 1, 1, 2, 1],
  [0, 0, 0, 1, 1, 1, 2, 1],
  [2, 0, 0, 1, 1, 1, 2, 1],
];

export const PIECE_CELLS = SPAWN_CELLS.map((flat, piece) => {
  const size = PIECE_BOXES[piece];
  const spawn = [];
  for (let i = 0; i < flat.length; i += 2) {
    spawn.push([flat[i], flat[i + 1]]);
  }
  const rotations = [spawn];
  for (let rotation = 1; rotation < 4; rotation++) {
    rotations.push(rotations[rotation - 1].map(([x, y]) => [size - 1 - y, x]));
  }
  return rotations;
});

// SRS 踢墙表，照抄规范里的写法(x 向右，y 向上，按 x、y 成对排列)，使用时把 y 取反。
// 下标为 [起始朝向][0 顺时针 / 1 逆时针]，朝向 0/1/2/3 即规范里的 0/R/2/L。
const JLSTZ_KICKS = [
  [
    [0, 0, -1, 0, -1, 1, 0, -2, -1, -2],
    [0, 0, 1, 0, 1, 1, 0, -2, 1, -2],
  ],
  [
    [0, 0, 1, 0, 1, -1, 0, 2, 1, 2],
    [0, 0, 1, 0, 1, -1, 0, 2, 1, 2],
  ],
  [
    [0, 0, 1, 0, 1, 1, 0, -2, 1, -2],
    [0, 0, -1, 0, -1, 1, 0, -2, -1, -2],
  ],
  [
    [0, 0, -1, 0, -1, -1, 0, 2, -1, 2],
    [0, 0, -1, 0, -1, -1, 0, 2, -1, 2],
  ],
];

const I_KICKS = [
  [
    [0, 0, -2, 0, 1, 0, -2, -1, 1, 2],
    [0, 0, -1, 0, 2, 0, -1, 2, 2, -1],
  ],
  [
    [0, 0, -1, 0, 2, 0, -1, 2, 2, -1],
    [0, 0, 2, 0, -1, 0, 2, 1, -1, -2],
  ],
  [
    [0, 0, 2, 0, -1, 0, 2, 1, -1, -2],
    [0, 0, 1, 0, -2, 0, 1, -2, -2, 1],
  ],
  [
    [0, 0, 1, 0, -2, 0, 1, -2, -2, 1],
    [0, 0, -2, 0, 1, 0, -2, -1, 1, 2],
  ],
];

// 各等级下方块每下落一行所需的帧数，13 级起每帧下落一行。
const GRAVITY_TABLE = [60, 48, 37, 28, 21, 16, 11, 8, 6, 4, 3, 2, 1];

const LINE_SCORES = [0, 100, 300, 500, 800];

// 规则表原样导出，只给 generate_golden.mjs 写进跨端对照记录。踢墙表里很少用到的位置，
// 整局对照未必覆盖得到，服务端测试会逐项比对这些表。
export const RULE_TABLES = {
  pieceBoxes: PIECE_BOXES,
  spawnCells: SPAWN_CELLS,
  jlstzKicks: JLSTZ_KICKS,
  iKicks: I_KICKS,
  gravity: GRAVITY_TABLE,
  lineScores: LINE_SCORES,
  lockDelay: LOCK_DELAY,
  maxLockResets: MAX_LOCK_RESETS,
  linesPerLevel: LINES_PER_LEVEL,
  previewSize: PREVIEW_SIZE,
};

function gravityTicks(level) {
  return level > GRAVITY_TABLE.length ? 1 : GRAVITY_TABLE[level - 1];
}

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

function emptyRow() {
  return new Array(WIDTH).fill(0);
}

export class TetrisGame {
  constructor(seed) {
    // 0 为空，否则为方块种类 + 1。
    this.board = Array.from({ length: HEIGHT }, emptyRow);
    this.random = createRandom(seed);
    this.queue = [];
    this.piece = 0;
    this.x = 0;
    this.y = 0;
    this.rotation = 0;
    this.hold = -1;
    this.holdUsed = false;
    this.gravityTimer = 0;
    this.lockTimer = 0;
    this.lockResets = 0;
    this.lowestY = 0;
    this.score = 0;
    this.lines = 0;
    this.level = 1;
    this.pieces = 0;
    this.ticks = 0;
    this.over = false;
    this.spawn(this.takeNext());
  }

  // 方块序列按 7 个一袋洗牌生成，只由种子决定。
  takeNext() {
    while (this.queue.length <= PREVIEW_SIZE) {
      const bag = [0, 1, 2, 3, 4, 5, 6];
      for (let i = 6; i > 0; i--) {
        const j = this.random() % (i + 1);
        [bag[i], bag[j]] = [bag[j], bag[i]];
      }
      this.queue.push(...bag);
    }
    return this.queue.shift();
  }

  spawn(piece) {
    this.piece = piece;
    this.rotation = 0;
    this.x = piece === PIECE_O ? 4 : 3;
    this.y = 0;
    this.gravityTimer = 0;
    this.lockTimer = 0;
    this.lockResets = 0;
    this.lowestY = this.y;
    if (!this.fits(this.x, this.y, this.rotation)) {
      this.over = true;
    }
  }

  fits(x, y, rotation) {
    for (const [cellX, cellY] of PIECE_CELLS[this.piece][rotation]) {
      const col = x + cellX;
      const row = y + cellY;
      if (
        col < 0 ||
        col >= WIDTH ||
        row < 0 ||
        row >= HEIGHT ||
        this.board[row][col] !== 0
      ) {
        return false;
      }
    }
    return true;
  }

  // 推进一帧：先依次执行这一帧的操作，再结算重力与落地锁定。
  tick(actions) {
    if (this.over) return;
    this.ticks++;
    for (const action of actions) {
      this.apply(action);
      if (this.over) return;
    }
    this.gravityTimer++;
    if (this.gravityTimer >= gravityTicks(this.level)) {
      this.gravityTimer = 0;
      this.moveDown();
    }
    if (this.fits(this.x, this.y + 1, this.rotation)) {
      this.lockTimer = 0;
      return;
    }
    this.lockTimer++;
    if (this.lockTimer >= LOCK_DELAY) {
      this.lock();
    }
  }

  apply(action) {
    switch (action) {
      case ACTION.LEFT:
        this.shift(-1);
        break;
      case ACTION.RIGHT:
        this.shift(1);
        break;
      case ACTION.SOFT_DROP:
        if (this.moveDown()) {
          this.score++;
          this.gravityTimer = 0;
        }
        break;
      case ACTION.HARD_DROP:
        while (this.moveDown()) {
          this.score += 2;
        }
        this.lock();
        break;
      case ACTION.ROTATE_CW:
        this.rotate(0);
        break;
      case ACTION.ROTATE_CCW:
        this.rotate(1);
        break;
      case ACTION.HOLD:
        this.holdPiece();
        break;
      default:
        break;
    }
  }

  moveDown() {
    if (!this.fits(this.x, this.y + 1, this.rotation)) return false;
    this.y++;
    if (this.y > this.lowestY) {
      this.lowestY = this.y;
      this.lockResets = 0;
    }
    return true;
  }

  shift(dx) {
    if (this.fits(this.x + dx, this.y, this.rotation)) {
      this.x += dx;
      this.resetLock();
    }
  }

  // 按 SRS 依次尝试踢墙位置，direction 为 0 顺时针、1 逆时针。O 方块不旋转。
  rotate(direction) {
    if (this.piece === PIECE_O) return;
    const target =
      direction === 1 ? (this.rotation + 3) % 4 : (this.rotation + 1) % 4;
    const table = this.piece === PIECE_I ? I_KICKS : JLSTZ_KICKS;
    const kicks = table[this.rotation][direction];
    for (let i = 0; i < kicks.length; i += 2) {
      const x = this.x + kicks[i];
      const y = this.y - kicks[i + 1];
      if (!this.fits(x, y, target)) continue;
      this.x = x;
      this.y = y;
      this.rotation = target;
      if (this.y > this.lowestY) {
        this.lowestY = this.y;
        this.lockResets = 0;
      }
      this.resetLock();
      return;
    }
  }

  // 落地后移动或旋转成功时重新开始锁定计时，次数用完后照常锁定。
  resetLock() {
    if (this.lockTimer > 0 && this.lockResets < MAX_LOCK_RESETS) {
      this.lockTimer = 0;
      this.lockResets++;
    }
  }

  // 把当前方块放进暂存区，换出暂存的方块(暂存区为空时取下一个)。每落下一个方块只能用一次。
  holdPiece() {
    if (this.holdUsed) return;
    const current = this.piece;
    if (this.hold < 0) {
      this.hold = current;
      this.spawn(this.takeNext());
    } else {
      const held = this.hold;
      this.hold = current;
      this.spawn(held);
    }
    this.holdUsed = true;
  }

  lock() {
    for (const [cellX, cellY] of PIECE_CELLS[this.piece][this.rotation]) {
      this.board[this.y + cellY][this.x + cellX] = this.piece + 1;
    }
    const cleared = this.clearLines();
    this.lines += cleared;
    this.score += LINE_SCORES[cleared] * this.level;
    this.level = 1 + Math.floor(this.lines / LINES_PER_LEVEL);
    this.pieces++;
    this.holdUsed = false;
    this.spawn(this.takeNext());
  }

  clearLines() {
    const kept = this.board.filter((row) => row.includes(0));
    const cleared = HEIGHT - kept.length;
    if (cleared > 0) {
      this.board = [...Array.from({ length: cleared }, emptyRow), ...kept];
    }
    return cleared;
  }

  // 以下只供渲染：当前方块各格、直落到底的位置与预览队列。
  cells(y = this.y) {
    return PIECE_CELLS[this.piece][this.rotation].map(([cellX, cellY]) => [
      this.x + cellX,
      y + cellY,
    ]);
  }

  ghostY() {
    let y = this.y;
    while (this.fits(this.x, y + 1, this.rotation)) y++;
    return y;
  }

  preview() {
    return this.queue.slice(0, PREVIEW_SIZE);
  }
}

// 记录操作的格式与服务端 tetris.DecodeInputs 对应：[距上一个操作的帧数, 操作] 成对排列。
export class InputRecorder {
  constructor() {
    this.inputs = [];
    this.lastTick = 0;
  }

  record(tick, action) {
    this.inputs.push(tick - this.lastTick, action);
    this.lastTick = tick;
  }
}
