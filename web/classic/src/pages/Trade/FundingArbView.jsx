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
  Modal,
  Radio,
  RadioGroup,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { ShieldCheck, Sparkles } from 'lucide-react';
import { showError, showSuccess } from '../../helpers';
import {
  formatFundingRate,
  formatPrice,
  toUsdt,
  tradePost,
  trendClass,
} from './api';
import SymbolIdentity from './SymbolIdentity';
import { formatSymbolLabel } from './symbols';
import { buildFundingArbPlan, rankFundingArbOpportunities } from './strategies';

const { Text, Title } = Typography;
const QUICK_CAPITALS = [100, 200, 500, 1000];

// 期现资金费率套利：自动扫描正资金费率最高的双挂品种，一键执行「现货买入 + 等量逐仓合约做空」对冲价格波动、稳吃每 8 小时资金费。
const FundingArbView = ({
  self,
  perUnit,
  spotMarket,
  futuresMarket,
  spotQuotes,
  futuresQuotes,
  onAccountChanged,
  t,
}) => {
  const [selected, setSelected] = useState(null);
  const [capital, setCapital] = useState('200');
  const [leverage, setLeverage] = useState(1);
  const [submitting, setSubmitting] = useState(false);

  const opportunities = useMemo(
    () =>
      rankFundingArbOpportunities(
        spotMarket?.symbols || [],
        futuresMarket?.symbols || [],
        spotQuotes,
        futuresQuotes,
      ),
    [spotMarket, futuresMarket, spotQuotes, futuresQuotes],
  );

  const activeOpp = useMemo(
    () => opportunities.find((o) => o.ticker === selected?.ticker) || null,
    [opportunities, selected],
  );

  const plan = useMemo(
    () =>
      buildFundingArbPlan({
        opportunity: activeOpp,
        totalUsdt: capital,
        leverage,
        spotFeeBps: spotMarket?.fee_bps ?? 10,
        futuresTakerFeeBps: futuresMarket?.taker_fee_bps ?? 4,
      }),
    [
      activeOpp,
      capital,
      leverage,
      spotMarket?.fee_bps,
      futuresMarket?.taker_fee_bps,
    ],
  );

  const executeArb = async () => {
    if (!plan.valid || !activeOpp) return;
    setSubmitting(true);
    const spotRes = await tradePost('/api/trade/orders', plan.spotPayload, t);
    if (spotRes.error) {
      setSubmitting(false);
      showError(`${t('现货腿买入失败')}：${spotRes.error}`);
      return;
    }
    const futRes = await tradePost(
      '/api/trade/futures/orders',
      plan.futuresPayload,
      t,
    );
    setSubmitting(false);
    onAccountChanged?.();
    if (futRes.error) {
      showError(
        t(
          '现货已成交，但合约空单失败({{err}})，请前往合约页手动补空或平掉现货',
          {
            err: futRes.error,
          },
        ),
      );
      return;
    }
    showSuccess(
      t('期现对冲建仓成功：已买入现货并同步开出等量 {{lev}}x 空单', {
        lev: leverage,
      }),
    );
    setSelected(null);
  };

  const cashUsdt = toUsdt(self?.account?.cash, perUnit);
  const canTrade = !!self?.enabled && !!self?.futures?.enabled;

  const columns = [
    {
      title: t('套利品种'),
      dataIndex: 'ticker',
      width: 220,
      render: (_, row) => (
        <SymbolIdentity item={row.spot} showSector size={30} t={t} />
      ),
    },
    {
      title: t('当前资金费率'),
      dataIndex: 'fundingRate',
      align: 'right',
      render: (rate) => (
        <span className={`trade-num font-semibold ${trendClass(rate)}`}>
          {formatFundingRate(rate)}
        </span>
      ),
    },
    {
      title: t('参考年化 (APR)'),
      dataIndex: 'aprPct',
      align: 'right',
      render: (apr, row) =>
        row.fundingRate === null ? (
          '--'
        ) : (
          <Tag color={apr >= 15 ? 'green' : apr > 0 ? 'blue' : 'grey'}>
            <span className='trade-num font-semibold'>
              {apr > 0 ? '+' : ''}
              {apr.toFixed(2)}%
            </span>
          </Tag>
        ),
    },
    {
      title: t('每千刀日息'),
      dataIndex: 'dailyPer1000',
      align: 'right',
      render: (val, row) =>
        row.fundingRate === null ? (
          '--'
        ) : (
          <span className={`trade-num ${trendClass(val)}`}>
            {val > 0 ? '+' : ''}
            {val.toFixed(3)} USDT
          </span>
        ),
    },
    {
      title: t('现货 / 合约价差'),
      align: 'right',
      render: (_, row) => (
        <div className='flex flex-col items-end leading-tight trade-num text-xs'>
          <span>
            {formatPrice(row.spotPrice)} / {formatPrice(row.futuresPrice)}
          </span>
          <span className={trendClass(row.basisPct)}>
            {row.basisPct > 0 ? '+' : ''}
            {row.basisPct.toFixed(3)}%
          </span>
        </div>
      ),
    },
    {
      title: '',
      align: 'right',
      width: 110,
      render: (_, row) => (
        <Button
          size='small'
          theme={row.aprPct > 0 ? 'solid' : 'light'}
          disabled={
            !canTrade || !(row.spotPrice > 0) || !(row.futuresPrice > 0)
          }
          onClick={() => {
            setSelected(row);
            setCapital('200');
            setLeverage(1);
          }}
        >
          {t('一键套利')}
        </Button>
      ),
    },
  ];

  return (
    <div className='flex flex-col gap-4'>
      <Banner
        type='info'
        closeIcon={null}
        description={t(
          '资金费率套利原理：当永续合约资金费率为正时，同时「买入现货 + 1x 等量合约做空」锁定价格涨跌（Delta 中性），每 4–8 小时稳定收取多头支付的资金费。完全复用真实模拟盘现货与合约撮合。',
        )}
      />

      <div className='trade-card flex flex-col gap-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex items-center gap-2'>
            <ShieldCheck size={18} className='trade-up' />
            <Title heading={6} className='!mb-0'>
              {t('实时资金费率套利榜')}
            </Title>
            <Tag size='small' color='green'>
              {t('共 {{count}} 个双挂品种', { count: opportunities.length })}
            </Tag>
          </div>
          <Text type='tertiary' size='small' className='trade-num'>
            {t('可用资金 {{amount}} USDT', { amount: cashUsdt.toFixed(2) })}
          </Text>
        </div>

        <Table
          size='small'
          rowKey='ticker'
          columns={columns}
          dataSource={opportunities}
          pagination={false}
          scroll={{ x: 780 }}
          empty={
            <Text type='tertiary'>{t('暂无同时开放现货与合约的套利品种')}</Text>
          }
        />
      </div>

      <Modal
        visible={!!activeOpp}
        onCancel={() => !submitting && setSelected(null)}
        width={520}
        title={
          activeOpp
            ? `${t('一键期现套利')} · ${formatSymbolLabel(activeOpp.spot, t)}`
            : ''
        }
        footer={
          <div className='flex justify-between items-center'>
            <Text type='tertiary' size='small' className='trade-num'>
              {t('可用资金 {{amount}} USDT', { amount: cashUsdt.toFixed(2) })}
            </Text>
            <div className='flex gap-2'>
              <Button disabled={submitting} onClick={() => setSelected(null)}>
                {t('取消')}
              </Button>
              <Button
                theme='solid'
                icon={<Sparkles size={14} />}
                loading={submitting}
                disabled={!plan.valid}
                onClick={executeArb}
              >
                {t('一键对冲建仓')}
              </Button>
            </div>
          </div>
        }
      >
        {activeOpp && (
          <div className='flex flex-col gap-3'>
            <Input
              value={capital}
              onChange={setCapital}
              prefix={t('总投入资金')}
              suffix='USDT'
              inputMode='decimal'
            />
            <div className='flex flex-wrap gap-1.5'>
              {QUICK_CAPITALS.map((v) => (
                <Button
                  key={v}
                  size='small'
                  theme='light'
                  onClick={() => setCapital(String(v))}
                >
                  ${v}
                </Button>
              ))}
            </div>
            <div className='flex items-center justify-between'>
              <Text type='tertiary' size='small'>
                {t('空单逐仓杠杆（1x 零爆仓风险，2x 资金利用率更高）')}
              </Text>
              <RadioGroup
                type='button'
                size='small'
                value={leverage}
                onChange={(e) => setLeverage(e.target.value)}
              >
                <Radio value={1}>1x</Radio>
                <Radio value={2}>2x</Radio>
              </RadioGroup>
            </div>
            <div className='trade-calc-result grid grid-cols-2 gap-2.5 text-xs trade-num'>
              <div>
                <Text type='tertiary' size='small'>
                  {t('对冲数量')}
                </Text>
                <div className='font-semibold'>
                  {plan.qty ? `${plan.qty} ${activeOpp.ticker}` : '--'}
                </div>
              </div>
              <div>
                <Text type='tertiary' size='small'>
                  {t('现货买单 / 空单保证金')}
                </Text>
                <div className='font-semibold'>
                  {plan.valid
                    ? `${plan.spotNotional.toFixed(2)} / ${plan.futuresMargin.toFixed(2)} USDT`
                    : '--'}
                </div>
              </div>
              <div>
                <Text type='tertiary' size='small'>
                  {t('预估每日资金费收入')}
                </Text>
                <div
                  className={`font-semibold ${trendClass(plan.dailyIncome)}`}
                >
                  {plan.valid
                    ? `${plan.dailyIncome > 0 ? '+' : ''}${plan.dailyIncome.toFixed(4)} USDT`
                    : '--'}
                </div>
              </div>
              <div>
                <Text type='tertiary' size='small'>
                  {t('预计回本周期(含双边费)')}
                </Text>
                <div className='font-semibold'>
                  {plan.breakevenDays
                    ? t('约 {{days}} 天', {
                        days: plan.breakevenDays.toFixed(1),
                      })
                    : '--'}
                </div>
              </div>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default FundingArbView;
