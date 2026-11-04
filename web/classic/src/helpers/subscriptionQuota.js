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

import { parseSubscriptionPeriodDuration } from './subscriptionDuration';

export function getSubscriptionQuotaMode(plan) {
  if (!plan) return 'multi';
  try {
    const tiers = JSON.parse(plan.quota_tiers || '[]');
    if (Array.isArray(tiers) && tiers.length > 0) return 'multi';
  } catch {
    // Invalid historical tier data falls back to the legacy quota fields.
  }
  if (Number(plan.total_amount || 0) <= 0) return 'unlimited';
  return (plan.quota_reset_period || 'never') === 'never'
    ? 'total'
    : 'periodic';
}

export function buildSubscriptionQuotaConfig({
  mode,
  totalAmount,
  resetPeriod,
  customSeconds,
  tiers = [],
  legacyPlan,
}) {
  if (legacyPlan) {
    return {
      total_amount: legacyPlan.total_amount,
      quota_reset_period: legacyPlan.quota_reset_period,
      quota_reset_custom_seconds: legacyPlan.quota_reset_custom_seconds,
      quota_tiers: legacyPlan.quota_tiers,
    };
  }
  return {
    total_amount: mode === 'total' || mode === 'periodic' ? totalAmount : 0,
    quota_reset_period: mode === 'periodic' ? resetPeriod : 'never',
    quota_reset_custom_seconds:
      mode === 'periodic' && resetPeriod === 'custom'
        ? Number(customSeconds || 0)
        : 0,
    quota_tiers: mode === 'multi' ? JSON.stringify(tiers) : '[]',
  };
}

export function buildSubscriptionQuotaTiers(tiers, toQuota) {
  if (!Array.isArray(tiers)) return [];
  return tiers.map((tier, index) => ({
    period: tier?.period || 'monthly',
    limit: toQuota(tier?.limit || 0),
    custom_seconds:
      tier?.period === 'custom'
        ? parseSubscriptionPeriodDuration(tier.duration_text)
        : 0,
    sort_priority: Number(tier?.sort_priority || (index + 1) * 10),
  }));
}

export function getSubscriptionQuotaLimits(
  plan,
  totalAmount = plan?.total_amount,
) {
  const limits = [];
  if (Number(totalAmount) > 0) {
    const period = plan?.quota_reset_period || 'never';
    limits.push({
      key: 'base',
      period: period === 'never' ? 'none' : period,
      custom_seconds: Number(plan?.quota_reset_custom_seconds || 0),
      limit: Number(totalAmount),
    });
  }
  try {
    const tiers = JSON.parse(plan?.quota_tiers || '[]');
    if (Array.isArray(tiers)) {
      tiers
        .sort((a, b) => (a.sort_priority || 0) - (b.sort_priority || 0))
        .forEach((tier, index) => {
          if (!(Number(tier?.limit) > 0)) return;
          limits.push({ ...tier, key: `tier-${index}`, tier_index: index });
        });
    }
  } catch {
    // Legacy plans may not have a tier configuration.
  }
  return limits;
}

export function getSubscriptionQuotaUsage(summary) {
  const subscription = summary?.subscription;
  const usages = new Map(
    (summary?.tier_usages || []).map((usage) => [usage.tier_index, usage]),
  );
  return getSubscriptionQuotaLimits(summary, subscription?.amount_total).map(
    (limit) => {
      const usage = usages.get(limit.tier_index);
      const usageAvailable = limit.key === 'base' || usage !== undefined;
      const isWindow = limit.period === 'hourly' || limit.period === 'custom';
      let used = Number(subscription?.amount_used || 0);
      let nextResetTime = Number(subscription?.next_reset_time || 0);
      if (limit.key !== 'base') {
        used =
          limit.period === 'none'
            ? Number(usage?.usage_in_window || 0) +
              Number(usage?.usage_in_period || 0)
            : Number(
                (isWindow ? usage?.usage_in_window : usage?.usage_in_period) ||
                  0,
              );
        nextResetTime = isWindow
          ? usage?.window_start > 0
            ? Number(usage.window_start) + Number(usage.window_seconds)
            : 0
          : Number(usage?.next_period_start || 0);
      }
      return {
        ...limit,
        usage_available: usageAvailable,
        used: usageAvailable ? used : null,
        remaining: usageAvailable ? Math.max(0, limit.limit - used) : null,
        next_reset_time: limit.period === 'none' ? 0 : nextResetTime,
        starts_on_use:
          usageAvailable &&
          limit.key !== 'base' &&
          isWindow &&
          !usage?.window_start,
      };
    },
  );
}
