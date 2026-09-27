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

import React, { useCallback, useEffect, useState } from 'react';
import {
  Button,
  InputNumber,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';
import { getGameCoinWithdrawalStatusMap } from '../../constants';

const { Text } = Typography;

const PAGE_SIZE = 20;

const GameAdminWithdrawals = () => {
  const { t } = useTranslation();
  const statusMap = getGameCoinWithdrawalStatusMap(t);
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [status, setStatus] = useState('');
  const [userId, setUserId] = useState();
  const [explorerUrl, setExplorerUrl] = useState('');

  const load = useCallback(
    async (targetPage) => {
      setLoading(true);
      try {
        const params = new URLSearchParams({
          p: String(targetPage),
          page_size: String(PAGE_SIZE),
        });
        if (status) params.set('status', status);
        if (userId) params.set('user_id', String(userId));
        const res = await API.get(`/api/game/admin/withdrawals?${params}`);
        if (res.data.success) {
          setItems(res.data.data.items || []);
          setTotal(res.data.data.total || 0);
          setPage(targetPage);
        } else {
          showError(res.data.message);
        }
      } finally {
        setLoading(false);
      }
    },
    [status, userId],
  );

  useEffect(() => {
    load(1);
  }, [load]);

  useEffect(() => {
    API.get('/api/game/admin/setting').then((res) => {
      if (res.data.success) {
        setExplorerUrl(res.data.data.setting.explorer_url || '');
      }
    });
  }, []);

  const columns = [
    {
      title: t('单号'),
      dataIndex: 'trade_no',
      width: 220,
      render: (value) => <Text copyable>{value}</Text>,
    },
    {
      title: t('时间'),
      dataIndex: 'created_at',
      width: 170,
      render: (value) => timestamp2string(value),
    },
    {
      title: t('用户'),
      dataIndex: 'user_id',
      width: 160,
      render: (value, record) => `${record.username || '-'} (#${value})`,
    },
    {
      title: t('数量'),
      dataIndex: 'amount',
      width: 80,
    },
    {
      title: t('收款地址'),
      dataIndex: 'to_address',
      width: 220,
      render: (value) => (
        <Text
          copyable={{ content: value }}
          ellipsis={{ showTooltip: true }}
          style={{ width: 190 }}
        >
          {value}
        </Text>
      ),
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 110,
      render: (value) => {
        const meta = statusMap[value];
        return <Tag color={meta?.color || 'grey'}>{meta?.text || value}</Tag>;
      },
    },
    {
      title: t('交易'),
      dataIndex: 'tx_hash',
      width: 200,
      render: (value, record) => {
        if (!value) return '-';
        const label = `${value.slice(0, 10)}… (nonce ${record.nonce})`;
        return explorerUrl ? (
          <a
            href={`${explorerUrl}/tx/${value}`}
            target='_blank'
            rel='noopener noreferrer'
          >
            {label}
          </a>
        ) : (
          <Text copyable={{ content: value }}>{label}</Text>
        );
      },
    },
    {
      title: t('失败原因 / 最近错误'),
      dataIndex: 'error',
      render: (value, record) => {
        const message = record.fail_reason || value;
        if (!message) return '-';
        return (
          <Text
            type={record.status === 'failed' ? 'danger' : 'warning'}
            ellipsis={{ showTooltip: true }}
            style={{ maxWidth: 220 }}
          >
            {message}
          </Text>
        );
      },
    },
  ];

  return (
    <div className='flex flex-col gap-3'>
      <Space wrap>
        <Select
          value={status}
          onChange={setStatus}
          style={{ width: 160 }}
          optionList={[
            { value: '', label: t('全部状态') },
            ...Object.entries(statusMap).map(([value, meta]) => ({
              value,
              label: meta.text,
            })),
          ]}
        />
        <InputNumber
          value={userId}
          min={1}
          precision={0}
          placeholder={t('用户 ID')}
          onChange={(value) =>
            setUserId(typeof value === 'number' ? value : undefined)
          }
        />
        <Button icon={<IconRefresh />} onClick={() => load(page)}>
          {t('刷新')}
        </Button>
      </Space>
      <Table
        rowKey='id'
        columns={columns}
        dataSource={items}
        loading={loading}
        scroll={{ x: 1400 }}
        pagination={{
          currentPage: page,
          pageSize: PAGE_SIZE,
          total,
          onPageChange: load,
        }}
      />
    </div>
  );
};

export default GameAdminWithdrawals;
