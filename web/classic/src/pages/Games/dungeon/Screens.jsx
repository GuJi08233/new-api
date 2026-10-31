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
import { Button } from '@douyinfe/semi-ui';
import {
  Coins,
  FlaskConical,
  Gem,
  Hammer,
  HeartPulse,
  Scissors,
  Skull,
  Star,
  Trophy,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { StageMessage } from '../common/SideBox';
import { skillResultMessage } from '../common/skill';
import { CardFace, DeckPicker, RelicChip, useDungeonText } from './parts';
import { getClassText, getEventText } from './text';

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

export const ShopScreen = ({ run, busy, send }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const shop = run.shop;
  const [removing, setRemoving] = useState(false);
  return (
    <div className='dg-screen dg-shop'>
      <h3>{t('坊市')}</h3>
      <p className='dg-screen-note'>
        {t('你有 {{gold}} 灵珠', { gold: run.gold })}
      </p>
      <div className='dg-offers'>
        {shop.offers.map((offer, index) => {
          const afford = offer.price <= run.gold;
          const full =
            offer.kind === 'potion' && run.potions.length >= run.potion_slots;
          return (
            <div
              key={index}
              className={`dg-offer is-${offer.kind} ${offer.sold ? 'is-sold' : ''}`}
            >
              {offer.kind === 'card' && <CardFace card={offer.card} small />}
              {offer.kind === 'relic' && (
                <div className='dg-offer-item'>
                  <RelicChip id={offer.id} />
                  <small>{text.relics[offer.id]?.desc}</small>
                </div>
              )}
              {offer.kind === 'potion' && (
                <div className='dg-offer-item'>
                  <span className='dg-loot-item'>
                    <FlaskConical size={16} />
                    {text.potions[offer.id]?.name}
                  </span>
                  <small>{text.potions[offer.id]?.desc}</small>
                </div>
              )}
              <Button
                size='small'
                icon={<Coins size={14} />}
                disabled={busy || offer.sold || !afford || full}
                onClick={() => send('buy', { choice: index })}
              >
                {offer.sold ? t('已售出') : offer.price}
              </Button>
            </div>
          );
        })}
      </div>
      <div className='dg-screen-actions'>
        <Button
          icon={<Scissors size={15} />}
          disabled={busy || shop.removed || shop.remove_price > run.gold}
          onClick={() => setRemoving(true)}
        >
          {shop.removed
            ? t('本店已移除过一张牌')
            : t('移除一张牌（{{price}} 灵珠）', { price: shop.remove_price })}
        </Button>
        <Button theme='solid' disabled={busy} onClick={() => send('leave')}>
          {t('离开坊市')}
        </Button>
      </div>
      <DeckPicker
        visible={removing}
        title={t('选择要移除的牌')}
        deck={run.deck}
        onCancel={() => setRemoving(false)}
        onPick={(card) => {
          setRemoving(false);
          send('remove', { card });
        }}
      />
    </div>
  );
};

export const RestScreen = ({ run, busy, send }) => {
  const { t } = useTranslation();
  const [smithing, setSmithing] = useState(false);
  const canUpgrade = run.deck.some((card) => card.upgrade);
  return (
    <div className='dg-screen dg-rest'>
      <h3>{t('打坐')}</h3>
      <p className='dg-screen-note'>{t('寻一处清净地打坐，二选一')}</p>
      <div className='dg-rest-options'>
        <button
          type='button'
          className='dg-rest-option'
          disabled={busy}
          onClick={() => send('rest')}
        >
          <HeartPulse size={30} />
          <strong>{t('调息')}</strong>
          <span>{t('回复 {{n}} 点生命', { n: run.rest_heal || 0 })}</span>
        </button>
        <button
          type='button'
          className='dg-rest-option'
          disabled={busy || !canUpgrade}
          onClick={() => setSmithing(true)}
        >
          <Hammer size={30} />
          <strong>{t('参悟')}</strong>
          <span>{t('参悟牌组里的一张功法')}</span>
        </button>
      </div>
      <DeckPicker
        visible={smithing}
        title={t('选择要参悟的功法')}
        deck={run.deck}
        upgrade
        filter={(card) => !!card.upgrade}
        onCancel={() => setSmithing(false)}
        onPick={(card) => {
          setSmithing(false);
          send('smith', { card });
        }}
      />
    </div>
  );
};

export const EventScreen = ({ run, busy, send }) => {
  const { t } = useTranslation();
  const event = getEventText(t)[run.event];
  const [picking, setPicking] = useState(-1);
  if (!event) return null;
  return (
    <div className='dg-screen dg-event'>
      <h3>{event.title}</h3>
      <p className='dg-event-text'>{event.text}</p>
      <div className='dg-event-choices'>
        {run.event_choices.map((choice, index) => (
          <Button
            key={index}
            size='large'
            disabled={busy || !choice.enabled}
            onClick={() =>
              choice.card ? setPicking(index) : send('event', { choice: index })
            }
          >
            {event.choices[index](choice.n)}
          </Button>
        ))}
      </div>
      <DeckPicker
        visible={picking >= 0}
        title={event.title}
        deck={run.deck}
        filter={
          run.event === 'mirror' ? (card) => card.type !== 'curse' : undefined
        }
        onCancel={() => setPicking(-1)}
        onPick={(card) => {
          const choice = picking;
          setPicking(-1);
          send('event', { choice, card });
        }}
      />
    </div>
  );
};

// 一局结束。无尽远征：走了多远、魂晶与游戏币奖励；试炼：过没过关、星级与魂晶。
export const EndScreen = ({ round, symbol, levelNames, onBack }) => {
  const { t } = useTranslation();
  const run = round.run;
  const classes = getClassText(t);
  const detail = round.result?.detail || {};
  const trial = run.mode === 'trial';
  const won = trial ? run.phase === 'won' : run.bosses > 0;
  const message =
    !trial &&
    round.result &&
    skillResultMessage(round.result, t, symbol, { byLevel: true });
  let title = t('倒在了秘境里');
  if (trial) title = run.phase === 'won' ? t('试炼过关！') : t('试炼失败');
  else if (detail.abandoned) title = t('本轮已放弃');
  return (
    <div className={`dg-screen dg-end ${won ? 'is-won' : 'is-lost'}`}>
      {won ? <Trophy size={44} /> : <Skull size={44} />}
      <h3>{title}</h3>
      {trial ? (
        <span className='dg-stars is-large'>
          {[1, 2, 3].map((n) => (
            <Star
              key={n}
              size={30}
              className={n <= run.stars ? 'is-lit' : ''}
            />
          ))}
        </span>
      ) : (
        <p className='dg-screen-note'>
          {t(
            '{{class}} · 劫数 {{abyss}} · 第 {{act}} 重 · 到达第 {{floor}} 层',
            {
              class: classes[run.class]?.name,
              abyss: run.abyss,
              act: run.act,
              floor: run.floor,
            },
          )}
        </p>
      )}
      <div className='dg-end-stats'>
        <span>
          {t('击败敌人')}
          <strong>{run.kills}</strong>
        </span>
        {trial ? (
          <>
            <span>
              {t('用了回合')}
              <strong>{run.turns + (run.combat?.turn || 0)}</strong>
            </span>
            <span>
              {t('剩余生命')}
              <strong>
                {run.hp} / {run.max_hp}
              </strong>
            </span>
          </>
        ) : (
          <>
            <span>
              {t('强敌')}
              <strong>{run.elites}</strong>
            </span>
            <span>
              {t('打通')}
              <strong>{run.score > 0 ? levelNames[run.score - 1] : '-'}</strong>
            </span>
          </>
        )}
      </div>
      <div className='dg-crystals-gain'>
        <Gem size={18} />
        {t('获得道韵 {{n}}', { n: detail.crystals ?? 0 })}
        {detail.bonus > 0 && (
          <small>{t('含首次打通前三重奖励 {{n}}', { n: detail.bonus })}</small>
        )}
        {trial && !detail.crystals && run.phase === 'won' && (
          <small>{t('这些星以前已经拿过')}</small>
        )}
      </div>
      {message && (
        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>
      )}
      <Button theme='solid' size='large' onClick={onBack}>
        {t('回到营地')}
      </Button>
    </div>
  );
};
