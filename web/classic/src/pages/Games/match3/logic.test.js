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
import { SIZE, blasts, fallDistances, swapOf, wouldMatch } from './logic';

// 8 行字符串写成盘面，每个字符是颜色。
const boardOf = (rows) => rows.join('').split('').map(Number);

const BASE = [
  '12345612',
  '23456123',
  '34561234',
  '45612345',
  '56123456',
  '61234561',
  '12345612',
  '23456123',
];

describe('wouldMatch', () => {
  test('a swap that lines up three of a colour matches', () => {
    const rows = [...BASE];
    rows[6] = '11213456';
    rows[7] = '34163456';
    const board = boardOf(rows);
    expect(wouldMatch(board, 6 * SIZE + 2, 7 * SIZE + 2)).toBe(true);
  });
  test('a swap without a run is rejected', () => {
    expect(wouldMatch(boardOf(BASE), 0, 1)).toBe(false);
  });
  test('special gems match by their colour', () => {
    const rows = [...BASE];
    rows[6] = '11213456';
    rows[7] = '34163456';
    const board = boardOf(rows);
    board[6 * SIZE] = (1 << 3) | 1;
    expect(wouldMatch(board, 6 * SIZE + 2, 7 * SIZE + 2)).toBe(true);
  });
});

describe('swapOf', () => {
  test('neighbours map to the left or upper cell and a direction', () => {
    expect(swapOf(9, 8)).toEqual({ cell: 8, dir: 0 });
    expect(swapOf(3, 11)).toEqual({ cell: 3, dir: 1 });
  });
  test('cells across a row edge or far apart are not neighbours', () => {
    expect(swapOf(7, 8)).toBeNull();
    expect(swapOf(0, 2)).toBeNull();
  });
});

describe('fallDistances', () => {
  test('gems above a cleared run fall by its height, new gems fill from the top', () => {
    const before = new Array(SIZE * SIZE).fill(1);
    // 第 0 列消掉第 5、6、7 行。
    const drops = fallDistances(before, [5 * SIZE, 6 * SIZE, 7 * SIZE], []);
    expect(drops[7 * SIZE]).toBe(3);
    expect(drops[3 * SIZE]).toBe(3);
    expect(drops[0]).toBe(3);
    expect(drops[1]).toBe(0);
  });
  test('a created special stays where it was made', () => {
    const before = new Array(SIZE * SIZE).fill(1);
    const cleared = [7 * SIZE, 7 * SIZE + 1, 7 * SIZE + 3];
    const drops = fallDistances(before, cleared, [
      { cell: 7 * SIZE + 2, gem: 9 },
    ]);
    expect(drops[7 * SIZE + 2]).toBe(0);
    expect(drops[7 * SIZE]).toBe(1);
    expect(drops[0]).toBe(1);
  });
});

describe('blasts', () => {
  test('lists cleared cells that held special gems', () => {
    const before = new Array(SIZE * SIZE).fill(1);
    before[3] = (2 << 3) | 4;
    before[9] = (3 << 3) | 2;
    expect(blasts(before, [3, 4, 9])).toEqual([
      { cell: 3, kind: 2 },
      { cell: 9, kind: 3 },
    ]);
  });
});
