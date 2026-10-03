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
  Modal,
  Table,
  TabPane,
  Tabs,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { showError, showSuccess, timestamp2string } from '../../helpers';
import { formatPrice, formatQty, formatUsdt, tradeGet, tradePost } from './api';

const { Text } = Typography;
const PAGE_SIZE = 10;

// orderStatusTag 是委托状态的标签，现货与合约共用；撤销的委托按原因显示。
export function orderStatusTag(order, t) {
  const partial =
    Number(order.filled_qty) > 0 &&
    Number(order.filled_qty) < Number(order.qty);
  if (order.status === 'open') {
    return <Tag color='blue'>{partial ? t('部分成交') : t('挂单中')}</Tag>;
  }
  if (order.status === 'filled') {
    return <Tag color='green'>{t('全部成交')}</Tag>;
  }
  const reasons = {
    user: t('已撤销'),
    depth: t('盘口不足，剩余已撤销'),
    balance: t('资金不足，剩余已撤销'),
    symbol: t('交易对已下架'),
    position: t('仓位已平，委托撤销'),
  };
  return (
    <Tag color={partial ? 'orange' : 'grey'}>
      {reasons[order.cancel_reason] || t('已撤销')}
    </Tag>
  );
}

// 委托列表：当前委托可以撤单，历史委托可以查看每一次成交。symbol 为空时列出所有交易对。
const OrdersCard = ({
  symbol,
  priceDigits,
  perUnit,
  refreshKey,
  onChanged,
  t,
}) => {
  const [tab, setTab] = useState('open');
  const [page, setPage] = useState(1);
  const [data, setData] = useState({ items: [], total: 0 });
  const [loading, setLoading] = useState(false);
  const [canceling, setCanceling] = useState(0);
  const [detail, setDetail] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    const res = await tradeGet('/api/trade/orders', t, {
      status: tab,
      symbol: symbol || undefined,
      p: page,
      page_size: PAGE_SIZE,
    });
    setLoading(false);
    if (res.error) {
      showError(res.error);
      return;
    }
    setData({ items: res.data.items || [], total: res.data.total || 0 });
  }, [tab, symbol, page, t]);

  useEffect(() => {
    load();
  }, [load, refreshKey]);

  const cancel = async (order) => {
    setCanceling(order.id);
    const res = await tradePost(`/api/trade/orders/${order.id}/cancel`, {}, t);
    setCanceling(0);
    if (res.error) {
      showError(res.error);
    } else {
      showSuccess(t('已撤单'));
    }
    load();
    onChanged?.();
  };

  const openDetail = async (order) => {
    const res = await tradeGet(`/api/trade/orders/${order.id}/fills`, t);
    if (res.error) {
      showError(res.error);
      return;
    }
    setDetail({ order, fills: res.data || [] });
  };

  const columns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      render: (value) => timestamp2string(value),
    },
    ...(symbol ? [] : [{ title: t('交易对'), dataIndex: 'symbol' }]),
    {
      title: t('方向'),
      dataIndex: 'side',
      render: (value) => (
        <span className={value === 'buy' ? 'trade-up' : 'trade-down'}>
          {value === 'buy' ? t('买入') : t('卖出')}
        </span>
      ),
    },
    {
      title: t('类型'),
      dataIndex: 'type',
      render: (value) => (value === 'limit' ? t('限价') : t('市价')),
    },
    {
      title: t('委托价'),
      dataIndex: 'price',
      render: (value) => (value ? formatPrice(value, priceDigits) : t('市价')),
    },
    {
      title: t('委托数量'),
      dataIndex: 'qty',
      render: (value, order) =>
        Number(order.budget) > 0
          ? `${formatUsdt(order.budget, perUnit)} USDT`
          : formatQty(value),
    },
    {
      title: t('已成交'),
      dataIndex: 'filled_qty',
      render: (value) => formatQty(value),
    },
    {
      title: t('成交均价'),
      dataIndex: 'avg_price',
      render: (value) => (value ? formatPrice(value, priceDigits) : '--'),
    },
    {
      title: t('成交额'),
      dataIndex: 'filled_amount',
      render: (value) => `${formatUsdt(value, perUnit)} USDT`,
    },
    {
      title: t('手续费'),
      dataIndex: 'fee',
      render: (value) => `${formatUsdt(value, perUnit, 4)} USDT`,
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      render: (_, order) => orderStatusTag(order, t),
    },
    {
      title: '',
      dataIndex: 'operate',
      render: (_, order) =>
        order.status === 'open' ? (
          <Button
            size='small'
            type='danger'
            theme='borderless'
            loading={canceling === order.id}
            onClick={() => cancel(order)}
          >
            {t('撤单')}
          </Button>
        ) : (
          Number(order.filled_qty) > 0 && (
            <Button
              size='small'
              theme='borderless'
              onClick={() => openDetail(order)}
            >
              {t('成交明细')}
            </Button>
          )
        ),
    },
  ];

  return (
    <div className='trade-card'>
      <Tabs
        type='line'
        activeKey={tab}
        onChange={(key) => {
          setTab(key);
          setPage(1);
        }}
      >
        <TabPane tab={t('当前委托')} itemKey='open' />
        <TabPane tab={t('历史委托')} itemKey='history' />
      </Tabs>
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
        empty={
          <Text type='tertiary'>
            {tab === 'open' ? t('没有挂着的委托') : t('还没有历史委托')}
          </Text>
        }
      />
      <Modal
        title={t('成交明细')}
        visible={!!detail}
        footer={null}
        onCancel={() => setDetail(null)}
        width={560}
      >
        {detail && (
          <Table
            size='small'
            rowKey='id'
            pagination={false}
            dataSource={detail.fills}
            columns={[
              {
                title: t('时间'),
                dataIndex: 'created_at',
                render: (value) => timestamp2string(value),
              },
              {
                title: t('成交数量'),
                dataIndex: 'qty',
                render: (value) => formatQty(value),
              },
              {
                title: t('成交价格'),
                dataIndex: 'price',
                render: (value) => formatPrice(value, priceDigits),
              },
              {
                title: t('成交额'),
                dataIndex: 'amount',
                render: (_, fill) =>
                  `${(Number(fill.qty) * Number(fill.price)).toFixed(2)} USDT`,
              },
              {
                title: t('手续费'),
                dataIndex: 'fee',
                render: (value) => `${formatUsdt(value, perUnit, 4)} USDT`,
              },
            ]}
          />
        )}
      </Modal>
    </div>
  );
};

export default OrdersCard;
