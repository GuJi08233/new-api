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
  Button,
  Input,
  Radio,
  RadioGroup,
  Slider,
  Typography,
} from '@douyinfe/semi-ui';
import { showError, showSuccess } from '../../helpers';
import FuturesLevelsEditor, {
  levelsIncomplete,
  levelsPayload,
  levelsQty,
} from './FuturesLevelsEditor';
import {
  bracketFor,
  decimalsOf,
  estimateLiquidationPrice,
  floorToStep,
  formatPrice,
  formatQty,
  leverageMarks,
  toUsdt,
  tradePost,
} from './api';

const { Text } = Typography;
const PERCENTS = [0.25, 0.5, 0.75, 1];
const DEFAULT_LEVERAGE = 10;
const emptyRows = () => [{ price: '', qty: '' }];

// 合约开仓面板：做多或做空，全仓或逐仓，市价或限价，选杠杆、填保证金(或开仓数量)，可以同时设多档止盈止损。这个合约已有
// 仓位时保证金模式与杠杆跟着仓位走：模式锁定，改杠杆要先点"应用"(多空两边一起改)。估算按当前盘口的最优价算，实际按盘口逐档
// 成交；强平价按 Binance 的风险限额档位估算，全仓按整个账户的权益估算。平仓、反手、调整保证金与止盈止损在仓位卡片上。
const FuturesOrderPanel = ({
  symbol,
  ticker,
  rules,
  quote,
  cash,
  cross,
  positions,
  perUnit,
  takerFeeBps,
  makerFeeBps,
  brackets,
  maxLeverage,
  canOpen,
  pickedPrice,
  onPlaced,
  t,
}) => {
  const held = positions?.[0];
  const heldMode = held
    ? held.margin_mode === 'cross'
      ? 'cross'
      : 'isolated'
    : '';
  const heldLeverage = held?.leverage || 0;
  const [side, setSide] = useState('long');
  const [mode, setMode] = useState('cross');
  const [type, setType] = useState('market');
  const [price, setPrice] = useState('');
  const [amount, setAmount] = useState('');
  const [unit, setUnit] = useState('usdt');
  const [leverage, setLeverage] = useState(DEFAULT_LEVERAGE);
  const [stopRows, setStopRows] = useState(emptyRows);
  const [takeRows, setTakeRows] = useState(emptyRows);
  const [applying, setApplying] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const previousQty = useRef(0);

  useEffect(() => {
    setPrice('');
    setAmount('');
    setStopRows(emptyRows());
    setTakeRows(emptyRows());
  }, [symbol]);

  // 有仓位时杠杆跟着仓位；没有仓位时不超过这个合约能选的最高杠杆。
  useEffect(() => {
    if (heldLeverage) {
      setLeverage(heldLeverage);
      return;
    }
    setLeverage((value) =>
      Math.min(Math.max(value, 1), Math.max(maxLeverage || 1, 1)),
    );
  }, [heldLeverage, maxLeverage, symbol]);

  // 点盘口上的价格时改成限价单并填上这个价格。
  useEffect(() => {
    if (pickedPrice) {
      setType('limit');
      setPrice(pickedPrice.price);
    }
  }, [pickedPrice]);

  const effMode = heldMode || mode;
  const isCross = effMode === 'cross';
  const isLong = side === 'long';
  const step = rules?.step_size;
  const priceDigits = decimalsOf(rules?.tick_size);
  const taker = (takerFeeBps || 0) / 10000;
  const maker = (makerFeeBps || 0) / 10000;
  const pending = !!heldLeverage && leverage !== heldLeverage;
  const lev = Math.max(leverage, 1);
  const last = Number(quote?.price) || 0;
  // 开多在卖一成交，开空在买一成交。
  const bookPrice = Number(isLong ? quote?.ask : quote?.bid) || last;
  const calcPrice = type === 'limit' ? Number(price) || 0 : bookPrice;
  const input = Number(amount) || 0;
  let qtyText = '';
  if (calcPrice > 0 && input > 0) {
    qtyText = floorToStep(
      unit === 'usdt' ? (input * lev) / calcPrice : input,
      step,
    );
  }
  const qty = Number(qtyText) || 0;
  const notional = qty * calcPrice;
  const margin = notional / lev;
  const fee = notional * taker;
  const cashUsdt = toUsdt(cash, perUnit);
  const crossAvailable = cross ? toUsdt(cross.available, perUnit) : cashUsdt;
  // 全仓市价单可以用浮动盈利开仓；逐仓与限价挂单要从资金里拿出保证金(挂单是冻结)，还不能超过全仓可用。
  const budget = Math.max(
    isCross && type === 'market'
      ? crossAvailable
      : Math.min(crossAvailable, cashUsdt),
    0,
  );
  const minNotional = Number(rules?.min_notional || 0);
  const minQty = Number(rules?.min_qty || 0);

  // 开仓后的预估强平价按并进同方向仓位后的整个仓位算。全仓仓位由资金、全仓挂单冻结的钱与其他全仓仓位的浮动盈亏减去它们的
  // 维持保证金撑着。
  const same = positions?.find((entry) => entry.side === side);
  const sameQty = Number(same?.qty || 0);
  const mergedQty = sameQty + qty;
  const mergedValue = Number(same?.entry_price || 0) * sameQty + notional;
  let liquidation = 0;
  if (qty > 0 && isCross) {
    const sameCross = same?.margin_mode === 'cross';
    const backing =
      cashUsdt -
      fee +
      toUsdt(cross?.pending, perUnit) +
      toUsdt((cross?.upnl || 0) - (sameCross ? same.pnl : 0), perUnit) -
      toUsdt(
        (cross?.maintenance || 0) - (sameCross ? same.maintenance : 0),
        perUnit,
      );
    liquidation = estimateLiquidationPrice(
      side,
      mergedValue,
      mergedQty,
      backing,
      brackets,
    );
  } else if (qty > 0) {
    liquidation = estimateLiquidationPrice(
      side,
      mergedValue,
      mergedQty,
      toUsdt(same?.margin, perUnit) + margin,
      brackets,
    );
  }
  const tier = bracketFor(brackets, mergedValue);

  // 开仓数量变了时，止盈止损每一档按原来占开仓数量的比例换算，没填数量的档位按全部数量。
  useEffect(() => {
    const previous = previousQty.current;
    previousQty.current = qty;
    if (!(qty > 0)) return;
    const rescale = (rows) =>
      rows.map((row) => {
        const current = Number(row.qty);
        if (!(current > 0) || !(previous > 0)) return { ...row, qty: qtyText };
        const percent = Math.round((current / previous) * 100);
        return {
          ...row,
          qty:
            percent >= 100
              ? qtyText
              : floorToStep((qty * percent) / 100, step) || qtyText,
        };
      });
    setStopRows(rescale);
    setTakeRows(rescale);
    // 只在开仓数量变化时换算。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qtyText]);

  // 百分比按钮按可用的这一部分算保证金：保证金加手续费正好用完。
  const applyPercent = (percent) => {
    if (!(calcPrice > 0)) return;
    const target = (budget * percent) / (1 + taker * lev);
    if (unit === 'usdt') {
      setAmount(String(Math.floor(target * 100) / 100));
      return;
    }
    setAmount(floorToStep((target * lev) / calcPrice, step));
  };

  const switchUnit = (next) => {
    if (next === unit) return;
    setUnit(next);
    if (!(qty > 0)) {
      setAmount('');
      return;
    }
    setAmount(
      next === 'usdt' ? String(Math.floor(margin * 100) / 100) : qtyText,
    );
  };

  const applyLeverage = async () => {
    setApplying(true);
    const res = await tradePost(
      '/api/trade/futures/leverage',
      { symbol, leverage: lev },
      t,
    );
    setApplying(false);
    if (res.error) {
      showError(res.error);
      setLeverage(heldLeverage);
      return;
    }
    showSuccess(t('杠杆已调整为 {{leverage}}x', { leverage: lev }));
    onPlaced?.();
  };

  const submit = async () => {
    if (pending) {
      showError(t('杠杆调整未确认，请先确认或还原'));
      return;
    }
    if (type === 'limit' && !(Number(price) > 0)) {
      showError(t('请输入委托价格'));
      return;
    }
    if (!(qty > 0) || qty < minQty) {
      showError(
        t('最小下单数量 {{qty}} {{ticker}}', {
          qty: formatQty(minQty || step),
          ticker,
        }),
      );
      return;
    }
    if (notional < minNotional) {
      showError(t('最小下单金额 {{min}} USDT', { min: minNotional }));
      return;
    }
    if (levelsIncomplete(stopRows) || levelsIncomplete(takeRows)) {
      showError(t('止盈止损的每一档都要填有效的触发价和数量'));
      return;
    }
    const stopLosses = levelsPayload(stopRows);
    const takeProfits = levelsPayload(takeRows);
    if (levelsQty(stopLosses) > qty + 1e-12) {
      showError(t('止损总量超过开仓数量'));
      return;
    }
    if (levelsQty(takeProfits) > qty + 1e-12) {
      showError(t('止盈总量超过开仓数量'));
      return;
    }
    const body = {
      symbol,
      side,
      action: 'open',
      type,
      margin_mode: effMode,
      leverage: lev,
      qty: qtyText,
      take_profits: takeProfits,
      stop_losses: stopLosses,
    };
    if (type === 'limit') body.price = price;
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
        `${isLong ? t('做多开仓成功') : t('做空开仓成功')}：${t(
          '已成交 {{qty}} {{ticker}}，均价 {{price}}',
          {
            qty: formatQty(order.filled_qty),
            ticker,
            price: formatPrice(order.avg_price, priceDigits),
          },
        )}`,
      );
    } else {
      showSuccess(
        t('成交 {{qty}} {{ticker}}，盘口不够的部分已撤销', {
          qty: formatQty(order.filled_qty),
          ticker,
        }),
      );
    }
    setAmount('');
    if (type === 'limit') setPrice('');
    setStopRows(emptyRows());
    setTakeRows(emptyRows());
    onPlaced?.();
  };

  const minLeverage = heldMode === 'isolated' ? heldLeverage : 1;
  const sliderMax = Math.max(maxLeverage || 1, lev, 1);
  const marketable =
    type === 'limit' &&
    Number(price) > 0 &&
    last > 0 &&
    (isLong ? Number(price) >= last : Number(price) <= last);
  const feeLabel =
    type === 'limit'
      ? `${t('手续费')} (${(maker * 100).toFixed(2)}% / ${(taker * 100).toFixed(2)}%)`
      : `${t('手续费')} (${t('吃单')} ${(taker * 100).toFixed(2)}%)`;
  const rows = [
    [t('仓位价值'), notional > 0 ? `${notional.toFixed(2)} USDT` : '--'],
    [t('保证金'), notional > 0 ? `${margin.toFixed(2)} USDT` : '--'],
    [feeLabel, notional > 0 ? `${fee.toFixed(4)} USDT` : '--'],
    [t('合计需要'), notional > 0 ? `${(margin + fee).toFixed(2)} USDT` : '--'],
    [
      t('预估强平价'),
      <span key='liq' className='trade-warning'>
        {liquidation > 0 ? formatPrice(liquidation, priceDigits) : '--'}
      </span>,
    ],
    [
      t('维持保证金率'),
      tier
        ? t('档 {{tier}} · {{rate}}%', {
            tier: tier.tier,
            rate: (tier.mmr * 100).toFixed(2),
          })
        : '--',
    ],
  ];
  const sideLabel = isLong ? t('开多') : t('开空');

  return (
    <div className='trade-card flex flex-col gap-3'>
      <div className='flex items-center justify-between'>
        <Text strong>{t('开仓')}</Text>
        <Text type='tertiary' size='small' className='trade-num'>
          {t('可用 {{amount}} USDT', { amount: budget.toFixed(2) })}
        </Text>
      </div>
      <div className='grid grid-cols-2 gap-2'>
        <Button
          theme={isLong ? 'solid' : 'light'}
          type={isLong ? 'primary' : 'tertiary'}
          onClick={() => setSide('long')}
          style={
            isLong ? { background: 'var(--semi-color-success)' } : undefined
          }
        >
          {t('做多')}
        </Button>
        <Button
          theme={!isLong ? 'solid' : 'light'}
          type={!isLong ? 'danger' : 'tertiary'}
          onClick={() => setSide('short')}
        >
          {t('做空')}
        </Button>
      </div>
      <div className='flex items-center justify-between gap-2'>
        <Text type='tertiary' size='small'>
          {t('保证金模式')}
        </Text>
        <RadioGroup
          type='button'
          value={effMode}
          disabled={!!heldMode}
          onChange={(e) => setMode(e.target.value)}
        >
          <Radio value='cross'>{t('全仓')}</Radio>
          <Radio value='isolated'>{t('逐仓')}</Radio>
        </RadioGroup>
      </div>
      <div className='flex items-center justify-between gap-2'>
        <Text type='tertiary' size='small'>
          {t('委托类型')}
        </Text>
        <RadioGroup
          type='button'
          value={type}
          onChange={(e) => setType(e.target.value)}
        >
          <Radio value='market'>{t('市价')}</Radio>
          <Radio value='limit'>{t('限价')}</Radio>
        </RadioGroup>
      </div>
      {heldMode && (
        <Text type='tertiary' size='small'>
          {t('有持仓时无法切换保证金模式')}
        </Text>
      )}
      {type === 'limit' && (
        <div className='flex flex-col gap-1'>
          <Input
            value={price}
            onChange={setPrice}
            prefix={t('委托价格')}
            aria-label={t('委托价格')}
            suffix='USDT'
            placeholder={formatPrice(quote?.price, priceDigits)}
            inputMode='decimal'
          />
          {marketable && (
            <Text type='warning' size='small'>
              {isLong
                ? t('限价≥当前价，将立即以市价成交')
                : t('限价≤当前价，将立即以市价成交')}
            </Text>
          )}
        </div>
      )}
      <div className='flex flex-col gap-1'>
        <div className='flex items-center justify-between'>
          <Text type='tertiary' size='small'>
            {t('杠杆')}
          </Text>
          <Text
            strong
            className={`trade-num ${pending ? 'trade-warning' : ''}`}
          >
            {lev}x
          </Text>
        </div>
        <Slider
          min={minLeverage}
          max={sliderMax}
          step={1}
          value={lev}
          marks={leverageMarks(minLeverage, sliderMax)}
          disabled={sliderMax <= minLeverage}
          tipFormatter={(value) => `${value}x`}
          onChange={setLeverage}
        />
        {pending && (
          <div className='flex items-center gap-2'>
            <Button
              size='small'
              theme='solid'
              loading={applying}
              onClick={applyLeverage}
            >
              {t('应用 {{leverage}}x', { leverage: lev })}
            </Button>
            <Button
              size='small'
              theme='light'
              type='tertiary'
              onClick={() => setLeverage(heldLeverage)}
            >
              {t('撤回')}
            </Button>
          </div>
        )}
        {tier && lev > tier.maxLeverage && (
          <Text type='warning' size='small'>
            {t('按这个仓位价值最高只能用 {{max}}x 杠杆', {
              max: tier.maxLeverage,
            })}
          </Text>
        )}
      </div>
      <div className='flex flex-col gap-1'>
        <RadioGroup
          type='button'
          size='small'
          value={unit}
          onChange={(e) => switchUnit(e.target.value)}
        >
          <Radio value='usdt'>{t('按保证金')}</Radio>
          <Radio value='coin'>{t('按数量')}</Radio>
        </RadioGroup>
        <Input
          value={amount}
          onChange={setAmount}
          prefix={unit === 'usdt' ? t('保证金') : t('开仓数量')}
          aria-label={unit === 'usdt' ? t('保证金') : t('开仓数量')}
          suffix={unit === 'usdt' ? 'USDT' : ticker}
          inputMode='decimal'
        />
        <Text type='tertiary' size='small' className='trade-num'>
          {t('最小 {{qty}} {{ticker}} · 最低保证金 {{margin}} USDT', {
            qty: formatQty(minQty || step),
            ticker,
            margin: (minNotional / lev).toFixed(2),
          })}
          {qty > 0 &&
            ` · ${t('开仓 {{qty}} {{ticker}}', { qty: formatQty(qty), ticker })}`}
        </Text>
      </div>
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
        {notional > 0 && margin + fee > budget && (
          <Text type='warning' size='small'>
            {t('可用资金不足')}
          </Text>
        )}
      </div>
      <div className='flex flex-col gap-2'>
        <Text type='tertiary' size='small'>
          {t('止损')}
          {' · '}
          {t('标记价格触及止损价时自动平仓对应数量，可设多档分批止损')}
        </Text>
        <FuturesLevelsEditor
          kind='sl'
          side={side}
          rows={stopRows}
          onChange={setStopRows}
          reference={calcPrice}
          entryPrice={calcPrice}
          positionQty={qty}
          margin={margin}
          takerRate={taker}
          ticker={ticker}
          priceDigits={priceDigits}
          t={t}
        />
        <Text type='tertiary' size='small'>
          {t('止盈')}
          {' · '}
          {t('现价触及止盈价时自动平仓对应数量，可设多档分批止盈')}
        </Text>
        <FuturesLevelsEditor
          kind='tp'
          side={side}
          rows={takeRows}
          onChange={setTakeRows}
          reference={calcPrice}
          entryPrice={calcPrice}
          positionQty={qty}
          margin={margin}
          takerRate={taker}
          ticker={ticker}
          priceDigits={priceDigits}
          t={t}
        />
      </div>
      <Button
        theme='solid'
        type={isLong ? 'primary' : 'danger'}
        style={isLong ? { background: 'var(--semi-color-success)' } : undefined}
        loading={submitting}
        disabled={!canOpen || !(calcPrice > 0) || pending}
        onClick={submit}
      >
        {`${sideLabel} ${symbol} · ${lev}x`}
      </Button>
      <Text type='tertiary' size='small'>
        {!canOpen
          ? t('该合约暂停开仓，已有仓位可以在仓位卡片上平仓。')
          : isCross
            ? t(
                '全仓由整个账户的资金兜底，资金亏成负数时从站内额度扣，额度可以扣成负数。',
              )
            : t(
                '逐仓一般最多亏光本仓保证金；价格跳空越过强平价时，超出的亏损从资金里扣。',
              )}
        {canOpen &&
          liquidation > 0 &&
          ` ${t('标记价触及 {{price}} 会被强平，请控制杠杆。', {
            price: formatPrice(liquidation, priceDigits),
          })}`}
      </Text>
    </div>
  );
};

export default FuturesOrderPanel;
