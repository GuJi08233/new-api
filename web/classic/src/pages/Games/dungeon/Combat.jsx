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

import React, { useEffect, useRef, useState } from 'react';
import { Button, Tooltip } from '@douyinfe/semi-ui';
import {
  HeartPulse,
  Hourglass,
  Layers,
  Shield,
  Swords,
  Trash2,
  TrendingDown,
  TrendingUp,
  Zap,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Floaters, useFloaters } from '../common/effects';
import { playSound } from '../common/sound';
import {
  CardFace,
  HpBar,
  PileModal,
  StatusList,
  useDungeonText,
} from './parts';
import { getEnemyNames } from './text';

// 一次操作里的事件逐个播放的间隔(毫秒)；敌人出招之间多停一会儿，看得清是谁打的。
const EVENT_GAP = 110;
const ACT_GAP = 420;

// 敌人公布的下一招：攻击带伤害数字，其余用图标表示，悬停看说明。
const IntentBadge = ({ intent }) => {
  const { t } = useTranslation();
  if (!intent) return null;
  const notes = [];
  if (intent.dmg > 0 || intent.times > 0) {
    notes.push(
      intent.times > 1
        ? t('攻击 {{times}} 次，每次 {{dmg}} 点伤害', intent)
        : t('攻击，造成 {{dmg}} 点伤害', intent),
    );
  }
  if (intent.block) notes.push(t('获得护体'));
  if (intent.buff) notes.push(t('强化自己'));
  if (intent.debuff) notes.push(t('削弱你'));
  if (intent.heal) notes.push(t('回复生命'));
  const attack = intent.dmg > 0 || intent.times > 0;
  return (
    <Tooltip content={notes.join('，')}>
      <span className={`dg-intent ${attack ? 'is-attack' : ''}`}>
        {attack && (
          <>
            <Swords size={15} />
            <strong>
              {intent.dmg}
              {intent.times > 1 && <small>×{intent.times}</small>}
            </strong>
          </>
        )}
        {intent.block && <Shield size={15} />}
        {intent.buff && <TrendingUp size={15} />}
        {intent.debuff && <TrendingDown size={15} />}
        {intent.heal && <HeartPulse size={15} />}
      </span>
    </Tooltip>
  );
};

// 战斗：敌人、玩家与手牌。targeting 是正在选目标的牌或药水，由页面持有(药水在顶栏里点)。
const Combat = ({ run, busy, send, events, targeting, setTargeting }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const enemyNames = getEnemyNames(t);
  const combat = run.combat;
  const [floaters, addFloater] = useFloaters();
  const [pulses, setPulses] = useState({});
  const [pile, setPile] = useState(null);
  const timers = useRef([]);

  useEffect(
    () => () => {
      timers.current.forEach(clearTimeout);
    },
    [],
  );

  // 播放这一步里发生的事：伤害与护甲飘字、受击与出招的动作、音效。
  useEffect(() => {
    if (!events?.list?.length) return;
    timers.current.forEach(clearTimeout);
    timers.current = [];
    const count = Math.max(combat?.enemies?.length || 1, 1);
    const at = (target) =>
      target < 0
        ? { x: 50, y: 62 }
        : { x: ((target + 0.5) / count) * 100, y: 22 };
    const pulse = (key, kind) => {
      setPulses((previous) => ({ ...previous, [key]: kind }));
      timers.current.push(
        setTimeout(
          () =>
            setPulses((previous) => {
              const next = { ...previous };
              delete next[key];
              return next;
            }),
          380,
        ),
      );
    };
    let delay = 0;
    for (const ev of events.list) {
      const run = () => {
        const where = at(ev.t);
        const who = ev.t < 0 ? 'p' : `e${ev.t}`;
        switch (ev.k) {
          case 'play':
            playSound('place');
            break;
          case 'act':
            pulse(who, 'act');
            break;
          case 'hit':
            if (ev.n > 0) {
              pulse(who, 'hit');
              playSound(ev.t < 0 ? 'hit' : 'break', {
                pitch: ev.s === 'burn' ? 0.8 : 1,
              });
              addFloater(`-${ev.n}`, {
                ...where,
                kind: ev.t < 0 ? 'bad' : 'gold',
              });
            } else if (ev.s === 'blocked') {
              playSound('chip');
              addFloater(t('格挡'), { ...where, kind: 'gain' });
            }
            break;
          case 'block':
            playSound('chip');
            addFloater(t('护体 +{{n}}', { n: ev.n }), {
              ...where,
              kind: 'gain',
            });
            break;
          case 'heal':
            addFloater(`+${ev.n}`, { ...where, kind: 'gain' });
            break;
          case 'apply':
            addFloater(`${text.statuses[ev.s] || ev.s} +${ev.n}`, {
              ...where,
              kind:
                ['weak', 'vuln', 'burn'].includes(ev.s) === ev.t < 0
                  ? 'bad'
                  : 'gold',
            });
            break;
          case 'dodge':
            playSound('whoosh');
            addFloater(t('闪避'), { ...where, kind: 'gain' });
            break;
          case 'die':
            pulse(who, 'die');
            playSound('bomb');
            break;
          case 'shuffle':
            playSound('deal');
            break;
          case 'energy':
            addFloater(t('灵力 +{{n}}', { n: ev.n }), {
              ...where,
              kind: 'gold',
            });
            break;
          case 'wound':
            addFloater(t('{{n}} 张伤口混入弃牌堆', { n: ev.n }), {
              ...where,
              kind: 'bad',
            });
            break;
          case 'potion':
            playSound('pour');
            break;
          case 'revive':
            playSound('sparkle');
            addFloater(t('涅槃重生'), { ...where, kind: 'gold' });
            break;
          case 'turn':
            if (ev.n > 1) playSound('deal');
            break;
          default:
        }
      };
      if (delay === 0) run();
      else timers.current.push(setTimeout(run, delay));
      delay += ev.k === 'act' ? ACT_GAP : EVENT_GAP;
    }
    // 只在新的一步到来时播放一次。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [events?.key]);

  if (!combat) return null;
  const alive = combat.enemies
    .map((enemy, index) => (!enemy.dead ? index : -1))
    .filter((index) => index >= 0);

  const playCard = (index) => {
    const card = combat.hand[index];
    if (busy) return;
    if (targeting?.card === index) {
      setTargeting(null);
      return;
    }
    if (card.cost < 0 || card.cost > combat.energy) {
      playSound('error');
      return;
    }
    if (card.target === 1 && alive.length > 1) {
      playSound('tick');
      setTargeting({ card: index });
      return;
    }
    setTargeting(null);
    send('play', { card: index, target: card.target === 1 ? alive[0] : -1 });
  };

  const pickEnemy = (index) => {
    if (!targeting || busy || combat.enemies[index].dead) return;
    const fields =
      targeting.card !== undefined
        ? { action: 'play', card: targeting.card, target: index }
        : { action: 'potion', potion: targeting.potion, target: index };
    setTargeting(null);
    send(fields.action, fields);
  };

  const piles = {
    draw: {
      title: t('抽牌堆'),
      cards: combat.draw,
      note: t('按牌名排列，不代表抽牌顺序'),
    },
    discard: { title: t('弃牌堆'), cards: combat.discard },
    exhaust: { title: t('消耗堆'), cards: combat.exhaust },
  };

  return (
    <div className={`dg-combat is-${combat.kind}`}>
      <div className='dg-enemies'>
        {combat.enemies.map((enemy, index) => (
          <button
            key={index}
            type='button'
            className={[
              'dg-enemy',
              enemy.dead ? 'is-dead' : '',
              targeting && !enemy.dead ? 'is-targetable' : '',
              pulses[`e${index}`] ? `is-${pulses[`e${index}`]}` : '',
            ].join(' ')}
            disabled={!targeting || enemy.dead || busy}
            onClick={() => pickEnemy(index)}
            aria-label={enemyNames[enemy.id] || enemy.id}
          >
            {!enemy.dead && <IntentBadge intent={enemy.intent} />}
            <span className={`dg-enemy-art is-${enemy.id}`} aria-hidden />
            <span className='dg-enemy-name'>
              {enemyNames[enemy.id] || enemy.id}
            </span>
            <HpBar hp={enemy.hp} max={enemy.max_hp} block={enemy.block} />
            <StatusList st={enemy.st} />
          </button>
        ))}
      </div>
      <div className={`dg-player ${pulses.p ? `is-${pulses.p}` : ''}`}>
        <Tooltip content={t('灵力：施展功法要花费的点数，每回合回满')}>
          <span className='dg-energy'>
            <Zap size={16} />
            <strong>{combat.energy}</strong>
          </span>
        </Tooltip>
        <div className='dg-player-body'>
          <HpBar hp={run.hp} max={run.max_hp} block={combat.block} />
          <StatusList st={combat.st} />
        </div>
        <span className='dg-turn'>
          {t('第 {{turn}} 回合', { turn: combat.turn })}
        </span>
      </div>
      <p className='dg-help' aria-live='polite'>
        {targeting
          ? t('选择一个敌人作为目标，再点一次这张牌取消')
          : t('点击手牌打出；需要目标的牌再点选一个敌人')}
      </p>
      <div className='dg-hand' style={{ '--hand': combat.hand.length }}>
        {combat.hand.map((card, index) => (
          <CardFace
            key={`${events?.key || 0}-${index}-${card.id}`}
            card={card}
            className='dg-hand-card'
            style={{ '--i': index }}
            selected={targeting?.card === index}
            dim={card.cost < 0 || card.cost > combat.energy}
            disabled={busy}
            onClick={() => playCard(index)}
          />
        ))}
      </div>
      <div className='dg-combat-bar'>
        <Button icon={<Layers size={15} />} onClick={() => setPile('draw')}>
          {t('抽牌堆 {{n}}', { n: combat.draw.length })}
        </Button>
        <Button
          icon={<Hourglass size={15} />}
          onClick={() => setPile('discard')}
        >
          {t('弃牌堆 {{n}}', { n: combat.discard.length })}
        </Button>
        {combat.exhaust.length > 0 && (
          <Button
            icon={<Trash2 size={15} />}
            onClick={() => setPile('exhaust')}
          >
            {t('消耗堆 {{n}}', { n: combat.exhaust.length })}
          </Button>
        )}
        <Button
          theme='solid'
          type='primary'
          className='dg-end-turn'
          loading={busy}
          onClick={() => {
            setTargeting(null);
            send('end');
          }}
        >
          {t('结束回合')}
        </Button>
      </div>
      <Floaters items={floaters} />
      <PileModal
        visible={!!pile}
        title={pile ? piles[pile].title : ''}
        cards={pile ? piles[pile].cards : []}
        note={pile ? piles[pile].note : ''}
        onCancel={() => setPile(null)}
      />
    </div>
  );
};

export default Combat;
