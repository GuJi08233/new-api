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
import { useTranslation } from 'react-i18next';
import PvpGame from '../common/pvp/PvpGame';
import { BoardAssist, currentHint } from '../common/pvp/BoardAssist';
import { playSound } from '../common/sound';
import { PITS, holeOf, sow, storeOf } from './logic';
import './mancala.css';

// 每落一颗子的间隔。
const SOW_MS = 110;
const START_BOARD = [4, 4, 4, 4, 4, 4, 0, 4, 4, 4, 4, 4, 4, 0];
// 坑里最多画几颗子，再多只看数字。
const MAX_PEBBLES = 16;
const PEBBLE_COLORS = ['#f59e0b', '#10b981', '#3b82f6', '#ec4899', '#a855f7'];

// 子在坑里的位置：按黄金角绕圈排开，同一个坑每次画出来一样。
const pebbleAt = (index) => {
  const angle = index * 2.39996;
  const radius = 9 + Math.sqrt(index) * 9;
  return {
    left: `calc(50% + ${Math.cos(angle) * radius}% - 6px)`,
    top: `calc(50% + ${Math.sin(angle) * radius}% - 6px)`,
    background: PEBBLE_COLORS[(index * 7) % PEBBLE_COLORS.length],
  };
};

const Pebbles = ({ count }) => (
  <span className='mancala-pebbles' aria-hidden>
    {Array.from({ length: Math.min(count, MAX_PEBBLES) }, (_, i) => (
      <span key={i} className='mancala-pebble' style={pebbleAt(i)} />
    ))}
  </span>
);

// 看的人是先手(side 0)还是后手(side 1)。旁观或还没开局时按座位 0 看。
const sideOf = (state, seat) => (state && state.first !== seat ? 1 : 0);

const MancalaBoard = ({
  state,
  seat,
  canMove,
  busy,
  over,
  onMove,
  onUndo,
  hint,
  hintStatus,
  onHint,
}) => {
  const { t } = useTranslation();
  const board = state?.board || START_BOARD;
  const side = sideOf(state, seat);
  // 自己这一步的播子动画：每一帧的盘面与正在落子的坑。播完保留最后一帧，直到服务端的局面到来。
  const [sowing, setSowing] = useState(null);
  const timers = useRef([]);
  const moves = state?.moves ?? 0;

  useEffect(() => () => timers.current.forEach(clearTimeout), []);
  // 自己这一步播完之后：服务端的局面到了(步数变了)就换上它，对方接着走的一步按顺序亮一遍；请求被拒绝(不忙了步数却没变)
  // 就收起动画，回到服务端的局面。
  useEffect(() => {
    if (!sowing?.done) return;
    if (moves !== sowing.fromMoves || !busy) setSowing(null);
  }, [sowing, moves, busy]);

  const shown = sowing ? sowing.frames[sowing.step] : board;
  const shownHint = sowing ? null : currentHint(hint, state);

  const play = (pit) => {
    if (!canMove || over || sowing) return;
    const plan = sow(board, side, pit);
    if (!plan) return;
    onMove({ pit });
    timers.current.forEach(clearTimeout);
    timers.current = [];
    setSowing({
      frames: plan.frames,
      path: plan.path,
      step: 0,
      done: false,
      plan,
      fromMoves: moves,
    });
    plan.frames.slice(1).forEach((_, index) => {
      timers.current.push(
        setTimeout(
          () => {
            const step = index + 1;
            const last = step === plan.frames.length - 1;
            setSowing((current) =>
              current ? { ...current, step, done: last } : current,
            );
            if (step <= plan.path.length)
              playSound('peg', { pitch: 0.9 + step * 0.05 });
            if (last && plan.captured >= 0) playSound('score');
            else if (last && plan.extra) playSound('coin');
          },
          SOW_MS * (index + 1),
        ),
      );
    });
  };

  // 落子高亮：自己正在播的这一步，或者服务端最后一步依次落子的坑(按顺序错开)。
  const trail = sowing
    ? new Map(sowing.path.slice(0, sowing.step).map((hole, i) => [hole, i]))
    : new Map((state?.last || []).map((hole, i) => [hole, i]));
  const active = sowing ? sowing.path[sowing.step - 1] : -1;
  const captured = sowing
    ? sowing.done
      ? sowing.plan.captured
      : -1
    : (state?.captured ?? -1);
  const fresh = sowing ? 'sow' : `m${state?.moves ?? 0}`;

  const pitClass = (hole) => {
    const classes = ['mancala-pit'];
    if (trail.has(hole)) classes.push('is-trail');
    if (hole === active) classes.push('is-active');
    if (hole === captured) classes.push('is-captured');
    return classes.join(' ');
  };
  const pitStyle = (hole) =>
    trail.has(hole) && !sowing
      ? { '--trail-delay': `${trail.get(hole) * 70}ms` }
      : undefined;

  const mine = Array.from({ length: PITS }, (_, pit) => holeOf(side, pit));
  // 对面的坑从右往左排，逆时针接着自己这一排。
  const theirs = Array.from({ length: PITS }, (_, pit) =>
    holeOf(1 - side, pit),
  ).reverse();
  const myStore = storeOf(side);
  const theirStore = storeOf(1 - side);
  const playable = canMove && !over && !sowing;

  const store = (hole, label) => (
    <div
      key={`${fresh}-${hole}`}
      className={`mancala-store ${trail.has(hole) ? 'is-trail' : ''} ${hole === myStore ? 'is-mine' : ''}`}
      style={pitStyle(hole)}
      aria-label={t('{{label}}：{{count}} 颗', { label, count: shown[hole] })}
    >
      <Pebbles count={shown[hole]} />
      <strong>{shown[hole]}</strong>
      <span>{label}</span>
    </div>
  );

  return (
    <div className='mancala-wrap'>
      <div className={`mancala-board ${over ? 'is-over' : ''}`}>
        {store(theirStore, t('对方大仓'))}
        <div className='mancala-rows'>
          <div className='mancala-row is-theirs'>
            {theirs.map((hole) => (
              <div
                key={`${fresh}-${hole}`}
                className={pitClass(hole)}
                style={pitStyle(hole)}
                aria-label={t('对方的坑：{{count}} 颗', { count: shown[hole] })}
              >
                <Pebbles count={shown[hole]} />
                <strong>{shown[hole]}</strong>
              </div>
            ))}
          </div>
          <div className='mancala-row is-mine'>
            {mine.map((hole, pit) => {
              const open = playable && shown[hole] > 0;
              const hinted = shownHint && shownHint.x === pit;
              return (
                <button
                  key={`${fresh}-${hole}`}
                  type='button'
                  className={`${pitClass(hole)} ${open ? 'is-open' : ''} ${hinted ? 'is-hint' : ''}`}
                  style={pitStyle(hole)}
                  disabled={!open}
                  aria-label={t('播第 {{pit}} 个坑：{{count}} 颗', {
                    pit: pit + 1,
                    count: shown[hole],
                  })}
                  onClick={() => play(pit)}
                >
                  <Pebbles count={shown[hole]} />
                  <strong>{shown[hole]}</strong>
                </button>
              );
            })}
          </div>
        </div>
        {store(myStore, t('我的大仓'))}
      </div>
      <p className='mancala-note' aria-live='polite'>
        {sowing
          ? t('播子中…')
          : state?.extra && !over
            ? state.to_move === seat
              ? t('最后一颗落进了大仓，再走一次')
              : t('对方的最后一颗落进了大仓，对方再走一次')
            : state?.captured >= 0 && !over
              ? t('吃子！对面坑里的子收进了大仓')
              : ''}
      </p>
      <BoardAssist
        state={state}
        seat={seat}
        busy={busy || !!sowing}
        over={over}
        onUndo={onUndo}
        hintStatus={hintStatus}
        onHint={onHint}
        hintShown={!!shownHint}
      />
    </div>
  );
};

function mancalaResultNote(state, seat, t) {
  if (!state?.over) return '';
  const [a, b] = state.scores || [0, 0];
  const mine = seat === 1 ? b : a;
  const theirs = seat === 1 ? a : b;
  return t('大仓 {{mine}} 比 {{theirs}}', { mine, theirs });
}

const Mancala = () => {
  const { t } = useTranslation();
  const seatLabels = (state) => {
    if (!state) return [t('待定'), t('待定')];
    return [0, 1].map((seat) => (
      <React.Fragment key={seat}>
        {state.first === seat ? t('先手') : t('后手')}
        <strong className='mancala-seat-score'>
          {state.scores?.[seat] ?? 0}
        </strong>
      </React.Fragment>
    ));
  };
  return (
    <PvpGame
      gameKey='mancala'
      title={t('播棋')}
      className='mancala-page'
      seatLabels={seatLabels}
      resultNote={({ state, seat }) => mancalaResultNote(state, seat, t)}
      rules={[
        t('两边各 6 个坑，每坑 4 颗子；自己的坑在下方，大仓在右边'),
        t('选自己一个有子的坑，把子逆时针一坑一颗播出去(跳过对方大仓)'),
        t('最后一颗落进自己的大仓，再走一次'),
        t('最后一颗落进自己一侧的空坑、对面有子时，连同对面的子一起收进大仓'),
        t('一方的坑全空时结束，另一方收走自己剩下的子，大仓多者胜'),
        t('每局前 3 次悔棋免费，之后每次 1 游戏币；房间里悔棋要对方同意'),
        t('人机对战可以请电脑分析下一步，每局前 3 次提示免费'),
      ]}
      renderBoard={(props) => <MancalaBoard {...props} />}
    />
  );
};

export default Mancala;
