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
import { Button, Input, Modal } from '@douyinfe/semi-ui';
import { Heart, PawPrint, Pencil, Sparkles, Swords } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  Bar,
  CostChips,
  ElementTag,
  TalentTag,
  canAfford,
  formatLeft,
  useSectText,
} from '../../sect/parts';
import BeastArt from '../art/BeastArt';
import {
  beastMove,
  getBaitText,
  getBeastStageNames,
  getBeastText,
  getFeedText,
} from './text';

// 一只灵兽(或一颗蛋)：画像、名字、资质、等级与经验、亲密、招式，以及喂养、嬉戏、出战、进化、起名与放生。
const BeastCard = ({ info, beast, hero, res, pills, rare, now, busy, act }) => {
  const { t } = useTranslation();
  const sectText = useSectText();
  const kinds = getBeastText(t);
  const stages = getBeastStageNames(t);
  const feeds = getFeedText(t);
  const rules = info.beast;
  const [naming, setNaming] = useState(null);
  const out = hero.beast_out === beast.id;

  const release = () =>
    Modal.confirm({
      title: t('放生这只灵兽？'),
      content: t('放生以后它回到山林，不会再回来；宗门得到一点声望。'),
      onOk: () => act('release', { beast: beast.id }),
    });

  if (beast.hatch) {
    return (
      <div className='cv-beast is-egg'>
        <BeastArt egg />
        <div className='cv-beast-body'>
          <strong>{t('灵兽蛋')}</strong>
          <p className='cv-room-desc'>
            {now >= beast.hatch
              ? t('蛋壳裂开了，马上就能见到它')
              : t('{{time}}后孵化，孵出来才知道是什么灵兽', {
                  time: formatLeft(beast.hatch - now, t),
                })}
          </p>
          <div className='cv-room-foot'>
            <Button
              size='small'
              type='tertiary'
              disabled={busy}
              onClick={release}
            >
              {t('放生')}
            </Button>
          </div>
        </div>
      </div>
    );
  }

  const name = beast.name || kinds[beast.kind]?.name;
  const capped = beast.level >= beast.cap;
  const stageCap = rules.stage_levels[beast.stage];
  const evo =
    beast.stage < rules.evolutions.length && beast.level >= stageCap
      ? rules.evolutions[beast.stage]
      : null;
  const fedOut = beast.fed_today >= rules.feeds_per_day;
  return (
    <div className={`cv-beast ${out ? 'is-out' : ''}`}>
      <BeastArt kind={beast.kind} stage={beast.stage} active={out} />
      <div className='cv-beast-body'>
        <div className='cv-relic-head'>
          <strong>{name}</strong>
          <button
            type='button'
            className='cv-icon-btn'
            aria-label={t('起名')}
            onClick={() => setNaming(beast.name || '')}
          >
            <Pencil size={13} />
          </button>
          {kinds[beast.kind]?.element >= 0 ? (
            <ElementTag element={kinds[beast.kind].element} />
          ) : (
            <span className='cv-kind is-edge'>{t('稀有')}</span>
          )}
          <TalentTag talent={beast.aptitude} />
          <span className='cv-stage'>{stages[beast.stage]}</span>
          {out && <span className='cv-worn'>{t('出战中')}</span>}
        </div>
        {naming !== null && (
          <div className='cv-naming'>
            <Input
              size='small'
              value={naming}
              maxLength={rules.max_name}
              placeholder={kinds[beast.kind]?.name}
              onChange={setNaming}
            />
            <Button
              size='small'
              theme='solid'
              disabled={busy}
              onClick={async () => {
                const data = await act('rename', {
                  beast: beast.id,
                  name: naming,
                });
                if (data) setNaming(null);
              }}
            >
              {t('确定')}
            </Button>
            <Button
              size='small'
              type='tertiary'
              onClick={() => setNaming(null)}
            >
              {t('取消')}
            </Button>
          </div>
        )}
        <p className='cv-room-effect'>
          {beastMove(t, beast.kind, beast.power)}
        </p>
        <div className='cv-beast-stat'>
          <span>
            {t('{{n}} 级', { n: beast.level })}
            {capped && ` · ${t('已到上限')}`}
          </span>
          <Bar
            value={capped ? 1 : beast.exp}
            max={capped ? 1 : rules.exp_need[beast.level]}
          />
        </div>
        <div className='cv-beast-stat'>
          <span>
            <Heart size={12} /> {t('亲密 {{n}}', { n: beast.bond })}
          </span>
          <Bar value={beast.bond} max={rules.max_bond} kind='bond' />
        </div>
        {evo && (
          <div className='cv-room-foot cv-evolve'>
            <span className='cv-cost-line'>
              <CostChips cost={evo.cost} have={res} />
              <span
                className={`sc-chip is-rare${(rare?.[evo.rare] || 0) < evo.rare_n ? ' is-short' : ''}`}
              >
                <Sparkles size={13} />
                {sectText.rares[evo.rare]} × {evo.rare_n}
              </span>
            </span>
            <Button
              size='small'
              theme='solid'
              disabled={
                busy ||
                (rare?.[evo.rare] || 0) < evo.rare_n ||
                !canAfford(evo.cost, res)
              }
              onClick={() => act('evolve', { beast: beast.id })}
            >
              {t('进化为{{stage}}', { stage: stages[beast.stage + 1] })}
            </Button>
          </div>
        )}
        <div className='cv-beast-actions'>
          <Button
            size='small'
            theme={out ? 'light' : 'solid'}
            icon={<Swords size={13} />}
            disabled={busy}
            onClick={() => act('summon', { beast: out ? 0 : beast.id })}
          >
            {out ? t('休息') : t('出战')}
          </Button>
          <Button
            size='small'
            icon={<PawPrint size={13} />}
            disabled={
              busy || beast.played_today || beast.bond >= rules.max_bond
            }
            onClick={() => act('play', { beast: beast.id })}
          >
            {beast.played_today ? t('今天玩过了') : t('嬉戏')}
          </Button>
          {rules.feeds.map((feed) => {
            const short = feed.pill
              ? (pills?.[feed.pill] || 0) < 1
              : !canAfford(feed.cost, res);
            return (
              <Button
                key={feed.key}
                size='small'
                disabled={busy || capped || fedOut || short}
                onClick={() => act('feed', { beast: beast.id, item: feed.key })}
              >
                {feeds[feed.key]}
                {feed.pill ? ` (${pills?.[feed.pill] || 0})` : ''}
              </Button>
            );
          })}
          <Button
            size='small'
            type='tertiary'
            disabled={busy}
            onClick={release}
          >
            {t('放生')}
          </Button>
        </div>
        <p className='cv-room-desc'>
          {t(
            '今天喂了 {{n}} / {{max}} 次；喂一次经验：灵草 {{herb}}、聚气丹 {{qi}}、培元丹 {{py}}',
            {
              n: beast.fed_today,
              max: rules.feeds_per_day,
              herb: rules.feeds[0].exp,
              qi: rules.feeds[1].exp,
              py: rules.feeds[2].exp,
            },
          )}
        </p>
      </div>
    </div>
  );
};

// 兽苑：引兽(三种饵料)与养着的灵兽。
const Beasts = ({ info, hero, res, pills, rare, now, busy, act }) => {
  const { t } = useTranslation();
  const sectText = useSectText();
  const baits = getBaitText(t);
  const rules = info.beast;
  const den = hero.cave.den || 0;
  const full = hero.beasts.length >= hero.beast_slots;
  const cooling = hero.lure_ready > now;
  return (
    <div className='cv-furnace'>
      <div className='cv-summary'>
        <span>
          {t('兽苑 {{n}} 级 · 灵兽 {{have}} / {{slots}}', {
            n: den,
            have: hero.beasts.length,
            slots: hero.beast_slots,
          })}
        </span>
        <span>
          {t('等级上限：兽苑等级 × {{n}}，到 {{a}}、{{b}} 级要进化', {
            n: rules.den_level_cap,
            a: rules.stage_levels[0],
            b: rules.stage_levels[1],
          })}
        </span>
      </div>
      <p className='jn-muted'>
        {t(
          '出战的灵兽在历练与问心阶里每个回合结束时自己出手，打赢了也长经验与亲密；亲密越高出手越重。灵兽带不进九幽秘境',
        )}
      </p>
      {den === 0 ? (
        <p className='cv-hint'>{t('先在洞府开辟兽苑，才能引来灵兽')}</p>
      ) : (
        <>
          <h5 className='cv-title'>{t('引兽')}</h5>
          <p className='jn-muted'>
            {cooling
              ? t('刚引过一次，{{time}}后才能再引', {
                  time: formatLeft(hero.lure_ready - now, t),
                })
              : full
                ? t('兽苑满了，扩建兽苑或放生一只才能再引')
                : t('每 {{h}} 小时能引一次，引来的蛋 {{egg}} 小时孵化', {
                    h: rules.lure_cooldown / 3600,
                    egg: rules.hatch / 3600,
                  })}
          </p>
          <div className='cv-relics'>
            {rules.baits.map((bait) => {
              const shortRare = bait.rare && (rare?.[bait.rare] || 0) < 1;
              return (
                <div key={bait.key} className='cv-relic'>
                  <div className='cv-relic-head'>
                    <PawPrint size={16} />
                    <strong>{baits[bait.key].name}</strong>
                    {bait.fox_pct > 0 && (
                      <span className='cv-worn'>
                        {t('九尾灵狐 {{n}}%', { n: bait.fox_pct })}
                      </span>
                    )}
                  </div>
                  <p className='cv-room-desc'>{baits[bait.key].desc}</p>
                  <div className='cv-room-foot'>
                    <span className='cv-cost-line'>
                      <CostChips cost={bait.cost} have={res} />
                      {bait.rare && (
                        <span
                          className={`sc-chip is-rare${shortRare ? ' is-short' : ''}`}
                        >
                          <Sparkles size={13} />
                          {sectText.rares[bait.rare]} × 1
                        </span>
                      )}
                    </span>
                    <Button
                      size='small'
                      theme='solid'
                      disabled={
                        busy ||
                        full ||
                        cooling ||
                        shortRare ||
                        !canAfford(bait.cost, res)
                      }
                      onClick={() => act('lure', { item: bait.key })}
                    >
                      {t('引兽')}
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        </>
      )}
      {hero.beasts.length > 0 && (
        <>
          <h5 className='cv-title'>{t('灵兽')}</h5>
          <div className='cv-beasts'>
            {hero.beasts.map((beast) => (
              <BeastCard
                key={beast.id}
                info={info}
                beast={beast}
                hero={hero}
                res={res}
                pills={pills}
                rare={rare}
                now={now}
                busy={busy}
                act={act}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
};

export default Beasts;
