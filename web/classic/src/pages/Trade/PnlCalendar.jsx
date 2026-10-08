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

import React, { useEffect, useState } from 'react';
import { Button, Typography } from '@douyinfe/semi-ui';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import dayjs from 'dayjs';
import { formatSignedUsdt, toUsdt, tradeGet, trendClass } from './api';

const { Text, Title } = Typography;

// 月度盈亏日历：每天的盈亏是当天结束时的总资产减去前一天，再扣掉当天的净转入；点开某一天看加密货币、美股代币与合约
// 各自的部分。今天按此刻的估值计算。
const PnlCalendar = ({ perUnit, refreshKey, t }) => {
  const [month, setMonth] = useState(() => dayjs().format('YYYY-MM'));
  const [days, setDays] = useState([]);
  const [selected, setSelected] = useState(null);

  useEffect(() => {
    let alive = true;
    tradeGet('/api/trade/assets/daily', t, { month }).then((res) => {
      if (alive && res.data) setDays(res.data);
    });
    return () => {
      alive = false;
    };
  }, [month, refreshKey, t]);

  const first = dayjs(`${month}-01`);
  const byDay = Object.fromEntries(days.map((entry) => [entry.day, entry]));
  const leading = (first.day() + 6) % 7;
  const cells = [
    ...Array.from({ length: leading }, () => null),
    ...Array.from({ length: first.daysInMonth() }, (_, i) =>
      first.add(i, 'day').format('YYYY-MM-DD'),
    ),
  ];
  const total = days.reduce((sum, entry) => sum + entry.pnl, 0);
  const isCurrentMonth = month === dayjs().format('YYYY-MM');
  const detail = selected ? byDay[selected] : null;
  const weekdays = [
    t('周一'),
    t('周二'),
    t('周三'),
    t('周四'),
    t('周五'),
    t('周六'),
    t('周日'),
  ];

  return (
    <div className='trade-card flex flex-col gap-3'>
      <div className='flex items-center justify-between'>
        <Title heading={6} className='!mb-0'>
          {t('盈亏日历')}
        </Title>
        <div className='flex items-center gap-1'>
          <Button
            size='small'
            theme='borderless'
            icon={<ChevronLeft size={16} />}
            onClick={() =>
              setMonth(first.subtract(1, 'month').format('YYYY-MM'))
            }
            aria-label={t('上个月')}
          />
          <Text className='trade-num'>{month}</Text>
          <Button
            size='small'
            theme='borderless'
            icon={<ChevronRight size={16} />}
            disabled={isCurrentMonth}
            onClick={() => setMonth(first.add(1, 'month').format('YYYY-MM'))}
            aria-label={t('下个月')}
          />
        </div>
      </div>
      <Text size='small'>
        {t('本月盈亏')}{' '}
        <span className={`trade-num ${trendClass(total)}`}>
          {formatSignedUsdt(total, perUnit)} USDT
        </span>
      </Text>
      <div className='trade-calendar'>
        {weekdays.map((label) => (
          <Text
            key={label}
            type='tertiary'
            size='small'
            className='text-center'
          >
            {label}
          </Text>
        ))}
        {cells.map((day, i) => {
          if (!day) return <div key={`blank-${i}`} />;
          const entry = byDay[day];
          return (
            <div
              key={day}
              className={`trade-calendar-cell ${entry ? 'trade-calendar-cell-active' : ''} ${selected === day ? 'trade-calendar-cell-selected' : ''}`}
              onClick={() =>
                entry && setSelected(selected === day ? null : day)
              }
            >
              <div className='trade-muted'>{Number(day.slice(8))}</div>
              {entry && (
                <div className={trendClass(entry.pnl)}>
                  {toUsdt(entry.pnl, perUnit).toFixed(2)}
                </div>
              )}
            </div>
          );
        })}
      </div>
      {detail && (
        <div className='flex flex-wrap gap-x-6 gap-y-1 text-sm'>
          <Text>
            {detail.day}
            {detail.live ? ` (${t('截至此刻')})` : ''}
          </Text>
          <Text>
            {t('加密货币')}{' '}
            <span className={`trade-num ${trendClass(detail.crypto_pnl)}`}>
              {formatSignedUsdt(detail.crypto_pnl, perUnit)}
            </span>
          </Text>
          <Text>
            {t('美股代币')}{' '}
            <span className={`trade-num ${trendClass(detail.stock_pnl)}`}>
              {formatSignedUsdt(detail.stock_pnl, perUnit)}
            </span>
          </Text>
          <Text>
            {t('合约')}{' '}
            <span className={`trade-num ${trendClass(detail.futures_pnl)}`}>
              {formatSignedUsdt(detail.futures_pnl, perUnit)}
            </span>
          </Text>
          {!!detail.prediction_pnl && (
            <Text>
              {t('BTC预测')}{' '}
              <span
                className={`trade-num ${trendClass(detail.prediction_pnl)}`}
              >
                {formatSignedUsdt(detail.prediction_pnl, perUnit)}
              </span>
            </Text>
          )}
          {!!detail.financing_pnl && (
            <Text>
              {t('借款利息')}{' '}
              <span className={`trade-num ${trendClass(detail.financing_pnl)}`}>
                {formatSignedUsdt(detail.financing_pnl, perUnit, 4)}
              </span>
            </Text>
          )}
          <Text>
            {t('合计')}{' '}
            <span className={`trade-num ${trendClass(detail.pnl)}`}>
              {formatSignedUsdt(detail.pnl, perUnit)} USDT
            </span>
          </Text>
        </div>
      )}
    </div>
  );
};

export default PnlCalendar;
