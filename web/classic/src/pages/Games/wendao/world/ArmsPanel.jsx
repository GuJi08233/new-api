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
import { Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  getSkillNames,
  getSkillText,
  getWeaponNames,
  getWeaponText,
} from '../doufa/text';

// ArmsPanel 配置出战：从手上有的法宝里挑一到两件(先挑的先拿在手上)，从手上有的法术里挑一个。法宝与法术在宗门藏经阁用贡献换，
// 流派起手的不用换。配置在历练与世界契约里带，九幽秘境里不带。
export default function ArmsPanel({ arms, busy, onEquip }) {
  const { t } = useTranslation();
  const [weapons, setWeapons] = useState(arms.loadout.weapons);
  const [skill, setSkill] = useState(arms.loadout.skill);
  const weaponNames = getWeaponNames(t);
  const weaponText = getWeaponText(t);
  const skillNames = getSkillNames(t);
  const skillText = getSkillText(t);

  useEffect(() => {
    setWeapons(arms.loadout.weapons);
    setSkill(arms.loadout.skill);
  }, [arms]);

  // 点一件法宝：在里面就拿掉(至少留一件)，不在就加上；已经两件了，换掉后一件。
  const toggle = (id) =>
    setWeapons((current) => {
      if (current.includes(id)) {
        return current.length > 1 ? current.filter((w) => w !== id) : current;
      }
      return current.length < 2 ? [...current, id] : [current[0], id];
    });

  const changed =
    skill !== arms.loadout.skill ||
    weapons.join() !== arms.loadout.weapons.join();
  return (
    <div className='df-card'>
      <div className='df-title'>{t('斗法配置')}</div>
      <p className='df-lead'>
        {t(
          '带哪两件法宝、用哪个法术出战。法宝与法术在宗门藏经阁用贡献换，永久属于你；历练与世界契约沿用这套配置，九幽秘境里不带。',
        )}
      </p>
      <div className='dl-heading'>{t('法宝')}</div>
      <p className='df-muted'>{t('选一到两件，先选的先拿在手上。')}</p>
      <div className='dl-picks'>
        {arms.weapons.map((id) => {
          const slot = weapons.indexOf(id);
          return (
            <button
              key={id}
              type='button'
              aria-pressed={slot >= 0}
              className={`dl-pick ${slot >= 0 ? 'is-on' : ''}`}
              onClick={() => toggle(id)}
            >
              <strong>
                {weaponNames[id]}
                {slot >= 0 && <small>{slot + 1}</small>}
              </strong>
              <span>{weaponText[id]}</span>
            </button>
          );
        })}
      </div>
      <div className='dl-heading'>{t('法术')}</div>
      <div className='dl-picks'>
        {arms.skills.map((id) => (
          <button
            key={id}
            type='button'
            aria-pressed={skill === id}
            className={`dl-pick ${skill === id ? 'is-on' : ''}`}
            onClick={() => setSkill(id)}
          >
            <strong>{skillNames[id]}</strong>
            <span>{skillText[id]}</span>
          </button>
        ))}
      </div>
      <div className='df-actions'>
        <Button
          theme='solid'
          disabled={busy || !changed}
          onClick={() => onEquip(weapons, skill)}
        >
          {t('保存配置')}
        </Button>
      </div>
    </div>
  );
}
