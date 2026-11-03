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
import { formatAmount } from '../../sect/parts';
import { getStatPartNames } from './text';

// 属性来源：最大生命、开战力量与护体、闭关速度各由哪些来源加起来。数与来源都来自服务端(hero.sheet、hero.rate)，这里只
// 负责写出来：祭炼标「佩戴」(换下法宝就没有)，丹药的药力标还管几场，没标的都是永久的；超过战斗上限的部分不生效，单独
// 写一句。
const StatSheet = ({ sheet, rate, hp, mods }) => {
  const { t } = useTranslation();
  const names = getStatPartNames(t);
  const rows = [
    { key: 'hp', label: t('修行生命'), parts: sheet.hp, cap: sheet.hp_cap },
    {
      key: 'strength',
      label: t('修行力量'),
      parts: sheet.strength,
      cap: sheet.strength_cap,
    },
    {
      key: 'block',
      label: t('修行护体'),
      parts: sheet.block,
      cap: sheet.block_cap,
    },
  ];
  const pace = Object.fromEntries(sheet.rate.map((part) => [part.key, part.n]));
  return (
    <dl className='jn-sheet'>
      <div className='jn-sheet-row'>
        <dt>{t('斗法基础')}</dt>
        <dd className='jn-sheet-parts' style={{ gridColumn: '1 / -1' }}>
          <span>
            {t('生命 {{hp}} · 伤害 +{{damage}}% · 护心 {{ward}} 次', {
              hp,
              damage: mods.damage || 0,
              ward: mods.ward || 0,
            })}
          </span>
          <span className='jn-muted'>
            {t(
              '下面是修行来源原值，折算为斗法加成；本场丹符、随行弟子与灵兽另计。',
            )}
          </span>
        </dd>
      </div>
      {rows.map((row) => {
        const sum = row.parts.reduce((total, part) => total + part.n, 0);
        return (
          <div key={row.key} className='jn-sheet-row'>
            <dt>{row.label}</dt>
            <dd className='jn-sheet-total'>{Math.min(sum, row.cap)}</dd>
            <dd className='jn-sheet-parts'>
              {row.parts
                .filter((part) => part.n > 0)
                .map((part) => (
                  <span key={part.key} className='jn-sheet-part'>
                    {names[part.key]} {part.key === 'class' ? '' : '+'}
                    {part.n}
                    {part.key === 'refine' && <em>{t('戴着才有')}</em>}
                    {part.left > 0 && (
                      <em>{t('还管 {{n}} 场', { n: part.left })}</em>
                    )}
                  </span>
                ))}
              {sum === 0 && <span className='jn-sheet-part'>—</span>}
              {sum > row.cap && (
                <span className='jn-sheet-cap'>
                  {t('上限 {{cap}}，多出的 {{n}} 不生效', {
                    cap: row.cap,
                    n: sum - row.cap,
                  })}
                </span>
              )}
            </dd>
          </div>
        );
      })}
      <div className='jn-sheet-row'>
        <dt>{t('闭关')}</dt>
        <dd className='jn-sheet-total'>
          {t('{{n}}/时', { n: formatAmount(Math.round(rate)) })}
        </dd>
        <dd className='jn-sheet-parts'>
          <span className='jn-sheet-part'>
            {pace.sect > 0
              ? t(
                  '基数 {{base}} × 境界 {{mult}} 倍 × (1 + 聚灵阵 {{array}}% + 静室 {{quiet}}% + 门派 {{sect}}%)',
                  {
                    base: pace.base,
                    mult: pace.realm,
                    array: pace.array,
                    quiet: pace.quiet,
                    sect: pace.sect,
                  },
                )
              : t(
                  '基数 {{base}} × 境界 {{mult}} 倍 × (1 + 聚灵阵 {{array}}% + 静室 {{quiet}}%)',
                  {
                    base: pace.base,
                    mult: pace.realm,
                    array: pace.array,
                    quiet: pace.quiet,
                  },
                )}
          </span>
          {sheet.meditation_pct > 0 && (
            <span className='jn-sheet-part'>
              {t('基础闭关公式 × (1 + 灵脉 {{pct}}%)', {
                pct: sheet.meditation_pct,
              })}
            </span>
          )}
        </dd>
      </div>
    </dl>
  );
};

export default StatSheet;
