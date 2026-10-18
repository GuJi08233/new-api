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
  formatXiangqiRecord,
  getXiangqiCaptures,
  groupXiangqiHistory,
  recentXiangqiRounds,
  xiangqiMoveLabel,
} from './records';

const opening = [
  { ply: 1, color: 'red', notation: '炮二平五' },
  { ply: 2, color: 'black', notation: '马8进7' },
  { ply: 3, color: 'red', notation: '马二进三' },
  { ply: 4, color: 'black', notation: '车9平8' },
  { ply: 5, color: 'red', notation: '车一平二' },
  { ply: 6, color: 'black', notation: '炮8进4' },
];

describe('中国象棋回合棋谱', () => {
  test('规则中的开局按红黑双方配对，复制时保持从先到后的顺序', () => {
    expect(groupXiangqiHistory(opening)).toEqual([
      { number: 1, red: opening[0], black: opening[1] },
      { number: 2, red: opening[2], black: opening[3] },
      { number: 3, red: opening[4], black: opening[5] },
    ]);
    expect(formatXiangqiRecord(opening)).toBe(
      '1. 炮二平五  马8进7\n2. 马二进三  车9平8\n3. 车一平二  炮8进4',
    );
  });

  test('旧局从黑方开始记录时保留真实回合和缺失位置', () => {
    expect(formatXiangqiRecord(opening.slice(3, 5))).toBe(
      '2. …  车9平8\n3. 车一平二  …',
    );
  });

  test('悔棋后的记录不保留被撤回的应着，空局不产生虚假回合', () => {
    expect(formatXiangqiRecord(opening.slice(0, 3))).toBe(
      '1. 炮二平五  马8进7\n2. 马二进三  …',
    );
    expect(groupXiangqiHistory([])).toEqual([]);
    expect(formatXiangqiRecord([])).toBe('');
  });

  test('旧接口没有记谱时保留固定坐标，存在记谱时不重新推测', () => {
    const entry = { piece: 'C', from: [7, 2], to: [4, 2] };
    expect(xiangqiMoveLabel(entry)).toBe('炮 H3 → E3');
    expect(xiangqiMoveLabel({ ...entry, notation: '前炮平五' })).toBe(
      '前炮平五',
    );
  });

  test('区分双方吃到的棋子，复制棋谱保留每一着吃子信息', () => {
    const history = [
      { ply: 1, color: 'red', notation: '炮八进七', piece: 'C', captured: 'n' },
      { ply: 2, color: 'black', notation: '车1平2', piece: 'r', captured: 'C' },
      { ply: 3, color: 'red', notation: '兵九进一', piece: 'P' },
    ];
    expect(getXiangqiCaptures(history)).toEqual({
      red: [history[0]],
      black: [history[1]],
    });
    expect(formatXiangqiRecord(history)).toBe(
      '1. 炮八进七（吃马）  车1平2（吃炮）\n2. 兵九进一  …',
    );
    expect(formatXiangqiRecord(history, (piece) => `capture ${piece}`)).toBe(
      '1. 炮八进七（capture n）  车1平2（capture C）\n2. 兵九进一  …',
    );
    expect(getXiangqiCaptures(history.slice(0, 1))).toEqual({
      red: [history[0]],
      black: [],
    });
    expect(getXiangqiCaptures([])).toEqual({ red: [], black: [] });
  });
});

describe('棋谱默认只列最近几回合', () => {
  const rounds = groupXiangqiHistory(opening);

  test('回合数不超过上限时原样返回，不必出现展开按钮', () => {
    expect(recentXiangqiRounds(rounds, 3, false)).toEqual(rounds);
    expect(recentXiangqiRounds(rounds, 3, true)).toEqual(rounds);
  });

  test('收起时保留最近的回合并维持先后顺序，展开后回到全部', () => {
    const recent = recentXiangqiRounds(rounds, 2, false);
    expect(recent.map((round) => round.number)).toEqual([2, 3]);
    expect(recent[recent.length - 1]).toEqual(rounds[rounds.length - 1]);
    expect(recentXiangqiRounds(rounds, 2, true)).toEqual(rounds);
    expect(recentXiangqiRounds([], 6, false)).toEqual([]);
  });
});
