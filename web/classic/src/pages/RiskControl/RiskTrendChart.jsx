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

import React, { useEffect, useMemo, useState } from 'react';
import { Empty, Radio, RadioGroup, Typography } from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { initVChartSemiTheme } from '@visactor/vchart-semi-theme';
import { useTranslation } from 'react-i18next';
import { riskEventTypeMeta } from './riskLabels';

const { Text } = Typography;

const CHART_CONFIG = { mode: 'desktop-browser' };

// 事件大类的固定配色:拦截偏红、封禁紫、解封绿、告警橙,与列表里的标签一致。
const CATEGORY_COLORS = {
  block: '#f5222d',
  ban: '#722ed1',
  unban: '#52c41a',
  alert: '#fa8c16',
};

const CATEGORY_LABELS = {
  block: '拦截',
  ban: '封禁与禁用',
  unban: '解封与恢复',
  alert: '告警',
};

const pad = (value) => String(value).padStart(2, '0');

// 本地时间的刻度文本。始终带日期:24 小时窗口的首尾两格时刻相同,只写时刻会被图表当成同一类。
const bucketLabel = (timestamp) => {
  const date = new Date(timestamp * 1000);
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:00`;
};

// 窗口超过两天时按本地时间合并成 6 小时一格,否则保持后端的 1 小时整点。
const slotStep = (hours) => (hours > 48 ? 6 * 3600 : 3600);

// 整点所在格子的起点。6 小时格按本地时间对齐,时区是整小时偏移时与后端的 UTC 整点无损对应。
const slotStart = (timestamp, step) => {
  if (step === 3600) return timestamp;
  const local = new Date(timestamp * 1000);
  return timestamp - ((local.getHours() % 6) * 3600 + local.getMinutes() * 60);
};

// 窗口内的全部格子起点,没有数据的格子也要占位,图上才看得出空档。
const slotsOf = (since, hours, step) => {
  const start =
    Math.floor((since || Date.now() / 1000 - hours * 3600) / 3600) * 3600;
  const end = Math.floor(Date.now() / 1000 / 3600) * 3600;
  const slots = [];
  for (let slot = slotStart(start, step); slot <= end; slot += step)
    slots.push(slot);
  return slots;
};

const stepHint = (step, t) =>
  step === 3600
    ? t('每根柱子为 1 小时，按本地时间显示。')
    : t('每根柱子为 6 小时，按本地时间显示。');

// 活动趋势:按事件大类堆叠的柱状图。后端按 UTC 整点分桶,窗口超过一天时这里按本地时间
// 合并成 6 小时一格,避免 168 根柱子挤在一起;可在命中次数与落库条数之间切换。
const RiskTrendChart = ({ buckets = [], since, hours = 24, height = 260 }) => {
  const { t } = useTranslation();
  const [measure, setMeasure] = useState('hits');

  useEffect(() => {
    initVChartSemiTheme({ isWatchingThemeSwitch: true });
  }, []);

  const step = slotStep(hours);
  const values = useMemo(() => {
    const totals = new Map();
    for (const bucket of buckets) {
      const category = riskEventTypeMeta(bucket.event_type, t).category;
      if (!category) continue;
      const key = `${slotStart(bucket.bucket, step)}:${category}`;
      totals.set(
        key,
        (totals.get(key) || 0) +
          (measure === 'hits' ? bucket.hits : bucket.records),
      );
    }
    return slotsOf(since, hours, step).flatMap((slot) =>
      Object.keys(CATEGORY_COLORS).map((category) => ({
        time: bucketLabel(slot),
        type: t(CATEGORY_LABELS[category]),
        value: totals.get(`${slot}:${category}`) || 0,
      })),
    );
  }, [buckets, measure, since, hours, step, t]);

  if (!buckets.length) {
    return <Empty description={t('所选时间范围内没有风控事件')} />;
  }

  const spec = {
    type: 'bar',
    data: [{ id: 'riskActivity', values }],
    xField: 'time',
    yField: 'value',
    seriesField: 'type',
    stack: true,
    height,
    padding: { top: 8, right: 8, bottom: 8, left: 8 },
    legends: { visible: true, position: 'middle', orient: 'bottom' },
    axes: [
      { orient: 'left', title: { visible: false } },
      { orient: 'bottom', label: { autoHide: true, autoRotate: false } },
    ],
    color: {
      type: 'ordinal',
      domain: Object.keys(CATEGORY_COLORS).map((category) =>
        t(CATEGORY_LABELS[category]),
      ),
      range: Object.values(CATEGORY_COLORS),
    },
    tooltip: {
      mark: {
        content: [
          { key: (datum) => datum.type, value: (datum) => datum.value },
        ],
      },
    },
    animation: false,
  };

  return (
    <div>
      <div className='flex flex-wrap items-center justify-between gap-2 mb-2'>
        <Text type='tertiary' size='small'>
          {stepHint(step, t)}
        </Text>
        <RadioGroup
          type='button'
          size='small'
          value={measure}
          onChange={(event) => setMeasure(event.target.value)}
          aria-label={t('统计口径')}
        >
          <Radio value='hits'>{t('命中次数')}</Radio>
          <Radio value='records'>{t('记录条数')}</Radio>
        </RadioGroup>
      </div>
      <VChart spec={spec} option={CHART_CONFIG} />
    </div>
  );
};

// 单个对象(用户 / IP / UA)在窗口内的调用时间线:正常与错误调用堆叠,柱高即总调用数。
export const RiskTimelineChart = ({
  timeline = [],
  hours = 24,
  height = 200,
}) => {
  const { t } = useTranslation();

  useEffect(() => {
    initVChartSemiTheme({ isWatchingThemeSwitch: true });
  }, []);

  const step = slotStep(hours);
  const values = useMemo(() => {
    const totals = new Map();
    for (const bucket of timeline) {
      const slot = slotStart(bucket.bucket, step);
      const current = totals.get(slot) || { requests: 0, errors: 0 };
      current.requests += bucket.requests;
      current.errors += bucket.errors;
      totals.set(slot, current);
    }
    return slotsOf(0, hours, step).flatMap((slot) => {
      const total = totals.get(slot) || { requests: 0, errors: 0 };
      return [
        {
          time: bucketLabel(slot),
          type: t('正常调用'),
          value: Math.max(0, total.requests - total.errors),
        },
        { time: bucketLabel(slot), type: t('错误调用'), value: total.errors },
      ];
    });
  }, [timeline, hours, step, t]);

  if (!timeline.length) {
    return <Empty description={t('所选时间范围内没有调用记录')} />;
  }

  const spec = {
    type: 'bar',
    data: [{ id: 'riskTimeline', values }],
    xField: 'time',
    yField: 'value',
    seriesField: 'type',
    stack: true,
    height,
    padding: { top: 8, right: 8, bottom: 8, left: 8 },
    legends: { visible: true, position: 'middle', orient: 'bottom' },
    axes: [
      { orient: 'left', title: { visible: false } },
      { orient: 'bottom', label: { autoHide: true, autoRotate: false } },
    ],
    color: {
      type: 'ordinal',
      domain: [t('正常调用'), t('错误调用')],
      range: ['#3b82f6', '#f5222d'],
    },
    tooltip: {
      mark: {
        content: [
          { key: (datum) => datum.type, value: (datum) => datum.value },
        ],
      },
    },
    animation: false,
  };

  return (
    <div>
      <Text type='tertiary' size='small' className='block mb-2'>
        {stepHint(step, t)}
      </Text>
      <VChart spec={spec} option={CHART_CONFIG} />
    </div>
  );
};

export default RiskTrendChart;
