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
  Input,
  Radio,
  RadioGroup,
  Typography,
} from '@douyinfe/semi-ui';
import { showError, showSuccess } from '../../helpers';
import {
  decimalsOf,
  floorToStep,
  formatPrice,
  formatQty,
  toUsdt,
  tradePost,
} from './api';

const { Text } = Typography;
const PERCENTS = [0.25, 0.5, 0.75, 1];

// 下单面板：买入或卖出，市价或限价；市价买入可以按数量也可以按金额。百分比按钮按可用资金(买入)或可卖数量(卖出)填写，
// 卖出 100% 是全部可卖数量。估算按当前盘口的最优价算，实际按盘口逐档成交。
const OrderPanel = ({
  symbol,
  ticker,
  rules,
  quote,
  cash,
  position,
  perUnit,
  feeBps,
  pickedPrice,
  onPlaced,
  t,
}) => {
  const [side, setSide] = useState('buy');
  const [type, setType] = useState('market');
  const [byAmount, setByAmount] = useState(false);
  const [price, setPrice] = useState('');
  const [qty, setQty] = useState('');
  const [amount, setAmount] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const step = rules?.step_size;
  const tick = rules?.tick_size;
  const priceDigits = decimalsOf(tick);
  const fee = (feeBps || 0) / 10000;
  const isBuy = side === 'buy';
  const amountMode = isBuy && type === 'market' && byAmount;

  useEffect(() => {
    setPrice('');
    setQty('');
    setAmount('');
  }, [symbol]);

  // 点盘口上的价格时改成限价单并填上这个价格。
  useEffect(() => {
    if (pickedPrice) {
      setType('limit');
      setPrice(pickedPrice.price);
    }
  }, [pickedPrice]);

  const cashUsdt = toUsdt(cash, perUnit);
  const heldQty = Math.max(
    Number(position?.qty || 0) - Number(position?.frozen_qty || 0),
    0,
  );
  const marketPrice =
    Number(isBuy ? quote?.ask || quote?.price : quote?.bid || quote?.price) ||
    0;
  const refPrice = type === 'limit' ? Number(price) || 0 : marketPrice;
  // 按金额买入时填的金额含手续费，成交额是扣掉手续费后的部分。
  const notional = amountMode
    ? (Number(amount) || 0) / (1 + fee)
    : (Number(qty) || 0) * refPrice;
  const estimatedFee = notional * fee;
  const total = isBuy ? notional + estimatedFee : notional - estimatedFee;
  const minNotional = Number(rules?.min_notional || 0);

  const applyPercent = (percent) => {
    if (amountMode) {
      setAmount((Math.floor(cashUsdt * percent * 100) / 100).toFixed(2));
      return;
    }
    if (isBuy) {
      if (refPrice <= 0) return;
      setQty(floorToStep((cashUsdt * percent) / (refPrice * (1 + fee)), step));
      return;
    }
    setQty(
      percent === 1 ? String(heldQty) : floorToStep(heldQty * percent, step),
    );
  };

  const submit = async () => {
    const body = { symbol, side, type };
    if (amountMode) {
      body.amount = amount;
    } else {
      body.qty = qty;
    }
    if (type === 'limit') body.price = price;
    setSubmitting(true);
    const res = await tradePost('/api/trade/orders', body, t);
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
    setAmount('');
    onPlaced?.();
  };

  const ready = amountMode
    ? Number(amount) > 0
    : Number(qty) > 0 && (type === 'market' || Number(price) > 0);

  return (
    <div className='trade-card flex flex-col gap-3'>
      <div className='grid grid-cols-2 gap-2'>
        <Button
          theme={isBuy ? 'solid' : 'light'}
          type={isBuy ? 'primary' : 'tertiary'}
          onClick={() => setSide('buy')}
          style={
            isBuy ? { background: 'var(--semi-color-success)' } : undefined
          }
        >
          {t('买入')}
        </Button>
        <Button
          theme={!isBuy ? 'solid' : 'light'}
          type={!isBuy ? 'danger' : 'tertiary'}
          onClick={() => setSide('sell')}
        >
          {t('卖出')}
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
      {isBuy && type === 'market' && (
        <RadioGroup
          type='button'
          size='small'
          value={byAmount ? 'amount' : 'qty'}
          onChange={(e) => setByAmount(e.target.value === 'amount')}
        >
          <Radio value='qty'>{t('按数量')}</Radio>
          <Radio value='amount'>{t('按金额')}</Radio>
        </RadioGroup>
      )}
      {amountMode ? (
        <Input
          value={amount}
          onChange={setAmount}
          prefix={t('买入金额')}
          aria-label={t('买入金额')}
          suffix='USDT'
          inputMode='decimal'
        />
      ) : (
        <Input
          value={qty}
          onChange={setQty}
          prefix={t('委托数量')}
          aria-label={t('委托数量')}
          suffix={ticker}
          inputMode='decimal'
        />
      )}
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
      <div className='flex flex-col gap-1 text-xs'>
        <div className='flex justify-between'>
          <Text type='tertiary' size='small'>
            {isBuy ? t('可用资金') : t('可卖数量')}
          </Text>
          <Text size='small' className='trade-num'>
            {isBuy
              ? `${cashUsdt.toFixed(2)} USDT`
              : `${formatQty(heldQty)} ${ticker}`}
          </Text>
        </div>
        <div className='flex justify-between'>
          <Text type='tertiary' size='small'>
            {t('预计成交额')}
          </Text>
          <Text size='small' className='trade-num'>
            {notional > 0 ? `${notional.toFixed(2)} USDT` : '--'}
          </Text>
        </div>
        <div className='flex justify-between'>
          <Text type='tertiary' size='small'>
            {t('手续费')} ({(fee * 100).toFixed(2)}%)
          </Text>
          <Text size='small' className='trade-num'>
            {notional > 0 ? `${estimatedFee.toFixed(4)} USDT` : '--'}
          </Text>
        </div>
        <div className='flex justify-between'>
          <Text type='tertiary' size='small'>
            {isBuy ? t('合计花费') : t('预计到账')}
          </Text>
          <Text size='small' strong className='trade-num'>
            {notional > 0 ? `${total.toFixed(2)} USDT` : '--'}
          </Text>
        </div>
        {notional > 0 && notional < minNotional && (
          <Text type='warning' size='small'>
            {t('单笔至少 {{min}} USDT', { min: minNotional })}
          </Text>
        )}
      </div>
      <Button
        theme='solid'
        type={isBuy ? 'primary' : 'danger'}
        style={isBuy ? { background: 'var(--semi-color-success)' } : undefined}
        loading={submitting}
        disabled={!ready}
        onClick={submit}
      >
        {isBuy
          ? t('买入 {{ticker}}', { ticker })
          : t('卖出 {{ticker}}', { ticker })}
      </Button>
      <Text type='tertiary' size='small'>
        {type === 'market'
          ? t('市价单按 Binance 实时盘口逐档成交，盘口不够时剩余部分撤销。')
          : t(
              '限价单按限价成交，盘口达到限价前会冻结资金或数量，可以随时撤单。',
            )}
      </Text>
    </div>
  );
};

export default OrderPanel;
