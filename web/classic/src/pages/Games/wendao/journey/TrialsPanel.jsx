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
import { Button } from '@douyinfe/semi-ui';
import {
  CalendarDays,
  Check,
  Crown,
  FlaskConical,
  Gift,
  Lock,
  Skull,
  Swords,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import EnemyArt from '../art/EnemyArt';
import { getEnemyNames } from '../../dungeon/text';
import { CostChips, formatLeft, pillName, useSectText } from '../../sect/parts';
import TeamPicker, { readyTeam } from './TeamPicker';
import { getChapterText, getNodeKindNames, getSignText } from './text';

const KIND_ICONS = { combat: Swords, elite: Skull, boss: Crown };

const TrialCard = ({ info, hero, trial, index, cleared, busy, onTrial }) => {
  const { t } = useTranslation();
  const sectText = useSectText();
  const kinds = getNodeKindNames(t);
  const enemyNames = getEnemyNames(t);
  const sign = info.weekly.signs.find((s) => s.key === trial.sign) || {};
  const signText = getSignText(t, sign)[trial.sign];
  const [team, setTeam] = useState([]);
  const Icon = KIND_ICONS[trial.kind];
  const number = index + 1;
  return (
    <section
      className={`jn-detail jn-trial is-${trial.kind}${cleared ? ' is-cleared' : ''}`}
    >
      <header>
        <Icon size={18} />
        <h4>{t('第 {{n}} 关', { n: number })}</h4>
        <span className='jn-kind'>{kinds[trial.kind]}</span>
        <span className={`jn-trial-sign is-${trial.sign}`}>
          {signText?.name}
        </span>
        {cleared && (
          <span className='jn-trial-done'>
            <Check size={14} />
            {t('已过')}
          </span>
        )}
      </header>
      <p className='jn-desc'>{signText?.desc}</p>
      <div className='jn-enemies'>
        {trial.enemies.map((id, slot) => (
          <span key={slot} className='jn-enemy'>
            <span className='jn-enemy-art'>
              <EnemyArt id={id} />
            </span>
            {enemyNames[id]}
          </span>
        ))}
        <span className='jn-muted'>
          {t('生命 ×{{hp}}% · 伤害 ×{{dmg}}%', {
            hp: trial.hp,
            dmg: trial.dmg,
          })}
        </span>
      </div>
      <div className='jn-rewards'>
        <span className='jn-reward-label'>{t('首次奖励')}</span>
        <CostChips cost={trial.reward} sign='+' />
        {trial.pill && (
          <span className='jn-reward is-key'>
            <FlaskConical size={13} />
            {pillName(sectText, trial.pill)}
          </span>
        )}
      </div>
      {!cleared && !sign.bare && (
        <TeamPicker
          companions={hero.companions}
          max={info.companion_max}
          team={team}
          onChange={setTeam}
        />
      )}
      {!cleared && (
        <div className='jn-detail-actions'>
          <Button
            theme='solid'
            type={trial.kind === 'boss' ? 'danger' : 'primary'}
            disabled={busy || !!hero.travel || !!hero.meditation}
            onClick={() =>
              onTrial(number, sign.bare ? [] : readyTeam(team, hero.companions))
            }
          >
            {t('挑战')}
          </Button>
        </div>
      )}
    </section>
  );
};

// 周天试炼：每周三关，敌人取自打败了首领的最后一章，每关一种天象。过了的关这一周不能再打；三关全过另有宝箱。
const TrialsPanel = ({ info, hero, now, busy, onTrial }) => {
  const { t } = useTranslation();
  const sectText = useSectText();
  const chapters = getChapterText(t);
  const weekly = hero.weekly;

  if (!weekly) {
    return (
      <section className='jn-detail jn-steps-locked'>
        <header>
          <Lock size={18} />
          <h4>{t('周天试炼')}</h4>
        </header>
        <p className='jn-desc'>
          {t(
            '每周三关：遭遇、强敌、首领，敌人取自你打完的最后一章，每关有一种天象改一改规则。',
          )}
        </p>
        <p className='jn-muted'>{t('打败第一章的首领以后开放。')}</p>
      </section>
    );
  }

  const done = weekly.cleared.filter(Boolean).length;
  const left = Math.max(0, weekly.ends - now);
  return (
    <div className='jn-map-wrap'>
      <section className='jn-detail jn-steps-head'>
        <div>
          <h4>{t('周天试炼')}</h4>
          <p className='jn-desc'>
            {t(
              '每周三关，敌人取自{{chapter}}，每关有一种天象。可以一直重试，每关第一次打赢有奖励，过了的关这一周不能再打。',
              { chapter: chapters[weekly.chapter]?.name },
            )}
          </p>
          <p className='jn-muted jn-trial-ends'>
            <CalendarDays size={13} aria-hidden />
            {left > 0
              ? t('{{time}}后换下一周的三关', { time: formatLeft(left, t) })
              : t('下一次操作时换到新的一周')}
          </p>
        </div>
        <span className='jn-steps-count'>
          <strong>{done}</strong> / {weekly.trials.length}
        </span>
      </section>
      {weekly.trials.map((trial, index) => (
        <TrialCard
          key={`${weekly.week}-${index}`}
          info={info}
          hero={hero}
          trial={trial}
          index={index}
          cleared={weekly.cleared[index]}
          busy={busy}
          onTrial={onTrial}
        />
      ))}
      <section className='jn-detail jn-trial-chest'>
        <header>
          <Gift size={18} />
          <h4>{t('三关全过')}</h4>
          {done === weekly.trials.length && (
            <span className='jn-trial-done'>
              <Check size={14} />
              {t('已领')}
            </span>
          )}
        </header>
        <p className='jn-desc'>
          {t('另得{{pill}}一颗、灵药一份。', {
            pill: pillName(sectText, info.weekly.chest),
          })}
        </p>
        {weekly.weeks > 0 && (
          <p className='jn-muted'>
            {t('已有 {{n}} 周三关全过', { n: weekly.weeks })}
          </p>
        )}
      </section>
    </div>
  );
};

export default TrialsPanel;
