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
import { Button, Modal, Tooltip } from '@douyinfe/semi-ui';
import {
  HeartCrack,
  HeartPulse,
  Hourglass,
  Keyboard,
  Layers,
  ScrollText,
  Shield,
  Swords,
  Trash2,
  TrendingDown,
  TrendingUp,
  Zap,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Floaters, useFloaters } from '../common/effects';
import { readGameStorage, writeGameStorage } from '../common/gameStorage';
import { playSound } from '../common/sound';
import EnemyArt from '../wendao/art/EnemyArt';
import { getBeastText } from '../wendao/cave/text';
import {
  CardFace,
  HpBar,
  PileModal,
  StatusList,
  useDungeonText,
} from './parts';
import { combatLog, getEnemyNames } from './text';

// 一次操作里的事件逐个播放的间隔(毫秒)；敌人出招之间多停一会儿，看得清是谁打的。
const EVENT_GAP = 110;
const ACT_GAP = 420;

// 键盘快捷键只在有鼠标的桌面上有：数字键出第几张牌(选目标时选第几个敌人)、E 结束回合、Esc 取消。可以关掉，记在浏览器里。
const SHORTCUTS_KEY = 'dungeon-shortcuts';
const hasKeyboard = () =>
  typeof window !== 'undefined' &&
  !!window.matchMedia?.('(hover: hover) and (pointer: fine)').matches;
// 还有能打出的牌时，E 要连按两次才结束回合，两次之间不超过这么久(毫秒)。
const END_CONFIRM_MS = 2500;
// 战况最多记这么多步；台面上只摆最近一步的最后几行，全部的在「战况」里看。
const LOG_STEPS = 80;
const LOG_SHOWN = 3;

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

// 战况的一行：起因加冒号，后面是它带来的变化。
const CombatLine = ({ line }) => {
  const { t } = useTranslation();
  return (
    <>
      {line.cause && <strong>{line.cause}</strong>}
      {line.cause && line.effects.length > 0 && t('：')}
      {line.effects.join(t('，'))}
    </>
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
  // log 是这场战斗的战况，每步一项 { key, lines }，新开一场(第 1 回合)时清空。
  const [log, setLog] = useState([]);
  const [logOpen, setLogOpen] = useState(false);
  const logEnd = useRef(null);
  const enemiesRef = useRef(null);
  const timers = useRef([]);
  const [keyboard] = useState(hasKeyboard);
  const [shortcuts, setShortcuts] = useState(
    () => readGameStorage(SHORTCUTS_KEY).value !== false,
  );
  // endArmed：按过一次 E、还有牌能出，等第二次确认。
  const [endArmed, setEndArmed] = useState(false);
  const endTimer = useRef(null);
  // aim 是鼠标停着或键盘焦点所在的那张手牌，敌人的生命条上预览它的伤害；选目标时预览选中的那张。
  const [aim, setAim] = useState(-1);
  useEffect(() => () => clearTimeout(endTimer.current), []);
  // 局面一变(出了牌、敌人行动过)就不再等第二次 E，手牌换了也不再预览原来那一张。
  useEffect(() => {
    clearTimeout(endTimer.current);
    setEndArmed(false);
    setAim(-1);
  }, [combat]);
  // keyHandler 每次渲染换成拿着最新局面的那一个，键盘监听只在开关变化时挂一次。
  const keyHandler = useRef(null);
  const keysOn = keyboard && shortcuts;
  useEffect(() => {
    if (!keysOn) return undefined;
    const onKey = (event) => keyHandler.current?.(event);
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [keysOn]);

  useEffect(
    () => () => {
      timers.current.forEach(clearTimeout);
    },
    [],
  );

  // 同名的敌人按出场顺序加上甲乙丙丁，台面上与战况里叫法一样。
  const marks = [t('甲'), t('乙'), t('丙'), t('丁'), t('戊')];
  const enemyLabels = (combat?.enemies || []).map((enemy, index, all) => {
    const name = enemyNames[enemy.id] || enemy.id;
    if (all.filter((other) => other.id === enemy.id).length < 2) return name;
    const nth = all
      .slice(0, index)
      .filter((other) => other.id === enemy.id).length;
    return t('{{name}}{{mark}}', { name, mark: marks[nth] ?? nth + 1 });
  });
  const beastNames = getBeastText(t);
  const names = {
    enemy: (i) => enemyLabels[i] || '',
    card: (id) => text.cards[id] || id,
    status: (id) => text.statuses[id] || id,
    potion: (id) => text.potions[id]?.name || id,
    item: (id) => text.items[id]?.name || id,
    relic: (id) => text.relics[id]?.name || id,
    beast: (kind) => beastNames[kind]?.name || kind,
  };

  // 播放这一步里发生的事：伤害与护甲飘字、受击与出招的动作、音效，并记进战况。
  useEffect(() => {
    if (!events?.list?.length) return;
    const lines = combatLog(events.list, names, t);
    const fresh = events.list.some((ev) => ev.k === 'turn' && ev.n === 1);
    setLog((previous) =>
      previous[previous.length - 1]?.key === events.key
        ? previous
        : [...(fresh ? [] : previous), { key: events.key, lines }].slice(
            -LOG_STEPS,
          ),
    );
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
            addFloater(names.potion(ev.s), { x: 50, y: 62, kind: 'gold' });
            break;
          case 'beast':
            pulse('p', 'act');
            playSound('whoosh');
            addFloater(t('{{name}}出手', { name: names.beast(ev.s) }), {
              ...where,
              kind: 'gold',
            });
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

  useEffect(() => {
    if (logOpen) logEnd.current?.scrollIntoView({ block: 'end' });
  }, [logOpen, log]);

  // 要选目标时把焦点移到第一个能选的敌人上：键盘接着按回车或方向上的 Tab 就能选，横屏或窄屏上敌人滚出去了也会滚回来。
  useEffect(() => {
    if (!targeting) return;
    enemiesRef.current?.querySelector('.dg-enemy:not(:disabled)')?.focus();
  }, [targeting]);

  if (!combat) {
    keyHandler.current = null;
    return null;
  }
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

  const playable = combat.hand.some(
    (card) => card.cost >= 0 && card.cost <= combat.energy,
  );
  const endTurn = () => {
    clearTimeout(endTimer.current);
    setEndArmed(false);
    setTargeting(null);
    send('end');
  };
  // 敌人这回合公开的攻击合计(公布的伤害已经算上双方的力量、虚弱与易伤)；护体与身法没扣，敌人出手前被真火烧死也不管。
  // 扣完以后最多掉多少血由服务端试算(combat.end_loss)。
  const incoming = combat.enemies.reduce(
    (sum, enemy) =>
      !enemy.dead && enemy.intent?.dmg > 0
        ? sum + enemy.intent.dmg * Math.max(enemy.intent.times, 1)
        : sum,
    0,
  );

  // 快捷键按键位认(event.code)，换了键盘布局也是那几个键；按住不放的连发、输入法组字、输入框、弹窗打开着、
  // 请求还没回来时不响应。
  keyHandler.current = (event) => {
    if (event.repeat || event.isComposing || busy) return;
    if (event.altKey || event.ctrlKey || event.metaKey) return;
    if (
      event.target?.closest?.(
        'input, textarea, select, [contenteditable="true"]',
      ) ||
      document.querySelector('.semi-modal-wrap')
    ) {
      return;
    }
    const digit = /^(Digit|Numpad)[0-9]$/.test(event.code)
      ? (Number(event.code.slice(-1)) + 9) % 10
      : -1;
    if (digit >= 0) {
      event.preventDefault();
      if (targeting) {
        if (combat.enemies[digit] && !combat.enemies[digit].dead) {
          pickEnemy(digit);
        }
      } else if (combat.hand[digit]) {
        playCard(digit);
      }
      return;
    }
    if (event.key === 'Escape') {
      setTargeting(null);
      setEndArmed(false);
      return;
    }
    if (event.code === 'KeyE') {
      event.preventDefault();
      if (!playable || endArmed) {
        endTurn();
        return;
      }
      playSound('tick');
      setEndArmed(true);
      clearTimeout(endTimer.current);
      endTimer.current = setTimeout(() => setEndArmed(false), END_CONFIRM_MS);
    }
  };

  // 伤害预览由服务端在局面的副本上试打得出(CardView.preview)；随机伤害的牌没有预览，灵力不够的牌不预览。
  const aimedCard = combat.hand[targeting?.card ?? aim];
  const aimed =
    aimedCard && aimedCard.cost >= 0 && aimedCard.cost <= combat.energy
      ? aimedCard
      : null;

  // 台面上只摆最近一步的最后几行，什么也没发生的新回合与回合结束不占地方。
  const latest = (log[log.length - 1]?.lines || [])
    .filter((line) => line.effects.length > 0 || !line.quiet)
    .slice(-LOG_SHOWN);

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
      <div className='dg-enemies' ref={enemiesRef}>
        {combat.enemies.map((enemy, index) => {
          const loss = !enemy.dead ? aimed?.preview?.[index] : undefined;
          return (
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
              aria-label={enemyLabels[index]}
            >
              {!enemy.dead && <IntentBadge intent={enemy.intent} />}
              {keysOn && targeting && !enemy.dead && index < 10 && (
                <kbd className='dg-hotkey'>{(index + 1) % 10}</kbd>
              )}
              <span className={`dg-enemy-art is-${enemy.id}`} aria-hidden>
                <EnemyArt id={enemy.id} />
              </span>
              <span className='dg-enemy-name'>{enemyLabels[index]}</span>
              {loss && (
                <span className='dg-preview'>
                  <strong>
                    {loss.hp >= enemy.hp ? t('击败') : `−${loss.hp}`}
                  </strong>
                  {loss.block > 0 && (
                    <small>{t('破护体 {{n}}', { n: loss.block })}</small>
                  )}
                </span>
              )}
              <HpBar
                hp={enemy.hp}
                max={enemy.max_hp}
                block={enemy.block}
                loss={loss}
              />
              <StatusList st={enemy.st} />
            </button>
          );
        })}
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
        <div className='dg-player-side'>
          <span className='dg-turn'>
            {t('第 {{turn}} 回合', { turn: combat.turn })}
          </span>
          {(incoming > 0 || combat.end_loss > 0) && (
            <Tooltip
              content={t(
                '来袭是敌人这回合公开的攻击合计，没扣护体与身法；心形后面是现在结束回合、到下一回合开始最多会失去的生命，护体、身法、铜皮与真火都算上了，灵兽出手只会让它更少，没算进去',
              )}
            >
              <span className='dg-incoming'>
                {incoming > 0 && (
                  <>
                    <Swords size={13} />
                    {t('来袭 {{n}}', { n: incoming })}
                  </>
                )}
                <span
                  className={`dg-end-loss${combat.end_loss >= run.hp ? ' is-lethal' : combat.end_loss > 0 ? ' is-hurt' : ''}`}
                >
                  <HeartCrack size={13} />
                  {combat.end_loss > 0 ? `−${combat.end_loss}` : t('无伤')}
                </span>
              </span>
            </Tooltip>
          )}
        </div>
      </div>
      {log.length > 0 && (
        <div className='dg-log'>
          <ol aria-live='polite'>
            {latest.map((line, index) => (
              <li key={`${log.length}-${index}`}>
                <CombatLine line={line} />
              </li>
            ))}
          </ol>
          <Button
            size='small'
            theme='borderless'
            icon={<ScrollText size={14} />}
            onClick={() => setLogOpen(true)}
          >
            {t('战况')}
          </Button>
        </div>
      )}
      <p className='dg-help' aria-live='polite'>
        {endArmed
          ? t('还有能打出的功法，再按一次 E 结束回合')
          : targeting
            ? keysOn
              ? t(
                  '选择一个敌人作为目标(数字键选第几个)，再点一次这张牌或按 Esc 取消',
                )
              : t('选择一个敌人作为目标，再点一次这张牌取消')
            : keysOn
              ? t(
                  '点击手牌或按数字键打出；需要目标的牌再选一个敌人；E 结束回合',
                )
              : t('点击手牌打出；需要目标的牌再点选一个敌人')}
      </p>
      <div
        className={`dg-hand${targeting ? ' is-targeting' : ''}`}
        style={{ '--hand': combat.hand.length }}
      >
        {combat.hand.map((card, index) => (
          <CardFace
            key={`${events?.key || 0}-${index}-${card.id}`}
            card={card}
            className='dg-hand-card'
            style={{ '--i': index }}
            selected={targeting?.card === index}
            dim={card.cost < 0 || card.cost > combat.energy}
            disabled={busy}
            hotkey={keysOn && index < 10 ? (index + 1) % 10 : undefined}
            onClick={() => playCard(index)}
            onMouseEnter={() => setAim(index)}
            onMouseLeave={() =>
              setAim((current) => (current === index ? -1 : current))
            }
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
        {keyboard && (
          <Button
            icon={<Keyboard size={15} />}
            theme={shortcuts ? 'light' : 'borderless'}
            aria-pressed={shortcuts}
            onClick={() => {
              writeGameStorage(SHORTCUTS_KEY, !shortcuts);
              setShortcuts(!shortcuts);
              setEndArmed(false);
            }}
          >
            {shortcuts ? t('快捷键：开') : t('快捷键：关')}
          </Button>
        )}
        <Button
          theme='solid'
          type='primary'
          className='dg-end-turn'
          loading={busy}
          onClick={endTurn}
        >
          {t('结束回合')}
          {keysOn && <kbd className='dg-hotkey is-inline'>E</kbd>}
        </Button>
      </div>
      <Floaters items={floaters} />
      <Modal
        visible={logOpen}
        title={t('战况')}
        footer={null}
        onCancel={() => setLogOpen(false)}
        width={560}
        bodyStyle={{ paddingBottom: 20 }}
      >
        <ol className='dg-log-full'>
          {log.flatMap((step) =>
            step.lines.map((line, index) =>
              line.quiet && !line.turn && !line.effects.length ? null : (
                <li
                  key={`${step.key}-${index}`}
                  className={line.turn ? 'is-turn' : undefined}
                >
                  <CombatLine line={line} />
                </li>
              ),
            ),
          )}
        </ol>
        <span ref={logEnd} />
      </Modal>
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
