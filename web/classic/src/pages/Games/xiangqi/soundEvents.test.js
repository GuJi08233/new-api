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
import { xiangqiSoundTransition } from './soundEvents';

const moves = [
  { ply: 1, color: 'red', piece: 'C', from: [1, 2], to: [1, 9], captured: 'n' },
  {
    ply: 2,
    color: 'black',
    piece: 'r',
    from: [0, 9],
    to: [1, 9],
    captured: 'C',
  },
  { ply: 3, color: 'red', piece: 'P', from: [0, 3], to: [0, 4] },
  { ply: 4, color: 'black', piece: 'p', from: [2, 6], to: [2, 5] },
];

const stateAt = (plies) => ({ plies, history: moves.slice(0, plies) });
const baseline = (plies, gameId = 'room:1') =>
  xiangqiSoundTransition(null, stateAt(plies), gameId).cursor;

describe('象棋音效只响应新增的服务端棋步', () => {
  test('首次恢复已有棋局和进入另一棋局均静音', () => {
    expect(xiangqiSoundTransition(null, stateAt(4), 'room:1').sounds).toEqual(
      [],
    );
    expect(
      xiangqiSoundTransition(baseline(2), stateAt(4), 'room:2'),
    ).toMatchObject({ sounds: [], reset: true });
  });

  test('同一轮询、选择变化和记谱文字补全不重复播放', () => {
    const previous = baseline(2);
    for (const state of [
      stateAt(2),
      { ...stateAt(2), selected: [1, 9] },
      {
        plies: 2,
        history: moves.slice(0, 2).map((m) => ({ ...m, notation: '谱' })),
      },
    ]) {
      expect(xiangqiSoundTransition(previous, state, 'room:1')).toMatchObject({
        sounds: [],
        reset: false,
      });
    }
  });

  test('普通走棋和吃子按新增历史分类，AI 一次回复的两步保持顺序', () => {
    expect(
      xiangqiSoundTransition(baseline(0), stateAt(1), 'room:1').sounds,
    ).toEqual(['capture']);
    expect(
      xiangqiSoundTransition(baseline(2), stateAt(4), 'room:1').sounds,
    ).toEqual(['move', 'move']);
    // 黑方玩家吃炮后，AI 回以普通进兵；最后一手没有吃子仍需先播放玩家吃子。
    const reply = xiangqiSoundTransition(baseline(1), stateAt(3), 'room:1');
    expect(reply.sounds).toEqual(['capture', 'move']);
    expect(
      xiangqiSoundTransition(reply.cursor, stateAt(3), 'room:1').sounds,
    ).toEqual([]);
  });

  test('悔棋清除排队并建立新基线，重新落子只播放新的一手', () => {
    const undone = xiangqiSoundTransition(baseline(4), stateAt(2), 'room:1');
    expect(undone).toMatchObject({ sounds: [], reset: true });
    const changed = { ...moves[2], from: [2, 3], to: [2, 4] };
    const replayed = { plies: 3, history: [...moves.slice(0, 2), changed] };
    expect(
      xiangqiSoundTransition(undone.cursor, replayed, 'room:1').sounds,
    ).toEqual(['move']);
    expect(
      xiangqiSoundTransition(baseline(3), replayed, 'room:1'),
    ).toMatchObject({ sounds: [], reset: true });
  });

  test('旧存档仅 last 时不猜吃子，升级后的连续新棋步可以发声', () => {
    const previous = xiangqiSoundTransition(
      null,
      { plies: 2, last: { from: [0, 9], to: [1, 9] } },
      'room:1',
    ).cursor;
    expect(
      xiangqiSoundTransition(
        previous,
        { plies: 3, history: [moves[2]] },
        'room:1',
      ).sounds,
    ).toEqual(['move']);
    expect(
      xiangqiSoundTransition(previous, stateAt(2), 'room:1'),
    ).toMatchObject({ sounds: [], reset: true });
  });

  test('缺失棋步、步数不匹配与局面清空都静音并清除队列', () => {
    for (const state of [
      { plies: 4, history: [moves[3]] },
      { plies: 3, history: [...moves.slice(0, 2), moves[3]] },
      { plies: 3, history: moves.slice(0, 2) },
      null,
    ]) {
      expect(
        xiangqiSoundTransition(baseline(2), state, 'room:1'),
      ).toMatchObject({ sounds: [], reset: true });
    }
  });
});
