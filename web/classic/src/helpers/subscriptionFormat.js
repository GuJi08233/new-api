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

import { renderQuota } from './render';

export function formatSubscriptionDuration(plan, t) {
  const unit = plan?.duration_unit || 'month';
  const value = plan?.duration_value || 1;
  const unitLabels = {
    year: t('年'),
    month: t('个月'),
    day: t('天'),
    hour: t('小时'),
    custom: t('自定义'),
  };
  if (unit === 'custom') {
    const seconds = plan?.custom_seconds || 0;
    if (seconds >= 86400) return `${Math.floor(seconds / 86400)} ${t('天')}`;
    if (seconds >= 3600) return `${Math.floor(seconds / 3600)} ${t('小时')}`;
    return `${seconds} ${t('秒')}`;
  }
  return `${value} ${unitLabels[unit] || unit}`;
}

export function formatSubscriptionResetPeriod(plan, t) {
  const period = plan?.quota_reset_period || 'never';
  if (period === 'never') return t('不重置');
  if (period === 'daily') return t('每自然日');
  if (period === 'weekly') return t('每自然周');
  if (period === 'monthly') return t('每自然月');
  if (period === 'custom') {
    const seconds = Number(plan?.quota_reset_custom_seconds || 0);
    if (seconds > 0 && seconds % 86400 === 0)
      return `${seconds / 86400} ${t('天')}`;
    if (seconds > 0 && seconds % 3600 === 0)
      return `${seconds / 3600} ${t('小时')}`;
    if (seconds > 0 && seconds % 60 === 0)
      return `${seconds / 60} ${t('分钟')}`;
    return `${seconds} ${t('秒')}`;
  }
  return t('不重置');
}

const TIER_PERIOD_LABELS = {
  monthly: '每自然月',
  weekly: '每自然周',
  daily: '每自然日',
  hourly: '每小时',
  custom: '自定义',
  none: '累计用量上限（续订不增加）',
};

export function formatQuotaTierPeriod(tier, t) {
  const label = TIER_PERIOD_LABELS[tier.period] || tier.period;
  if (tier.period === 'custom' && tier.custom_seconds > 0) {
    const seconds = tier.custom_seconds;
    if (seconds % 86400 === 0) return `${seconds / 86400}${t('天')}`;
    if (seconds % 3600 === 0) return `${seconds / 3600}${t('小时')}`;
    if (seconds % 60 === 0) return `${seconds / 60}${t('分钟')}`;
    return `${seconds}${t('秒')}`;
  }
  return t(label);
}

export function formatQuotaTierLimit(limit) {
  if (limit <= 0) return '∞';
  return renderQuota(limit);
}

export function formatTiersSummary(quotaTiers, t) {
  if (!quotaTiers || quotaTiers === '[]') return '';
  try {
    const tiers = JSON.parse(quotaTiers);
    if (!Array.isArray(tiers) || tiers.length === 0) return '';
    return tiers
      .filter((tier) => tier.limit > 0)
      .map(
        (tier) =>
          `${formatQuotaTierPeriod(tier, t)}: ${formatQuotaTierLimit(tier.limit)}`,
      )
      .join(' | ');
  } catch {
    return '';
  }
}

export function formatSubscriptionQuotaLabel(limit, t) {
  if (limit.period === 'none') {
    return limit.key === 'base'
      ? t('额度包（续订叠加）')
      : t('累计用量上限（续订不增加）');
  }
  return `${t('周期额度')} (${formatQuotaTierPeriod(limit, t)})`;
}

export function formatSubscriptionQuotaResetHint(limit, t) {
  if (limit.period === 'none') {
    return limit.key === 'base'
      ? t('使用后不自动恢复，续订时按套餐额度追加。')
      : t('累计用量不随周期重置，续订不会增加此上限。');
  }
  if (limit.period === 'custom' && limit.key === 'base') {
    return t('从订阅开始按固定间隔重置，续订不改变重置节奏。');
  }
  if (limit.period === 'custom') {
    return t(
      '自定义周期按配置秒数计时；从首次请求开始，窗口到期后由下一次请求开启新窗口。',
    );
  }
  if (limit.period === 'hourly') {
    return t('从首次请求开始计时；窗口到期后，由下一次请求开启新窗口。');
  }
  if (limit.period === 'monthly') {
    return t('按自然月重置，未用额度不结转；自然月不等于固定 30 天。');
  }
  return t('按自然日、自然周或自然月重置，未用额度不结转。');
}

export {
  getSubscriptionQuotaLimits,
  getSubscriptionQuotaUsage,
} from './subscriptionQuota';
