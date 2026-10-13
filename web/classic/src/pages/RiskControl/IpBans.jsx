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
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';
import {
  riskRelativeTime,
  riskRemainingTime,
  riskSourceLabel,
} from './riskLabels';
import useRiskFilters, { riskInteger } from './useRiskFilters';
import useRiskAssignees from './useRiskAssignees';
import { RiskBatchToolbar } from './RiskBatchModal';

// IP 封禁列表:状态与来源筛选、近 24 小时拦截次数、解除与批量解除。
const IpBans = ({ onInspect, onDisposition, onBatch, refreshKey }) => {
  const { t } = useTranslation();
  const { nameOf } = useRiskAssignees();
  const [filters, setFilters] = useRiskFilters('b', {
    keyword: '',
    status: 'active',
    source: '',
    p: 1,
  });
  const status = ['active', 'expired', 'permanent', ''].includes(filters.status)
    ? filters.status
    : 'active';
  const source = filters.source;
  const page = riskInteger(filters.p, 1, 1, 1000000);
  const [keyword, setKeyword] = useState(filters.keyword);
  const [selected, setSelected] = useState([]);
  const [reload, setReload] = useState(0);
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    const params = new URLSearchParams({
      p: String(page),
      page_size: '20',
      keyword: filters.keyword,
    });
    if (status) params.set('status', status);
    if (source) params.set('source', source);
    setLoading(true);
    setSelected([]);
    setError('');
    API.get(`/api/risk/ip_bans?${params}`, {
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
  }, [page, filters.keyword, status, source, reload, refreshKey]);
  useEffect(() => setKeyword(filters.keyword), [filters.keyword]);

  const search = () => setFilters({ keyword: keyword.trim(), p: 1 });
  const isExpired = (ban) =>
    ban.expires_at > 0 && ban.expires_at * 1000 <= Date.now();
  const columns = [
    {
      title: t('封禁目标'),
      dataIndex: 'target',
      width: 240,
      render: (value) => (
        <Space spacing={4}>
          {value.includes('/') ? (
            <Typography.Text code>{value}</Typography.Text>
          ) : (
            <IpTag ip={value} />
          )}
          <Button
            size='small'
            theme='borderless'
            onClick={() => onInspect({ type: 'ip', value })}
          >
            {t('调查')}
          </Button>
        </Space>
      ),
    },
    {
      title: t('期限'),
      dataIndex: 'expires_at',
      width: 220,
      render: (value) => {
        if (!value) return <Tag color='red'>{t('永久')}</Tag>;
        if (value * 1000 <= Date.now())
          return (
            <Space spacing={4}>
              <Tag color='grey'>{t('已过期')}</Tag>
              <Typography.Text type='tertiary' size='small'>
                {timestamp2string(value)}
              </Typography.Text>
            </Space>
          );
        return (
          <Space spacing={4}>
            <Tag color='orange'>
              {t('剩余 {{time}}', { time: riskRemainingTime(value) })}
            </Tag>
            <Typography.Text type='tertiary' size='small'>
              {timestamp2string(value)}
            </Typography.Text>
          </Space>
        );
      },
    },
    {
      title: t('近 24 小时拦截'),
      dataIndex: 'recent_hits',
      width: 130,
      render: (value) =>
        value ? (
          <Tag color='red'>{value}</Tag>
        ) : (
          <Typography.Text type='tertiary'>0</Typography.Text>
        ),
    },
    {
      title: t('来源'),
      dataIndex: 'source',
      width: 110,
      render: (value) => riskSourceLabel(value, t),
    },
    {
      title: t('原因'),
      dataIndex: 'reason',
      ellipsis: { showTitle: true },
      render: (value) => value || '-',
    },
    {
      title: t('添加时间'),
      dataIndex: 'created_at',
      width: 130,
      render: (value) => (
        <Typography.Text size='small' type='tertiary'>
          {t('{{time}} 前', { time: riskRelativeTime(value) })}
        </Typography.Text>
      ),
    },
    {
      title: t('操作人'),
      dataIndex: 'created_by',
      width: 90,
      render: (value) => (value > 0 ? nameOf(value) : t('自动')),
    },
    {
      title: t('操作'),
      width: 110,
      fixed: 'right',
      render: (_, ban) => (
        <Button
          size='small'
          disabled={isExpired(ban)}
          onClick={() =>
            onDisposition({
              type: 'ip',
              value: ban.target,
              action: 'unban_ip',
              banId: ban.id,
            })
          }
        >
          {t('解除封禁')}
        </Button>
      ),
    },
  ];
  return (
    <>
      <Space wrap className='mb-3'>
        <Input
          value={keyword}
          onChange={setKeyword}
          onEnterPress={search}
          placeholder={t('搜索目标或原因')}
          style={{ width: 220 }}
          showClear
          onClear={() => setFilters({ keyword: '', p: 1 })}
          aria-label={t('搜索封禁')}
        />
        <Select
          value={status}
          onChange={(value) => setFilters({ status: value, p: 1 })}
          style={{ width: 150 }}
          aria-label={t('封禁状态')}
        >
          <Select.Option value='active'>{t('生效中')}</Select.Option>
          <Select.Option value='permanent'>{t('仅永久')}</Select.Option>
          <Select.Option value='expired'>{t('已过期')}</Select.Option>
          <Select.Option value=''>{t('全部状态')}</Select.Option>
        </Select>
        <Select
          value={source}
          onChange={(value) => setFilters({ source: value, p: 1 })}
          style={{ width: 150 }}
          aria-label={t('封禁来源')}
        >
          <Select.Option value=''>{t('全部来源')}</Select.Option>
          {['manual', 'auto_rule', 'probe_guard', 'error_guard'].map(
            (value) => (
              <Select.Option key={value} value={value}>
                {riskSourceLabel(value, t)}
              </Select.Option>
            ),
          )}
        </Select>
        <Button icon={<IconSearch />} onClick={search} aria-label={t('查询')}>
          {t('查询')}
        </Button>
        <Button
          icon={<IconRefresh />}
          onClick={() => setReload((n) => n + 1)}
          aria-label={t('刷新')}
        >
          {t('刷新')}
        </Button>
        <Button
          theme='solid'
          icon={<IconPlus />}
          aria-label={t('封禁 IP')}
          onClick={() =>
            onDisposition({
              type: 'ip',
              value: '',
              action: 'ban_ip',
              editable: true,
            })
          }
        >
          {t('封禁 IP')}
        </Button>
      </Space>
      {error && <Banner type='danger' className='mb-3' description={error} />}
      <Typography.Text type='tertiary' size='small' className='block mb-2'>
        {t(
          '动态 IP 封禁不受风控总开关控制,添加后立即生效。临时封禁到期自动失效,过期记录保留 3 天后清理;封禁与解封历史可在「风控事件」中追溯。',
        )}
      </Typography.Text>
      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey='id'
        scroll={{ x: 1200 }}
        pagination={{
          currentPage: page,
          pageSize: 20,
          total,
          onPageChange: (value) => setFilters({ p: value }),
        }}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: setSelected,
          getCheckboxProps: (ban) => ({ disabled: isExpired(ban) }),
        }}
        empty={t('没有符合条件的封禁记录')}
      />
      <RiskBatchToolbar
        mode='ip_ban'
        targets={selected.map((value) => ({
          type: 'ip_ban',
          value: String(value),
        }))}
        onBatch={onBatch}
      />
    </>
  );
};

export default IpBans;
