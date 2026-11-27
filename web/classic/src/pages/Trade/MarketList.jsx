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

import React, { memo, useMemo, useState } from 'react';
import {
  Button,
  Input,
  Radio,
  RadioGroup,
  Select,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  BarChart3,
  Flame,
  LayoutGrid,
  List,
  Search,
  Star,
  TrendingDown,
  TrendingUp,
} from 'lucide-react';
import {
  changePercent,
  decimalsOf,
  formatCompact,
  formatFundingRate,
  formatPrice,
  trendClass,
} from './api';
import SymbolIdentity, { SymbolAvatar } from './SymbolIdentity';
import {
  TRADE_SECTORS,
  getSymbolName,
  matchesSymbolQuery,
  readTradeFavorites,
  resolveSymbolMeta,
  writeTradeFavorites,
} from './symbols';

const { Text, Title } = Typography;

const VIEW_STORAGE_KEY = 'trade-market-view:v1';

function readViewMode() {
  try {
    const raw = window.localStorage.getItem(VIEW_STORAGE_KEY);
    return raw === 'cards' ? 'cards' : 'table';
  } catch {
    return 'table';
  }
}

const SESSION_REGION_LABELS = {
  us: '美股',
  hk: '港股',
  cn: 'A股',
  krx: '韩股',
  premarket: 'Pre-IPO',
  cme: 'CME',
};

// 最近 24 小时的小时收盘价迷你走势图，带柔和面积渐变填充。
const Sparkline = memo(({ closes, width = 96, height = 30 }) => {
  const values = (closes || []).map(Number).filter(Number.isFinite);
  if (values.length < 2) return <span className='trade-muted'>--</span>;
  const lowest = Math.min(...values);
  const range = Math.max(...values) - lowest || 1;
  const padY = 2;
  const innerH = height - padY * 2;
  const coords = values.map((value, i) => [
    Number(((i / (values.length - 1)) * width).toFixed(1)),
    Number((padY + innerH - ((value - lowest) / range) * innerH).toFixed(1)),
  ]);
  const linePoints = coords.map(([x, y]) => `${x},${y}`).join(' ');
  const areaPoints = `0,${height} ${linePoints} ${width},${height}`;
  const up = values[values.length - 1] >= values[0];
  const color = up ? 'var(--semi-color-success)' : 'var(--semi-color-danger)';
  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      aria-hidden='true'
      className='shrink-0'
    >
      <polygon points={areaPoints} fill={color} fillOpacity='0.12' />
      <polyline
        points={linePoints}
        fill='none'
        strokeWidth='1.6'
        strokeLinecap='round'
        strokeLinejoin='round'
        stroke={color}
      />
    </svg>
  );
});

// 24 小时价格区间可视化条：标示最新价落在当日最低与最高之间的位置。
const RangeBar = ({ price, low, high, digits }) => {
  const p = Number(price);
  const l = Number(low);
  const h = Number(high);
  if (!Number.isFinite(p) || !Number.isFinite(l) || !Number.isFinite(h)) {
    return <span className='trade-muted'>--</span>;
  }
  const ratio =
    h > l ? Math.min(100, Math.max(0, ((p - l) / (h - l)) * 100)) : 50;
  return (
    <div className='trade-range-cell'>
      <div className='flex justify-between gap-2 text-[11px] trade-num text-[var(--semi-color-text-2)]'>
        <span>{formatPrice(low, digits)}</span>
        <span>{formatPrice(high, digits)}</span>
      </div>
      <div className='trade-range-track'>
        <span className='trade-range-fill' style={{ width: `${ratio}%` }} />
        <span className='trade-range-dot' style={{ left: `${ratio}%` }} />
      </div>
    </div>
  );
};

// compareQuotes 按数值比较两个行情字段。没有行情的不论升序降序都排在最后。
function compareQuotes(leftValue, rightValue, order) {
  const left =
    leftValue === undefined || leftValue === null || leftValue === ''
      ? NaN
      : Number(leftValue);
  const right =
    rightValue === undefined || rightValue === null || rightValue === ''
      ? NaN
      : Number(rightValue);
  const leftMissing = !Number.isFinite(left);
  const rightMissing = !Number.isFinite(right);
  if (leftMissing || rightMissing) {
    if (leftMissing === rightMissing) return 0;
    const last = leftMissing ? 1 : -1;
    return order === 'descend' ? -last : last;
  }
  return left - right;
}

const MarketCard = memo(
  ({ item, quote, futures, isFav, onToggleFav, onOpen, t }) => {
    const digits = decimalsOf(item.rules?.tick_size);
    const change = changePercent(quote?.price, quote?.open);
    const cls = change === null ? 'trade-flat' : trendClass(change);
    const region = item.session ? SESSION_REGION_LABELS[item.session] : '';

    return (
      <div
        role='button'
        tabIndex={0}
        className='trade-market-card'
        onClick={() => onOpen(item.symbol)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            onOpen(item.symbol);
          }
        }}
      >
        <div className='flex items-start justify-between gap-2'>
          <SymbolIdentity item={item} futures={futures} size={34} t={t} />
          <button
            type='button'
            className={`trade-fav-btn shrink-0${isFav ? ' is-fav' : ''}`}
            aria-label={isFav ? t('取消自选') : t('加入自选')}
            onClick={(event) => onToggleFav(item.symbol, event)}
          >
            <Star size={14} fill={isFav ? 'currentColor' : 'none'} />
          </button>
        </div>

        <div className='flex items-end justify-between gap-2'>
          <div className='flex flex-col gap-1 min-w-0'>
            <span
              className={`trade-num text-lg font-bold leading-none ${trendClass(change)}`}
            >
              {formatPrice(quote?.price, digits)}
            </span>
            <div className='flex flex-wrap items-center gap-1'>
              <span className={`trade-change-pill ${cls}`}>
                {change === null
                  ? '--'
                  : `${change > 0 ? '+' : ''}${change.toFixed(2)}%`}
              </span>
              {futures && item.max_leverage && (
                <Tag
                  size='small'
                  color='blue'
                  className='!text-[10px] !px-1.5 !leading-4'
                >
                  {item.max_leverage}x
                </Tag>
              )}
              {region && (
                <Tag
                  size='small'
                  color='violet'
                  type='light'
                  className='!text-[10px] !px-1.5 !leading-4'
                >
                  {t(region)}
                </Tag>
              )}
              {futures && !item.open && (
                <Tag
                  size='small'
                  color='grey'
                  className='!text-[10px] !px-1.5 !leading-4'
                >
                  {t('仅可平仓')}
                </Tag>
              )}
            </div>
          </div>
          <Sparkline closes={item.sparkline} width={88} height={32} />
        </div>

        <div className='flex items-center justify-between gap-2 border-t border-[var(--semi-color-border)] pt-2 text-xs trade-num text-[var(--semi-color-text-2)]'>
          <span>
            {t('成交额')} {formatCompact(quote?.quote_volume)}
          </span>
          {futures ? (
            <span
              className={
                Number(quote?.funding_rate || 0) > 0
                  ? 'trade-up'
                  : Number(quote?.funding_rate || 0) < 0
                    ? 'trade-down'
                    : ''
              }
            >
              {t('资金费')} {formatFundingRate(quote?.funding_rate)}
            </span>
          ) : (
            <span>
              {formatPrice(quote?.low, digits)} ~{' '}
              {formatPrice(quote?.high, digits)}
            </span>
          )}
        </div>
      </div>
    );
  },
);

// 行情列表：按板块(大模型、AI 算力、半导体、科技巨头、加密货币、大宗商品)直观分组，
// 全品种展示中文/品牌全名，支持卡片/列表双视图、自选收藏、中英文别名搜索与多维排序。
const MarketList = ({ symbols, quotes, futures = false, onOpen, t }) => {
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('all');
  const [viewMode, setViewMode] = useState(readViewMode);
  const [cardSort, setCardSort] = useState('default');
  const [favorites, setFavorites] = useState(() => readTradeFavorites(futures));

  const favSet = useMemo(() => new Set(favorites), [favorites]);

  const changeViewMode = (mode) => {
    setViewMode(mode);
    try {
      window.localStorage.setItem(VIEW_STORAGE_KEY, mode);
    } catch {
      // 忽略本地存储异常
    }
  };

  const toggleFavorite = (symbol, event) => {
    event.stopPropagation();
    setFavorites((prev) => {
      const next = prev.includes(symbol)
        ? prev.filter((item) => item !== symbol)
        : [...prev, symbol];
      writeTradeFavorites(futures, next);
      return next;
    });
  };

  // 单次遍历汇总涨跌分布、领涨、领跌、最热成交与各板块数量。
  const { overview, sectorCounts } = useMemo(() => {
    let upCount = 0;
    let downCount = 0;
    let topGainer = null;
    let topLoser = null;
    let topVolume = null;
    const counts = new Map();

    for (const item of symbols) {
      const meta = resolveSymbolMeta(item);
      const secKey = meta?.sector || item.kind;
      counts.set(secKey, (counts.get(secKey) || 0) + 1);

      const quote = quotes[item.symbol];
      if (!quote) continue;
      const change = changePercent(quote.price, quote.open);
      if (change !== null) {
        if (change > 0) upCount++;
        else if (change < 0) downCount++;
        if (!topGainer || change > topGainer.change) {
          topGainer = { item, quote, change };
        }
        if (!topLoser || change < topLoser.change) {
          topLoser = { item, quote, change };
        }
      }
      const vol = Number(quote.quote_volume || 0);
      if (Number.isFinite(vol) && vol > 0) {
        if (!topVolume || vol > topVolume.vol) {
          topVolume = { item, quote, vol };
        }
      }
    }

    return {
      overview: { upCount, downCount, topGainer, topLoser, topVolume },
      sectorCounts: counts,
    };
  }, [symbols, quotes]);

  const activeSectors = useMemo(
    () =>
      TRADE_SECTORS.filter((sector) => (sectorCounts.get(sector.key) || 0) > 0),
    [sectorCounts],
  );

  const matched = useMemo(() => {
    const list = symbols.filter((item) => {
      if (category === 'favorites' && !favSet.has(item.symbol)) return false;
      if (category !== 'all' && category !== 'favorites') {
        const meta = resolveSymbolMeta(item);
        if ((meta?.sector || item.kind) !== category) return false;
      }
      return matchesSymbolQuery(item, query, t);
    });

    if (viewMode !== 'cards' || cardSort === 'default') return list;

    return [...list].sort((a, b) => {
      if (cardSort === 'gainers') {
        return compareQuotes(
          changePercent(quotes[b.symbol]?.price, quotes[b.symbol]?.open),
          changePercent(quotes[a.symbol]?.price, quotes[a.symbol]?.open),
          'ascend',
        );
      }
      if (cardSort === 'losers') {
        return compareQuotes(
          changePercent(quotes[a.symbol]?.price, quotes[a.symbol]?.open),
          changePercent(quotes[b.symbol]?.price, quotes[b.symbol]?.open),
          'ascend',
        );
      }
      if (cardSort === 'volume') {
        return compareQuotes(
          quotes[b.symbol]?.quote_volume,
          quotes[a.symbol]?.quote_volume,
          'ascend',
        );
      }
      return compareQuotes(
        quotes[b.symbol]?.price,
        quotes[a.symbol]?.price,
        'ascend',
      );
    });
  }, [symbols, category, favSet, query, t, viewMode, cardSort, quotes]);

  const byQuote = (field) => (a, b, order) =>
    compareQuotes(quotes[a.symbol]?.[field], quotes[b.symbol]?.[field], order);
  const byChange = (a, b, order) =>
    compareQuotes(
      changePercent(quotes[a.symbol]?.price, quotes[a.symbol]?.open),
      changePercent(quotes[b.symbol]?.price, quotes[b.symbol]?.open),
      order,
    );

  const futuresColumns = [
    {
      title: t('标记价格'),
      dataIndex: 'mark',
      align: 'right',
      render: (_, item) => (
        <Text type='secondary' className='trade-num'>
          {formatPrice(
            quotes[item.symbol]?.mark,
            decimalsOf(item.rules?.tick_size),
          )}
        </Text>
      ),
    },
    {
      title: t('资金费率'),
      dataIndex: 'funding_rate',
      align: 'right',
      sorter: byQuote('funding_rate'),
      render: (_, item) => {
        const rate = Number(quotes[item.symbol]?.funding_rate);
        const cls =
          Number.isFinite(rate) && rate !== 0
            ? rate > 0
              ? 'trade-up'
              : 'trade-down'
            : '';
        return (
          <Text
            type={cls ? undefined : 'secondary'}
            className={`trade-num ${cls}`}
          >
            {formatFundingRate(quotes[item.symbol]?.funding_rate)}
          </Text>
        );
      },
    },
  ];

  const columns = [
    {
      title: futures ? t('合约品种') : t('交易品种'),
      dataIndex: 'ticker',
      width: 240,
      render: (_, item) => {
        const isFav = favSet.has(item.symbol);
        const region = item.session ? SESSION_REGION_LABELS[item.session] : '';
        return (
          <div className='flex items-center gap-2 whitespace-nowrap'>
            <button
              type='button'
              className={`trade-fav-btn shrink-0${isFav ? ' is-fav' : ''}`}
              aria-label={isFav ? t('取消自选') : t('加入自选')}
              onClick={(event) => toggleFavorite(item.symbol, event)}
            >
              <Star size={14} fill={isFav ? 'currentColor' : 'none'} />
            </button>
            <SymbolIdentity
              item={item}
              futures={futures}
              showSector={category === 'favorites' || !!query.trim()}
              size={30}
              t={t}
            />
            {region && (
              <Tag
                size='small'
                color='violet'
                type='light'
                className='shrink-0 !text-[10px] !px-1.5'
              >
                {t(region)}
              </Tag>
            )}
            {futures && item.max_leverage && (
              <Tag size='small' color='blue' className='shrink-0'>
                {item.max_leverage}x
              </Tag>
            )}
            {futures && !item.open && (
              <Tag size='small' color='grey' className='shrink-0'>
                {t('仅可平仓')}
              </Tag>
            )}
          </div>
        );
      },
    },
    {
      title: t('最新价'),
      dataIndex: 'price',
      align: 'right',
      sorter: byQuote('price'),
      render: (_, item) => {
        const quote = quotes[item.symbol] || {};
        const change = changePercent(quote.price, quote.open);
        return (
          <Text strong className={`trade-num ${trendClass(change)}`}>
            {formatPrice(quote.price, decimalsOf(item.rules?.tick_size))}
          </Text>
        );
      },
    },
    ...(futures ? futuresColumns : []),
    {
      title: t('24 小时涨跌'),
      dataIndex: 'change',
      align: 'right',
      sorter: byChange,
      render: (_, item) => {
        const quote = quotes[item.symbol] || {};
        const change = changePercent(quote.price, quote.open);
        const cls = change === null ? 'trade-flat' : trendClass(change);
        return (
          <span className={`trade-change-pill ${cls}`}>
            {change === null
              ? '--'
              : `${change > 0 ? '+' : ''}${change.toFixed(2)}%`}
          </span>
        );
      },
    },
    {
      title: t('24 小时区间'),
      dataIndex: 'range',
      width: 155,
      render: (_, item) => {
        const quote = quotes[item.symbol] || {};
        return (
          <RangeBar
            price={quote.price}
            low={quote.low}
            high={quote.high}
            digits={decimalsOf(item.rules?.tick_size)}
          />
        );
      },
    },
    {
      title: t('24 小时成交额'),
      dataIndex: 'volume',
      align: 'right',
      sorter: byQuote('quote_volume'),
      render: (_, item) => (
        <Text type='secondary' className='trade-num'>
          {formatCompact(quotes[item.symbol]?.quote_volume)} USDT
        </Text>
      ),
    },
    {
      title: t('24 小时走势'),
      dataIndex: 'sparkline',
      width: 112,
      render: (closes) => <Sparkline closes={closes} />,
    },
    {
      title: '',
      dataIndex: 'operate',
      width: 84,
      align: 'right',
      render: (_, item) => (
        <Button size='small' theme='light' onClick={() => onOpen(item.symbol)}>
          {t('去交易')}
        </Button>
      ),
    },
  ];

  const totalMoved = overview.upCount + overview.downCount;
  const upRatio = totalMoved > 0 ? (overview.upCount / totalMoved) * 100 : 50;
  const showGrouped =
    viewMode === 'table' && category === 'all' && !query.trim();

  const renderSectorHeader = (sector, items) => {
    const changes = items
      .map((item) =>
        changePercent(quotes[item.symbol]?.price, quotes[item.symbol]?.open),
      )
      .filter((value) => value !== null);
    const avg =
      changes.length > 0
        ? changes.reduce((sum, value) => sum + value, 0) / changes.length
        : null;
    return (
      <div className='flex items-center justify-between gap-2 mb-3'>
        <div className='flex items-center gap-2'>
          <Title heading={6} className='!mb-0'>
            {t(sector.label)}
          </Title>
          <Tag size='small' color={sector.color} type='light'>
            {t('{{count}} 品种', { count: items.length })}
          </Tag>
        </div>
        {avg !== null && (
          <span className={`trade-change-pill ${trendClass(avg)}`}>
            {t('均涨跌')} {avg > 0 ? '+' : ''}
            {avg.toFixed(2)}%
          </span>
        )}
      </div>
    );
  };

  return (
    <div className='flex flex-col gap-4'>
      {symbols.length > 0 && (
        <div className='trade-market-overview trade-market-overview-4'>
          <div className='trade-overview-card'>
            <div className='flex items-center justify-between text-xs text-[var(--semi-color-text-2)]'>
              <span className='inline-flex items-center gap-1.5'>
                <BarChart3 size={14} />
                {t('24 小时市场情绪')}
              </span>
              <span className='trade-num'>
                {t('共 {{count}} 品种', { count: symbols.length })}
              </span>
            </div>
            <div className='flex items-baseline justify-between gap-2 trade-num'>
              <span className='text-base font-semibold trade-up'>
                {t('涨 {{count}}', { count: overview.upCount })}
              </span>
              <span className='text-base font-semibold trade-down'>
                {t('跌 {{count}}', { count: overview.downCount })}
              </span>
            </div>
            <div className='trade-sentiment-bar' aria-hidden='true'>
              <span
                className='trade-sentiment-up'
                style={{ width: `${upRatio}%` }}
              />
            </div>
          </div>

          <div
            className={`trade-overview-card${overview.topGainer ? ' trade-overview-card-clickable' : ''}`}
            onClick={() =>
              overview.topGainer && onOpen(overview.topGainer.item.symbol)
            }
          >
            <div className='flex items-center gap-1.5 text-xs text-[var(--semi-color-text-2)]'>
              <TrendingUp size={14} className='trade-up' />
              <span>{t('24 小时领涨')}</span>
            </div>
            {overview.topGainer ? (
              <div className='flex items-center justify-between gap-2 trade-num'>
                <div className='flex items-center gap-2 min-w-0'>
                  <SymbolAvatar item={overview.topGainer.item} size={26} />
                  <div className='flex flex-col leading-tight min-w-0'>
                    <Text strong className='truncate text-sm'>
                      {getSymbolName(overview.topGainer.item, t)}
                    </Text>
                    <Text type='tertiary' size='small' className='!text-[11px]'>
                      {formatPrice(
                        overview.topGainer.quote.price,
                        decimalsOf(overview.topGainer.item.rules?.tick_size),
                      )}
                    </Text>
                  </div>
                </div>
                <span
                  className={`trade-change-pill shrink-0 ${trendClass(overview.topGainer.change)}`}
                >
                  {overview.topGainer.change > 0 ? '+' : ''}
                  {overview.topGainer.change.toFixed(2)}%
                </span>
              </div>
            ) : (
              <Text type='tertiary'>--</Text>
            )}
          </div>

          <div
            className={`trade-overview-card${overview.topLoser ? ' trade-overview-card-clickable' : ''}`}
            onClick={() =>
              overview.topLoser && onOpen(overview.topLoser.item.symbol)
            }
          >
            <div className='flex items-center gap-1.5 text-xs text-[var(--semi-color-text-2)]'>
              <TrendingDown size={14} className='trade-down' />
              <span>{t('24 小时领跌')}</span>
            </div>
            {overview.topLoser ? (
              <div className='flex items-center justify-between gap-2 trade-num'>
                <div className='flex items-center gap-2 min-w-0'>
                  <SymbolAvatar item={overview.topLoser.item} size={26} />
                  <div className='flex flex-col leading-tight min-w-0'>
                    <Text strong className='truncate text-sm'>
                      {getSymbolName(overview.topLoser.item, t)}
                    </Text>
                    <Text type='tertiary' size='small' className='!text-[11px]'>
                      {formatPrice(
                        overview.topLoser.quote.price,
                        decimalsOf(overview.topLoser.item.rules?.tick_size),
                      )}
                    </Text>
                  </div>
                </div>
                <span
                  className={`trade-change-pill shrink-0 ${trendClass(overview.topLoser.change)}`}
                >
                  {overview.topLoser.change > 0 ? '+' : ''}
                  {overview.topLoser.change.toFixed(2)}%
                </span>
              </div>
            ) : (
              <Text type='tertiary'>--</Text>
            )}
          </div>

          <div
            className={`trade-overview-card${overview.topVolume ? ' trade-overview-card-clickable' : ''}`}
            onClick={() =>
              overview.topVolume && onOpen(overview.topVolume.item.symbol)
            }
          >
            <div className='flex items-center gap-1.5 text-xs text-[var(--semi-color-text-2)]'>
              <Flame size={14} style={{ color: '#f59e0b' }} />
              <span>{t('24 小时热门成交')}</span>
            </div>
            {overview.topVolume ? (
              <div className='flex items-center justify-between gap-2 trade-num'>
                <div className='flex items-center gap-2 min-w-0'>
                  <SymbolAvatar item={overview.topVolume.item} size={26} />
                  <Text strong className='truncate text-sm'>
                    {getSymbolName(overview.topVolume.item, t)}
                  </Text>
                </div>
                <Text type='secondary' className='font-medium shrink-0 text-sm'>
                  {formatCompact(overview.topVolume.vol)} USDT
                </Text>
              </div>
            ) : (
              <Text type='tertiary'>--</Text>
            )}
          </div>
        </div>
      )}

      <div className='trade-card !py-3 flex flex-wrap items-center justify-between gap-3'>
        <div className='flex flex-wrap items-center gap-1.5'>
          {[
            { key: 'all', label: `${t('全部')} (${symbols.length})` },
            { key: 'favorites', label: `${t('自选')} (${favorites.length})` },
            ...activeSectors.map((sector) => ({
              key: sector.key,
              label: `${t(sector.label)} (${sectorCounts.get(sector.key)})`,
            })),
          ].map((tab) => (
            <button
              key={tab.key}
              type='button'
              className={`trade-sector-tab${category === tab.key ? ' is-active' : ''}`}
              onClick={() => setCategory(tab.key)}
            >
              {tab.key === 'favorites' && (
                <Star
                  size={12}
                  fill={favorites.length > 0 ? '#f59e0b' : 'none'}
                  style={{
                    color: favorites.length > 0 ? '#f59e0b' : 'currentColor',
                  }}
                />
              )}
              <span>{tab.label}</span>
            </button>
          ))}
        </div>

        <div className='flex flex-wrap items-center gap-2'>
          <Input
            className='trade-market-search'
            style={{ width: 260 }}
            prefix={<Search size={15} className='ml-2' />}
            value={query}
            onChange={setQuery}
            showClear
            placeholder={t('搜名称或代码，如 英伟达、OpenAI、谷歌')}
            aria-label={t('搜名称或代码，如 英伟达、OpenAI、谷歌')}
          />
          {viewMode === 'cards' && (
            <Select
              size='default'
              value={cardSort}
              style={{ width: 128 }}
              onChange={setCardSort}
              optionList={[
                { value: 'default', label: t('默认排序') },
                { value: 'gainers', label: t('涨幅优先') },
                { value: 'losers', label: t('跌幅优先') },
                { value: 'volume', label: t('成交额优先') },
                { value: 'price', label: t('价格优先') },
              ]}
            />
          )}
          <RadioGroup
            type='button'
            value={viewMode}
            onChange={(event) => changeViewMode(event.target.value)}
          >
            <Radio value='table'>
              <span className='inline-flex items-center gap-1'>
                <List size={14} />
                {t('列表')}
              </span>
            </Radio>
            <Radio value='cards'>
              <span className='inline-flex items-center gap-1'>
                <LayoutGrid size={14} />
                {t('卡片')}
              </span>
            </Radio>
          </RadioGroup>
        </div>
      </div>

      {!matched.length && (
        <div className='trade-card text-center py-8'>
          <Text type='tertiary'>
            {category === 'favorites' && !query.trim()
              ? t('暂无自选品种，点击品种左侧星标即可收藏')
              : futures
                ? t('没有匹配的合约')
                : t('没有匹配的交易对')}
          </Text>
        </div>
      )}

      {matched.length > 0 && viewMode === 'cards' && (
        <div className='trade-market-grid'>
          {matched.map((item) => (
            <MarketCard
              key={item.symbol}
              item={item}
              quote={quotes[item.symbol]}
              futures={futures}
              isFav={favSet.has(item.symbol)}
              onToggleFav={toggleFavorite}
              onOpen={onOpen}
              t={t}
            />
          ))}
        </div>
      )}

      {matched.length > 0 &&
        viewMode === 'table' &&
        showGrouped &&
        activeSectors.map((sector) => {
          const items = matched.filter(
            (item) =>
              (resolveSymbolMeta(item)?.sector || item.kind) === sector.key,
          );
          if (!items.length) return null;
          return (
            <div key={sector.key} className='trade-card'>
              {renderSectorHeader(sector, items)}
              <Table
                size='small'
                rowKey='symbol'
                columns={columns}
                dataSource={items}
                pagination={false}
                scroll={{ x: futures ? 1040 : 840 }}
                onRow={(item) => ({
                  onClick: () => onOpen(item.symbol),
                  style: { cursor: 'pointer' },
                })}
              />
            </div>
          );
        })}

      {matched.length > 0 && viewMode === 'table' && !showGrouped && (
        <div className='trade-card'>
          <Table
            size='small'
            rowKey='symbol'
            columns={columns}
            dataSource={matched}
            pagination={false}
            scroll={{ x: futures ? 1040 : 840 }}
            onRow={(item) => ({
              onClick: () => onOpen(item.symbol),
              style: { cursor: 'pointer' },
            })}
          />
        </div>
      )}

      <Text type='tertiary' size='small'>
        {futures
          ? t(
              '合约是 Binance 的 U 本位永续合约，可选全仓或逐仓：全仓由整个账户的资金兜底，逐仓每个仓位一般最多亏掉自己的保证金。杠杆上限与维持保证金率按 Binance 的风险限额档位，强平与止损按标记价格触发，止盈按最新成交价触发，资金费按 Binance 公布的费率结算。价格跳空越过强平价时，超出保证金的亏损由你承担：先扣模拟盘资金，不够的从站内额度扣，额度可以扣成负数。',
            )
          : t(
              '美股代币是 Binance 现货上 24 小时交易的代币化股票，盘口比主流加密货币薄得多，大额市价单可能只成交一部分。',
            )}
      </Text>
    </div>
  );
};

export default MarketList;
