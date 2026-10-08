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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Table, Typography } from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { ArrowDownToLine, ArrowUpFromLine } from 'lucide-react';
import FuturesPositions from './FuturesPositions';
import LedgerCard from './LedgerCard';
import PnlCalendar from './PnlCalendar';
import TransferModal from './TransferModal';
import SpotMarginCard from './SpotMarginCard';
import {
  formatPrice,
  formatQty,
  formatSignedUsdt,
  formatUsdt,
  toUsdt,
  tradeGet,
  trendClass,
} from './api';

const { Text, Title } = Typography;

// 资产页：总资产与盈亏、转入转出、现货持仓、合约仓位、资产分布、30 天总资产曲线、盈亏日历与账单。
const AssetsView = ({
  self,
  perUnit,
  tickers,
  onSelfChanged,
  onAccountChanged,
  onOpenSymbol,
  onOpenFutures,
  noticeKey,
  t,
}) => {
  const [transfer, setTransfer] = useState(null);
  const [history, setHistory] = useState([]);
  const [futuresPositions, setFuturesPositions] = useState([]);
  const [cross, setCross] = useState(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const valuation = self?.valuation;

  const loadFutures = useCallback(async () => {
    const res = await tradeGet('/api/trade/futures/positions', t);
    if (!res.data) return;
    setFuturesPositions(res.data.positions || []);
    setCross(res.data.cross || null);
  }, [t]);

  useEffect(() => {
    loadFutures();
  }, [loadFutures, refreshKey]);

  // 后台发生了成交、止盈止损或强平(见 NoticeBell)时刷新仓位、曲线、日历与账单。
  useEffect(() => {
    if (noticeKey) setRefreshKey((value) => value + 1);
  }, [noticeKey]);

  useEffect(() => {
    let alive = true;
    tradeGet('/api/trade/assets/history', t, { days: 30 }).then((res) => {
      if (alive && res.data) setHistory(res.data);
    });
    return () => {
      alive = false;
    };
  }, [refreshKey, t]);

  const curveSpec = useMemo(() => {
    const values = history.flatMap((point) => [
      {
        day: point.day,
        type: t('总资产'),
        value: toUsdt(point.equity, perUnit),
      },
      {
        day: point.day,
        type: t('累计净转入'),
        value: toUsdt(point.net_in, perUnit),
      },
    ]);
    return {
      type: 'line',
      data: [{ id: 'tradeEquity', values }],
      xField: 'day',
      yField: 'value',
      seriesField: 'type',
      legends: { visible: true, orient: 'top' },
      line: { style: { lineWidth: 2, curveType: 'monotone' } },
      point: { visible: false },
      axes: [
        {
          orient: 'left',
          label: { formatMethod: (value) => `${Number(value).toFixed(0)}` },
        },
      ],
      tooltip: {
        dimension: {
          content: [
            {
              key: (datum) => datum.type,
              value: (datum) => `${datum.value.toFixed(2)} USDT`,
            },
          ],
        },
      },
    };
  }, [history, perUnit, t]);

  const pieSpec = useMemo(() => {
    const values = [
      {
        type: t('资金'),
        value: toUsdt(
          (valuation?.cash || 0) + (valuation?.frozen || 0),
          perUnit,
        ),
      },
      { type: t('加密货币'), value: toUsdt(valuation?.crypto_value, perUnit) },
      { type: t('美股代币'), value: toUsdt(valuation?.stock_value, perUnit) },
      { type: t('合约'), value: toUsdt(valuation?.futures_value, perUnit) },
    ].filter((item) => item.value > 0);
    return {
      type: 'pie',
      data: [{ id: 'tradeAllocation', values }],
      categoryField: 'type',
      valueField: 'value',
      outerRadius: 0.8,
      innerRadius: 0.55,
      legends: { visible: true, orient: 'bottom' },
      label: { visible: false },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum) => datum.type,
              value: (datum) => `${datum.value.toFixed(2)} USDT`,
            },
          ],
        },
      },
    };
  }, [valuation, perUnit, t]);

  if (!self) return null;

  const positionValue =
    (valuation?.crypto_value || 0) + (valuation?.stock_value || 0);
  const summary = [
    [t('总资产'), `${formatUsdt(valuation?.equity, perUnit)} USDT`, ''],
    [
      t('今日盈亏'),
      `${formatSignedUsdt(self.today_pnl, perUnit)} USDT`,
      trendClass(self.today_pnl),
    ],
    [
      t('累计盈亏'),
      `${formatSignedUsdt(self.total_pnl, perUnit)} USDT`,
      trendClass(self.total_pnl),
    ],
    [t('可用资金'), `${formatUsdt(valuation?.cash, perUnit)} USDT`, ''],
    [t('挂单冻结'), `${formatUsdt(valuation?.frozen, perUnit)} USDT`, ''],
    [t('现货市值'), `${formatUsdt(positionValue, perUnit)} USDT`, ''],
    [
      t('合约价值'),
      `${formatUsdt(valuation?.futures_value, perUnit)} USDT`,
      '',
    ],
  ];

  const holdingColumns = [
    {
      title: t('交易对'),
      dataIndex: 'symbol',
      render: (symbol) => (
        <Button
          theme='borderless'
          size='small'
          onClick={() => onOpenSymbol(symbol)}
        >
          {tickers[symbol] || symbol}
        </Button>
      ),
    },
    {
      title: t('持有数量'),
      dataIndex: 'qty',
      align: 'right',
      render: (value) => formatQty(value),
    },
    {
      title: t('持仓均价'),
      dataIndex: 'avg_price',
      align: 'right',
      render: (value) => formatPrice(value),
    },
    {
      title: t('最新价'),
      dataIndex: 'price',
      align: 'right',
      render: (value) => (value ? formatPrice(value) : '--'),
    },
    {
      title: t('持仓市值'),
      dataIndex: 'value',
      align: 'right',
      render: (value) => `${formatUsdt(value, perUnit)} USDT`,
    },
    {
      title: t('浮动盈亏'),
      dataIndex: 'pnl',
      align: 'right',
      render: (_, holding) => {
        const pnl = holding.value - holding.cost;
        const percent = holding.cost > 0 ? (pnl / holding.cost) * 100 : 0;
        return (
          <span className={`trade-num ${trendClass(pnl)}`}>
            {formatSignedUsdt(pnl, perUnit)} ({percent > 0 ? '+' : ''}
            {percent.toFixed(2)}%)
          </span>
        );
      },
    },
  ];

  return (
    <div className='flex flex-col gap-4'>
      <div className='trade-card flex flex-col gap-4'>
        <div className='grid grid-cols-2 gap-4 md:grid-cols-4 xl:grid-cols-7'>
          {summary.map(([label, value, className]) => (
            <div key={label}>
              <Text type='tertiary' size='small'>
                {label}
              </Text>
              <div className={`trade-num text-lg font-semibold ${className}`}>
                {value}
              </div>
            </div>
          ))}
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            theme='solid'
            icon={<ArrowDownToLine size={16} />}
            disabled={!self.enabled}
            onClick={() => setTransfer('in')}
          >
            {t('转入')}
          </Button>
          <Button
            icon={<ArrowUpFromLine size={16} />}
            onClick={() => setTransfer('out')}
          >
            {t('转出')}
          </Button>
          <Text type='tertiary' size='small'>
            {t('可转出 {{quota}} USDT', {
              quota: formatUsdt(self.withdrawable?.quota, perUnit),
            })}
          </Text>
          <Text type='tertiary' size='small'>
            {t('游戏币余额：{{amount}}', {
              amount: self.wallet?.game_coins || 0,
            })}
          </Text>
        </div>
      </div>
      <SpotMarginCard
        self={self}
        perUnit={perUnit}
        onChanged={onSelfChanged}
        t={t}
      />
      <div className='trade-card'>
        <Title heading={6} className='!mb-2'>
          {t('现货持仓')}
        </Title>
        <Table
          size='small'
          rowKey='symbol'
          columns={holdingColumns}
          dataSource={valuation?.holdings || []}
          pagination={false}
          scroll={{ x: 'max-content' }}
          empty={<Text type='tertiary'>{t('还没有持仓')}</Text>}
        />
      </div>
      {(futuresPositions.length > 0 || self.futures?.enabled) && (
        <div className='trade-card'>
          <FuturesPositions
            positions={futuresPositions}
            cross={cross}
            cash={valuation?.cash}
            perUnit={perUnit}
            takerFeeBps={self.futures?.taker_fee_bps}
            onChanged={() => {
              loadFutures();
              onAccountChanged?.();
              setRefreshKey((value) => value + 1);
            }}
            onOpenSymbol={onOpenFutures}
            t={t}
          />
        </div>
      )}
      <div className='grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]'>
        <div className='trade-card'>
          <Title heading={6} className='!mb-2'>
            {t('近 30 天总资产')}
          </Title>
          {history.length > 1 ? (
            <div style={{ height: 260 }}>
              <VChart spec={curveSpec} />
            </div>
          ) : (
            <Text type='tertiary' size='small'>
              {t('每天 0 点后记录一次总资产，记录满两天后显示曲线。')}
            </Text>
          )}
        </div>
        <div className='trade-card'>
          <Title heading={6} className='!mb-2'>
            {t('资产分布')}
          </Title>
          {valuation?.equity > 0 ? (
            <div style={{ height: 260 }}>
              <VChart spec={pieSpec} />
            </div>
          ) : (
            <Text type='tertiary' size='small'>
              {t('账户里还没有资产')}
            </Text>
          )}
        </div>
      </div>
      <PnlCalendar perUnit={perUnit} refreshKey={refreshKey} t={t} />
      <LedgerCard perUnit={perUnit} refreshKey={refreshKey} t={t} />
      <TransferModal
        direction={transfer}
        self={self}
        perUnit={perUnit}
        onClose={() => setTransfer(null)}
        onDone={(next) => {
          setTransfer(null);
          onSelfChanged(next);
          setRefreshKey((value) => value + 1);
        }}
        t={t}
      />
    </div>
  );
};

export default AssetsView;
