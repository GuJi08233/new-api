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

import React, { useState } from 'react';
import { Button, Input, Table, Tag, Typography } from '@douyinfe/semi-ui';
import { Search } from 'lucide-react';
import {
  changePercent,
  decimalsOf,
  formatCompact,
  formatFundingRate,
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

// compareQuotes 按数值比较两个行情字段。没有行情的不论升序降序都排在最后，不当成 0 混进前面；Semi 的表格降序时会把比较结果
// 取反，所以这里按 order 先反一次。
function compareQuotes(leftValue, rightValue, order) {
  const left =
    leftValue === undefined || leftValue === null || leftValue === ''
      ? NaN
      : Number(leftValue);
  const right =
    rightValue === undefined || rightValue === null || rightValue === ''
      ? NaN
      : Number(rightValue);
  const leftMissing = !Number.isFinite(left);
  const rightMissing = !Number.isFinite(right);
  if (leftMissing || rightMissing) {
    if (leftMissing === rightMissing) return 0;
    const last = leftMissing ? 1 : -1;
    return order === 'descend' ? -last : last;
  }
  return left - right;
}

// 行情列表：加密货币、美股与大宗商品分组，价格随推送实时变化，点一行进入交易。futures 为真时列的是永续合约，多出标记价格、
// 资金费率与最高杠杆，暂停开仓(只能平仓)的合约标出来。可以按代码搜索，点列头按价格、涨跌幅、成交额(合约还有资金费率)排序。
const MarketList = ({ symbols, quotes, futures = false, onOpen, t }) => {
  const [query, setQuery] = useState('');
  const keyword = query.trim().toUpperCase();
  const matched = keyword
    ? symbols.filter(
        (item) =>
          item.ticker?.toUpperCase().includes(keyword) ||
          item.symbol?.toUpperCase().includes(keyword),
      )
    : symbols;
  const byQuote = (field) => (a, b, order) =>
    compareQuotes(quotes[a.symbol]?.[field], quotes[b.symbol]?.[field], order);
  const byChange = (a, b, order) =>
    compareQuotes(
      changePercent(quotes[a.symbol]?.price, quotes[a.symbol]?.open),
      changePercent(quotes[b.symbol]?.price, quotes[b.symbol]?.open),
      order,
    );
  const groups = [
    { kind: 'crypto', title: t('加密货币') },
    { kind: 'stock', title: futures ? t('股票永续') : t('美股代币') },
    { kind: 'commodity', title: t('大宗商品') },
  ];

  const futuresColumns = [
    {
      title: t('标记价格'),
      dataIndex: 'mark',
      align: 'right',
      render: (_, item) => (
        <Text type='secondary' className='trade-num'>
          {formatPrice(
            quotes[item.symbol]?.mark,
            decimalsOf(item.rules?.tick_size),
          )}
        </Text>
      ),
    },
    {
      title: t('资金费率'),
      dataIndex: 'funding_rate',
      align: 'right',
      sorter: byQuote('funding_rate'),
      render: (_, item) => (
        <Text type='secondary' className='trade-num'>
          {formatFundingRate(quotes[item.symbol]?.funding_rate)}
        </Text>
      ),
    },
  ];

  const columns = [
    {
      title: futures ? t('合约') : t('交易对'),
      dataIndex: 'ticker',
      render: (ticker, item) => (
        <div className='flex items-center gap-2 whitespace-nowrap'>
          <Text strong>{ticker}</Text>
          <Text type='tertiary' size='small'>
            {futures ? `USDT ${t('永续')}` : '/USDT'}
          </Text>
          {futures && (
            <Tag size='small' color='blue'>
              {item.max_leverage}x
            </Tag>
          )}
          {futures && !item.open && (
            <Tag size='small' color='grey'>
              {t('仅可平仓')}
            </Tag>
          )}
        </div>
      ),
    },
    {
      title: t('最新价'),
      dataIndex: 'price',
      align: 'right',
      sorter: byQuote('price'),
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
    ...(futures ? futuresColumns : []),
    {
      title: t('24 小时涨跌'),
      dataIndex: 'change',
      align: 'right',
      sorter: byChange,
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
      sorter: byQuote('quote_volume'),
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
      <Input
        className='trade-market-search'
        prefix={<Search size={15} className='ml-2' />}
        value={query}
        onChange={setQuery}
        showClear
        placeholder={t('按代码搜索，例如 BTC、NVDA')}
        aria-label={t('按代码搜索，例如 BTC、NVDA')}
      />
      {keyword && !matched.length && (
        <div className='trade-card'>
          <Text type='tertiary'>
            {futures ? t('没有匹配的合约') : t('没有匹配的交易对')}
          </Text>
        </div>
      )}
      {groups.map((group) => {
        const items = matched.filter((item) => item.kind === group.kind);
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
              scroll={{ x: futures ? 960 : 760 }}
              onRow={(item) => ({
                onClick: () => onOpen(item.symbol),
                style: { cursor: 'pointer' },
              })}
            />
          </div>
        );
      })}
      <Text type='tertiary' size='small'>
        {futures
          ? t(
              '合约是 Binance 的 U 本位永续合约，可选全仓或逐仓：全仓由整个账户的资金兜底，逐仓每个仓位一般最多亏掉自己的保证金。杠杆上限与维持保证金率按 Binance 的风险限额档位，强平与止损按标记价格触发，止盈按最新成交价触发，资金费按 Binance 公布的费率结算。价格跳空越过强平价时，超出保证金的亏损由你承担：先扣模拟盘资金，不够的从站内额度扣，额度可以扣成负数。',
            )
          : t(
              '美股代币是 Binance 现货上 24 小时交易的代币化股票，盘口比主流加密货币薄得多，大额市价单可能只成交一部分。',
            )}
      </Text>
    </div>
  );
};

export default MarketList;
