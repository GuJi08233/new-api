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
import { createReplayStore } from './gameStorage';
import { submitPendingReplay } from './usePendingReplayRequest';

function fixture(game = 'snake') {
  const data = new Map();
  const storage = {
    get length() {
      return data.size;
    },
    key: (index) => [...data.keys()][index],
    getItem: (key) => data.get(key),
    setItem: (key, value) => data.set(key, value),
    removeItem: (key) => data.delete(key),
  };
  const store = createReplayStore(
    () => storage,
    1,
    game,
    () => 1000,
  );
  const payload = { session_id: 7, ticks: 120, inputs: [[1, 2]] };
  store.save(payload, 900);
  return { store, payload };
}

test.each([undefined, 401, 429, 503])(
  '临时失败 %s 保留可原样重交的录像',
  async (status) => {
    const { store, payload } = fixture();
    const response = await submitPendingReplay(
      store,
      'snake',
      payload,
      async () => {
        throw { response: status ? { status } : undefined };
      },
    );
    expect(response.network || response.limited).toBe(true);
    expect(store.list().value[0].payload).toEqual(payload);
  },
);

test('服务端已结算但响应丢失后重交同一会话，确认后清理', async () => {
  const { store, payload } = fixture();
  let first = true;
  const received = [];
  const post = async (_url, body) => {
    received.push(body);
    if (first) {
      first = false;
      throw new Error('response lost');
    }
    return { data: { success: true, data: { session_id: 7, reward: 1 } } };
  };
  await submitPendingReplay(store, 'snake', payload, post);
  const restored = store.list().value[0].payload;
  const response = await submitPendingReplay(store, 'snake', restored, post);
  expect(received).toEqual([payload, payload]);
  expect(response.data.reward).toBe(1);
  expect(store.list().value).toEqual([]);
});

test('明确拒绝后清理，而俄罗斯方块保留查询参数会话协议', async () => {
  const { store, payload } = fixture('tetris');
  const response = await submitPendingReplay(
    store,
    'tetris',
    payload,
    async (url, body, options) => {
      expect(url).toBe('/api/game/tetris/finish');
      expect(body).toEqual({ ticks: 120, inputs: [[1, 2]] });
      expect(options.params).toEqual({ session_id: 7 });
      return { data: { success: false, message: 'expired' } };
    },
  );
  expect(response.error).toBe('expired');
  expect(store.list().value).toEqual([]);
});
