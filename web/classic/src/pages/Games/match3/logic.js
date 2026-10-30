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

// 宝石消除的页面逻辑。盘面与服务端 pkg/match3 一致：8×8，宝石的低 3 位是颜色，再往上是特殊宝石的种类
// (1 清整行、2 清整列、3 炸弹)。规则以服务端为准，这里只用来提前挡住连不成三颗的交换、算出落下的动画。
export const SIZE = 8;

export const colorOf = (gem) => gem & 7;
export const kindOf = (gem) => gem >> 3;

// 交换 a、b 两格之后，两格所在的行列里有没有连成三颗同色。
export function wouldMatch(board, a, b) {
  const next = board.slice();
  [next[a], next[b]] = [next[b], next[a]];
  return [a, b].some((cell) => {
    const color = colorOf(next[cell]);
    const row = Math.floor(cell / SIZE);
    const col = cell % SIZE;
    const run = (dr, dc) => {
      let n = 0;
      for (
        let r = row + dr, c = col + dc;
        r >= 0 &&
        r < SIZE &&
        c >= 0 &&
        c < SIZE &&
        colorOf(next[r * SIZE + c]) === color;
        r += dr, c += dc
      )
        n++;
      return n;
    };
    return run(0, -1) + run(0, 1) >= 2 || run(-1, 0) + run(1, 0) >= 2;
  });
}

// 把一次点击/滑动换成服务端的交换参数：cell 是左边(上面)那一格，dir 0 与右边交换、1 与下面交换。两格不相邻时为 null。
export function swapOf(a, b) {
  const [first, second] = a < b ? [a, b] : [b, a];
  if (
    second - first === 1 &&
    Math.floor(first / SIZE) === Math.floor(second / SIZE)
  )
    return { cell: first, dir: 0 };
  if (second - first === SIZE) return { cell: first, dir: 1 };
  return null;
}

// 一轮消除之后每一格的宝石往下落了几格：留下来的宝石落过下方被消掉的空位，从顶上补进来的宝石落过整段空位。
// before 是这一轮消除前的盘面，cleared 是消掉的格子，created 是留下的特殊宝石(它们不动)。
export function fallDistances(before, cleared, created) {
  const gone = new Set(cleared);
  const stays = new Set(created.map((c) => c.cell));
  const drops = new Array(SIZE * SIZE).fill(0);
  for (let col = 0; col < SIZE; col++) {
    let write = SIZE - 1;
    for (let row = SIZE - 1; row >= 0; row--) {
      const cell = row * SIZE + col;
      if (gone.has(cell) && !stays.has(cell)) continue;
      drops[write * SIZE + col] = write - row;
      write--;
    }
    for (let row = write; row >= 0; row--) drops[row * SIZE + col] = write + 1;
  }
  return drops;
}

// 这一轮引爆了哪些特殊宝石：消掉的格子里原来是特殊宝石的那些，页面在那里画光束或冲击波。
export function blasts(before, cleared) {
  return cleared
    .filter((cell) => kindOf(before[cell]) > 0)
    .map((cell) => ({ cell, kind: kindOf(before[cell]) }));
}
