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
import { Button, Modal } from '@douyinfe/semi-ui';
import { ArrowRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CardFace, cardName, useDungeonText } from '../../dungeon/parts';
import { CostChips } from '../../sect/parts';

// 功法：收藏里的所有功法，牌组里的高亮。选中一张后可以放进或拿出牌组、花灵石参悟，或者丢掉不在牌组里的。
const Collection = ({ info, hero, res, busy, act }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const [picked, setPicked] = useState(-1);
  const card = hero.cards[picked];
  const inDeck = hero.deck.includes(picked);
  const deckFull = hero.deck.length >= info.deck_max;
  const deckMin = hero.deck.length <= info.deck_min;
  const cost = card ? info.upgrade_cost[card.rarity] : 0;

  const toggle = () => {
    const deck = inDeck
      ? hero.deck.filter((index) => index !== picked)
      : [...hero.deck, picked];
    act('deck', { deck });
  };

  const forget = () =>
    Modal.confirm({
      title: t('丢掉这张功法？'),
      content: t('丢掉的功法找不回来，收藏满了以后赢下战斗也不能再收下新的。'),
      onOk: async () => {
        const data = await act('forget', { card: picked });
        if (data) setPicked(-1);
      },
    });

  // 牌组里的排在前面，各自按收下的先后。
  const order = [
    ...hero.deck,
    ...hero.cards
      .map((_, index) => index)
      .filter((i) => !hero.deck.includes(i)),
  ];

  return (
    <div className='jn-collection'>
      <p className='jn-muted'>
        {t(
          '牌组 {{deck}} 张（{{min}}–{{max}}），收藏 {{cards}} / {{limit}}。点一张功法放进或拿出牌组、参悟。',
          {
            deck: hero.deck.length,
            min: info.deck_min,
            max: info.deck_max,
            cards: hero.cards.length,
            limit: info.max_cards,
          },
        )}
      </p>
      {card && (
        <div className='jn-card-actions'>
          <strong>{cardName(card, text)}</strong>
          <Button
            size='small'
            disabled={busy || (inDeck ? deckMin : deckFull)}
            onClick={toggle}
          >
            {inDeck ? t('拿出牌组') : t('放进牌组')}
          </Button>
          {card.upgrade && (
            <>
              <Button
                size='small'
                theme='solid'
                disabled={busy || (res?.stone || 0) < cost}
                onClick={() => act('upgrade', { card: picked })}
              >
                {t('参悟')}
              </Button>
              <CostChips cost={{ stone: cost }} have={res} />
            </>
          )}
          {!inDeck && (
            <Button size='small' type='danger' disabled={busy} onClick={forget}>
              {t('丢掉')}
            </Button>
          )}
        </div>
      )}
      {card?.upgrade && (
        <div className='dg-upgrade-preview jn-upgrade-preview'>
          <CardFace card={card} />
          <ArrowRight size={22} />
          <CardFace card={card.upgrade} />
        </div>
      )}
      <div className='dg-deck-grid'>
        {order.map((index) => (
          <CardFace
            key={index}
            card={hero.cards[index]}
            small
            dim={!hero.deck.includes(index)}
            selected={picked === index}
            onClick={() => setPicked(picked === index ? -1 : index)}
          />
        ))}
      </div>
    </div>
  );
};

export default Collection;
