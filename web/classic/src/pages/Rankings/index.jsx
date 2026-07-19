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

import React, { useEffect, useState, useCallback } from 'react';
import {
  Card,
  Tabs,
  TabPane,
  Table,
  Tag,
  RadioGroup,
  Radio,
  Spin,
  Typography,
  Row,
  Col,
  Progress,
  Space,
  Empty,
} from '@douyinfe/semi-ui';
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

// ============================================================================
// Stat Card
// ============================================================================

function StatCard({ label, value, sub }) {
  return (
    <Card
      bodyStyle={{ padding: '16px 20px' }}
      style={{ borderRadius: 8, height: '100%' }}
    >
      <Text type='tertiary' size='small' style={{ display: 'block' }}>
        {label}
      </Text>
      <Title heading={4} style={{ margin: '4px 0 0' }}>
        {value}
      </Title>
      {sub && (
        <Text type='tertiary' size='small'>
          {sub}
        </Text>
      )}
    </Card>
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

  const totalTokens = data?.models?.reduce(
    (sum, m) => sum + (m.total_tokens || 0),
    0
  );

  const modelColumns = [
    {
      title: t('排名'),
      dataIndex: 'rank',
      width: 60,
      render: (text, record) => (
        <Tag
          color={text <= 3 ? 'blue' : 'grey'}
          shape='circle'
          size='small'
        >
          {text}
        </Tag>
      ),
    },
    {
      title: t('模型'),
      dataIndex: 'model_name',
      ellipsis: true,
      render: (text, record) => (
        <div>
          <Text strong style={{ fontSize: 13 }}>
            {text}
          </Text>
          <br />
          <Text type='tertiary' size='small'>
            {record.vendor}
          </Text>
        </div>
      ),
    },
    {
      title: t('Token 用量'),
      dataIndex: 'total_tokens',
      width: 120,
      sorter: (a, b) => a.total_tokens - b.total_tokens,
      render: (v) => formatNumber(v),
    },
    {
      title: t('份额'),
      dataIndex: 'share',
      width: 100,
      render: (v) => formatPercent(v),
    },
    {
      title: t('增长'),
      dataIndex: 'growth_pct',
      width: 100,
      render: (v) => (
        <Text type={v > 0 ? 'success' : v < 0 ? 'danger' : 'tertiary'}>
          {formatGrowth(v)}
        </Text>
      ),
    },
  ];

  const vendorColumns = [
    {
      title: t('排名'),
      dataIndex: 'rank',
      width: 60,
      render: (text) => (
        <Tag
          color={text <= 3 ? 'blue' : 'grey'}
          shape='circle'
          size='small'
        >
          {text}
        </Tag>
      ),
    },
    {
      title: t('厂商'),
      dataIndex: 'vendor',
      ellipsis: true,
    },
    {
      title: t('Token 用量'),
      dataIndex: 'total_tokens',
      width: 120,
      render: (v) => formatNumber(v),
    },
    {
      title: t('份额'),
      dataIndex: 'share',
      width: 100,
      render: (v) => formatPercent(v),
    },
    {
      title: t('模型数'),
      dataIndex: 'models_count',
      width: 80,
    },
    {
      title: t('热门模型'),
      dataIndex: 'top_model',
      ellipsis: true,
    },
  ];

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 60 }}>
        <Spin size='large' />
      </div>
    );
  }

  if (!data) {
    return <Empty description={t('暂无数据')} />;
  }

  return (
    <div>
      <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
        <Col xs={24} sm={8}>
          <StatCard label={t('总令牌用量')} value={formatNumber(totalTokens)} />
        </Col>
        <Col xs={24} sm={8}>
          <StatCard
            label={t('上榜模型')}
            value={data.models?.length || 0}
          />
        </Col>
        <Col xs={24} sm={8}>
          <StatCard
            label={t('活跃厂商')}
            value={data.vendors?.length || 0}
          />
        </Col>
      </Row>

      <Card
        title={t('模型排行')}
        style={{ marginBottom: 16 }}
        bodyStyle={{ padding: 0 }}
      >
        <Table
          columns={modelColumns}
          dataSource={data.models || []}
          rowKey='model_name'
          pagination={false}
          size='small'
        />
      </Card>

      <Card
        title={t('厂商排行')}
        style={{ marginBottom: 16 }}
        bodyStyle={{ padding: 0 }}
      >
        <Table
          columns={vendorColumns}
          dataSource={data.vendors || []}
          rowKey='vendor'
          pagination={false}
          size='small'
        />
      </Card>

      {(data.top_movers?.length > 0 || data.top_droppers?.length > 0) && (
        <Row gutter={[16, 16]}>
          {data.top_movers?.length > 0 && (
            <Col xs={24} md={12}>
              <Card title={t('上升趋势')} bodyStyle={{ padding: '12px 16px' }}>
                {data.top_movers.map((m, i) => (
                  <div
                    key={m.model_name}
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      padding: '6px 0',
                      borderBottom:
                        i < data.top_movers.length - 1
                          ? '1px solid var(--semi-color-border)'
                          : 'none',
                    }}
                  >
                    <Text ellipsis style={{ maxWidth: '60%' }}>
                      {m.model_name}
                    </Text>
                    <Space>
                      <Tag color='green' size='small'>
                        +{m.rank_delta}
                      </Tag>
                      <Text type='tertiary' size='small'>
                        {formatGrowth(m.growth_pct)}
                      </Text>
                    </Space>
                  </div>
                ))}
              </Card>
            </Col>
          )}
          {data.top_droppers?.length > 0 && (
            <Col xs={24} md={12}>
              <Card title={t('下降趋势')} bodyStyle={{ padding: '12px 16px' }}>
                {data.top_droppers.map((m, i) => (
                  <div
                    key={m.model_name}
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      padding: '6px 0',
                      borderBottom:
                        i < data.top_droppers.length - 1
                          ? '1px solid var(--semi-color-border)'
                          : 'none',
                    }}
                  >
                    <Text ellipsis style={{ maxWidth: '60%' }}>
                      {m.model_name}
                    </Text>
                    <Space>
                      <Tag color='red' size='small'>
                        {m.rank_delta}
                      </Tag>
                      <Text type='tertiary' size='small'>
                        {formatGrowth(m.growth_pct)}
                      </Text>
                    </Space>
                  </div>
                ))}
              </Card>
            </Col>
          )}
        </Row>
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

  const requestColumns = [
    {
      title: t('排名'),
      key: 'rank',
      width: 60,
      render: (_, __, idx) => (
        <Tag
          color={idx < 3 ? 'blue' : 'grey'}
          shape='circle'
          size='small'
        >
          {idx + 1}
        </Tag>
      ),
    },
    {
      title: t('用户'),
      dataIndex: 'username',
      ellipsis: true,
    },
    {
      title: t('请求次数'),
      dataIndex: 'request_count',
      width: 120,
      sorter: (a, b) => a.request_count - b.request_count,
      render: (v) => formatNumber(v),
    },
    {
      title: t('占比'),
      key: 'percent',
      width: 100,
      render: (_, record) => {
        const total = data?.summary?.total_requests || 1;
        return formatPercent(record.request_count / total);
      },
    },
  ];

  const quotaColumns = [
    {
      title: t('排名'),
      key: 'rank',
      width: 60,
      render: (_, __, idx) => (
        <Tag
          color={idx < 3 ? 'blue' : 'grey'}
          shape='circle'
          size='small'
        >
          {idx + 1}
        </Tag>
      ),
    },
    {
      title: t('用户'),
      dataIndex: 'username',
      ellipsis: true,
    },
    {
      title: t('额度消耗'),
      dataIndex: 'total_quota',
      width: 120,
      sorter: (a, b) => a.total_quota - b.total_quota,
      render: (v) => formatNumber(v),
    },
    {
      title: t('占比'),
      key: 'percent',
      width: 100,
      render: (_, record) => {
        const total = data?.summary?.total_quota || 1;
        return formatPercent(record.total_quota / total);
      },
    },
  ];

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 60 }}>
        <Spin size='large' />
      </div>
    );
  }

  if (!data) {
    return <Empty description={t('暂无数据')} />;
  }

  return (
    <div>
      <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
        <Col xs={24} sm={8}>
          <StatCard
            label={t('总请求次数')}
            value={formatNumber(data.summary?.total_requests)}
          />
        </Col>
        <Col xs={24} sm={8}>
          <StatCard
            label={t('总额度')}
            value={formatNumber(data.summary?.total_quota)}
          />
        </Col>
        <Col xs={24} sm={8}>
          <StatCard
            label={t('总 Tokens')}
            value={formatNumber(data.summary?.total_tokens)}
          />
        </Col>
      </Row>

      <Card
        title={t('用户调用次数排行')}
        style={{ marginBottom: 16 }}
        bodyStyle={{ padding: 0 }}
      >
        <Table
          columns={requestColumns}
          dataSource={data.request_rankings || []}
          rowKey='user_id'
          pagination={false}
          size='small'
        />
      </Card>

      <Card title={t('用户用量排行')} bodyStyle={{ padding: 0 }}>
        <Table
          columns={quotaColumns}
          dataSource={data.quota_rankings || []}
          rowKey='user_id'
          pagination={false}
          size='small'
        />
      </Card>
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

  const successSorted = [...models]
    .filter((m) => m.success_rate !== undefined)
    .sort((a, b) => b.success_rate - a.success_rate)
    .slice(0, 50);

  const tpsSorted = [...models]
    .filter((m) => m.avg_tps > 0)
    .sort((a, b) => b.avg_tps - a.avg_tps)
    .slice(0, 50);

  const latencySorted = [...models]
    .filter((m) => m.avg_ttft_ms > 0)
    .sort((a, b) => a.avg_ttft_ms - b.avg_ttft_ms)
    .slice(0, 50);

  const successColumns = [
    {
      title: t('排名'),
      key: 'rank',
      width: 60,
      render: (_, __, idx) => (
        <Tag color={idx < 3 ? 'blue' : 'grey'} shape='circle' size='small'>
          {idx + 1}
        </Tag>
      ),
    },
    {
      title: t('模型'),
      dataIndex: 'model_name',
      ellipsis: true,
    },
    {
      title: t('成功率'),
      dataIndex: 'success_rate',
      width: 120,
      render: (v) => (
        <Progress
          percent={Math.round(v * 100)}
          size='small'
          stroke={v >= 0.95 ? 'var(--semi-color-success)' : v >= 0.8 ? 'var(--semi-color-warning)' : 'var(--semi-color-danger)'}
          style={{ width: 80 }}
        />
      ),
    },
  ];

  const tpsColumns = [
    {
      title: t('排名'),
      key: 'rank',
      width: 60,
      render: (_, __, idx) => (
        <Tag color={idx < 3 ? 'blue' : 'grey'} shape='circle' size='small'>
          {idx + 1}
        </Tag>
      ),
    },
    {
      title: t('模型'),
      dataIndex: 'model_name',
      ellipsis: true,
    },
    {
      title: 'TPS (t/s)',
      dataIndex: 'avg_tps',
      width: 120,
      render: (v) => v?.toFixed(1),
    },
  ];

  const latencyColumns = [
    {
      title: t('排名'),
      key: 'rank',
      width: 60,
      render: (_, __, idx) => (
        <Tag color={idx < 3 ? 'blue' : 'grey'} shape='circle' size='small'>
          {idx + 1}
        </Tag>
      ),
    },
    {
      title: t('模型'),
      dataIndex: 'model_name',
      ellipsis: true,
    },
    {
      title: t('首字延迟'),
      dataIndex: 'avg_ttft_ms',
      width: 120,
      render: (v) => `${v}ms`,
    },
  ];

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 60 }}>
        <Spin size='large' />
      </div>
    );
  }

  if (models.length === 0) {
    return <Empty description={t('暂无性能数据')} />;
  }

  return (
    <div>
      <Text type='tertiary' style={{ display: 'block', marginBottom: 16 }}>
        {t('最近 24 小时内的成功率、TPS 与延迟表现')}
      </Text>
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={8}>
          <Card title={t('成功率排名')} bodyStyle={{ padding: 0 }}>
            <Table
              columns={successColumns}
              dataSource={successSorted}
              rowKey='model_name'
              pagination={false}
              size='small'
              scroll={{ y: 400 }}
            />
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title='TPS' bodyStyle={{ padding: 0 }}>
            <Table
              columns={tpsColumns}
              dataSource={tpsSorted}
              rowKey='model_name'
              pagination={false}
              size='small'
              scroll={{ y: 400 }}
            />
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title={t('延迟排名')} bodyStyle={{ padding: 0 }}>
            <Table
              columns={latencyColumns}
              dataSource={latencySorted}
              rowKey='model_name'
              pagination={false}
              size='small'
              scroll={{ y: 400 }}
            />
          </Card>
        </Col>
      </Row>
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
    <div style={{ padding: '16px 0' }}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginBottom: 16,
          flexWrap: 'wrap',
          gap: 12,
        }}
      >
        <Title heading={3} style={{ margin: 0 }}>
          {t('排行榜')}
        </Title>
        <RadioGroup
          type='button'
          buttonSize='small'
          value={period}
          onChange={(e) => setPeriod(e.target.value)}
        >
          {PERIODS.map((p) => (
            <Radio key={p.value} value={p.value}>
              {t(p.label)}
            </Radio>
          ))}
        </RadioGroup>
      </div>

      <Tabs type='line' defaultActiveKey='llm'>
        <TabPane tab={t('排行榜')} itemKey='llm'>
          <LLMRankings period={period} />
        </TabPane>
        <TabPane tab={t('用户排行榜')} itemKey='users'>
          <UserRankings period={period} />
        </TabPane>
        <TabPane tab={t('模型排行榜')} itemKey='models'>
          <ModelRankings />
        </TabPane>
      </Tabs>
    </div>
  );
};

export default Rankings;
