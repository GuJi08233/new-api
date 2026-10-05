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
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { RefreshCw } from 'lucide-react';
import { showError, showSuccess, showWarning } from '../../helpers';
import FuturesLevelsEditor, {
  levelsIncomplete,
  levelsPayload,
} from './FuturesLevelsEditor';
import {
  bracketFor,
  estimateLiquidationPrice,
  floorToStep,
  formatPrice,
  formatQty,
  formatSignedUsdt,
  leverageMarks,
  toUsdt,
  tradePost,
  trendClass,
  useFuturesBrackets,
} from './api';

const { Text } = Typography;
const PERCENTS = [0.25, 0.5, 0.75, 1];
// 一键全平与反手要在这么久之内再点一次才执行。
const CONFIRM_MS = 3000;

// 点两次才执行的按钮：第一次点击变成确认的样子，3 秒内再点才执行，过时恢复原样。
const ConfirmButton = ({ label, confirmLabel, onConfirm, ...props }) => {
  const [armed, setArmed] = useState(false);
  const [running, setRunning] = useState(false);
  const timer = useRef(null);

  useEffect(() => () => clearTimeout(timer.current), []);

  const click = async () => {
    if (!armed) {
      setArmed(true);
      timer.current = setTimeout(() => setArmed(false), CONFIRM_MS);
      return;
    }
    clearTimeout(timer.current);
    setArmed(false);
    setRunning(true);
    await onConfirm();
    setRunning(false);
  };

  return (
    <Button
      {...props}
      type={armed ? 'danger' : props.type}
      theme={armed ? 'solid' : props.theme}
      loading={running}
      onClick={click}
    >
      {armed ? confirmLabel : label}
    </Button>
  );
};

// 平仓面板：市价或限价，数量默认是全部可平的数量。选 100% 时提交"全部"，不怕止盈止损刚平掉了一部分。
const ClosePanel = ({ position, lastPrice, priceDigits, onDone, t }) => {
  const available = Math.max(
    Number(position.qty) - Number(position.frozen_qty || 0),
    0,
  );
  const [type, setType] = useState('market');
  const [price, setPrice] = useState('');
  const [qty, setQty] = useState(String(available));
  const [full, setFull] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const long = position.side === 'long';
  const last = Number(lastPrice) || 0;
  const marketable =
    type === 'limit' &&
    Number(price) > 0 &&
    last > 0 &&
    (long ? Number(price) <= last : Number(price) >= last);

  const submit = async () => {
    if (!full && !(Number(qty) > 0)) {
      showError(t('请输入平仓数量'));
      return;
    }
    if (type === 'limit' && !(Number(price) > 0)) {
      showError(t('请输入委托价格'));
      return;
    }
    const body = {
      symbol: position.symbol,
      side: position.side,
      action: 'close',
      type,
      qty: full ? '' : qty,
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
    } else if (order.status === 'filled') {
      showSuccess(
        t('平仓成功：成交 {{qty}} {{ticker}}，均价 {{price}}', {
          qty: formatQty(order.filled_qty),
          ticker: position.ticker,
          price: formatPrice(order.avg_price, priceDigits),
        }),
      );
    } else {
      showSuccess(
        t('成交 {{qty}} {{ticker}}，盘口不够的部分已撤销', {
          qty: formatQty(order.filled_qty),
          ticker: position.ticker,
        }),
      );
    }
    onDone();
  };

  return (
    <div className='trade-position-panel flex flex-col gap-2'>
      <RadioGroup
        type='button'
        size='small'
        value={type}
        onChange={(e) => setType(e.target.value)}
      >
        <Radio value='market'>{t('市价')}</Radio>
        <Radio value='limit'>{t('限价')}</Radio>
      </RadioGroup>
      {type === 'limit' && (
        <>
          <Input
            size='small'
            value={price}
            onChange={setPrice}
            prefix={t('委托价格')}
            aria-label={t('委托价格')}
            suffix='USDT'
            placeholder={formatPrice(lastPrice, priceDigits)}
            inputMode='decimal'
          />
          {marketable && (
            <Text type='warning' size='small'>
              {long
                ? t('限价≤当前价，将立即以市价成交')
                : t('限价≥当前价，将立即以市价成交')}
            </Text>
          )}
        </>
      )}
      <Input
        size='small'
        value={qty}
        onChange={(value) => {
          setQty(value);
          setFull(false);
        }}
        prefix={t('平仓数量')}
        aria-label={t('平仓数量')}
        suffix={position.ticker}
        inputMode='decimal'
      />
      <div className='grid grid-cols-4 gap-1'>
        {PERCENTS.map((percent) => (
          <Button
            key={percent}
            size='small'
            theme='light'
            type='tertiary'
            onClick={() => {
              setFull(percent === 1);
              setQty(
                percent === 1
                  ? String(available)
                  : floorToStep(available * percent, position.step),
              );
            }}
          >
            {percent * 100}%
          </Button>
        ))}
      </div>
      <Button
        size='small'
        theme='solid'
        type='danger'
        loading={submitting}
        disabled={!(available > 0)}
        onClick={submit}
      >
        {t('确认平仓')}
      </Button>
    </div>
  );
};

// 杠杆面板：多空两边的仓位一起调。逐仓只能调高，调高后多出的保证金退回资金；全仓调低时占用变多，全仓可用要够。
const LeveragePanel = ({ position, maxLeverage, onDone, t }) => {
  const cross = position.margin_mode === 'cross';
  const min = cross ? 1 : position.leverage;
  const max = Math.max(maxLeverage || position.leverage, position.leverage);
  const [value, setValue] = useState(position.leverage);
  const [submitting, setSubmitting] = useState(false);
  const changed = value !== position.leverage;

  const submit = async () => {
    setSubmitting(true);
    const res = await tradePost(
      '/api/trade/futures/leverage',
      { symbol: position.symbol, leverage: value },
      t,
    );
    setSubmitting(false);
    if (res.error) {
      showError(res.error);
      setValue(position.leverage);
      return;
    }
    showSuccess(t('杠杆调整成功'));
    onDone();
  };

  return (
    <div className='trade-position-panel flex flex-col gap-2'>
      <div className='flex items-center justify-between'>
        <Text type='tertiary' size='small'>
          {t('杠杆')}
        </Text>
        <Text strong className={`trade-num ${changed ? 'trade-warning' : ''}`}>
          {value}x
        </Text>
      </div>
      <Slider
        min={min}
        max={max}
        step={1}
        value={value}
        marks={leverageMarks(min, max)}
        disabled={max <= min}
        tipFormatter={(next) => `${next}x`}
        onChange={setValue}
      />
      <Text type='tertiary' size='small'>
        {cross
          ? t('多空两边的仓位一起调整。')
          : t(
              '多空两边的仓位一起调整。逐仓只能调高杠杆，调高后多出的保证金退回资金。',
            )}
      </Text>
      <Button
        size='small'
        theme='solid'
        loading={submitting}
        disabled={!changed}
        onClick={submit}
      >
        {t('应用 {{leverage}}x', { leverage: value })}
      </Button>
    </div>
  );
};

// 止盈止损面板：止损按标记价格触发，止盈按最新成交价触发，每组最多 4 档，各自保存或取消。保存要至少填好一档，全部取消用
// 取消按钮。
const LevelsPanel = ({
  position,
  margin,
  takerRate,
  priceDigits,
  onDone,
  t,
}) => {
  const initial = (levels) =>
    levels?.length
      ? levels.map((level) => ({ price: level.price, qty: level.qty }))
      : [{ price: '', qty: position.qty }];
  const [stopRows, setStopRows] = useState(() => initial(position.stop_losses));
  const [takeRows, setTakeRows] = useState(() =>
    initial(position.take_profits),
  );
  const [submitting, setSubmitting] = useState('');

  const save = async (kind, levels) => {
    setSubmitting(kind);
    const res = await tradePost(
      '/api/trade/futures/positions/levels',
      { symbol: position.symbol, side: position.side, kind, levels },
      t,
    );
    setSubmitting('');
    if (res.error) {
      showError(res.error);
      return;
    }
    const messages = {
      tp: levels.length ? t('设置止盈成功') : t('已取消止盈'),
      sl: levels.length ? t('设置止损成功') : t('已取消止损'),
    };
    showSuccess(messages[kind]);
    onDone();
  };

  const groups = [
    {
      kind: 'sl',
      title: t('止损'),
      hint: t('标记价格触及止损价时自动平仓对应数量，可设多档分批止损'),
      rows: stopRows,
      setRows: setStopRows,
      existing: position.stop_losses,
      saveLabel: t('保存止损'),
      cancelLabel: t('取消止损'),
    },
    {
      kind: 'tp',
      title: t('止盈'),
      hint: t('现价触及止盈价时自动平仓对应数量，可设多档分批止盈'),
      rows: takeRows,
      setRows: setTakeRows,
      existing: position.take_profits,
      saveLabel: t('保存止盈'),
      cancelLabel: t('取消止盈'),
    },
  ];

  return (
    <div className='trade-position-panel flex flex-col gap-3'>
      {groups.map((group) => (
        <div key={group.kind} className='flex flex-col gap-2'>
          <Text type='tertiary' size='small'>
            {group.title}
            {' · '}
            {group.hint}
          </Text>
          <FuturesLevelsEditor
            kind={group.kind}
            side={position.side}
            rows={group.rows}
            onChange={group.setRows}
            reference={
              Number(position.markPrice) || Number(position.entry_price)
            }
            entryPrice={Number(position.entry_price)}
            positionQty={Number(position.qty)}
            margin={margin}
            takerRate={takerRate}
            ticker={position.ticker}
            priceDigits={priceDigits}
            t={t}
          />
          <div className='flex gap-2'>
            <Button
              size='small'
              theme='solid'
              loading={submitting === group.kind}
              onClick={() => {
                const levels = levelsPayload(group.rows);
                if (!levels.length || levelsIncomplete(group.rows)) {
                  showError(t('止盈止损的每一档都要填有效的触发价和数量'));
                  return;
                }
                save(group.kind, levels);
              }}
            >
              {group.saveLabel}
            </Button>
            {group.existing?.length > 0 && (
              <Button
                size='small'
                theme='light'
                type='danger'
                onClick={() => save(group.kind, [])}
              >
                {group.cancelLabel}
              </Button>
            )}
          </div>
        </div>
      ))}
    </div>
  );
};

// 追加或减少逐仓保证金：追加从资金里扣，减少的退回资金。减少后剩下的保证金加上浮动亏损要不少于按标记价格算的开仓保证金。
const MarginPanel = ({
  position,
  direction,
  available,
  margin,
  brackets,
  priceDigits,
  onDone,
  t,
}) => {
  const [amount, setAmount] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const qty = Number(position.qty);
  const mark = Number(position.markPrice) || Number(position.entry_price);
  const maxReduce = Math.max(
    Math.floor(
      (margin +
        Math.min(position.pnlUsdt, 0) -
        (mark * qty) / position.leverage) *
        100,
    ) / 100,
    0,
  );
  const max =
    direction === 'add' ? Math.floor(available * 100) / 100 : maxReduce;
  const change = Number(amount) || 0;
  const nextLiquidation = estimateLiquidationPrice(
    position.side,
    Number(position.entry_price) * qty,
    qty,
    direction === 'add' ? margin + change : margin - change,
    brackets,
  );

  const submit = async () => {
    if (!(change > 0)) {
      showError(t('请输入有效金额'));
      return;
    }
    setSubmitting(true);
    const res = await tradePost(
      '/api/trade/futures/positions/margin',
      {
        symbol: position.symbol,
        side: position.side,
        direction,
        amount,
      },
      t,
    );
    setSubmitting(false);
    if (res.error) {
      showError(res.error);
      return;
    }
    showSuccess(t('保证金已调整'));
    onDone();
  };

  return (
    <div className='trade-position-panel flex flex-col gap-2'>
      <Input
        size='small'
        value={amount}
        onChange={setAmount}
        prefix={direction === 'add' ? t('追加金额') : t('减少金额')}
        aria-label={direction === 'add' ? t('追加金额') : t('减少金额')}
        suffix='USDT'
        inputMode='decimal'
      />
      <div className='flex justify-between'>
        <Text type='tertiary' size='small'>
          {direction === 'add'
            ? t('可用 {{amount}} USDT', { amount: max.toFixed(2) })
            : t('当前 {{margin}} USDT，最多可减少 {{max}} USDT', {
                margin: margin.toFixed(2),
                max: max.toFixed(2),
              })}
        </Text>
        <Button
          size='small'
          theme='borderless'
          onClick={() => setAmount(String(max))}
        >
          {t('最大')}
        </Button>
      </div>
      <div className='flex justify-between'>
        <Text type='tertiary' size='small'>
          {t('调整后预估强平价')}
        </Text>
        <Text size='small' className='trade-num trade-warning'>
          {change > 0 && nextLiquidation > 0
            ? formatPrice(nextLiquidation, priceDigits)
            : '--'}
        </Text>
      </div>
      <Button
        size='small'
        theme='solid'
        loading={submitting}
        disabled={!(change > 0)}
        onClick={submit}
      >
        {direction === 'add' ? t('确认追加') : t('确认减少')}
      </Button>
    </div>
  );
};

// levelsText 把一组止盈或止损按触发的先后显示成 "p1 / p2 (q2)"：价格往上触发的(多仓止盈、空仓止损)从低到高，往下的从高到低；
// 数量等于整个仓位时不写数量。
function levelsText(levels, qty, priceDigits, rising) {
  if (!levels?.length) return '--';
  return [...levels]
    .sort((a, b) => (Number(a.price) - Number(b.price)) * (rising ? 1 : -1))
    .map((level) =>
      Number(level.qty) === qty
        ? formatPrice(level.price, priceDigits)
        : `${formatPrice(level.price, priceDigits)} (${formatQty(level.qty)})`,
    )
    .join(' / ');
}

// 一个仓位的卡片：盈亏与收益率随标记价格实时变化，下面是数量、价格、强平价、止盈止损、资金费、维持保证金率与已实现盈亏，
// 按钮展开平仓、杠杆、止盈止损与调整保证金的面板，交易页上还能反手。
const PositionCard = ({
  position,
  perUnit,
  cash,
  cross,
  lastPrice,
  showReverse,
  onOpenSymbol,
  takerRate,
  onChanged,
  t,
}) => {
  const [panel, setPanel] = useState('');
  const bracketInfo = useFuturesBrackets(position.symbol, t);
  const brackets = bracketInfo?.brackets;
  const long = position.side === 'long';
  const isCross = position.margin_mode === 'cross';
  const qty = Number(position.qty);
  const margin = toUsdt(position.margin, perUnit);
  const pnl = position.pnlUsdt;
  const roe = margin > 0 ? (pnl / margin) * 100 : 0;
  const priceDigits = position.priceDigits;
  const markValue = (Number(position.markPrice) || 0) * qty;
  const tier = bracketFor(brackets, markValue);
  // 已实现盈亏与仓位历史的一样：平仓盈亏减手续费，加上收到(或减去付出)的资金费。
  const realized = position.realized_pnl - position.fees + position.funding;
  const cashUsdt = toUsdt(cash, perUnit);
  const available = Math.max(
    Math.min(cross ? toUsdt(cross.available, perUnit) : cashUsdt, cashUsdt),
    0,
  );

  const toggle = (name) =>
    setPanel((current) => (current === name ? '' : name));
  const done = () => {
    setPanel('');
    onChanged?.();
  };

  const reverse = async () => {
    const res = await tradePost(
      '/api/trade/futures/positions/reverse',
      { symbol: position.symbol, side: position.side },
      t,
    );
    if (res.error) {
      showError(res.error);
      return;
    }
    const { closed, opened, open_error: openError } = res.data;
    const closedText = t(
      '平掉{{side}} {{qty}} {{ticker}}(已实现盈亏 {{pnl}} USDT)',
      {
        side: long ? t('多仓') : t('空仓'),
        qty: formatQty(closed.filled_qty),
        ticker: position.ticker,
        pnl: formatSignedUsdt(closed.realized_pnl, perUnit),
      },
    );
    if (opened) {
      showSuccess(
        `${long ? t('已反手为空单') : t('已反手为多单')}：${closedText}，${t(
          '反向开 {{qty}} {{ticker}} {{leverage}}x。新仓没有止损止盈，需要的话重新设。',
          {
            qty: formatQty(opened.filled_qty),
            ticker: position.ticker,
            leverage: opened.leverage,
          },
        )}`,
      );
    } else {
      showWarning(
        `${t('反手只完成了一半：仓位已平，反向开仓失败')}。${closedText}，${t(
          '反向{{side}}没开成：{{reason}}。原仓已不在，要开反向仓请手动下单。',
          { side: long ? t('空仓') : t('多仓'), reason: openError },
        )}`,
      );
    }
    onChanged?.();
  };

  const metrics = [
    [
      t('数量'),
      `${formatQty(qty)} ${position.ticker}${
        Number(position.frozen_qty) > 0
          ? ` (${t('挂单 {{qty}}', { qty: formatQty(position.frozen_qty) })})`
          : ''
      }`,
    ],
    [t('开仓价'), formatPrice(position.entry_price, priceDigits)],
    [t('标记价'), formatPrice(position.markPrice, priceDigits)],
    [
      t('强平价'),
      <span key='liq' className='trade-warning'>
        {Number(position.liquidation_price) > 0
          ? formatPrice(position.liquidation_price, priceDigits)
          : '--'}
      </span>,
    ],
    [t('止损'), levelsText(position.stop_losses, qty, priceDigits, !long)],
    [t('止盈'), levelsText(position.take_profits, qty, priceDigits, long)],
    [
      t('资金费'),
      <span key='funding'>
        {t('每期')}{' '}
        <span className={trendClass(position.next_funding)}>
          {formatSignedUsdt(position.next_funding, perUnit, 4)}
        </span>
        {' · '}
        {t('累计')}{' '}
        <span className={trendClass(position.funding)}>
          {formatSignedUsdt(position.funding, perUnit, 4)}
        </span>
      </span>,
    ],
    [
      'MMR',
      tier
        ? t('档 {{tier}} · {{rate}}%', {
            tier: tier.tier,
            rate: (tier.mmr * 100).toFixed(2),
          })
        : '--',
    ],
    [
      t('已实现盈亏'),
      <span key='realized' className={trendClass(realized)}>
        {formatSignedUsdt(realized, perUnit)} USDT
      </span>,
    ],
  ];

  const buttons = [
    ['close', t('平仓')],
    ['leverage', t('杠杆')],
    ['levels', t('止盈止损')],
    ...(isCross
      ? []
      : [
          ['add', t('加保证金')],
          ['reduce', t('减保证金')],
        ]),
  ];

  return (
    <div
      className={`trade-position ${long ? 'trade-position-long' : 'trade-position-short'}`}
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          {onOpenSymbol ? (
            <Button
              theme='borderless'
              size='small'
              onClick={() => onOpenSymbol(position.symbol)}
            >
              {position.symbol}
            </Button>
          ) : (
            <Text strong>{position.symbol}</Text>
          )}
          <Tag size='small' type='solid' color={long ? 'green' : 'red'}>
            {long ? t('多仓') : t('空仓')} {position.leverage}x
          </Tag>
          <Tag size='small'>{isCross ? t('全仓') : t('逐仓')}</Tag>
        </div>
        <div className='flex items-center gap-3'>
          <span className={`trade-num font-semibold ${trendClass(pnl)}`}>
            {`${pnl > 0 ? '+' : ''}${pnl.toFixed(2)} USDT (${roe > 0 ? '+' : ''}${roe.toFixed(2)}%)`}
          </span>
          <Text type='tertiary' size='small' className='trade-num'>
            {t('保证金')} {margin.toFixed(2)}
          </Text>
        </div>
      </div>
      <div className='grid grid-cols-2 gap-x-4 gap-y-1 md:grid-cols-3'>
        {metrics.map(([label, value]) => (
          <div key={label} className='flex justify-between gap-2'>
            <Text type='tertiary' size='small' className='shrink-0'>
              {label}
            </Text>
            <Text size='small' className='trade-num text-right'>
              {value}
            </Text>
          </div>
        ))}
      </div>
      <div className='flex flex-wrap gap-2'>
        {buttons.map(([name, label]) => (
          <Button
            key={name}
            size='small'
            theme={panel === name ? 'solid' : 'light'}
            type={name === 'close' ? 'danger' : 'tertiary'}
            onClick={() => toggle(name)}
          >
            {label}
          </Button>
        ))}
        {showReverse ? (
          <ConfirmButton
            size='small'
            theme='light'
            type='tertiary'
            label={t('反手')}
            confirmLabel={t('确认反手')}
            onConfirm={reverse}
          />
        ) : (
          onOpenSymbol && (
            <Button
              size='small'
              theme='light'
              type='tertiary'
              onClick={() => onOpenSymbol(position.symbol)}
            >
              {t('去交易')}
            </Button>
          )
        )}
      </div>
      {panel === 'close' && (
        <ClosePanel
          position={position}
          lastPrice={lastPrice}
          priceDigits={priceDigits}
          onDone={done}
          t={t}
        />
      )}
      {panel === 'leverage' && (
        <LeveragePanel
          position={position}
          maxLeverage={bracketInfo?.maxLeverage}
          onDone={done}
          t={t}
        />
      )}
      {panel === 'levels' && (
        <LevelsPanel
          position={position}
          margin={margin}
          takerRate={takerRate}
          priceDigits={priceDigits}
          onDone={done}
          t={t}
        />
      )}
      {(panel === 'add' || panel === 'reduce') && (
        <MarginPanel
          key={panel}
          position={position}
          direction={panel}
          available={available}
          margin={margin}
          brackets={brackets}
          priceDigits={priceDigits}
          onDone={done}
          t={t}
        />
      )}
    </div>
  );
};

// 合约仓位：每个仓位一张卡片，标记价格有推送的仓位按它实时重算浮动盈亏。头部是仓位数、全仓/逐仓的个数与一键全平(平掉全部
// 合约的全部仓位，挂着的开仓委托不动)。marks 与 lastPrices 是按合约的实时标记价格与最新价；rules 给出各合约的价格精度与
// 数量步长。
const FuturesPositions = ({
  positions,
  cross,
  marks,
  lastPrices,
  rules,
  cash,
  perUnit,
  takerFeeBps,
  showReverse = false,
  onChanged,
  onOpenSymbol,
  t,
}) => {
  const rows = (positions || []).map((position) => {
    const qty = Number(position.qty);
    const entry = Number(position.entry_price);
    const liveMark = Number(marks?.[position.symbol]);
    let pnlUsdt = toUsdt(position.pnl, perUnit);
    if (liveMark > 0) {
      pnlUsdt =
        (position.side === 'long' ? liveMark - entry : entry - liveMark) * qty;
    }
    const symbolRules = rules?.[position.symbol];
    return {
      ...position,
      key: `${position.symbol}-${position.side}`,
      markPrice: liveMark > 0 ? liveMark : position.mark,
      pnlUsdt,
      priceDigits: symbolRules?.priceDigits,
      step: symbolRules?.step,
    };
  });
  const crossCount = rows.filter((row) => row.margin_mode === 'cross').length;

  const closeAll = async () => {
    const res = await tradePost(
      '/api/trade/futures/positions/close-all',
      {},
      t,
    );
    if (res.error) {
      showError(res.error);
      return;
    }
    const { closed, failed } = res.data;
    if (!failed.length) {
      showSuccess(t('已全部平仓（{{count}} 个）', { count: closed.length }));
    } else {
      showError(
        `${t('已平 {{closed}} 个仓位，{{failed}} 个失败', {
          closed: closed.length,
          failed: failed.length,
        })}：${failed
          .map(
            (item) =>
              `${item.symbol} ${item.side === 'long' ? t('多仓') : t('空仓')} ${item.message}`,
          )
          .join('；')}`,
      );
    }
    onChanged?.();
  };

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <Text strong>{t('合约持仓')}</Text>
          <Text type='tertiary' size='small'>
            {t('{{count}} 个 · 全仓 {{cross}} / 逐仓 {{isolated}}', {
              count: rows.length,
              cross: crossCount,
              isolated: rows.length - crossCount,
            })}
          </Text>
        </div>
        <div className='flex items-center gap-1'>
          <Button
            size='small'
            theme='borderless'
            type='tertiary'
            icon={<RefreshCw size={14} />}
            aria-label={t('刷新')}
            onClick={() => onChanged?.()}
          />
          {rows.length > 0 && (
            <ConfirmButton
              size='small'
              theme='borderless'
              type='danger'
              label={t('一键全平')}
              confirmLabel={t('确认全平')}
              onConfirm={closeAll}
            />
          )}
        </div>
      </div>
      {cross && crossCount > 0 && (
        <Text type='tertiary' size='small' className='trade-num'>
          {t(
            '全仓：占用 {{used}} · 浮动盈亏 {{upnl}} · 维持保证金 {{maintenance}} · 可用 {{available}} USDT',
            {
              used: toUsdt(cross.used, perUnit).toFixed(2),
              upnl: formatSignedUsdt(cross.upnl, perUnit),
              maintenance: toUsdt(cross.maintenance, perUnit).toFixed(2),
              available: toUsdt(cross.available, perUnit).toFixed(2),
            },
          )}
        </Text>
      )}
      {rows.length === 0 ? (
        <Text type='tertiary' size='small'>
          {t('没有合约仓位')}
        </Text>
      ) : (
        rows.map((position) => (
          <PositionCard
            key={position.key}
            position={position}
            perUnit={perUnit}
            cash={cash}
            cross={cross}
            lastPrice={lastPrices?.[position.symbol]}
            showReverse={showReverse}
            onOpenSymbol={onOpenSymbol}
            takerRate={(takerFeeBps || 0) / 10000}
            onChanged={onChanged}
            t={t}
          />
        ))
      )}
    </div>
  );
};

export default FuturesPositions;
