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

// 2048 规则引擎，与服务端 pkg/g2048/g2048.go 逐条对应。浏览器记下每一次移动，结束时上传，
// 服务端用同一个种子重放整局来结算成绩和奖励。两边任何规则差异都会让结算结果和玩家看到的不一致：
// 改规则时必须两边一起改，并用 pkg/g2048/testdata/generate_golden.mjs 重新生成跨端对照记录。
// 只用整数运算，随机数是与服务端逐位一致的 mulberry32。

export const SIZE = 4;
// 一局移动记录的长度上限：到了就要结束并提交，更长的记录服务端整局拒绝。
export const MAX_MOVES = 50000;
// 服务端按开局以来的真实时间限制有效移动数：不超过 (秒数 + 2) × 12，页面按同样的速度限制玩家的操作。
export const MAX_MOVES_PER_SECOND = 12;

// 新方块在随机数除以它余 0 时是 4，否则是 2。
const FOUR_CHANCE = 10;

// 移动方向的编码是上传记录的一部分，与服务端的 g2048.Direction 一一对应，不能改动。
export const MOVE = {
  UP: 0,
  RIGHT: 1,
  DOWN: 2,
  LEFT: 3,
};

// 规则常量原样导出，给 generate_golden.mjs 写进跨端对照记录，服务端测试会逐项比对。
export const RULES = {
  size: SIZE,
  fourChance: FOUR_CHANCE,
  maxMoves: MAX_MOVES,
  maxMovesPerSecond: MAX_MOVES_PER_SECOND,
};

// 每个移动方向下的四行(列)，每行按从移动方向那一侧往回的顺序列出格子编号(行优先，row × SIZE + col)。
const LINES = (() => {
  const lines = [[], [], [], []];
  for (let i = 0; i < SIZE; i++) {
    const up = [];
    const right = [];
    const down = [];
    const left = [];
    for (let j = 0; j < SIZE; j++) {
      up.push(j * SIZE + i);
      right.push(i * SIZE + SIZE - 1 - j);
      down.push((SIZE - 1 - j) * SIZE + i);
      left.push(i * SIZE + j);
    }
    lines[MOVE.UP].push(up);
    lines[MOVE.RIGHT].push(right);
    lines[MOVE.DOWN].push(down);
    lines[MOVE.LEFT].push(left);
  }
  return lines;
})();

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

// 按 direction 移动整个盘面(不生成新方块)：每一行里相邻的相同方块从移动方向那一侧开始两两合并，
// 每个方块最多合并一次。返回移动后的盘面、合并得分、盘面是否变化，以及每个方块的去向 slides
// (from、to 是移动前后的格子编号，merged 表示它在 to 与另一个方块合二为一)，供渲染滑动动画。
export function slideBoard(board, direction) {
  const next = new Array(SIZE * SIZE).fill(0);
  const slides = [];
  let gained = 0;
  for (const line of LINES[direction]) {
    // pending 是这一行里还在等合并对象的方块，target 是下一个要填的位置。
    let target = 0;
    let pending = 0;
    let pendingFrom = -1;
    for (const index of line) {
      const value = board[index];
      if (value === 0) continue;
      if (value === pending) {
        next[line[target]] = value * 2;
        gained += value * 2;
        slides.push(
          { from: pendingFrom, to: line[target], merged: true },
          { from: index, to: line[target], merged: true },
        );
        target++;
        pending = 0;
        continue;
      }
      if (pending !== 0) {
        next[line[target]] = pending;
        slides.push({ from: pendingFrom, to: line[target], merged: false });
        target++;
      }
      pending = value;
      pendingFrom = index;
    }
    if (pending !== 0) {
      next[line[target]] = pending;
      slides.push({ from: pendingFrom, to: line[target], merged: false });
    }
  }
  const changed = next.some((value, index) => value !== board[index]);
  return { board: next, gained, changed, slides };
}

// 还有让盘面变化的移动：有空格，或者有横向、纵向相邻的相同方块。
export function canMoveBoard(board) {
  for (let index = 0; index < board.length; index++) {
    const value = board[index];
    if (value === 0) return true;
    if (index % SIZE < SIZE - 1 && board[index + 1] === value) return true;
    if (index + SIZE < board.length && board[index + SIZE] === value) {
      return true;
    }
  }
  return false;
}

export class Game2048 {
  constructor(seed) {
    // 行优先，0 为空，否则为方块的数值。
    this.board = new Array(SIZE * SIZE).fill(0);
    this.random = createRandom(seed);
    this.score = 0;
    // 让盘面发生变化的移动数。
    this.moves = 0;
    this.over = false;
    // 最近一次生效的移动 { direction, slides, spawned }，只供渲染动画。
    this.lastMove = null;
    this.spawn();
    this.spawn();
  }

  // 在随机一个空格里放一个新方块：空格按行优先编号，先用一个随机数选格子，再用一个随机数决定是 2 还是 4。
  // 返回新方块 { index, value }。
  spawn() {
    const empty = [];
    for (let index = 0; index < this.board.length; index++) {
      if (this.board[index] === 0) empty.push(index);
    }
    // 让盘面变化的移动一定空出了格子，这里只是兜底。
    if (empty.length === 0) return null;
    const index = empty[this.random() % empty.length];
    const value = this.random() % FOUR_CHANCE === 0 ? 4 : 2;
    this.board[index] = value;
    return { index, value };
  }

  // 向 direction 移动。盘面没有变化(或已经结束)时什么都不做、不生成方块，返回 false；
  // 返回 true 的移动才需要记进上传的记录。
  move(direction) {
    if (this.over || LINES[direction] === undefined) return false;
    const { board, gained, changed, slides } = slideBoard(
      this.board,
      direction,
    );
    if (!changed) return false;
    this.board = board;
    this.score += gained;
    this.moves++;
    const spawned = this.spawn();
    this.over = !canMoveBoard(this.board);
    this.lastMove = { direction, slides, spawned };
    return true;
  }

  get maxTile() {
    return Math.max(...this.board);
  }
}

// 记录格式与服务端 g2048.Replay 对应：每次移动一个字符 '0' 到 '3'。只需记下生效的移动，
// 没让盘面变化的移动服务端同样忽略。
export class MoveRecorder {
  constructor() {
    this.moves = '';
  }

  record(direction) {
    this.moves += String(direction);
  }
}
