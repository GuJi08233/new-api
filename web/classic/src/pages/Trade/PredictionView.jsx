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

import React, { useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Input,
  Modal,
  Pagination,
  Radio,
  RadioGroup,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { ExternalLink, RefreshCw } from 'lucide-react';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import { formatQty, formatSignedUsdt, formatUsdt, tradePost } from './api';

const { Text, Title } = Typography;
const PAGE_SIZE = 10;
const POSITION_STATUS = {
  active: '持有中',
  sold: '已全部卖出',
  won: '预测正确',
  lost: '预测错误',
  half: '五五开结算',
};

function predictionNumber(value, maxDecimals = 8) {
  return (
    typeof value === 'string' &&
    value.length <= 32 &&
    new RegExp(`^\\d+(\\.\\d{1,${maxDecimals}})?$`).test(value) &&
    Number.isFinite(Number(value))
  );
}

// 成交仍由服务端重新拉取公开盘口；客户端赔率只用于展示与用户的限价保护。
const PredictionView = ({ self, perUnit, onAccountChanged, t }) => {
  const [market, setMarket] = useState(null);
  const [positions, setPositions] = useState({ items: [], total: 0 });
  const [rounds, setRounds] = useState({ items: [], total: 0 });
  const [filter, setFilter] = useState('active');
  const [page, setPage] = useState(1);
  const [roundPage, setRoundPage] = useState(1);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshKey, setRefreshKey] = useState(0);
  const [side, setSide] = useState('UP');
  const [amount, setAmount] = useState('10');
  const [priceInput, setPriceInput] = useState(null);
  const [submitting, setSubmitting] = useState(false);
  const [sellPosition, setSellPosition] = useState(null);
  const [sellShares, setSellShares] = useState('');
  const [sellPrice, setSellPrice] = useState('');
  const [now, setNow] = useState(Date.now);
  const [clockOffset, setClockOffset] = useState(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    let alive = true;
    let timer;
    let controller;
    let generation = 0;
    const load = async () => {
      if (!alive || document.hidden) return;
      const current = ++generation;
      controller = new AbortController();
      const options = { signal: controller.signal, skipErrorHandler: true };
      const results = await Promise.allSettled([
        API.get('/api/trade/prediction/market', options),
        API.get('/api/trade/prediction/positions', {
          ...options,
          params: { status: filter, page, page_size: PAGE_SIZE },
        }),
        API.get('/api/trade/prediction/rounds', {
          ...options,
          params: { page: roundPage, page_size: PAGE_SIZE },
        }),
      ]);
      if (!alive || current !== generation) return;
      const [marketResult, positionResult, roundResult] = results;
      const failed = results.find(
        (result) =>
          result.status !== 'fulfilled' || !result.value.data?.success,
      );
      setError(
        failed ? failed.value?.data?.message || t('网络异常，请稍后重试') : '',
      );
      if (
        marketResult.status === 'fulfilled' &&
        marketResult.value.data?.success
      ) {
        const view = marketResult.value.data.data;
        setMarket(view);
        setClockOffset(Number(view.server_time) * 1000 - Date.now());
        setNow(Date.now());
      } else {
        setMarket((value) =>
          value ? { ...value, status: 'unavailable' } : null,
        );
      }
      if (
        positionResult.status === 'fulfilled' &&
        positionResult.value.data?.success
      ) {
        setPositions(positionResult.value.data.data);
      }
      if (
        roundResult.status === 'fulfilled' &&
        roundResult.value.data?.success
      ) {
        setRounds(roundResult.value.data.data);
      }
      setLoading(false);
      timer = setTimeout(load, 3000);
    };
    const visibility = () => {
      clearTimeout(timer);
      controller?.abort();
      generation += 1;
      if (!document.hidden) load();
    };
    load();
    document.addEventListener('visibilitychange', visibility);
    return () => {
      alive = false;
      clearTimeout(timer);
      controller?.abort();
      document.removeEventListener('visibilitychange', visibility);
    };
  }, [filter, page, roundPage, refreshKey, t]);

  useEffect(() => {
    const timer = setInterval(() => {
      if (!document.hidden) setNow(Date.now());
    }, 1000);
    return () => clearInterval(timer);
  }, []);

  const serverNow = Math.floor((now + clockOffset) / 1000);
  const round = market?.round;
  const remaining = Math.max(0, Number(round?.end_time || 0) - serverNow);
  const ready =
    market?.status === 'ready' &&
    !!round &&
    remaining > 0 &&
    serverNow - Number(market.updated_at) <= 6;
  const quote = side === 'UP' ? market?.up : market?.down;
  const maxPrice =
    priceInput?.round === round?.window_start && priceInput?.side === side
      ? priceInput.value
      : quote?.ask || '';
  const spendable = Math.max(
    0,
    Math.min(
      Number(self?.account?.cash || 0),
      Number(self?.valuation?.cross?.available ?? self?.account?.cash ?? 0),
    ),
  );
  const validBuy =
    ready &&
    market?.enabled &&
    predictionNumber(amount) &&
    Number(amount) >= market.min_amount_usd &&
    Number(amount) <= market.max_amount_usd &&
    Number(amount) <= spendable / perUnit &&
    predictionNumber(maxPrice) &&
    Number(maxPrice) > 0 &&
    Number(maxPrice) < 1;
  const estimatePrice = Number(quote?.ask);
  const estimatedShares =
    ready && estimatePrice > 0 && predictionNumber(amount)
      ? Number(amount) /
        (estimatePrice +
          Number(market.fee_rate) * estimatePrice * (1 - estimatePrice))
      : null;
  const validSale =
    ready &&
    sellPosition?.window_start === round?.window_start &&
    predictionNumber(sellShares, 4) &&
    Number(sellShares) > 0 &&
    Number(sellShares) <= Number(sellPosition?.shares) &&
    predictionNumber(sellPrice) &&
    Number(sellPrice) > 0 &&
    Number(sellPrice) < 1;

  const buy = async () => {
    if (!validBuy || submitting) return;
    setSubmitting(true);
    const result = await tradePost(
      '/api/trade/prediction/orders',
      {
        window_start: round.window_start,
        side,
        amount,
        max_price: maxPrice,
      },
      t,
    );
    if (!mounted.current) return;
    setSubmitting(false);
    if (result.error) {
      showError(result.error);
    } else {
      showSuccess(t('预测买入已成交'));
      onAccountChanged?.();
    }
    setRefreshKey((value) => value + 1);
  };

  const sell = async () => {
    if (!validSale || submitting) return;
    setSubmitting(true);
    const result = await tradePost(
      `/api/trade/prediction/positions/${sellPosition.id}/sell`,
      { shares: sellShares, min_price: sellPrice },
      t,
    );
    if (!mounted.current) return;
    setSubmitting(false);
    if (result.error) {
      showError(result.error);
    } else {
      showSuccess(t('预测卖出已成交'));
      setSellPosition(null);
      onAccountChanged?.();
    }
    setRefreshKey((value) => value + 1);
  };

  const positionColumns = [
    {
      title: t('预测轮次'),
      dataIndex: 'window_start',
      render: (value) => timestamp2string(value),
    },
    {
      title: t('方向'),
      dataIndex: 'side',
      render: (value) => (
        <Tag color={value === 'UP' ? 'green' : 'red'}>
          {value === 'UP' ? t('看涨') : t('看跌')}
        </Tag>
      ),
    },
    {
      title: t('剩余份额'),
      dataIndex: 'shares',
      render: (value) => formatQty(value, 4),
    },
    {
      title: t('累计投入'),
      dataIndex: 'total_cost',
      render: (value) => `${formatUsdt(value, perUnit, 4)} USDT`,
    },
    {
      title: t('已收回金额'),
      dataIndex: 'payout',
      render: (value) => `${formatUsdt(value, perUnit, 4)} USDT`,
    },
    {
      title: t('已实现盈亏'),
      render: (_, position) =>
        `${formatSignedUsdt(position.payout - (position.total_cost - position.cost), perUnit, 4)} USDT`,
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      render: (value, position) =>
        value === 'active' && position.window_start + 300 <= serverNow
          ? t('等待官方结算')
          : t(POSITION_STATUS[value] || '持有中'),
    },
    {
      title: t('操作'),
      render: (_, position) =>
        position.status === 'active' ? (
          <Button
            size='small'
            disabled={
              !ready ||
              position.window_start !== round?.window_start ||
              submitting
            }
            onClick={() => {
              setSellShares(position.shares);
              setSellPrice(
                (position.side === 'UP' ? market.up.bid : market.down.bid) ||
                  '',
              );
              setSellPosition(position);
            }}
          >
            {t('卖出')}
          </Button>
        ) : null,
    },
  ];

  return (
    <div className='flex flex-col gap-4'>
      {error && <Banner type='danger' description={error} closeIcon={null} />}
      <div className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_360px]'>
        <div className='trade-card flex min-w-0 flex-col gap-4'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <Title heading={5}>{t('BTC 五分钟涨跌预测')}</Title>
            <Button
              size='small'
              icon={<RefreshCw size={14} />}
              aria-label={t('刷新')}
              loading={loading}
              onClick={() => setRefreshKey((value) => value + 1)}
            />
          </div>
          <Text type='tertiary'>
            {t(
              '使用模拟盘资金交易，行情和结果来自 Polymarket，不会向外部市场下单。',
            )}
          </Text>
          {round && (
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <Text>
                {timestamp2string(round.window_start)} –{' '}
                {timestamp2string(round.end_time)}
              </Text>
              <Tag color={remaining > 0 ? 'blue' : 'grey'}>
                {remaining > 0
                  ? t('本轮剩余 {{time}}', {
                      time: `${Math.floor(remaining / 60)}:${String(remaining % 60).padStart(2, '0')}`,
                    })
                  : t('等待下一轮行情')}
              </Tag>
            </div>
          )}
          {!ready && (
            <Banner
              type='warning'
              closeIcon={null}
              description={
                market?.status === 'disabled'
                  ? t('预测买入已关闭，已有持仓仍会自动结算。')
                  : t('预测行情暂不可用，已暂停下单；未决持仓等待官方结果。')
              }
            />
          )}
          <div className='grid grid-cols-2 gap-3'>
            {['UP', 'DOWN'].map((direction) => {
              const prices = direction === 'UP' ? market?.up : market?.down;
              return (
                <div
                  key={direction}
                  className='rounded-xl border border-[var(--semi-color-border)] p-4'
                >
                  <Text
                    strong
                    className={direction === 'UP' ? 'trade-up' : 'trade-down'}
                  >
                    {direction === 'UP' ? t('看涨') : t('看跌')}
                  </Text>
                  <div className='trade-num mt-3 text-3xl font-semibold'>
                    {ready && prices?.ask
                      ? `${(Number(prices.ask) * 100).toFixed(1)}%`
                      : '—'}
                  </div>
                  <div className='mt-2 text-xs text-[var(--semi-color-text-2)]'>
                    {t('买入价')} {ready && prices?.ask ? prices.ask : '—'} /{' '}
                    {t('卖出价')} {ready && prices?.bid ? prices.bid : '—'} USDT
                  </div>
                </div>
              );
            })}
          </div>
          <Text size='small' type='tertiary'>
            {t(
              '预测正确每份结算 1 USDT，错误为 0；结束后仅按官方确认结果结算，未决时继续等待。',
            )}
          </Text>
          <Text size='small' type='tertiary'>
            {t(
              '可在本轮结束前卖出；买卖按公开盘口深度成交，限价之外或深度不足的份额不会成交。',
            )}
          </Text>
          <a
            href={
              round?.slug
                ? `https://polymarket.com/event/${encodeURIComponent(round.slug)}`
                : 'https://polymarket.com'
            }
            target='_blank'
            rel='noopener noreferrer'
            className='inline-flex items-center gap-1 text-sm text-blue-500'
          >
            {t('查看官方市场与规则')}{' '}
            <ExternalLink size={13} aria-hidden='true' />
          </a>
        </div>

        <div className='trade-card flex flex-col gap-4'>
          <Text strong>{t('买入预测份额')}</Text>
          {!market?.enabled && ready && (
            <Text type='warning'>
              {t('预测买入已关闭，已有持仓仍会自动结算。')}
            </Text>
          )}
          <RadioGroup
            type='button'
            value={side}
            disabled={submitting}
            onChange={(event) => setSide(event.target.value)}
          >
            <Radio value='UP'>{t('看涨')}</Radio>
            <Radio value='DOWN'>{t('看跌')}</Radio>
          </RadioGroup>
          <label className='flex flex-col gap-2'>
            <Text size='small'>{t('投入预算（含手续费）')}</Text>
            <Input
              value={amount}
              onChange={setAmount}
              inputMode='decimal'
              suffix='USDT'
              disabled={submitting}
              aria-label={t('投入预算（含手续费）')}
            />
          </label>
          <label className='flex flex-col gap-2'>
            <Text size='small'>{t('最高接受买入价')}</Text>
            <Input
              value={maxPrice}
              onChange={(value) =>
                setPriceInput({ round: round?.window_start, side, value })
              }
              inputMode='decimal'
              suffix='USDT'
              disabled={submitting}
              aria-label={t('最高接受买入价')}
            />
          </label>
          <Text type='tertiary' size='small'>
            {t('可用资金')} {formatUsdt(spendable, perUnit)} USDT
          </Text>
          <Text type='tertiary' size='small'>
            {t('每笔预算 {{min}}–{{max}} USDT，未平仓成本上限 {{cap}} USDT。', {
              min: market?.min_amount_usd ?? 1,
              max: market?.max_amount_usd ?? self?.max_order_usd ?? '—',
              cap: market?.max_position_usd ?? self?.max_position_usd ?? '—',
            })}
          </Text>
          <Text size='small'>
            {t('预计份额')}{' '}
            {estimatedShares === null ? '—' : formatQty(estimatedShares, 4)}
          </Text>
          <Text type='tertiary' size='small'>
            {t(
              '预计份额按当前卖一价计算；手续费 = 份额 × {{rate}} × 价格 × (1 − 价格)，实际金额以成交为准。',
              { rate: market?.fee_rate || '0.07' },
            )}
          </Text>
          <Button
            theme='solid'
            block
            loading={submitting && !sellPosition}
            disabled={!validBuy || submitting}
            onClick={buy}
          >
            {t('买入预测份额')}
          </Button>
        </div>
      </div>

      <div className='trade-card flex flex-col gap-3'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <Text strong>{t('预测持仓与记录')}</Text>
          <RadioGroup
            type='button'
            value={filter}
            onChange={(event) => {
              setFilter(event.target.value);
              setPage(1);
            }}
          >
            <Radio value='active'>{t('当前持仓')}</Radio>
            <Radio value='all'>{t('全部记录')}</Radio>
          </RadioGroup>
        </div>
        <Table
          size='small'
          rowKey='id'
          columns={positionColumns}
          dataSource={positions.items || []}
          pagination={false}
          loading={loading}
          scroll={{ x: 940 }}
          empty={t('暂无预测持仓')}
        />
        <Pagination
          currentPage={page}
          pageSize={PAGE_SIZE}
          total={positions.total || 0}
          onPageChange={setPage}
        />
      </div>

      <div className='trade-card flex flex-col gap-3'>
        <Text strong>{t('预测轮次历史')}</Text>
        <Table
          size='small'
          rowKey='window_start'
          pagination={false}
          dataSource={rounds.items || []}
          empty={t('暂无预测轮次')}
          scroll={{ x: 520 }}
          columns={[
            {
              title: t('开始时间'),
              dataIndex: 'window_start',
              render: timestamp2string,
            },
            {
              title: t('结束时间'),
              dataIndex: 'end_time',
              render: timestamp2string,
            },
            {
              title: t('状态'),
              render: (_, item) =>
                item.status === 'settled'
                  ? t('已结算')
                  : item.end_time <= serverNow
                    ? t('等待官方结算')
                    : t('进行中'),
            },
            {
              title: t('结果'),
              render: (_, item) =>
                item.outcome === 'UP'
                  ? t('看涨')
                  : item.outcome === 'DOWN'
                    ? t('看跌')
                    : item.outcome === 'SPLIT'
                      ? t('五五开')
                      : '—',
            },
          ]}
        />
        <Pagination
          currentPage={roundPage}
          pageSize={PAGE_SIZE}
          total={rounds.total || 0}
          onPageChange={setRoundPage}
        />
      </div>

      <Modal
        visible={!!sellPosition}
        title={t('卖出预测份额')}
        onCancel={() => {
          if (!submitting) setSellPosition(null);
        }}
        closeOnEsc={!submitting}
        maskClosable={!submitting}
        footer={
          <Button
            theme='solid'
            loading={submitting}
            disabled={!validSale || submitting}
            onClick={sell}
          >
            {t('确认卖出')}
          </Button>
        }
      >
        <div className='flex flex-col gap-4'>
          <Text>
            {t('可卖份额')} {formatQty(sellPosition?.shares, 4)}
          </Text>
          <label className='flex flex-col gap-2'>
            <Text>{t('卖出份额')}</Text>
            <Input
              value={sellShares}
              onChange={setSellShares}
              inputMode='decimal'
              disabled={submitting}
              aria-label={t('卖出份额')}
              suffix={
                <Button
                  size='small'
                  theme='borderless'
                  disabled={submitting}
                  onClick={() => setSellShares(sellPosition?.shares || '')}
                >
                  {t('全部')}
                </Button>
              }
            />
          </label>
          <label className='flex flex-col gap-2'>
            <Text>{t('最低接受卖出价')}</Text>
            <Input
              value={sellPrice}
              onChange={setSellPrice}
              inputMode='decimal'
              suffix='USDT'
              disabled={submitting}
              aria-label={t('最低接受卖出价')}
            />
          </label>
          {!ready && (
            <Text type='warning'>
              {t('预测行情暂不可用，已暂停下单；未决持仓等待官方结果。')}
            </Text>
          )}
        </div>
      </Modal>
    </div>
  );
};

export default PredictionView;
