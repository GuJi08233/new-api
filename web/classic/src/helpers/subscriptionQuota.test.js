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
  buildSubscriptionQuotaConfig,
  buildSubscriptionQuotaTiers,
  getSubscriptionQuotaLimits,
  getSubscriptionQuotaMode,
  getSubscriptionQuotaUsage,
} from './subscriptionQuota';

describe('订阅额度配置', () => {
  test.each([
    [undefined, 'multi'],
    [{ total_amount: 100, quota_reset_period: 'never' }, 'total'],
    [{ total_amount: 100, quota_reset_period: 'daily' }, 'periodic'],
    [{ total_amount: 0 }, 'unlimited'],
    [
      { total_amount: 0, quota_tiers: '[{"period":"daily","limit":100}]' },
      'multi',
    ],
  ])('按已有额度规则选择初始模式 %j', (plan, expectedMode) => {
    expect(getSubscriptionQuotaMode(plan)).toBe(expectedMode);
  });

  test.each([
    [
      'total',
      'custom',
      {
        total_amount: 500,
        quota_reset_period: 'never',
        quota_reset_custom_seconds: 0,
        quota_tiers: '[]',
      },
    ],
    [
      'periodic',
      'custom',
      {
        total_amount: 500,
        quota_reset_period: 'custom',
        quota_reset_custom_seconds: 90,
        quota_tiers: '[]',
      },
    ],
    [
      'periodic',
      'daily',
      {
        total_amount: 500,
        quota_reset_period: 'daily',
        quota_reset_custom_seconds: 0,
        quota_tiers: '[]',
      },
    ],
    [
      'multi',
      'custom',
      {
        total_amount: 0,
        quota_reset_period: 'never',
        quota_reset_custom_seconds: 0,
        quota_tiers: '[{"period":"daily","limit":100}]',
      },
    ],
    [
      'unlimited',
      'custom',
      {
        total_amount: 0,
        quota_reset_period: 'never',
        quota_reset_custom_seconds: 0,
        quota_tiers: '[]',
      },
    ],
  ])(
    '保存 %s 模式时不携带其它模式的隐藏配置',
    (mode, resetPeriod, expected) => {
      expect(
        buildSubscriptionQuotaConfig({
          mode,
          totalAmount: 500,
          resetPeriod,
          customSeconds: 90,
          tiers: [{ period: 'daily', limit: 100 }],
        }),
      ).toEqual(expected);
    },
  );

  test('历史混合配置直接保存时完整保留原额度和JSON，避免单位换算丢失精度', () => {
    const legacyQuota = {
      total_amount: 9007199254740001,
      quota_reset_period: 'custom',
      quota_reset_custom_seconds: 90,
      quota_tiers:
        '[ {"period":"daily", "limit":1234567, "sort_priority":10} ]',
    };
    expect(
      buildSubscriptionQuotaConfig({
        mode: 'multi',
        totalAmount: 0,
        resetPeriod: 'never',
        tiers: [],
        legacyPlan: legacyQuota,
      }),
    ).toEqual(legacyQuota);
  });

  test('固定30天100只提交一条周期限额，不另外配置总量，也不发送编辑草稿', () => {
    const tiers = buildSubscriptionQuotaTiers(
      [
        {
          period: 'custom',
          limit: 100,
          duration_text: '30d',
          custom_seconds: 0,
          sort_priority: 10,
        },
      ],
      (amount) => amount * 1000,
    );
    expect(
      buildSubscriptionQuotaConfig({
        mode: 'multi',
        totalAmount: 999,
        resetPeriod: 'daily',
        tiers,
      }),
    ).toEqual({
      total_amount: 0,
      quota_reset_period: 'never',
      quota_reset_custom_seconds: 0,
      quota_tiers:
        '[{"period":"custom","limit":100000,"custom_seconds":2592000,"sort_priority":10}]',
    });
  });

  test('自定义周期无效草稿保留为校验失败，不回退到旧秒数', () => {
    const [tier] = buildSubscriptionQuotaTiers(
      [
        {
          period: 'custom',
          limit: 100,
          duration_text: '5hours',
          custom_seconds: 3600,
        },
      ],
      (amount) => amount,
    );
    expect(tier.custom_seconds).toBeNull();
  });
});

describe('订阅额度展示', () => {
  test.each([
    ['never', 'none'],
    ['daily', 'daily'],
    ['custom', 'custom'],
  ])('单额度按重置规则区分总量和周期：%s', (resetPeriod, period) => {
    const limits = getSubscriptionQuotaLimits({
      total_amount: 100,
      quota_reset_period: resetPeriod,
      quota_reset_custom_seconds: 90,
    });
    expect(limits).toHaveLength(1);
    expect(limits[0]).toMatchObject({ period, limit: 100 });
  });

  test('历史总额度与多周期限制同时展示，不能把多周期额度当作不限', () => {
    const limits = getSubscriptionQuotaLimits({
      total_amount: 1000,
      quota_reset_period: 'never',
      quota_tiers: JSON.stringify([
        { period: 'daily', limit: 100, sort_priority: 10 },
        { period: 'monthly', limit: 500, sort_priority: 20 },
      ]),
    });
    expect(limits.map(({ period, limit }) => ({ period, limit }))).toEqual([
      { period: 'none', limit: 1000 },
      { period: 'daily', limit: 100 },
      { period: 'monthly', limit: 500 },
    ]);
    expect(getSubscriptionQuotaLimits({ total_amount: 0 })).toEqual([]);
  });

  test('各周期使用独立计数器，总额度不受周期重置影响', () => {
    const limits = getSubscriptionQuotaUsage({
      subscription: { amount_total: 0, amount_used: 9999 },
      quota_tiers: JSON.stringify([
        { period: 'hourly', limit: 30, sort_priority: 10 },
        { period: 'daily', limit: 100, sort_priority: 20 },
        { period: 'none', limit: 1000, sort_priority: 30 },
      ]),
      tier_usages: [
        {
          tier_index: 0,
          usage_in_window: 20,
          window_start: 1700000000,
          window_seconds: 3600,
        },
        {
          tier_index: 1,
          usage_in_period: 80,
          next_period_start: 1700006400,
        },
        { tier_index: 2, usage_in_window: 700 },
      ],
    });
    expect(
      limits.map(({ period, used, remaining, next_reset_time }) => ({
        period,
        used,
        remaining,
        next_reset_time,
      })),
    ).toEqual([
      {
        period: 'hourly',
        used: 20,
        remaining: 10,
        next_reset_time: 1700003600,
      },
      {
        period: 'daily',
        used: 80,
        remaining: 20,
        next_reset_time: 1700006400,
      },
      { period: 'none', used: 700, remaining: 300, next_reset_time: 0 },
    ]);
  });

  test('忽略不限规则后仍关联正确的周期用量，并将剩余额度下限设为零', () => {
    const limits = getSubscriptionQuotaUsage({
      subscription: { amount_total: 0 },
      quota_tiers: JSON.stringify([
        { period: 'hourly', limit: 0, sort_priority: 10 },
        { period: 'daily', limit: 10, sort_priority: 20 },
      ]),
      tier_usages: [{ tier_index: 1, usage_in_period: 12 }],
    });
    expect(limits).toHaveLength(1);
    expect(limits[0]).toMatchObject({ used: 12, remaining: 0 });
  });

  test('尚未开始的按使用计时周期不虚构重置时间', () => {
    const [limit] = getSubscriptionQuotaUsage({
      subscription: { amount_total: 0 },
      quota_tiers: '[{"period":"custom","custom_seconds":90,"limit":100}]',
      tier_usages: [{ tier_index: 0, window_start: 0, window_seconds: 90 }],
    });
    expect(limit).toMatchObject({
      used: 0,
      remaining: 100,
      next_reset_time: 0,
      starts_on_use: true,
    });
  });

  test('缺失周期用量记录时不显示为全额可用', () => {
    const [limit] = getSubscriptionQuotaUsage({
      subscription: { amount_total: 0, amount_used: 500 },
      quota_tiers: '[{"period":"hourly","limit":100}]',
      tier_usages: [],
    });
    expect(limit).toMatchObject({
      usage_available: false,
      used: null,
      remaining: null,
      starts_on_use: false,
    });
  });
});
