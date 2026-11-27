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

import React, { useMemo, useState } from 'react';
import { Input, Popover, Tag, Typography } from '@douyinfe/semi-ui';
import { ChevronDown, Search, Star } from 'lucide-react';
import { changePercent, decimalsOf, formatPrice, trendClass } from './api';
import { SymbolAvatar } from './SymbolIdentity';
import {
  TRADE_SECTORS,
  getSymbolName,
  matchesSymbolQuery,
  readTradeFavorites,
  resolveSymbolMeta,
  writeTradeFavorites,
} from './symbols';

const { Text } = Typography;

// 交易页头部内联品种切换器：无需返回列表即可按中文名、英文全名、板块或自选快速切换品种。
const SymbolSwitcher = ({
  current,
  symbols = [],
  quotes = {},
  futures = false,
  onSelect,
  t,
}) => {
  const [visible, setVisible] = useState(false);
  const [query, setQuery] = useState('');
  const [sector, setSector] = useState('all');
  const [favorites, setFavorites] = useState(() => readTradeFavorites(futures));

  const favSet = useMemo(() => new Set(favorites), [favorites]);

  const toggleFav = (symbol, event) => {
    event.stopPropagation();
    setFavorites((prev) => {
      const next = prev.includes(symbol)
        ? prev.filter((s) => s !== symbol)
        : [...prev, symbol];
      writeTradeFavorites(futures, next);
      return next;
    });
  };

  const availableSectors = useMemo(() => {
    const present = new Set();
    for (const item of symbols) {
      const meta = resolveSymbolMeta(item);
      if (meta?.sector) present.add(meta.sector);
    }
    return TRADE_SECTORS.filter((s) => present.has(s.key));
  }, [symbols]);

  const filtered = useMemo(() => {
    return symbols.filter((item) => {
      if (sector === 'favorites' && !favSet.has(item.symbol)) return false;
      if (sector !== 'all' && sector !== 'favorites') {
        const meta = resolveSymbolMeta(item);
        if (meta?.sector !== sector) return false;
      }
      return matchesSymbolQuery(item, query, t);
    });
  }, [symbols, sector, favSet, query, t]);

  const currentName = getSymbolName(current, t);
  const currentTicker = current?.ticker || '';
  const showTickerSub =
    currentName.toUpperCase() !== currentTicker.toUpperCase();

  const content = (
    <div className='trade-switcher-popover'>
      <div className='p-2.5 border-b border-[var(--semi-color-border)] flex flex-col gap-2'>
        <Input
          size='small'
          prefix={<Search size={14} className='ml-1.5' />}
          value={query}
          onChange={setQuery}
          showClear
          autoFocus
          placeholder={t('搜索名称或代码，如 英伟达、OpenAI、谷歌')}
        />
        <div className='flex flex-wrap gap-1'>
          {[
            { key: 'all', label: t('全部') },
            { key: 'favorites', label: `${t('自选')} (${favorites.length})` },
            ...availableSectors.map((s) => ({ key: s.key, label: t(s.label) })),
          ].map((tab) => (
            <button
              key={tab.key}
              type='button'
              className={`trade-pill-filter${sector === tab.key ? ' is-active' : ''}`}
              onClick={() => setSector(tab.key)}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>
      <div className='trade-switcher-list'>
        {!filtered.length ? (
          <div className='py-8 text-center'>
            <Text type='tertiary' size='small'>
              {t('没有匹配的品种')}
            </Text>
          </div>
        ) : (
          filtered.map((item) => {
            const quote = quotes[item.symbol] || item.quote || {};
            const change = changePercent(quote.price, quote.open);
            const active = item.symbol === current?.symbol;
            const isFav = favSet.has(item.symbol);
            const name = getSymbolName(item, t);
            return (
              <div
                key={item.symbol}
                role='button'
                tabIndex={0}
                className={`trade-switcher-item${active ? ' is-current' : ''}`}
                onClick={() => {
                  setVisible(false);
                  setQuery('');
                  if (!active) onSelect?.(item.symbol);
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    setVisible(false);
                    setQuery('');
                    if (!active) onSelect?.(item.symbol);
                  }
                }}
              >
                <button
                  type='button'
                  className={`trade-fav-btn shrink-0${isFav ? ' is-fav' : ''}`}
                  aria-label={isFav ? t('取消自选') : t('加入自选')}
                  onClick={(event) => toggleFav(item.symbol, event)}
                >
                  <Star size={13} fill={isFav ? 'currentColor' : 'none'} />
                </button>
                <SymbolAvatar item={item} size={26} />
                <div className='flex flex-col min-w-0 flex-1 leading-tight'>
                  <div className='flex items-center gap-1.5 min-w-0'>
                    <Text strong size='small' className='truncate'>
                      {name}
                    </Text>
                    {futures && item.max_leverage && (
                      <Tag
                        size='small'
                        color='blue'
                        className='!text-[10px] !px-1 !leading-4 shrink-0'
                      >
                        {item.max_leverage}x
                      </Tag>
                    )}
                  </div>
                  <Text
                    type='tertiary'
                    size='small'
                    className='!text-[11px] truncate'
                  >
                    {item.ticker}
                    {futures ? ` ${t('永续')}` : '/USDT'}
                  </Text>
                </div>
                <div className='flex flex-col items-end shrink-0 trade-num leading-tight'>
                  <Text size='small' strong>
                    {formatPrice(
                      quote.price,
                      decimalsOf(item.rules?.tick_size),
                    )}
                  </Text>
                  <span
                    className={`text-[11px] font-medium ${trendClass(change)}`}
                  >
                    {change === null
                      ? '--'
                      : `${change > 0 ? '+' : ''}${change.toFixed(2)}%`}
                  </span>
                </div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );

  return (
    <Popover
      trigger='click'
      position='bottomLeft'
      visible={visible}
      onVisibleChange={(next) => {
        setVisible(next);
        if (next) setFavorites(readTradeFavorites(futures));
      }}
      content={content}
      style={{ padding: 0 }}
    >
      <button
        type='button'
        className='trade-switcher-trigger'
        aria-label={t('切换品种')}
      >
        <SymbolAvatar item={current} size={34} />
        <div className='flex flex-col items-start leading-tight'>
          <div className='flex items-center gap-1.5'>
            <span className='text-base font-bold text-[var(--semi-color-text-0)]'>
              {currentName}
            </span>
            <ChevronDown
              size={15}
              className='text-[var(--semi-color-text-2)]'
            />
          </div>
          <span className='text-xs text-[var(--semi-color-text-2)]'>
            {showTickerSub ? `${currentTicker}` : current?.symbol}
            {futures ? ` · USDT ${t('永续')}` : ' / USDT'}
          </span>
        </div>
      </button>
    </Popover>
  );
};

export default SymbolSwitcher;
