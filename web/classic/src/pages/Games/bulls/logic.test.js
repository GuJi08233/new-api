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
import { consistentWith, scoreGuess } from './logic';

describe('scoreGuess', () => {
  test('counts exact and misplaced digits like the server', () => {
    expect(scoreGuess('1234', '1234')).toEqual({ a: 4, b: 0 });
    expect(scoreGuess('1234', '4321')).toEqual({ a: 0, b: 4 });
    expect(scoreGuess('0123', '0456')).toEqual({ a: 1, b: 0 });
    expect(scoreGuess('052', '250')).toEqual({ a: 1, b: 2 });
  });
});

describe('consistentWith', () => {
  test('rejects guesses that contradict earlier clues', () => {
    const history = [{ guess: '123', a: 0, b: 1 }];
    expect(consistentWith('345', history)).toBe(true);
    expect(consistentWith('456', history)).toBe(false);
    expect(consistentWith('132', history)).toBe(false);
  });
});
