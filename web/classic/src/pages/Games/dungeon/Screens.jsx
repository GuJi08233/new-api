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

import React from 'react';
import { Button } from '@douyinfe/semi-ui';
import { Coins, FlaskConical } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CardFace, RelicChip, useDungeonText } from './parts';

// 战斗或宝箱的奖励：金币、遗物、药水已经到手，卡牌三选一或跳过。
export const RewardScreen = ({ run, busy, send }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const reward = run.reward;
  return (
    <div className='dg-screen dg-reward'>
      <h3>
        {reward.boss
          ? t('首领倒下了')
          : reward.cards.length
            ? t('战斗胜利')
            : t('收取宝藏')}
      </h3>
      <div className='dg-loot'>
        {reward.gold > 0 && (
          <span className='dg-loot-item'>
            <Coins size={16} />
            {t('灵珠 +{{n}}', { n: reward.gold })}
          </span>
        )}
        {reward.relic && <RelicChip id={reward.relic} fresh />}
        {reward.potion && (
          <span
            className={`dg-loot-item ${reward.potion_lost ? 'is-lost' : ''}`}
          >
            <FlaskConical size={16} />
            {text.potions[reward.potion]?.name}
            {reward.potion_lost && <small>{t('丹符栏已满，没有拿到')}</small>}
          </span>
        )}
      </div>
      {reward.cards.length > 0 && (
        <>
          <p className='dg-screen-note'>{t('选一张牌加入牌组，也可以跳过')}</p>
          <div className='dg-choices'>
            {reward.cards.map((card, index) => (
              <CardFace
                key={index}
                card={card}
                className='dg-deal'
                style={{ '--i': index }}
                disabled={busy}
                onClick={() => send('reward', { choice: index })}
              />
            ))}
          </div>
        </>
      )}
      <Button disabled={busy} onClick={() => send('reward', { choice: -1 })}>
        {reward.cards.length ? t('跳过') : t('继续前进')}
      </Button>
      {reward.boss && (
        <p className='dg-screen-note'>
          {t('离开后进入下一重，并回复一部分已损失的生命')}
        </p>
      )}
    </div>
  );
};
