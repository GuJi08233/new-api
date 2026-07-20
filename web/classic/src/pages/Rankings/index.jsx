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
import {
  Tabs,
  TabPane,
  Tag,
  Typography,
  Empty,
  Skeleton,
} from '@douyinfe/semi-ui';
import {
  IconArrowUp,
  IconArrowDown,
  IconStar,
  IconUser,
  IconActivity,
  IconBolt,
  IconClock,
  IconCrown,
  IconHistogram,
} from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, getLobeHubIcon } from '../../helpers';
import './rankings.css';

const { Text } = Typography;

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
  const sign = value > 0 ? '+' : '';
  return sign + value.toFixed(1) + '%';
}

function formatLatency(ms) {
  if (ms === undefined || ms === null) return '-';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

const MONO_FONT = "'JetBrains Mono', 'SF Mono', 'Fira Code', monospace";

// ============================================================================
// Rank Badge — gold/silver/bronze for top 3
// ============================================================================

function RankBadge({ rank }) {
  const cls = rank >= 1 && rank <= 3 ? ` rank-${rank}` : '';
  return <span className={`rankings-rank-badge${cls}`}>{rank}</span>;
}

// ============================================================================
// Growth pill — colored chip with arrow
// ============================================================================

function GrowthPill({ value }) {
  if (value === undefined || value === null) return null;
  const tone = value > 0 ? 'up' : value < 0 ? 'down' : 'flat';
  const Icon = value > 0 ? IconArrowUp : value < 0 ? IconArrowDown : null;
  return (
    <span className={`rankings-growth ${tone}`}>
      {Icon && <Icon size='extra-small' />}
      {formatGrowth(value)}
    </span>
  );
}

// ============================================================================
// Vendor icon chip (falls back to '?' via getLobeHubIcon)
// ============================================================================

function VendorIcon({ icon }) {
  return (
    <span className='rankings-vendor-icon'>{getLobeHubIcon(icon, 18)}</span>
  );
}

// ============================================================================
// User initial avatar — deterministic gradient by name
// ============================================================================

const AVATAR_GRADIENTS = [
  'linear-gradient(135deg, #6366f1, #8b5cf6)',
  'linear-gradient(135deg, #06b6d4, #3b82f6)',
  'linear-gradient(135deg, #f59e0b, #f97316)',
  'linear-gradient(135deg, #10b981, #059669)',
  'linear-gradient(135deg, #ec4899, #8b5cf6)',
  'linear-gradient(135deg, #f43f5e, #fb923c)',
];

function UserAvatar({ name }) {
  const str = name || '?';
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = (hash * 31 + str.charCodeAt(i)) >>> 0;
  }
  const gradient = AVATAR_GRADIENTS[hash % AVATAR_GRADIENTS.length];
  return (
    <span className='rankings-avatar' style={{ background: gradient }}>
      {str.charAt(0).toUpperCase()}
    </span>
  );
}

// ============================================================================
// Stat Card with gradient icon chip
// ============================================================================

function StatCard({ icon, label, value, gradient }) {
  return (
    <div className='rankings-stat-card'>
      <div className='rankings-stat-glow' style={{ background: gradient }} />
      <div className='rankings-stat-head'>
        {icon && (
          <span className='rankings-stat-icon' style={{ background: gradient }}>
            {icon}
          </span>
        )}
        <span className='rankings-stat-label'>{label}</span>
      </div>
      <div className='rankings-stat-value'>{value}</div>
    </div>
  );
}

// ============================================================================
// Section Card with icon header
// ============================================================================

function SectionCard({ icon, tone, title, subtitle, children, style }) {
  return (
    <div className='rankings-section' style={style}>
      <div className='rankings-section-header'>
        {icon && (
          <span className={`rankings-section-icon ${tone || 'tone-primary'}`}>
            {icon}
          </span>
        )}
        <div>
          <div className='rankings-section-title'>{title}</div>
          {subtitle && (
            <div className='rankings-section-subtitle'>{subtitle}</div>
          )}
        </div>
      </div>
      <div className='rankings-section-body'>{children}</div>
    </div>
  );
}

// ============================================================================
// Leaderboard Row — rank · icon · name/sub/share-bar · value · growth
// ============================================================================

function LeaderboardRow({
  rank,
  icon,
  name,
  sub,
  value,
  valueLabel,
  growth,
  share,
  barGradient,
}) {
  const sharePct =
    share !== undefined && share !== null ? Math.min(share * 100, 100) : null;
  return (
    <div className='rankings-row'>
      <RankBadge rank={rank} />
      {icon}
      <div className='rankings-row-main'>
        <div className='rankings-row-name'>{name}</div>
        {sub && <div className='rankings-row-sub'>{sub}</div>}
        {sharePct !== null && (
          <div className='rankings-share-bar'>
            <div
              className='rankings-share-bar-fill'
              style={{
                width: `${Math.max(sharePct, 1.5)}%`,
                background: barGradient || 'var(--semi-color-primary)',
              }}
            />
          </div>
        )}
      </div>
      <div className='rankings-row-value'>
        <div className='rankings-row-value-num'>{value}</div>
        {valueLabel && (
          <div className='rankings-row-value-label'>{valueLabel}</div>
        )}
      </div>
      {growth !== undefined && growth !== null && <GrowthPill value={growth} />}
    </div>
  );
}

// ============================================================================
// Loading skeleton
// ============================================================================

function ListSkeleton({ rows = 6 }) {
  return (
    <div className='rankings-section'>
      <div className='rankings-section-body' style={{ paddingTop: 16 }}>
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

// ============================================================================
// Tab 1: LLM Rankings
// ============================================================================

function LLMRankings({ period }) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get(`/api/rankings?period=${period}`);
      if (res.data?.success) {
        setData(res.data.data);
      } else {
        showError(res.data?.message || t('加载失败'));
      }
    } catch (e) {
      showError(t('加载排行榜失败'));
    } finally {
      setLoading(false);
    }
  }, [period, t]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const totalTokens = useMemo(
    () => data?.models?.reduce((sum, m) => sum + (m.total_tokens || 0), 0) || 0,
    [data],
  );

  if (loading) {
    return <ListSkeleton />;
  }

  if (!data) {
    return <Empty description={t('暂无数据')} style={{ padding: 60 }} />;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      {/* Stats row */}
      <div className='rankings-stats rankings-fade-in'>
        <StatCard
          icon={<IconHistogram size='small' />}
          label={t('总令牌用量')}
          value={formatNumber(totalTokens)}
          gradient='linear-gradient(135deg, #6366f1, #8b5cf6)'
        />
        <StatCard
          icon={<IconStar size='small' />}
          label={t('上榜模型')}
          value={data.models?.length || 0}
          gradient='linear-gradient(135deg, #06b6d4, #3b82f6)'
        />
        <StatCard
          icon={<IconBolt size='small' />}
          label={t('活跃厂商')}
          value={data.vendors?.length || 0}
          gradient='linear-gradient(135deg, #f59e0b, #f97316)'
        />
      </div>

      {/* Model Leaderboard */}
      <div className='rankings-fade-in delay-1'>
        <SectionCard
          icon={<IconCrown />}
          tone='tone-amber'
          title={t('模型排行')}
          subtitle={t('按 Token 用量排名的热门模型')}
        >
          {(data.models || []).map((m) => (
            <LeaderboardRow
              key={m.model_name}
              rank={m.rank}
              icon={<VendorIcon icon={m.vendor_icon} />}
              name={m.model_name}
              sub={m.vendor}
              value={formatNumber(m.total_tokens)}
              valueLabel='tokens'
              growth={m.growth_pct}
              share={m.share}
              barGradient='linear-gradient(90deg, #6366f1, #8b5cf6)'
            />
          ))}
        </SectionCard>
      </div>

      {/* Vendor Leaderboard */}
      <div className='rankings-fade-in delay-2'>
        <SectionCard
          icon={<IconActivity />}
          tone='tone-primary'
          title={t('厂商排行')}
          subtitle={t('按聚合 Token 用量排名的模型厂商')}
        >
          {(data.vendors || []).map((v) => (
            <LeaderboardRow
              key={v.vendor}
              rank={v.rank}
              icon={<VendorIcon icon={v.vendor_icon} />}
              name={v.vendor}
              sub={`${v.models_count || 0} ${t('个模型')} · ${v.top_model || ''}`}
              value={formatNumber(v.total_tokens)}
              valueLabel={formatPercent(v.share)}
              growth={v.growth_pct}
              share={v.share}
              barGradient='linear-gradient(90deg, #06b6d4, #3b82f6)'
            />
          ))}
        </SectionCard>
      </div>

      {/* Movers & Droppers */}
      {(data.top_movers?.length > 0 || data.top_droppers?.length > 0) && (
        <div className='rankings-grid-2 rankings-fade-in delay-2'>
          {data.top_movers?.length > 0 && (
            <SectionCard
              icon={<IconArrowUp />}
              tone='tone-green'
              title={t('上升趋势')}
              subtitle={t('排名上升最快的模型')}
            >
              {data.top_movers.map((m) => (
                <div className='rankings-row' key={m.model_name}>
                  <VendorIcon icon={m.vendor_icon} />
                  <div className='rankings-row-main'>
                    <div className='rankings-row-name'>{m.model_name}</div>
                    {m.vendor && (
                      <div className='rankings-row-sub'>{m.vendor}</div>
                    )}
                  </div>
                  <Tag
                    color='green'
                    size='small'
                    shape='circle'
                    style={{
                      fontWeight: 600,
                      fontFamily: MONO_FONT,
                      flexShrink: 0,
                    }}
                  >
                    ↑ {Math.abs(m.rank_delta)}
                  </Tag>
                  <GrowthPill value={m.growth_pct} />
                </div>
              ))}
            </SectionCard>
          )}
          {data.top_droppers?.length > 0 && (
            <SectionCard
              icon={<IconArrowDown />}
              tone='tone-red'
              title={t('下降趋势')}
              subtitle={t('排名下降最快的模型')}
            >
              {data.top_droppers.map((m) => (
                <div className='rankings-row' key={m.model_name}>
                  <VendorIcon icon={m.vendor_icon} />
                  <div className='rankings-row-main'>
                    <div className='rankings-row-name'>{m.model_name}</div>
                    {m.vendor && (
                      <div className='rankings-row-sub'>{m.vendor}</div>
                    )}
                  </div>
                  <Tag
                    color='red'
                    size='small'
                    shape='circle'
                    style={{
                      fontWeight: 600,
                      fontFamily: MONO_FONT,
                      flexShrink: 0,
                    }}
                  >
                    ↓ {Math.abs(m.rank_delta)}
                  </Tag>
                  <GrowthPill value={m.growth_pct} />
                </div>
              ))}
            </SectionCard>
          )}
        </div>
      )}
    </div>
  );
}

// ============================================================================
// Tab 2: User Rankings
// ============================================================================

function UserRankings({ period }) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get(`/api/user-rankings?period=${period}`);
      if (res.data?.success) {
        setData(res.data.data);
      } else {
        showError(res.data?.message || t('加载失败'));
      }
    } catch (e) {
      showError(t('加载用户排行失败'));
    } finally {
      setLoading(false);
    }
  }, [period, t]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  if (loading) {
    return <ListSkeleton />;
  }

  if (!data) {
    return <Empty description={t('暂无数据')} style={{ padding: 60 }} />;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      {/* Stats row */}
      <div className='rankings-stats rankings-fade-in'>
        <StatCard
          icon={<IconActivity size='small' />}
          label={t('总请求次数')}
          value={formatNumber(data.summary?.total_requests)}
          gradient='linear-gradient(135deg, #3b82f6, #60a5fa)'
        />
        <StatCard
          icon={<IconStar size='small' />}
          label={t('总额度')}
          value={formatNumber(data.summary?.total_quota)}
          gradient='linear-gradient(135deg, #8b5cf6, #a78bfa)'
        />
        <StatCard
          icon={<IconHistogram size='small' />}
          label={t('总 Tokens')}
          value={formatNumber(data.summary?.total_tokens)}
          gradient='linear-gradient(135deg, #10b981, #34d399)'
        />
      </div>

      {/* Request Rankings */}
      <div className='rankings-fade-in delay-1'>
        <SectionCard
          icon={<IconUser />}
          tone='tone-primary'
          title={t('用户调用次数排行')}
          subtitle={t('按 API 请求次数排名')}
        >
          {(data.request_rankings || []).map((u, idx) => (
            <LeaderboardRow
              key={u.user_id}
              rank={idx + 1}
              icon={<UserAvatar name={u.username} />}
              name={u.username}
              value={formatNumber(u.request_count)}
              valueLabel={t('次请求')}
            />
          ))}
        </SectionCard>
      </div>

      {/* Quota Rankings */}
      <div className='rankings-fade-in delay-2'>
        <SectionCard
          icon={<IconStar />}
          tone='tone-violet'
          title={t('用户用量排行')}
          subtitle={t('按额度消耗排名')}
        >
          {(data.quota_rankings || []).map((u, idx) => (
            <LeaderboardRow
              key={u.user_id}
              rank={idx + 1}
              icon={<UserAvatar name={u.username} />}
              name={u.username}
              value={formatNumber(u.total_quota)}
              valueLabel={t('额度')}
            />
          ))}
        </SectionCard>
      </div>
    </div>
  );
}

// ============================================================================
// Tab 3: Model Performance Rankings
// ============================================================================

function successRateColor(rate) {
  if (rate >= 95) return 'var(--semi-color-success)';
  if (rate >= 80) return 'var(--semi-color-warning)';
  return 'var(--semi-color-danger)';
}

function latencyColor(ms) {
  if (ms <= 500) return 'var(--semi-color-success)';
  if (ms <= 2000) return 'var(--semi-color-warning)';
  return 'var(--semi-color-danger)';
}

function ModelRankings() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [models, setModels] = useState([]);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/perf-metrics/summary?hours=24');
      if (res.data?.success && Array.isArray(res.data.data?.models)) {
        setModels(res.data.data.models);
      } else {
        showError(res.data?.message || t('加载失败'));
      }
    } catch (e) {
      showError(t('加载模型性能数据失败'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const successSorted = useMemo(
    () =>
      [...models]
        .filter(
          (m) => m.success_rate !== undefined && (m.request_count || 0) >= 5,
        )
        .sort((a, b) => b.success_rate - a.success_rate)
        .slice(0, 30),
    [models],
  );

  const tpsSorted = useMemo(
    () =>
      [...models]
        .filter((m) => m.avg_tps > 0)
        .sort((a, b) => b.avg_tps - a.avg_tps)
        .slice(0, 30),
    [models],
  );

  const latencySorted = useMemo(
    () =>
      [...models]
        .filter((m) => m.avg_ttft_ms > 0)
        .sort((a, b) => a.avg_ttft_ms - b.avg_ttft_ms)
        .slice(0, 30),
    [models],
  );

  const maxTps = useMemo(
    () => Math.max(...tpsSorted.map((m) => m.avg_tps || 0), 0),
    [tpsSorted],
  );

  const maxLatency = useMemo(
    () => Math.max(...latencySorted.map((m) => m.avg_ttft_ms || 0), 0),
    [latencySorted],
  );

  if (loading) {
    return <ListSkeleton rows={8} />;
  }

  if (models.length === 0) {
    return <Empty description={t('暂无性能数据')} style={{ padding: 60 }} />;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <Text type='tertiary' style={{ fontSize: 13 }}>
        {t('最近 24 小时内的成功率、TPS 与延迟表现')}
      </Text>

      <div className='rankings-grid-3 rankings-fade-in'>
        {/* Success Rate */}
        <SectionCard
          icon={<IconActivity />}
          tone='tone-green'
          title={t('成功率排名')}
          subtitle={t('按请求成功率排序（≥5 次请求）')}
        >
          <div className='rankings-scroll'>
            {successSorted.map((m, idx) => (
              <div className='rankings-row' key={m.model_name}>
                <RankBadge rank={idx + 1} />
                <div className='rankings-row-main'>
                  <div className='rankings-row-name' style={{ fontSize: 12 }}>
                    {m.model_name}
                  </div>
                  <div className='rankings-row-sub'>
                    {m.request_count || 0} {t('次请求')}
                  </div>
                  <div className='rankings-perf-bar'>
                    <div
                      className='rankings-perf-bar-fill'
                      style={{
                        width: `${Math.min(m.success_rate || 0, 100)}%`,
                        background: successRateColor(m.success_rate),
                      }}
                    />
                  </div>
                </div>
                <span
                  className='rankings-perf-value'
                  style={{ width: 56, color: successRateColor(m.success_rate) }}
                >
                  {m.success_rate.toFixed(1)}%
                </span>
              </div>
            ))}
          </div>
        </SectionCard>

        {/* TPS */}
        <SectionCard
          icon={<IconBolt />}
          tone='tone-primary'
          title='TPS'
          subtitle={t('按平均输出速度排序 (t/s)')}
        >
          <div className='rankings-scroll'>
            {tpsSorted.map((m, idx) => (
              <div className='rankings-row' key={m.model_name}>
                <RankBadge rank={idx + 1} />
                <div className='rankings-row-main'>
                  <div className='rankings-row-name' style={{ fontSize: 12 }}>
                    {m.model_name}
                  </div>
                  <div className='rankings-perf-bar'>
                    <div
                      className='rankings-perf-bar-fill'
                      style={{
                        width: `${maxTps > 0 ? (m.avg_tps / maxTps) * 100 : 0}%`,
                        background: 'linear-gradient(90deg, #6366f1, #8b5cf6)',
                      }}
                    />
                  </div>
                </div>
                <span className='rankings-perf-value' style={{ width: 64 }}>
                  {m.avg_tps?.toFixed(1)} t/s
                </span>
              </div>
            ))}
          </div>
        </SectionCard>

        {/* Latency */}
        <SectionCard
          icon={<IconClock />}
          tone='tone-amber'
          title={t('延迟排名')}
          subtitle={t('按首字延迟排序 (越低越好)')}
        >
          <div className='rankings-scroll'>
            {latencySorted.map((m, idx) => (
              <div className='rankings-row' key={m.model_name}>
                <RankBadge rank={idx + 1} />
                <div className='rankings-row-main'>
                  <div className='rankings-row-name' style={{ fontSize: 12 }}>
                    {m.model_name}
                  </div>
                  <div className='rankings-perf-bar'>
                    <div
                      className='rankings-perf-bar-fill'
                      style={{
                        width: `${maxLatency > 0 ? (m.avg_ttft_ms / maxLatency) * 100 : 0}%`,
                        background: latencyColor(m.avg_ttft_ms),
                      }}
                    />
                  </div>
                </div>
                <span
                  className='rankings-perf-value'
                  style={{ width: 64, color: latencyColor(m.avg_ttft_ms) }}
                >
                  {formatLatency(m.avg_ttft_ms)}
                </span>
              </div>
            ))}
          </div>
        </SectionCard>
      </div>
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

const Rankings = () => {
  const { t } = useTranslation();
  const [period, setPeriod] = useState('week');

  return (
    <div className='rankings-page'>
      <div className='rankings-glow' aria-hidden />

      <div className='rankings-container'>
        {/* Hero section */}
        <div className='rankings-hero'>
          <div className='rankings-hero-head'>
            <span className='rankings-hero-icon'>
              <IconCrown />
            </span>
            <h1 className='rankings-hero-title'>{t('排行榜')}</h1>
          </div>
          <p className='rankings-hero-sub'>
            {t('发现平台上最受欢迎的模型和上升最快的厂商，数据实时更新。')}
          </p>

          {/* Period selector */}
          <div
            className='rankings-period'
            role='tablist'
            aria-label={t('排行周期')}
          >
            {PERIODS.map((p) => (
              <button
                key={p.value}
                role='tab'
                aria-selected={period === p.value}
                onClick={() => setPeriod(p.value)}
                className={`rankings-period-btn${period === p.value ? ' active' : ''}`}
              >
                {t(p.label)}
              </button>
            ))}
          </div>
        </div>

        {/* Tabs */}
        <Tabs
          type='button'
          defaultActiveKey='llm'
          size='large'
          className='rankings-tabs'
        >
          <TabPane tab={t('用量排行')} itemKey='llm'>
            <div style={{ paddingTop: 20 }}>
              <LLMRankings period={period} />
            </div>
          </TabPane>
          <TabPane tab={t('用户排行')} itemKey='users'>
            <div style={{ paddingTop: 20 }}>
              <UserRankings period={period} />
            </div>
          </TabPane>
          <TabPane tab={t('性能排行')} itemKey='models'>
            <div style={{ paddingTop: 20 }}>
              <ModelRankings />
            </div>
          </TabPane>
        </Tabs>
      </div>
    </div>
  );
};

export default Rankings;
