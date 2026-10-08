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

import React, { useEffect, useRef, useState } from 'react';
import { Tag, Tooltip, Typography } from '@douyinfe/semi-ui';
import { ArrowDownRight, ArrowUpRight, HelpCircle } from 'lucide-react';
import { AreaSeries, createChart, LineStyle } from 'lightweight-charts';
import { useActualTheme } from '../../context/Theme';
import { API } from '../../helpers';
import { chartTime, cssColor, withAlpha } from './api';

const { Text } = Typography;
const POLL_MS = 1000;

const formatUsd = (value) =>
  `$${Number(value).toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;

const clockOf = (seconds) => {
  const time = new Date(seconds * 1000);
  return [time.getHours(), time.getMinutes(), time.getSeconds()]
    .map((part) => String(part).padStart(2, '0'))
    .join(':');
};

// usePredictionLive 每秒拉一次 BTC 价格(Chainlink 60 秒 TWAP)、本轮目标价与 Polymarket 最近成交。价格点在页面里按轮次
// 累积：每次只要上次之后的新点，换轮时保留新一轮开始前 60 秒的点。页面隐藏时暂停。
export function usePredictionLive() {
  const [live, setLive] = useState(null);

  useEffect(() => {
    let alive = true;
    let timer;
    let controller;
    let windowStart = 0;
    let points = [];
    const load = async () => {
      if (!alive) return;
      if (document.hidden) {
        timer = setTimeout(load, POLL_MS);
        return;
      }
      controller = new AbortController();
      let delay = POLL_MS;
      try {
        const response = await API.get('/api/trade/prediction/live', {
          params: { since: points.length ? points[points.length - 1].time : 0 },
          signal: controller.signal,
          skipErrorHandler: true,
        });
        const data = response.data?.success ? response.data.data : null;
        if (!alive) return;
        if (data) {
          const from = data.window_start * 1000 - 60000;
          const kept =
            data.window_start === windowStart
              ? points
              : points.filter((point) => point.time >= from);
          windowStart = data.window_start;
          const last = kept.length ? kept[kept.length - 1].time : 0;
          points = kept.concat(
            (data.points || []).filter((point) => point.time > last),
          );
          setLive({ ...data, points });
        } else {
          delay = POLL_MS * 2;
        }
      } catch {
        delay = POLL_MS * 2;
      }
      if (alive) timer = setTimeout(load, delay);
    };
    load();
    return () => {
      alive = false;
      clearTimeout(timer);
      controller?.abort();
    };
  }, []);

  return live;
}

// 本轮的 BTC 价格与目标价：价格高于或等于目标价时按现在结束会判涨。图表是本轮开始前 60 秒起的 TWAP 走势，虚线是目标价。
export const PredictionPricePanel = ({ live, t }) => {
  const theme = useActualTheme();
  const hostRef = useRef(null);
  const chartRef = useRef(null);
  const seriesRef = useRef(null);
  const targetRef = useRef(null);
  const price = Number(live?.price) || 0;
  const target = Number(live?.open_price) || 0;
  const diff = price && target ? price - target : null;
  const hasDiff = diff !== null;
  const rising = hasDiff && diff >= 0;
  const points = live?.points;

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return undefined;
    const text = cssColor('--semi-color-text-2', '#888');
    const border = cssColor('--semi-color-border', '#e5e7eb');
    const chart = createChart(host, {
      autoSize: true,
      layout: {
        background: { color: 'transparent' },
        textColor: text,
        fontSize: 11,
      },
      grid: {
        vertLines: { visible: false },
        horzLines: { color: border },
      },
      rightPriceScale: { borderVisible: false },
      timeScale: {
        borderVisible: false,
        timeVisible: true,
        secondsVisible: true,
      },
      handleScroll: false,
      handleScale: false,
    });
    chartRef.current = chart;
    seriesRef.current = chart.addSeries(AreaSeries, {
      lineWidth: 2,
      priceFormat: { type: 'price', precision: 2, minMove: 0.01 },
      priceLineVisible: false,
      // 价格离目标价远时也要看得到目标价那条线：纵轴范围把目标价算进去。
      autoscaleInfoProvider: (original) => {
        const info = original();
        const price = targetRef.current?.price;
        if (!info || !price) return info;
        return {
          ...info,
          priceRange: {
            minValue: Math.min(info.priceRange.minValue, price),
            maxValue: Math.max(info.priceRange.maxValue, price),
          },
        };
      },
    });
    return () => {
      chart.remove();
      chartRef.current = null;
      seriesRef.current = null;
      targetRef.current = null;
    };
  }, [theme]);

  useEffect(() => {
    const series = seriesRef.current;
    if (!series) return;
    const color = !hasDiff
      ? cssColor('--semi-color-primary', '#3b82f6')
      : rising
        ? cssColor('--semi-color-success', '#16a34a')
        : cssColor('--semi-color-danger', '#dc2626');
    series.applyOptions({
      lineColor: color,
      topColor: withAlpha(color, 0.2),
      bottomColor: 'transparent',
    });
    const data = [];
    (points || []).forEach((point) => {
      const time = chartTime(point.time);
      if (data.length && data[data.length - 1].time >= time) return;
      data.push({ time, value: point.price });
    });
    series.setData(data);
    if (targetRef.current?.price !== target) {
      if (targetRef.current) series.removePriceLine(targetRef.current.line);
      targetRef.current = target
        ? {
            price: target,
            line: series.createPriceLine({
              price: target,
              color: cssColor('--semi-color-text-2', '#888'),
              lineWidth: 1,
              lineStyle: LineStyle.Dashed,
              axisLabelVisible: true,
              title: t('目标价'),
            }),
          }
        : null;
    }
    chartRef.current?.timeScale().fitContent();
  }, [points, target, rising, hasDiff, theme, t]);

  return (
    <div className='flex flex-col gap-2'>
      <div className='flex flex-wrap items-end justify-between gap-2'>
        <div>
          <Text type='tertiary' size='small'>
            {t('BTC 当前价（Chainlink 60 秒 TWAP）')}
          </Text>
          <div
            className={`trade-num text-2xl font-semibold ${
              hasDiff ? (rising ? 'trade-up' : 'trade-down') : ''
            }`}
          >
            {price ? formatUsd(price) : '—'}
          </div>
          {hasDiff && (
            <div
              className={`trade-num flex items-center gap-1 text-xs font-semibold ${
                rising ? 'trade-up' : 'trade-down'
              }`}
            >
              {rising ? (
                <ArrowUpRight size={14} aria-hidden='true' />
              ) : (
                <ArrowDownRight size={14} aria-hidden='true' />
              )}
              {`${rising ? '+' : '-'}${formatUsd(Math.abs(diff))}`}
            </div>
          )}
        </div>
        <Tooltip
          content={t(
            '目标价是本轮开始那一刻往前 60 秒的 Chainlink BTC 时间加权均价（TWAP）。结算看本轮结束时同样口径的 TWAP：不低于目标价判涨，否则判跌。',
          )}
        >
          <span className='trade-prediction-target trade-num'>
            <HelpCircle size={12} aria-hidden='true' />
            {target ? `${t('目标价')} ${formatUsd(target)}` : t('目标价获取中')}
          </span>
        </Tooltip>
      </div>
      <div ref={hostRef} className='trade-prediction-chart' />
      {live && !live.connected && (
        <Text type='tertiary' size='small'>
          {t('价格推送连接中…')}
        </Text>
      )}
    </div>
  );
};

// Polymarket 上本轮最近的成交(吃单方)，新的在上面；不显示交易者。
export const PredictionTradesCard = ({ live, t }) => (
  <div className='trade-card flex flex-col gap-2'>
    <div className='flex items-center justify-between gap-2'>
      <Text strong>{t('实时成交')}</Text>
      <Text type='tertiary' size='small'>
        Polymarket
      </Text>
    </div>
    {live?.trades?.length ? (
      <div className='trade-prediction-trades'>
        <div className='trade-prediction-trade trade-prediction-trade-head'>
          <span>{t('时间')}</span>
          <span>{t('方向')}</span>
          <span className='text-right'>{t('成交价格')}</span>
          <span className='text-right'>{t('金额')}</span>
        </div>
        {live.trades.map((trade, index) => {
          const up = trade.outcome === 'UP';
          return (
            <div
              key={`${trade.time}-${index}`}
              className='trade-prediction-trade trade-num'
            >
              <span className='text-[var(--semi-color-text-2)]'>
                {clockOf(trade.time)}
              </span>
              <span className='flex items-center gap-1'>
                {trade.side === 'BUY' ? t('买入') : t('卖出')}
                <Tag size='small' color={up ? 'green' : 'red'}>
                  {up ? t('看涨') : t('看跌')}
                </Tag>
              </span>
              <span className='text-right'>
                {`${Number((trade.price * 100).toFixed(1))}¢`}
              </span>
              <span className='text-right'>
                {formatUsd(trade.size * trade.price)}
              </span>
            </div>
          );
        })}
      </div>
    ) : (
      <Text type='tertiary' size='small' className='py-6 text-center'>
        {t('等待成交…')}
      </Text>
    )}
  </div>
);
