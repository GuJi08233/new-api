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
  Input,
  Modal,
  Radio,
  RadioGroup,
  TabPane,
  Tabs,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { Calculator } from 'lucide-react';
import { formatPrice, trendClass } from './api';
import { formatSymbolLabel } from './symbols';

const { Text } = Typography;

const TARGET_ROES = [10, 25, 50, 100];

// 交易风控与收益计算器：支持收益/目标价、强平缓冲、补仓均价、按止损定仓位 4 种实用工具。
const TradeCalculatorModal = ({
  visible,
  onClose,
  symbolItem,
  currentPrice,
  priceDigits = 2,
  futures = false,
  defaultLeverage = 10,
  t,
}) => {
  const [tab, setTab] = useState('pnl');
  const [side, setSide] = useState('long');
  const [leverage, setLeverage] = useState(
    futures ? String(defaultLeverage) : '1',
  );
  const [entry, setEntry] = useState('');
  const [exit, setExit] = useState('');
  const [margin, setMargin] = useState('100');
  const [extraMargin, setExtraMargin] = useState('0');
  // 补仓均价
  const [oldAvg, setOldAvg] = useState('');
  const [oldQty, setOldQty] = useState('');
  const [addPrice, setAddPrice] = useState('');
  const [addUsdt, setAddUsdt] = useState('100');
  // 以损定仓
  const [capital, setCapital] = useState('1000');
  const [riskPct, setRiskPct] = useState('2');
  const [stopPrice, setStopPrice] = useState('');

  useEffect(() => {
    if (!visible) return;
    const priceNum = Number(currentPrice);
    if (Number.isFinite(priceNum) && priceNum > 0) {
      const formatted = priceNum.toFixed(priceDigits);
      setEntry((prev) => prev || formatted);
      setOldAvg((prev) => prev || formatted);
      setAddPrice(formatted);
      setExit((prev) => prev || (priceNum * 1.05).toFixed(priceDigits));
      setStopPrice((prev) => prev || (priceNum * 0.95).toFixed(priceDigits));
    }
    setLeverage(futures ? String(defaultLeverage || 10) : '1');
  }, [visible, currentPrice, priceDigits, futures, defaultLeverage]);

  const isLong = side === 'long';
  const dir = isLong ? 1 : -1;
  const lev = Math.max(1, Number(leverage) || 1);
  const entryNum = Number(entry) || 0;
  const exitNum = Number(exit) || 0;
  const marginNum = Number(margin) || 0;
  const extraNum = Math.max(0, Number(extraMargin) || 0);

  // 1. 收益计算
  const notional = marginNum * lev;
  const qty = entryNum > 0 ? notional / entryNum : 0;
  const pnl =
    entryNum > 0 && exitNum > 0 ? (exitNum - entryNum) * qty * dir : 0;
  const roe = marginNum > 0 ? (pnl / marginNum) * 100 : 0;

  // 2. 强平缓冲估算 (按 0.5% 基准维持保证金率近似展示)
  const mmr = 0.005;
  const calcLiq = (backing) => {
    if (!(entryNum > 0) || !(qty > 0)) return 0;
    const raw = isLong
      ? (entryNum * qty - backing) / (qty * (1 - mmr))
      : (entryNum * qty + backing) / (qty * (1 + mmr));
    return raw > 0 ? raw : 0;
  };
  const baseLiq = calcLiq(marginNum);
  const bufferedLiq = calcLiq(marginNum + extraNum);
  const liqDistance =
    entryNum > 0 && baseLiq > 0
      ? (Math.abs(entryNum - baseLiq) / entryNum) * 100
      : 0;

  // 3. 补仓均价
  const oldAvgNum = Number(oldAvg) || 0;
  const oldQtyNum = Number(oldQty) || 0;
  const addPriceNum = Number(addPrice) || 0;
  const addUsdtNum = Number(addUsdt) || 0;
  const addQty = addPriceNum > 0 ? addUsdtNum / addPriceNum : 0;
  const totalQty = oldQtyNum + addQty;
  const newAvg =
    totalQty > 0
      ? (oldAvgNum * oldQtyNum + addPriceNum * addQty) / totalQty
      : 0;
  const reboundPct =
    addPriceNum > 0 && newAvg > addPriceNum
      ? ((newAvg - addPriceNum) / addPriceNum) * 100
      : 0;

  // 4. 以损定仓
  const capitalNum = Number(capital) || 0;
  const riskPctNum = Number(riskPct) || 0;
  const stopNum = Number(stopPrice) || 0;
  const maxLossUsdt = (capitalNum * riskPctNum) / 100;
  const stopGapPct =
    entryNum > 0 && stopNum > 0
      ? (Math.abs(entryNum - stopNum) / entryNum) * 100
      : 0;
  const recNotional = stopGapPct > 0 ? maxLossUsdt / (stopGapPct / 100) : 0;
  const recQty = entryNum > 0 ? recNotional / entryNum : 0;
  const recMargin = lev > 0 ? recNotional / lev : recNotional;

  const titleLabel = symbolItem
    ? `${t('交易计算器')} · ${formatSymbolLabel(symbolItem, t)}`
    : t('交易计算器');

  return (
    <Modal
      visible={visible}
      onCancel={onClose}
      footer={null}
      width={540}
      title={
        <span className='inline-flex items-center gap-2'>
          <Calculator size={17} />
          {titleLabel}
        </span>
      }
    >
      <div className='flex flex-col gap-3 pb-2'>
        <Tabs activeKey={tab} onChange={setTab} size='small'>
          <TabPane tab={t('收益与目标价')} itemKey='pnl'>
            <div className='flex flex-col gap-3 pt-2'>
              <div className='flex flex-wrap items-center justify-between gap-2'>
                <RadioGroup
                  type='button'
                  value={side}
                  onChange={(e) => setSide(e.target.value)}
                >
                  <Radio value='long'>{t('做多')}</Radio>
                  <Radio value='short'>{t('做空')}</Radio>
                </RadioGroup>
                <div className='flex items-center gap-2'>
                  <Text type='tertiary' size='small'>
                    {t('杠杆')}
                  </Text>
                  <Input
                    size='small'
                    style={{ width: 88 }}
                    value={leverage}
                    onChange={setLeverage}
                    suffix='x'
                    inputMode='numeric'
                  />
                </div>
              </div>
              <div className='grid grid-cols-1 gap-2.5 sm:grid-cols-3'>
                <Input
                  value={entry}
                  onChange={setEntry}
                  prefix={t('开仓价')}
                  inputMode='decimal'
                />
                <Input
                  value={exit}
                  onChange={setExit}
                  prefix={t('平仓价')}
                  inputMode='decimal'
                />
                <Input
                  value={margin}
                  onChange={setMargin}
                  prefix={t('本金')}
                  suffix='USDT'
                  inputMode='decimal'
                />
              </div>
              <div className='trade-calc-result'>
                <div className='grid grid-cols-3 gap-2'>
                  <div>
                    <Text type='tertiary' size='small'>
                      {t('仓位价值')}
                    </Text>
                    <div className='trade-num font-semibold'>
                      {notional > 0 ? `${notional.toFixed(2)} USDT` : '--'}
                    </div>
                  </div>
                  <div>
                    <Text type='tertiary' size='small'>
                      {t('预估盈亏')}
                    </Text>
                    <div
                      className={`trade-num font-semibold ${trendClass(pnl)}`}
                    >
                      {entryNum > 0 && exitNum > 0
                        ? `${pnl > 0 ? '+' : ''}${pnl.toFixed(2)} USDT`
                        : '--'}
                    </div>
                  </div>
                  <div>
                    <Text type='tertiary' size='small'>
                      {t('收益率 (ROE)')}
                    </Text>
                    <div
                      className={`trade-num font-semibold ${trendClass(roe)}`}
                    >
                      {entryNum > 0 && exitNum > 0
                        ? `${roe > 0 ? '+' : ''}${roe.toFixed(2)}%`
                        : '--'}
                    </div>
                  </div>
                </div>
                {entryNum > 0 && (
                  <div className='flex flex-wrap items-center gap-1.5 pt-2 border-t border-[var(--semi-color-border)]'>
                    <Text type='tertiary' size='small'>
                      {t('目标止盈参考')}：
                    </Text>
                    {TARGET_ROES.map((pct) => {
                      const move = pct / 100 / lev;
                      const target = entryNum * (1 + move * dir);
                      return (
                        <Tag key={pct} size='small' color='green' type='light'>
                          +{pct}% → {formatPrice(target, priceDigits)}
                        </Tag>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>
          </TabPane>

          <TabPane tab={t('强平缓冲')} itemKey='liq'>
            <div className='flex flex-col gap-3 pt-2'>
              <div className='flex flex-wrap items-center justify-between gap-2'>
                <RadioGroup
                  type='button'
                  value={side}
                  onChange={(e) => setSide(e.target.value)}
                >
                  <Radio value='long'>{t('做多')}</Radio>
                  <Radio value='short'>{t('做空')}</Radio>
                </RadioGroup>
                <Input
                  size='small'
                  style={{ width: 96 }}
                  value={leverage}
                  onChange={setLeverage}
                  prefix={t('杠杆')}
                  suffix='x'
                  inputMode='numeric'
                />
              </div>
              <div className='grid grid-cols-1 gap-2.5 sm:grid-cols-3'>
                <Input
                  value={entry}
                  onChange={setEntry}
                  prefix={t('开仓价')}
                  inputMode='decimal'
                />
                <Input
                  value={margin}
                  onChange={setMargin}
                  prefix={t('保证金')}
                  suffix='USDT'
                  inputMode='decimal'
                />
                <Input
                  value={extraMargin}
                  onChange={setExtraMargin}
                  prefix={t('追加')}
                  suffix='USDT'
                  inputMode='decimal'
                />
              </div>
              <div className='trade-calc-result grid grid-cols-3 gap-2'>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('估算强平价')}
                  </Text>
                  <div className='trade-num font-semibold trade-warning'>
                    {baseLiq > 0 ? formatPrice(baseLiq, priceDigits) : '--'}
                  </div>
                </div>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('抗波动空间')}
                  </Text>
                  <div className='trade-num font-semibold'>
                    {liqDistance > 0 ? `${liqDistance.toFixed(2)}%` : '--'}
                  </div>
                </div>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('追加后强平价')}
                  </Text>
                  <div className='trade-num font-semibold trade-up'>
                    {bufferedLiq > 0
                      ? formatPrice(bufferedLiq, priceDigits)
                      : '--'}
                  </div>
                </div>
              </div>
              <Text type='tertiary' size='small'>
                {t(
                  '按 0.5% 基础维持保证金率估算，实际强平价以合约风险限额档位为准。',
                )}
              </Text>
            </div>
          </TabPane>

          <TabPane tab={t('补仓均价')} itemKey='dca'>
            <div className='flex flex-col gap-3 pt-2'>
              <div className='grid grid-cols-2 gap-2.5'>
                <Input
                  value={oldAvg}
                  onChange={setOldAvg}
                  prefix={t('原均价')}
                  inputMode='decimal'
                />
                <Input
                  value={oldQty}
                  onChange={setOldQty}
                  prefix={t('原数量')}
                  inputMode='decimal'
                />
                <Input
                  value={addPrice}
                  onChange={setAddPrice}
                  prefix={t('补仓价')}
                  inputMode='decimal'
                />
                <Input
                  value={addUsdt}
                  onChange={setAddUsdt}
                  prefix={t('补仓额')}
                  suffix='USDT'
                  inputMode='decimal'
                />
              </div>
              <div className='trade-calc-result grid grid-cols-3 gap-2'>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('补仓后均价')}
                  </Text>
                  <div className='trade-num font-semibold'>
                    {newAvg > 0 ? formatPrice(newAvg, priceDigits) : '--'}
                  </div>
                </div>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('合计数量')}
                  </Text>
                  <div className='trade-num font-semibold'>
                    {totalQty > 0 ? totalQty.toFixed(4) : '--'}
                  </div>
                </div>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('回本需涨')}
                  </Text>
                  <div className='trade-num font-semibold trade-warning'>
                    {reboundPct > 0 ? `+${reboundPct.toFixed(2)}%` : '0.00%'}
                  </div>
                </div>
              </div>
            </div>
          </TabPane>

          <TabPane tab={t('以损定仓')} itemKey='risk'>
            <div className='flex flex-col gap-3 pt-2'>
              <div className='grid grid-cols-2 gap-2.5'>
                <Input
                  value={capital}
                  onChange={setCapital}
                  prefix={t('总本金')}
                  suffix='USDT'
                  inputMode='decimal'
                />
                <Input
                  value={riskPct}
                  onChange={setRiskPct}
                  prefix={t('单笔愿亏')}
                  suffix='%'
                  inputMode='decimal'
                />
                <Input
                  value={entry}
                  onChange={setEntry}
                  prefix={t('计划开仓')}
                  inputMode='decimal'
                />
                <Input
                  value={stopPrice}
                  onChange={setStopPrice}
                  prefix={t('止损价格')}
                  inputMode='decimal'
                />
              </div>
              <div className='trade-calc-result grid grid-cols-2 gap-3 sm:grid-cols-4'>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('最大亏损额')}
                  </Text>
                  <div className='trade-num font-semibold trade-down'>
                    {maxLossUsdt > 0 ? `${maxLossUsdt.toFixed(2)} USDT` : '--'}
                  </div>
                </div>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('止损幅度')}
                  </Text>
                  <div className='trade-num font-semibold'>
                    {stopGapPct > 0 ? `${stopGapPct.toFixed(2)}%` : '--'}
                  </div>
                </div>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('建议仓位')}
                  </Text>
                  <div className='trade-num font-semibold trade-up'>
                    {recNotional > 0 ? `${recNotional.toFixed(2)} USDT` : '--'}
                  </div>
                </div>
                <div>
                  <Text type='tertiary' size='small'>
                    {t('约需保证金')}
                  </Text>
                  <div className='trade-num font-semibold'>
                    {recMargin > 0
                      ? `${recMargin.toFixed(2)} (${recQty.toFixed(4)})`
                      : '--'}
                  </div>
                </div>
              </div>
            </div>
          </TabPane>
        </Tabs>
      </div>
    </Modal>
  );
};

export default TradeCalculatorModal;
