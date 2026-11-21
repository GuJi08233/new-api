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
  Checkbox,
  Input,
  Modal,
  Select,
  Typography,
} from '@douyinfe/semi-ui';
import { showError, showSuccess } from '../../helpers';
import {
  PlayTurnstile,
  usePlayTurnstile,
} from '../Games/common/usePlayTurnstile';
import { toUsdt, tradePost } from './api';

const { Text } = Typography;

// 在平台额度或游戏币钱包与模拟盘之间转账，均支持小数。
// 游戏币转入不再受每日 15 币限制；游戏币本金只能退回游戏币，盈利只能转出到平台额度。
// 转入时若满足游戏中心任务且全局池有余量，可勾选领取 1:1 体验金加赠；出金时自动回收剩余体验金。
const TransferModal = ({ direction, self, perUnit, onClose, onDone, t }) => {
  const [amount, setAmount] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [wallet, setWallet] = useState('quota');
  const [claimMatch, setClaimMatch] = useState(false);
  const isIn = direction === 'in';
  const isCoin = wallet === 'game_coin';
  const bonus = self?.bonus;
  const activeBonus = self?.account?.bonus_principal || 0;
  const canOfferMatch =
    isIn &&
    activeBonus <= 0 &&
    bonus?.match_enabled &&
    !bonus?.match_claimed &&
    bonus?.task_completed &&
    (bonus?.pool_daily_usd <= 0 || (bonus?.pool_remaining || 0) > 0);
  const turnstile = usePlayTurnstile({
    required: Boolean(claimMatch && canOfferMatch && bonus?.turnstile_required),
    site_key: bonus?.site_key || '',
  });

  useEffect(() => {
    setAmount('');
    setClaimMatch(false);
  }, [direction, wallet]);

  if (!direction || !self) return null;

  const profitCap = self.withdrawable?.profit_out_cap || 0;
  const profitLeft = Math.max(
    profitCap - (self.withdrawable?.profit_out_used || 0),
    0,
  );
  const coinReturnable = self.wallet?.coin_returnable || 0;
  let available = toUsdt(
    isIn ? self.wallet?.quota : self.withdrawable?.quota,
    perUnit,
  );
  if (isCoin && isIn) {
    available = self.wallet?.game_coins || 0;
  } else if (isCoin) {
    available = coinReturnable;
  }
  const max = Math.max(0, available);
  const maxText = (isCoin ? max : Math.floor(max * 1e6) / 1e6).toString();
  const unit = isCoin ? t('游戏币') : 'USDT';

  const submit = async () => {
    const payload = { direction, amount, wallet };
    if (claimMatch && canOfferMatch) {
      payload.claim_match = true;
      if (turnstile.required) {
        payload.turnstile = turnstile.take();
      }
    }
    setSubmitting(true);
    const res = await tradePost('/api/trade/transfer', payload, t);
    setSubmitting(false);
    if (res.error) {
      showError(res.error);
      return;
    }
    showSuccess(isIn ? t('已转入模拟盘') : t('已从模拟盘转出'));
    onDone(res.data);
  };

  const pattern = isCoin ? /^\d+(\.\d{1,6})?$/ : /^\d+(\.\d+)?$/;
  const valid =
    pattern.test(amount) &&
    Number(amount) > 0 &&
    Number(amount) <= max &&
    !(isIn && activeBonus > 0) &&
    (!claimMatch || !canOfferMatch || turnstile.ready);

  const matchPreview =
    claimMatch && canOfferMatch && Number(amount) > 0
      ? Math.min(Number(amount), bonus?.match_max_usd || 0)
      : 0;

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
            isCoin ? t('可输入小数游戏币数量') : t('按美元填写，可以有小数')
          }
          inputMode='decimal'
        />
        <div className='flex items-center justify-between'>
          <Text type='tertiary' size='small'>
            {!isIn
              ? t('最多可转出：{{amount}}', { amount: `${maxText} ${unit}` })
              : isCoin
                ? t('最多可转入：{{amount}}', { amount: `${maxText} ${unit}` })
                : t('主钱包余额：{{amount}}', {
                    amount: `${maxText} ${unit}`,
                  })}
          </Text>
          <Button
            size='small'
            theme='borderless'
            onClick={() => setAmount(maxText)}
          >
            {t('全部')}
          </Button>
        </div>
        {isIn && activeBonus > 0 && (
          <Banner
            type='warning'
            closeIcon={null}
            description={t(
              '当前持有 {{bonus}} USDT 未结清体验金，请先在资产页转出盈利或放弃体验金后再转入自有资金。',
              { bonus: toUsdt(activeBonus, perUnit).toFixed(2) },
            )}
          />
        )}
        {!isIn && activeBonus > 0 && (
          <Banner
            type='warning'
            closeIcon={null}
            description={t(
              '当前持有 {{bonus}} USDT 体验金：转出资金将立即清零回收剩余体验金，且必须先平仓、撤单并结清借款。',
              { bonus: toUsdt(activeBonus, perUnit).toFixed(2) },
            )}
          />
        )}
        {canOfferMatch && (
          <div className='flex flex-col gap-2 rounded-lg border border-[var(--semi-color-border)] p-3'>
            <Checkbox
              checked={claimMatch}
              onChange={(e) => setClaimMatch(e.target.checked)}
            >
              {t('同时领取 1:1 体验金加赠（最高 {{max}} USDT，先到先得）', {
                max: bonus.match_max_usd,
              })}
            </Checkbox>
            {matchPreview > 0 && (
              <Text type='success' size='small'>
                {t('预计额外获赠 {{amount}} USDT 模拟盘体验金', {
                  amount: matchPreview.toFixed(2),
                })}
              </Text>
            )}
            {claimMatch && <PlayTurnstile turnstile={turnstile} />}
          </div>
        )}
        <Banner
          type='info'
          closeIcon={null}
          description={
            <div className='flex flex-col gap-1 text-xs'>
              <span>
                {isCoin
                  ? t(
                      '1 游戏币 = 1 USDT，转入转回均支持小数，且模拟盘真实成交将同步消减游戏币解锁流水。',
                    )
                  : t(
                      '1 USDT = 1 美元额度，平台额度本金与已实现盈利均可转回主钱包。',
                    )}
              </span>
              {isCoin && (
                <span>
                  {t(
                    '游戏币转入的本金仅支持退回游戏币钱包（当前最多可退 {{coins}} 币）；交易盈利请转出到平台额度。',
                    { coins: coinReturnable },
                  )}
                </span>
              )}
              <span>
                {profitCap > 0
                  ? t(
                      '超过本金的部分算盈利，每人每天最多转出 {{cap}} USDT 盈利，今天还能转出 {{left}} USDT。',
                      {
                        cap: toUsdt(profitCap, perUnit),
                        left: toUsdt(profitLeft, perUnit).toFixed(2),
                      },
                    )
                  : t('平台额度本金与交易盈利都能转回主钱包。')}
              </span>
              <span>
                {t('只有可用资金能转出：持仓、挂单和体验金本金不能转出。')}
              </span>
            </div>
          }
        />
      </div>
    </Modal>
  );
};

export default TransferModal;
