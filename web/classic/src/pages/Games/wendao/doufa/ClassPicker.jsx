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
import { useTranslation } from 'react-i18next';
import { CLASSES, getClassText } from '../../dungeon/text';
import { content } from './engine';
import {
  getSkillNames,
  getSkillText,
  getWeaponNames,
  getWeaponText,
} from './text';

// ClassPicker 是斗法里选流派的三张卡：生命灵力、法术与起手的两件法宝。试剑台与秘境斗法共用。
export default function ClassPicker({ value, onChange }) {
  const { t } = useTranslation();
  const classText = getClassText(t);
  const weaponNames = getWeaponNames(t);
  const weaponText = getWeaponText(t);
  const skillNames = getSkillNames(t);
  const skillText = getSkillText(t);
  return (
    <div className='df-classes' role='radiogroup' aria-label={t('流派')}>
      {CLASSES.map((key) => {
        const spec = content.classes[key];
        return (
          <button
            key={key}
            type='button'
            role='radio'
            aria-checked={value === key}
            className={`df-class ${value === key ? 'is-active' : ''}`}
            onClick={() => onChange(key)}
          >
            <span className='df-class-name'>{classText[key].name}</span>
            <span className='df-class-line'>
              {t('生命 {{hp}} · 灵力 {{mp}}', { hp: spec.hp, mp: spec.mp })}
            </span>
            <span className='df-class-line'>
              <b>{skillNames[spec.skill.kind]}</b> {skillText[spec.skill.kind]}
            </span>
            {spec.weapons.map((w) => (
              <span key={w} className='df-class-line'>
                <b>{weaponNames[w]}</b> {weaponText[w]}
              </span>
            ))}
          </button>
        );
      })}
    </div>
  );
}
