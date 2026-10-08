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

import assert from 'node:assert/strict';
import { describe, test } from 'node:test';
import { sessionKinds, sessionState, sessionWeek } from './marketSession.js';

// 时段图按本地时间画，测试固定在东八区。
process.env.TZ = 'Asia/Shanghai';

const at = (iso) => Date.parse(iso);
const iso = (ms) => new Date(ms).toISOString();

describe('sessionState', () => {
  const cases = [
    // [市场, 时刻, 时段, 起, 止, 倒计时目标]
    [
      'us',
      '2026-10-09T13:00:00Z',
      'pre',
      '2026-10-09T08:00:00.000Z',
      '2026-10-09T13:30:00.000Z',
      '2026-10-09T13:30:00.000Z',
    ],
    [
      'us',
      '2026-10-09T14:00:00Z',
      'open',
      '2026-10-09T13:30:00.000Z',
      '2026-10-09T20:00:00.000Z',
      '2026-10-09T20:00:00.000Z',
    ],
    [
      'us',
      '2026-10-09T21:00:00Z',
      'post',
      '2026-10-09T20:00:00.000Z',
      '2026-10-10T00:00:00.000Z',
      '2026-10-12T13:30:00.000Z',
    ],
    // 周五 20:00(美东)收盘后到周日 20:00 夜盘开始都是休市。
    [
      'us',
      '2026-10-10T12:00:00Z',
      'closed',
      '2026-10-10T00:00:00.000Z',
      '2026-10-12T00:00:00.000Z',
      '2026-10-12T13:30:00.000Z',
    ],
    [
      'us',
      '2026-10-12T01:00:00Z',
      'overnight',
      '2026-10-12T00:00:00.000Z',
      '2026-10-12T08:00:00.000Z',
      '2026-10-12T13:30:00.000Z',
    ],
    // 2026-11-01 夏令时结束：周日夜盘 20:00 EST 是 UTC 01:00，盘中 09:30 EST 是 UTC 14:30。
    [
      'us',
      '2026-11-02T01:30:00Z',
      'overnight',
      '2026-11-02T01:00:00.000Z',
      '2026-11-02T09:00:00.000Z',
      '2026-11-02T14:30:00.000Z',
    ],
    // 2026-03-08 夏令时开始：当晚的夜盘按 20:00 EDT 算。
    [
      'us',
      '2026-03-09T00:30:00Z',
      'overnight',
      '2026-03-09T00:00:00.000Z',
      '2026-03-09T08:00:00.000Z',
      '2026-03-09T13:30:00.000Z',
    ],
    // 韩国交易所连续竞价 09:00–15:20(首尔)，15:20 之后是收盘集合竞价，算休市。
    [
      'krx',
      '2026-10-09T01:00:00Z',
      'open',
      '2026-10-09T00:00:00.000Z',
      '2026-10-09T06:20:00.000Z',
      '2026-10-09T06:20:00.000Z',
    ],
    [
      'krx',
      '2026-10-09T06:25:00Z',
      'closed',
      '2026-10-09T06:20:00.000Z',
      '2026-10-12T00:00:00.000Z',
      '2026-10-12T00:00:00.000Z',
    ],
    // CME 每天 16:00–17:00(芝加哥)休息一小时，周五 16:00 到周日 17:00 休市。
    [
      'cme',
      '2026-10-08T21:30:00Z',
      'closed',
      '2026-10-08T21:00:00.000Z',
      '2026-10-08T22:00:00.000Z',
      '2026-10-08T22:00:00.000Z',
    ],
    [
      'cme',
      '2026-10-10T12:00:00Z',
      'closed',
      '2026-10-09T21:00:00.000Z',
      '2026-10-11T22:00:00.000Z',
      '2026-10-11T22:00:00.000Z',
    ],
    [
      'cme',
      '2026-10-11T22:30:00Z',
      'open',
      '2026-10-11T22:00:00.000Z',
      '2026-10-12T21:00:00.000Z',
      '2026-10-12T21:00:00.000Z',
    ],
  ];
  cases.forEach(([market, now, kind, start, end, target]) => {
    test(`${market} ${now}`, () => {
      const state = sessionState(market, at(now));
      assert.deepEqual(
        {
          kind: state.kind,
          start: iso(state.start),
          end: iso(state.end),
          target: iso(state.targetAt),
        },
        { kind, start, end, target },
      );
    });
  });

  test('crypto has no session', () => {
    assert.equal(sessionState('', at('2026-10-09T13:00:00Z')), null);
    assert.deepEqual(sessionKinds(''), []);
  });
});

describe('sessionWeek', () => {
  test('us week in UTC+8 starts the overnight session on Monday 08:00', () => {
    const week = sessionWeek('us', at('2026-10-09T13:00:00Z'));
    assert.deepEqual(week.ticks, [4 * 60, 8 * 60, 16 * 60, 21 * 60 + 30]);
    assert.equal(week.rows.length, 7);
    assert.equal(
      new Date(week.rows[0].dayStart).toISOString(),
      '2026-10-04T16:00:00.000Z',
    );
    assert.deepEqual(week.rows[0].bars, [
      { kind: 'overnight', from: 480, to: 960 },
      { kind: 'pre', from: 960, to: 1290 },
      { kind: 'open', from: 1290, to: 1440 },
    ]);
    assert.deepEqual(week.rows[5].bars, [
      { kind: 'open', from: 0, to: 240 },
      { kind: 'post', from: 240, to: 480 },
    ]);
    assert.deepEqual(week.rows[6].bars, []);
  });

  test('legend lists only the sessions a market has', () => {
    assert.deepEqual(sessionKinds('us'), [
      'pre',
      'open',
      'post',
      'overnight',
      'closed',
    ]);
    assert.deepEqual(sessionKinds('krx'), ['open', 'closed']);
  });
});
