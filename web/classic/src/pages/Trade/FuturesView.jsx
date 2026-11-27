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
import { ArrowLeft, ArrowLeftRight, Calculator, Star } from 'lucide-react';
import CandleChart from './CandleChart';
import FuturesOrderPanel from './FuturesOrderPanel';
import FuturesOrdersCard from './FuturesOrdersCard';
import FuturesPositions from './FuturesPositions';
import OrderBook from './OrderBook';
import SessionBadge from './SessionBadge';
import SymbolSwitcher from './SymbolSwitcher';
import TradeCalculatorModal from './TradeCalculatorModal';
import {
  changePercent,
  decimalsOf,
  formatCompact,
  formatFundingRate,
  formatPrice,
  mergeTradeTape,
  trendClass,
  tradeGet,
  useFuturesBrackets,
  useTradeStream,
} from './api';
import {
  getSymbolSector,
  readTradeFavorites,
  writeTradeFavorites,
} from './symbols';

const { Text } = Typography;

// 止盈止损、强平与后台撮合的限价单都在服务端发生，页面不会收到通知，定时刷新仓位、账户与委托。
const REFRESH_MS = 5_000;

// 一个永续合约的交易页：内联品种切换、行情头(含标记价格、资金费率与倒计时)、K 线、下单面板、盘口、仓位与委托。
const FuturesView = ({
  item,
  initialQuote,
  allSymbols,
  allQuotes,
  self,
  perUnit,
  market,
  tickerSymbols,
  onTicker,
  onBack,
  onOpenSymbol,
  onSwitchSpot,
  onAccountChanged,
  noticeKey,
  t,
}) => {
  const symbol = item.symbol;
  const [quote, setQuote] = useState(initialQuote || {});
  const [book, setBook] = useState(null);
  const [trades, setTrades] = useState([]);
  const [kline, setKline] = useState(null);
  const [fills, setFills] = useState([]);
  const [positions, setPositions] = useState([]);
  const [cross, setCross] = useState(null);
  const [picked, setPicked] = useState(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [now, setNow] = useState(() => Date.now());
  const [calcVisible, setCalcVisible] = useState(false);
  const [isFav, setIsFav] = useState(() =>
    readTradeFavorites(true).includes(symbol),
  );

  const toggleFav = () => {
    const list = readTradeFavorites(true);
    const next = list.includes(symbol)
      ? list.filter((s) => s !== symbol)
      : [...list, symbol];
    writeTradeFavorites(true, next);
    setIsFav(next.includes(symbol));
  };

  // 顶部行情条显示时顺带订阅它要的全部品种，行情转给行情条，一个页面只开一条推送连接。
  const connected = useTradeStream(
    {
      symbols: tickerSymbols || [symbol],
      kline: symbol,
      book: symbol,
      trades: symbol,
      futures: true,
    },
    {
      ticker: (data) => {
        onTicker?.(data);
        if (data.s !== symbol) return;
        setQuote((previous) => ({
          ...previous,
          price: data.c,
          open: data.o,
          high: data.h,
          low: data.l,
          volume: data.v,
          quote_volume: data.q,
        }));
      },
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
      trades: (data) =>
        data.s === symbol &&
        setTrades((previous) => mergeTradeTape(previous, data)),
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
    setTrades([]);
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

  // 后台发生了成交、止盈止损或强平(见 NoticeBell)时马上刷新，不等下一轮。
  useEffect(() => {
    if (noticeKey) refresh();
  }, [noticeKey, refresh]);

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
            side: entry.side,
          }));
        return [
          {
            price: entry.entry_price,
            title: long ? t('多仓均价') : t('空仓均价'),
            side: entry.side,
          },
          {
            price: entry.liquidation_price,
            title: long ? t('多仓强平价') : t('空仓强平价'),
            side: entry.side,
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

  const sector = getSymbolSector(item);
  const fundingRateNum = Number(quote.funding_rate);

  return (
    <div className='flex flex-col gap-4'>
      <div className='trade-card trade-terminal-header'>
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            icon={<ArrowLeft size={16} />}
            theme='borderless'
            type='tertiary'
            onClick={onBack}
            aria-label={t('返回行情')}
          />
          <SymbolSwitcher
            futures
            current={item}
            symbols={allSymbols || [item]}
            quotes={{ ...(allQuotes || {}), [symbol]: quote }}
            onSelect={onOpenSymbol}
            t={t}
          />
          <button
            type='button'
            className={`trade-fav-btn${isFav ? ' is-fav' : ''}`}
            aria-label={isFav ? t('取消自选') : t('加入自选')}
            onClick={toggleFav}
          >
            <Star size={15} fill={isFav ? 'currentColor' : 'none'} />
          </button>
          {sector && (
            <Tag color={sector.color} type='light'>
              {t(sector.label)}
            </Tag>
          )}
          {item.kind === 'stock' && (
            <Tag color='violet'>
              {{
                krx: t('韩股'),
                hk: t('港股'),
                cn: t('A股'),
                premarket: t('Pre-IPO'),
              }[item.session] || t('美股')}
            </Tag>
          )}
          {!item.open && <Tag color='grey'>{t('仅可平仓')}</Tag>}
          {item.session && <SessionBadge session={item.session} t={t} />}
          <Tag color={connected ? 'green' : 'orange'}>
            <span className='inline-flex items-center gap-1'>
              <span
                className={`trade-live-dot${connected ? ' is-live' : ''}`}
              />
              {connected ? t('实时') : t('连接中')}
            </span>
          </Tag>
        </div>

        <div className='flex flex-wrap items-center gap-x-5 gap-y-2'>
          <div className='flex items-baseline gap-2.5'>
            <Text
              className={`trade-num text-2xl font-bold leading-none ${trendClass(change)}`}
            >
              {formatPrice(quote.price, priceDigits)}
            </Text>
            <span
              className={`trade-change-pill ${change === null ? 'trade-flat' : trendClass(change)}`}
            >
              {change === null
                ? '--'
                : `${change > 0 ? '+' : ''}${change.toFixed(2)}%`}
            </span>
          </div>
          {[
            [t('标记价格'), formatPrice(quote.mark, priceDigits), ''],
            [t('指数价格'), formatPrice(quote.index, priceDigits), ''],
            [
              t('资金费率 / 倒计时'),
              `${formatFundingRate(quote.funding_rate)} / ${countdown}`,
              Number.isFinite(fundingRateNum) && fundingRateNum !== 0
                ? fundingRateNum > 0
                  ? 'trade-up'
                  : 'trade-down'
                : '',
            ],
            [t('24 小时最高'), formatPrice(quote.high, priceDigits), ''],
            [t('24 小时最低'), formatPrice(quote.low, priceDigits), ''],
            [
              t('24 小时成交额'),
              `${formatCompact(quote.quote_volume)} USDT`,
              '',
            ],
          ].map(([label, value, cls]) => (
            <div key={label} className='leading-tight'>
              <Text type='tertiary' size='small' className='!text-[11px]'>
                {label}
              </Text>
              <div className={`trade-num text-sm font-medium ${cls}`}>
                {value}
              </div>
            </div>
          ))}
        </div>

        <div className='flex items-center gap-2 ml-auto'>
          <Button
            size='small'
            theme='light'
            icon={<Calculator size={14} />}
            onClick={() => setCalcVisible(true)}
          >
            {t('计算器')}
          </Button>
          {onSwitchSpot && (
            <Button
              size='small'
              theme='light'
              type='primary'
              icon={<ArrowLeftRight size={14} />}
              onClick={onSwitchSpot}
            >
              {t('去现货')}
            </Button>
          )}
        </div>
      </div>
      <div className='grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_336px]'>
        <div className='flex min-w-0 flex-col gap-4'>
          <CandleChart
            symbol={symbol}
            klineUrl='/api/trade/futures/klines'
            priceDigits={priceDigits}
            kline={kline}
            priceLines={priceLines}
            fills={fills}
            calendarEnabled={!!self?.insights}
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
            trades={trades}
            priceDigits={priceDigits}
            qtyDigits={decimalsOf(item.rules?.step_size)}
            lastPrice={quote.price}
            connected={connected}
            onPick={setPicked}
            t={t}
          />
        </div>
      </div>
      <TradeCalculatorModal
        visible={calcVisible}
        onClose={() => setCalcVisible(false)}
        symbolItem={item}
        currentPrice={quote.price}
        priceDigits={priceDigits}
        futures
        defaultLeverage={
          symbolPositions[0]?.leverage || Math.min(10, item.max_leverage || 10)
        }
        t={t}
      />
    </div>
  );
};

export default FuturesView;
