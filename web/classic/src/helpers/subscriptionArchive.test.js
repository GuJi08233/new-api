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
import { partitionSubscriptionArchive } from './subscriptionArchive';

test.each([
  ['active', 101, true],
  ['inactive', 101, true],
  ['active', 100, false],
  ['inactive', 99, false],
  ['cancelled', 101, false],
  ['expired', 101, false],
  ['active', 0, false],
])('状态 %s、到期时间 %d 决定是否仍是当前订阅', (status, endTime, current) => {
  const summary = { subscription: { status, end_time: endTime } };
  const result = partitionSubscriptionArchive([summary], 100);
  expect(result.currentSubscriptions).toEqual(current ? [summary] : []);
  expect(result.archivedSubscriptions).toEqual(current ? [] : [summary]);
});

test('保留订阅的完整信息及原顺序，只统计当前生效和未激活订阅', () => {
  const subscriptions = [
    { subscription: { id: 1, status: 'active', end_time: 140 } },
    {
      subscription: { id: 2, status: 'cancelled', end_time: 200 },
      plan_title: 'Archived plan',
      quota_tiers: '[{"period":"daily","limit":100}]',
      tier_usages: [{ tier_index: 0, usage_in_period: 75 }],
    },
    { subscription: { id: 3, status: 'inactive', end_time: 120 } },
    { subscription: { id: 4, status: 'active', end_time: 100 } },
  ];
  const result = partitionSubscriptionArchive(subscriptions, 100);
  expect(result.currentSubscriptions).toEqual([
    subscriptions[0],
    subscriptions[2],
  ]);
  expect(result.archivedSubscriptions).toEqual([
    subscriptions[1],
    subscriptions[3],
  ]);
  expect(result.archivedSubscriptions[0]).toBe(subscriptions[1]);
  expect(result.activeCount).toBe(1);
  expect(result.inactiveCount).toBe(1);
  expect(result.nextExpiry).toBe(120);
});

test('所有订阅结束后没有生效计数和下一次到期定时器', () => {
  const result = partitionSubscriptionArchive(
    [
      { subscription: { status: 'active', end_time: 99 } },
      { subscription: { status: 'inactive', end_time: 100 } },
    ],
    100,
  );
  expect(result.currentSubscriptions).toEqual([]);
  expect(result.archivedSubscriptions).toHaveLength(2);
  expect(result.activeCount).toBe(0);
  expect(result.inactiveCount).toBe(0);
  expect(result.nextExpiry).toBe(0);
});
