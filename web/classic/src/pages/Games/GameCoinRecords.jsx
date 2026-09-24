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
  Table,
  TabPane,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { History } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';
import { renderQuota } from '../../helpers/render';
import {
  getGameCoinLogTypeMap,
  getGameCoinWithdrawalStatusMap,
  getGameMetaMap,
} from '../../constants';

const { Text } = Typography;

const PAGE_SIZE = 10;

// 两张记录表共用的分页加载逻辑；version 变化时回到第一页重新拉取。
function usePagedRecords(url, version) {
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);

  const load = useCallback(
    async (targetPage) => {
      setLoading(true);
      try {
        const res = await API.get(
          `${url}?p=${targetPage}&page_size=${PAGE_SIZE}`,
        );
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
    [url],
  );

  useEffect(() => {
    load(1);
  }, [load, version]);

  return {
    items,
    loading,
    pagination: {
      currentPage: page,
      pageSize: PAGE_SIZE,
      total,
      onPageChange: load,
    },
  };
}

const GameCoinRecords = ({ version, symbol, explorerUrl }) => {
  const { t } = useTranslation();
  const logs = usePagedRecords('/api/game/logs', version);
  const withdrawals = usePagedRecords('/api/game/withdrawals', version);
  const logTypeMap = getGameCoinLogTypeMap(t);
  const statusMap = getGameCoinWithdrawalStatusMap(t);
  const gameMetaMap = getGameMetaMap(t);

  const logColumns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      render: (value) => timestamp2string(value),
    },
    {
      title: t('类型'),
      dataIndex: 'type',
      render: (value) => {
        const meta = logTypeMap[value];
        return <Tag color={meta?.color || 'grey'}>{meta?.text || value}</Tag>;
      },
    },
    {
      title: t('变动'),
      dataIndex: 'amount',
      render: (value) => (
        <Text type={value > 0 ? 'success' : 'danger'} strong>
          {value > 0 ? `+${value}` : value} {symbol}
        </Text>
      ),
    },
    {
      title: t('余额'),
      dataIndex: 'balance',
      render: (value) => `${value} ${symbol}`,
    },
    {
      title: t('说明'),
      dataIndex: 'remark',
      render: (value, record) => {
        if (record.type === 'convert') {
          return t('获得额度 {{quota}}', { quota: renderQuota(record.quota) });
        }
        // 游戏奖励的备注是游戏标识。
        if (record.type === 'game_reward') {
          return gameMetaMap[value]?.name || value;
        }
        return value || record.ref_id || '-';
      },
    },
  ];

  const withdrawalColumns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      render: (value) => timestamp2string(value),
    },
    {
      title: t('数量'),
      dataIndex: 'amount',
      render: (value) => `${value} ${symbol}`,
    },
    {
      title: t('收款地址'),
      dataIndex: 'to_address',
      render: (value) => (
        <Text
          copyable={{ content: value }}
          ellipsis={{ showTooltip: true }}
          style={{ maxWidth: 180 }}
        >
          {value}
        </Text>
      ),
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      render: (value, record) => {
        const meta = statusMap[value];
        const tag = (
          <Tag color={meta?.color || 'grey'}>{meta?.text || value}</Tag>
        );
        return record.fail_reason ? (
          <Tooltip content={record.fail_reason}>{tag}</Tooltip>
        ) : (
          tag
        );
      },
    },
    {
      title: t('交易'),
      dataIndex: 'tx_hash',
      render: (value) => {
        if (!value) return '-';
        const short = `${value.slice(0, 10)}…`;
        return explorerUrl ? (
          <a
            href={`${explorerUrl}/tx/${value}`}
            target='_blank'
            rel='noopener noreferrer'
          >
            {short}
          </a>
        ) : (
          <Text copyable={{ content: value }}>{short}</Text>
        );
      },
    },
  ];

  return (
    <section className='games-section'>
      <div className='games-section-header'>
        <span className='games-card-icon tone-primary'>
          <History size={18} />
        </span>
        <div className='games-section-title'>{t('游戏币记录')}</div>
      </div>
      <div className='games-section-body'>
        <Tabs type='line' defaultActiveKey='logs'>
          <TabPane tab={t('游戏币明细')} itemKey='logs'>
            <Table
              rowKey='id'
              size='small'
              columns={logColumns}
              dataSource={logs.items}
              loading={logs.loading}
              pagination={logs.pagination}
            />
          </TabPane>
          <TabPane tab={t('提现记录')} itemKey='withdrawals'>
            <Table
              rowKey='id'
              size='small'
              columns={withdrawalColumns}
              dataSource={withdrawals.items}
              loading={withdrawals.loading}
              pagination={withdrawals.pagination}
            />
          </TabPane>
        </Tabs>
      </div>
    </section>
  );
};

export default GameCoinRecords;
