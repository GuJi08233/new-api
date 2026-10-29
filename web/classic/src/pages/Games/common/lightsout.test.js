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

import { describe, expect, test } from 'bun:test';
import { lightsMinimum } from './lightsout';

// 按下 cells 里的灯得到的盘面：从全灭开始，每按一下切换自己与上下左右。
function pressed(size, cells) {
  const board = new Array(size * size).fill(false);
  for (const cell of cells) {
    const row = Math.floor(cell / size);
    const col = cell % size;
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
        board[r * size + c] = !board[r * size + c];
      }
    }
  }
  return board;
}

describe('lightsMinimum', () => {
  test('a dark board needs no presses', () => {
    expect(lightsMinimum(pressed(3, []), 3)).toBe(0);
  });

  test('3x3 boards have a unique solution, so the minimum is the press count', () => {
    expect(lightsMinimum(pressed(3, [4]), 3)).toBe(1);
    expect(lightsMinimum(pressed(3, [0, 8]), 3)).toBe(2);
    expect(lightsMinimum(pressed(3, [0, 2, 6, 8]), 3)).toBe(4);
  });

  test('never exceeds the presses that built the board', () => {
    for (const size of [4, 5]) {
      const cells = [0, size + 1, 2 * size + 2, size * size - 1];
      const minimum = lightsMinimum(pressed(size, cells), size);
      expect(minimum).toBeGreaterThanOrEqual(0);
      expect(minimum).toBeLessThanOrEqual(cells.length);
    }
  });

  test('pressing the 5x5 quiet pattern changes nothing', () => {
    // 5x5 有不改变盘面的按法组合(零空间)，按一遍等于没按。
    const quiet = [0, 2, 4, 5, 7, 9, 15, 17, 19, 20, 22, 24];
    expect(lightsMinimum(pressed(5, quiet), 5)).toBe(0);
  });

  test('a single lit corner on 5x5 is unsolvable', () => {
    const board = new Array(25).fill(false);
    board[0] = true;
    expect(lightsMinimum(board, 5)).toBe(-1);
  });
});
