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

import React, { useEffect, useState, useCallback, useMemo } from 'react';
import { Empty, Skeleton } from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { initVChartSemiTheme } from '@visactor/vchart-semi-theme';
import { useTranslation } from 'react-i18next';
import { API, showError, getLobeHubIcon, renderQuota } from '../../helpers';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { pinePath, ridge } from '../Games/wendao/art/ink';
import './rankings.css';

// ============================================================================
// Utility helpers
// ============================================================================

function formatNumber(num) {
  if (num === undefined || num === null) return '0';
  if (num >= 1e9) return (num / 1e9).toFixed(2) + 'B';
  if (num >= 1e6) return (num / 1e6).toFixed(2) + 'M';
  if (num >= 1e3) return (num / 1e3).toFixed(2) + 'K';
  return String(num);
}

function formatPercent(value) {
  if (value === undefined || value === null) return '0%';
  return (value * 100).toFixed(1) + '%';
}

function formatGrowth(value) {
  if (value === undefined || value === null) return '-';
  const sign = value > 0 ? '+' : value < 0 ? '−' : '';
  return sign + Math.abs(value).toFixed(1) + '%';
}

function formatLatency(ms) {
  if (ms === undefined || ms === null) return '-';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function formatRangeStamp(ts) {
  const date = new Date(ts * 1000);
  const pad = (value) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function formatRange(range) {
  if (!range || !range.start || !range.end) return '';
  return `${formatRangeStamp(range.start)} — ${formatRangeStamp(range.end)}`;
}

function formatPercentValue(value) {
  return `${Number(value || 0).toFixed(1)}%`;
}

// Stat concentration hints round to a whole percent; below 1% a rounded "0%"
// would misstate a non-zero concentration.
function formatSharePct(pct) {
  return pct >= 1 ? Math.round(pct) : '<1';
}

// Long model names would each take most of a phone-width legend line, leaving
// room for one entry per row. The compact legend keeps the head of the name
// so two entries fit per line; the full name stays in the tooltip.
function truncateLegendLabel(text, max = 14) {
  const label = String(text ?? '');
  return label.length > max ? `${label.slice(0, max - 1)}…` : label;
}

// Mineral pigments of the Wendao ink-wash palette — cinnabar, azurite,
// malachite, ochre and so on — picked mid-tone so every line reads on both the
// rice-paper and the night-paper card. The chart draws on a canvas, which
// cannot resolve CSS variables, so they are literal colors; the compact legend
// reuses them for its dots.
const SERIES_COLORS = [
  '#b23a2a',
  '#4d7a99',
  '#6f8f5a',
  '#c08a3e',
  '#7a5f96',
  '#8a7356',
  '#4f9690',
  '#a85d7a',
  '#7b7d78',
  '#c2542f',
];

function fillTrendSeries(points, seriesField, valueField) {
  const labels = [];
  const series = [];
  const values = new Map();

  points.forEach((point) => {
    const label = point.Time;
    const seriesName = point[seriesField];
    const key = `${label}\u0000${seriesName}`;
    if (!labels.includes(label)) labels.push(label);
    if (!series.includes(seriesName)) series.push(seriesName);
    values.set(key, point[valueField] || 0);
  });

  return labels.flatMap((label) =>
    series.map((seriesName) => ({
      Time: label,
      [seriesField]: seriesName,
      [valueField]: values.get(`${label}\u0000${seriesName}`) || 0,
    })),
  );
}

/**
 * Trend chart with a built-in legend. On a phone VChart lays the legend out
 * one entry per row — the canvas is only ~330px wide and every model name eats
 * a full line — which cut the plot area down to a few rows. The compact layout
 * therefore hides VChart's legend and renders the entries as a single
 * horizontally scrollable chip row above the chart; tapping a chip hides that
 * series, matching the legend behaviour it replaces.
 */
function TrendChart({
  data,
  yField,
  seriesField,
  formatValue = formatNumber,
  compact = false,
}) {
  // A phone-width chart is shorter so two of them fit one screen; the height
  // has to be passed to the chart, not set in CSS, because the renderer sizes
  // its canvas to the wrapper at mount.
  const height = compact ? 200 : 240;
  const series = useMemo(
    () => [...new Set(data.map((point) => point[seriesField]))],
    [data, seriesField],
  );
  const [hiddenSeries, setHiddenSeries] = useState(() => new Set());

  // Filtering the data (rather than the hidden legend component) is what makes
  // the toggle work: with `legends.visible: false` there is no legend
  // component for the imperative setLegendSelectedData API to reach.
  const visibleData = useMemo(
    () =>
      hiddenSeries.size === 0
        ? data
        : data.filter((point) => !hiddenSeries.has(point[seriesField])),
    [data, hiddenSeries, seriesField],
  );

  const toggleSeries = (name) => {
    setHiddenSeries((previous) => {
      const next = new Set(previous);
      if (next.has(name)) {
        next.delete(name);
      } else if (next.size < series.length - 1) {
        // Keep at least one series on the chart; an empty chart reads as a
        // loading failure rather than a filter.
        next.add(name);
      }
      return next;
    });
  };

  const spec = useMemo(
    () => ({
      type: 'line',
      // The dark theme paints its own page color behind the plot, which shows
      // as a darker block on the card; let the paper show through instead.
      background: 'transparent',
      data: [{ id: 'rankingsTrendData', values: visibleData }],
      xField: 'Time',
      yField,
      seriesField,
      // Pinning the palette keeps the DOM legend's dots in step with the
      // lines; the theme's generated palette is not readable from here.
      color: SERIES_COLORS,
      // The compact legend lives in the DOM above the chart.
      legends: compact
        ? { visible: false }
        : { visible: true, orient: 'top', selectMode: 'single' },
      line: { style: { lineWidth: 2, curveType: 'monotone' } },
      point: { visible: false },
      axes: [
        {
          orient: 'left',
          label: { formatMethod: (value) => formatValue(value) },
        },
      ],
      tooltip: {
        dimension: {
          content: [
            {
              key: (datum) => datum[seriesField],
              value: (datum) => formatValue(datum[yField]),
            },
          ],
        },
      },
    }),
    [visibleData, formatValue, seriesField, yField, compact],
  );

  return (
    <>
      {compact && series.length > 1 && (
        <div className='rankings-legend' role='group'>
          {series.map((name, index) => {
            const hidden = hiddenSeries.has(name);
            return (
              <button
                type='button'
                key={name}
                className={`rankings-legend-item${hidden ? ' is-off' : ''}`}
                aria-pressed={!hidden}
                title={name}
                onClick={() => toggleSeries(name)}
              >
                <span
                  className='rankings-legend-dot'
                  style={{
                    background: SERIES_COLORS[index % SERIES_COLORS.length],
                  }}
                />
                {truncateLegendLabel(name, 11)}
              </button>
            );
          })}
        </div>
      )}
      <div className='rankings-trend-chart' style={{ height }}>
        <VChart spec={spec} />
      </div>
    </>
  );
}

// ============================================================================
// Sparkline — tiny inline trend curve for each ranking row
// ============================================================================

// Canvas strokes cannot read CSS variables, so the ink grey is literal; it sits
// between the light and the dark paper and reads on both.
const SPARK_COLOR = '#8f897d';

/**
 * Tiny inline trend curve. `allowZero` keeps legitimate zero readings (an hour
 * where every request failed) on the curve — dropping them would hide exactly
 * the incidents this chart exists to surface. Values are still dropped when the
 * metric is simply not collected (negative/undefined).
 */
function Sparkline({ series, metric, data, allowZero = false }) {
  const values = useMemo(() => {
    const keep = (value) =>
      typeof value === 'number' &&
      Number.isFinite(value) &&
      (allowZero ? value >= 0 : value > 0);

    // Support a plain number array (data prop) or object series + metric.
    const source = data
      ? data.filter(keep)
      : (series || [])
          .filter((point) => keep(point[metric]))
          .map((point) => point[metric]);

    return source.map((value, index) => ({ x: index, y: value }));
  }, [series, metric, data, allowZero]);

  const spec = useMemo(
    () => ({
      type: 'line',
      background: 'transparent',
      data: [{ id: 'spark', values }],
      xField: 'x',
      yField: 'y',
      line: {
        style: {
          lineWidth: 1.25,
          curveType: 'monotone',
          stroke: SPARK_COLOR,
        },
      },
      point: { visible: false },
      axes: [
        // Scale to the data, not from zero: a success rate moving between
        // 97% and 99% is the whole story, and from zero it draws flat.
        { orient: 'left', visible: false, zero: false, nice: false },
        { orient: 'bottom', visible: false },
      ],
      legends: { visible: false },
      tooltip: { visible: false },
      animation: false,
      padding: { top: 2, bottom: 2, left: 0, right: 0 },
    }),
    [values],
  );

  // Every hook above runs unconditionally: bailing out earlier changed the hook
  // count between renders and crashed the page when a row gained or lost data.
  if (values.length < 2) {
    return <span className='rankings-sparkline rankings-sparkline-empty' />;
  }

  return (
    <span className='rankings-sparkline'>
      <VChart spec={spec} />
    </span>
  );
}

// ============================================================================
// Banner art — distant ink mountains, drawn the same way as the Wendao scenes
// ============================================================================

const BANNER_W = 800;
const BANNER_H = 200;
const bannerRidge = (y, amp, seed, sharp = 0) =>
  ridge({ y, amp, seed, sharp, width: BANNER_W, height: BANNER_H, step: 10 });
// The paths never change, so they are built once rather than on every render.
const BANNER_PATHS = {
  far: bannerRidge(118, 74, 40, 1.2),
  mid: bannerRidge(158, 62, 260, 0.8),
  near: bannerRidge(198, 30, 520),
  pines: [
    [338, 196, 0.78],
    [362, 192, 0.62],
    [470, 197, 0.68],
  ].map(([x, y, s]) => pinePath(x, y, s)),
};

function BannerArt() {
  return (
    <svg
      className='rankings-banner-art'
      viewBox={`0 0 ${BANNER_W} ${BANNER_H}`}
      preserveAspectRatio='xMaxYMax slice'
      aria-hidden='true'
      focusable='false'
    >
      <defs>
        <linearGradient id='rankings-mist' x1='0' y1='0' x2='0' y2='1'>
          <stop offset='0' className='rankings-mist-edge' />
          <stop offset='0.5' className='rankings-mist-core' />
          <stop offset='1' className='rankings-mist-edge' />
        </linearGradient>
      </defs>
      <circle cx='650' cy='54' r='20' className='rankings-art-sun' />
      <path d={BANNER_PATHS.far} className='rankings-art-far' />
      <rect y='96' width={BANNER_W} height='34' fill='url(#rankings-mist)' />
      <path d={BANNER_PATHS.mid} className='rankings-art-mid' />
      <rect y='146' width={BANNER_W} height='26' fill='url(#rankings-mist)' />
      <path d={BANNER_PATHS.near} className='rankings-art-near' />
      {BANNER_PATHS.pines.map((d) => (
        <path key={d} d={d} className='rankings-art-pine' />
      ))}
    </svg>
  );
}

// ============================================================================
// Small marks — rank seal, growth figure, identity icons
// ============================================================================

// The top three carry a seal instead of a medal: first place a solid
// white-character seal, second and third an outlined red-character one.
function RankMark({ rank }) {
  if (rank >= 1 && rank <= 3) {
    return <span className={`rankings-seal rank-${rank}`}>{rank}</span>;
  }
  return <span className='rankings-rank'>{rank}</span>;
}

function Growth({ value }) {
  if (value === undefined || value === null) return null;
  const tone = value > 0 ? 'up' : value < 0 ? 'down' : 'flat';
  return (
    <span className={`rankings-growth ${tone}`}>{formatGrowth(value)}</span>
  );
}

// Falls back to '?' via getLobeHubIcon.
function VendorIcon({ icon }) {
  return (
    <span className='rankings-vendor-icon'>{getLobeHubIcon(icon, 20)}</span>
  );
}

function UserAvatar({ name }) {
  const str = name || '?';
  return <span className='rankings-avatar'>{str.charAt(0).toUpperCase()}</span>;
}

// ============================================================================
// Stat strip and section frame
// ============================================================================

function StatStrip({ items }) {
  return (
    <div className='rankings-stats'>
      {items.map((item) => (
        <div className='rankings-stat' key={item.label}>
          <div className='rankings-stat-label'>{item.label}</div>
          <div className='rankings-stat-value'>{item.value}</div>
          {item.hint && <div className='rankings-stat-hint'>{item.hint}</div>}
        </div>
      ))}
    </div>
  );
}

function SectionCard({ title, subtitle, aside, children }) {
  return (
    <section className='rankings-section'>
      <header className='rankings-section-header'>
        <div className='rankings-section-heading'>
          <h2 className='rankings-section-title'>{title}</h2>
          {subtitle && (
            <div className='rankings-section-subtitle'>{subtitle}</div>
          )}
        </div>
        {aside}
      </header>
      <div className='rankings-section-body'>{children}</div>
    </section>
  );
}

// ============================================================================
// Board paging
// ============================================================================

// Boards list up to 50 rows. A screenful is shown by default with the rest one
// tap away; phones get fewer because the page is their only scroll surface.
const MOBILE_BOARD_ROWS = 10;
const DESKTOP_BOARD_ROWS = 20;

function useBoardRows(rows, isMobile) {
  const [expanded, setExpanded] = useState(false);
  const limit = isMobile ? MOBILE_BOARD_ROWS : DESKTOP_BOARD_ROWS;
  const canToggle = rows.length > limit;
  const visibleRows = canToggle && !expanded ? rows.slice(0, limit) : rows;
  const toggle = useCallback(() => setExpanded((value) => !value), []);
  return { visibleRows, canToggle, expanded, toggle };
}

// ============================================================================
// Rank table — one standings table per tab
// ============================================================================

/**
 * Every tab ranks one population — models by usage, users, models by
 * performance — that can be ordered several ways. Instead of one card per
 * ordering (the same model listed three times), the table shows every metric
 * as a column and the sort switch picks the one that orders the rows.
 *
 * Columns carry a priority: the sorted column always stays, the others drop
 * out as the card narrows (see the container queries in rankings.css), so a
 * phone keeps rank, name and the metric the board is sorted by.
 */
function RankTable({
  title,
  note,
  sorts,
  sortKey,
  onSort,
  nameLabel,
  columns,
  rows,
  rowKey,
  rankOf,
  identity,
  barOf,
}) {
  const { t } = useTranslation();
  const isMobile = useIsMobile();
  const list = useBoardRows(rows, isMobile);

  const sortSwitch = sorts && sorts.length > 1 && (
    <div className='rankings-sort' role='group' aria-label={t('排序')}>
      {sorts.map((sort) => (
        <button
          type='button'
          key={sort.key}
          aria-pressed={sort.key === sortKey}
          className={`rankings-sort-btn${sort.key === sortKey ? ' is-active' : ''}`}
          onClick={() => onSort(sort.key)}
        >
          {sort.label}
        </button>
      ))}
    </div>
  );

  const columnClass = (column) =>
    [
      column.numeric === false ? 'is-chart' : 'is-num',
      column.sortKey === sortKey ? 'is-sorted' : `rk-p${column.priority || 1}`,
    ].join(' ');

  return (
    <SectionCard title={title} subtitle={note} aside={sortSwitch}>
      {rows.length === 0 ? (
        <Empty description={t('暂无数据')} style={{ padding: 32 }} />
      ) : (
        <table className='rankings-table'>
          <thead>
            <tr>
              <th className='rk-col-rank'>{t('排名')}</th>
              <th className='rk-col-name'>{nameLabel}</th>
              {columns.map((column) => (
                <th key={column.key} className={columnClass(column)}>
                  {column.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {list.visibleRows.map((row, index) => {
              const who = identity(row);
              const bar = barOf ? barOf(row) : null;
              return (
                <tr key={rowKey(row)}>
                  <td className='rk-col-rank'>
                    <RankMark rank={rankOf ? rankOf(row) : index + 1} />
                  </td>
                  <td className='rk-col-name'>
                    <div className='rankings-who'>
                      {who.icon}
                      <div className='rankings-who-text'>
                        <div className='rankings-who-name' title={who.name}>
                          {who.name}
                        </div>
                        {who.sub && (
                          <div className='rankings-who-sub'>{who.sub}</div>
                        )}
                        {bar !== null && (
                          <div className='rankings-bar'>
                            <div
                              className='rankings-bar-fill'
                              style={{ width: `${Math.max(bar, 2)}%` }}
                            />
                          </div>
                        )}
                      </div>
                    </div>
                  </td>
                  {columns.map((column) => (
                    <td key={column.key} className={columnClass(column)}>
                      {column.render(row)}
                    </td>
                  ))}
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {list.canToggle && (
        <button
          type='button'
          className='rankings-show-more'
          onClick={list.toggle}
        >
          {list.expanded ? t('收起') : t('显示更多')}
        </button>
      )}
    </SectionCard>
  );
}

// ============================================================================
// Loading skeleton
// ============================================================================

function ListSkeleton({ rows = 6 }) {
  return (
    <div className='rankings-section'>
      <div className='rankings-section-body rankings-skeleton'>
        {Array.from({ length: rows }).map((_, i) => (
          <div className='rankings-skeleton-row' key={i}>
            <Skeleton.Avatar size='small' />
            <div style={{ flex: 1 }}>
              <Skeleton.Title style={{ width: '40%', marginBottom: 8 }} />
              <Skeleton.Paragraph rows={1} style={{ width: '75%' }} />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * Every tab loads the same way: fetch for the period, report the resolved
 * window, keep the payload. `pick` returns what the tab needs from the
 * response, or null when the response does not carry it; it must be a
 * module-level function so it never changes between renders.
 */
function useRankingData(url, period, onRange, failMessage, pick) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(null);

  useEffect(() => {
    // A slow response for the previous period must not overwrite the current
    // one after the user switched.
    let stale = false;
    const load = async () => {
      setLoading(true);
      try {
        const res = await API.get(`${url}?period=${period}`);
        if (stale) return;
        const payload = res.data?.success ? pick(res.data.data) : null;
        if (!payload) {
          showError(res.data?.message || t('加载失败'));
          return;
        }
        setData(payload);
        onRange?.({
          start: res.data.data?.start_time,
          end: res.data.data?.end_time,
        });
      } catch (e) {
        if (!stale) showError(t(failMessage));
      } finally {
        if (!stale) setLoading(false);
      }
    };
    load();
    return () => {
      stale = true;
    };
  }, [url, period, onRange, failMessage, pick, t]);

  return { loading, data };
}

const pickPayload = (payload) => payload || null;
const pickPerfModels = (payload) =>
  Array.isArray(payload?.models) ? payload.models : null;

// ============================================================================
// Tab 1: Usage — models ranked by tokens or by calls
// ============================================================================

function MoverList({ title, subtitle, rows, direction }) {
  return (
    <SectionCard title={title} subtitle={subtitle}>
      <ul className='rankings-movers'>
        {rows.map((m) => (
          <li className='rankings-mover' key={m.model_name}>
            <VendorIcon icon={m.vendor_icon} />
            <div className='rankings-who-text'>
              <div className='rankings-who-name' title={m.model_name}>
                {m.model_name}
              </div>
              {m.vendor && <div className='rankings-who-sub'>{m.vendor}</div>}
            </div>
            <span className={`rankings-rank-shift ${direction}`}>
              {direction === 'up' ? '↑' : '↓'}
              {Math.abs(m.rank_delta)}
            </span>
            <Growth value={m.growth_pct} />
          </li>
        ))}
      </ul>
    </SectionCard>
  );
}

// Top-3 concentration. The share fields are computed server-side against the
// FULL window total, while the boards are truncated to the leaderboard size —
// summing raw values would overstate the share, so sum the per-row shares
// instead. Null when the board has 3 rows or fewer ("top 3 of 3" is always
// 100%).
function topThreeShare(rows) {
  if (!rows || rows.length <= 3) return null;
  return rows.slice(0, 3).reduce((sum, m) => sum + (m.share || 0), 0) * 100;
}

function LLMRankings({ period, onRange }) {
  const { t } = useTranslation();
  const isMobile = useIsMobile();
  const [sortKey, setSortKey] = useState('tokens');
  const { loading, data } = useRankingData(
    '/api/rankings',
    period,
    onRange,
    '加载排行榜失败',
    pickPayload,
  );

  const totalTokens = useMemo(
    () => data?.models?.reduce((sum, m) => sum + (m.total_tokens || 0), 0) || 0,
    [data],
  );

  const totalRequests = useMemo(
    () =>
      data?.models_by_requests?.reduce(
        (sum, m) => sum + (m.total_requests || 0),
        0,
      ) || 0,
    [data],
  );

  const modelTrendData = useMemo(
    () =>
      fillTrendSeries(
        (data?.models_history?.points || []).map((point) => ({
          Time: point.label,
          Model: point.model,
          Tokens: point.tokens,
        })),
        'Model',
        'Tokens',
      ),
    [data],
  );

  const vendorTrendData = useMemo(
    () =>
      fillTrendSeries(
        (data?.vendor_share_history?.points || []).map((point) => ({
          Time: point.label,
          Vendor: point.vendor,
          Share: point.share * 100,
        })),
        'Vendor',
        'Share',
      ),
    [data],
  );

  // Per-model token sparkline series extracted from models_history.
  const modelSparklines = useMemo(() => {
    const map = new Map();
    (data?.models_history?.points || []).forEach((point) => {
      if (!map.has(point.model)) map.set(point.model, []);
      map.get(point.model).push(point.tokens);
    });
    return map;
  }, [data]);

  if (loading) {
    return <ListSkeleton />;
  }

  if (!data) {
    return <Empty description={t('暂无数据')} style={{ padding: 60 }} />;
  }

  const topTokenShare = topThreeShare(data.models);
  const topRequestShare = topThreeShare(data.models_by_requests);
  const topVendorShare = topThreeShare(data.vendors);

  // The server ranks the two orderings separately (the token board leaves out
  // request-only models), so the sort switch swaps lists rather than
  // re-sorting one.
  const byTokens = sortKey === 'tokens';
  const rows = (byTokens ? data.models : data.models_by_requests) || [];
  const metricOf = (m) => (byTokens ? m.total_tokens : m.total_requests) || 0;
  const maxMetric = Math.max(...rows.map(metricOf), 0);

  const columns = [
    // models_history only follows the token leaders, so the trend column
    // belongs to the token ordering.
    ...(byTokens
      ? [
          {
            key: 'trend',
            label: t('走势'),
            numeric: false,
            priority: 3,
            render: (m) => (
              <Sparkline data={modelSparklines.get(m.model_name)} />
            ),
          },
        ]
      : []),
    {
      key: 'tokens',
      sortKey: 'tokens',
      label: 'Tokens',
      priority: 2,
      render: (m) => formatNumber(m.total_tokens),
    },
    {
      key: 'requests',
      sortKey: 'requests',
      label: t('调用次数'),
      priority: 2,
      render: (m) => formatNumber(m.total_requests),
    },
    {
      key: 'share',
      label: t('占比'),
      priority: 3,
      render: (m) => formatPercent(m.share),
    },
    {
      key: 'growth',
      label: t('环比'),
      priority: 1,
      render: (m) => <Growth value={m.growth_pct} />,
    },
  ];

  return (
    <div className='rankings-stack'>
      <StatStrip
        items={[
          {
            label: t('总令牌用量'),
            value: formatNumber(totalTokens),
            hint:
              topTokenShare !== null
                ? t('Top 3 模型占 Token {{percent}}%', {
                    percent: formatSharePct(topTokenShare),
                  })
                : undefined,
          },
          { label: t('上榜模型'), value: data.models?.length || 0 },
          {
            label: t('总调用次数'),
            value: formatNumber(totalRequests),
            hint:
              topRequestShare !== null
                ? t('Top 3 模型占调用 {{percent}}%', {
                    percent: formatSharePct(topRequestShare),
                  })
                : undefined,
          },
        ]}
      />

      <RankTable
        title={t('群模榜')}
        sorts={[
          { key: 'tokens', label: 'Tokens' },
          { key: 'requests', label: t('调用次数') },
        ]}
        sortKey={sortKey}
        onSort={setSortKey}
        nameLabel={t('模型')}
        columns={columns}
        rows={rows}
        rowKey={(m) => m.model_name}
        rankOf={(m) => m.rank}
        identity={(m) => ({
          icon: <VendorIcon icon={m.vendor_icon} />,
          name: m.model_name,
          sub: m.vendor,
        })}
        barOf={(m) => (maxMetric > 0 ? (metricOf(m) / maxMetric) * 100 : null)}
      />

      <div className='rankings-grid-2'>
        <SectionCard title={t('模型消耗趋势')}>
          {modelTrendData.length > 0 ? (
            <TrendChart
              data={modelTrendData}
              yField='Tokens'
              seriesField='Model'
              formatValue={formatNumber}
              compact={isMobile}
            />
          ) : (
            <Empty description={t('暂无数据')} />
          )}
        </SectionCard>
        <SectionCard
          title={t('厂商份额趋势')}
          subtitle={
            topVendorShare !== null
              ? `${t('各厂商 Token 消耗占比随时间的变化')} · ${t('Top 3 厂商占比 {{percent}}%', { percent: formatSharePct(topVendorShare) })}`
              : t('各厂商 Token 消耗占比随时间的变化')
          }
        >
          {vendorTrendData.length > 0 ? (
            <TrendChart
              data={vendorTrendData}
              yField='Share'
              seriesField='Vendor'
              formatValue={formatPercentValue}
              compact={isMobile}
            />
          ) : (
            <Empty description={t('暂无数据')} />
          )}
        </SectionCard>
      </div>

      {(data.top_movers?.length > 0 || data.top_droppers?.length > 0) && (
        <div className='rankings-grid-2'>
          {data.top_movers?.length > 0 && (
            <MoverList
              title={t('上升趋势')}
              subtitle={t('排名上升最快的模型')}
              rows={data.top_movers}
              direction='up'
            />
          )}
          {data.top_droppers?.length > 0 && (
            <MoverList
              title={t('下降趋势')}
              subtitle={t('排名下降最快的模型')}
              rows={data.top_droppers}
              direction='down'
            />
          )}
        </div>
      )}
    </div>
  );
}

// ============================================================================
// Tab 2: Users — ranked by tokens, requests or quota spent
// ============================================================================

// Each ordering is its own server-side top list; `total` names the summary
// field the share column divides by.
const USER_SORTS = {
  tokens: {
    list: 'token_rankings',
    total: 'total_tokens',
    metricOf: (row) => row.total_tokens || 0,
  },
  requests: {
    list: 'request_rankings',
    total: 'total_requests',
    metricOf: (row) => row.request_count || 0,
  },
  quota: {
    list: 'quota_rankings',
    total: 'total_quota',
    metricOf: (row) => row.total_quota || 0,
  },
};

function UserRankings({ period, onRange }) {
  const { t } = useTranslation();
  const [sortKey, setSortKey] = useState('tokens');
  const { loading, data } = useRankingData(
    '/api/user-rankings',
    period,
    onRange,
    '加载用户排行失败',
    pickPayload,
  );

  if (loading) {
    return <ListSkeleton />;
  }

  if (!data) {
    return <Empty description={t('暂无数据')} style={{ padding: 60 }} />;
  }

  // Top-3 concentration hint. The denominator is the summary total over ALL
  // quota_data rows, while the boards only list named users — so this reads
  // "share of the whole site", and the label says so. Hidden when the board
  // has 3 rows or fewer ("top 3 of 3" carries no information).
  const siteShareHint = (key) => {
    const sort = USER_SORTS[key];
    const rows = data[sort.list] || [];
    const total = data.summary?.[sort.total] || 0;
    if (rows.length <= 3 || total <= 0) return undefined;
    const top = rows.slice(0, 3).reduce((sum, r) => sum + sort.metricOf(r), 0);
    return t('Top 3 用户占全站 {{percent}}%', {
      percent: formatSharePct((top / total) * 100),
    });
  };

  const sort = USER_SORTS[sortKey];
  const rows = data[sort.list] || [];
  const siteTotal = data.summary?.[sort.total] || 0;
  const maxMetric = Math.max(...rows.map(sort.metricOf), 0);

  return (
    <div className='rankings-stack'>
      <StatStrip
        items={[
          {
            label: t('总 Tokens'),
            value: formatNumber(data.summary?.total_tokens),
            hint: siteShareHint('tokens'),
          },
          {
            label: t('总请求次数'),
            value: formatNumber(data.summary?.total_requests),
            hint: siteShareHint('requests'),
          },
          {
            label: t('总消费额度'),
            value: renderQuota(data.summary?.total_quota || 0),
            hint: siteShareHint('quota'),
          },
        ]}
      />

      <RankTable
        title={t('修士榜')}
        sorts={[
          { key: 'tokens', label: 'Tokens' },
          { key: 'requests', label: t('请求次数') },
          { key: 'quota', label: t('消费额度') },
        ]}
        sortKey={sortKey}
        onSort={setSortKey}
        nameLabel={t('用户')}
        columns={[
          {
            key: 'tokens',
            sortKey: 'tokens',
            label: 'Tokens',
            priority: 2,
            render: (u) => formatNumber(u.total_tokens),
          },
          {
            key: 'requests',
            sortKey: 'requests',
            label: t('请求次数'),
            priority: 2,
            render: (u) => formatNumber(u.request_count),
          },
          {
            key: 'quota',
            sortKey: 'quota',
            label: t('消费额度'),
            priority: 2,
            render: (u) => renderQuota(u.total_quota || 0),
          },
          {
            key: 'share',
            label: t('占比'),
            priority: 3,
            render: (u) =>
              siteTotal > 0 ? formatPercent(sort.metricOf(u) / siteTotal) : '-',
          },
        ]}
        rows={rows}
        // Rows are grouped by (user_id, username), so a user who renamed
        // inside the window appears twice — key on both.
        rowKey={(u) => `${u.user_id}-${u.username}`}
        identity={(u) => ({
          icon: <UserAvatar name={u.username} />,
          name: u.username,
        })}
        barOf={(u) =>
          maxMetric > 0 ? (sort.metricOf(u) / maxMetric) * 100 : null
        }
      />
    </div>
  );
}

// ============================================================================
// Tab 3: Model performance — success rate, output speed, latency
// ============================================================================

// Only degraded readings are colored. A board where every healthy row is green
// reads as decoration, and the one red row stops standing out.
function successRateTone(rate) {
  if (rate >= 95) return undefined;
  if (rate >= 80) return 'is-warn';
  return 'is-bad';
}

// A first token within a second is normal for hosted models; flagging
// everything above half a second turned nearly every row amber.
function latencyTone(ms) {
  if (ms <= 1000) return undefined;
  if (ms <= 3000) return 'is-warn';
  return 'is-bad';
}

// A single lucky request must not outrank a model with thousands of them, so
// every performance ordering requires a minimum sample size.
const MIN_PERF_SAMPLES = 5;
const PERF_BOARD_LIMIT = 30;

/**
 * Time-to-first-token is only observable on streaming requests. Models that are
 * only ever called non-streaming have no TTFT at all, so fall back to overall
 * latency and label it, instead of dropping those models from the board.
 */
function latencyMetric(row) {
  if ((row.ttft_sample_count || 0) > 0 && row.avg_ttft_ms > 0) {
    return { value: row.avg_ttft_ms, isTtft: true };
  }
  if (row.avg_latency_ms > 0) {
    return { value: row.avg_latency_ms, isTtft: false };
  }
  return null;
}

// Each ordering keeps only the models that actually report its metric, and
// the trend column follows the metric the board is sorted by.
const PERF_SORTS = {
  success: {
    keep: (m) => typeof m.success_rate === 'number',
    compare: (a, b) => b.success_rate - a.success_rate,
    spark: () => ({ metric: 'success_rate', allowZero: true }),
  },
  tps: {
    keep: (m) => m.avg_tps > 0,
    compare: (a, b) => b.avg_tps - a.avg_tps,
    spark: () => ({ metric: 'avg_tps' }),
  },
  latency: {
    keep: (m) => m.latency !== null,
    compare: (a, b) => a.latency.value - b.latency.value,
    spark: (m) => ({
      metric: m.latency?.isTtft ? 'avg_ttft_ms' : 'avg_latency_ms',
    }),
  },
};

function ModelRankings({ period, onRange }) {
  const { t } = useTranslation();
  const [sortKey, setSortKey] = useState('success');
  const { loading, data: models } = useRankingData(
    '/api/perf-metrics/summary',
    period,
    onRange,
    '加载模型性能数据失败',
    pickPerfModels,
  );

  const sampled = useMemo(
    () =>
      (models || [])
        .filter((m) => (m.request_count || 0) >= MIN_PERF_SAMPLES)
        .map((m) => ({ ...m, latency: latencyMetric(m) })),
    [models],
  );

  const rows = useMemo(() => {
    const sort = PERF_SORTS[sortKey];
    return sampled
      .filter(sort.keep)
      .sort(sort.compare)
      .slice(0, PERF_BOARD_LIMIT);
  }, [sampled, sortKey]);

  if (loading) {
    return <ListSkeleton rows={8} />;
  }

  if (sampled.length === 0) {
    return (
      <Empty
        description={
          (models || []).length > 0
            ? t('暂无满足最小样本量的模型')
            : t('暂无性能数据')
        }
        style={{ padding: 60 }}
      />
    );
  }

  return (
    <div className='rankings-stack'>
      <RankTable
        title={t('斗法榜')}
        note={t('仅统计请求数不少于 {{count}} 次的模型', {
          count: MIN_PERF_SAMPLES,
        })}
        sorts={[
          { key: 'success', label: t('成功率') },
          { key: 'tps', label: 'TPS' },
          { key: 'latency', label: t('延迟') },
        ]}
        sortKey={sortKey}
        onSort={setSortKey}
        nameLabel={t('模型')}
        columns={[
          {
            key: 'trend',
            label: t('走势'),
            numeric: false,
            priority: 3,
            render: (m) => (
              <Sparkline
                series={m.recent_series}
                {...PERF_SORTS[sortKey].spark(m)}
              />
            ),
          },
          {
            key: 'success',
            sortKey: 'success',
            label: t('成功率'),
            priority: 2,
            render: (m) =>
              typeof m.success_rate === 'number' ? (
                <span className={successRateTone(m.success_rate)}>
                  {m.success_rate.toFixed(1)}%
                </span>
              ) : (
                '-'
              ),
          },
          {
            key: 'tps',
            sortKey: 'tps',
            label: 'TPS',
            priority: 2,
            render: (m) =>
              m.avg_tps > 0 ? `${m.avg_tps.toFixed(1)} t/s` : '-',
          },
          {
            key: 'latency',
            sortKey: 'latency',
            label: t('延迟'),
            priority: 2,
            render: (m) =>
              m.latency ? (
                <span className={latencyTone(m.latency.value)}>
                  {formatLatency(m.latency.value)}
                  <span className='rankings-cell-note'>
                    {m.latency.isTtft ? t('首字延迟') : t('总延迟')}
                  </span>
                </span>
              ) : (
                '-'
              ),
          },
        ]}
        rows={rows}
        rowKey={(m) => m.model_name}
        identity={(m) => ({
          name: m.model_name,
          sub: `${formatNumber(m.request_count || 0)} ${t('次请求')}${
            m.tps_from_stream === false ? ` · ${t('非流式')}` : ''
          }`,
        })}
      />
    </div>
  );
}

// ============================================================================
// Main Rankings Page
// ============================================================================

const PERIODS = [
  { value: 'today', label: '今日' },
  { value: 'week', label: '本周' },
  { value: 'month', label: '本月' },
  { value: 'year', label: '今年' },
];

const TABS = [
  { key: 'llm', label: '用量排行', Panel: LLMRankings },
  { key: 'users', label: '用户排行', Panel: UserRankings },
  { key: 'models', label: '性能排行', Panel: ModelRankings },
];

const Rankings = () => {
  const { t } = useTranslation();
  const [period, setPeriod] = useState('week');
  const [range, setRange] = useState(null);
  const [tab, setTab] = useState('llm');
  // A tab stays mounted once opened, so switching back does not refetch or
  // flash a skeleton.
  const [opened, setOpened] = useState(['llm']);

  useEffect(() => {
    initVChartSemiTheme({ isWatchingThemeSwitch: true });
  }, []);

  // Every tab resolves the same calendar window server-side, so whichever one
  // is active can report it; clear it first so a stale range is never shown
  // next to a freshly picked period.
  const handlePeriodChange = useCallback((next) => {
    setRange(null);
    setPeriod(next);
  }, []);

  const handleRange = useCallback((next) => {
    if (!next?.start || !next?.end) return;
    setRange(next);
  }, []);

  const openTab = (key) => {
    setTab(key);
    setOpened((previous) =>
      previous.includes(key) ? previous : [...previous, key],
    );
  };

  return (
    <div className='rankings-page'>
      <div className='rankings-container'>
        <header className='rankings-banner'>
          <BannerArt />
          <div className='rankings-banner-text'>
            <h1 className='rankings-title'>{t('排行榜')}</h1>
            <p className='rankings-tagline'>{t('万模论道，以修为定高下')}</p>
            {/* Always rendered so the line does not jump in after loading. */}
            <div className='rankings-range'>
              {range && `${t('统计区间')}：${formatRange(range)}`}
            </div>
          </div>
          <div
            className='rankings-period'
            role='tablist'
            aria-label={t('排行周期')}
          >
            {PERIODS.map((p) => (
              <button
                type='button'
                key={p.value}
                role='tab'
                aria-selected={period === p.value}
                onClick={() => handlePeriodChange(p.value)}
                className={`rankings-period-btn${period === p.value ? ' is-active' : ''}`}
              >
                {t(p.label)}
              </button>
            ))}
          </div>
        </header>

        <div className='rankings-tabs' role='tablist'>
          {TABS.map(({ key, label }) => (
            <button
              type='button'
              key={key}
              role='tab'
              id={`rankings-tab-${key}`}
              aria-selected={tab === key}
              aria-controls={`rankings-panel-${key}`}
              className={`rankings-tab${tab === key ? ' is-active' : ''}`}
              onClick={() => openTab(key)}
            >
              {t(label)}
            </button>
          ))}
        </div>

        {TABS.filter(({ key }) => opened.includes(key)).map(
          ({ key, Panel }) => (
            <div
              key={key}
              id={`rankings-panel-${key}`}
              role='tabpanel'
              aria-labelledby={`rankings-tab-${key}`}
              hidden={tab !== key}
            >
              <Panel period={period} onRange={handleRange} />
            </div>
          ),
        )}
      </div>
    </div>
  );
};

export default Rankings;
