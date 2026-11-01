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
import { Flame, Pill, Sparkles } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  Bar,
  CostChips,
  canAfford,
  formatLeft,
  useSectText,
} from '../../sect/parts';
import FurnaceArt from '../art/FurnaceArt';
import { getElixirText, getFireText, getGradeNames, gradeOdds } from './text';

// 品质的几率写成"上品 30% · 中品 45% · 下品 25%"。
const Odds = ({ score, rules }) => {
  const { t } = useTranslation();
  const names = getGradeNames(t);
  const odds = gradeOdds(score, rules);
  return (
    <span className='cv-odds'>
      {[2, 1, 0].map((grade) => (
        <span key={grade} className={`cv-grade is-g${grade}`}>
          {names[grade]} {odds[grade]}%
        </span>
      ))}
    </span>
  );
};

// 一座正在炼的丹炉：进度、调火时机、出炉。
const BrewCard = ({ brew, index, now, rules, text, busy, act }) => {
  const { t } = useTranslation();
  const fires = getFireText(t);
  const recipe = rules.recipes.find((r) => r.id === brew.recipe);
  const done = now >= brew.end;
  const progress = (now - brew.start) / Math.max(brew.end - brew.start, 1);
  const canStoke =
    !brew.stoked && now >= brew.stoke_from && now <= brew.stoke_to;
  let stokeLine;
  if (brew.stoked)
    stokeLine = t('已调火，品质 +{{n}}', { n: rules.stoke_bonus });
  else if (now < brew.stoke_from)
    stokeLine = t('{{time}}后可以调火', {
      time: formatLeft(brew.stoke_from - now, t),
    });
  else if (now <= brew.stoke_to)
    stokeLine = t('正是调火的时候，还剩 {{time}}', {
      time: formatLeft(brew.stoke_to - now, t),
    });
  else stokeLine = t('错过了调火的时机');
  return (
    <div className={`cv-brew ${done ? 'is-done' : ''}`}>
      <FurnaceArt fire={brew.fire} done={done} progress={progress} />
      <div className='cv-brew-body'>
        <div className='cv-relic-head'>
          <strong>{text[brew.recipe]?.name}</strong>
          <span className={`cv-fire is-${brew.fire}`}>
            {fires[brew.fire]?.name}
          </span>
        </div>
        {done ? (
          <p className='cv-room-effect'>{t('丹已炼成，开炉看品质')}</p>
        ) : (
          <>
            <Bar value={now - brew.start} max={brew.end - brew.start} />
            <p className='cv-room-desc'>
              {t('还剩 {{time}}', { time: formatLeft(brew.end - now, t) })}
            </p>
          </>
        )}
        {recipe?.graded && <Odds score={brew.score} rules={rules} />}
        {!done && <p className='cv-room-desc'>{stokeLine}</p>}
        <div className='cv-room-foot'>
          {done ? (
            <Button
              size='small'
              theme='solid'
              disabled={busy}
              onClick={() => act('collect', { index })}
            >
              {t('开炉取丹')}
            </Button>
          ) : (
            <Button
              size='small'
              disabled={busy || !canStoke}
              onClick={() => act('stoke', { index })}
            >
              {t('调火')}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
};

// 丹室：正在炼的几炉、丹方(选火候开炉)与炼好的丹(服用)。
const Furnace = ({ info, hero, res, rare, now, busy, act }) => {
  const { t } = useTranslation();
  const sectText = useSectText();
  const rules = info.elixir;
  const text = getElixirText(t, hero, rules.golden_max);
  const grades = getGradeNames(t);
  const fires = getFireText(t);
  const [fire, setFire] = useState('steady');
  const level = hero.cave.furnace || 0;
  const free = hero.furnace_slots - hero.furnaces.length;
  const fireSpec = rules.fires.find((f) => f.key === fire);
  const owned = rules.recipes.filter((r) =>
    (hero.elixirs[r.id] || []).some((n) => n > 0),
  );
  const buffs = Object.entries(hero.buffs);

  return (
    <div className='cv-furnace'>
      <div className='cv-summary'>
        <span>
          {t('丹室 {{n}} 级 · 丹炉 {{used}} / {{slots}}', {
            n: level,
            used: hero.furnaces.length,
            slots: hero.furnace_slots,
          })}
        </span>
        <span>
          {t('淬体 {{n}} / {{cap}} · 九转金丹 {{g}} / {{max}}', {
            n: hero.tempered,
            cap: hero.temper_cap,
            g: hero.golden,
            max: rules.golden_max,
          })}
        </span>
      </div>
      {buffs.length > 0 && (
        <div className='cv-buffs'>
          {buffs.map(([id, buff]) => (
            <span key={id} className='cv-buff'>
              <Sparkles size={13} />
              {id === 'jingang'
                ? t('{{name}}：开战护体 +{{n}}，还有 {{left}} 场', {
                    name: text[id].name,
                    n: buff.power,
                    left: buff.left,
                  })
                : t('{{name}}：开战力量 +{{n}}，还有 {{left}} 场', {
                    name: text[id].name,
                    n: buff.power,
                    left: buff.left,
                  })}
            </span>
          ))}
        </div>
      )}
      {level === 0 ? (
        <p className='cv-hint'>{t('先在洞府开辟丹室，才能开炉炼丹')}</p>
      ) : (
        <div className='cv-brews'>
          {hero.furnaces.map((brew, index) => (
            <BrewCard
              key={`${brew.recipe}-${brew.start}`}
              brew={brew}
              index={index}
              now={now}
              rules={rules}
              text={text}
              busy={busy}
              act={act}
            />
          ))}
          {Array.from({ length: Math.max(free, 0) }, (_, i) => (
            <div key={`free-${i}`} className='cv-brew is-free'>
              <FurnaceArt idle />
              <p className='cv-room-desc'>
                {t('空着的丹炉，在下面选一张丹方开炉')}
              </p>
            </div>
          ))}
        </div>
      )}

      <h5 className='cv-title'>{t('丹方')}</h5>
      <div className='cv-fires' role='radiogroup' aria-label={t('火候')}>
        <span className='cv-fires-label'>{t('火候')}</span>
        {rules.fires.map((f) => (
          <button
            type='button'
            role='radio'
            aria-checked={fire === f.key}
            key={f.key}
            className={`cv-view cv-fire-pick is-${f.key} ${fire === f.key ? 'is-active' : ''}`}
            onClick={() => setFire(f.key)}
          >
            <Flame size={13} />
            {fires[f.key].name}
          </button>
        ))}
        <span className='cv-room-desc'>
          {fires[fire].desc}
          {fireSpec.explode > 0 &&
            t('（炸炉 {{n}}%）', { n: fireSpec.explode })}
        </span>
      </div>
      <p className='jn-muted'>
        {t(
          '品质分 = 丹室每级 {{f}} + 火候 + 调火 {{s}} + 熟练每炉 {{m}}（最多 {{max}} 炉），出炉时再加 0–99 的随机数：{{high}} 起上品，{{mid}} 起中品',
          {
            f: rules.furnace_score,
            s: rules.stoke_bonus,
            m: rules.mastery_score,
            max: rules.mastery_max,
            high: rules.grade_high,
            mid: rules.grade_mid,
          },
        )}
      </p>
      <div className='cv-relics'>
        {rules.recipes.map((recipe) => {
          const locked = level < recipe.furnace;
          const mastery = hero.mastery[recipe.id] || 0;
          const score =
            rules.furnace_score * level +
            fireSpec.score +
            rules.mastery_score * Math.min(mastery, rules.mastery_max);
          const shortRare =
            recipe.rare && (rare?.[recipe.rare] || 0) < recipe.rare_n;
          const seconds = (recipe.hours * 3600 * fireSpec.time_pct) / 100;
          return (
            <div
              key={recipe.id}
              className={`cv-relic ${locked ? 'is-locked' : ''}`}
            >
              <div className='cv-relic-head'>
                <Pill size={16} />
                <strong>{text[recipe.id].name}</strong>
                <span className='cv-worn'>
                  {t('一炉 {{n}} 颗 · 熟练 {{m}}', {
                    n: recipe.yield,
                    m: Math.min(mastery, rules.mastery_max),
                  })}
                </span>
              </div>
              <p className='cv-room-desc'>
                {text[recipe.id].desc(recipe.effect, recipe.battles)}
              </p>
              {locked ? (
                <span className='jn-muted'>
                  {t('丹室 {{n}} 级', { n: recipe.furnace })}
                </span>
              ) : (
                <>
                  {recipe.graded && <Odds score={score} rules={rules} />}
                  <div className='cv-room-foot'>
                    <span className='cv-cost-line'>
                      <CostChips cost={recipe.cost} have={res} />
                      {recipe.rare && (
                        <span
                          className={`sc-chip is-rare${shortRare ? ' is-short' : ''}`}
                        >
                          <Sparkles size={13} />
                          {sectText.rares[recipe.rare]} × {recipe.rare_n}
                        </span>
                      )}
                      <span className='cv-chance'>
                        {formatLeft(seconds, t)}
                      </span>
                    </span>
                    <Button
                      size='small'
                      theme='solid'
                      disabled={
                        busy ||
                        free <= 0 ||
                        shortRare ||
                        !canAfford(recipe.cost, res)
                      }
                      onClick={() => act('brew', { item: recipe.id, fire })}
                    >
                      {t('开炉')}
                    </Button>
                  </div>
                </>
              )}
            </div>
          );
        })}
      </div>

      <h5 className='cv-title'>{t('丹囊')}</h5>
      {owned.length === 0 ? (
        <p className='jn-muted'>{t('还没有炼好的丹')}</p>
      ) : (
        <div className='cv-bag'>
          {owned.map((recipe) =>
            [2, 1, 0]
              .filter((grade) => hero.elixirs[recipe.id][grade] > 0)
              .map((grade) => (
                <div key={`${recipe.id}-${grade}`} className='cv-bag-item'>
                  <span className={`cv-grade is-g${grade}`}>
                    {grades[grade]}
                  </span>
                  <strong>{text[recipe.id].name}</strong>
                  <span className='cv-room-desc'>
                    × {hero.elixirs[recipe.id][grade]}
                  </span>
                  <Button
                    size='small'
                    disabled={busy}
                    onClick={() => act('elixir', { item: recipe.id, grade })}
                  >
                    {t('服下')}
                  </Button>
                </div>
              )),
          )}
        </div>
      )}
    </div>
  );
};

export default Furnace;
