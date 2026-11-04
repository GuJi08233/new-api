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
  formatSubscriptionPeriodDuration,
  parseSubscriptionPeriodDuration,
} from './subscriptionDuration';

describe('订阅周期时长输入', () => {
  test.each([
    ['5h', 18000],
    ['1h', 3600],
    ['1d', 86400],
    ['30m', 1800],
    ['90s', 90],
    ['1w', 604800],
    ['1h30m', 5400],
    [' 1 D  2 H 30 M 5 S ', 95405],
    ['1.5h', 5400],
    ['.5h', 1800],
    ['0.07h', 252],
    ['0.5m', 30],
    ['1.000s', 1],
    ['0h1m', 60],
    ['9007199254740991s', Number.MAX_SAFE_INTEGER],
  ])('将 %s 精确转换为秒', (input, seconds) => {
    expect(parseSubscriptionPeriodDuration(input)).toBe(seconds);
  });

  test.each([
    '',
    '   ',
    '3600',
    '-1h',
    '1h-30m',
    '+1h',
    '0s',
    '0h0m',
    '1mo',
    '1hour',
    '1h later',
    'later 1h',
    '1h 30',
    '1h,30m',
    '1e3s',
    'Infinityh',
    'NaNh',
    '0.5s',
    '0.001h',
    '1.0000000000000001h',
    '9007199254740992s',
    '9007199254740991s1s',
    null,
    undefined,
    3600,
  ])('拒绝不完整、无效或不能精确表示的周期 %j', (input) => {
    expect(parseSubscriptionPeriodDuration(input)).toBeNull();
  });
});

describe('订阅周期时长回显', () => {
  test.each([
    [90000, '1d1h'],
    [3600, '1h'],
    [90, '1m30s'],
    [604800, '7d'],
    [2592000, '30d'],
    [1, '1s'],
  ])('将 %d 秒回显为 %s', (seconds, expected) => {
    expect(formatSubscriptionPeriodDuration(seconds)).toBe(expected);
  });

  test.each([0, -1, 0.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1, null])(
    '无效秒数 %j 不生成周期文本',
    (seconds) => {
      expect(formatSubscriptionPeriodDuration(seconds)).toBe('');
    },
  );

  test.each([1, 59, 60, 3601, 90000, Number.MAX_SAFE_INTEGER])(
    '已保存的整数秒 %d 格式化后可无损解析',
    (seconds) => {
      expect(
        parseSubscriptionPeriodDuration(
          formatSubscriptionPeriodDuration(seconds),
        ),
      ).toBe(seconds);
    },
  );
});
