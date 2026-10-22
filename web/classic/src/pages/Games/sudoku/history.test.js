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

import { expect, test } from 'bun:test';
import { SudokuHistory } from './history';

test('撤销误填时恢复整盘数字以及被连带删除的候选笔记', () => {
  const history = new SudokuHistory();
  const values = Array(81).fill(0);
  const notes = Array(81).fill(0);
  notes[0] = (1 << 3) | (1 << 7);
  notes[1] = 1 << 3;
  history.record(values, notes);
  values[0] = 3;
  notes[0] = 0;
  notes[1] = 0;
  const restored = history.undo();
  expect(restored.values[0]).toBe(0);
  expect(restored.notes[0]).toBe((1 << 3) | (1 << 7));
  expect(restored.notes[1]).toBe(1 << 3);
  expect(history.undo()).toBe(null);
});

test('恢复或新开一局后不能撤销回上一局', () => {
  const history = new SudokuHistory();
  history.record([1], [2]);
  history.clear();
  expect(history.undo()).toBe(null);
});
