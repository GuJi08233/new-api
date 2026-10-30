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
import { sow } from './logic';

const START = [4, 4, 4, 4, 4, 4, 0, 4, 4, 4, 4, 4, 4, 0];

describe('sow', () => {
  test('the last seed in the own store earns another move', () => {
    const result = sow(START, 0, 2);
    expect(result.path).toEqual([3, 4, 5, 6]);
    expect(result.extra).toBe(true);
    expect(result.frames.at(-1)).toEqual([
      4, 4, 0, 5, 5, 5, 1, 4, 4, 4, 4, 4, 4, 0,
    ]);
  });
  test('the second player skips the first store', () => {
    const board = [1, 1, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 9, 0];
    const result = sow(board, 1, 5);
    expect(result.path).not.toContain(6);
    expect(result.frames.at(-1)).toEqual([
      2, 2, 2, 2, 2, 2, 0, 2, 2, 1, 1, 1, 0, 1,
    ]);
  });
  test('landing in an own empty pit captures the opposite pit', () => {
    const board = [1, 0, 0, 0, 0, 1, 0, 2, 2, 2, 2, 5, 3, 0];
    const result = sow(board, 0, 0);
    expect(result.captured).toBe(11);
    expect(result.frames.at(-1)[6]).toBe(6);
    expect(result.frames.at(-1)[11]).toBe(0);
  });
  test('an empty pit cannot be sown', () => {
    expect(
      sow(
        START.map((n, i) => (i === 0 ? 0 : n)),
        0,
        0,
      ),
    ).toBeNull();
  });
});
