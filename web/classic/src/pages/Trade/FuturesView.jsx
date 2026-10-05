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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Tag, Typography } from '@douyinfe/semi-ui';
import { ArrowLeft } from 'lucide-react';
import CandleChart from './CandleChart';
import FuturesOrderPanel from './FuturesOrderPanel';
import FuturesOrdersCard from './FuturesOrdersCard';
import FuturesPositions from './FuturesPositions';
import OrderBook from './OrderBook';
import {
  changePercent,
  decimalsOf,
  formatCompact,
  formatFundingRate,
  formatPrice,
  trendClass,
  tradeGet,
  useFuturesBrackets,
  useTradeStream,
} from './api';

const { Text, Title } = Typography;

// 止盈止损、强平与后台撮合的限价单都在服务端发生，页面不会收到通知，定时刷新仓位、账户与委托。
const REFRESH_MS = 5_000;

// 一个永续合约的交易页：行情头(含标记价格、资金费率与下次结算倒计时)、K 线、下单面板、盘口、仓位与委托。
const FuturesView = ({
  item,
  initialQuote,
  self,
  perUnit,
  market,
  onBack,
  onOpenSymbol,
  onAccountChanged,
  t,
}) => {
  const symbol = item.symbol;
  const [quote, setQuote] = useState(initialQuote || {});
  const [book, setBook] = useState(null);
  const [kline, setKline] = useState(null);
  const [fills, setFills] = useState([]);
  const [positions, setPositions] = useState([]);
  const [cross, setCross] = useState(null);
  const [picked, setPicked] = useState(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [now, setNow] = useState(() => Date.now());

  const connected = useTradeStream(
    { symbols: [symbol], kline: symbol, book: symbol, futures: true },
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
      mark: (data) =>
        data.s === symbol &&
        setQuote((previous) => ({
          ...previous,
          mark: data.p,
          index: data.i,
          funding_rate: data.r,
          next_funding_time: data.T,
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
    const res = await tradeGet('/api/trade/futures/fills', t, { symbol });
    if (res.data) setFills(res.data);
  }, [symbol, t]);

  const loadPositions = useCallback(async () => {
    const res = await tradeGet('/api/trade/futures/positions', t);
    if (!res.data) return;
    setPositions(res.data.positions || []);
    setCross(res.data.cross || null);
  }, [t]);

  const refresh = useCallback(() => {
    setRefreshKey((value) => value + 1);
    loadFills();
    loadPositions();
    onAccountChanged?.();
  }, [loadFills, loadPositions, onAccountChanged]);

  useEffect(() => {
    setQuote(initialQuote || {});
    setBook(null);
    setKline(null);
    loadFills();
    loadPositions();
    // 换合约时才重置，initialQuote 之后的变化由推送接管。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [symbol]);

  useEffect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') refresh();
    }, REFRESH_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  // 资金费结算倒计时每秒走一次。
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);

  const bracketInfo = useFuturesBrackets(symbol, t);
  const priceDigits = decimalsOf(item.rules?.tick_size);
  const change = changePercent(quote.price, quote.open);
  const symbolPositions = useMemo(
    () => positions.filter((entry) => entry.symbol === symbol),
    [positions, symbol],
  );
  // 仓位的开仓均价、强平价与每一档止盈止损画在 K 线上，例如 "多 10x TP1"、"多 10x SL2"，只有一档时不带序号。
  const priceLines = useMemo(
    () =>
      symbolPositions.flatMap((entry) => {
        const long = entry.side === 'long';
        const prefix = `${long ? t('多仓') : t('空仓')} ${entry.leverage}x`;
        const levels = (list, name, color) =>
          (list || []).map((level, i) => ({
            price: level.price,
            title: `${prefix} ${name}${list.length > 1 ? i + 1 : ''}`,
            color,
          }));
        return [
          {
            price: entry.entry_price,
            title: long ? t('多仓均价') : t('空仓均价'),
          },
          {
            price: entry.liquidation_price,
            title: long ? t('多仓强平价') : t('空仓强平价'),
            color: 'down',
          },
          ...levels(entry.take_profits, 'TP', 'up'),
          ...levels(entry.stop_losses, 'SL', 'down'),
        ];
      }),
    [symbolPositions, t],
  );
  const positionRules = useMemo(
    () => ({
      [symbol]: {
        priceDigits: decimalsOf(item.rules?.tick_size),
        step: item.rules?.step_size,
      },
    }),
    [symbol, item.rules],
  );
  const marks = useMemo(
    () => (quote.mark ? { [symbol]: quote.mark } : {}),
    [quote.mark, symbol],
  );
  let countdown = '--';
  const left = Number(quote.next_funding_time) - now;
  if (left > 0) {
    const seconds = Math.floor(left / 1000);
    countdown = [
      Math.floor(seconds / 3600),
      Math.floor((seconds % 3600) / 60),
      seconds % 60,
    ]
      .map((part) => String(part).padStart(2, '0'))
      .join(':');
  }

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
            <Text type='tertiary'>USDT</Text>
          </Title>
          <Tag color='blue'>{t('永续')}</Tag>
          {item.kind === 'stock' && <Tag color='violet'>{t('美股')}</Tag>}
          {item.kind === 'commodity' && (
            <Tag color='amber'>{t('大宗商品')}</Tag>
          )}
          {!item.open && <Tag color='grey'>{t('仅可平仓')}</Tag>}
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
          [t('标记价格'), formatPrice(quote.mark, priceDigits)],
          [t('指数价格'), formatPrice(quote.index, priceDigits)],
          [
            t('资金费率 / 倒计时'),
            `${formatFundingRate(quote.funding_rate)} / ${countdown}`,
          ],
          [t('24 小时最高'), formatPrice(quote.high, priceDigits)],
          [t('24 小时最低'), formatPrice(quote.low, priceDigits)],
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
            klineUrl='/api/trade/futures/klines'
            priceDigits={priceDigits}
            kline={kline}
            priceLines={priceLines}
            fills={fills}
            t={t}
          />
          <div className='trade-card'>
            <FuturesPositions
              positions={positions}
              cross={cross}
              marks={marks}
              lastPrices={{ [symbol]: quote.price }}
              rules={positionRules}
              cash={self?.account?.cash}
              perUnit={perUnit}
              takerFeeBps={market.taker_fee_bps}
              showReverse={item.open}
              onChanged={refresh}
              onOpenSymbol={(next) => next !== symbol && onOpenSymbol(next)}
              t={t}
            />
          </div>
          <FuturesOrdersCard
            symbol={symbol}
            priceDigits={priceDigits}
            perUnit={perUnit}
            refreshKey={refreshKey}
            onChanged={refresh}
            t={t}
          />
        </div>
        <div className='flex flex-col gap-4'>
          <FuturesOrderPanel
            symbol={symbol}
            ticker={item.ticker}
            rules={item.rules}
            quote={quote}
            cash={self?.account?.cash}
            cross={cross}
            positions={symbolPositions}
            perUnit={perUnit}
            takerFeeBps={market.taker_fee_bps}
            makerFeeBps={market.maker_fee_bps}
            brackets={bracketInfo?.brackets}
            maxLeverage={item.max_leverage}
            canOpen={item.open}
            pickedPrice={picked}
            onPlaced={refresh}
            t={t}
          />
          <OrderBook
            book={book}
            priceDigits={priceDigits}
            qtyDigits={decimalsOf(item.rules?.step_size)}
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

export default FuturesView;
