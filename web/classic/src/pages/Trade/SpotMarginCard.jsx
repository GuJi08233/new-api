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

import React, { useState } from 'react';
import { Button, Input, Tag, Typography } from '@douyinfe/semi-ui';
import { showError, showSuccess } from '../../helpers';
import { formatUsdt, toUsdt, tradePost } from './api';

const { Text } = Typography;

// 现货借款卡：有借款时显示本金、未还利息、实时日利率和风险率。风险率照 Binance 全仓杠杆的线着色：不高于追加保证金线变橙，
// 不高于强平线变红。还款只从模拟盘资金里扣，不动主钱包。
const SpotMarginCard = ({ self, perUnit, onChanged, t }) => {
  const [repay, setRepay] = useState('');
  const [busy, setBusy] = useState(false);
  const valuation = self?.valuation;
  const margin = valuation?.spot_margin;
  const debt = valuation?.spot_debt || 0;
  const spot = self?.spot || {};

  const doRepay = async () => {
    const amount = Number(repay);
    if (!(amount > 0)) return;
    setBusy(true);
    const result = await tradePost(
      '/api/trade/spot/repay',
      { amount: repay },
      t,
    );
    setBusy(false);
    if (result.error) {
      showError(result.error);
      return;
    }
    setRepay('');
    showSuccess(t('还款成功'));
    onChanged?.(result.data);
  };

  if (!margin || debt <= 0) return null;
  const fresh = valuation.spot_prices_fresh !== false;
  const level = Number(valuation.spot_level || 0);
  const levelColor = !fresh
    ? 'grey'
    : level <= Number(spot.liquidation_level || 1.1)
      ? 'red'
      : level <= Number(spot.margin_call_level || 1.3)
        ? 'orange'
        : 'green';
  return (
    <div className='trade-card flex flex-col gap-3'>
      <div className='flex items-center justify-between'>
        <Text strong>{t('现货借款')}</Text>
        <Tag color={levelColor}>
          {t('风险率')} {fresh ? level.toFixed(2) : '--'}
        </Tag>
      </div>
      <div className='grid grid-cols-2 gap-2 text-sm'>
        <div>
          <Text type='tertiary' size='small'>
            {t('借款本金')}
          </Text>
          <div className='trade-num'>
            {formatUsdt(margin.principal, perUnit)} USDT
          </div>
        </div>
        <div>
          <Text type='tertiary' size='small'>
            {t('未还利息')}
          </Text>
          <div className='trade-num'>
            {formatUsdt(margin.interest, perUnit, 4)} USDT
          </div>
        </div>
        <div>
          <Text type='tertiary' size='small'>
            {t('日利率')}
          </Text>
          <div className='trade-num'>
            {(Number(spot.daily_rate || 0) * 100).toFixed(4)}%
          </div>
        </div>
        <div>
          <Text type='tertiary' size='small'>
            {t('强平线')}
          </Text>
          <div className='trade-num'>{spot.liquidation_level}</div>
        </div>
      </div>
      <Text type='tertiary' size='small'>
        {t(
          '风险率 = (资金 + 现货按买一的市值) ÷ (借款本金 + 利息)。不高于 {{call}} 提醒追加保证金，不高于 {{liquidation}} 强平并收还款额 2% 的强平费；转出资金后不能低于 {{transfer}}。利息按 Binance 借 USDT 的实时利率每小时收一次。',
          {
            call: spot.margin_call_level,
            liquidation: spot.liquidation_level,
            transfer: spot.transfer_level,
          },
        )}
      </Text>
      {!fresh && (
        <Text type='warning' size='small'>
          {t('现货盘口暂时不新鲜，风险率和可转出金额稍后更新')}
        </Text>
      )}
      <div className='flex gap-2'>
        <Input
          value={repay}
          onChange={setRepay}
          suffix='USDT'
          prefix={t('还款')}
          inputMode='decimal'
          aria-label={t('还款金额')}
        />
        <Button
          theme='light'
          onClick={() =>
            setRepay((Math.ceil(toUsdt(debt, perUnit) * 1e6) / 1e6).toString())
          }
        >
          {t('全部')}
        </Button>
        <Button
          theme='solid'
          type='primary'
          loading={busy}
          disabled={!Number(repay)}
          onClick={doRepay}
        >
          {t('还款')}
        </Button>
      </div>
    </div>
  );
};

export default SpotMarginCard;
