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

import React from 'react';
import { Progress, Tooltip } from '@douyinfe/semi-ui';
import { renderQuota } from '../../../helpers/render';
import {
  formatSubscriptionQuotaLabel,
  getSubscriptionQuotaLimits,
  getSubscriptionQuotaUsage,
} from '../../../helpers/subscriptionFormat';

const SubscriptionQuotaDetails = ({
  plan,
  summary,
  showProgress = false,
  t,
}) => {
  const limits = summary
    ? getSubscriptionQuotaUsage(summary)
    : getSubscriptionQuotaLimits(plan);
  if (limits.length === 0) {
    return <div className='text-xs text-gray-500'>{t('额度不限')}</div>;
  }
  const subscription = summary?.subscription;
  const showReset =
    subscription &&
    ['active', 'inactive'].includes(subscription.status) &&
    subscription.end_time > Date.now() / 1000;

  return (
    <div className='space-y-2 text-xs text-gray-500'>
      {limits.map((limit) => {
        const percent = limit.usage_available
          ? Math.max(0, (limit.used / limit.limit) * 100)
          : 0;
        const displayProgress =
          showProgress && summary && limit.usage_available;
        return (
          <div key={limit.key}>
            {displayProgress && (
              <>
                <div className='flex items-center justify-between gap-3 mb-1'>
                  <span className='font-medium'>
                    {formatSubscriptionQuotaLabel(limit, t)}
                  </span>
                  <span className='shrink-0'>
                    {t('已用')} {Number(percent.toFixed(1))}%
                  </span>
                </div>
                <Progress
                  percent={Math.min(100, percent)}
                  showInfo={false}
                  size='small'
                  stroke={
                    percent >= 100
                      ? 'var(--semi-color-danger)'
                      : percent >= 80
                        ? 'var(--semi-color-warning)'
                        : 'var(--semi-color-primary)'
                  }
                  aria-label={formatSubscriptionQuotaLabel(limit, t)}
                  aria-valuetext={`${t('已用')} ${Number(percent.toFixed(1))}% · ${renderQuota(limit.used)} / ${renderQuota(limit.limit)}`}
                  className='mb-1'
                />
              </>
            )}
            <Tooltip
              content={`${t('原生额度')}：${summary && limit.usage_available ? `${limit.used}/` : ''}${limit.limit}`}
            >
              <div>
                {!displayProgress && (
                  <span className='font-medium'>
                    {formatSubscriptionQuotaLabel(limit, t)}：
                  </span>
                )}
                {summary && limit.usage_available
                  ? `${t('已用')} ${renderQuota(limit.used)} / `
                  : ''}
                {renderQuota(limit.limit)}
                {summary && limit.usage_available && (
                  <span>
                    {' '}
                    · {t('剩余')} {renderQuota(limit.remaining)}
                  </span>
                )}
                {summary && !limit.usage_available && (
                  <span> · {t('用量暂不可用')}</span>
                )}
              </div>
            </Tooltip>
            {showReset && limit.period !== 'none' && limit.usage_available ? (
              <div>
                {limit.next_reset_time > 0 &&
                limit.next_reset_time < subscription.end_time
                  ? `${t('下一次重置')}：${new Date(limit.next_reset_time * 1000).toLocaleString()}`
                  : limit.starts_on_use
                    ? t('首次使用后开始计时')
                    : t('订阅到期前不再重置')}
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
};

export default SubscriptionQuotaDetails;
