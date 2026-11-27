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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  Banner,
  Button,
  Radio,
  RadioGroup,
  Spin,
  Switch,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  ArrowUpRight,
  Crosshair,
  Eye,
  EyeOff,
  FastForward,
  Flag,
  Pause,
  Play,
  RotateCcw,
  Sparkles,
  TrendingDown,
  TrendingUp,
  Trophy,
} from 'lucide-react';
import {
  CandlestickSeries,
  CrosshairMode,
  HistogramSeries,
  LineStyle,
  createChart,
  createSeriesMarkers,
} from 'lightweight-charts';
import { useActualTheme } from '../../context/Theme';
import { timestamp2string } from '../../helpers';
import { cssColor, formatPrice, tradeGet, trendClass, withAlpha } from './api';
import { SymbolAvatar } from './SymbolIdentity';
import { TRADE_SYMBOL_META, formatSymbolLabel } from './symbols';
import {
  ARENA_INITIAL_CASH,
  ARENA_PLAY_BARS,
  ARENA_WARMUP_BARS,
  calcArenaMetrics,
  closeArenaPosition,
  createArenaState,
  finishArenaEarly,
  generateSyntheticCandles,
  gradeArenaResult,
  openArenaPosition,
  readArenaCareerStats,
  recordArenaRound,
  stepArenaCandle,
} from './arenaEngine';

const { Text, Title } = Typography;

const INTERVAL_OPTIONS = [
  { value: '5m', label: '5分 极速' },
  { value: '15m', label: '15分 日内' },
  { value: '1h', label: '1小时 波段' },
  { value: '4h', label: '4小时 趋势' },
];

const LEVERAGES = [1, 3, 5, 10, 20];
const FRACTIONS = [
  { value: 0.25, label: '25%' },
  { value: 0.5, label: '50%' },
  { value: 1, label: '全仓' },
];
const SPEEDS = [
  { value: 900, label: '1x' },
  { value: 450, label: '2x' },
  { value: 220, label: '4x' },
];

const GRADE_COLORS = {
  'S+': 'amber',
  S: 'violet',
  A: 'green',
  B: 'blue',
  C: 'orange',
  D: 'red',
};

// K 线双盲训练营：使用真实历史 K 线隐藏标的与日期，零风险用 10,000 虚拟 USDT 演练多空择时、仓位与杠杆控制。
const ArenaView = ({
  spotSymbols = [],
  futuresSymbols = [],
  onOpenSpot,
  onOpenFutures,
  t,
}) => {
  const actualTheme = useActualTheme();
  const [interval, setIntervalValue] = useState('15m');
  const [blindMode, setBlindMode] = useState(true);
  const [leverage, setLeverage] = useState(5);
  const [fraction, setFraction] = useState(0.5);
  const [autoplay, setAutoplay] = useState(false);
  const [speedMs, setSpeedMs] = useState(450);
  const [loading, setLoading] = useState(false);
  const [meta, setMeta] = useState(null);
  const [arena, setArena] = useState(null);
  const [career, setCareer] = useState(readArenaCareerStats);
  const recordedRef = useRef(false);

  const chartHostRef = useRef(null);
  const chartRef = useRef(null);
  const candleSeriesRef = useRef(null);
  const volumeSeriesRef = useRef(null);
  const markersRef = useRef(null);
  const priceLinesRef = useRef([]);

  const candidatePool = useMemo(() => {
    const items = [];
    const seen = new Set();
    for (const s of futuresSymbols) {
      if (!seen.has(s.ticker)) {
        seen.add(s.ticker);
        items.push({
          ...s,
          market: 'futures',
          url: '/api/trade/futures/klines',
        });
      }
    }
    for (const s of spotSymbols) {
      if (!seen.has(s.ticker)) {
        seen.add(s.ticker);
        items.push({ ...s, market: 'spot', url: '/api/trade/klines' });
      }
    }
    return items;
  }, [spotSymbols, futuresSymbols]);

  const startNewRound = useCallback(async () => {
    setAutoplay(false);
    setLoading(true);
    recordedRef.current = false;

    const needed = ARENA_WARMUP_BARS + ARENA_PLAY_BARS;
    let chosen = null;
    let candles = null;
    let synthetic = false;

    if (candidatePool.length > 0) {
      const shuffled = [...candidatePool].sort(() => Math.random() - 0.5);
      for (const candidate of shuffled.slice(0, 4)) {
        const res = await tradeGet(candidate.url, t, {
          symbol: candidate.symbol,
          interval,
          limit: 240,
        });
        const rows = Array.isArray(res.data) ? res.data : [];
        if (rows.length >= needed + 5) {
          const parsed = rows.map((row) => ({
            time: Math.floor(Number(row[0]) / 1000),
            open: Number(row[1]),
            high: Number(row[2]),
            low: Number(row[3]),
            close: Number(row[4]),
            volume: Number(row[5]),
          }));
          const maxStart = parsed.length - needed;
          const startIdx = Math.floor(Math.random() * (maxStart + 1));
          candles = parsed.slice(startIdx, startIdx + needed);
          chosen = candidate;
          break;
        }
      }
    }

    if (!candles) {
      synthetic = true;
      const tickers = Object.keys(TRADE_SYMBOL_META);
      const pick =
        tickers[Math.floor(Math.random() * tickers.length)] || 'NVDA';
      chosen = {
        ticker: pick,
        symbol: `${pick}USDT`,
        market: 'futures',
      };
      const basePrice = 30 + Math.random() * 220;
      candles = generateSyntheticCandles(
        needed,
        Math.floor(Date.now() % 100000),
        basePrice,
      );
    }

    setMeta({
      ...chosen,
      interval,
      synthetic,
      startTime: candles[ARENA_WARMUP_BARS - 1]?.time,
      endTime: candles[candles.length - 1]?.time,
    });
    setArena(
      createArenaState(candles, {
        initialCash: ARENA_INITIAL_CASH,
        warmup: ARENA_WARMUP_BARS,
      }),
    );
    setLoading(false);
  }, [candidatePool, interval, t]);

  // 首次进入自动开启第一局
  useEffect(() => {
    if (!arena && !loading) {
      startNewRound();
    }
  }, [arena, loading, startNewRound]);

  // 自动播放推进 K 线
  useEffect(() => {
    if (!autoplay || !arena || arena.finished) return undefined;
    const timer = setInterval(() => {
      setArena((prev) => (prev ? stepArenaCandle(prev) : prev));
    }, speedMs);
    return () => clearInterval(timer);
  }, [autoplay, speedMs, arena]);

  // 结算时自动停播并记录战绩
  useEffect(() => {
    if (!arena?.finished || recordedRef.current || !meta) return;
    recordedRef.current = true;
    setAutoplay(false);
    const result = gradeArenaResult(arena);
    const updated = recordArenaRound({
      at: Date.now(),
      ticker: meta.ticker,
      interval: meta.interval,
      grade: result.grade,
      returnPct: Number(result.returnPct.toFixed(2)),
      alphaPct: Number(result.alphaPct.toFixed(2)),
    });
    setCareer(updated);
  }, [arena, meta]);

  // 快捷键支持：B 做多、S 做空、C 平仓、Space 下一根
  useEffect(() => {
    const onKeyDown = (event) => {
      if (
        !arena ||
        arena.finished ||
        ['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target?.tagName)
      ) {
        return;
      }
      const key = event.key.toLowerCase();
      if (key === ' ') {
        event.preventDefault();
        setArena((prev) => (prev ? stepArenaCandle(prev) : prev));
      } else if (key === 'b') {
        event.preventDefault();
        setArena((prev) =>
          prev
            ? openArenaPosition(prev, { side: 'long', leverage, fraction })
            : prev,
        );
      } else if (key === 's') {
        event.preventDefault();
        setArena((prev) =>
          prev
            ? openArenaPosition(prev, { side: 'short', leverage, fraction })
            : prev,
        );
      } else if (key === 'c') {
        event.preventDefault();
        setArena((prev) => (prev ? closeArenaPosition(prev) : prev));
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [arena, leverage, fraction]);

  // 初始化轻量级 K 线画布
  useEffect(() => {
    const host = chartHostRef.current;
    if (!host) return undefined;
    const up = cssColor('--semi-color-success', '#10b981');
    const down = cssColor('--semi-color-danger', '#ef4444');
    const textColor = cssColor('--semi-color-text-2', '#94a3b8');
    const borderColor = cssColor(
      '--semi-color-border',
      'rgba(148,163,184,0.2)',
    );
    const bgColor = cssColor('--semi-color-bg-1', '#ffffff');

    const chart = createChart(host, {
      width: host.clientWidth,
      height: host.clientHeight || 380,
      layout: {
        background: { color: bgColor },
        textColor,
        attributionLogo: true,
      },
      grid: {
        vertLines: { color: withAlpha(borderColor, 0.35) },
        horzLines: { color: withAlpha(borderColor, 0.35) },
      },
      crosshair: { mode: CrosshairMode.Normal },
      rightPriceScale: { borderColor },
      timeScale: {
        borderColor,
        timeVisible: !blindMode,
        secondsVisible: false,
      },
    });

    const candleSeries = chart.addSeries(CandlestickSeries, {
      upColor: up,
      downColor: down,
      borderUpColor: up,
      borderDownColor: down,
      wickUpColor: up,
      wickDownColor: down,
    });
    const volumeSeries = chart.addSeries(HistogramSeries, {
      priceFormat: { type: 'volume' },
      priceScaleId: '',
    });
    volumeSeries.priceScale().applyOptions({
      scaleMargins: { top: 0.82, bottom: 0 },
    });

    chartRef.current = chart;
    candleSeriesRef.current = candleSeries;
    volumeSeriesRef.current = volumeSeries;
    markersRef.current = createSeriesMarkers(candleSeries, []);

    const observer = new ResizeObserver(() => {
      if (host.clientWidth > 0) {
        chart.applyOptions({
          width: host.clientWidth,
          height: host.clientHeight || 380,
        });
      }
    });
    observer.observe(host);

    return () => {
      observer.disconnect();
      chart.remove();
      chartRef.current = null;
      candleSeriesRef.current = null;
      volumeSeriesRef.current = null;
      markersRef.current = null;
      priceLinesRef.current = [];
    };
  }, [actualTheme, blindMode]);

  // 同步当前可见 K 线、持仓价位线与买卖打点
  useEffect(() => {
    if (!arena || !candleSeriesRef.current || !volumeSeriesRef.current) return;
    const visible = arena.candles.slice(0, arena.cursor + 1);
    const up = cssColor('--semi-color-success', '#10b981');
    const down = cssColor('--semi-color-danger', '#ef4444');

    candleSeriesRef.current.setData(
      visible.map((bar) => ({
        time: bar.time,
        open: bar.open,
        high: bar.high,
        low: bar.low,
        close: bar.close,
      })),
    );
    volumeSeriesRef.current.setData(
      visible.map((bar) => ({
        time: bar.time,
        value: bar.volume,
        color: withAlpha(bar.close >= bar.open ? up : down, 0.28),
      })),
    );
    markersRef.current?.setMarkers(arena.markers);

    for (const line of priceLinesRef.current) {
      candleSeriesRef.current.removePriceLine(line);
    }
    priceLinesRef.current = [];

    if (arena.position) {
      const m = calcArenaMetrics(arena);
      priceLinesRef.current.push(
        candleSeriesRef.current.createPriceLine({
          price: arena.position.entryPrice,
          color: arena.position.side === 'long' ? up : down,
          lineWidth: 1,
          lineStyle: LineStyle.Dashed,
          axisLabelVisible: true,
          title:
            arena.position.side === 'long'
              ? `${t('多仓')} ${arena.position.leverage}x`
              : `${t('空仓')} ${arena.position.leverage}x`,
        }),
      );
      if (m.liqPrice > 0) {
        priceLinesRef.current.push(
          candleSeriesRef.current.createPriceLine({
            price: m.liqPrice,
            color: '#f59e0b',
            lineWidth: 1,
            lineStyle: LineStyle.Dotted,
            axisLabelVisible: true,
            title: t('强平价'),
          }),
        );
      }
    }
    chartRef.current?.timeScale().scrollToRealTime();
  }, [arena, t]);

  const metrics = arena ? calcArenaMetrics(arena) : null;
  const result = arena?.finished ? gradeArenaResult(arena) : null;
  const revealed = !blindMode || arena?.finished;
  const priceDigits = metrics?.price >= 1000 ? 2 : metrics?.price >= 1 ? 3 : 5;

  const winRate =
    career.rounds > 0 ? Math.round((career.wins / career.rounds) * 100) : 0;

  return (
    <div className='flex flex-col gap-4'>
      <div className='trade-card trade-hero-banner flex flex-wrap items-center justify-between gap-3'>
        <div className='flex flex-col gap-1'>
          <div className='flex flex-wrap items-center gap-2'>
            <Crosshair size={20} className='text-[var(--semi-color-primary)]' />
            <Title heading={5} className='!mb-0'>
              {t('K 线双盲训练营')}
            </Title>
            <Tag size='small' color='amber'>
              {t('零风险练手')}
            </Tag>
            {meta?.synthetic && (
              <Tag size='small' color='grey'>
                {t('离线演练行情')}
              </Tag>
            )}
          </div>
          <Text type='secondary' size='small'>
            {t(
              '隐藏真实品种名称与日期，用 10,000 虚拟 USDT 逐根推演 50 根真实历史 K 线，零资金风险锤炼盘感与风控（快捷键：B 做多 / S 做空 / C 平仓 / Space 下一根）。',
            )}
          </Text>
        </div>

        <div className='flex flex-wrap items-center gap-3'>
          <div className='flex items-center gap-3 trade-num text-xs'>
            <div className='trade-stat-chip'>
              <Text type='tertiary' size='small' className='!text-[11px]'>
                {t('已训练')}
              </Text>
              <Text strong>{t('{{count}} 局', { count: career.rounds })}</Text>
            </div>
            <div className='trade-stat-chip'>
              <Text type='tertiary' size='small' className='!text-[11px]'>
                {t('盈利胜率')}
              </Text>
              <Text strong>{winRate}%</Text>
            </div>
            <div className='trade-stat-chip'>
              <Text type='tertiary' size='small' className='!text-[11px]'>
                {t('最佳单局')}
              </Text>
              <Text strong className={trendClass(career.bestReturn)}>
                {career.rounds > 0
                  ? `${career.bestReturn > 0 ? '+' : ''}${career.bestReturn.toFixed(1)}%`
                  : '--'}
              </Text>
            </div>
          </div>
        </div>
      </div>

      {/* 顶部控制栏 */}
      <div className='trade-card !py-3 flex flex-wrap items-center justify-between gap-3'>
        <div className='flex flex-wrap items-center gap-3'>
          <RadioGroup
            type='button'
            size='small'
            value={interval}
            onChange={(e) => setIntervalValue(e.target.value)}
          >
            {INTERVAL_OPTIONS.map((opt) => (
              <Radio key={opt.value} value={opt.value}>
                {t(opt.label)}
              </Radio>
            ))}
          </RadioGroup>
          <label className='inline-flex items-center gap-1.5 text-xs text-[var(--semi-color-text-1)]'>
            {blindMode ? <EyeOff size={14} /> : <Eye size={14} />}
            <span>{t('双盲隐藏标的')}</span>
            <Switch size='small' checked={blindMode} onChange={setBlindMode} />
          </label>
        </div>

        <div className='flex flex-wrap items-center gap-2'>
          {meta && (
            <Tag size='large' color={revealed ? 'blue' : 'violet'}>
              {revealed
                ? `${formatSymbolLabel(meta.ticker, t)} · ${meta.interval}`
                : `${t('神秘标的 ???')} · ${meta.interval}`}
            </Tag>
          )}
          <Button
            size='small'
            theme='solid'
            icon={<RotateCcw size={14} />}
            loading={loading}
            onClick={startNewRound}
          >
            {t('换一局挑战')}
          </Button>
        </div>
      </div>

      {/* 核心主区：图表 + 操纵台 */}
      <div className='grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_320px]'>
        <div className='flex flex-col gap-4 min-w-0'>
          <div className='trade-card !p-0 overflow-hidden'>
            <div className='flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 border-b border-[var(--semi-color-border)]'>
              <div className='flex items-center gap-3 trade-num'>
                <Text strong className='text-lg'>
                  {metrics ? formatPrice(metrics.price, priceDigits) : '--'}
                </Text>
                {metrics && (
                  <Tag size='small' color='blue'>
                    {t('进度 {{done}} / {{total}} 根', {
                      done: metrics.elapsedBars,
                      total: metrics.totalPlayBars,
                    })}
                  </Tag>
                )}
              </div>
              <div className='flex items-center gap-1.5'>
                {SPEEDS.map((sp) => (
                  <Button
                    key={sp.value}
                    size='small'
                    theme={speedMs === sp.value ? 'solid' : 'light'}
                    onClick={() => setSpeedMs(sp.value)}
                  >
                    {sp.label}
                  </Button>
                ))}
                <Button
                  size='small'
                  theme={autoplay ? 'solid' : 'light'}
                  type={autoplay ? 'warning' : 'primary'}
                  icon={autoplay ? <Pause size={14} /> : <Play size={14} />}
                  disabled={!arena || arena.finished}
                  onClick={() => setAutoplay((v) => !v)}
                >
                  {autoplay ? t('暂停') : t('自动推演')}
                </Button>
              </div>
            </div>
            <div className='relative' style={{ height: 420 }}>
              {loading && (
                <div className='trade-chart-overlay'>
                  <Spin size='large' />
                </div>
              )}
              <div ref={chartHostRef} className='h-full w-full' />
            </div>
          </div>

          {/* 结算揭晓卡 */}
          {result && meta && (
            <div className='trade-card trade-arena-reveal flex flex-col gap-4'>
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <div className='flex items-center gap-3'>
                  <span className='trade-arena-grade'>
                    <Tag
                      size='large'
                      type='solid'
                      color={GRADE_COLORS[result.grade] || 'blue'}
                      className='!text-lg !px-3 !py-1 font-bold'
                    >
                      {result.grade}
                    </Tag>
                  </span>
                  <div className='flex flex-col'>
                    <div className='flex items-center gap-2'>
                      <SymbolAvatar item={meta.ticker} size={24} />
                      <Title heading={5} className='!mb-0'>
                        {t('真相揭晓：{{name}}', {
                          name: formatSymbolLabel(meta.ticker, t),
                        })}
                      </Title>
                    </div>
                    <Text type='tertiary' size='small'>
                      {meta.synthetic
                        ? t('本次为离线演练行情')
                        : `${timestamp2string(meta.startTime)} ~ ${timestamp2string(meta.endTime)}`}
                    </Text>
                  </div>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    theme='light'
                    icon={<ArrowUpRight size={14} />}
                    iconPosition='right'
                    onClick={() =>
                      meta.market === 'spot'
                        ? onOpenSpot?.(meta.symbol)
                        : onOpenFutures?.(meta.symbol)
                    }
                  >
                    {t('去实盘交易此品种')}
                  </Button>
                  <Button
                    theme='solid'
                    icon={<Sparkles size={14} />}
                    onClick={startNewRound}
                  >
                    {t('再来一局')}
                  </Button>
                </div>
              </div>

              <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
                <div className='trade-stat-chip !p-3'>
                  <Text type='tertiary' size='small'>
                    {t('最终净值')}
                  </Text>
                  <div className='trade-num text-base font-bold'>
                    {result.finalEquity.toFixed(2)} USDT
                  </div>
                </div>
                <div className='trade-stat-chip !p-3'>
                  <Text type='tertiary' size='small'>
                    {t('本局收益率')}
                  </Text>
                  <div
                    className={`trade-num text-base font-bold ${trendClass(result.returnPct)}`}
                  >
                    {result.returnPct > 0 ? '+' : ''}
                    {result.returnPct.toFixed(2)}%
                  </div>
                </div>
                <div className='trade-stat-chip !p-3'>
                  <Text type='tertiary' size='small'>
                    {t('同期持有不动')}
                  </Text>
                  <div
                    className={`trade-num text-base font-bold ${trendClass(result.benchmarkPct)}`}
                  >
                    {result.benchmarkPct > 0 ? '+' : ''}
                    {result.benchmarkPct.toFixed(2)}%
                  </div>
                </div>
                <div className='trade-stat-chip !p-3'>
                  <Text type='tertiary' size='small'>
                    {t('超额收益 (Alpha)')}
                  </Text>
                  <div
                    className={`trade-num text-base font-bold ${trendClass(result.alphaPct)}`}
                  >
                    {result.alphaPct > 0 ? '+' : ''}
                    {result.alphaPct.toFixed(2)}%
                  </div>
                </div>
              </div>

              <div className='flex flex-wrap gap-4 text-xs text-[var(--semi-color-text-2)] trade-num'>
                <span>
                  {t('成交笔数')}：<b>{result.tradeCount}</b>
                </span>
                <span>
                  {t('单局胜率')}：<b>{result.winRate.toFixed(0)}%</b>
                </span>
                <span>
                  {t('最大回撤')}：
                  <b className='trade-down'>{result.maxDrawdown.toFixed(2)}%</b>
                </span>
              </div>
            </div>
          )}
        </div>

        {/* 右侧交易操纵台 */}
        <div className='flex flex-col gap-4'>
          <div className='trade-card flex flex-col gap-3'>
            <div className='flex items-center justify-between'>
              <Text strong>{t('虚拟训练账户')}</Text>
              <Tag size='small' color='cyan'>
                {t('初始 10,000 USDT')}
              </Tag>
            </div>

            {metrics && arena && (
              <div className='grid grid-cols-2 gap-2.5 trade-num'>
                <div className='trade-stat-chip !p-2.5'>
                  <Text type='tertiary' size='small' className='!text-[11px]'>
                    {t('当前净值')}
                  </Text>
                  <div className='font-bold text-base'>
                    {metrics.equity.toFixed(2)}
                  </div>
                </div>
                <div className='trade-stat-chip !p-2.5'>
                  <Text type='tertiary' size='small' className='!text-[11px]'>
                    {t('总收益率')}
                  </Text>
                  <div
                    className={`font-bold text-base ${trendClass(metrics.totalReturnPct)}`}
                  >
                    {metrics.totalReturnPct > 0 ? '+' : ''}
                    {metrics.totalReturnPct.toFixed(2)}%
                  </div>
                </div>
                <div className='trade-stat-chip !p-2.5'>
                  <Text type='tertiary' size='small' className='!text-[11px]'>
                    {t('可用余额')}
                  </Text>
                  <div className='font-semibold text-sm'>
                    {arena.balance.toFixed(2)}
                  </div>
                </div>
                <div className='trade-stat-chip !p-2.5'>
                  <Text type='tertiary' size='small' className='!text-[11px]'>
                    {t('最大回撤')}
                  </Text>
                  <div className='font-semibold text-sm trade-down'>
                    {arena.maxDrawdown.toFixed(2)}%
                  </div>
                </div>
              </div>
            )}

            {arena?.position ? (
              <div
                className={`trade-position ${
                  arena.position.side === 'long'
                    ? 'trade-position-long'
                    : 'trade-position-short'
                }`}
              >
                <div className='flex items-center justify-between'>
                  <Tag
                    size='small'
                    type='solid'
                    color={arena.position.side === 'long' ? 'green' : 'red'}
                  >
                    {arena.position.side === 'long' ? t('多仓') : t('空仓')}{' '}
                    {arena.position.leverage}x
                  </Tag>
                  <span
                    className={`trade-num text-sm font-bold ${trendClass(metrics?.unrealizedPnl)}`}
                  >
                    {metrics.unrealizedPnl > 0 ? '+' : ''}
                    {metrics.unrealizedPnl.toFixed(2)} (
                    {metrics.roe > 0 ? '+' : ''}
                    {metrics.roe.toFixed(1)}%)
                  </span>
                </div>
                <div className='flex justify-between text-xs trade-num text-[var(--semi-color-text-2)]'>
                  <span>
                    {t('均价')}{' '}
                    {formatPrice(arena.position.entryPrice, priceDigits)}
                  </span>
                  <span className='trade-warning'>
                    {t('强平')} {formatPrice(metrics.liqPrice, priceDigits)}
                  </span>
                </div>
              </div>
            ) : (
              <Banner
                type='info'
                closeIcon={null}
                description={t(
                  '当前空仓：可点击做多/做空开仓，或推进下一根 K 线观察形态。',
                )}
              />
            )}

            <div className='flex flex-col gap-1.5'>
              <Text type='tertiary' size='small'>
                {t('杠杆倍数')}
              </Text>
              <div className='grid grid-cols-5 gap-1'>
                {LEVERAGES.map((lev) => (
                  <Button
                    key={lev}
                    size='small'
                    theme={leverage === lev ? 'solid' : 'light'}
                    onClick={() => setLeverage(lev)}
                  >
                    {lev}x
                  </Button>
                ))}
              </div>
            </div>

            <div className='flex flex-col gap-1.5'>
              <Text type='tertiary' size='small'>
                {t('开仓仓位比例')}
              </Text>
              <div className='grid grid-cols-3 gap-1.5'>
                {FRACTIONS.map((item) => (
                  <Button
                    key={item.value}
                    size='small'
                    theme={fraction === item.value ? 'solid' : 'light'}
                    onClick={() => setFraction(item.value)}
                  >
                    {t(item.label)}
                  </Button>
                ))}
              </div>
            </div>

            <div className='grid grid-cols-2 gap-2 pt-1'>
              <Button
                theme='solid'
                icon={<TrendingUp size={15} />}
                disabled={!arena || arena.finished}
                style={{ background: 'var(--semi-color-success)' }}
                onClick={() =>
                  setArena((prev) =>
                    prev
                      ? openArenaPosition(prev, {
                          side: 'long',
                          leverage,
                          fraction,
                        })
                      : prev,
                  )
                }
              >
                {arena?.position?.side === 'long'
                  ? t('加多 (B)')
                  : arena?.position?.side === 'short'
                    ? t('反手多 (B)')
                    : t('做多 (B)')}
              </Button>
              <Button
                theme='solid'
                type='danger'
                icon={<TrendingDown size={15} />}
                disabled={!arena || arena.finished}
                onClick={() =>
                  setArena((prev) =>
                    prev
                      ? openArenaPosition(prev, {
                          side: 'short',
                          leverage,
                          fraction,
                        })
                      : prev,
                  )
                }
              >
                {arena?.position?.side === 'short'
                  ? t('加空 (S)')
                  : arena?.position?.side === 'long'
                    ? t('反手空 (S)')
                    : t('做空 (S)')}
              </Button>
            </div>

            <div className='grid grid-cols-2 gap-2'>
              <Button
                theme='light'
                type='warning'
                disabled={!arena || arena.finished || !arena.position}
                onClick={() =>
                  setArena((prev) => (prev ? closeArenaPosition(prev) : prev))
                }
              >
                {t('一键平仓 (C)')}
              </Button>
              <Button
                theme='solid'
                type='primary'
                icon={<FastForward size={14} />}
                disabled={!arena || arena.finished}
                onClick={() =>
                  setArena((prev) => (prev ? stepArenaCandle(prev) : prev))
                }
              >
                {t('下一根 (Space)')}
              </Button>
            </div>

            <Button
              size='small'
              theme='borderless'
              type='tertiary'
              icon={<Flag size={13} />}
              disabled={!arena || arena.finished}
              onClick={() =>
                setArena((prev) => (prev ? finishArenaEarly(prev) : prev))
              }
            >
              {t('提前结算并揭晓标的')}
            </Button>
          </div>

          {career.history.length > 0 && (
            <div className='trade-card flex flex-col gap-2'>
              <div className='flex items-center gap-1.5'>
                <Trophy size={15} style={{ color: '#f59e0b' }} />
                <Text strong size='small'>
                  {t('近期盲测战绩')}
                </Text>
              </div>
              <div className='flex flex-col gap-1.5'>
                {career.history.slice(0, 5).map((item, idx) => (
                  <div
                    key={`${item.at}-${idx}`}
                    className='flex items-center justify-between text-xs trade-num'
                  >
                    <div className='flex items-center gap-1.5 min-w-0'>
                      <Tag
                        size='small'
                        color={GRADE_COLORS[item.grade] || 'blue'}
                      >
                        {item.grade}
                      </Tag>
                      <Text size='small' className='truncate'>
                        {formatSymbolLabel(item.ticker, t)}
                      </Text>
                    </div>
                    <span
                      className={`font-semibold ${trendClass(item.returnPct)}`}
                    >
                      {item.returnPct > 0 ? '+' : ''}
                      {item.returnPct.toFixed(1)}%
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default ArenaView;
