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
import { Button, Tooltip } from '@douyinfe/semi-ui';
import { Gem, Sparkles } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDungeonText } from '../../dungeon/parts';
import { CostChips, canAfford, useSectText } from '../../sect/parts';
import { getRefineKindText, refineEffect } from './text';

// 器室：祭炼主角的法宝。戴着的法宝排在前面；每件显示路数、现在的加成、下一级的花费、灵药与成功率(含祝福)。祭炼的
// 结果在这件法宝上闪一下：成了是金光，没成是一缕青烟。
const Forge = ({ info, hero, res, rare, busy, act }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const sectText = useSectText();
  const rules = info.refine;
  const kinds = getRefineKindText(t, rules);
  const forge = hero.cave.forge || 0;
  const [flash, setFlash] = useState(null);
  const bonus = hero.refine_bonus;
  const relics = [
    ...hero.worn,
    ...hero.relics.filter((id) => !hero.worn.includes(id)),
  ];

  const refine = async (relic) => {
    const data = await act('refine', { item: relic });
    if (!data) return;
    const ok = data.events.some((ev) => ev.k === 'hero_refine');
    setFlash({ relic, ok, key: Date.now() });
  };

  return (
    <div className='cv-forge'>
      <div className='cv-summary'>
        <span>{t('器室 {{n}} 级：法宝最多祭炼到 +{{n}}', { n: forge })}</span>
        <span>
          {t(
            '戴着的法宝祭炼加成：生命 +{{hp}} · 护体 +{{block}} · 力量 +{{str}}',
            {
              hp: bonus.hp,
              block: bonus.block,
              str: bonus.strength,
            },
          )}
        </span>
      </div>
      <p className='jn-muted'>
        {t(
          '只有戴着的法宝才算；失败只损失材料、不掉级，每失败一次下一次成功率 +{{n}}%，成了以后清零。锋路法宝给的力量合计最多 {{max}} 点',
          { n: rules.bless, max: rules.strength_max },
        )}
      </p>
      {forge === 0 && (
        <p className='cv-hint'>{t('先在洞府开辟器室，才能祭炼法宝')}</p>
      )}
      <div className='cv-relics'>
        {relics.map((id) => {
          const kind = rules.kinds[id] || 'vital';
          const level = hero.refine[id] || 0;
          const next = level + 1;
          const step = rules.steps[level];
          const bless = hero.bless[id] || 0;
          const worn = hero.worn.includes(id);
          const maxed = level >= rules.steps.length;
          const locked = !maxed && next > forge;
          const shortRare = step?.rare && (rare?.[step.rare] || 0) < 1;
          const flashing = flash?.relic === id;
          return (
            <div
              key={flashing ? `${id}-${flash.key}` : id}
              className={[
                'cv-relic',
                worn ? 'is-worn' : '',
                flashing ? (flash.ok ? 'is-flash-ok' : 'is-flash-fail') : '',
              ].join(' ')}
            >
              <div className='cv-relic-head'>
                <Gem size={16} />
                <Tooltip content={text.relics[id]?.desc}>
                  <strong>{text.relics[id]?.name}</strong>
                </Tooltip>
                {level > 0 && <span className='cv-plus'>+{level}</span>}
                <Tooltip content={kinds[kind].desc}>
                  <span className={`cv-kind is-${kind}`}>
                    {kinds[kind].name}
                  </span>
                </Tooltip>
                {worn && <span className='cv-worn'>{t('佩戴中')}</span>}
              </div>
              <p className='cv-room-effect'>
                {refineEffect(t, rules, kind, level)}
                {!maxed && kind !== 'edge' && (
                  <span className='cv-next'>
                    {' → '}
                    {refineEffect(t, rules, kind, next)}
                  </span>
                )}
              </p>
              {maxed ? (
                <span className='jn-muted'>{t('已祭炼圆满')}</span>
              ) : locked ? (
                <span className='jn-muted'>
                  {t('器室 {{n}} 级才能祭炼到 +{{n}}', { n: next })}
                </span>
              ) : (
                <div className='cv-room-foot'>
                  <span className='cv-cost-line'>
                    <CostChips cost={step.cost} have={res} />
                    {step.rare && (
                      <span
                        className={`sc-chip is-rare${shortRare ? ' is-short' : ''}`}
                      >
                        <Sparkles size={13} />
                        {sectText.rares[step.rare]} × 1
                      </span>
                    )}
                    <span className='cv-chance'>
                      {t('成功率 {{n}}%', {
                        n: Math.min(step.chance + bless, 100),
                      })}
                      {bless > 0 && (
                        <em>{t('（祝福 +{{n}}%）', { n: bless })}</em>
                      )}
                    </span>
                  </span>
                  <Button
                    size='small'
                    theme='solid'
                    disabled={busy || shortRare || !canAfford(step.cost, res)}
                    onClick={() => refine(id)}
                  >
                    {t('祭炼')}
                  </Button>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default Forge;
