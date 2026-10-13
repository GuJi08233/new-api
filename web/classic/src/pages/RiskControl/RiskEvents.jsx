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
  Radio,
  RadioGroup,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconClose, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';
import {
  riskCurrentUserId,
  riskEventTypeMeta,
  riskRelativeTime,
  riskReviewColor,
  riskReviewLabel,
  riskRuleLabel,
} from './riskLabels';
import useRiskFilters, { riskInteger } from './useRiskFilters';
import useRiskAssignees from './useRiskAssignees';
import RiskEventReviewModal from './RiskEventReviewModal';
import RiskEventEvidence from './RiskEventEvidence';

const { Text } = Typography;

const EVENT_TYPES = [
  'block_ip',
  'block_ua',
  'block_client_ua',
  'ban_ip',
  'ban_auto',
  'ban_manual',
  'unban_ip',
  'unban',
  'unban_auto',
  'alert',
];

const DEFAULT_FILTERS = {
  category: '',
  event_type: '',
  keyword: '',
  user_id: '',
  ip: '',
  ua: '',
  hours: 0,
  rule_id: '',
  review_status: '',
  assignee_id: '',
  unassigned: '',
  start_timestamp: '',
  end_timestamp: '',
  p: 1,
};

// 快捷视图:「待处理告警」是告警加待处理的组合,其余对应事件大类。
const QUICK_VIEWS = [
  { value: 'all', label: '全部' },
  { value: 'pending_alert', label: '待处理告警' },
  { value: 'block', label: '拦截' },
  { value: 'ban', label: '封禁与禁用' },
  { value: 'unban', label: '解封与恢复' },
  { value: 'alert', label: '告警' },
];

const quickViewOf = (filters) => {
  if (filters.category === 'alert' && filters.review_status === 'pending')
    return 'pending_alert';
  return filters.category || 'all';
};

// 风控事件列表。独立页面时筛选与页码保存在地址里;嵌在对象详情里时(embedded)只按传入的
// 用户 / IP / UA 过滤,分页留在本地,不影响事件页的地址。
const RiskEvents = ({
  fixedFilters,
  embedded = false,
  onInspect,
  refreshKey,
}) => {
  const { t } = useTranslation();
  const { list: assignees, nameOf } = useRiskAssignees();
  const [urlFilters, setUrlFilters] = useRiskFilters('e', DEFAULT_FILTERS);
  const [localPage, setLocalPage] = useState(1);
  const filters = embedded
    ? { ...DEFAULT_FILTERS, ...fixedFilters, p: localPage }
    : urlFilters;
  const filterKey = JSON.stringify(filters);
  const page = riskInteger(filters.p, 1, 1, 1000000);
  const [keyword, setKeyword] = useState(filters.keyword);
  const [rules, setRules] = useState([]);
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [reload, setReload] = useState(0);
  const [selected, setSelected] = useState([]);
  const [batching, setBatching] = useState(false);
  const [reviewEvent, setReviewEvent] = useState(null);
  const me = riskCurrentUserId();

  useEffect(() => setKeyword(filters.keyword), [filters.keyword]);
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
    for (const [key, value] of Object.entries(query)) {
      if (!value || key === 'p' || key === 'hours') continue;
      params.set(key, String(value));
    }
    setLoading(true);
    setError('');
    setSelected([]);
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

  const update = (patch) => {
    if (embedded) {
      if (Object.hasOwn(patch, 'p')) setLocalPage(patch.p);
      return;
    }
    setUrlFilters(patch);
  };
  const applyQuickView = (view) => {
    const patch = { category: '', review_status: '', event_type: '' };
    if (view === 'pending_alert') {
      patch.category = 'alert';
      patch.review_status = 'pending';
    } else if (view !== 'all') {
      patch.category = view;
    }
    update(patch);
  };
  const applyHours = (hours) =>
    update({
      hours,
      start_timestamp: hours
        ? Math.floor(Date.now() / 1000) - hours * 3600
        : '',
      end_timestamp: '',
    });
  const resetFilters = () => update({ ...DEFAULT_FILTERS });

  const inspect = (type, value) => onInspect?.({ type, value: String(value) });
  const reviewBatch = async (patch, label) => {
    const targets = items
      .filter((item) => selected.includes(item.id))
      .map((item) => ({ id: item.id, version: item.review_version || 0 }));
    if (!targets.length) return;
    setBatching(true);
    try {
      const res = await API.post(
        '/api/risk/event_reviews',
        { items: targets, ...patch },
        { skipErrorHandler: true },
      );
      if (!res.data.success) throw new Error(res.data.message);
      const results = res.data.data?.results || [];
      const failed = results.filter((result) => !result.success);
      if (failed.length) {
        showError(
          t('{{action}}：成功 {{ok}} 条，失败 {{failed}} 条（{{reason}}）', {
            action: label,
            ok: results.length - failed.length,
            failed: failed.length,
            reason: t(failed[0].message),
          }),
        );
      } else {
        showSuccess(
          t('{{action}}：已处理 {{count}} 条', {
            action: label,
            count: results.length,
          }),
        );
      }
      setReload((n) => n + 1);
    } catch (e) {
      showError(e.response?.data?.message || e.message);
    }
    setBatching(false);
  };

  const activeSubjectTags = embedded
    ? []
    : [
        ['user_id', (value) => `${t('用户')} #${value}`],
        ['ip', (value) => `IP ${value}`],
        [
          'ua',
          (value) =>
            `UA ${value.length > 40 ? `${value.slice(0, 40)}…` : value}`,
        ],
        ['rule_id', (value) => `${t('规则')} ${value}`],
        ['event_type', (value) => riskEventTypeMeta(value, t).label],
      ].filter(([key]) => filters[key]);

  const assigneeFilterValue =
    filters.unassigned === 'true'
      ? 'unassigned'
      : filters.assignee_id
        ? Number(filters.assignee_id)
        : '';

  const columns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      width: 150,
      render: (value) => (
        <div className='flex flex-col'>
          <Text size='small'>{timestamp2string(value)}</Text>
          <Text type='tertiary' size='small'>
            {t('{{time}} 前', { time: riskRelativeTime(value) })}
          </Text>
        </div>
      ),
    },
    {
      title: t('类型'),
      dataIndex: 'event_type',
      width: 140,
      render: (_, row) => {
        const meta = riskEventTypeMeta(row.event_type, t);
        return <Tag color={meta.color}>{meta.label}</Tag>;
      },
    },
    {
      title: t('对象'),
      width: 230,
      render: (_, row) => (
        <div className='flex flex-col items-start gap-1'>
          {row.user_id > 0 && (
            <Button
              size='small'
              theme='borderless'
              style={{ padding: 0, height: 'auto' }}
              onClick={() => inspect('user', row.user_id)}
            >
              {row.username || t('用户')} #{row.user_id}
            </Button>
          )}
          {row.ip && (
            <Space spacing={4}>
              <IpTag ip={row.ip} />
              {!row.ip.includes('/') && (
                <Button
                  size='small'
                  theme='borderless'
                  onClick={() => inspect('ip', row.ip)}
                >
                  {t('调查')}
                </Button>
              )}
            </Space>
          )}
          {!row.user_id && !row.ip && <Text type='tertiary'>-</Text>}
        </div>
      ),
    },
    {
      title: t('规则与原因'),
      render: (_, row) => (
        <div className='flex flex-col min-w-0'>
          <Text strong ellipsis={{ showTooltip: true }}>
            {riskRuleLabel(row, rules, t)}
          </Text>
          <Text type='tertiary' size='small' ellipsis={{ showTooltip: true }}>
            {row.reason || '-'}
          </Text>
        </div>
      ),
    },
    {
      title: t('次数'),
      dataIndex: 'count',
      width: 70,
      render: (value) => (value > 1 ? <Tag color='blue'>{value}</Tag> : value),
    },
    {
      title: t('处理状态'),
      width: 150,
      render: (_, row) => (
        <div className='flex flex-col items-start gap-1'>
          <Tag color={riskReviewColor(row.review_status)}>
            {riskReviewLabel(row.review_status, t)}
          </Tag>
          {row.assignee_id > 0 && (
            <Text type='tertiary' size='small'>
              {t('负责人')}: {nameOf(row.assignee_id)}
            </Text>
          )}
        </div>
      ),
    },
    {
      title: t('操作'),
      width: 90,
      fixed: 'right',
      render: (_, row) => (
        <Button size='small' onClick={() => setReviewEvent(row)}>
          {t('处理')}
        </Button>
      ),
    },
  ];

  return (
    <>
      {!embedded && (
        <div className='flex flex-col gap-3 mb-3'>
          <RadioGroup
            type='button'
            value={quickViewOf(filters)}
            onChange={(event) => applyQuickView(event.target.value)}
            aria-label={t('事件分类')}
          >
            {QUICK_VIEWS.map((view) => (
              <Radio key={view.value} value={view.value}>
                {t(view.label)}
              </Radio>
            ))}
          </RadioGroup>
          <Space wrap>
            <Input
              value={keyword}
              onChange={setKeyword}
              onEnterPress={() => update({ keyword: keyword.trim() })}
              placeholder={t('用户 ID、IP、用户名、UA 或原因')}
              style={{ width: 260 }}
              showClear
              onClear={() => update({ keyword: '' })}
              aria-label={t('搜索事件')}
            />
            <Select
              value={Number(filters.hours) || 0}
              onChange={applyHours}
              style={{ width: 130 }}
              aria-label={t('时间范围')}
            >
              <Select.Option value={0}>{t('全部时间')}</Select.Option>
              <Select.Option value={1}>{t('近 1 小时')}</Select.Option>
              <Select.Option value={24}>{t('近 24 小时')}</Select.Option>
              <Select.Option value={72}>{t('近 3 天')}</Select.Option>
              <Select.Option value={168}>{t('近 7 天')}</Select.Option>
            </Select>
            <Select
              value={filters.review_status}
              onChange={(value) => update({ review_status: value })}
              style={{ width: 140 }}
              aria-label={t('处理状态')}
            >
              <Select.Option value=''>{t('全部处理状态')}</Select.Option>
              {['pending', 'confirmed', 'false_positive'].map((value) => (
                <Select.Option key={value} value={value}>
                  {riskReviewLabel(value, t)}
                </Select.Option>
              ))}
            </Select>
            <Select
              value={assigneeFilterValue}
              onChange={(value) =>
                update(
                  value === 'unassigned'
                    ? { unassigned: 'true', assignee_id: '' }
                    : { unassigned: '', assignee_id: value || '' },
                )
              }
              style={{ width: 170 }}
              filter
              aria-label={t('负责人')}
              optionList={[
                { value: '', label: t('全部负责人') },
                ...(me ? [{ value: me, label: t('我负责的') }] : []),
                { value: 'unassigned', label: t('未分配') },
                ...assignees
                  .filter((admin) => admin.id !== me)
                  .map((admin) => ({
                    value: admin.id,
                    label: admin.display_name || admin.username,
                  })),
              ]}
            />
            <Select
              value={filters.event_type}
              onChange={(value) => update({ event_type: value })}
              style={{ width: 150 }}
              aria-label={t('事件类型')}
            >
              <Select.Option value=''>{t('全部类型')}</Select.Option>
              {EVENT_TYPES.map((value) => (
                <Select.Option key={value} value={value}>
                  {riskEventTypeMeta(value, t).label}
                </Select.Option>
              ))}
            </Select>
            <Select
              value={filters.rule_id}
              onChange={(value) => update({ rule_id: value || '' })}
              style={{ width: 200 }}
              filter
              allowCreate
              showClear
              placeholder={t('全部规则')}
              aria-label={t('按规则筛选')}
              optionList={[
                ...['probe_guard', 'error_guard'].map((value) => ({
                  value,
                  label: riskRuleLabel({ rule_id: value }, rules, t),
                })),
                ...rules.map((rule) => ({
                  value: rule.id,
                  label: riskRuleLabel({ rule_id: rule.id }, rules, t),
                })),
              ]}
            />
            <Button
              icon={<IconSearch />}
              onClick={() => update({ keyword: keyword.trim() })}
              aria-label={t('查询')}
            >
              {t('查询')}
            </Button>
            <Button onClick={resetFilters}>{t('重置')}</Button>
            <Button
              icon={<IconRefresh />}
              onClick={() => setReload((n) => n + 1)}
              aria-label={t('刷新')}
            >
              {t('刷新')}
            </Button>
          </Space>
          {activeSubjectTags.length > 0 && (
            <Space wrap>
              <Text type='tertiary' size='small'>
                {t('当前筛选')}:
              </Text>
              {activeSubjectTags.map(([key, render]) => (
                <Tag
                  key={key}
                  color='blue'
                  closable
                  onClose={() => update({ [key]: '' })}
                >
                  {render(String(filters[key]))}
                </Tag>
              ))}
            </Space>
          )}
        </div>
      )}
      {error && <Banner type='danger' className='mb-3' description={error} />}
      {selected.length > 0 && (
        <div className='flex flex-wrap items-center gap-2 mb-3 p-2 rounded bg-[var(--semi-color-fill-0)]'>
          <Text>{t('已选择 {{count}} 条', { count: selected.length })}</Text>
          <Button
            size='small'
            loading={batching}
            onClick={() =>
              reviewBatch({ status: 'confirmed' }, t('标记为已确认'))
            }
          >
            {t('标记为已确认')}
          </Button>
          <Button
            size='small'
            loading={batching}
            onClick={() =>
              reviewBatch({ status: 'false_positive' }, t('标记为误报'))
            }
          >
            {t('标记为误报')}
          </Button>
          <Button
            size='small'
            loading={batching}
            onClick={() =>
              reviewBatch({ status: 'pending' }, t('恢复为待处理'))
            }
          >
            {t('恢复为待处理')}
          </Button>
          {me > 0 && (
            <Button
              size='small'
              loading={batching}
              onClick={() => reviewBatch({ assignee_id: me }, t('分配给我'))}
            >
              {t('分配给我')}
            </Button>
          )}
          <Button
            size='small'
            theme='borderless'
            icon={<IconClose />}
            onClick={() => setSelected([])}
          >
            {t('取消选择')}
          </Button>
        </div>
      )}
      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey='id'
        size={embedded ? 'small' : 'default'}
        scroll={{ x: 1100 }}
        hideExpandedColumn={false}
        expandedRowRender={(row) => (
          <div className='py-2'>
            <RiskEventEvidence event={row} rules={rules} />
            {row.review_note && (
              <Text type='tertiary' size='small' className='block mt-2'>
                {t('处理备注')}: {row.review_note}
              </Text>
            )}
            {row.operator_id > 0 && (
              <Text type='tertiary' size='small' className='block mt-1'>
                {t('操作人')}: {nameOf(row.operator_id)}
              </Text>
            )}
            {row.ua && (
              <Button
                size='small'
                className='mt-2'
                onClick={() => inspect('ua', row.ua)}
              >
                {t('调查此 UA')}
              </Button>
            )}
          </div>
        )}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: (keys) => setSelected(keys),
        }}
        pagination={{
          currentPage: page,
          pageSize: 20,
          total,
          onPageChange: (value) => update({ p: value }),
        }}
        empty={t('没有符合条件的事件')}
      />
      {!embedded && (
        <Text type='tertiary' size='small' className='block mt-2'>
          {t(
            '拦截事件按来源聚合，「次数」为聚合窗口内的累计命中数；封禁与解封记录永久保留，拦截与告警按设置的保留天数清理。点击行首箭头查看命中证据。',
          )}
        </Text>
      )}
      {reviewEvent && (
        <RiskEventReviewModal
          event={reviewEvent}
          rules={rules}
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
