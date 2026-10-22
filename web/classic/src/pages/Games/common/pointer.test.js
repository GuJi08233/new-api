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
import { beginSwipe, finishSwipe } from './pointer';

test('第二根手指按下和松开都不覆盖第一根手指的滑动', () => {
  const first = beginSwipe(null, {
    pointerId: 1,
    isPrimary: true,
    clientX: 10,
    clientY: 20,
  });
  const second = { pointerId: 2, isPrimary: false, clientX: 70, clientY: 80 };
  expect(beginSwipe(first, second)).toBe(first);
  expect(finishSwipe(first, second, 20)).toEqual({
    current: first,
    delta: null,
  });
  expect(
    finishSwipe(first, { pointerId: 1, clientX: 50, clientY: 25 }, 20),
  ).toEqual({ current: null, delta: { x: 40, y: 5 } });
});

test('非主指针不能开始滑动，小于阈值的点按不会移动棋盘', () => {
  expect(beginSwipe(null, { pointerId: 2, isPrimary: false })).toBe(null);
  const first = beginSwipe(null, { pointerId: 1, clientX: 10, clientY: 20 });
  expect(
    finishSwipe(first, { pointerId: 1, clientX: 15, clientY: 25 }, 20),
  ).toEqual({ current: null, delta: null });
});
