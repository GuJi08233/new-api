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
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';
import { riskSourceLabel } from './riskLabels';
import useRiskFilters, { riskInteger } from './useRiskFilters';
import { RiskBatchToolbar } from './RiskBatchModal';

const IpBans = ({ onInspect, onDisposition, onBatch, refreshKey }) => {
  const { t } = useTranslation();
  const [filters, setFilters] = useRiskFilters('b', { keyword: '', p: 1 });
  const query = filters.keyword;
  const page = riskInteger(filters.p, 1, 1, 1000000);
  const [keyword, setKeyword] = useState(query);
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
      keyword: query,
    });
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
  }, [page, query, reload, refreshKey]);
  useEffect(() => setKeyword(query), [query]);

  const search = () => {
    setFilters({ keyword: keyword.trim() });
    setReload((n) => n + 1);
  };
  const columns = [
    {
      title: t('封禁目标'),
      dataIndex: 'target',
      render: (v) => (
        <Space>
          {v.includes('/') ? (
            <Typography.Text code>{v}</Typography.Text>
          ) : (
            <IpTag ip={v} />
          )}
          <Button
            size='small'
            theme='borderless'
            onClick={() => onInspect({ type: 'ip', value: v })}
          >
            {t('详情')}
          </Button>
        </Space>
      ),
    },
    {
      title: t('到期时间'),
      dataIndex: 'expires_at',
      width: 180,
      render: (v) => (
        <Tag color={!v ? 'red' : v * 1000 <= Date.now() ? 'grey' : 'orange'}>
          {!v
            ? t('永久')
            : v * 1000 <= Date.now()
              ? t('已过期')
              : timestamp2string(v)}
        </Tag>
      ),
    },
    {
      title: t('来源'),
      dataIndex: 'source',
      width: 120,
      render: (v) => riskSourceLabel(v, t),
    },
    {
      title: t('原因'),
      dataIndex: 'reason',
      ellipsis: { showTitle: true },
      render: (v) => v || '-',
    },
    {
      title: t('添加时间'),
      dataIndex: 'created_at',
      width: 160,
      render: (v) => timestamp2string(v),
    },
    {
      title: t('操作人'),
      dataIndex: 'created_by',
      width: 90,
      render: (v) => (v > 0 ? `#${v}` : '-'),
    },
    {
      title: t('操作'),
      width: 120,
      render: (_, ban) => (
        <Button
          size='small'
          disabled={ban.expires_at > 0 && ban.expires_at * 1000 <= Date.now()}
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
      <Space wrap className='mb-4'>
        <Input
          value={keyword}
          onChange={setKeyword}
          placeholder={t('搜索目标或原因')}
          style={{ width: 240 }}
          onEnterPress={search}
        />
        <Button icon={<IconSearch />} aria-label={t('查询')} onClick={search}>
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
        scroll={{ x: 1050 }}
        pagination={{
          currentPage: page,
          pageSize: 20,
          total,
          onPageChange: (value) => setFilters({ p: value }),
        }}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: setSelected,
          getCheckboxProps: (ban) => ({
            disabled: ban.expires_at > 0 && ban.expires_at * 1000 <= Date.now(),
          }),
        }}
        empty={t('暂无数据')}
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
