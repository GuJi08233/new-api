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

import React, { useEffect, useState } from 'react';
import {
  Banner,
  Button,
  Input,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';
import {
  riskSourceLabel,
  riskMetricLabel,
  riskReviewLabel,
} from './riskLabels';
import useRiskFilters, { riskInteger } from './useRiskFilters';
import RiskEventReviewModal from './RiskEventReviewModal';

const EVENT_TYPE_META = {
  block_ua: { labelKey: 'UA 拦截', color: 'orange' },
  block_ip: { labelKey: 'IP 拦截', color: 'red' },
  block_client_ua: { labelKey: '客户端规则拦截', color: 'yellow' },
  ban_auto: { labelKey: '自动封禁', color: 'red' },
  ban_manual: { labelKey: '手动封禁', color: 'purple' },
  unban: { labelKey: '解除封禁', color: 'green' },
  unban_auto: { labelKey: '到期解封', color: 'green' },
  ban_ip: { labelKey: 'IP 封禁', color: 'red' },
  unban_ip: { labelKey: 'IP 解封', color: 'green' },
  alert: { labelKey: '规则告警', color: 'amber' },
};

const RiskEvents = ({
  filters,
  hideFilters = false,
  onInspect,
  refreshKey,
}) => {
  const { t } = useTranslation();
  const [urlFilters, setUrlFilters] = useRiskFilters('e', {
    user_id: filters?.user_id || '',
    ip: filters?.ip || '',
    ua: filters?.ua || '',
    event_type: '',
    hours: 0,
    rule_id: '',
    review_status: '',
    assignee_id: '',
    start_timestamp: '',
    end_timestamp: '',
    p: 1,
  });
  const filterKey = JSON.stringify(urlFilters);
  const [draft, setDraft] = useState(urlFilters);
  const [reviewEvent, setReviewEvent] = useState(null);
  const [rules, setRules] = useState([]);
  const [reload, setReload] = useState(0);
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const page = riskInteger(urlFilters.p, 1, 1, 1000000);
  useEffect(() => setDraft(JSON.parse(filterKey)), [filterKey]);
  useEffect(() => {
    const controller = new AbortController();
    API.get('/api/risk/rules', {
      signal: controller.signal,
      skipErrorHandler: true,
    })
      .then((res) => {
        if (!controller.signal.aborted && res.data.success)
          setRules(res.data.data?.rules || []);
      })
      .catch(() => {});
    return () => controller.abort();
  }, [refreshKey]);
  useEffect(() => {
    const controller = new AbortController();
    const query = JSON.parse(filterKey);
    const params = new URLSearchParams({ p: String(page), page_size: '20' });
    for (const [key, value] of Object.entries(query))
      if (value && key !== 'p' && key !== 'hours')
        params.set(key, String(value));
    setLoading(true);
    setError('');
    setItems([]);
    API.get(`/api/risk/events?${params}`, {
      signal: controller.signal,
      skipErrorHandler: true,
    })
      .then((res) => {
        if (controller.signal.aborted) return;
        if (!res.data.success) throw new Error(res.data.message);
        setItems(res.data.data?.items || []);
        setTotal(res.data.data?.total || 0);
      })
      .catch((e) => {
        if (!controller.signal.aborted) {
          setItems([]);
          setTotal(0);
          setError(e.response?.data?.message || e.message);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [filterKey, page, reload, refreshKey]);
  const applyFilters = () => {
    if (draft.user_id.trim() && !/^[1-9]\d*$/.test(draft.user_id.trim())) {
      setError(t('无效的用户 ID'));
      return;
    }
    if (
      draft.assignee_id.trim() &&
      !/^[1-9]\d*$/.test(draft.assignee_id.trim())
    ) {
      setError(t('负责人 ID 必须为正整数'));
      return;
    }
    const hours = riskInteger(draft.hours, 0, 0, 168);
    setUrlFilters({
      ...draft,
      p: 1,
      user_id: draft.user_id.trim(),
      ip: draft.ip.trim(),
      ua: draft.ua.trim(),
      assignee_id: draft.assignee_id.trim(),
      rule_id: draft.rule_id.trim(),
      start_timestamp: hours
        ? Math.floor(Date.now() / 1000) - hours * 3600
        : '',
      end_timestamp: '',
    });
    setReload((n) => n + 1);
  };
  const inspect = (type, value) => onInspect?.({ type, value: String(value) });
  const columns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      width: 160,
      render: (v) => timestamp2string(v),
    },
    {
      title: t('类型'),
      dataIndex: 'event_type',
      width: 110,
      render: (v) => (
        <Tag color={EVENT_TYPE_META[v]?.color}>
          {t(EVENT_TYPE_META[v]?.labelKey || v)}
        </Tag>
      ),
    },
    {
      title: t('用户'),
      dataIndex: 'user_id',
      width: 150,
      render: (v, row) =>
        v ? (
          <Button theme='borderless' onClick={() => inspect('user', v)}>
            {row.username || `#${v}`}
          </Button>
        ) : (
          '-'
        ),
    },
    {
      title: 'IP',
      dataIndex: 'ip',
      width: 180,
      render: (v) =>
        v ? (
          <Space>
            <IpTag ip={v} />
            <Button
              size='small'
              theme='borderless'
              onClick={() => inspect('ip', v)}
            >
              {t('详情')}
            </Button>
          </Space>
        ) : (
          '-'
        ),
    },
    {
      title: 'User-Agent',
      dataIndex: 'ua',
      width: 180,
      ellipsis: { showTitle: true },
      render: (v) =>
        v ? (
          <Button theme='borderless' onClick={() => inspect('ua', v)}>
            {v}
          </Button>
        ) : (
          '-'
        ),
    },
    {
      title: t('命中规则'),
      dataIndex: 'rule',
      width: 150,
      ellipsis: { showTitle: true },
      render: (v, row) =>
        row.evidence?.name || riskMetricLabel(row.evidence?.metric || v, t),
    },
    {
      title: t('原因'),
      dataIndex: 'reason',
      width: 220,
      ellipsis: { showTitle: true },
      render: (v) => v || '-',
    },
    { title: t('次数'), dataIndex: 'count', width: 80 },
    {
      title: t('处理状态'),
      dataIndex: 'review_status',
      width: 110,
      render: (value) => (
        <Tag
          color={
            value === 'false_positive'
              ? 'grey'
              : value === 'confirmed'
                ? 'green'
                : 'orange'
          }
        >
          {riskReviewLabel(value, t)}
        </Tag>
      ),
    },
    {
      title: t('负责人'),
      dataIndex: 'assignee_id',
      width: 100,
      render: (value) => (value ? `#${value}` : '-'),
    },
    {
      title: t('处理备注'),
      dataIndex: 'review_note',
      width: 180,
      ellipsis: { showTitle: true },
    },
    {
      title: t('操作'),
      width: 120,
      fixed: 'right',
      render: (_, row) => (
        <Button size='small' onClick={() => setReviewEvent(row)}>
          {t('处理事件')}
        </Button>
      ),
    },
    {
      title: t('操作人'),
      dataIndex: 'operator_id',
      width: 100,
      render: (v) => (v > 0 ? `#${v}` : '-'),
    },
  ];

  return (
    <>
      {!hideFilters && (
        <Space wrap className='mb-4'>
          <Select
            value={draft.event_type}
            onChange={(value) =>
              setDraft((current) => ({ ...current, event_type: value }))
            }
            style={{ width: 150 }}
          >
            <Select.Option value=''>{t('全部类型')}</Select.Option>
            {Object.entries(EVENT_TYPE_META).map(([key, meta]) => (
              <Select.Option key={key} value={key}>
                {t(meta.labelKey)}
              </Select.Option>
            ))}
          </Select>
          <Input
            value={draft.user_id}
            onChange={(value) =>
              setDraft((current) => ({ ...current, user_id: value }))
            }
            placeholder={t('用户 ID')}
            style={{ width: 120 }}
            onEnterPress={applyFilters}
          />
          <Input
            value={draft.ip}
            onChange={(value) =>
              setDraft((current) => ({ ...current, ip: value }))
            }
            placeholder='IP'
            style={{ width: 180 }}
            onEnterPress={applyFilters}
          />
          <Input
            value={draft.ua}
            onChange={(value) =>
              setDraft((current) => ({ ...current, ua: value }))
            }
            placeholder='User-Agent'
            style={{ width: 180 }}
            onEnterPress={applyFilters}
          />
          <Select
            value={Number(draft.hours)}
            onChange={(value) =>
              setDraft((current) => ({ ...current, hours: String(value) }))
            }
            style={{ width: 140 }}
          >
            <Select.Option value={0}>{t('全部时间')}</Select.Option>
            <Select.Option value={24}>{t('近 1 天')}</Select.Option>
            <Select.Option value={72}>{t('近 3 天')}</Select.Option>
            <Select.Option value={168}>{t('近 7 天')}</Select.Option>
          </Select>
          <Select
            value={draft.rule_id}
            onChange={(value) =>
              setDraft((current) => ({ ...current, rule_id: value }))
            }
            filter
            style={{ width: 240 }}
            aria-label={t('按规则筛选')}
            optionList={[
              { value: '', label: t('全部规则') },
              ...['probe_guard', 'error_guard'].map((value) => ({
                value,
                label: riskSourceLabel(value, t),
              })),
              ...rules.map((rule) => ({
                value: rule.id,
                label: `${rule.name || riskMetricLabel(rule.metric, t)} (${rule.id})`,
              })),
              ...(draft.rule_id &&
              ![
                'probe_guard',
                'error_guard',
                ...rules.map((rule) => rule.id),
              ].includes(draft.rule_id)
                ? [{ value: draft.rule_id, label: draft.rule_id }]
                : []),
            ]}
          />
          <Input
            value={draft.rule_id}
            onChange={(value) =>
              setDraft((current) => ({ ...current, rule_id: value }))
            }
            placeholder={t('规则标识（支持历史规则）')}
            style={{ width: 200 }}
            onEnterPress={applyFilters}
          />
          <Select
            value={draft.review_status}
            onChange={(value) =>
              setDraft((current) => ({ ...current, review_status: value }))
            }
            style={{ width: 150 }}
            aria-label={t('处理状态筛选')}
          >
            <Select.Option value=''>{t('全部处理状态')}</Select.Option>
            {['pending', 'confirmed', 'false_positive'].map((value) => (
              <Select.Option key={value} value={value}>
                {riskReviewLabel(value, t)}
              </Select.Option>
            ))}
          </Select>
          <Input
            value={draft.assignee_id}
            onChange={(value) =>
              setDraft((current) => ({ ...current, assignee_id: value }))
            }
            placeholder={t('负责人 ID')}
            style={{ width: 120 }}
            onEnterPress={applyFilters}
          />
          <Button
            icon={<IconSearch />}
            aria-label={t('查询')}
            onClick={applyFilters}
          >
            {t('查询')}
          </Button>
          <Button
            icon={<IconRefresh />}
            aria-label={t('刷新')}
            onClick={() => setReload((n) => n + 1)}
          >
            {t('刷新')}
          </Button>
          <Button
            onClick={() => {
              setUrlFilters({
                user_id: '',
                ip: '',
                ua: '',
                event_type: '',
                hours: 0,
                rule_id: '',
                review_status: '',
                assignee_id: '',
                start_timestamp: '',
                end_timestamp: '',
                p: 1,
              });
            }}
          >
            {t('重置')}
          </Button>
        </Space>
      )}
      {error && <Banner type='danger' className='mb-3' description={error} />}
      <Typography.Text type='tertiary' size='small' className='block mb-2'>
        {t(
          '拦截事件按来源聚合记录,「次数」为聚合窗口内的累计命中数;封禁与解禁记录永久保留,拦截与告警记录按设置的保留天数自动清理。',
        )}
      </Typography.Text>
      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey='id'
        scroll={{ x: 1950 }}
        pagination={{
          currentPage: page,
          pageSize: 20,
          total,
          onPageChange: (value) => setUrlFilters({ p: value }),
        }}
        empty={t('暂无数据')}
      />
      {reviewEvent && (
        <RiskEventReviewModal
          event={reviewEvent}
          onClose={() => setReviewEvent(null)}
          onSaved={() => {
            setReviewEvent(null);
            setReload((n) => n + 1);
          }}
        />
      )}
    </>
  );
};

export default RiskEvents;
