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
import { orderStatusTag } from './OrdersCard';
import {
  formatPrice,
  formatQty,
  formatSignedUsdt,
  formatUsdt,
  tradeGet,
  tradePost,
  trendClass,
} from './api';

const { Text } = Typography;
const PAGE_SIZE = 10;

// 合约委托与仓位历史：当前委托可以撤单，历史委托可以查看每一次成交，仓位历史是已经结束的仓位(平仓、止盈止损或强平)
// 与它们的净盈亏。symbol 为空时列出所有合约。
const FuturesOrdersCard = ({
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
    const params = {
      symbol: symbol || undefined,
      p: page,
      page_size: PAGE_SIZE,
    };
    const res =
      tab === 'positions'
        ? await tradeGet('/api/trade/futures/history', t, params)
        : await tradeGet('/api/trade/futures/orders', t, {
            ...params,
            status: tab,
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
    const res = await tradePost(
      `/api/trade/futures/orders/${order.id}/cancel`,
      {},
      t,
    );
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
    const res = await tradeGet(
      `/api/trade/futures/orders/${order.id}/fills`,
      t,
    );
    if (res.error) {
      showError(res.error);
      return;
    }
    setDetail({ order, fills: res.data || [] });
  };

  const actionLabels = {
    'open-long': t('开多'),
    'open-short': t('开空'),
    'close-long': t('平多'),
    'close-short': t('平空'),
  };
  const triggerLabels = {
    tp: t('止盈'),
    sl: t('止损'),
    liquidation: t('强平'),
  };
  const closeReasons = {
    close: t('平仓'),
    tp: t('止盈'),
    sl: t('止损'),
    liquidation: t('强平'),
  };
  const symbolColumn = symbol
    ? []
    : [{ title: t('合约'), dataIndex: 'symbol' }];
  const price = (value) => formatPrice(value, priceDigits);

  const orderColumns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      render: (value) => timestamp2string(value),
    },
    ...symbolColumn,
    {
      title: t('方向'),
      dataIndex: 'side',
      render: (value, order) => (
        <span className={value === 'long' ? 'trade-up' : 'trade-down'}>
          {actionLabels[`${order.action}-${value}`]}
        </span>
      ),
    },
    {
      title: t('类型'),
      dataIndex: 'type',
      render: (value, order) => (
        <div className='flex items-center gap-1 whitespace-nowrap'>
          {value === 'limit' ? t('限价') : t('市价')}
          {order.trigger && (
            <Tag size='small' color={order.trigger === 'tp' ? 'green' : 'red'}>
              {triggerLabels[order.trigger] || order.trigger}
            </Tag>
          )}
        </div>
      ),
    },
    {
      title: t('杠杆'),
      dataIndex: 'leverage',
      render: (value) => `${value}x`,
    },
    {
      title: t('委托价'),
      dataIndex: 'price',
      render: (value) => (value ? price(value) : t('市价')),
    },
    {
      title: t('委托数量'),
      dataIndex: 'qty',
      render: (value) => formatQty(value),
    },
    {
      title: t('已成交'),
      dataIndex: 'filled_qty',
      render: (value) => formatQty(value),
    },
    {
      title: t('成交均价'),
      dataIndex: 'avg_price',
      render: (value) => (value ? price(value) : '--'),
    },
    {
      title: t('已实现盈亏'),
      dataIndex: 'realized_pnl',
      render: (value, order) =>
        order.action === 'close' && Number(order.filled_qty) > 0 ? (
          <span className={`trade-num ${trendClass(value)}`}>
            {formatSignedUsdt(value, perUnit, 4)}
          </span>
        ) : (
          '--'
        ),
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

  const historyColumns = [
    {
      title: t('平仓时间'),
      dataIndex: 'closed_at',
      render: (value) => timestamp2string(value),
    },
    ...symbolColumn,
    {
      title: t('方向'),
      dataIndex: 'side',
      render: (value, item) => (
        <div className='flex items-center gap-1 whitespace-nowrap'>
          <Tag size='small' color={value === 'long' ? 'green' : 'red'}>
            {value === 'long' ? t('多仓') : t('空仓')}
          </Tag>
          <Tag size='small'>{item.leverage}x</Tag>
        </div>
      ),
    },
    {
      title: t('数量'),
      dataIndex: 'qty',
      render: (value) => formatQty(value),
    },
    {
      title: t('开仓均价'),
      dataIndex: 'entry_price',
      render: (value) => price(value),
    },
    {
      title: t('平仓均价'),
      dataIndex: 'close_price',
      render: (value) => (value ? price(value) : '--'),
    },
    {
      title: t('已实现盈亏'),
      dataIndex: 'realized_pnl',
      render: (value) => (
        <span className={`trade-num ${trendClass(value)}`}>
          {formatSignedUsdt(value, perUnit, 4)}
        </span>
      ),
    },
    {
      title: t('手续费'),
      dataIndex: 'fees',
      render: (value) => formatUsdt(value, perUnit, 4),
    },
    {
      title: t('资金费'),
      dataIndex: 'funding',
      render: (value) => (
        <span className={`trade-num ${trendClass(value)}`}>
          {formatSignedUsdt(value, perUnit, 4)}
        </span>
      ),
    },
    {
      title: t('净盈亏'),
      dataIndex: 'pnl',
      render: (value) => (
        <Text strong className={`trade-num ${trendClass(value)}`}>
          {formatSignedUsdt(value, perUnit, 4)} USDT
        </Text>
      ),
    },
    {
      title: t('结束方式'),
      dataIndex: 'close_reason',
      render: (value) => (
        <Tag
          size='small'
          color={
            value === 'liquidation'
              ? 'red'
              : value === 'close'
                ? 'grey'
                : 'blue'
          }
        >
          {closeReasons[value] || value}
        </Tag>
      ),
    },
  ];

  const empty = {
    open: t('没有挂着的委托'),
    history: t('还没有历史委托'),
    positions: t('还没有结束的仓位'),
  };

  return (
    <div className='trade-card'>
      <Tabs
        type='line'
        activeKey={tab}
        onChange={(key) => {
          setTab(key);
          setPage(1);
          setData({ items: [], total: 0 });
        }}
      >
        <TabPane tab={t('当前委托')} itemKey='open' />
        <TabPane tab={t('历史委托')} itemKey='history' />
        <TabPane tab={t('仓位历史')} itemKey='positions' />
      </Tabs>
      <Table
        size='small'
        rowKey='id'
        columns={tab === 'positions' ? historyColumns : orderColumns}
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
        empty={<Text type='tertiary'>{empty[tab]}</Text>}
      />
      <Modal
        title={t('成交明细')}
        visible={!!detail}
        footer={null}
        onCancel={() => setDetail(null)}
        width={620}
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
                render: (value) => price(value),
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
              ...(detail.order.action === 'close'
                ? [
                    {
                      title: t('已实现盈亏'),
                      dataIndex: 'pnl',
                      render: (value) => (
                        <span className={`trade-num ${trendClass(value)}`}>
                          {formatSignedUsdt(value, perUnit, 4)}
                        </span>
                      ),
                    },
                  ]
                : []),
            ]}
          />
        )}
      </Modal>
    </div>
  );
};

export default FuturesOrdersCard;
