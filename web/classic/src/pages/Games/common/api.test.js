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

import { beforeAll, beforeEach, describe, expect, mock, test } from 'bun:test';

// 服务端的回应按顺序排好：'drop' 是连接断了没有答复，其余是 { success, data | message }。
let replies = [];
const sent = [];
mock.module('../../../helpers', () => ({
  showError: () => {},
  API: {
    post: (url, body) => {
      sent.push({ url, body });
      const reply = replies.shift();
      if (reply === 'drop') return Promise.reject(new Error('Network Error'));
      return Promise.resolve({ data: reply });
    },
  },
}));

const t = (key) => key;
// 模块要在 mock.module 之后再加载，才会拿到假的 API。
let gamePostOnce;
beforeAll(async () => {
  ({ gamePostOnce } = await import('./api'));
});

describe('gamePostOnce', () => {
  beforeEach(() => {
    replies = [];
    sent.length = 0;
  });

  // 第一次没有答复：用同一个请求编号重发一次，服务端认出做过的那一次，交回 replayed。
  test('a dropped request is sent again with the same request id', async () => {
    replies = ['drop', { success: true, data: { replayed: true } }];
    const result = await gamePostOnce(
      '/api/game/wendao/hero/action',
      { action: 'visit' },
      t,
    );
    expect(result).toEqual({ data: { replayed: true } });
    expect(sent).toHaveLength(2);
    expect(sent[0].body.action).toBe('visit');
    expect(sent[0].body.request_id).toBeTruthy();
    expect(sent[1].body).toEqual(sent[0].body);
  });

  // 服务端答复了拒绝原因就不重发；每次操作都换一个新编号。
  test('a refusal is not retried and every action gets its own id', async () => {
    replies = [
      { success: false, message: 'not enough' },
      { success: true, data: {} },
    ];
    const refused = await gamePostOnce(
      '/api/game/wendao/sect/action',
      { action: 'build' },
      t,
    );
    expect(refused).toEqual({ error: 'not enough' });
    await gamePostOnce('/api/game/wendao/sect/action', { action: 'build' }, t);
    expect(sent).toHaveLength(2);
    expect(sent[1].body.request_id).not.toBe(sent[0].body.request_id);
  });

  // 重发也断了：照实告诉页面没有答复，由页面去读现在的样子。
  test('two drops in a row report a network failure', async () => {
    replies = ['drop', 'drop'];
    const result = await gamePostOnce(
      '/api/game/wendao/sect/shop/buy',
      { item: 'qi', count: 1 },
      t,
    );
    expect(result.network).toBe(true);
    expect(sent).toHaveLength(2);
  });
});
