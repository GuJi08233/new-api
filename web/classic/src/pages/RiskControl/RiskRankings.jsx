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
  Switch,
  Table,
  Tabs,
  TabPane,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';
import RiskUserState from './RiskUserState';
import useRiskFilters, { riskInteger } from './useRiskFilters';
import { RiskBatchToolbar } from './RiskBatchModal';

const RiskRankings = ({ onInspect, onDisposition, onBatch, refreshKey }) => {
  const { t } = useTranslation();
  const [filters, setFilters] = useRiskFilters('r', {
    metric: 'user_overview',
    hours: 24,
    exclude: false,
    sort: 'request_count',
    order: 'descend',
    p: 1,
    keyword: '',
  });
  const metric = ['user_overview', 'ip_overview', 'ua'].includes(filters.metric)
    ? filters.metric
    : 'user_overview';
  const hours = riskInteger(filters.hours, 24, 1, 168);
  const excludeWhitelist = filters.exclude === 'true';
  const sort = {
    by: filters.sort,
    order: filters.order === 'ascend' ? 'ascend' : 'descend',
  };
  const page = riskInteger(filters.p, 1, 1, 1000000);
  const [keyword, setKeyword] = useState(filters.keyword);
  const [total, setTotal] = useState(0);
  const [selected, setSelected] = useState([]);
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [meta, setMeta] = useState({});
  const [error, setError] = useState('');
  const [reload, setReload] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    const params = new URLSearchParams({
      metric,
      hours: String(hours),
      paginated: 'true',
      p: String(page),
      page_size: '20',
      keyword: filters.keyword,
      sort_by: sort.by,
      sort_order: sort.order === 'ascend' ? 'asc' : 'desc',
    });
    if (excludeWhitelist) params.set('exclude_whitelist', 'true');
    setLoading(true);
    setItems([]);
    setMeta({});
    setSelected([]);
    setError('');
    API.get(`/api/risk/rankings?${params}`, {
      signal: controller.signal,
      skipErrorHandler: true,
    })
      .then((res) => {
        if (controller.signal.aborted) return;
        if (!res.data.success) throw new Error(res.data.message);
        setItems(res.data.data?.items || []);
        setMeta(res.data.data?.meta || {});
        setTotal(res.data.data?.total || 0);
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setError(e.response?.data?.message || e.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [
    metric,
    hours,
    excludeWhitelist,
    sort.by,
    sort.order,
    page,
    filters.keyword,
    reload,
    refreshKey,
  ]);
  useEffect(() => setKeyword(filters.keyword), [filters.keyword]);

  const inspect = (type, value) =>
    onInspect({ type, value: String(value), hours, excludeWhitelist });
  const firstColumn =
    metric === 'user_overview'
      ? {
          title: t('用户'),
          dataIndex: 'username',
          width: 190,
          fixed: 'left',
          render: (v, row) => (
            <Button
              theme='borderless'
              onClick={() => inspect('user', row.user_id)}
            >
              {v || '-'} (#{row.user_id})
            </Button>
          ),
        }
      : metric === 'ip_overview'
        ? {
            title: 'IP',
            dataIndex: 'ip',
            width: 200,
            fixed: 'left',
            render: (v) => <IpTag ip={v} />,
          }
        : {
            title: 'User-Agent',
            dataIndex: 'ua',
            width: 260,
            fixed: 'left',
            ellipsis: { showTitle: true },
          };
  const counts = [
    ['request_count', t('请求数')],
    [
      metric === 'user_overview' ? 'ip_count' : 'user_count',
      metric === 'user_overview' ? t('IP 数') : t('用户数'),
    ],
    ...(metric === 'ua' ? [['ip_count', t('IP 数')]] : []),
    ['token_count', t('令牌数')],
    ['tiny_request_count', t('微量请求')],
    ['error_count', t('错误数')],
  ];
  const columns = [
    firstColumn,
    ...(metric === 'user_overview'
      ? [
          {
            title: t('当前状态'),
            width: 220,
            render: (_, row) => (
              <RiskUserState state={meta.user_states?.[row.user_id]} />
            ),
          },
        ]
      : metric === 'ip_overview'
        ? [
            {
              title: t('动态封禁'),
              width: 110,
              render: (_, row) =>
                meta.ip_states?.[row.ip]?.length ? (
                  <Tag color='red'>{t('已封禁')}</Tag>
                ) : (
                  <Typography.Text type='tertiary'>-</Typography.Text>
                ),
            },
          ]
        : []),
    ...counts.map(([key, title]) => ({
      title,
      dataIndex: key,
      width: 110,
      sorter: true,
      sortOrder: sort.by === key ? sort.order : false,
      render: (v) =>
        v ? (
          <Tag color='blue'>{v}</Tag>
        ) : (
          <Typography.Text type='tertiary'>0</Typography.Text>
        ),
    })),
    {
      title: t('最近活跃'),
      dataIndex: 'last_seen',
      width: 170,
      sorter: true,
      sortOrder: sort.by === 'last_seen' ? sort.order : false,
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
    {
      title: t('操作'),
      width: 200,
      fixed: 'right',
      render: (_, row) => {
        const type =
          metric === 'user_overview'
            ? 'user'
            : metric === 'ip_overview'
              ? 'ip'
              : 'ua';
        const value =
          type === 'user' ? row.user_id : type === 'ip' ? row.ip : row.ua;
        const userState = meta.user_states?.[row.user_id];
        return (
          <Space>
            <Button size='small' onClick={() => inspect(type, value)}>
              {t('查看详情')}
            </Button>
            {type === 'ip' && (
              <Button
                size='small'
                type='danger'
                onClick={() =>
                  onDisposition({ type, value: row.ip, action: 'ban_ip' })
                }
              >
                {t('封禁 IP')}
              </Button>
            )}
            {type === 'user' && userState?.can_disable && (
              <Button
                size='small'
                type='danger'
                onClick={() =>
                  onDisposition({
                    type,
                    value: String(row.user_id),
                    label: row.username,
                    action: 'disable',
                  })
                }
              >
                {userState.status === 2 ? t('转为永久禁用') : t('禁用用户')}
              </Button>
            )}
            {type === 'user' && userState?.can_enable && (
              <Button
                size='small'
                onClick={() =>
                  onDisposition({
                    type,
                    value: String(row.user_id),
                    label: row.username,
                    action: 'enable',
                  })
                }
              >
                {t('启用用户')}
              </Button>
            )}
          </Space>
        );
      },
    },
  ];

  return (
    <>
      {meta.state_available === false && (
        <Banner
          type='warning'
          className='mb-4'
          description={t('当前状态暂时无法读取，处置前请重试。')}
        />
      )}
      {error && <Banner type='danger' className='mb-4' description={error} />}
      {(meta.ip_log_enabled === false || meta.ua_log_enabled === false) && (
        <Banner
          type='warning'
          className='mb-4'
          description={t(
            '风控依赖 IP / User-Agent 日志记录。当前部分记录开关未开启，排行榜数据可能不完整。请前往「运营设置」开启全局 IP / UA 记录。',
          )}
        />
      )}
      <Space wrap className='mb-4'>
        <Typography.Text>{t('时间范围')}</Typography.Text>
        <Select
          value={hours}
          onChange={(value) => setFilters({ hours: value })}
          style={{ width: 140 }}
        >
          <Select.Option value={24}>{t('近 1 天')}</Select.Option>
          <Select.Option value={72}>{t('近 3 天')}</Select.Option>
          <Select.Option value={168}>{t('近 7 天')}</Select.Option>
        </Select>
        <Typography.Text>{t('过滤全局白名单')}</Typography.Text>
        <Switch
          checked={excludeWhitelist}
          onChange={(value) => setFilters({ exclude: value })}
          size='small'
          aria-label={t('过滤全局白名单')}
        />
        <Input
          value={keyword}
          onChange={setKeyword}
          placeholder={t('搜索用户 ID、用户名、IP 或 UA')}
          style={{ width: 240 }}
          onEnterPress={() => setFilters({ keyword: keyword.trim() })}
        />
        <Button
          icon={<IconSearch />}
          aria-label={t('查询')}
          onClick={() => setFilters({ keyword: keyword.trim() })}
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
      </Space>
      <Tabs
        type='button'
        activeKey={metric}
        onChange={(key) => {
          setFilters({
            metric: key,
            sort: 'request_count',
            order: 'descend',
            keyword: '',
          });
        }}
      >
        <TabPane tab={t('用户排行')} itemKey='user_overview' />
        <TabPane tab={t('IP 排行')} itemKey='ip_overview' />
        <TabPane tab={t('UA 排行')} itemKey='ua' />
      </Tabs>
      <Typography.Text type='tertiary' className='block mb-2'>
        {t('数值表示统计结果，是否触发自动处置取决于实际规则。')}
      </Typography.Text>
      <Typography.Text type='tertiary' className='block mb-2'>
        {t('搜索、排序和分页均在服务端执行，筛选条件保存在页面地址中。')}
      </Typography.Text>
      <RiskBatchToolbar
        mode={metric === 'user_overview' ? 'user' : 'ip'}
        targets={selected.map((value) => ({
          type: metric === 'user_overview' ? 'user' : 'ip',
          value: String(value),
        }))}
        onBatch={onBatch}
      />
      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey={(row) => row.user_id || row.ip || row.ua}
        scroll={{ x: metric === 'ua' ? 1450 : 1550 }}
        pagination={{
          pageSize: 20,
          currentPage: page,
          total,
          onPageChange: (value) => setFilters({ p: value }),
        }}
        rowSelection={
          metric === 'ua'
            ? undefined
            : {
                selectedRowKeys: selected,
                onChange: setSelected,
                getCheckboxProps: (row) => ({
                  disabled:
                    metric === 'user_overview' &&
                    !meta.user_states?.[row.user_id]?.can_manage,
                }),
              }
        }
        empty={t('暂无数据')}
        onChange={({ sorter }) => {
          if (!sorter?.dataIndex) return;
          const next = sorter.sortOrder
            ? { by: sorter.dataIndex, order: sorter.sortOrder }
            : { by: 'request_count', order: 'descend' };
          if (sort.by !== next.by || sort.order !== next.order)
            setFilters({ sort: next.by, order: next.order });
        }}
      />
    </>
  );
};

export default RiskRankings;
