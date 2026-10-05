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
  Banner,
  Button,
  Input,
  Modal,
  Select,
  Typography,
} from '@douyinfe/semi-ui';
import { showError, showSuccess } from '../../helpers';
import { toUsdt, tradePost } from './api';

const { Text } = Typography;

// 在平台额度或游戏币钱包与模拟盘之间转账，额度按美元填写，可以有小数，游戏币只接受整数。转出上限由后端算好：只有可用资金能转出，超过本金的部分
// 算盈利，受每天的上限约束。
const TransferModal = ({ direction, self, perUnit, onClose, onDone, t }) => {
  const [amount, setAmount] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [wallet, setWallet] = useState('quota');
  const isIn = direction === 'in';
  const isCoin = wallet === 'game_coin';

  useEffect(() => {
    setAmount('');
  }, [direction, wallet]);

  if (!direction || !self) return null;

  const profitCap = self.withdrawable?.profit_out_cap || 0;
  const profitLeft = Math.max(
    profitCap - (self.withdrawable?.profit_out_used || 0),
    0,
  );
  const available =
    isIn && isCoin
      ? self.wallet?.game_coins || 0
      : toUsdt(isIn ? self.wallet?.quota : self.withdrawable?.quota, perUnit);
  const max = Math.max(0, available);
  const maxText = (
    isCoin ? Math.floor(max) : Math.floor(max * 1e6) / 1e6
  ).toString();
  const unit = isCoin ? t('游戏币') : 'USDT';

  const submit = async () => {
    setSubmitting(true);
    const res = await tradePost(
      '/api/trade/transfer',
      { direction, amount, wallet },
      t,
    );
    setSubmitting(false);
    if (res.error) {
      showError(res.error);
      return;
    }
    showSuccess(isIn ? t('已转入模拟盘') : t('已从模拟盘转出'));
    onDone(res.data);
  };

  const valid =
    (isCoin ? /^\d+$/ : /^\d+(\.\d+)?$/).test(amount) &&
    Number(amount) > 0 &&
    Number(amount) <= max;

  return (
    <Modal
      visible
      title={isIn ? t('转入模拟盘') : t('从模拟盘转出')}
      onCancel={onClose}
      footer={
        <Button
          theme='solid'
          loading={submitting}
          disabled={!valid}
          onClick={submit}
        >
          {isIn ? t('转入') : t('转出')}
        </Button>
      }
    >
      <div className='flex flex-col gap-3'>
        <Select
          value={wallet}
          onChange={setWallet}
          aria-label={t('转账钱包')}
          optionList={[
            { value: 'quota', label: t('平台额度') },
            { value: 'game_coin', label: t('游戏币') },
          ]}
        />
        <Input
          value={amount}
          onChange={setAmount}
          aria-label={isIn ? t('转入') : t('转出')}
          suffix={unit}
          placeholder={
            isCoin ? t('请输入整数游戏币数量') : t('按美元填写，可以有小数')
          }
          inputMode={isCoin ? 'numeric' : 'decimal'}
        />
        <div className='flex items-center justify-between'>
          <Text type='tertiary' size='small'>
            {isIn
              ? t('主钱包余额：{{amount}}', { amount: `${maxText} ${unit}` })
              : t('最多可转出：{{amount}}', { amount: `${maxText} ${unit}` })}
          </Text>
          <Button
            size='small'
            theme='borderless'
            onClick={() => setAmount(maxText)}
          >
            {t('全部')}
          </Button>
        </div>
        <Banner
          type='info'
          closeIcon={null}
          description={
            <div className='flex flex-col gap-1 text-xs'>
              <span>
                {isCoin
                  ? t('1 游戏币 = 1 USDT，只能转入转出整币，钱包从 0 开始。')
                  : t('1 USDT = 1 美元额度，账户从 0 开始，不送初始资金。')}
              </span>
              {isCoin && (
                <span>{t('游戏币与平台额度共用每日盈利转出上限。')}</span>
              )}
              <span>
                {profitCap > 0
                  ? t(
                      '超过转入额度的部分算盈利，每人每天最多转出 {{cap}} USDT 盈利，今天还能转出 {{left}} USDT。',
                      {
                        cap: toUsdt(profitCap, perUnit),
                        left: toUsdt(profitLeft, perUnit).toFixed(2),
                      },
                    )
                  : t('转入的额度与交易盈利都能转回主钱包。')}
              </span>
              <span>
                {t('只有可用资金能转出：持仓和挂单占用的资金不能转出。')}
              </span>
            </div>
          }
        />
      </div>
    </Modal>
  );
};

export default TransferModal;
