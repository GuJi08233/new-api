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
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react';
import { Button, Radio, RadioGroup, Spin, Tooltip } from '@douyinfe/semi-ui';
import { Maximize2, Minimize2, X } from 'lucide-react';
import {
  CandlestickSeries,
  CrosshairMode,
  HistogramSeries,
  LineSeries,
  LineStyle,
  createChart,
  createSeriesMarkers,
} from 'lightweight-charts';
import { useTranslation } from 'react-i18next';
import { useActualTheme } from '../../context/Theme';
import { UserContext } from '../../context/User';
import { timestamp2string } from '../../helpers';
import {
  bollSeries,
  emaSeries,
  macdSeries,
  maSeries,
  rsiSeries,
} from './indicators';
import { formatPrice, formatQty, tradeGet } from './api';
import { DrawOverlay, DrawToolPopover, DrawToolRail } from './DrawingTools';
import { useDrawings } from './useDrawings';

// K 线图：蜡烛与成交量，可叠加 MA / EMA / 布林带，可开 MACD、RSI 副图；往左拖到头时加载更早的历史；
// 用 1 分钟 K 线推送实时更新最后一根；画出持仓的价格线(现货的持仓均价，合约的开仓均价与强平价)与自己的买卖点，
// 点一根有买卖点的 K 线列出它上面的每一次成交。
// klineUrl 是拉历史 K 线的接口，现货与合约各一个。图表库是 TradingView 的 lightweight-charts，按它的许可证要求保留
// 右下角的 TradingView 标志。

const INTERVALS = ['1m', '5m', '15m', '1h', '4h', '1d'];
const INTERVAL_MS = {
  '1m': 60_000,
  '5m': 300_000,
  '15m': 900_000,
  '1h': 3_600_000,
  '4h': 14_400_000,
  '1d': 86_400_000,
};
const MA_PERIODS = [7, 25, 99];
const RSI_PERIODS = [6, 12, 24];
const PAGE_SIZE = 500;
const MAX_BARS = 3000;
const LINE_COLORS = ['#f59e0b', '#3b82f6', '#a855f7'];

function readSetting(key, fallback) {
  try {
    const value = JSON.parse(localStorage.getItem(key));
    return value ?? fallback;
  } catch {
    return fallback;
  }
}

function writeSetting(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // 存不了就只在本次会话里生效。
  }
}

// 图表库按 UTC 显示时间，平移本地时区的偏移量后横轴就是本地时间。
function chartTime(ms) {
  return Math.floor(ms / 1000) - new Date(ms).getTimezoneOffset() * 60;
}

function toBar(row) {
  return {
    openMs: row[0],
    time: chartTime(row[0]),
    open: Number(row[1]),
    high: Number(row[2]),
    low: Number(row[3]),
    close: Number(row[4]),
    volume: Number(row[5]),
  };
}

function cssColor(name, fallback) {
  const value = getComputedStyle(document.body).getPropertyValue(name).trim();
  return value || fallback;
}

function chartColors() {
  return {
    text: cssColor('--semi-color-text-2', '#888'),
    border: cssColor('--semi-color-border', '#e5e7eb'),
    up: cssColor('--semi-color-success', '#16a34a'),
    down: cssColor('--semi-color-danger', '#dc2626'),
    mute: cssColor('--semi-color-text-3', '#aaa'),
    primary: cssColor('--semi-color-primary', '#3b82f6'),
  };
}

// withAlpha 把颜色的透明度乘上 alpha。Semi 的颜色变量本身可能带透明度(边框色是 0.08)，要在它的基础上调，不能直接覆盖。
function withAlpha(color, alpha) {
  const match = color.match(/rgba?\(([^)]+)\)/);
  if (match) {
    const [r, g, b, a = '1'] = match[1].split(',').map((part) => part.trim());
    return `rgba(${r},${g},${b},${Number(a) * alpha})`;
  }
  if (color.startsWith('#') && color.length === 7) {
    const value = parseInt(color.slice(1), 16);
    return `rgba(${(value >> 16) & 255},${(value >> 8) & 255},${value & 255},${alpha})`;
  }
  return color;
}

// fillSide 是一次成交在盘口上的方向：现货成交的 type 就是买卖方向；合约成交带着仓位方向，开多、平空是买入，开空、平多(含强平)
// 是卖出。
function fillSide(fill) {
  if (!fill.side) return fill.type;
  return (fill.side === 'long') === (fill.type === 'futures_open')
    ? 'buy'
    : 'sell';
}

function lineData(bars, values) {
  const out = [];
  values.forEach((value, i) => {
    if (value !== null) out.push({ time: bars[i].time, value });
  });
  return out;
}

const CandleChart = ({
  symbol,
  klineUrl = '/api/trade/klines',
  priceDigits,
  kline,
  priceLines,
  fills,
  t,
}) => {
  const actualTheme = useActualTheme();
  const [userState] = useContext(UserContext);
  const userId = userState.user?.id;
  const drawing = useDrawings();
  const { attach: attachDrawings, shouldHandleChartClick } = drawing;
  const { i18n } = useTranslation();
  const locale = i18n.language;
  const wrapRef = useRef(null);
  const hostRef = useRef(null);
  const chartRef = useRef(null);
  const seriesRef = useRef(null);
  const barsRef = useRef([]);
  const barIndexRef = useRef(new Map());
  const liveRef = useRef(null);
  const pagingRef = useRef({ loading: false, done: false });
  const priceLinesRef = useRef([]);
  const markersRef = useRef(null);
  // 点 K 线时要用最新的成交与周期，建图时订阅的回调从这里读。
  const fillsRef = useRef(fills);
  fillsRef.current = fills;
  const periodRef = useRef(null);
  const [period, setPeriod] = useState(() =>
    readSetting('trade-chart-interval', '15m'),
  );
  const [, setTick] = useState(0);
  const [overlays, setOverlays] = useState(() =>
    readSetting('trade-chart-overlays', { ma: true, ema: false, boll: false }),
  );
  const [panes, setPanes] = useState(() =>
    readSetting('trade-chart-panes', { macd: false, rsi: false }),
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [legend, setLegend] = useState(null);
  const [fullscreen, setFullscreen] = useState(false);
  const [version, setVersion] = useState(0);
  // picked 是点开的那根 K 线上的成交列表与弹出的位置。
  const [picked, setPicked] = useState(null);
  periodRef.current = period;

  // 用全部 K 线重算指标并灌进各条线；live 为真时只更新最后一个点(实时推送每秒一次，整段重灌会闪)。
  const paint = useCallback((live) => {
    const series = seriesRef.current;
    const bars = barsRef.current;
    if (!series || !bars.length) return;
    const colors = chartColors();
    const volumePoint = (bar) => ({
      time: bar.time,
      value: bar.volume,
      color: withAlpha(bar.close >= bar.open ? colors.up : colors.down, 0.45),
    });
    const closes = bars.map((bar) => bar.close);
    const lines = [];
    MA_PERIODS.forEach((period, i) => {
      lines.push([series.ma[i], maSeries(closes, period)]);
      lines.push([series.ema[i], emaSeries(closes, period)]);
    });
    const boll = bollSeries(closes);
    lines.push(
      [series.boll[0], boll.upper],
      [series.boll[1], boll.mid],
      [series.boll[2], boll.lower],
    );
    let macd = null;
    if (series.macd) {
      macd = macdSeries(closes);
      lines.push([series.macd.dif, macd.dif], [series.macd.dea, macd.dea]);
    }
    if (series.rsi) {
      RSI_PERIODS.forEach((period, i) =>
        lines.push([series.rsi[i], rsiSeries(closes, period)]),
      );
    }
    const histPoint = (i) => ({
      time: bars[i].time,
      value: macd.hist[i],
      color: withAlpha(macd.hist[i] >= 0 ? colors.up : colors.down, 0.7),
    });
    if (live) {
      const last = bars.length - 1;
      series.candle.update(bars[last]);
      series.volume.update(volumePoint(bars[last]));
      lines.forEach(([line, values]) => {
        if (values[last] !== null)
          line.update({ time: bars[last].time, value: values[last] });
      });
      if (macd && macd.hist[last] !== null)
        series.macd.hist.update(histPoint(last));
      return;
    }
    barIndexRef.current = new Map(
      bars.map((bar, index) => [bar.openMs / 1000, index]),
    );
    series.candle.setData(bars);
    series.volume.setData(bars.map(volumePoint));
    lines.forEach(([line, values]) => line.setData(lineData(bars, values)));
    if (macd) {
      const hist = [];
      macd.hist.forEach((value, i) => {
        if (value !== null) hist.push(histPoint(i));
      });
      series.macd.hist.setData(hist);
    }
  }, []);

  // 建图：交易对、周期或副图变了就重建并重新拉历史。
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return undefined;
    let disposed = false;
    const colors = chartColors();
    const chart = createChart(host, {
      autoSize: true,
      // 横轴日期与价格按站点当前的语言显示，不跟浏览器的语言。
      localization: { locale },
      layout: {
        background: { color: 'transparent' },
        textColor: colors.text,
        fontSize: 11,
        panes: { separatorColor: colors.border, enableResize: true },
      },
      grid: {
        vertLines: { color: colors.border },
        horzLines: { color: colors.border },
      },
      crosshair: { mode: CrosshairMode.Normal },
      rightPriceScale: {
        borderColor: colors.border,
        scaleMargins: { top: 0.08, bottom: 0.26 },
      },
      timeScale: {
        borderColor: colors.border,
        timeVisible: true,
        secondsVisible: false,
        rightOffset: 5,
      },
    });
    chartRef.current = chart;
    const precision = Math.min(Math.max(priceDigits ?? 2, 0), 8);
    const priceFormat = {
      type: 'price',
      precision,
      minMove: 1 / 10 ** precision,
    };
    const thin = {
      lineWidth: 1,
      priceLineVisible: false,
      lastValueVisible: false,
      crosshairMarkerVisible: false,
    };
    const candle = chart.addSeries(CandlestickSeries, {
      upColor: colors.up,
      downColor: colors.down,
      borderUpColor: colors.up,
      borderDownColor: colors.down,
      wickUpColor: colors.up,
      wickDownColor: colors.down,
      priceFormat,
    });
    const volume = chart.addSeries(HistogramSeries, {
      priceScaleId: '',
      priceFormat: { type: 'volume' },
      lastValueVisible: false,
      priceLineVisible: false,
    });
    volume.priceScale().applyOptions({ scaleMargins: { top: 0.8, bottom: 0 } });
    const series = {
      candle,
      volume,
      ma: MA_PERIODS.map((_, i) =>
        chart.addSeries(LineSeries, {
          ...thin,
          color: LINE_COLORS[i],
          visible: overlays.ma,
          priceFormat,
        }),
      ),
      ema: MA_PERIODS.map((_, i) =>
        chart.addSeries(LineSeries, {
          ...thin,
          color: LINE_COLORS[i],
          lineStyle: LineStyle.Dashed,
          visible: overlays.ema,
          priceFormat,
        }),
      ),
      boll: [0, 1, 2].map((i) =>
        chart.addSeries(LineSeries, {
          ...thin,
          color: colors.mute,
          lineStyle: i === 1 ? LineStyle.Solid : LineStyle.Dashed,
          visible: overlays.boll,
          priceFormat,
        }),
      ),
      macd: null,
      rsi: null,
    };
    let pane = 1;
    if (panes.macd) {
      const index = pane++;
      series.macd = {
        hist: chart.addSeries(
          HistogramSeries,
          { priceLineVisible: false, lastValueVisible: false },
          index,
        ),
        dif: chart.addSeries(
          LineSeries,
          { ...thin, color: LINE_COLORS[0] },
          index,
        ),
        dea: chart.addSeries(
          LineSeries,
          { ...thin, color: LINE_COLORS[1] },
          index,
        ),
      };
    }
    if (panes.rsi) {
      const index = pane++;
      series.rsi = RSI_PERIODS.map((_, i) =>
        chart.addSeries(
          LineSeries,
          {
            ...thin,
            color: LINE_COLORS[i],
            autoscaleInfoProvider: () => ({
              priceRange: { minValue: 0, maxValue: 100 },
            }),
          },
          index,
        ),
      );
      [70, 30].forEach((price) =>
        series.rsi[0].createPriceLine({
          price,
          color: withAlpha(colors.mute, 0.6),
          lineWidth: 1,
          lineStyle: LineStyle.Dashed,
          axisLabelVisible: false,
        }),
      );
    }
    if (pane > 1) {
      const chartPanes = chart.panes();
      chartPanes[0].setStretchFactor(3);
      for (let i = 1; i < chartPanes.length; i++)
        chartPanes[i].setStretchFactor(1);
    }
    seriesRef.current = series;
    markersRef.current = createSeriesMarkers(candle, []);
    priceLinesRef.current = [];
    barsRef.current = [];
    barIndexRef.current = new Map();
    liveRef.current = null;
    pagingRef.current = { loading: false, done: false };
    setPicked(null);
    const detachDrawings = attachDrawings({
      chart,
      series: candle,
      host,
      scope: wrapRef.current,
      market: klineUrl.includes('/futures/') ? 'futures' : 'spot',
      symbol,
      userId,
      ctx: {
        series: candle,
        timeScale: chart.timeScale(),
        bars: () => barsRef.current,
        idx: () => barIndexRef.current,
        bucketSec: INTERVAL_MS[period] / 1000,
      },
      decimals: precision,
      fmtTime: (seconds) => timestamp2string(seconds).slice(0, 16),
      t,
      onInteract: () => setPicked(null),
    });

    // 点一根有买卖点的 K 线，在点的位置列出这根 K 线上的成交；点别处收起。
    chart.subscribeClick((param) => {
      if (!shouldHandleChartClick()) return;
      const bar = barsRef.current.find((entry) => entry.time === param.time);
      if (!bar || !param.point) {
        setPicked(null);
        return;
      }
      const end = bar.openMs + INTERVAL_MS[periodRef.current];
      const list = (fillsRef.current || [])
        .filter((fill) => {
          const ms = fill.created_at * 1000;
          return ms >= bar.openMs && ms < end;
        })
        .sort((a, b) => a.created_at - b.created_at);
      setPicked(
        list.length
          ? {
              openMs: bar.openMs,
              fills: list,
              x: param.point.x,
              y: param.point.y,
            }
          : null,
      );
    });

    chart.subscribeCrosshairMove((param) => {
      const bars = barsRef.current;
      if (!param.time || !bars.length) {
        setLegend(null);
        return;
      }
      const index = bars.findIndex((bar) => bar.time === param.time);
      setLegend(index >= 0 ? index : null);
    });

    // 往左拖到离最早一根不到 50 根时，接着往前加载一页。
    chart.timeScale().subscribeVisibleLogicalRangeChange((range) => {
      const paging = pagingRef.current;
      const bars = barsRef.current;
      if (
        !range ||
        range.from > 50 ||
        paging.loading ||
        paging.done ||
        !bars.length ||
        bars.length >= MAX_BARS
      )
        return;
      paging.loading = true;
      tradeGet(klineUrl, t, {
        symbol,
        interval: period,
        limit: PAGE_SIZE,
        end_time: bars[0].openMs - 1,
      }).then((res) => {
        paging.loading = false;
        if (disposed || !res.data) return;
        const older = res.data
          .map(toBar)
          .filter((bar) => bar.openMs < barsRef.current[0].openMs);
        if (!older.length) {
          paging.done = true;
          return;
        }
        const visible = chart.timeScale().getVisibleLogicalRange();
        barsRef.current = [...older, ...barsRef.current];
        paint(false);
        if (visible) {
          chart.timeScale().setVisibleLogicalRange({
            from: visible.from + older.length,
            to: visible.to + older.length,
          });
        }
        if (older.length < PAGE_SIZE - 1) paging.done = true;
      });
    });

    setLoading(true);
    setError('');
    tradeGet(klineUrl, t, {
      symbol,
      interval: period,
      limit: PAGE_SIZE,
    }).then((res) => {
      if (disposed) return;
      setLoading(false);
      if (res.error) {
        setError(res.error);
        return;
      }
      barsRef.current = res.data.map(toBar);
      if (barsRef.current.length < PAGE_SIZE) pagingRef.current.done = true;
      paint(false);
      chart.timeScale().setVisibleLogicalRange({
        from: Math.max(barsRef.current.length - 120, 0),
        to: barsRef.current.length + 4,
      });
      setVersion((value) => value + 1);
    });

    return () => {
      disposed = true;
      detachDrawings();
      seriesRef.current = null;
      chartRef.current = null;
      chart.remove();
    };
    // 叠加线的开关只切显示，不重建图；主题变化在下面单独处理。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    symbol,
    klineUrl,
    period,
    panes.macd,
    panes.rsi,
    priceDigits,
    userId,
    locale,
    paint,
    attachDrawings,
    shouldHandleChartClick,
  ]);

  // 叠加线开关。
  useEffect(() => {
    const series = seriesRef.current;
    if (!series) return;
    series.ma.forEach((line) => line.applyOptions({ visible: overlays.ma }));
    series.ema.forEach((line) => line.applyOptions({ visible: overlays.ema }));
    series.boll.forEach((line) =>
      line.applyOptions({ visible: overlays.boll }),
    );
  }, [overlays, version]);

  // 换主题时重读颜色。
  useEffect(() => {
    const chart = chartRef.current;
    const series = seriesRef.current;
    if (!chart || !series) return;
    const colors = chartColors();
    chart.applyOptions({
      layout: {
        textColor: colors.text,
        panes: { separatorColor: colors.border },
      },
      grid: {
        vertLines: { color: colors.border },
        horzLines: { color: colors.border },
      },
      rightPriceScale: { borderColor: colors.border },
      timeScale: { borderColor: colors.border },
    });
    series.candle.applyOptions({
      upColor: colors.up,
      downColor: colors.down,
      borderUpColor: colors.up,
      borderDownColor: colors.down,
      wickUpColor: colors.up,
      wickDownColor: colors.down,
    });
    paint(false);
  }, [actualTheme, paint]);

  // 1 分钟 K 线推送并进当前周期的最后一根。成交量按分钟累加：同一分钟里推送的是累计值，换分钟时把上一分钟的最终值记进底数。
  useEffect(() => {
    const bars = barsRef.current;
    if (!kline || kline.s !== symbol || !bars.length || !seriesRef.current)
      return;
    const size = INTERVAL_MS[period];
    const bucket = Math.floor(kline.t / size) * size;
    const last = bars[bars.length - 1];
    const open = Number(kline.o);
    const high = Number(kline.h);
    const low = Number(kline.l);
    const close = Number(kline.c);
    const minuteVolume = Number(kline.v);
    if (bucket < last.openMs) return;
    let live = liveRef.current;
    if (bucket > last.openMs) {
      bars.push({
        openMs: bucket,
        time: chartTime(bucket),
        open,
        high,
        low,
        close,
        volume: minuteVolume,
      });
      if (bars.length > MAX_BARS) bars.shift();
      liveRef.current = { bucket, minute: kline.t, minuteVolume, base: 0 };
      paint(false);
      setTick((value) => value + 1);
      return;
    }
    if (!live || live.bucket !== bucket) {
      // 历史里的最后一根已经包含了这一分钟的一部分成交量，按此刻的累计值扣掉，之后再逐分钟加回。
      live = {
        bucket,
        minute: kline.t,
        minuteVolume,
        base: Math.max(last.volume - minuteVolume, 0),
      };
    } else if (kline.t > live.minute) {
      live = {
        bucket,
        minute: kline.t,
        minuteVolume,
        base: live.base + live.minuteVolume,
      };
    } else if (kline.t === live.minute) {
      live = { ...live, minuteVolume };
    }
    liveRef.current = live;
    last.high = Math.max(last.high, high);
    last.low = Math.min(last.low, low);
    last.close = close;
    last.volume = live.base + live.minuteVolume;
    paint(true);
    setTick((value) => value + 1);
  }, [kline, symbol, period, paint]);

  // 持仓的价格线，priceLines 是 [{ price, title, color }]：color 为 up 的线(止盈)用涨色，down 的线(强平价、止损)用跌色，
  // 其余用主色。
  useEffect(() => {
    const series = seriesRef.current;
    if (!series) return;
    priceLinesRef.current.forEach((line) =>
      series.candle.removePriceLine(line),
    );
    priceLinesRef.current = [];
    const colors = chartColors();
    (priceLines || []).forEach((line) => {
      const price = Number(line.price);
      if (!(price > 0)) return;
      priceLinesRef.current.push(
        series.candle.createPriceLine({
          price,
          color:
            { up: colors.up, down: colors.down }[line.color] || colors.primary,
          lineWidth: 1,
          lineStyle: LineStyle.Dashed,
          axisLabelVisible: true,
          title: line.title,
        }),
      );
    });
  }, [priceLines, version]);

  // 自己的买卖点，同一根 K 线上同方向的成交合成一个标记。
  useEffect(() => {
    const markers = markersRef.current;
    const bars = barsRef.current;
    if (!markers) return;
    if (!fills?.length || !bars.length) {
      markers.setMarkers([]);
      return;
    }
    const size = INTERVAL_MS[period];
    const first = bars[0].openMs;
    const colors = chartColors();
    const grouped = new Map();
    fills.forEach((fill) => {
      const ms = fill.created_at * 1000;
      if (ms < first) return;
      const bucket = Math.floor(ms / size) * size;
      const side = fillSide(fill);
      const key = `${bucket}|${side}`;
      const entry = grouped.get(key) || { bucket, side, qty: 0 };
      entry.qty += Number(fill.qty);
      grouped.set(key, entry);
    });
    const list = [...grouped.values()]
      .sort((a, b) => a.bucket - b.bucket)
      .map((entry) => ({
        time: chartTime(entry.bucket),
        position: entry.side === 'buy' ? 'belowBar' : 'aboveBar',
        color: entry.side === 'buy' ? colors.up : colors.down,
        shape: entry.side === 'buy' ? 'arrowUp' : 'arrowDown',
        text: `${entry.side === 'buy' ? 'B' : 'S'} ${formatQty(entry.qty, 6)}`,
      }));
    markers.setMarkers(list);
  }, [fills, period, version]);

  useEffect(() => {
    const onChange = () =>
      setFullscreen(document.fullscreenElement === wrapRef.current);
    document.addEventListener('fullscreenchange', onChange);
    return () => document.removeEventListener('fullscreenchange', onChange);
  }, []);

  const toggleFullscreen = () => {
    if (document.fullscreenElement) {
      document.exitFullscreen?.();
    } else {
      wrapRef.current?.requestFullscreen?.();
    }
  };

  const changeInterval = (value) => {
    setPicked(null);
    setPeriod(value);
    writeSetting('trade-chart-interval', value);
  };
  const toggleOverlay = (key) => {
    const next = { ...overlays, [key]: !overlays[key] };
    setOverlays(next);
    writeSetting('trade-chart-overlays', next);
  };
  const togglePane = (key) => {
    const next = { ...panes, [key]: !panes[key] };
    setPanes(next);
    writeSetting('trade-chart-panes', next);
  };

  const bars = barsRef.current;
  const shown =
    legend !== null && bars[legend] ? bars[legend] : bars[bars.length - 1];
  const previous = shown ? bars[bars.indexOf(shown) - 1] : null;
  const change =
    shown && previous
      ? ((shown.close - previous.close) / previous.close) * 100
      : null;

  return (
    <div
      ref={wrapRef}
      tabIndex={0}
      className={`trade-chart ${fullscreen ? 'trade-chart-fullscreen' : ''}`}
    >
      <div className='trade-chart-toolbar'>
        <RadioGroup
          type='button'
          size='small'
          value={period}
          onChange={(e) => changeInterval(e.target.value)}
        >
          {INTERVALS.map((value) => (
            <Radio key={value} value={value}>
              {value}
            </Radio>
          ))}
        </RadioGroup>
        <div className='trade-chart-switches'>
          <div className='trade-draw-mobile'>
            <DrawToolPopover d={drawing} t={t} />
          </div>
          {[
            ['ma', 'MA', overlays.ma, toggleOverlay],
            ['ema', 'EMA', overlays.ema, toggleOverlay],
            ['boll', 'BOLL', overlays.boll, toggleOverlay],
            ['macd', 'MACD', panes.macd, togglePane],
            ['rsi', 'RSI', panes.rsi, togglePane],
          ].map(([key, label, on, toggle]) => (
            <Button
              key={key}
              size='small'
              theme={on ? 'solid' : 'borderless'}
              type={on ? 'primary' : 'tertiary'}
              onClick={() => toggle(key)}
            >
              {label}
            </Button>
          ))}
          <Tooltip content={fullscreen ? t('退出全屏') : t('全屏')}>
            <Button
              size='small'
              theme='borderless'
              type='tertiary'
              icon={
                fullscreen ? <Minimize2 size={14} /> : <Maximize2 size={14} />
              }
              onClick={toggleFullscreen}
              aria-label={fullscreen ? t('退出全屏') : t('全屏')}
            />
          </Tooltip>
        </div>
      </div>
      <div className='trade-chart-workspace'>
        <DrawToolRail d={drawing} t={t} />
        <div className='trade-chart-body'>
          {shown && (
            <div className='trade-chart-legend'>
              <span>
                {t('开盘价')} {formatPrice(shown.open, priceDigits)}
              </span>
              <span>
                {t('最高价')} {formatPrice(shown.high, priceDigits)}
              </span>
              <span>
                {t('最低价')} {formatPrice(shown.low, priceDigits)}
              </span>
              <span>
                {t('收盘价')} {formatPrice(shown.close, priceDigits)}
              </span>
              {change !== null && (
                <span className={change >= 0 ? 'trade-up' : 'trade-down'}>
                  {change >= 0 ? '+' : ''}
                  {change.toFixed(2)}%
                </span>
              )}
              <span>
                {t('成交量')} {formatQty(shown.volume, 4)}
              </span>
            </div>
          )}
          <div ref={hostRef} className='trade-chart-canvas' />
          <DrawOverlay d={drawing} t={t} />
          {picked && (
            <div
              className='trade-chart-fills'
              style={{
                left: Math.max(
                  0,
                  Math.min(
                    picked.x + 12,
                    (hostRef.current?.clientWidth || 0) - 260,
                  ),
                ),
                top: Math.max(picked.y - 12, 6),
              }}
            >
              <div className='flex items-center justify-between gap-2'>
                <span className='trade-muted'>
                  {timestamp2string(picked.openMs / 1000).slice(0, 16)}
                </span>
                <Button
                  size='small'
                  theme='borderless'
                  type='tertiary'
                  icon={<X size={12} />}
                  aria-label={t('关闭')}
                  onClick={() => setPicked(null)}
                />
              </div>
              {picked.fills.map((fill) => {
                const side = fillSide(fill);
                const label = fill.side
                  ? {
                      futures_open: { long: t('开多'), short: t('开空') },
                      futures_close: { long: t('平多'), short: t('平空') },
                      liquidation: { long: t('强平'), short: t('强平') },
                    }[fill.type]?.[fill.side]
                  : side === 'buy'
                    ? t('买入')
                    : t('卖出');
                return (
                  <div key={fill.id} className='trade-chart-fill-row'>
                    <span
                      className={side === 'buy' ? 'trade-up' : 'trade-down'}
                    >
                      {label}
                    </span>
                    <span className='trade-muted'>
                      {timestamp2string(fill.created_at).slice(11)}
                    </span>
                    <span>
                      {formatQty(fill.qty)} @{' '}
                      {formatPrice(fill.price, priceDigits)}
                    </span>
                  </div>
                );
              })}
            </div>
          )}
          {loading && (
            <div className='trade-chart-overlay'>
              <Spin />
            </div>
          )}
          {error && !loading && (
            <div className='trade-chart-overlay trade-chart-error'>{error}</div>
          )}
        </div>
      </div>
    </div>
  );
};

export default CandleChart;
