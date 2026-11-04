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
import { useTranslation } from 'react-i18next';
import { Badge, Button, Divider, Tag } from '@douyinfe/semi-ui';
import SubscriptionQuotaDetails from '../common/ui/SubscriptionQuotaDetails';

const SubscriptionList = ({
  subscriptions,
  planTitleMap,
  now,
  switchingSubscriptionId,
  onSwitchSubscription,
}) => {
  const { t } = useTranslation();

  return (
    <div className='max-h-64 overflow-y-auto pr-1 semi-table-body'>
      {subscriptions.map((summary, index) => {
        const subscription = summary.subscription;
        const endTime = Number(subscription?.end_time || 0);
        const planTitle =
          summary.plan_title || planTitleMap.get(subscription?.plan_id) || '';
        const isActive = subscription?.status === 'active' && endTime > now;
        const isInactive = subscription?.status === 'inactive' && endTime > now;
        const isCancelled = subscription?.status === 'cancelled';
        const isExpired = subscription?.status === 'expired' || endTime <= now;
        const remainingDays = Math.max(0, Math.ceil((endTime - now) / 86400));

        return (
          <div key={subscription?.id || index}>
            <div className='flex items-center justify-between text-xs mb-2 gap-2'>
              <div className='flex items-center gap-2 min-w-0'>
                <span className='font-medium'>
                  {planTitle
                    ? `${planTitle} · ${t('订阅')} #${subscription?.id}`
                    : `${t('订阅')} #${subscription?.id}`}
                </span>
                {isActive ? (
                  <Tag
                    color='white'
                    size='small'
                    shape='circle'
                    prefixIcon={<Badge dot type='success' />}
                  >
                    {t('生效')}
                  </Tag>
                ) : isInactive ? (
                  <Tag color='orange' size='small' shape='circle'>
                    {t('未激活')}
                  </Tag>
                ) : (
                  <Tag color='white' size='small' shape='circle'>
                    {isCancelled
                      ? t('已作废')
                      : isExpired
                        ? t('已过期')
                        : t('已结束')}
                  </Tag>
                )}
              </div>
              {isActive && (
                <span className='text-gray-500 shrink-0'>
                  {t('剩余')} {remainingDays} {t('天')}
                </span>
              )}
            </div>
            <div className='text-xs text-gray-500 mb-2'>
              {isActive || isInactive
                ? t('至')
                : isCancelled
                  ? t('作废于')
                  : t('过期于')}{' '}
              {new Date(endTime * 1000).toLocaleString()}
            </div>
            <div className='mb-2'>
              <SubscriptionQuotaDetails summary={summary} t={t} showProgress />
            </div>
            {isInactive && (
              <div className='flex justify-end mb-2'>
                <Button
                  size='small'
                  theme='light'
                  type='primary'
                  loading={switchingSubscriptionId === subscription?.id}
                  onClick={() => onSwitchSubscription(subscription?.id)}
                >
                  {t('切换到此订阅')}
                </Button>
              </div>
            )}
            {index < subscriptions.length - 1 && <Divider margin={12} />}
          </div>
        );
      })}
    </div>
  );
};

export default SubscriptionList;
