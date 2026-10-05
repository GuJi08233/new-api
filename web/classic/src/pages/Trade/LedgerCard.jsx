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
import { Select, Table, Typography } from '@douyinfe/semi-ui';
import { showError, timestamp2string } from '../../helpers';
import {
  formatFundingRate,
  formatPrice,
  formatQty,
  formatSignedUsdt,
  formatUsdt,
  tradeGet,
  trendClass,
} from './api';

const { Text, Title } = Typography;
const PAGE_SIZE = 10;

// 账单：资金每变动一次一条(转入、转出、每一次成交、合约的开平仓与保证金调整)，余额是变动后的资金合计(可用 + 冻结)。
// 合约的资金费与强平记在仓位的保证金上，资金变动为 0，金额看盈亏一栏。
const LedgerCard = ({ perUnit, refreshKey, t }) => {
  const [type, setType] = useState('');
  const [page, setPage] = useState(1);
  const [data, setData] = useState({ items: [], total: 0 });
  const [loading, setLoading] = useState(false);
  const typeLabels = {
    quota_in: t('额度转入'),
    quota_out: t('额度转出'),
    coin_in: t('游戏币转入'),
    coin_out: t('游戏币转出'),
    buy: t('买入'),
    sell: t('卖出'),
    futures_open: t('合约开仓'),
    futures_close: t('合约平仓'),
    futures_margin: t('调整保证金'),
    futures_funding: t('资金费'),
    liquidation: t('强制平仓'),
    futures_cover: t('额度补足亏空'),
  };
  const fillTypes = [
    'buy',
    'sell',
    'futures_open',
    'futures_close',
    'liquidation',
  ];

  useEffect(() => {
    let alive = true;
    setLoading(true);
    tradeGet('/api/trade/ledger', t, {
      type: type || undefined,
      p: page,
      page_size: PAGE_SIZE,
    }).then((res) => {
      if (!alive) return;
      setLoading(false);
      if (res.error) {
        showError(res.error);
        return;
      }
      setData({ items: res.data.items || [], total: res.data.total || 0 });
    });
    return () => {
      alive = false;
    };
  }, [type, page, refreshKey, t]);

  const columns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      render: (value) => timestamp2string(value),
    },
    {
      title: t('类型'),
      dataIndex: 'type',
      render: (value) => typeLabels[value] || value,
    },
    {
      title: t('明细'),
      dataIndex: 'symbol',
      render: (_, entry) => {
        if (fillTypes.includes(entry.type)) {
          return `${entry.symbol} ${formatQty(entry.qty)} @ ${formatPrice(entry.price)}`;
        }
        if (entry.type === 'futures_funding') {
          return `${entry.symbol} ${t('资金费率')} ${formatFundingRate(entry.price)}`;
        }
        return entry.symbol || '--';
      },
    },
    {
      title: t('资金变动'),
      dataIndex: 'amount',
      align: 'right',
      render: (value) => (
        <span className={`trade-num ${trendClass(value)}`}>
          {formatSignedUsdt(value, perUnit, 4)}
        </span>
      ),
    },
    {
      title: t('手续费'),
      dataIndex: 'fee',
      align: 'right',
      render: (value) => (value ? formatUsdt(value, perUnit, 4) : '--'),
    },
    {
      title: t('盈亏'),
      dataIndex: 'pnl',
      align: 'right',
      render: (value, entry) =>
        ['futures_close', 'futures_funding', 'liquidation'].includes(
          entry.type,
        ) ? (
          <span className={`trade-num ${trendClass(value)}`}>
            {formatSignedUsdt(value, perUnit, 4)}
          </span>
        ) : (
          '--'
        ),
    },
    {
      title: t('余额'),
      dataIndex: 'balance',
      align: 'right',
      render: (value) => (
        <span className='trade-num'>{formatUsdt(value, perUnit)}</span>
      ),
    },
  ];

  return (
    <div className='trade-card flex flex-col gap-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <Title heading={6} className='!mb-0'>
          {t('资金流水')}
        </Title>
        <Select
          value={type}
          style={{ width: 160 }}
          onChange={(value) => {
            setType(value);
            setPage(1);
          }}
          optionList={[
            { value: '', label: t('全部类型') },
            ...Object.entries(typeLabels).map(([value, label]) => ({
              value,
              label,
            })),
          ]}
        />
      </div>
      <Table
        size='small'
        rowKey='id'
        columns={columns}
        dataSource={data.items}
        loading={loading}
        scroll={{ x: 'max-content' }}
        pagination={
          data.total > PAGE_SIZE && {
            currentPage: page,
            pageSize: PAGE_SIZE,
            total: data.total,
            onPageChange: setPage,
          }
        }
        empty={<Text type='tertiary'>{t('还没有账单')}</Text>}
      />
    </div>
  );
};

export default LedgerCard;
