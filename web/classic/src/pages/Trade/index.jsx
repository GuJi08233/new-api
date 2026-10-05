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
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Banner,
  Empty,
  Radio,
  RadioGroup,
  Spin,
  Typography,
} from '@douyinfe/semi-ui';
import { initVChartSemiTheme } from '@visactor/vchart-semi-theme';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import AssetsView from './AssetsView';
import FuturesView from './FuturesView';
import LeaderboardView from './LeaderboardView';
import InsightsView from './InsightsView';
import MarketList from './MarketList';
import NoticeBell from './NoticeBell';
import TickerStrip from './TickerStrip';
import TradeView from './TradeView';
import {
  formatSignedUsdt,
  formatUsdt,
  tradeGet,
  trendClass,
  useTradeStream,
} from './api';
import './trade.css';

const { Text, Title } = Typography;

// 合并接口返回的行情：推送已经带来的字段比接口里的新，保留推送的。
function mergeQuotes(previous, symbols) {
  const next = { ...previous };
  symbols.forEach((item) => {
    if (item.quote)
      next[item.symbol] = { ...item.quote, ...previous[item.symbol] };
  });
  return next;
}

// 24 小时行情推送并进行情表。
function applyTicker(previous, data) {
  return {
    ...previous,
    [data.s]: {
      ...previous[data.s],
      price: data.c,
      open: data.o,
      high: data.h,
      low: data.l,
      volume: data.v,
      quote_volume: data.q,
    },
  };
}

// 模拟盘：按 Binance 实时盘口买卖现货与永续合约，账户里的钱从主钱包额度转入。/trade 是现货行情，/trade/futures 是
// 合约行情，/trade/assets 是资产，/trade/leaderboard 是排行榜，/trade/<交易对> 与 /trade/futures/<合约> 是交易页。
const Trade = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { symbol } = useParams();
  const location = useLocation();
  const isAssets = location.pathname === '/trade/assets';
  const isLeaderboard = location.pathname === '/trade/leaderboard';
  const isInsights = location.pathname === '/trade/insights';
  const isFutures = location.pathname.startsWith('/trade/futures');
  // 现货行情列表页。资产页与排行榜没有自己的行情，顶部行情条在那里也列现货。
  const isSpotList =
    !symbol && !isFutures && !isAssets && !isLeaderboard && !isInsights;
  const [self, setSelf] = useState(null);
  const [market, setMarket] = useState(null);
  const [marketError, setMarketError] = useState('');
  const [quotes, setQuotes] = useState({});
  const [futures, setFutures] = useState(null);
  const [futuresError, setFuturesError] = useState('');
  const [futuresQuotes, setFuturesQuotes] = useState({});
  const [loadError, setLoadError] = useState('');
  // 每来一批新通知加一，当前页面据此刷新仓位与委托。
  const [noticeKey, setNoticeKey] = useState(0);
  // 顶部行情条的数据：页面上已有的行情推送把每条 24 小时行情发到这里，行情条自己保存，推送不会让整个页面重绘。
  const [tickerFeed] = useState(() => new EventTarget());
  const isMobile = useIsMobile();

  useEffect(() => {
    initVChartSemiTheme({ isWatchingThemeSwitch: true });
  }, []);

  const loadSelf = useCallback(async () => {
    const res = await tradeGet('/api/trade/self', t);
    if (res.error) {
      setLoadError(res.error);
      return;
    }
    setSelf(res.data);
  }, [t]);

  const loadMarket = useCallback(async () => {
    const res = await tradeGet('/api/trade/market', t);
    if (res.error) {
      setMarketError(res.error);
      return;
    }
    setMarket(res.data);
    setQuotes((previous) => mergeQuotes(previous, res.data.symbols));
  }, [t]);

  const loadFutures = useCallback(async () => {
    const res = await tradeGet('/api/trade/futures/market', t);
    if (res.error) {
      setFuturesError(res.error);
      return;
    }
    setFutures(res.data);
    setFuturesQuotes((previous) => mergeQuotes(previous, res.data.symbols));
  }, [t]);

  useEffect(() => {
    loadSelf();
    if (isFutures) {
      loadFutures();
    } else if (!isInsights) {
      loadMarket();
    }
  }, [loadSelf, loadMarket, loadFutures, isFutures, isInsights]);

  const symbols = useMemo(
    () => market?.symbols.map((item) => item.symbol) || [],
    [market],
  );
  const futuresSymbols = useMemo(
    () => futures?.symbols.map((item) => item.symbol) || [],
    [futures],
  );
  // 行情条只在桌面宽度、模拟盘开放时显示，列的是当前市场(资产页算现货)的全部品种。不显示时交易页只订阅自己的交易对。
  const stripItems = (isFutures ? futures : market)?.symbols || [];
  const showStrip =
    !isMobile && !isInsights && !!self?.enabled && stripItems.length > 0;
  const publishTicker = useCallback(
    (data) =>
      tickerFeed.dispatchEvent(new CustomEvent('ticker', { detail: data })),
    [tickerFeed],
  );
  // 现货列表页推全部交易对给列表和行情条；资产页与排行榜没有别的推送，单为行情条订阅，行情不进页面状态，免得页面跟着重绘。
  useTradeStream(
    {
      symbols,
      enabled: isSpotList || (!symbol && !isFutures && showStrip),
    },
    {
      ticker: (data) => {
        if (isSpotList) setQuotes((previous) => applyTicker(previous, data));
        publishTicker(data);
      },
    },
  );
  useTradeStream(
    { symbols: futuresSymbols, futures: true, enabled: isFutures && !symbol },
    {
      ticker: (data) => {
        setFuturesQuotes((previous) => applyTicker(previous, data));
        publishTicker(data);
      },
      mark: (data) =>
        setFuturesQuotes((previous) => ({
          ...previous,
          [data.s]: {
            ...previous[data.s],
            mark: data.p,
            index: data.i,
            funding_rate: data.r,
            next_funding_time: data.T,
          },
        })),
    },
  );

  const perUnit = self?.quota_per_unit;
  const tickers = useMemo(
    () =>
      Object.fromEntries(
        (market?.symbols || []).map((item) => [item.symbol, item.ticker]),
      ),
    [market],
  );
  const item = symbol
    ? (isFutures ? futures : market)?.symbols.find(
        (entry) => entry.symbol === symbol,
      )
    : null;
  const openSymbol = (next) => navigate(`/trade/${next}`);
  const openFutures = (next) => navigate(`/trade/futures/${next}`);
  const onNotice = useCallback(() => {
    loadSelf();
    setNoticeKey((value) => value + 1);
  }, [loadSelf]);

  let content;
  if (loadError) {
    content = <Empty title={loadError} />;
  } else if (!self) {
    content = <Spin size='large' />;
  } else if (isInsights) {
    content = self.insights ? (
      <InsightsView t={t} />
    ) : (
      <Empty title={t('市场资讯暂未开放')} />
    );
  } else if (isLeaderboard) {
    content = <LeaderboardView perUnit={perUnit} t={t} />;
  } else if (isAssets) {
    content = (
      <AssetsView
        self={self}
        perUnit={perUnit}
        tickers={tickers}
        onSelfChanged={setSelf}
        onAccountChanged={loadSelf}
        onOpenSymbol={openSymbol}
        onOpenFutures={openFutures}
        noticeKey={noticeKey}
        t={t}
      />
    );
  } else if (isFutures && !futures) {
    content = futuresError ? (
      <Empty title={futuresError} />
    ) : (
      <Spin size='large' />
    );
  } else if (isFutures && symbol && !item) {
    content = <Empty title={t('这个合约没有开放交易')} />;
  } else if (isFutures && item) {
    content = (
      <FuturesView
        key={item.symbol}
        item={item}
        initialQuote={futuresQuotes[item.symbol]}
        self={self}
        perUnit={perUnit}
        market={futures}
        tickerSymbols={showStrip ? futuresSymbols : null}
        onTicker={publishTicker}
        onBack={() => navigate('/trade/futures')}
        onOpenSymbol={openFutures}
        onAccountChanged={loadSelf}
        noticeKey={noticeKey}
        t={t}
      />
    );
  } else if (isFutures) {
    content = (
      <MarketList
        futures
        symbols={futures.symbols}
        quotes={futuresQuotes}
        onOpen={openFutures}
        t={t}
      />
    );
  } else if (!market) {
    content = marketError ? (
      <Empty title={marketError} />
    ) : (
      <Spin size='large' />
    );
  } else if (symbol && !item) {
    content = <Empty title={t('这个交易对没有开放交易')} />;
  } else if (item) {
    content = (
      <TradeView
        key={item.symbol}
        item={item}
        initialQuote={quotes[item.symbol]}
        self={self}
        perUnit={perUnit}
        feeBps={market.fee_bps}
        tickerSymbols={showStrip ? symbols : null}
        onTicker={publishTicker}
        onBack={() => navigate('/trade')}
        onAccountChanged={loadSelf}
        noticeKey={noticeKey}
        t={t}
      />
    );
  } else {
    content = (
      <MarketList
        symbols={market.symbols}
        quotes={quotes}
        onOpen={openSymbol}
        t={t}
      />
    );
  }

  return (
    <div className='mt-[60px] px-2 pb-6 md:px-4'>
      <div className='mx-auto flex max-w-[1440px] flex-col gap-4'>
        {showStrip && (
          <TickerStrip
            key={isFutures ? 'futures' : 'spot'}
            items={stripItems}
            feed={tickerFeed}
            current={symbol}
            onOpen={isFutures ? openFutures : openSymbol}
            t={t}
          />
        )}
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div className='flex flex-wrap items-center gap-4'>
            <Title heading={3} className='!mb-0'>
              {t('模拟盘')}
            </Title>
            <RadioGroup
              type='button'
              value={
                isAssets
                  ? 'assets'
                  : isLeaderboard
                    ? 'leaderboard'
                    : isInsights
                      ? 'insights'
                      : isFutures
                        ? 'futures'
                        : 'spot'
              }
              onChange={(e) =>
                navigate(
                  {
                    spot: '/trade',
                    futures: '/trade/futures',
                    assets: '/trade/assets',
                    leaderboard: '/trade/leaderboard',
                    insights: '/trade/insights',
                  }[e.target.value],
                )
              }
            >
              <Radio value='spot'>{t('现货')}</Radio>
              <Radio value='futures'>{t('合约')}</Radio>
              <Radio value='assets'>{t('资产')}</Radio>
              {(self?.leaderboard || isLeaderboard) && (
                <Radio value='leaderboard'>{t('排行榜')}</Radio>
              )}
              {(self?.insights || isInsights) && (
                <Radio value='insights'>{t('市场资讯')}</Radio>
              )}
            </RadioGroup>
          </div>
          {self && (
            <div className='flex items-center gap-4'>
              <NoticeBell perUnit={perUnit} onNotice={onNotice} t={t} />
              <Text type='tertiary' size='small'>
                {t('总资产')}{' '}
                <Text strong className='trade-num'>
                  {formatUsdt(self.valuation?.equity, perUnit)} USDT
                </Text>
              </Text>
              <Text type='tertiary' size='small'>
                {t('今日盈亏')}{' '}
                <span className={`trade-num ${trendClass(self.today_pnl)}`}>
                  {formatSignedUsdt(self.today_pnl, perUnit)} USDT
                </span>
              </Text>
            </div>
          )}
        </div>
        {self && !self.enabled && (
          <Banner
            type='warning'
            closeIcon={null}
            description={t(
              '模拟盘暂未开放交易，账户里的资金可以在资产页转出。',
            )}
          />
        )}
        {self?.enabled && isFutures && !self.futures?.enabled && (
          <Banner
            type='warning'
            closeIcon={null}
            description={t('合约暂未开放开仓，已有的仓位可以平仓。')}
          />
        )}
        {content}
        <Text type='tertiary' size='small' className='text-center'>
          {t('行情来自 Binance，按真实盘口逐档成交，只用于模拟交易。K 线图由')}{' '}
          <a
            href='https://www.tradingview.com/'
            target='_blank'
            rel='noopener noreferrer'
          >
            TradingView Lightweight Charts™
          </a>{' '}
          {t('提供。')}
        </Text>
      </div>
    </div>
  );
};

export default Trade;
