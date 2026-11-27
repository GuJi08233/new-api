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

import React, { useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Input,
  Radio,
  RadioGroup,
  Select,
  Slider,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { Grid3x3, Rocket, Trash2 } from 'lucide-react';
import { showError, showSuccess } from '../../helpers';
import { formatPrice, toUsdt, tradeGet, tradePost, trendClass } from './api';
import { formatSymbolLabel } from './symbols';
import { buildGridOrderPlan } from './strategies';

const { Text, Title } = Typography;

const RANGE_PRESETS = [
  { label: '±3% 震荡', pct: 0.03 },
  { label: '±5% 稳健', pct: 0.05 },
  { label: '±10% 宽幅', pct: 0.1 },
];

// 智能网格策略：将价格区间自动拆分为多档真实限价挂单，震荡市自动低买高卖，支持现货低吸网格与合约双向中性网格。
const GridBotView = ({
  self,
  perUnit,
  spotMarket,
  futuresMarket,
  spotQuotes,
  futuresQuotes,
  onAccountChanged,
  t,
}) => {
  const [market, setMarket] = useState('spot');
  const [symbol, setSymbol] = useState('');
  const [lower, setLower] = useState('');
  const [upper, setUpper] = useState('');
  const [gridCount, setGridCount] = useState(6);
  const [spacingMode, setSpacingMode] = useState('arithmetic');
  const [totalUsdt, setTotalUsdt] = useState('200');
  const [leverage, setLeverage] = useState(3);
  const [deploying, setDeploying] = useState(false);
  const [openOrders, setOpenOrders] = useState([]);
  const [cancelingAll, setCancelingAll] = useState(false);

  const activeSymbols = useMemo(
    () =>
      (
        (market === 'spot' ? spotMarket?.symbols : futuresMarket?.symbols) || []
      ).filter((s) => s.open !== false),
    [market, spotMarket, futuresMarket],
  );

  useEffect(() => {
    if (!activeSymbols.length) return;
    if (!activeSymbols.some((s) => s.symbol === symbol)) {
      setSymbol(activeSymbols[0].symbol);
    }
  }, [activeSymbols, symbol]);

  const item = useMemo(
    () => activeSymbols.find((s) => s.symbol === symbol) || null,
    [activeSymbols, symbol],
  );
  const quote =
    (market === 'spot' ? spotQuotes : futuresQuotes)?.[symbol] ||
    item?.quote ||
    {};
  const currentPrice = Number(quote.price || 0);

  const applyPreset = (pct) => {
    if (!(currentPrice > 0)) return;
    const digits = currentPrice >= 1000 ? 2 : currentPrice >= 1 ? 3 : 5;
    setLower((currentPrice * (1 - pct)).toFixed(digits));
    setUpper((currentPrice * (1 + pct)).toFixed(digits));
  };

  // 切换品种时自动填充默认 ±5% 区间
  useEffect(() => {
    if (currentPrice > 0) {
      const digits = currentPrice >= 1000 ? 2 : currentPrice >= 1 ? 3 : 5;
      setLower((currentPrice * 0.95).toFixed(digits));
      setUpper((currentPrice * 1.05).toFixed(digits));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [symbol, market]);

  const loadOpenOrders = async () => {
    if (!symbol) return;
    const url =
      market === 'spot' ? '/api/trade/orders' : '/api/trade/futures/orders';
    const res = await tradeGet(url, t, {
      symbol,
      status: 'open',
      p: 1,
      page_size: 30,
    });
    if (res.data) setOpenOrders(res.data.items || []);
  };

  useEffect(() => {
    loadOpenOrders();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [symbol, market]);

  const feeBps =
    market === 'spot'
      ? (spotMarket?.fee_bps ?? 10)
      : (futuresMarket?.maker_fee_bps ?? 2);

  const plan = useMemo(
    () =>
      buildGridOrderPlan({
        market,
        symbolItem: item,
        currentPrice,
        lowerPrice: lower,
        upperPrice: upper,
        gridCount,
        mode: spacingMode,
        totalUsdt,
        leverage,
        feeBps,
      }),
    [
      market,
      item,
      currentPrice,
      lower,
      upper,
      gridCount,
      spacingMode,
      totalUsdt,
      leverage,
      feeBps,
    ],
  );

  const deployGrid = async () => {
    if (!plan.valid || !plan.orders.length) return;
    setDeploying(true);
    let ok = 0;
    let firstErr = '';
    for (const order of plan.orders) {
      const res = await tradePost(order.endpoint, order.payload, t);
      if (res.error) {
        if (!firstErr) firstErr = res.error;
      } else {
        ok++;
      }
    }
    setDeploying(false);
    onAccountChanged?.();
    loadOpenOrders();
    if (ok === plan.orders.length) {
      showSuccess(
        t('网格部署成功：已挂出 {{count}} 档真实限价单', { count: ok }),
      );
    } else {
      showError(
        t('网格部分挂出：{{ok}}/{{total}} 成功（{{err}}）', {
          ok,
          total: plan.orders.length,
          err: firstErr,
        }),
      );
    }
  };

  const cancelAllForSymbol = async () => {
    if (!openOrders.length) return;
    setCancelingAll(true);
    const prefix =
      market === 'spot' ? '/api/trade/orders' : '/api/trade/futures/orders';
    for (const ord of openOrders) {
      await tradePost(`${prefix}/${ord.id}/cancel`, {}, t);
    }
    setCancelingAll(false);
    showSuccess(t('已撤销当前品种全部挂单'));
    onAccountChanged?.();
    loadOpenOrders();
  };

  const cashUsdt = toUsdt(self?.account?.cash, perUnit);
  const symbolOptions = activeSymbols.map((s) => ({
    value: s.symbol,
    label: `${formatSymbolLabel(s, t)} (${s.symbol})`,
  }));

  return (
    <div className='grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_380px]'>
      <div className='flex flex-col gap-4'>
        <div className='trade-card flex flex-col gap-4'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='flex items-center gap-2'>
              <Grid3x3 size={18} className='text-[var(--semi-color-primary)]' />
              <Title heading={6} className='!mb-0'>
                {t('智能网格策略生成器')}
              </Title>
            </div>
            <RadioGroup
              type='button'
              size='small'
              value={market}
              onChange={(e) => setMarket(e.target.value)}
            >
              <Radio value='spot'>{t('现货低吸网格')}</Radio>
              <Radio value='futures' disabled={!self?.futures?.enabled}>
                {t('合约双向中性网格')}
              </Radio>
            </RadioGroup>
          </div>

          <div className='flex flex-wrap items-center gap-3'>
            <Select
              filter
              style={{ width: 250 }}
              value={symbol}
              optionList={symbolOptions}
              onChange={setSymbol}
            />
            <Text type='secondary' size='small' className='trade-num'>
              {t('当前市价')}：
              <b>
                {currentPrice > 0 ? `${formatPrice(currentPrice)} USDT` : '--'}
              </b>
            </Text>
          </div>

          <div className='flex flex-wrap items-center gap-2'>
            <Text type='tertiary' size='small'>
              {t('快捷区间')}：
            </Text>
            {RANGE_PRESETS.map((p) => (
              <Button
                key={p.label}
                size='small'
                theme='light'
                onClick={() => applyPreset(p.pct)}
              >
                {t(p.label)}
              </Button>
            ))}
          </div>

          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <Input
              value={lower}
              onChange={setLower}
              prefix={t('区间下限')}
              suffix='USDT'
              inputMode='decimal'
            />
            <Input
              value={upper}
              onChange={setUpper}
              prefix={t('区间上限')}
              suffix='USDT'
              inputMode='decimal'
            />
          </div>

          <div className='flex flex-col gap-1'>
            <div className='flex justify-between text-xs'>
              <Text type='tertiary' size='small'>
                {t('网格数量 (3–16 格)')}
              </Text>
              <Text strong className='trade-num'>
                {gridCount} {t('格')}
              </Text>
            </div>
            <Slider
              min={3}
              max={16}
              value={gridCount}
              onChange={setGridCount}
            />
          </div>

          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <div className='flex flex-col gap-1'>
              <Text type='tertiary' size='small'>
                {t('分格模式')}
              </Text>
              <RadioGroup
                type='button'
                size='small'
                value={spacingMode}
                onChange={(e) => setSpacingMode(e.target.value)}
              >
                <Radio value='arithmetic'>{t('等差网格')}</Radio>
                <Radio value='geometric'>{t('等比网格')}</Radio>
              </RadioGroup>
            </div>
            <Input
              value={totalUsdt}
              onChange={setTotalUsdt}
              prefix={market === 'spot' ? t('总投入') : t('总保证金')}
              suffix='USDT'
              inputMode='decimal'
            />
          </div>

          {market === 'futures' && (
            <div className='flex flex-col gap-1'>
              <div className='flex justify-between text-xs'>
                <Text type='tertiary' size='small'>
                  {t('网格杠杆')}
                </Text>
                <Text strong className='trade-num'>
                  {leverage}x
                </Text>
              </div>
              <Slider
                min={1}
                max={Math.min(10, item?.max_leverage || 10)}
                value={leverage}
                onChange={setLeverage}
              />
            </div>
          )}

          {plan.feeWarning && (
            <Banner
              type='warning'
              closeIcon={null}
              description={t(
                '当前单格利润率不足以覆盖双边手续费，建议放宽价格区间或减少格数。',
              )}
            />
          )}
        </div>

        <div className='trade-card flex flex-col gap-2'>
          <div className='flex items-center justify-between'>
            <Title heading={6} className='!mb-0'>
              {t('当前品种在途网格/限价单 ({{count}})', {
                count: openOrders.length,
              })}
            </Title>
            {openOrders.length > 0 && (
              <Button
                size='small'
                type='danger'
                theme='light'
                icon={<Trash2 size={13} />}
                loading={cancelingAll}
                onClick={cancelAllForSymbol}
              >
                {t('一键全部撤单')}
              </Button>
            )}
          </div>
          <Table
            size='small'
            rowKey='id'
            pagination={false}
            dataSource={openOrders}
            columns={[
              {
                title: t('方向'),
                dataIndex: 'side',
                render: (s) => (
                  <Tag color={s === 'buy' || s === 'long' ? 'green' : 'red'}>
                    {s === 'buy'
                      ? t('买入')
                      : s === 'long'
                        ? t('开多')
                        : s === 'short'
                          ? t('开空')
                          : t('卖出')}
                  </Tag>
                ),
              },
              { title: t('限价'), dataIndex: 'price', align: 'right' },
              { title: t('数量'), dataIndex: 'qty', align: 'right' },
            ]}
            empty={<Text type='tertiary'>{t('当前品种暂无挂单')}</Text>}
          />
        </div>
      </div>

      <div className='trade-card flex flex-col gap-3'>
        <div className='flex items-center justify-between'>
          <Text strong>
            {t('网格预览 ({{count}} 笔挂单)', { count: plan.orders.length })}
          </Text>
          <Text type='tertiary' size='small' className='trade-num'>
            {t('可用 {{amount}} USDT', { amount: cashUsdt.toFixed(2) })}
          </Text>
        </div>
        <div className='trade-calc-result grid grid-cols-2 gap-2 text-xs trade-num'>
          <div>
            <Text type='tertiary' size='small'>
              {t('单格净收益率')}
            </Text>
            <div
              className={`font-bold text-sm ${trendClass(plan.profitPerGridPct)}`}
            >
              {plan.profitPerGridPct > 0 ? '+' : ''}
              {plan.profitPerGridPct.toFixed(2)}%
            </div>
          </div>
          <div>
            <Text type='tertiary' size='small'>
              {t('平均格间距')}
            </Text>
            <div className='font-bold text-sm'>
              {(plan.avgSpacingPct || 0).toFixed(2)}%
            </div>
          </div>
        </div>

        <div className='flex flex-col gap-1 max-h-[300px] overflow-y-auto'>
          {plan.orders.map((o, idx) => (
            <div
              key={`${o.price}-${idx}`}
              className='flex items-center justify-between text-xs trade-num py-1.5 px-2 rounded bg-[var(--semi-color-fill-0)]'
            >
              <Tag
                size='small'
                color={o.side === 'buy' || o.side === 'long' ? 'green' : 'red'}
              >
                {o.side === 'buy'
                  ? t('限价买')
                  : o.side === 'long'
                    ? t('限价多')
                    : t('限价空')}
              </Tag>
              <span>{o.price} USDT</span>
              <span>
                {o.qty || '--'} {item?.ticker}
              </span>
              {o.error && (
                <Tag size='small' color='red'>
                  {t('额不足')}
                </Tag>
              )}
            </div>
          ))}
        </div>

        <Button
          size='large'
          theme='solid'
          icon={<Rocket size={15} />}
          loading={deploying}
          disabled={!plan.valid || !self?.enabled || !plan.orders.length}
          onClick={deployGrid}
        >
          {t('一键部署网格 ({{count}} 笔)', { count: plan.orders.length })}
        </Button>
      </div>
    </div>
  );
};

export default GridBotView;
