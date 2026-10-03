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

import React from 'react';
import { Button, Table, Typography } from '@douyinfe/semi-ui';
import {
  changePercent,
  decimalsOf,
  formatCompact,
  formatPrice,
  trendClass,
} from './api';

const { Text, Title } = Typography;

// 最近 24 小时的小时收盘价画成的迷你走势，首尾比较决定颜色。
const Sparkline = ({ closes }) => {
  const values = (closes || []).map(Number).filter(Number.isFinite);
  if (values.length < 2) return <span className='trade-muted'>--</span>;
  const width = 96;
  const height = 28;
  const lowest = Math.min(...values);
  const range = Math.max(...values) - lowest || 1;
  const points = values
    .map(
      (value, i) =>
        `${((i / (values.length - 1)) * width).toFixed(1)},${(height - ((value - lowest) / range) * height).toFixed(1)}`,
    )
    .join(' ');
  const up = values[values.length - 1] >= values[0];
  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      aria-hidden='true'
    >
      <polyline
        points={points}
        fill='none'
        strokeWidth='1.5'
        stroke={up ? 'var(--semi-color-success)' : 'var(--semi-color-danger)'}
      />
    </svg>
  );
};

// 行情列表：加密货币与美股代币分两组，价格随推送实时变化，点一行进入交易。
const MarketList = ({ symbols, quotes, onOpen, t }) => {
  const groups = [
    { kind: 'crypto', title: t('加密货币') },
    { kind: 'stock', title: t('美股代币') },
  ];

  const columns = [
    {
      title: t('交易对'),
      dataIndex: 'ticker',
      render: (ticker, item) => (
        <div className='flex items-center gap-2'>
          <Text strong>{ticker}</Text>
          <Text type='tertiary' size='small'>
            /USDT
          </Text>
        </div>
      ),
    },
    {
      title: t('最新价'),
      dataIndex: 'price',
      align: 'right',
      render: (_, item) => {
        const quote = quotes[item.symbol] || {};
        const change = changePercent(quote.price, quote.open);
        return (
          <Text className={`trade-num ${trendClass(change)}`}>
            {formatPrice(quote.price, decimalsOf(item.rules?.tick_size))}
          </Text>
        );
      },
    },
    {
      title: t('24 小时涨跌'),
      dataIndex: 'change',
      align: 'right',
      render: (_, item) => {
        const quote = quotes[item.symbol] || {};
        const change = changePercent(quote.price, quote.open);
        return (
          <Text className={`trade-num ${trendClass(change)}`}>
            {change === null
              ? '--'
              : `${change > 0 ? '+' : ''}${change.toFixed(2)}%`}
          </Text>
        );
      },
    },
    {
      title: t('24 小时最高 / 最低'),
      dataIndex: 'range',
      align: 'right',
      render: (_, item) => {
        const quote = quotes[item.symbol] || {};
        const digits = decimalsOf(item.rules?.tick_size);
        return (
          <Text type='secondary' className='trade-num'>
            {formatPrice(quote.high, digits)} / {formatPrice(quote.low, digits)}
          </Text>
        );
      },
    },
    {
      title: t('24 小时成交额'),
      dataIndex: 'volume',
      align: 'right',
      render: (_, item) => (
        <Text type='secondary' className='trade-num'>
          {formatCompact(quotes[item.symbol]?.quote_volume)} USDT
        </Text>
      ),
    },
    {
      title: t('24 小时走势'),
      dataIndex: 'sparkline',
      render: (closes) => <Sparkline closes={closes} />,
    },
    {
      title: '',
      dataIndex: 'operate',
      render: (_, item) => (
        <Button size='small' theme='light' onClick={() => onOpen(item.symbol)}>
          {t('去交易')}
        </Button>
      ),
    },
  ];

  return (
    <div className='flex flex-col gap-4'>
      {groups.map((group) => {
        const items = symbols.filter((item) => item.kind === group.kind);
        if (!items.length) return null;
        return (
          <div key={group.kind} className='trade-card'>
            <Title heading={6} className='!mb-2'>
              {group.title}
            </Title>
            <Table
              size='small'
              rowKey='symbol'
              columns={columns}
              dataSource={items}
              pagination={false}
              scroll={{ x: 760 }}
              onRow={(item) => ({
                onClick: () => onOpen(item.symbol),
                style: { cursor: 'pointer' },
              })}
            />
          </div>
        );
      })}
      <Text type='tertiary' size='small'>
        {t(
          '美股代币是 Binance 现货上 24 小时交易的代币化股票，盘口比主流加密货币薄得多，大额市价单可能只成交一部分。',
        )}
      </Text>
    </div>
  );
};

export default MarketList;
