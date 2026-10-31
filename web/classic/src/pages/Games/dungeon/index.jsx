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

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Dropdown, Modal } from '@douyinfe/semi-ui';
import { Coins, FlaskConical, Layers } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { playSkillFeedback, SkillSide } from '../common/skill';
import { Floaters, celebrate, useFloaters } from '../common/effects';
import { playSound } from '../common/sound';
import { gameGet, gamePost, withTurnstile } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import { usePlayTurnstile } from '../common/usePlayTurnstile';
import Combat from './Combat';
import Lobby from './Lobby';
import MapView from './MapView';
import {
  EndScreen,
  EventScreen,
  RestScreen,
  RewardScreen,
  ShopScreen,
} from './Screens';
import {
  DungeonTextProvider,
  HpBar,
  PileModal,
  RelicChip,
  useDungeonText,
} from './parts';
import { getGameLevelNames } from '../../../constants/game.constants';
import { getClassText } from './text';
import '../common/logic-puzzles.css';
import './dungeon.css';

const MODES = ['endless', 'trial', 'practice'];

// 战斗之外的事件(拿到遗物、金币、升级……)在台面中央飘一行字。
function useOutsideFloaters() {
  const { t } = useTranslation();
  const text = useDungeonText();
  const [floaters, addFloater] = useFloaters();
  const show = useCallback(
    (events) => {
      let row = 0;
      for (const ev of events) {
        let line = '';
        let kind = 'gold';
        switch (ev.k) {
          case 'relic':
            line = t('获得遗物：{{name}}', { name: text.relics[ev.s]?.name });
            playSound('sparkle');
            break;
          case 'gold':
            line = ev.n > 0 ? `+${ev.n} ${t('金币')}` : `${ev.n} ${t('金币')}`;
            kind = ev.n > 0 ? 'gold' : 'bad';
            playSound('coin');
            break;
          case 'card':
            line = t('获得卡牌：{{name}}', { name: text.cards[ev.s] });
            playSound('flip');
            break;
          case 'upgrade':
            line = t('升级：{{name}}', { name: text.cards[ev.s] });
            playSound('merge');
            break;
          case 'remove':
            line = t('移除：{{name}}', { name: text.cards[ev.s] });
            playSound('whoosh');
            break;
          case 'heal':
            line = `+${ev.n}`;
            kind = 'gain';
            break;
          case 'max_hp':
            line = t('最大生命 +{{n}}', { n: ev.n });
            kind = 'gain';
            break;
          case 'hit':
            if (ev.n > 0) {
              line = `-${ev.n}`;
              kind = 'bad';
              playSound('hit');
            }
            break;
          default:
        }
        if (line) {
          addFloater(line, { x: 50, y: 30 + row * 9, kind });
          row += 1;
        }
      }
    },
    [addFloater, t, text],
  );
  return [floaters, show];
}

const DungeonPage = () => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const { info, loading, unavailable, patch, reload } = useGameInfo('dungeon');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  // 每种模式各自的一局；active 是正在台面上的模式，为空时显示营地。
  const [rounds, setRounds] = useState({});
  const [active, setActive] = useState(null);
  const [lobbyTab, setLobbyTab] = useState('endless');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [checked, setChecked] = useState(false);
  const [events, setEvents] = useState(null);
  const [targeting, setTargeting] = useState(null);
  const [deckOpen, setDeckOpen] = useState(false);
  const [floaters, showOutside] = useOutsideFloaters();
  const busyRef = useRef(false);
  const eventKey = useRef(0);
  const levelNames = getGameLevelNames(t).dungeon;
  const classes = getClassText(t);

  const round = active ? rounds[active] : null;
  const run = round?.run;

  // 换上服务端给的局面，并播放这一步发生的事。同一局更旧的回应不覆盖新的。
  const accept = useCallback(
    (mode, next) => {
      setRounds((previous) => {
        const current = previous[mode];
        if (
          current &&
          next &&
          current.session_id === next.session_id &&
          current.version > next.version
        )
          return previous;
        return { ...previous, [mode]: next };
      });
      if (!next?.run) return;
      const list = next.run.events || [];
      if (list.length) {
        eventKey.current += 1;
        setEvents({ key: eventKey.current, list });
        if (!next.run.combat) showOutside(list);
        if (list.some((ev) => ev.k === 'win')) playSound('win');
        if (list.some((ev) => ev.k === 'victory')) celebrate(1);
        if (list.some((ev) => ev.k === 'defeat')) playSound('lose');
      }
      if (next.result) {
        patch({
          balance: next.result.balance,
          progress: next.result.progress,
          best_score: next.result.best_score,
        });
        if (mode === 'endless')
          playSkillFeedback(next.result, next.run.bosses > 0);
        reload(true);
      }
    },
    [patch, reload, showOutside],
  );

  const restore = useCallback(
    async (mode, id = 0) => {
      const query = `mode=${mode}${id ? `&session_id=${id}` : ''}`;
      const response = await gameGet(`/api/game/dungeon/state?${query}`, t);
      if (response.data) {
        setRounds((previous) => ({
          ...previous,
          [mode]: response.data.session,
        }));
        return response.data.session;
      }
      setError(response.error);
      return null;
    },
    [t],
  );

  // 打开页面时读出三种模式进行中的一局；有进行中的无尽远征就直接回到它。
  const loaded = !!info;
  useEffect(() => {
    if (!loaded) return;
    Promise.all(MODES.map((mode) => restore(mode))).then((sessions) => {
      if (sessions[0]?.status === 'playing') setActive('endless');
      setChecked(true);
    });
  }, [loaded, restore]);

  const post = async (url, body, mode) => {
    if (busyRef.current) return false;
    busyRef.current = true;
    setBusy(true);
    setError('');
    try {
      const response = await gamePost(url, body, t);
      if (response.data) {
        if (mode) accept(mode, response.data);
        return response.data;
      }
      playSound('error');
      setError(response.error);
      return null;
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const start = async (mode, fields) => {
    const url =
      mode === 'endless'
        ? withTurnstile('/api/game/dungeon/start', turnstile.take())
        : '/api/game/dungeon/start';
    const data = await post(url, { mode, ...fields }, mode);
    if (data) {
      playSound('start');
      setTargeting(null);
      setActive(mode);
    }
  };

  const send = async (action, fields = {}) => {
    const current = rounds[active];
    if (!current || current.status !== 'playing') return false;
    const data = await post(
      '/api/game/dungeon/action',
      {
        ...fields,
        action,
        mode: active,
        session_id: current.session_id,
        version: current.version,
      },
      active,
    );
    if (!data) await restore(active, current.session_id);
    return !!data;
  };

  const unlock = async (item) => {
    const data = await post('/api/game/dungeon/unlock', { item });
    if (data) {
      playSound('sparkle');
      patch({ meta: data.meta });
    }
  };

  const confirmAbandon = () =>
    Modal.confirm({
      title: active === 'trial' ? t('放弃试炼') : t('放弃远征'),
      content:
        active === 'trial'
          ? t('放弃后本次挑战按未过关结算。')
          : t('放弃后按当前进度结算魂晶与奖励，不能继续。'),
      onOk: () => send('abandon'),
    });

  const backToCamp = () => {
    setTargeting(null);
    setLobbyTab(active || 'endless');
    setActive(null);
  };

  const drinkPotion = (slot) => {
    const potion = run.potions[slot];
    const alive = (run.combat?.enemies || [])
      .map((enemy, index) => (!enemy.dead ? index : -1))
      .filter((index) => index >= 0);
    if (potion === 'fire' && alive.length > 1) {
      setTargeting({ potion: slot });
      return;
    }
    send('potion', { potion: slot, target: alive[0] ?? -1 });
  };

  const finished = round?.status === 'finished';
  const practiceDone = run?.phase === 'done';

  let stage = null;
  if (!active || !round) {
    stage = (
      <Lobby
        info={info}
        tab={lobbyTab}
        setTab={setLobbyTab}
        rounds={rounds}
        busy={busy}
        turnstile={turnstile}
        onStart={start}
        onResume={(mode) => setActive(mode)}
        onUnlock={unlock}
      />
    );
  } else if (finished) {
    stage = (
      <EndScreen
        round={round}
        symbol={info?.symbol}
        levelNames={levelNames}
        onBack={backToCamp}
      />
    );
  } else if (practiceDone) {
    stage = (
      <div
        className={`dg-screen dg-end ${run.result === 'won' ? 'is-won' : 'is-lost'}`}
      >
        <h3>{run.result === 'won' ? t('练习战胜利') : t('练习战失败')}</h3>
        <div className='dg-screen-actions'>
          <Button theme='solid' disabled={busy} onClick={() => send('reset')}>
            {t('再来一次')}
          </Button>
          <Button onClick={backToCamp}>{t('修改设置')}</Button>
        </div>
      </div>
    );
  } else if (run.phase === 'combat') {
    stage = (
      <Combat
        run={run}
        busy={busy}
        send={send}
        events={events}
        targeting={targeting}
        setTargeting={setTargeting}
      />
    );
  } else if (run.phase === 'map') {
    stage = (
      <MapView
        run={run}
        busy={busy}
        onPick={(node) => send('path', { node })}
      />
    );
  } else if (run.phase === 'reward') {
    stage = <RewardScreen run={run} busy={busy} send={send} />;
  } else if (run.phase === 'shop') {
    stage = <ShopScreen run={run} busy={busy} send={send} />;
  } else if (run.phase === 'rest') {
    stage = <RestScreen run={run} busy={busy} send={send} />;
  } else if (run.phase === 'event') {
    stage = <EventScreen run={run} busy={busy} send={send} />;
  }

  const showHud = active && run && !finished;
  const modeNames = {
    endless: t('无尽远征'),
    trial: t('试炼'),
    practice: t('练习'),
  };

  return (
    <GameLayout
      title={t('深渊远征')}
      loading={loading || (loaded && !checked)}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={info?.symbol}
      className='dg-page'
      side={
        info && (
          <SkillSide
            info={info}
            symbol={info.symbol}
            scoreName={t('打通')}
            levelNames={levelNames}
            extra={
              <>
                <SideBox
                  label={t('魂晶')}
                  value={info.meta?.crystals ?? 0}
                  note={t('累计获得 {{n}}', { n: info.meta?.earned ?? 0 })}
                />
                <SideBox label={t('玩法说明')}>
                  <RuleList
                    items={[
                      t(
                        '无尽远征：一幕接一幕往下打，打败首领进入下一幕，第四幕起敌人越来越强；按打通的幕数领游戏币',
                      ),
                      t('试炼：固定牌组的关卡，按星级奖励魂晶，不发游戏币'),
                      t('练习：自由组牌、选敌人，测试伤害与数值，没有奖励'),
                      t('魂晶用来解锁职业与天赋；打通前三幕解锁下一个深渊等级'),
                      t('每一步都保存在服务器上，刷新或换设备都能继续'),
                    ]}
                  />
                </SideBox>
              </>
            }
          />
        )
      }
    >
      <div className='games-stage dg-stage'>
        {showHud && (
          <div className='dg-hud'>
            <div className='dg-hud-row'>
              <span className={`dg-mode is-${active}`}>
                {modeNames[active]}
              </span>
              <strong>{classes[run.class]?.name}</strong>
              {active === 'endless' && (
                <>
                  <span>{t('深渊 {{abyss}}', { abyss: run.abyss })}</span>
                  <span>
                    {t('第 {{act}} 幕 · 第 {{floor}} 层', {
                      act: run.act,
                      floor: run.floor,
                    })}
                  </span>
                </>
              )}
              {active === 'trial' && (
                <span>
                  {t('第 {{fight}} / {{fights}} 场 · 已用 {{turns}} 回合', {
                    fight: run.fight + 1,
                    fights: run.fights,
                    turns: run.turns + (run.combat?.turn || 0),
                  })}
                </span>
              )}
              <span className='dg-hud-spacer' />
              {active === 'practice' ? (
                <>
                  <Button
                    size='small'
                    disabled={busy}
                    onClick={() => send('reset')}
                  >
                    {t('重新开始')}
                  </Button>
                  <Button size='small' onClick={backToCamp}>
                    {t('修改设置')}
                  </Button>
                </>
              ) : (
                <>
                  <Button size='small' onClick={backToCamp}>
                    {t('回到营地')}
                  </Button>
                  <Button
                    size='small'
                    type='danger'
                    disabled={busy}
                    onClick={confirmAbandon}
                  >
                    {t('放弃')}
                  </Button>
                </>
              )}
            </div>
            <div className='dg-hud-row'>
              <span className='dg-hud-hp'>
                <HpBar hp={run.hp} max={run.max_hp} />
              </span>
              {active === 'endless' && (
                <span className='dg-gold'>
                  <Coins size={15} />
                  {run.gold}
                </span>
              )}
              <span className='dg-potions'>
                {Array.from({ length: run.potion_slots }, (_, slot) => {
                  const potion = run.potions[slot];
                  if (!potion)
                    return <span key={slot} className='dg-potion is-empty' />;
                  const usable =
                    !busy && (run.phase === 'combat' || potion === 'heal');
                  return (
                    <Dropdown
                      key={slot}
                      trigger='click'
                      position='bottom'
                      render={
                        <Dropdown.Menu>
                          <Dropdown.Item disabled>
                            {text.potions[potion]?.desc}
                          </Dropdown.Item>
                          <Dropdown.Item
                            disabled={!usable}
                            onClick={() => drinkPotion(slot)}
                          >
                            {t('使用')}
                          </Dropdown.Item>
                          <Dropdown.Item
                            type='danger'
                            disabled={busy}
                            onClick={() => send('toss', { potion: slot })}
                          >
                            {t('丢弃')}
                          </Dropdown.Item>
                        </Dropdown.Menu>
                      }
                    >
                      <button
                        type='button'
                        className={`dg-potion is-${potion}`}
                        aria-label={text.potions[potion]?.name}
                      >
                        <FlaskConical size={15} />
                      </button>
                    </Dropdown>
                  );
                })}
              </span>
              <Button
                size='small'
                icon={<Layers size={14} />}
                onClick={() => setDeckOpen(true)}
              >
                {t('牌组 {{n}}', { n: run.deck.length })}
              </Button>
            </div>
            {run.relics.length > 0 && (
              <div className='dg-hud-row dg-relics'>
                {run.relics.map((id) => (
                  <RelicChip key={id} id={id} />
                ))}
              </div>
            )}
            {active === 'practice' && run.combat && (
              <div className='dg-meter'>
                <span>
                  {t('本场伤害')}
                  <strong>{run.combat.dealt}</strong>
                </span>
                <span>
                  {t('本回合伤害')}
                  <strong>{run.combat.turn_dealt}</strong>
                </span>
                <span>
                  {t('单次最高')}
                  <strong>{run.combat.max_hit}</strong>
                </span>
                <span>
                  {t('平均每回合')}
                  <strong>
                    {Math.round(
                      run.combat.dealt / Math.max(run.combat.turn, 1),
                    )}
                  </strong>
                </span>
              </div>
            )}
          </div>
        )}
        {round?.outdated && (
          <StageMessage kind='error'>
            {t('这一局是按旧版规则开始的，不能继续，请放弃后结算')}
          </StageMessage>
        )}
        <div className='dg-stage-body'>
          {stage}
          <Floaters items={floaters} />
        </div>
        {error && <StageMessage kind='error'>{error}</StageMessage>}
        {run && (
          <PileModal
            visible={deckOpen}
            title={t('牌组')}
            cards={run.deck}
            onCancel={() => setDeckOpen(false)}
          />
        )}
      </div>
    </GameLayout>
  );
};

// 文案表要在组件树里先提供，页面本身也要用到(药水说明、飘字)。
const Dungeon = () => (
  <DungeonTextProvider>
    <DungeonPage />
  </DungeonTextProvider>
);

export default Dungeon;
