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
import { Button, Input, Modal, Radio, RadioGroup } from '@douyinfe/semi-ui';
import { Search } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CardFace, cardName, useDungeonText } from '../../dungeon/parts';
import { CostChips } from '../../sect/parts';

// 旧功法保留为修行收藏：参悟后的总数给斗法加成，不再编排牌组或在战斗中出牌。
const Collection = ({ info, hero, res, busy, act }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const [picked, setPicked] = useState(-1);
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState('all');
  const card = hero.cards[picked];
  const cost = card ? info.upgrade_cost[card.rarity] : 0;
  const salvageStone = card ? (hero.salvage?.[picked] ?? -1) : -1;
  const counts = useMemo(() => {
    const out = {};
    for (const c of hero.cards) {
      const key = `${c.id}${c.up ? '+' : ''}`;
      out[key] = (out[key] || 0) + 1;
    }
    return out;
  }, [hero.cards]);
  const understood = hero.cards.filter((c) => c.up).length;
  const search = query.trim().toLowerCase();
  const shown = hero.cards
    .map((c, index) => ({ card: c, index }))
    .filter(
      ({ card: c }) =>
        (!search || cardName(c, text).toLowerCase().includes(search)) &&
        (scope === 'all' ||
          (scope === 'up' && c.up) ||
          (scope === 'can' && c.upgrade)),
    );
  const discard = (salvage) => {
    Modal.confirm({
      title: salvage ? t('拆解这张功法？') : t('丢掉这张功法？'),
      content: [
        salvage
          ? t(
              '拆解后这张功法就没了，换回 {{n}} 灵石；今天还能拆解 {{left}} 张。',
              {
                n: salvageStone,
                left: hero.salvage_left,
              },
            )
          : t('丢掉的功法找不回来。'),
        card.up && t('这张功法已经参悟，失去它会减少斗法伤害加成。'),
      ]
        .filter(Boolean)
        .join(' '),
      onOk: async () => {
        const data = await act(salvage ? 'salvage' : 'forget', {
          card: picked,
        });
        if (data) setPicked(-1);
      },
    });
  };

  return (
    <div className='jn-collection'>
      <p className='jn-muted'>
        {t(
          '功法收藏 {{cards}} / {{limit}}，已参悟 {{up}} 张。参悟后的总数提高斗法伤害，不需要编排牌组。',
          {
            cards: hero.cards.length,
            limit: info.max_cards,
            up: understood,
          },
        )}
      </p>
      <div className='jn-filters'>
        <Input
          size='small'
          prefix={<Search size={14} />}
          value={query}
          placeholder={t('搜索功法')}
          showClear
          onChange={setQuery}
          className='jn-filter-search'
        />
        <RadioGroup
          type='button'
          buttonSize='small'
          value={scope}
          onChange={(event) => setScope(event.target.value)}
          aria-label={t('参悟')}
        >
          <Radio value='all'>{t('全部')}</Radio>
          <Radio value='up'>{t('已参悟')}</Radio>
          <Radio value='can'>{t('可参悟')}</Radio>
        </RadioGroup>
      </div>
      {card && (
        <div className='jn-card-actions'>
          <strong>{cardName(card, text)}</strong>
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
          {salvageStone >= 0 && (
            <Button
              size='small'
              disabled={busy || hero.salvage_left < 1}
              onClick={() => discard(true)}
            >
              {t('拆解 +{{n}} 灵石', { n: salvageStone })}
            </Button>
          )}
          <Button
            size='small'
            type='danger'
            disabled={busy}
            onClick={() => discard(false)}
          >
            {t('丢掉')}
          </Button>
          <span className='jn-muted'>
            {t('今天还能拆解 {{n}} / {{max}} 张', {
              n: hero.salvage_left,
              max: info.salvage_daily,
            })}
          </span>
        </div>
      )}
      <div className='dg-deck-grid'>
        {shown.map(({ card: c, index }) => {
          const copies = counts[`${c.id}${c.up ? '+' : ''}`];
          return (
            <div key={index} className='jn-card-cell'>
              <CardFace
                card={c}
                small
                selected={picked === index}
                onClick={() => setPicked(picked === index ? -1 : index)}
              />
              {copies > 1 && <span className='jn-card-copies'>×{copies}</span>}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default Collection;
