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
import { checkAnswer, evaluateExpression, formatFraction } from './expr';

// 本地判定要和服务端 pkg/twentyfour.Check 一致，否则会把服务端认可的算式拦下来，或者放过会被拒绝的算式。
describe('checkAnswer', () => {
  test('accepts exact fractions and every operator spelling', () => {
    expect(checkAnswer('8/(3-8/3)', [3, 3, 8, 8]).ok).toBe(true);
    expect(checkAnswer('（5−1÷5）×5', [1, 5, 5, 5]).ok).toBe(true);
    expect(checkAnswer('6x4 + 13 - 13', [4, 6, 13, 13]).ok).toBe(true);
  });

  test('tells which part is wrong', () => {
    expect(checkAnswer('3*8+', [3, 8, 1, 1]).reason).toBe('syntax');
    expect(checkAnswer('3*8', [3, 8, 1, 1]).reason).toBe('numbers');
    expect(checkAnswer('3*8*1*2', [3, 8, 1, 2]).reason).toBe('value');
    expect(checkAnswer('(1+2', [1, 2, 3, 4]).reason).toBe('syntax');
    expect(checkAnswer('4/(2-2)*1', [4, 2, 2, 1]).reason).toBe('syntax');
    expect(checkAnswer('123+1', [1, 2, 3, 1]).reason).toBe('syntax');
  });

  test('shows intermediate values as fractions', () => {
    expect(formatFraction(evaluateExpression('8/3').value)).toBe('8/3');
    expect(formatFraction(evaluateExpression('2-8/3').value)).toBe('-2/3');
    expect(formatFraction(evaluateExpression('6/3').value)).toBe('2');
  });
});
