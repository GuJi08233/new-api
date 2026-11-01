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
import {
  Check,
  Crown,
  FlaskConical,
  Gem,
  Lock,
  Skull,
  Sparkles,
  Swords,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import EnemyArt from '../art/EnemyArt';
import { getEnemyNames } from '../../dungeon/text';
import {
  CostChips,
  formatAmount,
  pillName,
  useSectText,
} from '../../sect/parts';
import TeamPicker, { readyTeam } from './TeamPicker';
import { getNodeKindNames } from './text';

const KIND_ICONS = { combat: Swords, elite: Skull, boss: Crown };
const ROW = 10;

// 问心阶：重铸封印以后开放的五十级石阶。石阶从下往上画，十级一排；点一级看心魔与奖励，下一级可以挑战。
const StepsPanel = ({ info, hero, busy, onClimb }) => {
  const { t } = useTranslation();
  const steps = info.steps || [];
  const top = steps.length;
  const next = hero.steps < top ? hero.steps + 1 : top;
  const [selected, setSelected] = useState(next);
  useEffect(() => setSelected(next), [next]);

  if (!hero.steps_open) {
    return (
      <section className='jn-detail jn-steps-locked'>
        <header>
          <Lock size={18} />
          <h4>{t('问心阶')}</h4>
        </header>
        <p className='jn-desc'>
          {t(
            '问心阶在问道宗山门之后，是开山祖师留下的试炼：五十级石阶，每一级守着一个心魔。',
          )}
        </p>
        <p className='jn-muted'>{t('重铸封印、走完第五章的尾声之后开放。')}</p>
      </section>
    );
  }

  const rows = [];
  for (let start = 0; start < top; start += ROW) {
    rows.unshift(steps.slice(start, start + ROW));
  }
  const current = steps[selected - 1];
  return (
    <div className='jn-map-wrap'>
      <section className='jn-detail jn-steps-head'>
        <div>
          <h4>{t('问心阶')}</h4>
          <p className='jn-desc'>
            {t(
              '每一级守着一个心魔，化作你一路打过的敌人。只能挑战下一级，输了可以再来；第一次登上每一级才有奖励。',
            )}
          </p>
        </div>
        <span className='jn-steps-count'>
          <strong>{hero.steps}</strong> / {top}
        </span>
      </section>
      {hero.steps >= top && (
        <p className='jn-steps-summit'>
          {t('五十级问心阶已全部登上。问道于心，问心无愧。')}
        </p>
      )}
      <div className='jn-steps' role='list' aria-label={t('问心阶')}>
        {rows.map((row) => (
          <div className='jn-steps-row' key={row[0].step} role='presentation'>
            {row.map((step) => {
              const state =
                step.step <= hero.steps
                  ? 'done'
                  : step.step === next
                    ? 'open'
                    : 'locked';
              const Icon = KIND_ICONS[step.kind];
              return (
                <button
                  type='button'
                  role='listitem'
                  key={step.step}
                  className={`jn-step is-${step.kind} is-${state} ${selected === step.step ? 'is-selected' : ''}`}
                  aria-label={t('第 {{n}} 级', { n: step.step })}
                  aria-current={state === 'open' ? 'step' : undefined}
                  onClick={() => setSelected(step.step)}
                >
                  {step.kind !== 'combat' && <Icon size={12} />}
                  <span>{step.step}</span>
                  {state === 'done' && (
                    <Check size={10} className='jn-step-check' />
                  )}
                </button>
              );
            })}
          </div>
        ))}
      </div>
      {current && (
        <StepDetail
          key={current.step}
          info={info}
          step={current}
          hero={hero}
          busy={busy}
          next={next}
          onClimb={onClimb}
        />
      )}
    </div>
  );
};

const StepDetail = ({ info, step, hero, busy, next, onClimb }) => {
  const { t } = useTranslation();
  const sectText = useSectText();
  const kinds = getNodeKindNames(t);
  const enemyNames = getEnemyNames(t);
  const [team, setTeam] = useState([]);
  const done = step.step <= hero.steps;
  const open = !done && step.step === next;
  const Icon = KIND_ICONS[step.kind];
  return (
    <section className={`jn-detail is-${step.kind}`}>
      <header>
        <Icon size={18} />
        <h4>{t('第 {{n}} 级', { n: step.step })}</h4>
        <span className='jn-kind'>{kinds[step.kind]}</span>
        {done && <span className='jn-muted'>{t('已登上')}</span>}
      </header>
      <div className='jn-enemies'>
        {step.enemies.map((id, index) => (
          <span key={index} className='jn-enemy is-demon'>
            <span className='jn-enemy-art'>
              <EnemyArt id={id} />
            </span>
            {t('{{name}}的心魔', { name: enemyNames[id] })}
          </span>
        ))}
        <span className='jn-muted'>
          {t('生命 ×{{hp}}% · 伤害 ×{{dmg}}%', { hp: step.hp, dmg: step.dmg })}
        </span>
      </div>
      <div className='jn-rewards'>
        <span className='jn-reward-label'>{t('首次奖励')}</span>
        <span className='jn-reward'>
          <Sparkles size={13} />
          {t('修为 +{{n}}', { n: formatAmount(step.exp) })}
        </span>
        <CostChips cost={step.reward} sign='+' />
        {step.pill && (
          <span className='jn-reward is-key'>
            <FlaskConical size={13} />
            {pillName(sectText, step.pill)}
          </span>
        )}
        <span className='jn-reward'>
          <Gem size={13} />
          {t('功法三选一')}
        </span>
      </div>
      {open && (
        <TeamPicker
          companions={hero.companions}
          max={info.companion_max}
          team={team}
          onChange={setTeam}
        />
      )}
      <div className='jn-detail-actions'>
        {!done && !open && (
          <span className='jn-lock'>
            {t('先登上第 {{n}} 级', { n: step.step - 1 })}
          </span>
        )}
        {open && (
          <Button
            theme='solid'
            type={step.kind === 'boss' ? 'danger' : 'primary'}
            disabled={busy || hero.deck.length < info.deck_min}
            onClick={() => onClimb(readyTeam(team, hero.companions))}
          >
            {t('登阶')}
          </Button>
        )}
      </div>
    </section>
  );
};

export default StepsPanel;
