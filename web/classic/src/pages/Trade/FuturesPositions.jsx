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
  Modal,
  Popconfirm,
  Radio,
  RadioGroup,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { Pencil } from 'lucide-react';
import { showError, showSuccess } from '../../helpers';
import {
  estimateLiquidationPrice,
  formatPrice,
  formatQty,
  formatSignedUsdt,
  toUsdt,
  tradePost,
  trendClass,
} from './api';

const { Text } = Typography;

// 调整一个仓位的保证金：追加从可用资金扣，减少的退回可用资金。减少后剩下的保证金加上浮动亏损，要不少于按标记价格算的
// 开仓保证金。
const MarginModal = ({ position, cash, perUnit, onClose, onDone, t }) => {
  const [direction, setDirection] = useState('add');
  const [amount, setAmount] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setDirection('add');
    setAmount('');
  }, [position]);

  if (!position) return null;
  const qty = Number(position.qty);
  const entryValue = Number(position.entry_price) * qty;
  const margin = toUsdt(position.margin, perUnit);
  const mark = Number(position.markPrice) || Number(position.entry_price);
  const pnl = position.pnlUsdt;
  const maxReduce = Math.max(
    Math.floor(
      (margin + Math.min(pnl, 0) - (mark * qty) / position.leverage) * 100,
    ) / 100,
    0,
  );
  const maxAdd = Math.floor(toUsdt(cash, perUnit) * 100) / 100;
  const change = Number(amount) || 0;
  const nextMargin = direction === 'add' ? margin + change : margin - change;
  const nextLiquidation = estimateLiquidationPrice(
    position.side,
    entryValue,
    qty,
    nextMargin,
    position.mmr_bps,
  );

  const submit = async () => {
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
    <Modal
      title={t('调整保证金')}
      visible
      onCancel={onClose}
      onOk={submit}
      okText={t('确认')}
      okButtonProps={{ loading: submitting, disabled: !(change > 0) }}
      width={420}
    >
      <div className='flex flex-col gap-3'>
        <RadioGroup
          type='button'
          value={direction}
          onChange={(e) => setDirection(e.target.value)}
        >
          <Radio value='add'>{t('增加保证金')}</Radio>
          <Radio value='reduce'>{t('减少保证金')}</Radio>
        </RadioGroup>
        <Input
          value={amount}
          onChange={setAmount}
          prefix={t('调整金额')}
          aria-label={t('调整金额')}
          suffix='USDT'
          inputMode='decimal'
        />
        {[
          [t('当前保证金'), `${margin.toFixed(2)} USDT`],
          [
            direction === 'add' ? t('最多可增加') : t('最多可减少'),
            `${(direction === 'add' ? maxAdd : maxReduce).toFixed(2)} USDT`,
          ],
          [
            t('调整后预估强平价'),
            change > 0 && nextLiquidation > 0
              ? formatPrice(nextLiquidation)
              : '--',
          ],
        ].map(([label, value]) => (
          <div key={label} className='flex justify-between'>
            <Text type='tertiary' size='small'>
              {label}
            </Text>
            <Text size='small' className='trade-num'>
              {value}
            </Text>
          </div>
        ))}
      </div>
    </Modal>
  );
};

// 设置一个仓位的止盈止损：按标记价格触发，触发后按市价平掉整个仓位。留空表示不设。
const TpSlModal = ({ position, onClose, onDone, t }) => {
  const [takeProfit, setTakeProfit] = useState('');
  const [stopLoss, setStopLoss] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setTakeProfit(position?.take_profit || '');
    setStopLoss(position?.stop_loss || '');
  }, [position]);

  if (!position) return null;

  const submit = async () => {
    setSubmitting(true);
    const res = await tradePost(
      '/api/trade/futures/positions/tpsl',
      {
        symbol: position.symbol,
        side: position.side,
        take_profit: takeProfit,
        stop_loss: stopLoss,
      },
      t,
    );
    setSubmitting(false);
    if (res.error) {
      showError(res.error);
      return;
    }
    showSuccess(t('止盈止损已设置'));
    onDone();
  };

  return (
    <Modal
      title={t('止盈止损')}
      visible
      onCancel={onClose}
      onOk={submit}
      okText={t('确认')}
      okButtonProps={{ loading: submitting }}
      width={420}
    >
      <div className='flex flex-col gap-3'>
        <div className='flex justify-between'>
          <Text type='tertiary' size='small'>
            {t('标记价格')}
          </Text>
          <Text size='small' className='trade-num'>
            {formatPrice(position.markPrice)}
          </Text>
        </div>
        <div className='flex justify-between'>
          <Text type='tertiary' size='small'>
            {t('强平价')}
          </Text>
          <Text size='small' className='trade-num'>
            {Number(position.liquidation_price) > 0
              ? formatPrice(position.liquidation_price)
              : '--'}
          </Text>
        </div>
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
          {position.side === 'long'
            ? t('多仓的止盈价要高于标记价格，止损价要低于标记价格。')
            : t('空仓的止盈价要低于标记价格，止损价要高于标记价格。')}{' '}
          {t('按标记价格触发，触发后按市价平掉整个仓位。留空表示不设。')}
        </Text>
      </div>
    </Modal>
  );
};

// 合约仓位列表：数量、开仓均价、标记价格、强平价、保证金、浮动盈亏与收益率、止盈止损和累计资金费；可以市价全平、
// 调整保证金、设置止盈止损。marks 是实时推送的标记价格，有推送的仓位按它即时重算浮动盈亏，其余用接口返回的估值。
const FuturesPositions = ({
  positions,
  marks,
  cash,
  perUnit,
  onChanged,
  onOpenSymbol,
  t,
}) => {
  const [closing, setClosing] = useState('');
  const [marginTarget, setMarginTarget] = useState(null);
  const [tpslTarget, setTpslTarget] = useState(null);

  const rows = (positions || []).map((position) => {
    const qty = Number(position.qty);
    const entry = Number(position.entry_price);
    const liveMark = Number(marks?.[position.symbol]);
    let pnlUsdt = toUsdt(position.pnl, perUnit);
    if (liveMark > 0) {
      pnlUsdt =
        (position.side === 'long' ? liveMark - entry : entry - liveMark) * qty;
    }
    return {
      ...position,
      key: `${position.symbol}-${position.side}`,
      markPrice: liveMark > 0 ? liveMark : position.mark,
      pnlUsdt,
    };
  });

  const closeAll = async (position) => {
    setClosing(position.key);
    const res = await tradePost(
      '/api/trade/futures/orders',
      {
        symbol: position.symbol,
        side: position.side,
        action: 'close',
        type: 'market',
        qty: '0',
      },
      t,
    );
    setClosing('');
    if (res.error) {
      showError(res.error);
    } else if (res.data.status === 'filled') {
      showSuccess(
        t('已成交 {{qty}} {{ticker}}，均价 {{price}}', {
          qty: formatQty(res.data.filled_qty),
          ticker: position.ticker,
          price: formatPrice(res.data.avg_price),
        }),
      );
    } else {
      showSuccess(
        t('成交 {{qty}} {{ticker}}，盘口不够的部分已撤销', {
          qty: formatQty(res.data.filled_qty),
          ticker: position.ticker,
        }),
      );
    }
    onChanged?.();
  };

  const columns = [
    {
      title: t('合约'),
      dataIndex: 'symbol',
      render: (_, position) => (
        <div className='flex items-center gap-1 whitespace-nowrap'>
          {onOpenSymbol ? (
            <Button
              theme='borderless'
              size='small'
              onClick={() => onOpenSymbol(position.symbol)}
            >
              {position.ticker}
            </Button>
          ) : (
            <Text strong>{position.ticker}</Text>
          )}
          <Tag size='small' color={position.side === 'long' ? 'green' : 'red'}>
            {position.side === 'long' ? t('多仓') : t('空仓')}
          </Tag>
          <Tag size='small'>{position.leverage}x</Tag>
        </div>
      ),
    },
    {
      title: t('数量'),
      dataIndex: 'qty',
      align: 'right',
      render: (value, position) => (
        <div className='trade-num'>
          <div>{formatQty(value)}</div>
          {Number(position.frozen_qty) > 0 && (
            <Text type='tertiary' size='small'>
              {t('挂单 {{qty}}', { qty: formatQty(position.frozen_qty) })}
            </Text>
          )}
        </div>
      ),
    },
    {
      title: t('开仓均价'),
      dataIndex: 'entry_price',
      align: 'right',
      render: (value) => (
        <span className='trade-num'>{formatPrice(value)}</span>
      ),
    },
    {
      title: t('标记价格'),
      dataIndex: 'markPrice',
      align: 'right',
      render: (value) => (
        <span className='trade-num'>{formatPrice(value)}</span>
      ),
    },
    {
      title: t('强平价'),
      dataIndex: 'liquidation_price',
      align: 'right',
      render: (value) => (
        <span className='trade-num trade-warning'>
          {Number(value) > 0 ? formatPrice(value) : '--'}
        </span>
      ),
    },
    {
      title: t('保证金'),
      dataIndex: 'margin',
      align: 'right',
      render: (value, position) => (
        <div className='flex items-center justify-end gap-1 whitespace-nowrap'>
          <span className='trade-num'>{toUsdt(value, perUnit).toFixed(2)}</span>
          <Button
            size='small'
            theme='borderless'
            type='tertiary'
            icon={<Pencil size={12} />}
            aria-label={t('调整保证金')}
            onClick={() => setMarginTarget(position)}
          />
        </div>
      ),
    },
    {
      title: t('浮动盈亏(收益率)'),
      dataIndex: 'pnlUsdt',
      align: 'right',
      render: (value, position) => {
        const margin = toUsdt(position.margin, perUnit);
        const roe = margin > 0 ? (value / margin) * 100 : 0;
        return (
          <span className={`trade-num whitespace-nowrap ${trendClass(value)}`}>
            {`${value > 0 ? '+' : ''}${value.toFixed(2)} (${roe > 0 ? '+' : ''}${roe.toFixed(2)}%)`}
          </span>
        );
      },
    },
    {
      title: t('止盈 / 止损'),
      dataIndex: 'take_profit',
      align: 'right',
      render: (_, position) => (
        <div className='flex items-center justify-end gap-1 whitespace-nowrap'>
          <span className='trade-num'>
            {position.take_profit ? formatPrice(position.take_profit) : '--'}
            {' / '}
            {position.stop_loss ? formatPrice(position.stop_loss) : '--'}
          </span>
          <Button
            size='small'
            theme='borderless'
            type='tertiary'
            icon={<Pencil size={12} />}
            aria-label={t('止盈止损')}
            onClick={() => setTpslTarget(position)}
          />
        </div>
      ),
    },
    {
      title: t('资金费'),
      dataIndex: 'funding',
      align: 'right',
      render: (value) => (
        <span className={`trade-num ${trendClass(value)}`}>
          {formatSignedUsdt(value, perUnit, 4)}
        </span>
      ),
    },
    {
      title: '',
      dataIndex: 'operate',
      render: (_, position) => (
        <Popconfirm
          title={t('按市价平掉这个仓位全部可平的数量？')}
          onConfirm={() => closeAll(position)}
        >
          <Button
            size='small'
            type='danger'
            theme='light'
            loading={closing === position.key}
          >
            {t('市价全平')}
          </Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <>
      <Table
        size='small'
        rowKey='key'
        columns={columns}
        dataSource={rows}
        pagination={false}
        scroll={{ x: 'max-content' }}
        empty={<Text type='tertiary'>{t('没有合约仓位')}</Text>}
      />
      <MarginModal
        position={marginTarget}
        cash={cash}
        perUnit={perUnit}
        onClose={() => setMarginTarget(null)}
        onDone={() => {
          setMarginTarget(null);
          onChanged?.();
        }}
        t={t}
      />
      <TpSlModal
        position={tpslTarget}
        onClose={() => setTpslTarget(null)}
        onDone={() => {
          setTpslTarget(null);
          onChanged?.();
        }}
        t={t}
      />
    </>
  );
};

export default FuturesPositions;
