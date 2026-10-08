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
  formatDuration,
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

// 合约委托与仓位历史：当前委托可以撤单，历史委托可以查看每一次成交。仓位历史一行是一段从开仓到平完(或强平)的仓位，
// 收益率按投入的保证金算，点开看这段仓位的每一次成交。symbol 为空时列出所有合约。
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
  const modeTag = (item) => (
    <Tag size='small'>
      {item.margin_mode === 'cross' ? t('全仓') : t('逐仓')} {item.leverage}x
    </Tag>
  );
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
      // 开多、平空是买入用涨色，开空、平多是卖出用跌色。
      render: (value, order) => (
        <span
          className={
            (value === 'long') === (order.action === 'open')
              ? 'trade-up'
              : 'trade-down'
          }
        >
          {actionLabels[`${order.action}-${value}`]}
        </span>
      ),
    },
    {
      title: t('类型'),
      dataIndex: 'type',
      render: (value, order) => (
        <div className='flex flex-col gap-1'>
          <div className='flex items-center gap-1 whitespace-nowrap'>
            {value === 'limit' ? t('限价') : t('市价')}
            {modeTag(order)}
            {order.trigger && (
              <Tag
                size='small'
                color={order.trigger === 'tp' ? 'green' : 'red'}
              >
                {triggerLabels[order.trigger] || order.trigger}
              </Tag>
            )}
          </div>
          {(order.take_profits?.length > 0 ||
            order.stop_losses?.length > 0) && (
            <Text type='tertiary' size='small'>
              {t('止盈 {{tp}} 档 · 止损 {{sl}} 档', {
                tp: order.take_profits?.length || 0,
                sl: order.stop_losses?.length || 0,
              })}
            </Text>
          )}
        </div>
      ),
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
      fixed: 'right',
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
      title: t('合约'),
      dataIndex: 'symbol',
      render: (value, item) => (
        <div className='flex items-center gap-1 whitespace-nowrap'>
          <Text strong>{value}</Text>
          <Tag
            size='small'
            type='solid'
            color={item.side === 'long' ? 'green' : 'red'}
          >
            {item.side === 'long' ? t('多仓') : t('空仓')}
          </Tag>
          {modeTag(item)}
          {item.close_reason === 'liquidation' && (
            <Tag size='small' color='red'>
              {t('强平')}
            </Tag>
          )}
          {(item.close_reason === 'tp' || item.close_reason === 'sl') && (
            <Tag
              size='small'
              color={item.close_reason === 'tp' ? 'green' : 'orange'}
            >
              {triggerLabels[item.close_reason]}
            </Tag>
          )}
        </div>
      ),
    },
    {
      title: t('已平仓量'),
      dataIndex: 'qty',
      render: (value) => formatQty(value, 4),
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
      dataIndex: 'pnl',
      render: (value) => (
        <Text strong className={`trade-num ${trendClass(value)}`}>
          {formatSignedUsdt(value, perUnit)} USDT
        </Text>
      ),
    },
    {
      title: t('回报率'),
      dataIndex: 'initial_margin',
      render: (value, item) => {
        if (!(value > 0)) return '--';
        const roi = (item.pnl / value) * 100;
        return (
          <span className={`trade-num ${trendClass(roi)}`}>
            {`${roi > 0 ? '+' : ''}${roi.toFixed(2)}%`}
          </span>
        );
      },
    },
    {
      title: t('开仓时间'),
      dataIndex: 'opened_at',
      render: (value) => timestamp2string(value),
    },
    {
      title: t('持仓时间'),
      dataIndex: 'closed_at',
      render: (value, item) => formatDuration(value - item.opened_at, t),
    },
  ];

  // 仓位历史展开后的每一次成交与这段仓位的保证金、手续费、资金费和平仓时间。资金费收到为正。
  const historyDetail = (item) => (
    <div className='flex flex-col gap-1 py-1'>
      {item.fills.length === 0 ? (
        <Text type='tertiary' size='small'>
          {t('这笔仓位没有成交记录')}
        </Text>
      ) : (
        item.fills.map((fill) => {
          const opening = fill.type === 'futures_open';
          return (
            <div
              key={fill.id}
              className='flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'
            >
              <Text type='tertiary' size='small'>
                {timestamp2string(fill.created_at)}
              </Text>
              <span
                className={
                  (item.side === 'long') === opening ? 'trade-up' : 'trade-down'
                }
              >
                {actionLabels[`${opening ? 'open' : 'close'}-${item.side}`]}
              </span>
              {fill.trigger && (
                <Tag
                  size='small'
                  color={fill.trigger === 'tp' ? 'green' : 'red'}
                >
                  {triggerLabels[fill.trigger] || fill.trigger}
                </Tag>
              )}
              <span className='trade-num'>
                {`${formatQty(fill.qty)} @ ${price(fill.price)}`}
              </span>
              <Text type='tertiary' size='small' className='trade-num'>
                {t('费 {{fee}}', { fee: formatUsdt(fill.fee, perUnit, 4) })}
              </Text>
              <span className={`trade-num ${trendClass(fill.pnl)}`}>
                {opening ? '--' : formatSignedUsdt(fill.pnl, perUnit, 4)}
              </span>
            </div>
          );
        })
      )}
      <Text type='tertiary' size='small' className='trade-num'>
        {t('投入保证金')} {formatUsdt(item.initial_margin, perUnit)}
        {' · '}
        {t('手续费')} -{formatUsdt(item.fees, perUnit, 4)}
        {' · '}
        {t('资金费')} {formatSignedUsdt(item.funding, perUnit, 4)}
        {' · '}
        {t('平仓时间')} {timestamp2string(item.closed_at)}
      </Text>
    </div>
  );

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
        key={tab}
        size='small'
        rowKey='id'
        columns={tab === 'positions' ? historyColumns : orderColumns}
        dataSource={data.items}
        loading={loading}
        scroll={{ x: 'max-content' }}
        expandedRowRender={tab === 'positions' ? historyDetail : undefined}
        expandRowByClick={tab === 'positions'}
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
        width={720}
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
