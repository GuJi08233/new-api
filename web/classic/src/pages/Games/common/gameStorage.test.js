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
import { createReplayStore, REPLAY_TTL_MS } from './gameStorage';

export function replayStorage() {
  const data = new Map();
  return {
    get length() {
      return data.size;
    },
    key: (index) => [...data.keys()][index] ?? null,
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => data.set(key, value),
    removeItem: (key) => data.delete(key),
  };
}

describe('待结算录像的账户隔离与生命周期', () => {
  test('刷新能恢复同账户同游戏的完整录像，其他账户或游戏不能读取', () => {
    const storage = replayStorage();
    const make = (user, game) =>
      createReplayStore(
        () => storage,
        user,
        game,
        () => 1000,
      );
    const inputs = [
      [0, 1],
      [10, 2],
    ];
    expect(
      make(1, 'snake').save({ session_id: 7, inputs, steps: 12 }, 900).ok,
    ).toBe(true);
    inputs.push([12, 3]);
    expect(make(1, 'snake').list().value[0].payload).toEqual({
      session_id: 7,
      inputs: [
        [0, 1],
        [10, 2],
      ],
      steps: 12,
    });
    expect(make(2, 'snake').list().value).toEqual([]);
    expect(make(1, 'tetris').list().value).toEqual([]);
    make(2, 'snake').save({ session_id: 7, inputs: [], steps: 1 }, 900);
    make(1, 'snake').remove(7);
    expect(make(1, 'snake').list().value).toEqual([]);
    expect(make(2, 'snake').list().value).toHaveLength(1);
  });

  test('TTL 到期不会恢复过期录像；更新检查点不会延长对局寿命', () => {
    const storage = replayStorage();
    let now = 1000;
    const store = createReplayStore(
      () => storage,
      1,
      '2048',
      () => now,
    );
    store.save({ session_id: 8, moves: '012' }, 1000);
    now += REPLAY_TTL_MS - 1;
    store.save({ session_id: 8, moves: '0123' }, 1000);
    expect(store.list().value).toHaveLength(1);
    now += 1;
    expect(store.list().value).toEqual([]);
    expect(storage.length).toBe(0);
  });

  test('损坏或不兼容的录像被跳过，不阻断其他对局的恢复', () => {
    const storage = replayStorage();
    const store = createReplayStore(
      () => storage,
      1,
      'snake',
      () => 1000,
    );
    storage.setItem('game-replay:v1:1:snake:3', '{broken');
    storage.setItem('game-replay:v1:1:snake:4', JSON.stringify({ version: 9 }));
    store.save({ session_id: 5, steps: 3, inputs: [] }, 900);
    expect(store.list().value.map((record) => record.sessionId)).toEqual([5]);
  });

  test('存储写满仍保留最新内存录像，并报告不能刷新恢复', () => {
    const storage = replayStorage();
    const store = createReplayStore(
      () => storage,
      1,
      'snake',
      () => 1000,
    );
    store.save({ session_id: 7, steps: 1, inputs: [] }, 900);
    storage.setItem = () => {
      throw new Error('QuotaExceededError');
    };
    expect(
      store.save({ session_id: 7, steps: 12, inputs: [[2, 1]] }, 900).ok,
    ).toBe(false);
    expect(store.list().value[0].payload.steps).toBe(12);
  });

  test('浏览器拒绝整个存储 API 也不会抛出到游戏页面', () => {
    const store = createReplayStore(
      () => {
        throw new Error('SecurityError');
      },
      1,
      'flappy',
      () => 1000,
    );
    expect(store.save({ session_id: 9, ticks: 10, flaps: [0] }, 900).ok).toBe(
      false,
    );
    expect(store.list().value).toHaveLength(1);
    expect(store.remove(9).ok).toBe(false);
    expect(store.list().value).toEqual([]);
  });
});
