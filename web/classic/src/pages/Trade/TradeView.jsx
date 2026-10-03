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
import { Button, Tag, Typography } from '@douyinfe/semi-ui';
import { ArrowLeft } from 'lucide-react';
import CandleChart from './CandleChart';
import OrderBook from './OrderBook';
import OrderPanel from './OrderPanel';
import OrdersCard from './OrdersCard';
import {
  changePercent,
  decimalsOf,
  formatCompact,
  formatPrice,
  formatQty,
  formatSignedUsdt,
  formatUsdt,
  trendClass,
  tradeGet,
  useTradeStream,
} from './api';

const { Text, Title } = Typography;

// 后台撮合的限价单成交后页面不会收到通知，定时刷新账户与委托。
const REFRESH_MS = 10_000;

// 一个交易对的交易页：行情头、K 线、下单面板、盘口、当前持仓与委托。
const TradeView = ({
  item,
  initialQuote,
  self,
  perUnit,
  feeBps,
  onBack,
  onAccountChanged,
  t,
}) => {
  const symbol = item.symbol;
  const [quote, setQuote] = useState(initialQuote || {});
  const [book, setBook] = useState(null);
  const [kline, setKline] = useState(null);
  const [fills, setFills] = useState([]);
  const [picked, setPicked] = useState(null);
  const [refreshKey, setRefreshKey] = useState(0);

  const connected = useTradeStream(
    { symbols: [symbol], kline: symbol, book: symbol },
    {
      ticker: (data) =>
        data.s === symbol &&
        setQuote((previous) => ({
          ...previous,
          price: data.c,
          open: data.o,
          high: data.h,
          low: data.l,
          volume: data.v,
          quote_volume: data.q,
        })),
      kline: (data) => data.s === symbol && setKline(data),
      book: (data) => {
        if (data.s !== symbol) return;
        setBook(data);
        setQuote((previous) => ({
          ...previous,
          bid: data.b?.[0]?.[0],
          ask: data.a?.[0]?.[0],
        }));
      },
    },
  );

  const loadFills = useCallback(async () => {
    const res = await tradeGet('/api/trade/fills', t, { symbol });
    if (res.data) setFills(res.data);
  }, [symbol, t]);

  const refresh = useCallback(() => {
    setRefreshKey((value) => value + 1);
    loadFills();
    onAccountChanged?.();
  }, [loadFills, onAccountChanged]);

  useEffect(() => {
    setQuote(initialQuote || {});
    setBook(null);
    setKline(null);
    loadFills();
    // 换交易对时才重置，initialQuote 之后的变化由推送接管。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [symbol]);

  useEffect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') refresh();
    }, REFRESH_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  const priceDigits = decimalsOf(item.rules?.tick_size);
  const qtyDigits = decimalsOf(item.rules?.step_size);
  const change = changePercent(quote.price, quote.open);
  const holding = self?.valuation?.holdings?.find(
    (entry) => entry.symbol === symbol,
  );
  const holdingPnl = holding ? holding.value - holding.cost : 0;

  return (
    <div className='flex flex-col gap-4'>
      <div className='trade-card flex flex-wrap items-center gap-x-8 gap-y-3'>
        <div className='flex items-center gap-2'>
          <Button
            icon={<ArrowLeft size={16} />}
            theme='borderless'
            type='tertiary'
            onClick={onBack}
            aria-label={t('返回行情')}
          />
          <Title heading={4} className='!mb-0'>
            {item.ticker}
            <Text type='tertiary'>/USDT</Text>
          </Title>
          {item.kind === 'stock' && <Tag color='violet'>{t('美股代币')}</Tag>}
          <Tag color={connected ? 'green' : 'orange'}>
            {connected ? t('实时') : t('连接中')}
          </Tag>
        </div>
        <div>
          <Text
            className={`trade-num text-xl font-semibold ${trendClass(change)}`}
          >
            {formatPrice(quote.price, priceDigits)}
          </Text>
          <div>
            <Text size='small' className={`trade-num ${trendClass(change)}`}>
              {change === null
                ? '--'
                : `${change > 0 ? '+' : ''}${change.toFixed(2)}%`}
            </Text>
          </div>
        </div>
        {[
          [t('24 小时最高'), formatPrice(quote.high, priceDigits)],
          [t('24 小时最低'), formatPrice(quote.low, priceDigits)],
          [t('24 小时成交量'), `${formatCompact(quote.volume)} ${item.ticker}`],
          [t('24 小时成交额'), `${formatCompact(quote.quote_volume)} USDT`],
        ].map(([label, value]) => (
          <div key={label}>
            <Text type='tertiary' size='small'>
              {label}
            </Text>
            <div className='trade-num'>{value}</div>
          </div>
        ))}
      </div>
      <div className='grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_320px]'>
        <div className='flex min-w-0 flex-col gap-4'>
          <CandleChart
            symbol={symbol}
            priceDigits={priceDigits}
            kline={kline}
            avgPrice={holding?.avg_price}
            fills={fills}
            t={t}
          />
          <div className='trade-card grid grid-cols-2 gap-3 sm:grid-cols-4'>
            {[
              [
                t('持有数量'),
                holding
                  ? `${formatQty(holding.qty, qtyDigits)} ${item.ticker}`
                  : '--',
              ],
              [
                t('持仓均价'),
                holding ? formatPrice(holding.avg_price, priceDigits) : '--',
              ],
              [
                t('持仓市值'),
                holding ? `${formatUsdt(holding.value, perUnit)} USDT` : '--',
              ],
              [
                t('浮动盈亏'),
                holding ? (
                  <span
                    className={trendClass(holdingPnl)}
                  >{`${formatSignedUsdt(holdingPnl, perUnit)} USDT`}</span>
                ) : (
                  '--'
                ),
              ],
            ].map(([label, value]) => (
              <div key={label}>
                <Text type='tertiary' size='small'>
                  {label}
                </Text>
                <div className='trade-num'>{value}</div>
              </div>
            ))}
          </div>
          <OrdersCard
            symbol={symbol}
            priceDigits={priceDigits}
            perUnit={perUnit}
            refreshKey={refreshKey}
            onChanged={refresh}
            t={t}
          />
        </div>
        <div className='flex flex-col gap-4'>
          <OrderPanel
            symbol={symbol}
            ticker={item.ticker}
            rules={item.rules}
            quote={quote}
            cash={self?.account?.cash}
            position={holding}
            perUnit={perUnit}
            feeBps={feeBps}
            pickedPrice={picked}
            onPlaced={refresh}
            t={t}
          />
          <OrderBook
            book={book}
            priceDigits={priceDigits}
            qtyDigits={qtyDigits}
            lastPrice={quote.price}
            connected={connected}
            onPick={setPicked}
            t={t}
          />
        </div>
      </div>
    </div>
  );
};

export default TradeView;
