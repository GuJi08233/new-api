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

import React, { useEffect, useMemo, useState } from 'react';
import {
  Button,
  InputNumber,
  Select,
  Switch,
  TabPane,
  Tabs,
} from '@douyinfe/semi-ui';
import { Gem, Lock, Star, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import GameShop from '../common/GameShop';
import { RuleList } from '../common/SideBox';
import { PlayTurnstile } from '../common/usePlayTurnstile';
import { CardFace, cardName, ITEM_ICONS, useDungeonText } from './parts';
import {
  CLASSES,
  getAbyssRules,
  getClassText,
  getEnemyNames,
  getTalentText,
} from './text';

const PRACTICE_KEY = 'dungeon-practice-setup';

// 练习设置存在本机，下次打开接着用。
function loadPractice(catalog) {
  try {
    const saved = JSON.parse(localStorage.getItem(PRACTICE_KEY) || 'null');
    if (saved?.class && Array.isArray(saved.deck)) return saved;
  } catch {
    // 本机存的设置坏了就用默认的。
  }
  return {
    class: 'warrior',
    deck: (catalog?.starter?.warrior || []).map((id) => ({ id })),
    relics: [],
    enemies: ['dummy'],
    stage: 1,
    max_hp: 0,
    infinite_energy: false,
    invincible: false,
  };
}

const StarRow = ({ stars }) => (
  <span className='dg-stars'>
    {[1, 2, 3].map((n) => (
      <Star key={n} size={15} className={n <= stars ? 'is-lit' : ''} />
    ))}
  </span>
);

const EndlessTab = ({
  info,
  round,
  busy,
  turnstile,
  onStart,
  onResume,
  onUnlock,
}) => {
  const { t } = useTranslation();
  const meta = info.meta;
  const catalog = info.catalog;
  const classes = getClassText(t);
  const talents = getTalentText(t);
  const abyssRules = getAbyssRules(t);
  const [klass, setKlass] = useState('warrior');
  const [abyss, setAbyss] = useState(0);
  const maxAbyss = meta.max_abyss?.[klass] || 0;
  useEffect(() => {
    setAbyss((current) => Math.min(current, maxAbyss));
  }, [maxAbyss]);
  const playing = round?.status === 'playing';
  const record = meta.records?.[klass] || {};

  return (
    <div className='dg-tab'>
      {playing && (
        <div className='dg-resume'>
          <span>
            {t(
              '你有一轮进行中的远征：{{class}} · 第 {{act}} 幕 · 第 {{floor}} 层',
              {
                class: classes[round.run.class]?.name,
                act: round.run.act,
                floor: round.run.floor,
              },
            )}
          </span>
          <Button theme='solid' onClick={() => onResume('endless')}>
            {t('继续远征')}
          </Button>
        </div>
      )}
      <h4>{t('选择职业')}</h4>
      <div className='dg-classes'>
        {CLASSES.map((id) => {
          const unlocked = meta.classes?.includes(id);
          const cost = catalog.class_costs?.[id] || 0;
          return (
            <div
              key={id}
              role='button'
              tabIndex={0}
              className={`dg-class is-${id} ${klass === id ? 'is-selected' : ''} ${unlocked ? '' : 'is-locked'}`}
              onClick={() => unlocked && setKlass(id)}
              onKeyDown={(event) =>
                event.key === 'Enter' && unlocked && setKlass(id)
              }
            >
              <strong>{classes[id].name}</strong>
              <span>{classes[id].desc}</span>
              {unlocked ? (
                <small>
                  {t('最多打通 {{acts}} 幕 · 深渊可选到 {{abyss}}', {
                    acts: meta.records?.[id]?.best_acts || 0,
                    abyss: meta.max_abyss?.[id] || 0,
                  })}
                </small>
              ) : (
                <Button
                  size='small'
                  icon={<Lock size={13} />}
                  disabled={busy || meta.crystals < cost}
                  onClick={(event) => {
                    event.stopPropagation();
                    onUnlock(`class:${id}`);
                  }}
                >
                  {t('{{n}} 魂晶解锁', { n: cost })}
                </Button>
              )}
            </div>
          );
        })}
      </div>
      <div className='dg-start-row'>
        <Select
          value={abyss}
          onChange={setAbyss}
          aria-label={t('深渊等级')}
          style={{ width: 160 }}
          optionList={Array.from({ length: maxAbyss + 1 }, (_, level) => ({
            value: level,
            label:
              level === 0
                ? t('深渊 0（标准）')
                : t('深渊 {{abyss}}', { abyss: level }),
          }))}
        />
        <Button
          theme='solid'
          size='large'
          loading={busy}
          disabled={playing || !turnstile.ready}
          onClick={() => onStart('endless', { class: klass, abyss })}
        >
          {t('开始远征')}
        </Button>
      </div>
      <PlayTurnstile turnstile={turnstile} />
      {abyss > 0 && (
        <RuleList
          items={abyssRules
            .slice(0, abyss)
            .map(
              (rule, index) =>
                `${t('深渊 {{abyss}}', { abyss: index + 1 })}：${rule}`,
            )}
        />
      )}
      <p className='dg-tab-note'>
        {t(
          '这个职业：远征 {{runs}} 次，打通前三幕 {{wins}} 次，最远到达第 {{floor}} 层',
          {
            runs: record.runs || 0,
            wins: record.wins || 0,
            floor: record.best_floor || 0,
          },
        )}
      </p>
      <h4>
        {t('天赋')}
        <span className='dg-crystals'>
          <Gem size={14} />
          {meta.crystals}
        </span>
      </h4>
      <div className='dg-talents'>
        {catalog.talents.map((talent) => {
          const level = meta.talents?.[talent.id] || 0;
          const maxed = level >= talent.costs.length;
          const cost = talent.costs[level];
          return (
            <div
              key={talent.id}
              className={`dg-talent ${level > 0 ? 'is-owned' : ''}`}
            >
              <strong>
                {talents[talent.id]?.name}
                <small>
                  {level}/{talent.costs.length}
                </small>
              </strong>
              <span>{talents[talent.id]?.desc}</span>
              <Button
                size='small'
                disabled={busy || maxed || meta.crystals < cost}
                onClick={() => onUnlock(`talent:${talent.id}`)}
              >
                {maxed ? t('已满级') : t('{{n}} 魂晶', { n: cost })}
              </Button>
            </div>
          );
        })}
      </div>
    </div>
  );
};

const TrialTab = ({ info, round, busy, onStart, onResume }) => {
  const { t } = useTranslation();
  const classes = getClassText(t);
  const enemies = getEnemyNames(t);
  const trials = info.catalog.content.trials;
  const done = info.meta.trials || {};
  const playing = round?.status === 'playing';
  return (
    <div className='dg-tab'>
      {playing && (
        <div className='dg-resume'>
          <span>{t('你有一关进行中的试炼')}</span>
          <Button theme='solid' onClick={() => onResume('trial')}>
            {t('继续试炼')}
          </Button>
        </div>
      )}
      <p className='dg-tab-note'>
        {t(
          '每关用固定的牌组挑战。过关得一星，回合数与剩余生命达标各再得一星；第一次拿到的每颗星奖励魂晶。',
        )}
      </p>
      <div className='dg-trials'>
        {trials.map((trial, index) => {
          const unlocked = index === 0 || (done[trials[index - 1].id] || 0) > 0;
          const stars = done[trial.id] || 0;
          return (
            <div
              key={trial.id}
              className={`dg-trial ${unlocked ? '' : 'is-locked'}`}
            >
              <span className='dg-trial-index'>{index + 1}</span>
              <div className='dg-trial-body'>
                <strong>
                  {classes[trial.class]?.name}
                  <StarRow stars={stars} />
                </strong>
                <span>
                  {trial.fights
                    .map((fight) => fight.map((id) => enemies[id]).join(' + '))
                    .join(' → ')}
                </span>
                <small>
                  {t('★ 过关　★ {{turns}} 回合内　★ 剩余生命 ≥ {{pct}}%', {
                    turns: trial.turns,
                    pct: trial.hp_pct,
                  })}
                  {' · '}
                  {t('每星 {{n}} 魂晶', { n: trial.crystals })}
                </small>
              </div>
              <Button
                disabled={busy || !unlocked}
                icon={unlocked ? undefined : <Lock size={14} />}
                onClick={() => onStart('trial', { trial: trial.id })}
              >
                {stars > 0 ? t('再次挑战') : t('挑战')}
              </Button>
            </div>
          );
        })}
      </div>
    </div>
  );
};

const PracticeTab = ({ info, round, busy, onStart, onResume }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const classes = getClassText(t);
  const enemyNames = getEnemyNames(t);
  const catalog = info.catalog;
  const content = catalog.content;
  const [setup, setSetup] = useState(() => loadPractice(content));
  const [upgraded, setUpgraded] = useState(false);
  const [pool, setPool] = useState('warrior');
  useEffect(() => {
    localStorage.setItem(PRACTICE_KEY, JSON.stringify(setup));
  }, [setup]);
  const update = (changes) =>
    setSetup((previous) => ({ ...previous, ...changes }));
  const poolCards = useMemo(
    () =>
      content.cards.filter((card) =>
        pool === 'any' ? card.class === 'any' : card.class === pool,
      ),
    [content.cards, pool],
  );
  const deckFull = setup.deck.length >= catalog.practice_max_deck;
  const deckViews = useMemo(() => {
    const byId = Object.fromEntries(
      content.cards.map((card) => [card.id, card]),
    );
    return setup.deck.map((card) => {
      const base = byId[card.id];
      if (!base) return null;
      return card.up && base.upgrade ? base.upgrade : base;
    });
  }, [content.cards, setup.deck]);
  const playing = round?.status === 'playing';

  return (
    <div className='dg-tab dg-practice'>
      {playing && (
        <div className='dg-resume'>
          <span>{t('你有一场进行中的练习')}</span>
          <Button onClick={() => onResume('practice')}>{t('回到练习')}</Button>
        </div>
      )}
      <p className='dg-tab-note'>
        {t(
          '随意组牌、选择敌人与强度，测试伤害和数值；练习没有任何奖励，也不影响存档。',
        )}
      </p>
      <div className='dg-practice-grid'>
        <label>
          {t('职业')}
          <Select
            value={setup.class}
            onChange={(value) => update({ class: value })}
            optionList={CLASSES.map((id) => ({
              value: id,
              label: classes[id].name,
            }))}
          />
        </label>
        <label>
          {t('敌人（最多 {{n}} 个）', { n: catalog.practice_max_enemies })}
          <Select
            multiple
            max={catalog.practice_max_enemies}
            value={setup.enemies}
            onChange={(value) => update({ enemies: value })}
            optionList={content.enemies.map((enemy) => ({
              value: enemy.id,
              label:
                enemy.act > 0
                  ? `${enemyNames[enemy.id]} · ${t('第 {{act}} 幕', { act: enemy.act })}`
                  : t('训练木桩（不会行动）'),
            }))}
          />
        </label>
        <label>
          {t('敌人强度（按第几幕放大）')}
          <InputNumber
            min={1}
            max={catalog.practice_max_stage}
            value={setup.stage}
            onChange={(value) => update({ stage: Number(value) || 1 })}
          />
        </label>
        <label>
          {t('最大生命（0 为职业默认）')}
          <InputNumber
            min={0}
            max={999}
            value={setup.max_hp}
            onChange={(value) => update({ max_hp: Number(value) || 0 })}
          />
        </label>
        <label>
          {t('遗物')}
          <Select
            multiple
            value={setup.relics}
            onChange={(value) => update({ relics: value })}
            optionList={content.relics.map((id) => ({
              value: id,
              label: text.relics[id]?.name || id,
            }))}
          />
        </label>
        <div className='dg-switches'>
          <label>
            <Switch
              checked={setup.infinite_energy}
              onChange={(value) => update({ infinite_energy: value })}
            />
            {t('无限能量')}
          </label>
          <label>
            <Switch
              checked={setup.invincible}
              onChange={(value) => update({ invincible: value })}
            />
            {t('不会倒下')}
          </label>
        </div>
      </div>
      <h4>
        {t('牌组 {{n}} / {{max}}', {
          n: setup.deck.length,
          max: catalog.practice_max_deck,
        })}
        <span className='dg-h4-actions'>
          <Button
            size='small'
            onClick={() =>
              update({
                deck: (content.starter[setup.class] || []).map((id) => ({
                  id,
                })),
              })
            }
          >
            {t('换成初始牌组')}
          </Button>
          <Button
            size='small'
            icon={<Trash2 size={13} />}
            disabled={!setup.deck.length}
            onClick={() => update({ deck: [] })}
          >
            {t('清空')}
          </Button>
        </span>
      </h4>
      <div className='dg-practice-deck'>
        {setup.deck.length === 0 && (
          <span className='dg-tab-note'>{t('点下面的牌加入牌组')}</span>
        )}
        {deckViews.map(
          (card, index) =>
            card && (
              <button
                key={index}
                type='button'
                className='dg-deck-chip'
                title={t('点击移出牌组')}
                onClick={() =>
                  update({ deck: setup.deck.filter((_, i) => i !== index) })
                }
              >
                {cardName(card, text)}
              </button>
            ),
        )}
      </div>
      <div className='dg-pool-head'>
        <Tabs type='button' size='small' activeKey={pool} onChange={setPool}>
          {CLASSES.map((id) => (
            <TabPane key={id} itemKey={id} tab={classes[id].name} />
          ))}
          <TabPane itemKey='any' tab={t('中立与特殊')} />
        </Tabs>
        <label>
          <Switch size='small' checked={upgraded} onChange={setUpgraded} />
          {t('加入升级版')}
        </label>
      </div>
      <div className='dg-deck-grid'>
        {poolCards.map((card) => {
          const shown = upgraded && card.upgrade ? card.upgrade : card;
          return (
            <CardFace
              key={card.id}
              card={shown}
              small
              disabled={deckFull}
              onClick={() =>
                update({
                  deck: [
                    ...setup.deck,
                    { id: card.id, up: !!(upgraded && card.upgrade) },
                  ],
                })
              }
            />
          );
        })}
      </div>
      <div className='dg-start-row'>
        <Button
          theme='solid'
          size='large'
          loading={busy}
          disabled={!setup.deck.length || !setup.enemies.length}
          onClick={() => onStart('practice', { practice: setup })}
        >
          {t('开始练习')}
        </Button>
      </div>
    </div>
  );
};

// 营地：三种模式的入口，以及魂晶解锁的职业与天赋。
const Lobby = ({
  info,
  tab,
  setTab,
  rounds,
  busy,
  turnstile,
  onStart,
  onResume,
  onUnlock,
  onBuy,
}) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  if (!info?.meta) return null;
  return (
    <div className='dg-lobby'>
      <div className='logic-heading'>
        <span className='logic-eyebrow'>{t('卡牌肉鸽')}</span>
        <h2>{t('深渊远征')}</h2>
        <p>{t('组牌、闯关、越走越深')}</p>
      </div>
      <Tabs type='line' activeKey={tab} onChange={setTab}>
        <TabPane itemKey='endless' tab={t('无尽远征')}>
          <EndlessTab
            info={info}
            round={rounds.endless}
            busy={busy}
            turnstile={turnstile}
            onStart={onStart}
            onResume={onResume}
            onUnlock={onUnlock}
          />
        </TabPane>
        <TabPane itemKey='trial' tab={t('试炼')}>
          <TrialTab
            info={info}
            round={rounds.trial}
            busy={busy}
            onStart={onStart}
            onResume={onResume}
          />
        </TabPane>
        <TabPane itemKey='practice' tab={t('练习')}>
          <PracticeTab
            info={info}
            round={rounds.practice}
            busy={busy}
            onStart={onStart}
            onResume={onResume}
          />
        </TabPane>
        <TabPane itemKey='shop' tab={t('道具商店')}>
          <GameShop
            shop={info.shop}
            texts={text.items}
            icons={ITEM_ICONS}
            owned={info.meta.items}
            maxStack={info.catalog?.max_item_stack || 99}
            symbol={info.symbol}
            busy={busy}
            note={t(
              '道具在三种模式里都能用，用掉一件少一件；每轮最多用 {{n}} 件',
              { n: info.catalog?.items_per_run || 5 },
            )}
            onBuy={onBuy}
          />
        </TabPane>
      </Tabs>
    </div>
  );
};

export default Lobby;
