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
  Spin,
  Typography,
  Progress,
  Empty,
  Avatar,
} from '@douyinfe/semi-ui';
import {
  IconArrowUp,
  IconArrowDown,
  IconStar,
  IconUser,
  IconActivity,
} from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../helpers';

const { Title, Text } = Typography;

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

// ============================================================================
// Rank Badge — gold/silver/bronze for top 3
// ============================================================================

const RANK_STYLES = {
  1: {
    background: 'linear-gradient(135deg, #ffd700 0%, #ffb800 100%)',
    color: '#fff',
    boxShadow: '0 2px 8px rgba(255, 184, 0, 0.4)',
  },
  2: {
    background: 'linear-gradient(135deg, #c0c0c0 0%, #a8a8a8 100%)',
    color: '#fff',
    boxShadow: '0 2px 8px rgba(168, 168, 168, 0.4)',
  },
  3: {
    background: 'linear-gradient(135deg, #cd7f32 0%, #b87333 100%)',
    color: '#fff',
    boxShadow: '0 2px 8px rgba(205, 127, 50, 0.4)',
  },
};

function RankBadge({ rank }) {
  const style = RANK_STYLES[rank];
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: 26,
        height: 26,
        borderRadius: '50%',
        fontSize: 12,
        fontWeight: 700,
        fontFamily: 'monospace',
        ...(style || {
          background: 'var(--semi-color-fill-1)',
          color: 'var(--semi-color-text-2)',
        }),
      }}
    >
      {rank}
    </span>
  );
}

// ============================================================================
// Stat Card with gradient accent
// ============================================================================

function StatCard({ icon, label, value, gradient }) {
  return (
    <div
      style={{
        position: 'relative',
        overflow: 'hidden',
        borderRadius: 12,
        padding: '20px 24px',
        background: 'var(--semi-color-bg-1)',
        border: '1px solid var(--semi-color-border)',
        flex: 1,
        minWidth: 160,
      }}
    >
      <div
        style={{
          position: 'absolute',
          top: 0,
          left: 0,
          right: 0,
          height: 3,
          background: gradient || 'var(--semi-color-primary)',
        }}
      />
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
        {icon && (
          <span style={{ fontSize: 16, opacity: 0.7 }}>{icon}</span>
        )}
        <Text type='tertiary' size='small' style={{ fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.5px' }}>
          {label}
        </Text>
      </div>
      <div
        style={{
          fontSize: 28,
          fontWeight: 700,
          fontFamily: "'JetBrains Mono', 'SF Mono', 'Fira Code', monospace",
          color: 'var(--semi-color-text-0)',
          lineHeight: 1.2,
        }}
      >
        {value}
      </div>
    </div>
  );
}

// ============================================================================
// Section Card with icon header
// ============================================================================

function SectionCard({ icon, title, subtitle, children, style }) {
  return (
    <div
      style={{
        borderRadius: 12,
        border: '1px solid var(--semi-color-border)',
        background: 'var(--semi-color-bg-1)',
        overflow: 'hidden',
        ...style,
      }}
    >
      <div
        style={{
          padding: '16px 20px',
          borderBottom: '1px solid var(--semi-color-border)',
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}
      >
        {icon && (
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 32,
              height: 32,
              borderRadius: 8,
              background: 'var(--semi-color-primary-light-default)',
              color: 'var(--semi-color-primary)',
              fontSize: 16,
            }}
          >
            {icon}
          </span>
        )}
        <div>
          <div style={{ fontWeight: 600, fontSize: 15, color: 'var(--semi-color-text-0)' }}>
            {title}
          </div>
          {subtitle && (
            <div style={{ fontSize: 12, color: 'var(--semi-color-text-2)', marginTop: 2 }}>
              {subtitle}
            </div>
          )}
        </div>
      </div>
      <div style={{ padding: '12px 20px 16px' }}>{children}</div>
    </div>
  );
}

// ============================================================================
// Leaderboard Row
// ============================================================================

function LeaderboardRow({ rank, name, sub, value, valueLabel, share, growth }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '10px 4px',
        borderBottom: '1px solid var(--semi-color-fill-0)',
        transition: 'background 0.15s',
      }}
      onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--semi-color-fill-0)')}
      onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
    >
      <RankBadge rank={rank} />
      <div style={{ flex: 1, minWidth: 0 }}>
        <div
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: 'var(--semi-color-text-0)',
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            fontFamily: "'JetBrains Mono', 'SF Mono', monospace",
          }}
        >
          {name}
        </div>
        {sub && (
          <div style={{ fontSize: 11, color: 'var(--semi-color-text-2)', marginTop: 2 }}>
            {sub}
          </div>
        )}
      </div>
      {share !== undefined && (
        <div style={{ width: 80, flexShrink: 0 }}>
          <Progress
            percent={Math.round(share * 100)}
            size='small'
            showInfo={false}
            stroke='var(--semi-color-primary)'
            trailColor='var(--semi-color-fill-1)'
          />
        </div>
      )}
      <div style={{ textAlign: 'right', flexShrink: 0, minWidth: 70 }}>
        <div
          style={{
            fontSize: 13,
            fontWeight: 700,
            fontFamily: "'JetBrains Mono', 'SF Mono', monospace",
            color: 'var(--semi-color-text-0)',
          }}
        >
          {value}
        </div>
        {valueLabel && (
          <div style={{ fontSize: 10, color: 'var(--semi-color-text-2)' }}>{valueLabel}</div>
        )}
      </div>
      {growth !== undefined && (
        <div style={{ flexShrink: 0, width: 60, textAlign: 'right' }}>
          <span
            style={{
              fontSize: 12,
              fontWeight: 600,
              fontFamily: 'monospace',
              color:
                growth > 0
                  ? 'var(--semi-color-success)'
                  : growth < 0
                    ? 'var(--semi-color-danger)'
                    : 'var(--semi-color-text-2)',
            }}
          >
            {formatGrowth(growth)}
          </span>
        </div>
      )}
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
    [data]
  );

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 80 }}>
        <Spin size='large' />
      </div>
    );
  }

  if (!data) {
    return <Empty description={t('暂无数据')} style={{ padding: 60 }} />;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      {/* Stats row */}
      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
        <StatCard
          label={t('总令牌用量')}
          value={formatNumber(totalTokens)}
          gradient='linear-gradient(90deg, #6366f1, #8b5cf6)'
        />
        <StatCard
          label={t('上榜模型')}
          value={data.models?.length || 0}
          gradient='linear-gradient(90deg, #06b6d4, #22d3ee)'
        />
        <StatCard
          label={t('活跃厂商')}
          value={data.vendors?.length || 0}
          gradient='linear-gradient(90deg, #f59e0b, #fbbf24)'
        />
      </div>

      {/* Model Leaderboard */}
      <SectionCard
        icon={<IconStar />}
        title={t('模型排行')}
        subtitle={t('按 Token 用量排名的热门模型')}
      >
        {(data.models || []).map((m) => (
          <LeaderboardRow
            key={m.model_name}
            rank={m.rank}
            name={m.model_name}
            sub={m.vendor}
            value={formatNumber(m.total_tokens)}
            valueLabel='tokens'
            share={m.share}
            growth={m.growth_pct}
          />
        ))}
      </SectionCard>

      {/* Vendor Leaderboard */}
      <SectionCard
        icon={<IconActivity />}
        title={t('厂商排行')}
        subtitle={t('按聚合 Token 用量排名的模型厂商')}
      >
        {(data.vendors || []).map((v) => (
          <LeaderboardRow
            key={v.vendor}
            rank={v.rank}
            name={v.vendor}
            sub={`${v.models_count || 0} ${t('个模型')} · ${v.top_model || ''}`}
            value={formatNumber(v.total_tokens)}
            valueLabel={formatPercent(v.share)}
            share={v.share}
          />
        ))}
      </SectionCard>

      {/* Movers & Droppers */}
      {(data.top_movers?.length > 0 || data.top_droppers?.length > 0) && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: 16 }}>
          {data.top_movers?.length > 0 && (
            <SectionCard
              icon={<IconArrowUp style={{ color: 'var(--semi-color-success)' }} />}
              title={t('上升趋势')}
              subtitle={t('排名上升最快的模型')}
            >
              {data.top_movers.map((m) => (
                <div
                  key={m.model_name}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '8px 4px',
                    borderBottom: '1px solid var(--semi-color-fill-0)',
                  }}
                >
                  <div style={{ minWidth: 0, flex: 1 }}>
                    <Text
                      ellipsis
                      style={{ fontSize: 13, fontWeight: 500, fontFamily: 'monospace' }}
                    >
                      {m.model_name}
                    </Text>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
                    <Tag
                      color='green'
                      size='small'
                      shape='circle'
                      style={{ fontWeight: 600, fontFamily: 'monospace' }}
                    >
                      ↑ {Math.abs(m.rank_delta)}
                    </Tag>
                    <Text
                      type='tertiary'
                      size='small'
                      style={{ fontFamily: 'monospace', fontSize: 11 }}
                    >
                      {formatGrowth(m.growth_pct)}
                    </Text>
                  </div>
                </div>
              ))}
            </SectionCard>
          )}
          {data.top_droppers?.length > 0 && (
            <SectionCard
              icon={<IconArrowDown style={{ color: 'var(--semi-color-danger)' }} />}
              title={t('下降趋势')}
              subtitle={t('排名下降最快的模型')}
            >
              {data.top_droppers.map((m) => (
                <div
                  key={m.model_name}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '8px 4px',
                    borderBottom: '1px solid var(--semi-color-fill-0)',
                  }}
                >
                  <div style={{ minWidth: 0, flex: 1 }}>
                    <Text
                      ellipsis
                      style={{ fontSize: 13, fontWeight: 500, fontFamily: 'monospace' }}
                    >
                      {m.model_name}
                    </Text>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
                    <Tag
                      color='red'
                      size='small'
                      shape='circle'
                      style={{ fontWeight: 600, fontFamily: 'monospace' }}
                    >
                      ↓ {Math.abs(m.rank_delta)}
                    </Tag>
                    <Text
                      type='tertiary'
                      size='small'
                      style={{ fontFamily: 'monospace', fontSize: 11 }}
                    >
                      {formatGrowth(m.growth_pct)}
                    </Text>
                  </div>
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
    return (
      <div style={{ textAlign: 'center', padding: 80 }}>
        <Spin size='large' />
      </div>
    );
  }

  if (!data) {
    return <Empty description={t('暂无数据')} style={{ padding: 60 }} />;
  }

  const totalRequests = data.summary?.total_requests || 1;
  const totalQuota = data.summary?.total_quota || 1;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      {/* Stats row */}
      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
        <StatCard
          icon={<IconActivity size='small' />}
          label={t('总请求次数')}
          value={formatNumber(data.summary?.total_requests)}
          gradient='linear-gradient(90deg, #3b82f6, #60a5fa)'
        />
        <StatCard
          icon={<IconStar size='small' />}
          label={t('总额度')}
          value={formatNumber(data.summary?.total_quota)}
          gradient='linear-gradient(90deg, #8b5cf6, #a78bfa)'
        />
        <StatCard
          label={t('总 Tokens')}
          value={formatNumber(data.summary?.total_tokens)}
          gradient='linear-gradient(90deg, #10b981, #34d399)'
        />
      </div>

      {/* Request Rankings */}
      <SectionCard
        icon={<IconUser />}
        title={t('用户调用次数排行')}
        subtitle={t('按 API 请求次数排名')}
      >
        {(data.request_rankings || []).map((u, idx) => (
          <LeaderboardRow
            key={u.user_id}
            rank={idx + 1}
            name={u.username}
            value={formatNumber(u.request_count)}
            valueLabel={t('次请求')}
            share={u.request_count / totalRequests}
          />
        ))}
      </SectionCard>

      {/* Quota Rankings */}
      <SectionCard
        icon={<IconStar />}
        title={t('用户用量排行')}
        subtitle={t('按额度消耗排名')}
      >
        {(data.quota_rankings || []).map((u, idx) => (
          <LeaderboardRow
            key={u.user_id}
            rank={idx + 1}
            name={u.username}
            value={formatNumber(u.total_quota)}
            valueLabel={t('额度')}
            share={u.total_quota / totalQuota}
          />
        ))}
      </SectionCard>
    </div>
  );
}

// ============================================================================
// Tab 3: Model Performance Rankings
// ============================================================================

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
        .filter((m) => m.success_rate !== undefined)
        .sort((a, b) => b.success_rate - a.success_rate)
        .slice(0, 30),
    [models]
  );

  const tpsSorted = useMemo(
    () =>
      [...models]
        .filter((m) => m.avg_tps > 0)
        .sort((a, b) => b.avg_tps - a.avg_tps)
        .slice(0, 30),
    [models]
  );

  const latencySorted = useMemo(
    () =>
      [...models]
        .filter((m) => m.avg_ttft_ms > 0)
        .sort((a, b) => a.avg_ttft_ms - b.avg_ttft_ms)
        .slice(0, 30),
    [models]
  );

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 80 }}>
        <Spin size='large' />
      </div>
    );
  }

  if (models.length === 0) {
    return <Empty description={t('暂无性能数据')} style={{ padding: 60 }} />;
  }

  const maxTps = tpsSorted.length > 0 ? tpsSorted[0].avg_tps : 1;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <Text type='tertiary' style={{ fontSize: 13 }}>
        {t('最近 24 小时内的成功率、TPS 与延迟表现')}
      </Text>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))',
          gap: 16,
        }}
      >
        {/* Success Rate */}
        <SectionCard
          icon={<IconActivity style={{ color: 'var(--semi-color-success)' }} />}
          title={t('成功率排名')}
          subtitle={t('按请求成功率排序')}
        >
          <div style={{ maxHeight: 480, overflowY: 'auto' }}>
            {successSorted.map((m, idx) => (
              <div
                key={m.model_name}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                  padding: '8px 4px',
                  borderBottom: '1px solid var(--semi-color-fill-0)',
                }}
              >
                <RankBadge rank={idx + 1} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <Text
                    ellipsis
                    style={{ fontSize: 12, fontWeight: 500, fontFamily: 'monospace' }}
                  >
                    {m.model_name}
                  </Text>
                </div>
                <Text
                  style={{
                    width: 56,
                    textAlign: 'right',
                    fontSize: 13,
                    fontWeight: 700,
                    fontFamily: 'monospace',
                    flexShrink: 0,
                    color:
                      m.success_rate >= 0.95
                        ? 'var(--semi-color-success)'
                        : m.success_rate >= 0.8
                          ? 'var(--semi-color-warning)'
                          : 'var(--semi-color-danger)',
                  }}
                >
                  {(m.success_rate * 100).toFixed(1)}%
                </Text>
              </div>
            ))}
          </div>
        </SectionCard>

        {/* TPS */}
        <SectionCard
          icon={<IconArrowUp style={{ color: 'var(--semi-color-primary)' }} />}
          title='TPS'
          subtitle={t('按平均输出速度排序 (tokens/s)')}
        >
          <div style={{ maxHeight: 480, overflowY: 'auto' }}>
            {tpsSorted.map((m, idx) => (
              <div
                key={m.model_name}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                  padding: '8px 4px',
                  borderBottom: '1px solid var(--semi-color-fill-0)',
                }}
              >
                <RankBadge rank={idx + 1} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <Text
                    ellipsis
                    style={{ fontSize: 12, fontWeight: 500, fontFamily: 'monospace' }}
                  >
                    {m.model_name}
                  </Text>
                </div>
                <div style={{ width: 80, flexShrink: 0 }}>
                  <Progress
                    percent={Math.round((m.avg_tps / maxTps) * 100)}
                    size='small'
                    showInfo={false}
                    stroke='var(--semi-color-primary)'
                    trailColor='var(--semi-color-fill-1)'
                  />
                </div>
                <Text
                  style={{
                    width: 56,
                    textAlign: 'right',
                    fontSize: 12,
                    fontWeight: 600,
                    fontFamily: 'monospace',
                    flexShrink: 0,
                  }}
                >
                  {m.avg_tps?.toFixed(1)}
                </Text>
              </div>
            ))}
          </div>
        </SectionCard>

        {/* Latency */}
        <SectionCard
          icon={<IconArrowDown style={{ color: 'var(--semi-color-warning)' }} />}
          title={t('延迟排名')}
          subtitle={t('按首字延迟排序 (越低越好)')}
        >
          <div style={{ maxHeight: 480, overflowY: 'auto' }}>
            {latencySorted.map((m, idx) => (
              <div
                key={m.model_name}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                  padding: '8px 4px',
                  borderBottom: '1px solid var(--semi-color-fill-0)',
                }}
              >
                <RankBadge rank={idx + 1} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <Text
                    ellipsis
                    style={{ fontSize: 12, fontWeight: 500, fontFamily: 'monospace' }}
                  >
                    {m.model_name}
                  </Text>
                </div>
                <Text
                  style={{
                    width: 64,
                    textAlign: 'right',
                    fontSize: 13,
                    fontWeight: 700,
                    fontFamily: 'monospace',
                    flexShrink: 0,
                    color:
                      m.avg_ttft_ms <= 500
                        ? 'var(--semi-color-success)'
                        : m.avg_ttft_ms <= 2000
                          ? 'var(--semi-color-warning)'
                          : 'var(--semi-color-danger)',
                  }}
                >
                  {formatLatency(m.avg_ttft_ms)}
                </Text>
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
    <div style={{ position: 'relative', minHeight: '100%', paddingTop: 64 }}>

      <div style={{ position: 'relative', padding: '24px 24px 40px', maxWidth: 1200, margin: '0 auto' }}>
        {/* Hero section */}
        <div style={{ marginBottom: 24 }}>
          <h1
            style={{
              fontSize: 20,
              fontWeight: 600,
              color: 'var(--semi-color-text-0)',
              margin: 0,
              lineHeight: 1.4,
            }}
          >
            {t('排行榜')}
          </h1>
          <p
            style={{
              fontSize: 14,
              color: 'var(--semi-color-text-2)',
              margin: '8px 0 0',
              maxWidth: 560,
            }}
          >
            {t('发现平台上最受欢迎的模型和上升最快的厂商，数据实时更新。')}
          </p>

          {/* Period selector */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 4,
              marginTop: 20,
              borderBottom: '1px solid var(--semi-color-border)',
              paddingBottom: 0,
            }}
          >
            {PERIODS.map((p) => {
              const isActive = period === p.value;
              return (
                <button
                  key={p.value}
                  onClick={() => setPeriod(p.value)}
                  style={{
                    position: 'relative',
                    padding: '8px 16px',
                    fontSize: 14,
                    fontWeight: isActive ? 600 : 400,
                    color: isActive
                      ? 'var(--semi-color-text-0)'
                      : 'var(--semi-color-text-2)',
                    background: 'transparent',
                    border: 'none',
                    cursor: 'pointer',
                    transition: 'color 0.2s',
                    marginBottom: -1,
                  }}
                >
                  {t(p.label)}
                  <span
                    style={{
                      position: 'absolute',
                      left: 12,
                      right: 12,
                      bottom: 0,
                      height: 2,
                      borderRadius: 1,
                      background: 'var(--semi-color-text-0)',
                      opacity: isActive ? 1 : 0,
                      transition: 'opacity 0.2s',
                    }}
                  />
                </button>
              );
            })}
          </div>
        </div>

        {/* Tabs */}
        <Tabs type='button' defaultActiveKey='llm' size='large'>
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
