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
import { FlaskConical, Gem, KeyRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDungeonText } from '../../dungeon/parts';
import { CostChips, canAfford } from '../../sect/parts';
import { getKeyText } from './text';

// 旧法宝的祭炼与丹符转为历练斗法加成；出战武器另在世界配置。
const Gear = ({ info, hero, res, busy, act }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const potionEffects = {
    heal: t('本场生命上限 +20'),
    fire: t('本场伤害 +10%'),
    strength: t('本场伤害 +10%'),
    iron: t('开局护心 +1，抵挡一次伤害'),
    weakness: t('本场敌人伤害降低 10%'),
    swift: t('本场移速 +8%，身法冷却缩短 5%'),
    energy: t('本场灵力回复 +25%'),
  };
  const keys = getKeyText(t);
  const full = hero.worn.length >= hero.relic_slots;
  const toggle = (id) => {
    const worn = hero.worn.includes(id)
      ? hero.worn.filter((relic) => relic !== id)
      : [...hero.worn, id];
    act('wear', { relics: worn });
  };
  const potions = Object.entries(hero.potions).filter(([, n]) => n > 0);
  const owned = Object.entries(hero.keys).filter(([, n]) => n > 0);
  return (
    <div className='jn-gear'>
      <h5>
        {t('法宝（佩戴 {{worn}} / {{slots}}）', {
          worn: hero.worn.length,
          slots: hero.relic_slots,
        })}
      </h5>
      <p className='jn-muted'>{t('境界每高一层大境界，多戴一件法宝')}</p>
      <p className='jn-muted'>
        {t(
          '佩戴后的祭炼加成计入历练斗法；出战武器与法术在世界的斗法配置里选择。',
        )}
      </p>
      <div className='jn-relics'>
        {hero.relics.map((id) => {
          const worn = hero.worn.includes(id);
          return (
            <div key={id} className={`jn-relic ${worn ? 'is-worn' : ''}`}>
              <Gem size={16} />
              <div>
                <strong>{text.relics[id]?.name}</strong>
                <span>{t('祭炼 {{n}} 级', { n: hero.refine[id] || 0 })}</span>
              </div>
              <Button
                size='small'
                theme={worn ? 'light' : 'solid'}
                disabled={busy || (!worn && full)}
                onClick={() => toggle(id)}
              >
                {worn ? t('摘下') : t('佩戴')}
              </Button>
            </div>
          );
        })}
      </div>
      <h5>{t('丹符（开战时带 {{n}} 份）', { n: hero.potion_slots })}</h5>
      <p className='jn-muted'>
        {t('开战时按栏位自动消耗丹符并获得本场加成；落败或退走也不退回。')}
      </p>
      {potions.length === 0 ? (
        <p className='jn-muted'>
          {t('行囊里没有丹符，第一次通过一些节点会得到')}
        </p>
      ) : (
        <div className='jn-bag'>
          {potions.map(([id, n]) => (
            <span key={id} className='jn-bag-item' title={potionEffects[id]}>
              <FlaskConical size={14} />
              {text.potions[id]?.name} × {n} · {potionEffects[id]}
            </span>
          ))}
        </div>
      )}
      <h5>{t('丹房炼丹符（丹房 {{n}} 级）', { n: hero.alchemy })}</h5>
      <p className='jn-muted'>
        {t(
          '用库房的资源给主角炼战斗用的丹符，每种最多存 {{n}} 份；宗门丹房与洞府丹室按高的那一级算',
          { n: info.max_potion_stack },
        )}
      </p>
      <div className='jn-recipes'>
        {info.potion_recipes.map((recipe) => {
          const locked = hero.alchemy < recipe.alchemy;
          const full =
            (hero.potions[recipe.potion] || 0) >= info.max_potion_stack;
          return (
            <div
              key={recipe.potion}
              className={locked ? 'jn-recipe is-locked' : 'jn-recipe'}
            >
              <FlaskConical size={15} />
              <div>
                <strong>{text.potions[recipe.potion]?.name}</strong>
                <span>{potionEffects[recipe.potion]}</span>
              </div>
              {locked ? (
                <span className='jn-muted'>
                  {t('丹房 {{n}} 级', { n: recipe.alchemy })}
                </span>
              ) : (
                <>
                  <CostChips cost={recipe.cost} have={res} />
                  <Button
                    size='small'
                    disabled={busy || full || !canAfford(recipe.cost, res)}
                    onClick={() =>
                      act('craft', { item: recipe.potion, count: 1 })
                    }
                  >
                    {t('炼一份')}
                  </Button>
                </>
              )}
            </div>
          );
        })}
      </div>
      <h5>{t('机缘')}</h5>
      {owned.length === 0 ? (
        <p className='jn-muted'>
          {t('机缘由每章的首领掉落，突破大境界时用掉')}
        </p>
      ) : (
        <div className='jn-bag'>
          {owned.map(([id, n]) => (
            <span
              key={id}
              className='jn-bag-item is-key'
              title={keys[id]?.desc}
            >
              <KeyRound size={14} />
              {keys[id]?.name} × {n}
            </span>
          ))}
        </div>
      )}
    </div>
  );
};

export default Gear;
