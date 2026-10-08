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

import React, { useState } from 'react';
import { Radio, RadioGroup, Typography } from '@douyinfe/semi-ui';
import { formatPrice, formatQty } from './api';

const { Text } = Typography;

const clockOf = (ms) => {
  const time = new Date(ms);
  return [time.getHours(), time.getMinutes(), time.getSeconds()]
    .map((part) => String(part).padStart(2, '0'))
    .join(':');
};

// 盘口：上面卖单(最优的卖一在最下)、中间最新价、下面买单。成交是按这份盘口逐档吃的，挂着的数量就是能成交的上限。
// 另一页是 Binance 的最新成交：新的在上，主动买入标绿、主动卖出标红。点一档价格或一笔成交把价格填进限价单。
const OrderBook = ({
  book,
  trades,
  priceDigits,
  qtyDigits,
  lastPrice,
  connected,
  onPick,
  t,
}) => {
  const [tab, setTab] = useState('book');
  const asks = (book?.a || []).slice(0, 10).reverse();
  const bids = (book?.b || []).slice(0, 10);
  const maxQty = Math.max(
    ...asks.map((level) => Number(level[1])),
    ...bids.map((level) => Number(level[1])),
    0,
  );

  const row = (level, side) => (
    <div
      key={`${side}-${level[0]}`}
      className='trade-book-row'
      onClick={() => onPick?.({ price: level[0], at: Date.now() })}
    >
      <span
        className='trade-book-depth'
        style={{
          width: maxQty > 0 ? `${(Number(level[1]) / maxQty) * 100}%` : 0,
          background:
            side === 'ask'
              ? 'var(--semi-color-danger)'
              : 'var(--semi-color-success)',
        }}
      />
      <span className={side === 'ask' ? 'trade-down' : 'trade-up'}>
        {formatPrice(level[0], priceDigits)}
      </span>
      <span className='text-right'>{formatQty(level[1], qtyDigits)}</span>
    </div>
  );

  const recent = (trades || []).slice(-21).reverse();

  return (
    <div className='trade-card flex flex-col gap-1'>
      <div className='flex items-center justify-between gap-2'>
        <RadioGroup
          type='button'
          size='small'
          value={tab}
          onChange={(event) => setTab(event.target.value)}
        >
          <Radio value='book'>{t('盘口')}</Radio>
          <Radio value='trades'>{t('最新成交')}</Radio>
        </RadioGroup>
        <Text type='tertiary' size='small'>
          {tab === 'book' ? t('前 10 档') : 'Binance'}
        </Text>
      </div>
      {tab === 'trades' ? (
        <>
          <div
            className='trade-book-row trade-tape-row trade-muted'
            style={{ cursor: 'default' }}
          >
            <span>{t('价格 (USDT)')}</span>
            <span className='text-right'>{t('数量')}</span>
            <span className='text-right'>{t('时间')}</span>
          </div>
          {!recent.length ? (
            <Text type='tertiary' size='small' className='py-6 text-center'>
              {connected ? t('等待成交…') : t('行情连接中…')}
            </Text>
          ) : (
            recent.map((trade) => (
              <div
                key={trade.i}
                className='trade-book-row trade-tape-row'
                onClick={() => onPick?.({ price: trade.p, at: Date.now() })}
              >
                <span className={trade.m ? 'trade-down' : 'trade-up'}>
                  {formatPrice(trade.p, priceDigits)}
                </span>
                <span className='text-right'>
                  {formatQty(trade.q, qtyDigits)}
                </span>
                <span className='text-right trade-muted'>
                  {clockOf(trade.T)}
                </span>
              </div>
            ))
          )}
        </>
      ) : (
        <>
          <div
            className='trade-book-row trade-muted'
            style={{ cursor: 'default' }}
          >
            <span>{t('价格 (USDT)')}</span>
            <span className='text-right'>{t('挂单数量')}</span>
          </div>
          {!connected || (!asks.length && !bids.length) ? (
            <Text type='tertiary' size='small' className='py-6 text-center'>
              {connected ? t('等待盘口数据…') : t('行情连接中…')}
            </Text>
          ) : (
            <>
              {asks.map((level) => row(level, 'ask'))}
              <div className='py-1 text-center'>
                <Text strong className='trade-num'>
                  {formatPrice(lastPrice, priceDigits)}
                </Text>
              </div>
              {bids.map((level) => row(level, 'bid'))}
            </>
          )}
        </>
      )}
    </div>
  );
};

export default OrderBook;
