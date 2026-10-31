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
import { Button, Checkbox, Tooltip } from '@douyinfe/semi-ui';
import { Heart, Sparkles, Swords } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getClassText } from '../../dungeon/text';
import {
  Bar,
  formatAmount,
  formatLeft,
  realmLabel,
  useSectText,
} from '../../sect/parts';
import Portrait from '../art/Portrait';
import { getKeyText } from './text';

// 主角：流派、境界与修为条(两次同步之间按闭关速度往前推)、生命与力量；第九层圆满时可以突破。
const HeroCard = ({ hero, pills, elapsed, now, busy, onBreak, onQi }) => {
  const { t } = useTranslation();
  const sectText = useSectText();
  const classes = getClassText(t);
  const keys = getKeyText(t);
  const [usePill, setUsePill] = useState(false);
  const exp = hero.bottleneck
    ? hero.need
    : Math.min(hero.exp + (hero.rate * elapsed) / 3600, hero.need);
  const key = hero.break_key;
  const hasKey = key && (hero.keys[key] || 0) > 0;
  const pillCount = pills?.break || 0;
  const qiCount = pills?.qi || 0;
  const qiWait = hero.qi_ready > now ? hero.qi_ready - now : 0;
  return (
    <section className='jn-hero'>
      <div className='jn-hero-head'>
        <span className='jn-hero-face'>
          <Portrait who={`hero_${hero.class}`} />
        </span>
        <div className='jn-hero-title'>
          <strong>{realmLabel(sectText, t, hero.realm, hero.layer)}</strong>
          <span>
            {t('{{class}}宗主', { class: classes[hero.class]?.name })}
          </span>
        </div>
        <span className='jn-hero-stat' title={t('最大生命')}>
          <Heart size={14} />
          {hero.max_hp}
        </span>
        <Tooltip content={t('境界带来的开战力量')}>
          <span className='jn-hero-stat'>
            <Swords size={14} />+{hero.strength}
          </span>
        </Tooltip>
      </div>
      <div className='jn-exp'>
        <Bar value={exp} max={hero.need} kind='exp' />
        <span className='jn-exp-row'>
          <span>
            {formatAmount(exp)} / {formatAmount(hero.need)}
            {!hero.bottleneck &&
              ` · ${t('闭关 +{{n}}/时', { n: Math.round(hero.rate) })}`}
          </span>
          {!hero.bottleneck && (
            <Tooltip
              content={
                qiWait > 0
                  ? t('丹力还没化开，{{left}}后能服下一颗', {
                      left: formatLeft(qiWait, t),
                    })
                  : t('服一颗宗门丹房的聚气丹，马上得到三小时闭关的修为')
              }
            >
              <Button
                size='small'
                disabled={busy || qiCount < 1 || qiWait > 0}
                onClick={onQi}
              >
                {t('服聚气丹（剩 {{n}}）', { n: qiCount })}
              </Button>
            </Tooltip>
          )}
        </span>
      </div>
      {hero.bottleneck && (
        <div className='jn-break'>
          <Sparkles size={16} />
          {!key ? (
            <span>{t('下一个境界的机缘还没有现世，敬请期待')}</span>
          ) : !hasKey ? (
            <span>
              {t('第九层圆满。突破需要{{key}}：{{desc}}', {
                key: keys[key]?.name,
                desc: keys[key]?.desc,
              })}
            </span>
          ) : (
            <>
              <span>
                {t('成功率 {{pct}}%', {
                  pct: hero.break_chance[usePill ? 1 : 0],
                })}
              </span>
              <Checkbox
                checked={usePill}
                disabled={pillCount < 1}
                onChange={(event) => setUsePill(event.target.checked)}
              >
                {t('服破境丹（剩 {{n}}）', { n: pillCount })}
              </Checkbox>
              <Button
                theme='solid'
                size='small'
                disabled={busy}
                onClick={() => onBreak(usePill && pillCount > 0)}
              >
                {t('突破{{realm}}', { realm: sectText.realms[hero.realm + 1] })}
              </Button>
            </>
          )}
        </div>
      )}
    </section>
  );
};

export default HeroCard;
