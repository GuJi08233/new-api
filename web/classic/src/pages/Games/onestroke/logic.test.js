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
import { adjacent, planStroke } from './logic';

describe('adjacent', () => {
  test('only orthogonal neighbours on the same board', () => {
    expect(adjacent(5, 0, 1)).toBe(true);
    expect(adjacent(5, 0, 5)).toBe(true);
    expect(adjacent(5, 4, 5)).toBe(false);
    expect(adjacent(5, 0, 6)).toBe(false);
    expect(adjacent(5, 0, undefined)).toBe(false);
  });
});

describe('planStroke', () => {
  test('extending the line only adds cells', () => {
    expect(planStroke([0, 1], [0, 1, 2, 7])).toEqual({
      undo: 0,
      cells: [2, 7],
    });
  });
  test('backing up then drawing a new branch undoes first', () => {
    expect(planStroke([0, 1, 2, 3], [0, 1, 6])).toEqual({
      undo: 2,
      cells: [6],
    });
  });
  test('only retracting sends an undo without cells', () => {
    expect(planStroke([0, 1, 2], [0, 1])).toEqual({ undo: 1, cells: [] });
  });
  test('the start cell is never undone', () => {
    expect(planStroke([0, 1, 2], [0])).toEqual({ undo: 2, cells: [] });
    expect(planStroke([0], [0])).toEqual({ undo: 0, cells: [] });
  });
});
