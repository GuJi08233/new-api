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
import {
  Banner,
  Button,
  Input,
  Pagination,
  Radio,
  RadioGroup,
  Select,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { RefreshCw, Swords, TrendingDown, TrendingUp, Zap } from 'lucide-react';
import { showError, showSuccess, timestamp2string } from '../../helpers';
import {
  formatDuration,
  formatPrice,
  formatSignedUsdt,
  formatUsdt,
  toUsdt,
  tradeGet,
  tradePost,
  trendClass,
} from './api';
import { SymbolAvatar } from './SymbolIdentity';
import { formatSymbolLabel, getSymbolName } from './symbols';

const { Text, Title } = Typography;

const QUICK_STAKES = [5, 10, 25, 50, 100];
const FLASH_DURATIONS = [
  { value: 60, label: '1 分钟' },
  { value: 180, label: '3 分钟' },
  { value: 300, label: '5 分钟' },
  { value: 900, label: '15 分钟' },
  { value: 3600, label: '1 小时' },
];
const PK_INSTANT_DURATIONS = [
  { value: 300, label: '5 分钟' },
  { value: 900, label: '15 分钟' },
  { value: 3600, label: '1 小时' },
];
const RANGE_DURATIONS = [
  { value: 900, label: '15 分钟 (±0.5%)', pct: 0.5 },
  { value: 3600, label: '1 小时 (±1.5%)', pct: 1.5 },
  { value: 14400, label: '4 小时 (±2.5%)', pct: 2.5 },
];
const TOUCH_DURATIONS = [
  { value: 900, label: '15 分钟 (±1.0%)', pct: 1.0 },
  { value: 3600, label: '1 小时 (±2.0%)', pct: 2.0 },
];
const HOT_TICKERS = [
  'NVDA',
  'ANT',
  'OPENAI',
  'BTC',
  'ETH',
  'SOL',
  'GOOGL',
  'XAU',
];

const STATUS_TAG = {
  active: { color: 'blue', label: '进行中' },
  won: { color: 'green', label: '已获胜' },
  lost: { color: 'red', label: '未猜中' },
  refunded: { color: 'grey', label: '已退本' },
};

const CHOICE_LABEL = {
  UP: '看涨',
  DOWN: '看跌',
  A: '押左方 A',
  B: '押右方 B',
  B0: '大跌档',
  B1: '小跌档',
  B2: '小涨档',
  B3: '大涨档',
  TOUCH_UP: '触及上限',
  TOUCH_DOWN: '触及下限',
  NO_TOUCH: '全程未触及',
  REFUND: '平局退本',
};

const ShortEventsView = ({
  section = 'pk',
  self,
  perUnit,
  spotSymbols = [],
  futuresSymbols = [],
  spotQuotes = {},
  futuresQuotes = {},
  onAccountChanged,
  t,
}) => {
  const [config, setConfig] = useState(null);
  const [orders, setOrders] = useState({ items: [], total: 0 });
  const [orderFilter, setOrderFilter] = useState('all');
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState('');
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000));

  // PK 擂台状态
  const [pkMode, setPkMode] = useState('pool');
  const [pkPoolDuration, setPkPoolDuration] = useState(900);
  const [pkInstantDuration, setPkInstantDuration] = useState(900);
  const [pkStake, setPkStake] = useState('10');

  // 极速涨跌状态
  const [flashSymbol, setFlashSymbol] = useState('BTCUSDT');
  const [flashDuration, setFlashDuration] = useState(180);
  const [flashStake, setFlashStake] = useState('10');

  // 区间 & 触碰共享奖池状态
  const [rtSub, setRtSub] = useState('range');
  const [rangeDuration, setRangeDuration] = useState(3600);
  const [rangeStake, setRangeStake] = useState('10');
  const [touchDuration, setTouchDuration] = useState(900);
  const [touchStake, setTouchStake] = useState('10');

  // 合并所有可用交易品种(优先永续合约，覆盖全部 47 个品种)
  const symbolPool = useMemo(() => {
    const map = new Map();
    for (const s of futuresSymbols) {
      if (s.open !== false) map.set(s.ticker, s);
    }
    for (const s of spotSymbols) {
      if (s.open !== false && !map.has(s.ticker)) map.set(s.ticker, s);
    }
    return [...map.values()];
  }, [spotSymbols, futuresSymbols]);

  const quoteOf = useCallback(
    (symbol) => {
      if (!symbol) return null;
      return (
        futuresQuotes[symbol] ||
        spotQuotes[symbol] ||
        futuresSymbols.find((s) => s.symbol === symbol)?.quote ||
        spotSymbols.find((s) => s.symbol === symbol)?.quote ||
        null
      );
    },
    [futuresQuotes, spotQuotes, futuresSymbols, spotSymbols],
  );

  // 初始化默认选中可用标的代码
  useEffect(() => {
    if (!symbolPool.length) return;
    if (!symbolPool.some((s) => s.symbol === flashSymbol)) {
      setFlashSymbol(symbolPool[0].symbol);
    }
  }, [symbolPool, flashSymbol]);

  const loadConfig = useCallback(async () => {
    const res = await tradeGet('/api/trade/short/config', t, {
      pk_duration: pkPoolDuration,
      range_duration: rangeDuration,
      touch_duration: touchDuration,
    });
    if (res.data) setConfig(res.data);
  }, [pkPoolDuration, rangeDuration, touchDuration, t]);

  const loadOrders = useCallback(async () => {
    setLoading(true);
    const res = await tradeGet('/api/trade/short/orders', t, {
      kind: section === 'range' ? '' : section,
      status: orderFilter,
      page,
      page_size: 10,
    });
    setLoading(false);
    if (res.data) {
      setOrders({ items: res.data.items || [], total: res.data.total || 0 });
    }
  }, [section, orderFilter, page, t]);

  useEffect(() => {
    loadConfig();
    loadOrders();
    const timer = setInterval(() => {
      if (!document.hidden) {
        loadConfig();
        loadOrders();
      }
    }, 5000);
    return () => clearInterval(timer);
  }, [loadConfig, loadOrders]);

  useEffect(() => {
    const timer = setInterval(
      () => setNow(Math.floor(Date.now() / 1000)),
      1000,
    );
    return () => clearInterval(timer);
  }, []);

  const submitOrder = async (key, payload, successMsg) => {
    setSubmitting(key);
    const res = await tradePost('/api/trade/short/orders', payload, t);
    setSubmitting('');
    if (res.error) {
      showError(res.error);
      return;
    }
    showSuccess(successMsg || t('短线委托已生效，到期自动结算'));
    onAccountChanged?.();
    loadConfig();
    loadOrders();
  };

  const basePayoutX = (
    (config?.payout_bps || self?.short_events?.payout_bps || 18500) / 10000
  ).toFixed(2);
  const poolFeePct = ((config?.fee_bps || 300) / 100).toFixed(1);
  const maxOrderUsd =
    config?.max_order_usd || self?.short_events?.max_order_usd || 100;
  const cashUsdt = toUsdt(self?.account?.cash, perUnit);
  const hasBonus = Number(self?.account?.bonus_principal || 0) > 0;

  const symbolOptions = useMemo(
    () =>
      symbolPool.map((s) => ({
        value: s.symbol,
        label: `${formatSymbolLabel(s, t)} (${s.symbol})`,
      })),
    [symbolPool, t],
  );

  const hotItems = useMemo(
    () =>
      HOT_TICKERS.map((tk) => symbolPool.find((s) => s.ticker === tk)).filter(
        Boolean,
      ),
    [symbolPool],
  );

  const orderColumns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      width: 155,
      render: (v) => timestamp2string(v),
    },
    {
      title: t('玩法'),
      dataIndex: 'kind',
      width: 110,
      render: (kind, row) => (
        <div className='flex items-center gap-1'>
          <Tag size='small' color='blue'>
            {{
              pk: t('PK擂台'),
              flash: t('极速涨跌'),
              range: t('区间竞猜'),
              touch: t('极速触碰'),
            }[kind] || kind}
          </Tag>
          {row.mode === 'pool' && (
            <Tag size='small' color='violet'>
              {t('奖池')}
            </Tag>
          )}
        </div>
      ),
    },
    {
      title: t('标的'),
      dataIndex: 'symbol_a',
      render: (_, row) =>
        row.symbol_b
          ? `${getSymbolName(row.symbol_a, t)} vs ${getSymbolName(row.symbol_b, t)}`
          : formatSymbolLabel(row.symbol_a, t),
    },
    {
      title: t('方向 / 档位'),
      dataIndex: 'choice',
      render: (choice, row) => {
        if (row.kind === 'pk') {
          const picked = choice === 'A' ? row.symbol_a : row.symbol_b;
          return (
            <Tag color={choice === 'A' ? 'blue' : 'violet'}>
              {getSymbolName(picked, t)}
            </Tag>
          );
        }
        return (
          <Tag
            color={
              choice === 'UP' ||
              choice === 'TOUCH_UP' ||
              choice === 'B3' ||
              choice === 'B2'
                ? 'green'
                : choice === 'NO_TOUCH'
                  ? 'amber'
                  : 'red'
            }
          >
            {t(CHOICE_LABEL[choice] || choice)}
          </Tag>
        );
      },
    },
    {
      title: t('锚定价 → 结算价'),
      render: (_, row) => (
        <span className='trade-num text-xs'>
          {row.entry_price_a ? formatPrice(row.entry_price_a) : '--'} →{' '}
          {row.settle_price_a ? formatPrice(row.settle_price_a) : '--'}
        </span>
      ),
    },
    {
      title: t('投入 / 派彩'),
      align: 'right',
      render: (_, row) => (
        <span className='trade-num text-xs'>
          {formatUsdt(row.stake, perUnit)} / {formatUsdt(row.payout, perUnit)}{' '}
          USDT
        </span>
      ),
    },
    {
      title: t('盈亏'),
      align: 'right',
      render: (_, row) => {
        if (row.status === 'active') {
          const rem = Math.max(0, row.expires_at - now);
          return (
            <Tag size='small' color='blue'>
              {rem > 0 ? formatDuration(rem, t) : t('结算中')}
            </Tag>
          );
        }
        const pnl = row.payout - row.stake;
        return (
          <span className={`trade-num font-semibold ${trendClass(pnl)}`}>
            {formatSignedUsdt(pnl, perUnit)} USDT
          </span>
        );
      },
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      render: (status) => {
        const info = STATUS_TAG[status] || STATUS_TAG.active;
        return <Tag color={info.color}>{t(info.label)}</Tag>;
      },
    },
  ];

  // ==================== 1. AI 擂台 PK ====================
  const renderPKSection = () => {
    const pkRounds = config?.pk_rounds || [];
    const matchups = config?.pk_matchups || [];
    return (
      <div className='flex flex-col gap-4'>
        <div className='trade-card !py-3 flex flex-wrap items-center justify-between gap-3'>
          <div className='flex flex-wrap items-center gap-3'>
            <RadioGroup
              type='button'
              value={pkMode}
              onChange={(e) => setPkMode(e.target.value)}
            >
              <Radio value='pool'>{t('多人奖池场 (零风险分池)')}</Radio>
              <Radio value='instant'>
                {t('即时 1v1 对决 ({{x}}x)', { x: basePayoutX })}
              </Radio>
            </RadioGroup>
            {pkMode === 'pool' ? (
              <RadioGroup
                type='button'
                size='small'
                value={pkPoolDuration}
                onChange={(e) => setPkPoolDuration(e.target.value)}
              >
                <Radio value={900}>{t('15 分钟场')}</Radio>
                <Radio value={3600}>{t('1 小时场')}</Radio>
              </RadioGroup>
            ) : (
              <RadioGroup
                type='button'
                size='small'
                value={pkInstantDuration}
                onChange={(e) => setPkInstantDuration(e.target.value)}
              >
                {PK_INSTANT_DURATIONS.map((d) => (
                  <Radio key={d.value} value={d.value}>
                    {t(d.label)}
                  </Radio>
                ))}
              </RadioGroup>
            )}
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              size='small'
              style={{ width: 150 }}
              value={pkStake}
              onChange={setPkStake}
              prefix={t('投入')}
              suffix='USDT'
              inputMode='decimal'
            />
            {QUICK_STAKES.slice(0, 4).map((val) => (
              <Button
                key={val}
                size='small'
                theme={pkStake === String(val) ? 'solid' : 'light'}
                onClick={() => setPkStake(String(val))}
              >
                ${val}
              </Button>
            ))}
          </div>
        </div>

        <Banner
          type='info'
          closeIcon={null}
          description={
            pkMode === 'pool'
              ? t(
                  '多人奖池擂台：开赛瞬间即封盘（零盘中偷跑），到期比较两强涨跌幅高低，胜方按本金占比瓜分总奖池（抽成 {{fee}}%）；仅一边押注或打平 100% 退本；持有体验金暂不可参与奖池。',
                  { fee: poolFeePct },
                )
              : t(
                  '即时 1v1 对决：下单瞬间按同源实时盘口中间价锁定双方入场价（点差 ≤ 0.2%），到期同源结算，胜出派彩 {{x}}x，打平退本；禁止同组合双向对冲，单笔上限 {{max}} USDT。',
                  { x: basePayoutX, max: maxOrderUsd },
                )
          }
        />

        {pkMode === 'pool' ? (
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            {pkRounds.map((round) => {
              const poolA = round.pools?.A || 0;
              const poolB = round.pools?.B || 0;
              const sum = poolA + poolB;
              const pctA = sum > 0 ? Math.round((poolA / sum) * 100) : 50;
              const pctB = 100 - pctA;
              const lockRem = Math.max(0, round.lock_time - now);
              const endRem = Math.max(0, round.end_time - now);
              const multA = round.multipliers?.A;
              const multB = round.multipliers?.B;
              const nameA = getSymbolName(round.symbol_a, t);
              const nameB = getSymbolName(round.symbol_b, t);

              return (
                <div
                  key={round.id}
                  className='trade-card trade-pk-card flex flex-col gap-3'
                >
                  <div className='flex items-center justify-between gap-2'>
                    <div className='flex items-center gap-2'>
                      <Swords
                        size={16}
                        className='text-[var(--semi-color-primary)]'
                      />
                      <Text strong>
                        {nameA} vs {nameB}
                      </Text>
                      <Tag size='small' color='violet'>
                        {round.duration_sec === 3600
                          ? t('1 小时')
                          : t('15 分钟')}
                      </Tag>
                    </div>
                    <Tag size='small' color={round.locked ? 'orange' : 'green'}>
                      {round.locked
                        ? t('已封盘 · {{time}}后结算', {
                            time: formatDuration(endRem, t),
                          })
                        : t('{{time}}后封盘', {
                            time: formatDuration(lockRem, t),
                          })}
                    </Tag>
                  </div>

                  <div className='grid grid-cols-[1fr_auto_1fr] items-center gap-2'>
                    <div className='flex items-center gap-2 min-w-0'>
                      <SymbolAvatar item={round.symbol_a} size={32} />
                      <div className='flex flex-col leading-tight min-w-0'>
                        <Text strong className='truncate'>
                          {nameA}
                        </Text>
                        <Text
                          type='tertiary'
                          size='small'
                          className='trade-num'
                        >
                          {round.price_a ? formatPrice(round.price_a) : '--'}
                        </Text>
                      </div>
                    </div>
                    <span className='trade-pk-vs'>VS</span>
                    <div className='flex items-center justify-end gap-2 min-w-0 text-right'>
                      <div className='flex flex-col leading-tight min-w-0'>
                        <Text strong className='truncate'>
                          {nameB}
                        </Text>
                        <Text
                          type='tertiary'
                          size='small'
                          className='trade-num'
                        >
                          {round.price_b ? formatPrice(round.price_b) : '--'}
                        </Text>
                      </div>
                      <SymbolAvatar item={round.symbol_b} size={32} />
                    </div>
                  </div>

                  <div className='flex flex-col gap-1'>
                    <div className='flex justify-between text-xs trade-num text-[var(--semi-color-text-2)]'>
                      <span>
                        {nameA} {pctA}% ({formatUsdt(poolA, perUnit)} USDT)
                      </span>
                      <span>
                        {nameB} {pctB}% ({formatUsdt(poolB, perUnit)} USDT)
                      </span>
                    </div>
                    <div className='trade-pk-bar'>
                      <span
                        className='trade-pk-bar-a'
                        style={{ width: `${pctA}%` }}
                      />
                    </div>
                  </div>

                  <div className='grid grid-cols-2 gap-2'>
                    <Button
                      theme='solid'
                      type='primary'
                      disabled={round.locked || hasBonus}
                      loading={submitting === `pk-pool-${round.id}-A`}
                      onClick={() =>
                        submitOrder(`pk-pool-${round.id}-A`, {
                          kind: 'pk',
                          mode: 'pool',
                          round_id: round.id,
                          choice: 'A',
                          amount: pkStake,
                        })
                      }
                    >
                      {t('支持 {{name}}', { name: nameA })}
                      {multA ? ` · ${multA.toFixed(2)}x` : ''}
                    </Button>
                    <Button
                      theme='solid'
                      type='secondary'
                      disabled={round.locked || hasBonus}
                      loading={submitting === `pk-pool-${round.id}-B`}
                      onClick={() =>
                        submitOrder(`pk-pool-${round.id}-B`, {
                          kind: 'pk',
                          mode: 'pool',
                          round_id: round.id,
                          choice: 'B',
                          amount: pkStake,
                        })
                      }
                    >
                      {t('支持 {{name}}', { name: nameB })}
                      {multB ? ` · ${multB.toFixed(2)}x` : ''}
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        ) : (
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            {matchups.map((m) => {
              const nameA = getSymbolName(m.ticker_a, t);
              const nameB = getSymbolName(m.ticker_b, t);
              const pA = quoteOf(m.symbol_a)?.price;
              const pB = quoteOf(m.symbol_b)?.price;
              return (
                <div
                  key={m.id}
                  className='trade-card trade-pk-card flex flex-col gap-3'
                >
                  <div className='flex items-center justify-between'>
                    <Text strong>
                      {nameA} vs {nameB}
                    </Text>
                    <Tag size='small' color='green'>
                      {t('即时锁价 · {{x}}x', { x: basePayoutX })}
                    </Tag>
                  </div>
                  <div className='grid grid-cols-[1fr_auto_1fr] items-center gap-2'>
                    <div className='flex items-center gap-2 min-w-0'>
                      <SymbolAvatar item={m.ticker_a} size={32} />
                      <div className='flex flex-col leading-tight min-w-0'>
                        <Text strong className='truncate'>
                          {nameA}
                        </Text>
                        <Text
                          type='tertiary'
                          size='small'
                          className='trade-num'
                        >
                          {pA ? formatPrice(pA) : '--'}
                        </Text>
                      </div>
                    </div>
                    <span className='trade-pk-vs'>VS</span>
                    <div className='flex items-center justify-end gap-2 min-w-0 text-right'>
                      <div className='flex flex-col leading-tight min-w-0'>
                        <Text strong className='truncate'>
                          {nameB}
                        </Text>
                        <Text
                          type='tertiary'
                          size='small'
                          className='trade-num'
                        >
                          {pB ? formatPrice(pB) : '--'}
                        </Text>
                      </div>
                      <SymbolAvatar item={m.ticker_b} size={32} />
                    </div>
                  </div>
                  <div className='grid grid-cols-2 gap-2'>
                    <Button
                      theme='solid'
                      type='primary'
                      loading={submitting === `pk-inst-${m.id}-A`}
                      onClick={() =>
                        submitOrder(`pk-inst-${m.id}-A`, {
                          kind: 'pk',
                          mode: 'instant',
                          symbol_a: m.symbol_a,
                          symbol_b: m.symbol_b,
                          choice: 'A',
                          duration_sec: pkInstantDuration,
                          amount: pkStake,
                        })
                      }
                    >
                      {t('{{name}} 涨幅更高', { name: nameA })}
                    </Button>
                    <Button
                      theme='solid'
                      type='secondary'
                      loading={submitting === `pk-inst-${m.id}-B`}
                      onClick={() =>
                        submitOrder(`pk-inst-${m.id}-B`, {
                          kind: 'pk',
                          mode: 'instant',
                          symbol_a: m.symbol_a,
                          symbol_b: m.symbol_b,
                          choice: 'B',
                          duration_sec: pkInstantDuration,
                          amount: pkStake,
                        })
                      }
                    >
                      {t('{{name}} 涨幅更高', { name: nameB })}
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    );
  };

  // ==================== 2. 全品种极速涨跌 ====================
  const renderFlashSection = () => {
    const q = quoteOf(flashSymbol);
    const estPayout = ((Number(flashStake) || 0) * Number(basePayoutX)).toFixed(
      2,
    );
    return (
      <div className='grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_360px]'>
        <div className='trade-card flex flex-col gap-4'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='flex items-center gap-2'>
              <Zap size={18} className='text-[var(--semi-color-primary)]' />
              <Title heading={5} className='!mb-0'>
                {t('全品种极速涨跌事件合约')}
              </Title>
              <Tag color='green'>
                {t('猜中即得 {{x}}x', { x: basePayoutX })}
              </Tag>
            </div>
            <Text type='tertiary' size='small' className='trade-num'>
              {t('可用资金 {{amount}} USDT', { amount: cashUsdt.toFixed(2) })}
            </Text>
          </div>

          <div className='flex flex-wrap items-center gap-1.5'>
            {hotItems.map((item) => (
              <button
                key={item.symbol}
                type='button'
                className={`trade-sector-tab${flashSymbol === item.symbol ? ' is-active' : ''}`}
                onClick={() => setFlashSymbol(item.symbol)}
              >
                {getSymbolName(item, t)}
              </button>
            ))}
          </div>

          <div className='flex flex-wrap items-center gap-3'>
            <Select
              filter
              style={{ width: 260 }}
              value={flashSymbol}
              optionList={symbolOptions}
              onChange={setFlashSymbol}
            />
            <div className='flex items-center gap-2.5'>
              <SymbolAvatar item={flashSymbol} size={36} />
              <div className='flex flex-col leading-tight'>
                <Text strong className='text-base'>
                  {formatSymbolLabel(flashSymbol, t)}
                </Text>
                <span className='trade-num text-xl font-bold'>
                  {q?.price ? `${formatPrice(q.price)} USDT` : '--'}
                </span>
              </div>
            </div>
          </div>

          <div className='flex flex-col gap-2'>
            <Text type='tertiary' size='small'>
              {t('选择结算周期（下单瞬间按 Binance 实时价锁定入场价）')}
            </Text>
            <RadioGroup
              type='button'
              value={flashDuration}
              onChange={(e) => setFlashDuration(e.target.value)}
            >
              {FLASH_DURATIONS.map((d) => (
                <Radio key={d.value} value={d.value}>
                  {t(d.label)}
                </Radio>
              ))}
            </RadioGroup>
          </div>

          <Banner
            type='info'
            closeIcon={null}
            description={t(
              '支持全部流动性充足的开放品种（盘口点差 ≤ 0.2%）。下单与结算严格使用同一市场实时盘口中间价，到期高于入场价判涨、低于判跌、相等 100% 退本；禁止同标的双向对冲，单笔 1–{{max}} USDT。',
              { max: maxOrderUsd },
            )}
          />
        </div>

        <div className='trade-card flex flex-col gap-3'>
          <Text strong>{t('极速下单')}</Text>
          <Input
            value={flashStake}
            onChange={setFlashStake}
            prefix={t('投入金额')}
            suffix='USDT'
            inputMode='decimal'
          />
          <div className='grid grid-cols-5 gap-1'>
            {QUICK_STAKES.map((v) => (
              <Button
                key={v}
                size='small'
                theme={flashStake === String(v) ? 'solid' : 'light'}
                onClick={() => setFlashStake(String(v))}
              >
                ${v}
              </Button>
            ))}
          </div>
          <div className='trade-calc-result flex flex-col gap-1 text-xs trade-num'>
            <div className='flex justify-between'>
              <Text type='tertiary' size='small'>
                {t('胜出派彩倍数')}
              </Text>
              <Text strong className='trade-up'>
                {basePayoutX}x
              </Text>
            </div>
            <div className='flex justify-between'>
              <Text type='tertiary' size='small'>
                {t('猜中预计到账')}
              </Text>
              <Text strong className='trade-up'>
                {estPayout} USDT
              </Text>
            </div>
          </div>
          <div className='grid grid-cols-2 gap-2 pt-1'>
            <Button
              size='large'
              theme='solid'
              icon={<TrendingUp size={16} />}
              style={{ background: 'var(--semi-color-success)' }}
              loading={submitting === 'flash-UP'}
              onClick={() =>
                submitOrder('flash-UP', {
                  kind: 'flash',
                  mode: 'instant',
                  symbol_a: flashSymbol,
                  choice: 'UP',
                  duration_sec: flashDuration,
                  amount: flashStake,
                })
              }
            >
              {t('看涨')}
            </Button>
            <Button
              size='large'
              theme='solid'
              type='danger'
              icon={<TrendingDown size={16} />}
              loading={submitting === 'flash-DOWN'}
              onClick={() =>
                submitOrder('flash-DOWN', {
                  kind: 'flash',
                  mode: 'instant',
                  symbol_a: flashSymbol,
                  choice: 'DOWN',
                  duration_sec: flashDuration,
                  amount: flashStake,
                })
              }
            >
              {t('看跌')}
            </Button>
          </div>
        </div>
      </div>
    );
  };

  // ==================== 3. 价格区间 & 极速触碰（多人共享奖池） ====================
  const renderRangeTouchSection = () => {
    const activeStake = rtSub === 'range' ? rangeStake : touchStake;
    const setActiveStake = rtSub === 'range' ? setRangeStake : setTouchStake;

    return (
      <div className='flex flex-col gap-4'>
        <div className='trade-card !py-3 flex flex-wrap items-center justify-between gap-3'>
          <div className='flex flex-wrap items-center gap-3'>
            <RadioGroup
              type='button'
              value={rtSub}
              onChange={(e) => setRtSub(e.target.value)}
            >
              <Radio value='range'>{t('4 档价格区间奖池')}</Radio>
              <Radio value='touch'>{t('极速触碰奖池 (触及即刻派彩)')}</Radio>
            </RadioGroup>
            {rtSub === 'range' ? (
              <RadioGroup
                type='button'
                size='small'
                value={rangeDuration}
                onChange={(e) => setRangeDuration(e.target.value)}
              >
                {RANGE_DURATIONS.map((d) => (
                  <Radio key={d.value} value={d.value}>
                    {t(d.label)}
                  </Radio>
                ))}
              </RadioGroup>
            ) : (
              <RadioGroup
                type='button'
                size='small'
                value={touchDuration}
                onChange={(e) => setTouchDuration(e.target.value)}
              >
                {TOUCH_DURATIONS.map((d) => (
                  <Radio key={d.value} value={d.value}>
                    {t(d.label)}
                  </Radio>
                ))}
              </RadioGroup>
            )}
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              size='small'
              style={{ width: 150 }}
              value={activeStake}
              onChange={setActiveStake}
              prefix={t('投入')}
              suffix='USDT'
              inputMode='decimal'
            />
            {QUICK_STAKES.slice(0, 4).map((val) => (
              <Button
                key={val}
                size='small'
                theme={activeStake === String(val) ? 'solid' : 'light'}
                onClick={() => setActiveStake(String(val))}
              >
                ${val}
              </Button>
            ))}
          </div>
        </div>

        <Banner
          type={hasBonus ? 'warning' : 'info'}
          closeIcon={null}
          description={
            hasBonus
              ? t(
                  '当前账户持有未结清体验金，暂不可参与多人共享奖池局，请先提盈回收或放弃体验金后再下注。',
                )
              : rtSub === 'range'
                ? t(
                    '4 档区间共享奖池：开赛瞬间即封盘（零盘中偷跑），到期按同源价格涨跌幅落入对应区间，胜方瓜分总奖池（抽成 {{fee}}%）；价格持平、单边押注或无人猜中 100% 退本；同局仅可押一个档位。',
                    { fee: poolFeePct },
                  )
                : t(
                    '极速触碰共享奖池：开赛瞬间即封盘，开赛后盘中实时中间价率先触及上轨或下轨即刻提前派彩，全程未触及则「全程未触及」获胜（抽成 {{fee}}%）；双向触及、单边押注或无人猜中 100% 退本；同局仅可押一个方向。',
                    { fee: poolFeePct },
                  )
          }
        />

        {rtSub === 'range' && (
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            {(config?.range_rounds || []).map((round) => {
              const th = (round.threshold_bps || 150) / 100;
              const lockRem = Math.max(0, round.lock_time - now);
              const endRem = Math.max(0, round.end_time - now);
              const refPrice = Number(
                round.start_price_a || round.price_a || 0,
              );
              const upperP = refPrice > 0 ? refPrice * (1 + th / 100) : 0;
              const lowerP = refPrice > 0 ? refPrice * (1 - th / 100) : 0;
              const disabled = round.locked || hasBonus;
              return (
                <div key={round.id} className='trade-card flex flex-col gap-3'>
                  <div className='flex items-center justify-between gap-2'>
                    <div className='flex items-center gap-2 min-w-0'>
                      <SymbolAvatar item={round.symbol_a} size={28} />
                      <div className='flex flex-col leading-tight min-w-0'>
                        <Text strong className='truncate'>
                          {formatSymbolLabel(round.symbol_a, t)}
                        </Text>
                        <Text
                          type='tertiary'
                          size='small'
                          className='trade-num'
                        >
                          {refPrice > 0 ? formatPrice(refPrice) : '--'} ·{' '}
                          {t('总池 {{amt}} USDT', {
                            amt: formatUsdt(round.total_pool, perUnit),
                          })}
                        </Text>
                      </div>
                    </div>
                    <Tag size='small' color={round.locked ? 'orange' : 'green'}>
                      {round.locked
                        ? t('已封盘 · {{time}}后结算', {
                            time: formatDuration(endRem, t),
                          })
                        : t('{{time}}后开赛封盘', {
                            time: formatDuration(lockRem, t),
                          })}
                    </Tag>
                  </div>
                  <div className='grid grid-cols-2 gap-2'>
                    {[
                      [
                        'B0',
                        `< -${th}%`,
                        lowerP > 0 ? `< ${formatPrice(lowerP)}` : '',
                      ],
                      [
                        'B1',
                        `-${th}% ~ 0%`,
                        lowerP > 0
                          ? `${formatPrice(lowerP)}~${formatPrice(refPrice)}`
                          : '',
                      ],
                      [
                        'B2',
                        `0% ~ +${th}%`,
                        upperP > 0
                          ? `${formatPrice(refPrice)}~${formatPrice(upperP)}`
                          : '',
                      ],
                      [
                        'B3',
                        `> +${th}%`,
                        upperP > 0 ? `> ${formatPrice(upperP)}` : '',
                      ],
                    ].map(([code, lbl, bound]) => {
                      const mult = round.multipliers?.[code];
                      const sideAmt = round.pools?.[code] || 0;
                      return (
                        <Button
                          key={code}
                          size='small'
                          theme='light'
                          disabled={disabled}
                          loading={
                            submitting === `range-pool-${round.id}-${code}`
                          }
                          onClick={() =>
                            submitOrder(`range-pool-${round.id}-${code}`, {
                              kind: 'range',
                              mode: 'pool',
                              round_id: round.id,
                              choice: code,
                              amount: rangeStake,
                            })
                          }
                        >
                          <span className='trade-num'>
                            {lbl}
                            {mult
                              ? ` · ${mult.toFixed(2)}x`
                              : sideAmt > 0
                                ? ` · ${formatUsdt(sideAmt, perUnit)}`
                                : bound
                                  ? ` (${bound})`
                                  : ''}
                          </span>
                        </Button>
                      );
                    })}
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {rtSub === 'touch' && (
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            {(config?.touch_rounds || []).map((round) => {
              const th = (round.threshold_bps || 100) / 100;
              const lockRem = Math.max(0, round.lock_time - now);
              const endRem = Math.max(0, round.end_time - now);
              const refPrice = Number(
                round.start_price_a || round.price_a || 0,
              );
              const upperP = refPrice > 0 ? refPrice * (1 + th / 100) : 0;
              const lowerP = refPrice > 0 ? refPrice * (1 - th / 100) : 0;
              const disabled = round.locked || hasBonus;
              const multUp = round.multipliers?.TOUCH_UP;
              const multDown = round.multipliers?.TOUCH_DOWN;
              const multNo = round.multipliers?.NO_TOUCH;

              return (
                <div key={round.id} className='trade-card flex flex-col gap-3'>
                  <div className='flex items-center justify-between gap-2'>
                    <div className='flex items-center gap-2 min-w-0'>
                      <SymbolAvatar item={round.symbol_a} size={28} />
                      <div className='flex flex-col leading-tight min-w-0'>
                        <Text strong className='truncate'>
                          {formatSymbolLabel(round.symbol_a, t)}
                        </Text>
                        <Text
                          type='tertiary'
                          size='small'
                          className='trade-num'
                        >
                          {refPrice > 0 ? formatPrice(refPrice) : '--'} ·{' '}
                          {t('总池 {{amt}} USDT', {
                            amt: formatUsdt(round.total_pool, perUnit),
                          })}
                        </Text>
                      </div>
                    </div>
                    <Tag size='small' color={round.locked ? 'orange' : 'green'}>
                      {round.locked
                        ? t('已封盘 · {{time}}后结算', {
                            time: formatDuration(endRem, t),
                          })
                        : t('{{time}}后开赛封盘', {
                            time: formatDuration(lockRem, t),
                          })}
                    </Tag>
                  </div>
                  <div className='grid grid-cols-3 gap-2 text-xs trade-num'>
                    <div className='trade-stat-chip !p-2'>
                      <Text
                        type='tertiary'
                        size='small'
                        className='!text-[11px]'
                      >
                        {t('上轨 (+{{p}}%)', { p: th })}
                      </Text>
                      <div className='font-semibold trade-up'>
                        {upperP > 0 ? formatPrice(upperP) : '--'}
                      </div>
                    </div>
                    <div className='trade-stat-chip !p-2'>
                      <Text
                        type='tertiary'
                        size='small'
                        className='!text-[11px]'
                      >
                        {t('参考价')}
                      </Text>
                      <div className='font-semibold'>
                        {refPrice > 0 ? formatPrice(refPrice) : '--'}
                      </div>
                    </div>
                    <div className='trade-stat-chip !p-2'>
                      <Text
                        type='tertiary'
                        size='small'
                        className='!text-[11px]'
                      >
                        {t('下轨 (-{{p}}%)', { p: th })}
                      </Text>
                      <div className='font-semibold trade-down'>
                        {lowerP > 0 ? formatPrice(lowerP) : '--'}
                      </div>
                    </div>
                  </div>
                  <div className='grid grid-cols-3 gap-2'>
                    <Button
                      size='small'
                      theme='solid'
                      disabled={disabled}
                      style={{ background: 'var(--semi-color-success)' }}
                      loading={submitting === `touch-pool-${round.id}-UP`}
                      onClick={() =>
                        submitOrder(`touch-pool-${round.id}-UP`, {
                          kind: 'touch',
                          mode: 'pool',
                          round_id: round.id,
                          choice: 'TOUCH_UP',
                          amount: touchStake,
                        })
                      }
                    >
                      {t('触及上限')}
                      {multUp ? ` · ${multUp.toFixed(2)}x` : ''}
                    </Button>
                    <Button
                      size='small'
                      theme='solid'
                      type='danger'
                      disabled={disabled}
                      loading={submitting === `touch-pool-${round.id}-DOWN`}
                      onClick={() =>
                        submitOrder(`touch-pool-${round.id}-DOWN`, {
                          kind: 'touch',
                          mode: 'pool',
                          round_id: round.id,
                          choice: 'TOUCH_DOWN',
                          amount: touchStake,
                        })
                      }
                    >
                      {t('触及下限')}
                      {multDown ? ` · ${multDown.toFixed(2)}x` : ''}
                    </Button>
                    <Button
                      size='small'
                      theme='light'
                      type='warning'
                      disabled={disabled}
                      loading={submitting === `touch-pool-${round.id}-NO`}
                      onClick={() =>
                        submitOrder(`touch-pool-${round.id}-NO`, {
                          kind: 'touch',
                          mode: 'pool',
                          round_id: round.id,
                          choice: 'NO_TOUCH',
                          amount: touchStake,
                        })
                      }
                    >
                      {t('全程未触及')}
                      {multNo ? ` · ${multNo.toFixed(2)}x` : ''}
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className='flex flex-col gap-4'>
      {section === 'pk' && renderPKSection()}
      {section === 'flash' && renderFlashSection()}
      {section === 'range' && renderRangeTouchSection()}

      <div className='trade-card flex flex-col gap-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <Title heading={6} className='!mb-0'>
            {t('我的短线记录')}
          </Title>
          <div className='flex items-center gap-2'>
            <RadioGroup
              type='button'
              size='small'
              value={orderFilter}
              onChange={(e) => {
                setOrderFilter(e.target.value);
                setPage(1);
              }}
            >
              <Radio value='all'>{t('全部')}</Radio>
              <Radio value='active'>{t('进行中')}</Radio>
            </RadioGroup>
            <Button
              size='small'
              icon={<RefreshCw size={13} />}
              loading={loading}
              onClick={loadOrders}
            />
          </div>
        </div>
        <Table
          size='small'
          rowKey='id'
          columns={orderColumns}
          dataSource={orders.items}
          pagination={false}
          scroll={{ x: 860 }}
          empty={<Text type='tertiary'>{t('暂无短线记录')}</Text>}
        />
        {orders.total > 10 && (
          <Pagination
            total={orders.total}
            pageSize={10}
            currentPage={page}
            onPageChange={setPage}
          />
        )}
      </div>
    </div>
  );
};

export default ShortEventsView;
