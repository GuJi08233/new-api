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
import { ChevronDown, Heart, Shield, Sparkles, Swords } from 'lucide-react';
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
import StatSheet from './StatSheet';
import { getHeroTitle, getKeyText } from './text';

// 主角：流派、境界与修为条(两次同步之间按闭关速度往前推)、斗法基础属性(含药力)，点开看各自的来源；第九层
// 圆满时可以突破。通关以后名字旁多一方称号印，随问心阶登到的级数变。
const HeroCard = ({ hero, pills, elapsed, now, busy, onBreak, onQi }) => {
  const { t } = useTranslation();
  const sectText = useSectText();
  const classes = getClassText(t);
  const keys = getKeyText(t);
  const [usePill, setUsePill] = useState(false);
  const [sheetOpen, setSheetOpen] = useState(false);
  const sheet = hero.sheet;
  const mods = hero.fight_mods || {};
  const exp = hero.bottleneck
    ? hero.need
    : Math.min(hero.exp + (hero.rate * elapsed) / 3600, hero.need);
  const key = hero.break_key;
  const hasKey = key && (hero.keys[key] || 0) > 0;
  const pillCount = pills?.break || 0;
  const qiCount = pills?.qi || 0;
  const peiyuanCount = pills?.peiyuan || 0;
  const qiWait = hero.qi_ready > now ? hero.qi_ready - now : 0;
  const title = getHeroTitle(t, hero);
  const className = classes[hero.class]?.name;
  // 身份：开了宗的是宗主，拜入正道宗门的写宗门与身份，其余是散修。
  const identity =
    hero.identity === 'master'
      ? t('{{class}}宗主', { class: className })
      : hero.identity === 'disciple'
        ? t('{{class}} · {{sect}}{{rank}}', {
            class: className,
            sect: sectText.path.sects[hero.path?.sect]?.name,
            rank: sectText.path.ranks[hero.path?.rank || 0],
          })
        : t('{{class}}散修', { class: className });
  return (
    <section className='jn-hero'>
      <div className='jn-hero-head'>
        <span className='jn-hero-face'>
          <Portrait who={`hero_${hero.class}`} />
        </span>
        <div className='jn-hero-title'>
          <strong>{realmLabel(sectText, t, hero.realm, hero.layer)}</strong>
          <span>
            {identity}
            {title && <em className='jn-hero-seal'>{title}</em>}
          </span>
        </div>
        <button
          type='button'
          className='jn-hero-stats'
          aria-expanded={sheetOpen}
          aria-label={t('属性来源')}
          onClick={() => setSheetOpen(!sheetOpen)}
        >
          <span className='jn-hero-stat' title={t('最大生命')}>
            <Heart size={14} />
            {hero.fight_max_hp}
          </span>
          <span className='jn-hero-stat' title={t('斗法伤害加成')}>
            <Swords size={14} />+{mods.damage || 0}%
          </span>
          {mods.ward > 0 && (
            <span className='jn-hero-stat' title={t('开局护心')}>
              <Shield size={14} />
              {mods.ward}
            </span>
          )}
          <ChevronDown size={14} className='jn-hero-stats-more' />
        </button>
      </div>
      {sheetOpen && (
        <StatSheet
          sheet={sheet}
          rate={hero.rate}
          hp={hero.fight_max_hp}
          mods={mods}
        />
      )}
      <div className='jn-exp'>
        <Bar value={exp} max={hero.need} kind='exp' />
        <span className='jn-exp-row'>
          <span>
            {formatAmount(exp)} / {formatAmount(hero.need)}
            {hero.travel
              ? ` · ${t('游历中，闭关暂停')}`
              : !hero.bottleneck &&
                ` · ${t('闭关 +{{n}}/时', { n: Math.round(hero.rate) })}`}
            {!hero.bottleneck &&
              qiWait > 0 &&
              ` · ${t('丹力还没化开，{{left}}后能服下一颗', {
                left: formatLeft(qiWait, t),
              })}`}
          </span>
          {!hero.bottleneck && (
            <Tooltip
              content={
                qiWait > 0
                  ? t('丹力还没化开，{{left}}后能服下一颗', {
                      left: formatLeft(qiWait, t),
                    })
                  : t('服一颗库房里的聚气丹，马上得到三小时闭关的修为')
              }
            >
              <Button
                size='small'
                disabled={busy || qiCount < 1 || qiWait > 0}
                onClick={() => onQi('qi')}
              >
                {t('服聚气丹（剩 {{n}}）', { n: qiCount })}
              </Button>
            </Tooltip>
          )}
          {!hero.bottleneck && peiyuanCount > 0 && (
            <Tooltip
              content={t(
                '服一颗灵田朱果炼成的培元丹，马上得到五小时闭关的修为，与聚气丹共用冷却',
              )}
            >
              <Button
                size='small'
                disabled={busy || qiWait > 0}
                onClick={() => onQi('peiyuan')}
              >
                {t('服培元丹（剩 {{n}}）', { n: peiyuanCount })}
              </Button>
            </Tooltip>
          )}
        </span>
      </div>
      {hero.bottleneck && (
        <div className='jn-break'>
          <Sparkles size={16} />
          {!key ? (
            <span>{t('已修至化神圆满，沧澜界再没有更高的境界')}</span>
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
