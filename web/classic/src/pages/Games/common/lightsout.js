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

// 熄灯盘面最少还要按几下：第一行的按法定下之后，下面每一行只能去按上一行还亮着的灯(追灯法)，最后一行全灭才是解。
// 枚举第一行的全部 2^n 种按法取最少的一种；n 最大 5，最多 32 种。无解时返回 -1(服务端生成的盘面总是有解)。
export function lightsMinimum(board, size) {
  let best = -1;
  for (let mask = 0; mask < 1 << size; mask++) {
    const lit = board.slice();
    let presses = 0;
    const press = (row, col) => {
      presses++;
      for (const [dr, dc] of [
        [0, 0],
        [1, 0],
        [-1, 0],
        [0, 1],
        [0, -1],
      ]) {
        const r = row + dr;
        const c = col + dc;
        if (r >= 0 && r < size && c >= 0 && c < size) {
          lit[r * size + c] = !lit[r * size + c];
        }
      }
    };
    for (let col = 0; col < size; col++) {
      if (mask & (1 << col)) press(0, col);
    }
    for (let row = 1; row < size; row++) {
      for (let col = 0; col < size; col++) {
        if (lit[(row - 1) * size + col]) press(row, col);
      }
    }
    if (lit.every((on) => !on) && (best < 0 || presses < best)) best = presses;
  }
  return best;
}
