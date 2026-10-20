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
import {
  classifyCombination,
  selectedHintIndex,
  selectionFeedback,
} from './selection';

// 使用具体点数生成唯一花色的牌号；这些牌型和禁例沿用 Go 规则测试的协议样例。
function cardsOf(...ranks) {
  const used = new Map();
  return ranks.map((rank) => {
    const suit = used.get(rank) || 0;
    used.set(rank, suit + 1);
    return rank >= 16 ? rank + 36 : (rank - 3) * 4 + suit;
  });
}

describe('手选牌按服务端规则识别全部牌型', () => {
  const cases = [
    ['single', [17], 17, 1],
    ['pair', [15, 15], 15, 1],
    ['triple', [14, 14, 14], 14, 1],
    ['triple_single', [3, 3, 3, 16], 3, 1],
    ['triple_pair', [3, 3, 3, 15, 15], 3, 1],
    ['straight', [10, 11, 12, 13, 14], 14, 5],
    ['pair_straight', [11, 11, 12, 12, 13, 13], 13, 3],
    ['airplane', [3, 3, 3, 4, 4, 4], 4, 2],
    ['airplane_single', [3, 3, 3, 4, 4, 4, 6, 7], 4, 2],
    ['airplane_single', [3, 3, 3, 4, 4, 4, 6, 6], 4, 2],
    ['airplane_pair', [3, 3, 3, 4, 4, 4, 6, 6, 7, 7], 4, 2],
    ['four_two', [3, 3, 3, 3, 16, 17], 3, 1],
    ['four_two', [3, 3, 3, 3, 4, 4], 3, 1],
    ['four_two_pairs', [3, 3, 3, 3, 4, 4, 5, 5], 3, 1],
    ['bomb', [15, 15, 15, 15], 15, 1],
    ['rocket', [16, 17], 17, 1],
    [
      'airplane_pair',
      [3, 3, 3, 4, 4, 4, 5, 5, 5, 6, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10],
      6,
      4,
    ],
  ];
  for (const [type, ranks, rank, chain] of cases) {
    test(`${type}: ${ranks.join(',')}`, () => {
      const cards = cardsOf(...ranks).reverse();
      const before = [...cards];
      expect(classifyCombination(cards)).toEqual({ type, rank, chain });
      expect(cards).toEqual(before);
      expect(selectionFeedback(cards, null).reason).toBeNull();
    });
  }
});

describe('选牌未完成或违反翅膀、连续点数约束时不能出牌', () => {
  const invalid = [
    ['未选牌', []],
    ['不存在的负牌号', [-1]],
    ['不存在的牌号', [54]],
    ['非整数牌号', [0.5]],
    ['同一张牌重复', [0, 0]],
    ['超过手牌上限', Array.from({ length: 21 }, (_, index) => index)],
    ['混合对子', cardsOf(3, 4)],
    ['顺子太短', cardsOf(3, 4, 5, 6)],
    ['顺子不能带二', cardsOf(11, 12, 13, 14, 15)],
    ['顺子不能带王', cardsOf(13, 14, 15, 16, 17)],
    ['连对太短', cardsOf(3, 3, 4, 4)],
    ['连对不能带二', cardsOf(13, 13, 14, 14, 15, 15)],
    ['飞机不能带二', cardsOf(14, 14, 14, 15, 15, 15)],
    ['飞机机身不连续', cardsOf(3, 3, 3, 5, 5, 5)],
    ['翅膀不能借用机身', cardsOf(3, 3, 3, 3, 4, 4, 4, 4)],
    ['翅膀不能是三张', cardsOf(3, 3, 3, 4, 4, 4, 5, 5, 5, 7, 7, 7)],
    ['飞机对子翅膀不能带两张王', cardsOf(3, 3, 3, 4, 4, 4, 6, 6, 16, 17)],
    ['四带二缺一张', cardsOf(3, 3, 3, 3, 4)],
    ['四带两对必须是两个对子', cardsOf(3, 3, 3, 3, 4, 4, 5, 6)],
  ];
  for (const [name, cards] of invalid) {
    test(name, () => {
      expect(classifyCombination(cards)).toBeNull();
      expect(selectionFeedback(cards, null).reason).toBe(
        cards.length === 0 ? 'empty' : 'invalid',
      );
    });
  }
});

describe('即时反馈区分牌型不符、压不过和可出牌', () => {
  const cases = [
    ['同点数不能压过', [7, 7], [7, 7], 'rank'],
    ['更大的对子', [8, 8], [7, 7], null],
    ['单张不能压对子', [17], [7, 7], 'shape'],
    ['顺子长度必须一致', [6, 7, 8, 9, 10, 11], [3, 4, 5, 6, 7], 'shape'],
    ['附件点数不决定大小', [4, 4, 4, 3], [3, 3, 3, 17], null],
    ['四带二不是炸弹', [3, 3, 3, 3, 4, 4], [5], 'shape'],
    ['炸弹压普通牌', [3, 3, 3, 3], [17], null],
    ['小炸弹不能压大炸弹', [3, 3, 3, 3], [4, 4, 4, 4], 'rank'],
    ['王炸压炸弹', [16, 17], [15, 15, 15, 15], null],
    ['炸弹不能压王炸', [15, 15, 15, 15], [16, 17], 'rank'],
  ];
  for (const [name, selected, previous, reason] of cases) {
    test(name, () => {
      const prior = classifyCombination(cardsOf(...previous));
      expect(prior).not.toBeNull();
      expect(selectionFeedback(cardsOf(...selected), prior).reason).toBe(
        reason,
      );
    });
  }
});

describe('提示序号只标记仍然选中的候选', () => {
  const hints = [cardsOf(3, 3), cardsOf(4, 4)];
  const choice = { key: 'turn:2', index: 1 };

  test('手牌排序不影响当前提示序号', () => {
    expect(
      selectedHintIndex([...hints[1]].reverse(), hints, choice, 'turn:2'),
    ).toBe(1);
  });

  test('手动改牌或清空后不显示旧序号', () => {
    expect(selectedHintIndex(cardsOf(4), hints, choice, 'turn:2')).toBe(-1);
    expect(selectedHintIndex(hints[0], hints, choice, 'turn:2')).toBe(-1);
    expect(selectedHintIndex([], hints, choice, 'turn:2')).toBe(-1);
  });

  test('轮次变化或候选消失后旧提示失效', () => {
    expect(selectedHintIndex(hints[1], hints, choice, 'turn:3')).toBe(-1);
    expect(selectedHintIndex(hints[1], [], choice, 'turn:2')).toBe(-1);
  });
});
