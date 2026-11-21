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
import { formatAmount } from './format';

// 三套按 ISO 周轮换的仙盟周首领机制展示文案，键与 pkg/wendao.AllianceBoss* 一致。
export function bossMechanicMeta(kind, t) {
  switch (kind) {
    case 'blood_fury':
      return {
        name: t('狂血魔尊'),
        trait: t('血怒破绽'),
        desc: t('妖王气血低于 50% 时露出破绽，讨伐伤害 +25%'),
      };
    case 'spirit_tide':
      return {
        name: t('聚灵古蛟'),
        trait: t('灵潮共鸣'),
        desc: t(
          '本周每多 1 名其他道友先出手 +10%（最高 +30%），多五行同阵另 +10%',
        ),
      };
    case 'iron_shell':
    default:
      return {
        name: t('玄甲妖王'),
        trait: t('三才破甲'),
        desc: t('满编 3 名弟子同阵出战可破甲，讨伐伤害 +20%'),
      };
  }
}

// 预览当前所选阵容可触发的周机制增伤百分数（仅作界面提示，实际伤害由服务端按锁内状态裁定）。
export function previewBossBonus(boss, selectedDisciples = []) {
  if (!boss) return 0;
  const count = selectedDisciples.length;
  switch (boss.kind) {
    case 'iron_shell':
      return count >= 3 ? 20 : 0;
    case 'blood_fury':
      return boss.max_hp > 0 && boss.hp * 2 < boss.max_hp ? 25 : 0;
    case 'spirit_tide': {
      const peers = Math.min(Math.max(boss.other_attackers || 0, 0), 3);
      const elements = new Set(
        selectedDisciples
          .map((d) => d?.element)
          .filter((e) => e !== undefined && e !== null),
      );
      return peers * 10 + (elements.size >= 2 ? 10 : 0);
    }
    default:
      return 0;
  }
}

const rewardSummary = (item, resources, pillName, t) => {
  const parts = [];
  if (item.stone > 0)
    parts.push(`${resources.stone} +${formatAmount(item.stone)}`);
  if (item.rep > 0) parts.push(`${resources.rep} +${formatAmount(item.rep)}`);
  if (item.pills > 0) parts.push(`${pillName} ×${item.pills}`);
  return parts.join(' · ') || t('无');
};

// 仙盟周首领机制、全盟协作目标、个人周目标与最近一次讨伐反馈面板。
export default function AllianceBossPanel({
  boss,
  selectedDisciples = [],
  lastAttack = null,
  resources,
  breakPillName,
  t,
}) {
  if (!boss) return null;
  const meta = bossMechanicMeta(boss.kind, t);
  const bonusPct = previewBossBonus(boss, selectedDisciples);
  const stages = boss.stages || [];
  const personal = boss.personal || [];
  const pillLabel = breakPillName || t('破境丹');

  return (
    <div className='sc-boss-details'>
      <div className='sc-boss-mechanic'>
        <div className='sc-boss-mechanic-head'>
          <strong>{meta.name}</strong>
          <span className='sc-chip'>{meta.trait}</span>
          {boss.attackers > 0 && (
            <span className='sc-muted'>
              {t('本周已有 {{n}} 人出战', { n: boss.attackers })}
            </span>
          )}
        </div>
        <p className='sc-boss-mechanic-desc'>{meta.desc}</p>
        {boss.hp > 0 && (
          <p className='sc-boss-preview'>
            {bonusPct > 0
              ? t('当前阵容可触发机制增伤 +{{pct}}%', { pct: bonusPct })
              : t('当前阵容未触发周机制加成（按常值结算）')}
          </p>
        )}
      </div>

      {lastAttack && (
        <div className='sc-boss-feedback' role='status'>
          <strong>
            {lastAttack.killed
              ? t('本次讨伐造成 {{n}} 伤害并击败妖王！', {
                  n: formatAmount(lastAttack.damage),
                })
              : t('本次讨伐造成 {{n}} 伤害', {
                  n: formatAmount(lastAttack.damage),
                })}
          </strong>
          {lastAttack.bonus_pct > 0 && (
            <span>
              {t('机制增伤 +{{pct}}%', { pct: lastAttack.bonus_pct })}
            </span>
          )}
          <span>
            {t('已记入待领：{{rewards}}', {
              rewards: rewardSummary(
                {
                  stone: lastAttack.reward_stone,
                  rep: lastAttack.reward_rep,
                  pills: lastAttack.reward_pills,
                },
                resources,
                pillLabel,
                t,
              ),
            })}
          </span>
        </div>
      )}

      {stages.length > 0 && (
        <div className='sc-boss-goal-block'>
          <div className='sc-boss-goal-head'>
            <strong>{t('全盟协作目标')}</strong>
            <span className='sc-muted'>
              {t('本周出过力的成员均分阶段赏，每档每周每人限领一次')}
            </span>
          </div>
          <div className='sc-boss-goals'>
            {stages.map((s) => {
              const cls = s.earned
                ? 'is-earned'
                : s.reached
                  ? 'is-reached'
                  : '';
              const stateText = s.earned
                ? t('已记入待领')
                : s.reached
                  ? t('已达成，出手即领')
                  : t('未达成');
              return (
                <div key={s.stage} className={`sc-boss-goal ${cls}`}>
                  <div className='sc-boss-goal-top'>
                    <strong>
                      {s.damage_pct >= 100
                        ? t('击杀妖王')
                        : t('削血 {{pct}}%', { pct: s.damage_pct })}
                    </strong>
                    <span className='sc-boss-goal-state'>{stateText}</span>
                  </div>
                  <span className='sc-muted'>
                    {rewardSummary(s, resources, pillLabel, t)}
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {personal.length > 0 && (
        <div className='sc-boss-goal-block'>
          <div className='sc-boss-goal-head'>
            <strong>
              {t('个人周目标（本周已讨伐 {{n}} 次）', {
                n: boss.my_attacks || 0,
              })}
            </strong>
            <span className='sc-muted'>
              {t('不依赖全盟击杀，小盟坚持讨伐也可领满三档')}
            </span>
          </div>
          <div className='sc-boss-goals'>
            {personal.map((p) => {
              const cls = p.earned ? 'is-earned' : p.done ? 'is-reached' : '';
              const stateText = p.earned ? t('已记入待领') : t('未达成');
              return (
                <div key={p.id} className={`sc-boss-goal ${cls}`}>
                  <div className='sc-boss-goal-top'>
                    <strong>{t('本周讨伐 {{n}} 次', { n: p.attacks })}</strong>
                    <span className='sc-boss-goal-state'>{stateText}</span>
                  </div>
                  <span className='sc-muted'>
                    {rewardSummary(p, resources, pillLabel, t)}
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
