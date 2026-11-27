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

import React, { useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Input,
  InputNumber,
  Modal,
  Radio,
  RadioGroup,
  Select,
  Slider,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  CheckCircle2,
  Layers,
  Plus,
  Rocket,
  Sparkles,
  Trash2,
  XCircle,
} from 'lucide-react';
import { showError, showSuccess } from '../../helpers';
import { formatPrice, toUsdt, tradePost, trendClass } from './api';
import { SymbolAvatar } from './SymbolIdentity';
import {
  PRESET_BASKETS,
  buildBasketOrderPlan,
  evaluateBasket,
  normalizeWeights,
  readCustomBaskets,
  writeCustomBaskets,
} from './baskets';
import { TRADE_SYMBOL_META, formatSymbolLabel, getSymbolName } from './symbols';

const { Text, Title } = Typography;
const QUICK_AMOUNTS = [50, 100, 200, 500];

const PLAN_ERRORS = {
  spot_closed: '现货未开放',
  futures_closed: '合约未开放',
  no_price: '暂无实时报价',
  below_min_qty: '低于最小下单量',
  below_min_notional: '低于最小下单额',
};

// 主题组合 / 一键篮子投资：按 AI 大模型、算力芯片、科技巨头、加密蓝筹、宏观避险等主题一键批量下单，支持自定义权重篮子。
const BasketsView = ({
  self,
  perUnit,
  spotMarket,
  futuresMarket,
  spotQuotes,
  futuresQuotes,
  onOpenSpot,
  onOpenFutures,
  onAccountChanged,
  t,
}) => {
  const [customBaskets, setCustomBaskets] = useState(readCustomBaskets);
  const [activeBasket, setActiveBasket] = useState(null);
  const [execMode, setExecMode] = useState('spot');
  const [futuresSide, setFuturesSide] = useState('long');
  const [marginMode, setMarginMode] = useState('cross');
  const [leverage, setLeverage] = useState(5);
  const [totalUsdt, setTotalUsdt] = useState('100');
  const [running, setRunning] = useState(false);
  const [results, setResults] = useState(null);

  // 自定义组合弹窗状态
  const [creating, setCreating] = useState(false);
  const [draftName, setDraftName] = useState('');
  const [draftLegs, setDraftLegs] = useState([
    { ticker: 'NVDA', weight: 50 },
    { ticker: 'ANT', weight: 50 },
  ]);

  const spotByTicker = useMemo(
    () =>
      new Map((spotMarket?.symbols || []).map((item) => [item.ticker, item])),
    [spotMarket],
  );
  const futuresByTicker = useMemo(
    () =>
      new Map(
        (futuresMarket?.symbols || []).map((item) => [item.ticker, item]),
      ),
    [futuresMarket],
  );

  const allBaskets = useMemo(
    () => [
      ...PRESET_BASKETS.map((b) => ({ ...b, custom: false })),
      ...customBaskets.map((b) => ({ ...b, custom: true })),
    ],
    [customBaskets],
  );

  const evaluatedList = useMemo(
    () =>
      allBaskets.map((basket) => ({
        basket,
        evalInfo: evaluateBasket(
          basket,
          spotByTicker,
          futuresByTicker,
          spotQuotes,
          futuresQuotes,
        ),
      })),
    [allBaskets, spotByTicker, futuresByTicker, spotQuotes, futuresQuotes],
  );

  const activeEntry = useMemo(
    () => evaluatedList.find((e) => e.basket.id === activeBasket?.id) || null,
    [evaluatedList, activeBasket],
  );

  const openDeployModal = (entry) => {
    setActiveBasket(entry.basket);
    setResults(null);
    const defaultMode = entry.evalInfo.canSpot ? 'spot' : 'futures';
    setExecMode(defaultMode);
    setFuturesSide('long');
    setLeverage(Math.min(5, entry.evalInfo.maxFuturesLeverage));
    setTotalUsdt('100');
  };

  const plan = useMemo(() => {
    if (!activeEntry) return { items: [], valid: false };
    return buildBasketOrderPlan({
      legs: activeEntry.evalInfo.legs,
      mode: execMode,
      side: futuresSide,
      marginMode,
      leverage,
      totalUsdt,
    });
  }, [activeEntry, execMode, futuresSide, marginMode, leverage, totalUsdt]);

  const executeBasket = async () => {
    if (!plan.valid || !plan.items.length) return;
    setRunning(true);
    setResults([]);
    let okCount = 0;
    const outcome = [];
    for (const item of plan.items) {
      const res = await tradePost(item.endpoint, item.payload, t);
      if (res.error) {
        outcome.push({ ...item, ok: false, message: res.error });
      } else {
        okCount++;
        outcome.push({ ...item, ok: true, order: res.data });
      }
      setResults([...outcome]);
    }
    setRunning(false);
    onAccountChanged?.();
    if (okCount === plan.items.length) {
      showSuccess(
        t('组合下单完成：{{ok}}/{{total}} 笔已成交', {
          ok: okCount,
          total: plan.items.length,
        }),
      );
    } else {
      showError(
        t('组合部分完成：{{ok}}/{{total}} 笔成功，请查看明细', {
          ok: okCount,
          total: plan.items.length,
        }),
      );
    }
  };

  const allTickerOptions = useMemo(() => {
    const tickers = new Set([
      ...spotByTicker.keys(),
      ...futuresByTicker.keys(),
      ...Object.keys(TRADE_SYMBOL_META),
    ]);
    return [...tickers].map((ticker) => ({
      value: ticker,
      label: formatSymbolLabel(ticker, t),
    }));
  }, [spotByTicker, futuresByTicker, t]);

  const saveCustomBasket = () => {
    const name = draftName.trim();
    if (!name) {
      showError(t('请输入组合名称'));
      return;
    }
    const dedup = [];
    const seen = new Set();
    for (const leg of draftLegs) {
      if (!leg.ticker || seen.has(leg.ticker)) continue;
      seen.add(leg.ticker);
      dedup.push({
        ticker: leg.ticker,
        weight: Math.max(1, Number(leg.weight) || 1),
      });
    }
    if (dedup.length < 2) {
      showError(t('自定义组合至少包含 2 个不同品种'));
      return;
    }
    const next = [
      ...customBaskets,
      {
        id: `custom_${Date.now()}`,
        name,
        description: t('自定义配置组合'),
        tag: t('自建'),
        color: 'green',
        legs: normalizeWeights(dedup),
      },
    ];
    setCustomBaskets(next);
    writeCustomBaskets(next);
    setCreating(false);
    setDraftName('');
    showSuccess(t('已保存自定义组合'));
  };

  const deleteCustomBasket = (id) => {
    const next = customBaskets.filter((item) => item.id !== id);
    setCustomBaskets(next);
    writeCustomBaskets(next);
  };

  const cashUsdt = toUsdt(self?.account?.cash, perUnit);

  return (
    <div className='flex flex-col gap-4'>
      <div className='trade-card trade-hero-banner flex flex-wrap items-center justify-between gap-4'>
        <div className='flex flex-col gap-1'>
          <div className='flex items-center gap-2'>
            <Layers size={20} className='text-[var(--semi-color-primary)]' />
            <Title heading={5} className='!mb-0'>
              {t('主题组合 · 一键篮子配置')}
            </Title>
            <Tag size='small' color='violet'>
              {t('新玩法')}
            </Tag>
          </div>
          <Text type='secondary' size='small'>
            {t(
              '无需逐个搜索下单：按大模型、AI 算力、科技巨头、中国 AI 或宏观避险等赛道一键按权重分散配置，支持现货买入与合约多空，也可自建专属投资篮子。',
            )}
          </Text>
        </div>
        <Button
          theme='solid'
          icon={<Plus size={15} />}
          onClick={() => setCreating(true)}
        >
          {t('新建自定义组合')}
        </Button>
      </div>

      <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'>
        {evaluatedList.map((entry) => {
          const { basket, evalInfo } = entry;
          const change = evalInfo.weightedChange;
          const cls = change === null ? 'trade-flat' : trendClass(change);
          const canDeploy = evalInfo.canSpot || evalInfo.canFutures;
          return (
            <div key={basket.id} className='trade-card trade-basket-card'>
              <div className='flex items-start justify-between gap-2'>
                <div className='flex flex-col gap-1 min-w-0'>
                  <div className='flex items-center gap-2'>
                    <Title heading={6} className='!mb-0 truncate'>
                      {basket.custom ? basket.name : t(basket.name)}
                    </Title>
                    <Tag
                      size='small'
                      color={basket.color || 'blue'}
                      type='light'
                    >
                      {basket.custom ? basket.tag : t(basket.tag)}
                    </Tag>
                  </div>
                  <Text type='tertiary' size='small'>
                    {basket.custom ? basket.description : t(basket.description)}
                  </Text>
                </div>
                <div className='flex items-center gap-1.5 shrink-0'>
                  <span className={`trade-change-pill ${cls}`}>
                    {change === null
                      ? '--'
                      : `${change > 0 ? '+' : ''}${change.toFixed(2)}%`}
                  </span>
                  {basket.custom && (
                    <Button
                      size='small'
                      theme='borderless'
                      type='danger'
                      icon={<Trash2 size={14} />}
                      aria-label={t('删除组合')}
                      onClick={() => deleteCustomBasket(basket.id)}
                    />
                  )}
                </div>
              </div>

              <div className='flex flex-col gap-1.5 my-1'>
                {evalInfo.legs.map((leg) => {
                  const legCls =
                    leg.change === null
                      ? 'trade-muted'
                      : trendClass(leg.change);
                  const jump = () => {
                    if (leg.spotItem) onOpenSpot?.(leg.spotItem.symbol);
                    else if (leg.futuresItem)
                      onOpenFutures?.(leg.futuresItem.symbol);
                  };
                  return (
                    <div
                      key={leg.ticker}
                      role='button'
                      tabIndex={0}
                      className='trade-basket-leg'
                      onClick={jump}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault();
                          jump();
                        }
                      }}
                    >
                      <div className='flex items-center gap-2 min-w-0'>
                        <SymbolAvatar item={leg.ticker} size={22} />
                        <Text strong size='small' className='truncate'>
                          {getSymbolName(leg.ticker, t)}
                        </Text>
                        <Text
                          type='tertiary'
                          size='small'
                          className='!text-[11px]'
                        >
                          {leg.weight}%
                        </Text>
                      </div>
                      <div className='flex items-center gap-2 trade-num text-xs shrink-0'>
                        <Text type='secondary' size='small'>
                          {leg.price ? formatPrice(leg.price) : '--'}
                        </Text>
                        <span
                          className={`w-14 text-right font-medium ${legCls}`}
                        >
                          {leg.change === null
                            ? '--'
                            : `${leg.change > 0 ? '+' : ''}${leg.change.toFixed(2)}%`}
                        </span>
                      </div>
                    </div>
                  );
                })}
              </div>

              <div className='mt-auto flex items-center justify-between gap-2 border-t border-[var(--semi-color-border)] pt-3'>
                <div className='flex items-center gap-1'>
                  {evalInfo.canSpot && (
                    <Tag size='small' color='green' type='ghost'>
                      {t('支持现货')}
                    </Tag>
                  )}
                  {evalInfo.canFutures && (
                    <Tag size='small' color='blue' type='ghost'>
                      {t('支持合约')}
                    </Tag>
                  )}
                  {!canDeploy && (
                    <Tag size='small' color='grey'>
                      {t('含未开放品种')}
                    </Tag>
                  )}
                </div>
                <Button
                  size='small'
                  theme='solid'
                  icon={<Rocket size={13} />}
                  disabled={!canDeploy || !self?.enabled}
                  onClick={() => openDeployModal(entry)}
                >
                  {t('一键配置')}
                </Button>
              </div>
            </div>
          );
        })}
      </div>

      {/* 一键下单确认弹窗 */}
      <Modal
        visible={!!activeEntry}
        onCancel={() => !running && setActiveBasket(null)}
        width={580}
        title={
          activeEntry
            ? `${t('一键配置组合')} · ${
                activeEntry.basket.custom
                  ? activeEntry.basket.name
                  : t(activeEntry.basket.name)
              }`
            : ''
        }
        footer={
          <div className='flex items-center justify-between'>
            <Text type='tertiary' size='small' className='trade-num'>
              {t('可用资金 {{amount}} USDT', { amount: cashUsdt.toFixed(2) })}
            </Text>
            <div className='flex gap-2'>
              <Button disabled={running} onClick={() => setActiveBasket(null)}>
                {t('关闭')}
              </Button>
              <Button
                theme='solid'
                icon={<Sparkles size={14} />}
                loading={running}
                disabled={!plan.valid}
                onClick={executeBasket}
              >
                {execMode === 'spot'
                  ? t('一键市价买入 ({{count}} 笔)', {
                      count: plan.items.length,
                    })
                  : futuresSide === 'long'
                    ? t('一键开多 {{lev}}x ({{count}} 笔)', {
                        lev: leverage,
                        count: plan.items.length,
                      })
                    : t('一键开空 {{lev}}x ({{count}} 笔)', {
                        lev: leverage,
                        count: plan.items.length,
                      })}
              </Button>
            </div>
          </div>
        }
      >
        {activeEntry && (
          <div className='flex flex-col gap-3'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <RadioGroup
                type='button'
                value={execMode}
                onChange={(e) => setExecMode(e.target.value)}
              >
                <Radio value='spot' disabled={!activeEntry.evalInfo.canSpot}>
                  {t('现货买入')}
                </Radio>
                <Radio
                  value='futures'
                  disabled={!activeEntry.evalInfo.canFutures}
                >
                  {t('永续合约')}
                </Radio>
              </RadioGroup>
              {execMode === 'futures' && (
                <div className='flex items-center gap-2'>
                  <RadioGroup
                    type='button'
                    size='small'
                    value={futuresSide}
                    onChange={(e) => setFuturesSide(e.target.value)}
                  >
                    <Radio value='long'>{t('做多')}</Radio>
                    <Radio value='short'>{t('做空')}</Radio>
                  </RadioGroup>
                  <RadioGroup
                    type='button'
                    size='small'
                    value={marginMode}
                    onChange={(e) => setMarginMode(e.target.value)}
                  >
                    <Radio value='cross'>{t('全仓')}</Radio>
                    <Radio value='isolated'>{t('逐仓')}</Radio>
                  </RadioGroup>
                </div>
              )}
            </div>

            <div className='flex flex-col gap-2'>
              <Input
                value={totalUsdt}
                onChange={setTotalUsdt}
                prefix={execMode === 'spot' ? t('总投入金额') : t('总保证金')}
                suffix='USDT'
                inputMode='decimal'
              />
              <div className='flex flex-wrap gap-1.5'>
                {QUICK_AMOUNTS.map((val) => (
                  <Button
                    key={val}
                    size='small'
                    theme='light'
                    onClick={() => setTotalUsdt(String(val))}
                  >
                    ${val}
                  </Button>
                ))}
                {cashUsdt >= 20 && (
                  <Button
                    size='small'
                    theme='light'
                    onClick={() =>
                      setTotalUsdt(
                        (Math.floor(cashUsdt * 0.5 * 100) / 100).toFixed(2),
                      )
                    }
                  >
                    {t('半仓')}
                  </Button>
                )}
              </div>
            </div>

            {execMode === 'futures' && (
              <div className='flex flex-col gap-1'>
                <div className='flex justify-between text-xs'>
                  <Text type='tertiary' size='small'>
                    {t('组合统一杠杆 (受各合约上限约束)')}
                  </Text>
                  <Text strong className='trade-num'>
                    {leverage}x
                  </Text>
                </div>
                <Slider
                  min={1}
                  max={Math.max(1, activeEntry.evalInfo.maxFuturesLeverage)}
                  value={leverage}
                  onChange={setLeverage}
                />
              </div>
            )}

            <div className='flex flex-col gap-1.5 border rounded-lg border-[var(--semi-color-border)] p-2.5'>
              {plan.items.map((item) => {
                const resItem = results?.find((r) => r.ticker === item.ticker);
                return (
                  <div
                    key={item.ticker}
                    className='flex items-center justify-between gap-2 text-xs trade-num py-1 border-b last:border-b-0 border-[var(--semi-color-border)]'
                  >
                    <div className='flex items-center gap-2 min-w-0'>
                      <SymbolAvatar item={item.ticker} size={20} />
                      <Text strong size='small' className='truncate'>
                        {getSymbolName(item.ticker, t)}
                      </Text>
                      <Tag size='small'>{item.weight}%</Tag>
                    </div>
                    <div className='flex items-center gap-2 shrink-0'>
                      <span>
                        {item.allocUsd.toFixed(2)} USDT
                        {execMode === 'futures' && item.qty
                          ? ` (${item.qty} ${item.ticker})`
                          : ''}
                      </span>
                      {item.error && (
                        <Tag size='small' color='red'>
                          {t(PLAN_ERRORS[item.error] || item.error)}
                        </Tag>
                      )}
                      {resItem &&
                        (resItem.ok ? (
                          <CheckCircle2 size={14} className='trade-up' />
                        ) : (
                          <Tag size='small' color='red'>
                            <span className='inline-flex items-center gap-1'>
                              <XCircle size={12} />
                              {resItem.message}
                            </span>
                          </Tag>
                        ))}
                    </div>
                  </div>
                );
              })}
            </div>

            <Banner
              type='info'
              closeIcon={null}
              description={t(
                '组合下单会按每笔权重依次向模拟盘提交真实市价委托，遵循单笔上限、持仓上限和手续费规则。',
              )}
            />
          </div>
        )}
      </Modal>

      {/* 新建自定义组合弹窗 */}
      <Modal
        visible={creating}
        onCancel={() => setCreating(false)}
        width={520}
        title={t('新建自定义组合')}
        onOk={saveCustomBasket}
        okText={t('保存组合')}
      >
        <div className='flex flex-col gap-3'>
          <Input
            value={draftName}
            onChange={setDraftName}
            prefix={t('组合名称')}
            placeholder={t('例如：我的 AI 全明星组合')}
            maxLength={32}
          />
          <div className='flex items-center justify-between'>
            <Text type='tertiary' size='small'>
              {t('选择 2–8 个品种并分配权重（保存时自动归一化为 100%）')}
            </Text>
            <Button
              size='small'
              theme='borderless'
              onClick={() => {
                const equal = Math.floor(100 / draftLegs.length);
                setDraftLegs((prev) =>
                  prev.map((leg) => ({ ...leg, weight: equal })),
                );
              }}
            >
              {t('平均权重')}
            </Button>
          </div>
          <div className='flex flex-col gap-2'>
            {draftLegs.map((leg, idx) => (
              <div key={idx} className='flex items-center gap-2'>
                <Select
                  filter
                  style={{ flex: 1 }}
                  value={leg.ticker}
                  optionList={allTickerOptions}
                  onChange={(value) =>
                    setDraftLegs((prev) =>
                      prev.map((item, i) =>
                        i === idx ? { ...item, ticker: value } : item,
                      ),
                    )
                  }
                />
                <InputNumber
                  style={{ width: 100 }}
                  min={1}
                  max={100}
                  suffix='%'
                  value={leg.weight}
                  onChange={(value) =>
                    setDraftLegs((prev) =>
                      prev.map((item, i) =>
                        i === idx ? { ...item, weight: value || 1 } : item,
                      ),
                    )
                  }
                />
                {draftLegs.length > 2 && (
                  <Button
                    size='small'
                    theme='borderless'
                    type='danger'
                    icon={<Trash2 size={14} />}
                    onClick={() =>
                      setDraftLegs((prev) => prev.filter((_, i) => i !== idx))
                    }
                  />
                )}
              </div>
            ))}
          </div>
          {draftLegs.length < 8 && (
            <Button
              size='small'
              theme='light'
              icon={<Plus size={14} />}
              onClick={() =>
                setDraftLegs((prev) => [
                  ...prev,
                  { ticker: 'GOOGL', weight: 20 },
                ])
              }
            >
              {t('添加品种')}
            </Button>
          )}
        </div>
      </Modal>
    </div>
  );
};

export default BasketsView;
