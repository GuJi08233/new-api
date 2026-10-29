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
import { basicStrategy } from './strategy';

const card = (rank) => ({ rank, suit: 0 });
// 一手牌：ranks 是点数(A 为 1)，total、soft 按规则算出来。
function hand(...ranks) {
  let total = 0;
  let aces = 0;
  for (const rank of ranks) {
    if (rank === 1) aces++;
    total += rank === 1 ? 11 : Math.min(rank, 10);
  }
  while (total > 21 && aces > 0) {
    total -= 10;
    aces--;
  }
  return { cards: ranks.map(card), total, soft: aces > 0 };
}
const ALL = ['hit', 'stand', 'double', 'split'];
const PLAY = ['hit', 'stand'];

describe('basicStrategy', () => {
  test.each([
    // [手牌, 庄家明牌, 可做的动作, 建议]
    [[1, 1], 6, ALL, 'split'],
    [[8, 8], 10, ALL, 'split'],
    [[10, 13], 6, ALL, 'stand'],
    [[5, 5], 9, ALL, 'double'],
    [[9, 9], 7, ALL, 'stand'],
    [[6, 5], 10, ALL, 'double'],
    [[6, 5], 1, ALL, 'hit'],
    [[10, 6], 6, ALL, 'stand'],
    [[10, 6], 7, ALL, 'hit'],
    [[10, 2], 3, ALL, 'hit'],
    [[10, 2], 4, ALL, 'stand'],
    [[1, 7], 4, ALL, 'double'],
    [[1, 7], 4, PLAY, 'stand'],
    [[1, 7], 9, ALL, 'hit'],
    [[1, 6], 2, ALL, 'hit'],
    [[1, 1], 6, PLAY, 'hit'],
    [[10, 7], 1, ALL, 'stand'],
  ])('%p against %p', (ranks, upcard, actions, expected) => {
    expect(basicStrategy(hand(...ranks), card(upcard), actions)).toBe(expected);
  });

  test('falls back when doubling is not allowed', () => {
    expect(basicStrategy(hand(4, 3, 4), card(6), PLAY)).toBe('hit');
  });
});
