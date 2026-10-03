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
import AssetsView from './AssetsView';
import MarketList from './MarketList';
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

// 模拟盘：按 Binance 实时盘口买卖现货，账户里的钱从主钱包额度转入。/trade 是行情列表，/trade/assets 是资产，
// /trade/<交易对> 是交易页。
const Trade = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { symbol } = useParams();
  const location = useLocation();
  const isAssets = location.pathname === '/trade/assets';
  const [self, setSelf] = useState(null);
  const [market, setMarket] = useState(null);
  const [marketError, setMarketError] = useState('');
  const [quotes, setQuotes] = useState({});
  const [loadError, setLoadError] = useState('');

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
    setQuotes((previous) => {
      const next = { ...previous };
      res.data.symbols.forEach((item) => {
        if (item.quote)
          next[item.symbol] = { ...item.quote, ...previous[item.symbol] };
      });
      return next;
    });
  }, [t]);

  useEffect(() => {
    loadSelf();
    loadMarket();
  }, [loadSelf, loadMarket]);

  const symbols = useMemo(
    () => market?.symbols.map((item) => item.symbol) || [],
    [market],
  );
  useTradeStream(
    { symbols, enabled: !symbol && !isAssets },
    {
      ticker: (data) =>
        setQuotes((previous) => ({
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
    ? market?.symbols.find((entry) => entry.symbol === symbol)
    : null;
  const openSymbol = (next) => navigate(`/trade/${next}`);

  let content;
  if (loadError) {
    content = <Empty title={loadError} />;
  } else if (!self) {
    content = <Spin size='large' />;
  } else if (isAssets) {
    content = (
      <AssetsView
        self={self}
        perUnit={perUnit}
        tickers={tickers}
        onSelfChanged={setSelf}
        onOpenSymbol={openSymbol}
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
        onBack={() => navigate('/trade')}
        onAccountChanged={loadSelf}
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
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div className='flex items-center gap-4'>
            <Title heading={3} className='!mb-0'>
              {t('模拟盘')}
            </Title>
            <RadioGroup
              type='button'
              value={isAssets ? 'assets' : 'market'}
              onChange={(e) =>
                navigate(
                  e.target.value === 'assets' ? '/trade/assets' : '/trade',
                )
              }
            >
              <Radio value='market'>{t('行情')}</Radio>
              <Radio value='assets'>{t('资产')}</Radio>
            </RadioGroup>
          </div>
          {self && (
            <div className='flex items-center gap-4'>
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
