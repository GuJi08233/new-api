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
import { getKeyText } from './text';

// 法宝与行囊：戴上或摘下法宝(数量有上限)，看行囊里的丹符与机缘。丹符开战时自动带上，没用掉的打完放回来。
const Gear = ({ hero, busy, act }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
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
      <div className='jn-relics'>
        {hero.relics.map((id) => {
          const worn = hero.worn.includes(id);
          return (
            <div key={id} className={`jn-relic ${worn ? 'is-worn' : ''}`}>
              <Gem size={16} />
              <div>
                <strong>{text.relics[id]?.name}</strong>
                <span>{text.relics[id]?.desc}</span>
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
      {potions.length === 0 ? (
        <p className='jn-muted'>
          {t('行囊里没有丹符，第一次通过一些节点会得到')}
        </p>
      ) : (
        <div className='jn-bag'>
          {potions.map(([id, n]) => (
            <span
              key={id}
              className='jn-bag-item'
              title={text.potions[id]?.desc}
            >
              <FlaskConical size={14} />
              {text.potions[id]?.name} × {n}
            </span>
          ))}
        </div>
      )}
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
