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
import { boardLines, lineMatches, lineRuns } from './logic';

describe('nonogram lines', () => {
  test('runs are the lengths of filled segments', () => {
    expect(lineRuns([true, true, false, true, false])).toEqual([2, 1]);
    expect(lineRuns([false, false])).toEqual([]);
  });

  test('a blank line matches an empty or zero clue', () => {
    expect(lineMatches([false, false, false], [])).toBe(true);
    expect(lineMatches([false, false, false], [0])).toBe(true);
    expect(lineMatches([true, false, true], [1, 1])).toBe(true);
    expect(lineMatches([true, true, false], [1, 1])).toBe(false);
  });

  test('columns are read top to bottom', () => {
    const { rows, cols } = boardLines([true, false, true, true], 2);
    expect(rows).toEqual([
      [true, false],
      [true, true],
    ]);
    expect(cols).toEqual([
      [true, true],
      [false, true],
    ]);
  });
});
