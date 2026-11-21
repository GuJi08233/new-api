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

import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Tag, Typography } from '@douyinfe/semi-ui';
import { Gift, Trophy, ArrowUpRight, CheckCircle2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import {
  PlayTurnstile,
  usePlayTurnstile,
} from '../Games/common/usePlayTurnstile';

const { Text } = Typography;

// 上期排行榜模拟盘体验金奖励横幅：同时用于主站「修士榜」与模拟盘「排行榜」。
// 未登录或活动未开启时自动隐藏，不影响页面正常浏览。
export default function RankingBonusBanner({
  onClaimed,
  showTradeLink = false,
  compact = false,
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [info, setInfo] = useState(null);
  const [claiming, setClaiming] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await API.get('/api/trade/bonus/ranking');
      if (res.data?.success && res.data.data?.enabled) {
        setInfo(res.data.data);
      } else {
        setInfo(null);
      }
    } catch {
      // 未登录或无权限时静默忽略，不弹报错提示
      setInfo(null);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const turnstile = usePlayTurnstile({
    required: Boolean(info?.can_claim && info?.turnstile_required),
    site_key: info?.site_key || '',
  });

  if (!info) return null;

  const isDaily = info.period === 'daily';
  const periodLabel = isDaily ? t('昨日修士榜') : t('上周修士榜');

  const handleClaim = async () => {
    setClaiming(true);
    try {
      const payload = turnstile.required
        ? { turnstile: turnstile.take() }
        : {};
      const res = await API.post('/api/trade/bonus/ranking/claim', payload);
      if (!res.data?.success) {
        showError(res.data?.message || t('领取失败'));
        return;
      }
      setInfo(res.data.data);
      showSuccess(
        t('已领取 {{amount}} USDT 模拟盘体验金', { amount: info.reward_usd }),
      );
      onClaimed?.(res.data.data);
    } catch (err) {
      showError(err?.response?.data?.message || t('领取失败'));
    } finally {
      setClaiming(false);
    }
  };

  return (
    <div
      className={`rounded-xl border border-[var(--semi-color-primary-light-active)] bg-[var(--semi-color-primary-light-default)] ${
        compact ? 'p-3.5' : 'p-4'
      }`}
    >
      <div className='flex flex-col gap-3 md:flex-row md:items-center md:justify-between'>
        <div className='flex flex-col gap-1.5'>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='inline-flex items-center gap-1.5 font-semibold text-[var(--semi-color-text-0)]'>
              <Gift size={16} className='text-[var(--semi-color-primary)]' />
              {t('{{period}}模拟盘体验金奖励', { period: periodLabel })}
            </span>
            <Tag size='small' color='blue'>
              {info.period_key}
            </Tag>
            {info.my_rank > 0 && (
              <Tag size='small' color='amber'>
                <span className='inline-flex items-center gap-1'>
                  <Trophy size={12} />
                  {t('上期第 {{rank}} 名', { rank: info.my_rank })}
                </span>
              </Tag>
            )}
          </div>
          <div className='flex flex-wrap items-center gap-2 text-xs'>
            <Text type='secondary' size='small'>
              {t('消费额度前 {{count}} 名可领：', { count: info.top_n })}
            </Text>
            {(info.tiers || []).map((tier, idx) => (
              <Tag key={idx} size='small' color='white' type='ghost'>
                {tier.from === tier.to
                  ? t('第 {{rank}} 名 {{amount}} USDT', {
                      rank: tier.from,
                      amount: tier.amount_usd,
                    })
                  : t('第 {{from}}–{{to}} 名 {{amount}} USDT', {
                      from: tier.from,
                      to: tier.to,
                      amount: tier.amount_usd,
                    })}
              </Tag>
            ))}
          </div>
          <Text type='tertiary' size='small'>
            {info.claimed
              ? t('你已领取本期榜单体验金，盈利可在模拟盘资产页转出成平台额度。')
              : info.my_rank > 0 && info.active_bonus > 0
                ? t(
                    '上期已获得 {{amount}} USDT 奖励资格，请先在模拟盘结清或放弃当前体验金后再领取。',
                    { amount: info.reward_usd },
                  )
                : info.my_rank > 0
                  ? t(
                      '恭喜上榜！可立即领取 {{amount}} USDT 模拟盘体验金，盈利可转出，亏损不扣自有额度。',
                      { amount: info.reward_usd },
                    )
                  : t(
                      '上期未进入前 {{count}} 名，继续使用 API 消费即可冲击下期榜单奖励。',
                      { count: info.top_n },
                    )}
          </Text>
        </div>

        <div className='flex flex-wrap items-center gap-2 shrink-0'>
          {info.can_claim && (
            <>
              <PlayTurnstile turnstile={turnstile} />
              <Button
                theme='solid'
                type='primary'
                icon={<Gift size={14} />}
                loading={claiming}
                disabled={turnstile.blocked}
                onClick={handleClaim}
              >
                {t('领取 {{amount}} USDT 体验金', { amount: info.reward_usd })}
              </Button>
            </>
          )}
          {info.claimed && (
            <Tag color='green' size='large'>
              <span className='inline-flex items-center gap-1'>
                <CheckCircle2 size={14} />
                {t('本期已领取')}
              </span>
            </Tag>
          )}
          {showTradeLink && (
            <Button
              theme='light'
              icon={<ArrowUpRight size={14} />}
              iconPosition='right'
              onClick={() => navigate('/trade')}
            >
              {t('前往模拟盘')}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
