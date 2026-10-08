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
import { Button, Input, Typography } from '@douyinfe/semi-ui';
import { Plus, X } from 'lucide-react';
import { formatPrice, trendClass } from './api';

const { Text } = Typography;

// 一个仓位最多几档止盈、几档止损，与后端一致。
export const MAX_LEVELS = 4;

// levelsPayload 是要提交的档位：触发价与数量都填了的行。
export function levelsPayload(rows) {
  return rows
    .filter((row) => Number(row.price) > 0 && Number(row.qty) > 0)
    .map((row) => ({ price: String(row.price), qty: String(row.qty) }));
}

// levelsIncomplete 表示有填了触发价、却没填数量或触发价不是正数的档位：levelsPayload 会把它丢掉，要先让用户改好。
// 只填了数量的档位(开仓数量变化时自动填上的)算没设。
export function levelsIncomplete(rows) {
  return rows.some((row) => {
    const price = String(row.price ?? '').trim();
    return price !== '' && !(Number(price) > 0 && Number(row.qty) > 0);
  });
}

// levelsQty 是一组档位的数量合计。
export function levelsQty(levels) {
  return levels.reduce((sum, level) => sum + (Number(level.qty) || 0), 0);
}

// 一组止盈(kind 为 tp)或止损(sl)的编辑器：每档一个触发价和触发时要平的数量，最多 4 档。触发价后面是它离参考价 reference 的
// 距离；价格和数量都填了的档位估算触发时的盈亏(按开仓价 entryPrice，扣掉吃单手续费)与按这部分保证金算的回报率。底部是已经
// 分配出去的数量折成的价值，超过仓位时标红。
const FuturesLevelsEditor = ({
  kind,
  side,
  rows,
  onChange,
  reference,
  entryPrice,
  positionQty,
  margin,
  takerRate,
  ticker,
  priceDigits,
  t,
}) => {
  const isTp = kind === 'tp';
  const update = (index, patch) =>
    onChange(rows.map((row, i) => (i === index ? { ...row, ...patch } : row)));
  const allocated = levelsQty(rows);
  const over = allocated > positionQty + 1e-12;

  return (
    <div className='flex flex-col gap-2'>
      {rows.map((row, index) => {
        const price = Number(row.price);
        const qty = Number(row.qty);
        const distance =
          reference > 0 && price > 0
            ? ((price - reference) / reference) * 100
            : null;
        const ready = price > 0 && qty > 0 && entryPrice > 0;
        const pnl = ready
          ? (side === 'long' ? price - entryPrice : entryPrice - price) * qty -
            price * qty * takerRate
          : 0;
        const share = positionQty > 0 ? (margin * qty) / positionQty : 0;
        return (
          <div key={index} className='flex flex-col gap-1'>
            <div className='flex items-center gap-1'>
              <Input
                size='small'
                value={row.price}
                onChange={(value) => update(index, { price: value })}
                placeholder={isTp ? t('止盈价') : t('止损价')}
                aria-label={isTp ? t('止盈价') : t('止损价')}
                suffix={
                  distance === null
                    ? undefined
                    : `${distance > 0 ? '+' : ''}${distance.toFixed(1)}%`
                }
                inputMode='decimal'
              />
              <Input
                size='small'
                value={row.qty}
                onChange={(value) => update(index, { qty: value })}
                placeholder={t('数量')}
                aria-label={t('数量')}
                suffix={ticker}
                inputMode='decimal'
              />
              {rows.length > 1 && (
                <Button
                  size='small'
                  theme='borderless'
                  type='tertiary'
                  icon={<X size={14} />}
                  aria-label={t('删除')}
                  onClick={() => onChange(rows.filter((_, i) => i !== index))}
                />
              )}
            </div>
            {ready && (
              <Text type='tertiary' size='small' className='trade-num'>
                {t('触发价 {{price}}', {
                  price: formatPrice(price, priceDigits),
                })}
                {' · '}
                {t('预计盈亏')}{' '}
                <span className={trendClass(pnl)}>
                  {`${pnl > 0 ? '+' : ''}${pnl.toFixed(2)} USDT`}
                </span>
                {share > 0 &&
                  ` · ${t('回报率')} ${pnl > 0 ? '+' : ''}${((pnl / share) * 100).toFixed(1)}%`}
              </Text>
            )}
          </div>
        );
      })}
      <div className='flex items-center justify-between gap-2'>
        {rows.length < MAX_LEVELS ? (
          <Button
            size='small'
            theme='borderless'
            icon={<Plus size={14} />}
            onClick={() => onChange([...rows, { price: '', qty: '' }])}
          >
            {t('添加')}
          </Button>
        ) : (
          <span />
        )}
        <Text
          size='small'
          type={over ? 'danger' : 'tertiary'}
          className='trade-num'
        >
          {t('已分配 ≈{{used}} / {{total}} USDT', {
            used: (allocated * entryPrice).toFixed(2),
            total: (positionQty * entryPrice).toFixed(2),
          })}
        </Text>
      </div>
    </div>
  );
};

export default FuturesLevelsEditor;
