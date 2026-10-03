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
import {
  Button,
  Checkbox,
  Input,
  Radio,
  RadioGroup,
  Slider,
  Typography,
} from '@douyinfe/semi-ui';
import { showError, showSuccess } from '../../helpers';
import {
  decimalsOf,
  estimateLiquidationPrice,
  floorToStep,
  formatPrice,
  formatQty,
  toUsdt,
  tradePost,
  trendClass,
} from './api';

const { Text } = Typography;
const PERCENTS = [0.25, 0.5, 0.75, 1];
const LEVERAGE_KEY = 'trade-futures-leverage';

// 合约下单面板：开仓或平仓，做多或做空，市价或限价。开仓时选杠杆(这个方向已有仓位时沿用仓位的杠杆)，可以同时设止盈止损；
// 百分比按钮在开仓时按可用资金最多能开的数量填写，平仓时按可平数量填写。估算按当前盘口的最优价算，实际按盘口逐档成交。
const FuturesOrderPanel = ({
  symbol,
  ticker,
  rules,
  quote,
  cash,
  positions,
  perUnit,
  takerFeeBps,
  makerFeeBps,
  maxLeverage,
  mmrBps,
  canOpen,
  pickedPrice,
  onPlaced,
  t,
}) => {
  const [action, setAction] = useState(canOpen ? 'open' : 'close');
  const [side, setSide] = useState('long');
  const [type, setType] = useState('market');
  const [price, setPrice] = useState('');
  const [qty, setQty] = useState('');
  const [leverage, setLeverage] = useState(() => {
    try {
      const saved = Number(localStorage.getItem(LEVERAGE_KEY));
      if (saved >= 1) return saved;
    } catch {
      // 读不到就用默认值。
    }
    return 5;
  });
  const [withTpSl, setWithTpSl] = useState(false);
  const [takeProfit, setTakeProfit] = useState('');
  const [stopLoss, setStopLoss] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setPrice('');
    setQty('');
    setTakeProfit('');
    setStopLoss('');
  }, [symbol]);

  // 合约暂停开仓时只能平仓。
  useEffect(() => {
    if (!canOpen) setAction('close');
  }, [canOpen]);

  // 点盘口上的价格时改成限价单并填上这个价格。
  useEffect(() => {
    if (pickedPrice) {
      setType('limit');
      setPrice(pickedPrice.price);
    }
  }, [pickedPrice]);

  const isOpen = action === 'open';
  const isLong = side === 'long';
  const step = rules?.step_size;
  const priceDigits = decimalsOf(rules?.tick_size);
  const taker = (takerFeeBps || 0) / 10000;
  const maker = (makerFeeBps || 0) / 10000;
  const position = positions?.find((entry) => entry.side === side);
  // 同一方向的仓位只有一个杠杆，加仓沿用仓位的杠杆。
  const lockedLeverage = position?.leverage;
  const lev = lockedLeverage || Math.min(Math.max(leverage, 1), maxLeverage);
  // 开多、平空是买入，在卖一成交；开空、平多是卖出，在买一成交。
  const buying = isLong === isOpen;
  const marketPrice =
    Number(buying ? quote?.ask || quote?.price : quote?.bid || quote?.price) ||
    0;
  const refPrice = type === 'limit' ? Number(price) || 0 : marketPrice;
  const qtyNumber = Number(qty) || 0;
  const notional = qtyNumber * refPrice;
  // 限价单挂着之后成交的部分按挂单费率收，下单就成交的部分按吃单收，这里按较高的吃单估算。
  const fee = notional * taker;
  const margin = notional / lev;
  const cashUsdt = toUsdt(cash, perUnit);
  const maxOpenQty =
    refPrice > 0 ? cashUsdt / (refPrice * (1 / lev + taker)) : 0;
  const available = position
    ? Math.max(Number(position.qty) - Number(position.frozen_qty), 0)
    : 0;
  const entryPrice = Number(position?.entry_price || 0);
  const minNotional = Number(rules?.min_notional || 0);

  // 开仓后的预估强平价，已有仓位时按加仓后的整个仓位算。
  const heldQty = Number(position?.qty || 0);
  const liquidation =
    isOpen && notional > 0
      ? estimateLiquidationPrice(
          side,
          entryPrice * heldQty + notional,
          heldQty + qtyNumber,
          toUsdt(position?.margin, perUnit) + margin,
          mmrBps,
        )
      : 0;
  const closePnl =
    !isOpen && position
      ? (isLong ? refPrice - entryPrice : entryPrice - refPrice) * qtyNumber
      : 0;

  const changeLeverage = (value) => {
    setLeverage(value);
    try {
      localStorage.setItem(LEVERAGE_KEY, String(value));
    } catch {
      // 存不了就只在本次会话里生效。
    }
  };

  const applyPercent = (percent) => {
    if (isOpen) {
      setQty(floorToStep(maxOpenQty * percent, step));
      return;
    }
    setQty(
      percent === 1
        ? String(available)
        : floorToStep(available * percent, step),
    );
  };

  const submit = async () => {
    const body = { symbol, side, action, type, qty };
    if (type === 'limit') body.price = price;
    if (isOpen) {
      body.leverage = lev;
      if (withTpSl) {
        body.take_profit = takeProfit;
        body.stop_loss = stopLoss;
      }
    }
    setSubmitting(true);
    const res = await tradePost('/api/trade/futures/orders', body, t);
    setSubmitting(false);
    if (res.error) {
      showError(res.error);
      return;
    }
    const order = res.data;
    if (order.status === 'open' && Number(order.filled_qty) === 0) {
      showSuccess(t('限价单已挂出'));
    } else if (order.status === 'open') {
      showSuccess(
        t('已成交 {{qty}} {{ticker}}，剩余部分挂单', {
          qty: formatQty(order.filled_qty),
          ticker,
        }),
      );
    } else if (order.status === 'filled') {
      showSuccess(
        t('已成交 {{qty}} {{ticker}}，均价 {{price}}', {
          qty: formatQty(order.filled_qty),
          ticker,
          price: formatPrice(order.avg_price, priceDigits),
        }),
      );
    } else {
      showSuccess(
        t('成交 {{qty}} {{ticker}}，盘口不够的部分已撤销', {
          qty: formatQty(order.filled_qty),
          ticker,
        }),
      );
    }
    setQty('');
    onPlaced?.();
  };

  // 限价单列出挂单与吃单两个费率，估算按吃单。
  const feeLabel =
    type === 'limit'
      ? `${t('手续费')} (${(maker * 100).toFixed(2)}% / ${(taker * 100).toFixed(2)}%)`
      : `${t('手续费')} (${(taker * 100).toFixed(2)}%)`;
  const ready =
    qtyNumber > 0 &&
    (type === 'market' || Number(price) > 0) &&
    (isOpen ? canOpen : available > 0);
  let actionLabel = isLong ? t('平多') : t('平空');
  if (isOpen) actionLabel = isLong ? t('开多') : t('开空');
  const rows = isOpen
    ? [
        [t('可用资金'), `${cashUsdt.toFixed(2)} USDT`],
        [
          t('最多可开'),
          `${formatQty(floorToStep(maxOpenQty, step) || 0)} ${ticker}`,
        ],
        [t('委托价值'), notional > 0 ? `${notional.toFixed(2)} USDT` : '--'],
        [t('所需保证金'), notional > 0 ? `${margin.toFixed(2)} USDT` : '--'],
        [feeLabel, notional > 0 ? `${fee.toFixed(4)} USDT` : '--'],
        [
          t('预估强平价'),
          liquidation > 0 ? formatPrice(liquidation, priceDigits) : '--',
        ],
      ]
    : [
        [t('可平数量'), `${formatQty(available)} ${ticker}`],
        [
          t('预计盈亏'),
          notional > 0 ? (
            <span className={trendClass(closePnl)}>
              {`${closePnl > 0 ? '+' : ''}${closePnl.toFixed(2)} USDT`}
            </span>
          ) : (
            '--'
          ),
        ],
        [feeLabel, notional > 0 ? `${fee.toFixed(4)} USDT` : '--'],
      ];

  return (
    <div className='trade-card flex flex-col gap-3'>
      <RadioGroup
        type='button'
        value={action}
        onChange={(e) => setAction(e.target.value)}
      >
        <Radio value='open' disabled={!canOpen}>
          {t('开仓')}
        </Radio>
        <Radio value='close'>{t('平仓')}</Radio>
      </RadioGroup>
      <div className='grid grid-cols-2 gap-2'>
        <Button
          theme={isLong ? 'solid' : 'light'}
          type={isLong ? 'primary' : 'tertiary'}
          onClick={() => setSide('long')}
          style={
            isLong ? { background: 'var(--semi-color-success)' } : undefined
          }
        >
          {isOpen ? t('开多') : t('平多')}
        </Button>
        <Button
          theme={!isLong ? 'solid' : 'light'}
          type={!isLong ? 'danger' : 'tertiary'}
          onClick={() => setSide('short')}
        >
          {isOpen ? t('开空') : t('平空')}
        </Button>
      </div>
      <RadioGroup
        type='button'
        value={type}
        onChange={(e) => setType(e.target.value)}
      >
        <Radio value='market'>{t('市价')}</Radio>
        <Radio value='limit'>{t('限价')}</Radio>
      </RadioGroup>
      {isOpen && (
        <div className='flex flex-col gap-1'>
          <div className='flex items-center justify-between'>
            <Text type='tertiary' size='small'>
              {t('杠杆')}
            </Text>
            <Text strong className='trade-num'>
              {lev}x
            </Text>
          </div>
          <Slider
            min={1}
            max={Math.max(maxLeverage, 1)}
            step={1}
            value={lev}
            disabled={!!lockedLeverage || maxLeverage <= 1}
            tipFormatter={(value) => `${value}x`}
            onChange={changeLeverage}
          />
          {lockedLeverage && (
            <Text type='tertiary' size='small'>
              {t('这个方向已有仓位，沿用仓位的杠杆')}
            </Text>
          )}
        </div>
      )}
      {type === 'limit' && (
        <Input
          value={price}
          onChange={setPrice}
          prefix={t('委托价格')}
          aria-label={t('委托价格')}
          suffix='USDT'
          placeholder={formatPrice(quote?.price, priceDigits)}
          inputMode='decimal'
        />
      )}
      <Input
        value={qty}
        onChange={setQty}
        prefix={t('委托数量')}
        aria-label={t('委托数量')}
        suffix={ticker}
        inputMode='decimal'
      />
      <div className='grid grid-cols-4 gap-1'>
        {PERCENTS.map((percent) => (
          <Button
            key={percent}
            size='small'
            theme='light'
            type='tertiary'
            onClick={() => applyPercent(percent)}
          >
            {percent * 100}%
          </Button>
        ))}
      </div>
      {isOpen && (
        <Checkbox
          checked={withTpSl}
          onChange={(e) => setWithTpSl(e.target.checked)}
        >
          {t('止盈止损')}
        </Checkbox>
      )}
      {isOpen && withTpSl && (
        <div className='flex flex-col gap-2'>
          <Input
            value={takeProfit}
            onChange={setTakeProfit}
            prefix={t('止盈价')}
            aria-label={t('止盈价')}
            suffix='USDT'
            inputMode='decimal'
          />
          <Input
            value={stopLoss}
            onChange={setStopLoss}
            prefix={t('止损价')}
            aria-label={t('止损价')}
            suffix='USDT'
            inputMode='decimal'
          />
          <Text type='tertiary' size='small'>
            {t('按标记价格触发，触发后按市价平掉整个仓位。')}
          </Text>
        </div>
      )}
      <div className='flex flex-col gap-1 text-xs'>
        {rows.map(([label, value]) => (
          <div key={label} className='flex justify-between'>
            <Text type='tertiary' size='small'>
              {label}
            </Text>
            <Text size='small' className='trade-num'>
              {value}
            </Text>
          </div>
        ))}
        {isOpen && notional > 0 && notional < minNotional && (
          <Text type='warning' size='small'>
            {t('单笔至少 {{min}} USDT', { min: minNotional })}
          </Text>
        )}
      </div>
      <Button
        theme='solid'
        type={isLong ? 'primary' : 'danger'}
        style={isLong ? { background: 'var(--semi-color-success)' } : undefined}
        loading={submitting}
        disabled={!ready}
        onClick={submit}
      >
        {`${actionLabel} ${ticker}`}
      </Button>
      <Text type='tertiary' size='small'>
        {!isOpen
          ? t('平仓数量不能超过可平数量，挂着的限价平仓单会占用可平数量。')
          : type === 'market'
            ? t('市价单按 Binance 实时盘口逐档成交，盘口不够时剩余部分撤销。')
            : t(
                '限价开仓会冻结保证金和手续费，挂着之后成交的部分按挂单费率收费，可以随时撤单。',
              )}
      </Text>
    </div>
  );
};

export default FuturesOrderPanel;
