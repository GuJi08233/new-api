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
import { Button, Modal, Select } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import {
  playSkillFeedback,
  SkillSide,
  skillResultMessage,
} from '../common/skill';
import { LocalBestBox, localBestNote } from '../common/LocalBest';
import { useLocalBest } from '../common/useLocalBest';
import { Floaters, useFloaters } from '../common/effects';
import { playSound } from '../common/sound';
import { useStepPuzzle } from '../common/useStepPuzzle';
import { PlayTurnstile } from '../common/usePlayTurnstile';
import {
  SIZE,
  blasts,
  colorOf,
  fallDistances,
  kindOf,
  swapOf,
  wouldMatch,
} from './logic';
import '../common/logic-puzzles.css';
import './match3.css';

// 动画各段的时长(毫秒)，与 match3.css 一致。
const SWAP_MS = 180;
const CLEAR_MS = 260;
const FALL_MS = 300;
const SWIPE_PX = 18;

const reducedMotion = () =>
  window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;

const wait = (ms, run) =>
  new Promise((resolve) => {
    run.timers.push(setTimeout(resolve, ms));
  });

// 连锁轮数对应的喝彩。
const cheerOf = (chain, t) =>
  chain >= 5
    ? t('无与伦比！')
    : chain >= 4
      ? t('太精彩了！')
      : chain >= 3
        ? t('漂亮！')
        : t('连锁！');

export default function Match3() {
  const { t } = useTranslation();
  const names = [t('简单'), t('普通'), t('困难')];
  const { bests, record } = useLocalBest('match3');
  const [floaters, addFloater] = useFloaters();
  const [selected, setSelected] = useState(-1);
  // 正在播放的动画帧：盘面、正在消失的格子、每格下落的格数、光束与喝彩。为空时显示服务端的盘面。
  const [frame, setFrame] = useState(null);
  const [shownScore, setShownScore] = useState(null);
  const [finish, setFinish] = useState(null);
  const runRef = useRef(null);
  const swipeRef = useRef(null);

  const stopRun = () => {
    const run = runRef.current;
    if (!run) return;
    run.cancelled = true;
    run.timers.forEach(clearTimeout);
    runRef.current = null;
  };
  useEffect(() => stopRun, []);

  // 把一次交换引起的各轮消除依次播放出来：先换位，再一轮轮爆开、落下；播完换回服务端的盘面。
  const animate = async (before, next) => {
    stopRun();
    const run = { timers: [], cancelled: false };
    runRef.current = run;
    const swap = next.swap;
    const other = swap.dir === 0 ? swap.cell + 1 : swap.cell + SIZE;
    let board = before.slice();
    [board[swap.cell], board[other]] = [board[other], board[swap.cell]];
    let score = next.score - next.phases.reduce((n, p) => n + p.gained, 0);
    setShownScore(score);
    setFrame({ board, swapped: [swap.cell, other], dir: swap.dir, step: 0 });
    playSound('move');
    await wait(SWAP_MS, run);
    for (const [index, phase] of next.phases.entries()) {
      if (run.cancelled) return;
      const beams = blasts(board, phase.cleared);
      setFrame({
        board,
        clearing: new Set(phase.cleared),
        beams,
        chain: phase.chain,
        step: index * 2 + 1,
      });
      playSound(phase.chain > 1 ? 'combo' : 'clear', {
        pitch: 1 + (phase.chain - 1) * 0.12,
      });
      if (beams.length)
        playSound(beams.some((b) => b.kind === 3) ? 'bomb' : 'whoosh');
      const center = phase.cleared.reduce(
        (acc, cell) => [
          acc[0] + (cell % SIZE),
          acc[1] + Math.floor(cell / SIZE),
        ],
        [0, 0],
      );
      const count = phase.cleared.length || 1;
      addFloater(`+${phase.gained}`, {
        x: ((center[0] / count + 0.5) / SIZE) * 100,
        y: ((center[1] / count + 0.5) / SIZE) * 100,
        kind: phase.chain > 1 ? 'gold' : 'gain',
      });
      if (phase.created.length) playSound('sparkle');
      await wait(CLEAR_MS, run);
      if (run.cancelled) return;
      score += phase.gained;
      setShownScore(score);
      const drops = fallDistances(board, phase.cleared, phase.created);
      board = phase.board;
      setFrame({
        board,
        drops,
        created: new Set(phase.created.map((c) => c.cell)),
        chain: phase.chain,
        step: index * 2 + 2,
      });
      playSound('drop', { pitch: 1.2 });
      await wait(FALL_MS, run);
    }
    if (run.cancelled) return;
    if (next.shuffled) {
      playSound('rotate');
      setFrame({ board: next.board, shuffling: true, step: 99 });
      await wait(500, run);
    }
    if (run.cancelled) return;
    runRef.current = null;
    setFrame(null);
    setShownScore(null);
  };

  const {
    info,
    loading,
    unavailable,
    turnstile,
    round,
    playing,
    difficulty,
    setDifficulty,
    busy,
    checked,
    error,
    send,
    restore,
    elapsedSeconds,
  } = useStepPuzzle('match3', {
    onStep: (current, next) => {
      const sameRound =
        current && next && current.session_id === next.session_id;
      if (!sameRound) {
        stopRun();
        setFrame(null);
        setShownScore(null);
        if (next?.status === 'playing') playSound('start');
        return;
      }
      if (
        next.moves === current.moves + 1 &&
        next.swap &&
        next.phases?.length &&
        !reducedMotion()
      )
        animate(current.board, next);
      if (current.status !== 'playing' || !next.result) return;
      const won = !!next.won;
      const seconds = elapsedSeconds(next.session_id);
      setFinish(
        won
          ? {
              session: next.session_id,
              moves: next.moves,
              improved: record(next.difficulty, {
                moves: next.moves,
                time: seconds > 0 ? seconds : undefined,
              }),
            }
          : null,
      );
      playSkillFeedback(next.result, won);
    },
  });

  useEffect(() => setSelected(-1), [round?.session_id]);

  const board = frame?.board || round?.board || [];
  const animating = !!frame;
  const score = shownScore ?? round?.score ?? 0;
  const target = round?.target || 0;
  const progress = target ? Math.min(1, score / target) : 0;

  const trySwap = (a, b) => {
    const swap = swapOf(a, b);
    setSelected(-1);
    if (!swap) return;
    if (!wouldMatch(round.board, a, b)) {
      playSound('error');
      setFrame({
        board: round.board,
        denied: [a, b],
        step: -1,
      });
      stopRun();
      const run = { timers: [], cancelled: false };
      runRef.current = run;
      wait(320, run).then(() => {
        if (run.cancelled) return;
        runRef.current = null;
        setFrame(null);
      });
      return;
    }
    send('move', swap);
  };

  const canPlay = playing && !busy && !animating;
  const press = (cell) => {
    if (!canPlay) return;
    if (selected < 0) {
      playSound('tick');
      setSelected(cell);
      return;
    }
    if (selected === cell) {
      setSelected(-1);
      return;
    }
    if (!swapOf(selected, cell)) {
      playSound('tick');
      setSelected(cell);
      return;
    }
    trySwap(selected, cell);
  };

  const onPointerDown = (event, cell) => {
    if (!canPlay) return;
    swipeRef.current = { cell, x: event.clientX, y: event.clientY };
  };
  const onPointerMove = (event) => {
    const swipe = swipeRef.current;
    if (!swipe || !canPlay) return;
    const dx = event.clientX - swipe.x;
    const dy = event.clientY - swipe.y;
    if (Math.max(Math.abs(dx), Math.abs(dy)) < SWIPE_PX) return;
    swipeRef.current = null;
    const row = Math.floor(swipe.cell / SIZE);
    const col = swipe.cell % SIZE;
    let target = -1;
    if (Math.abs(dx) > Math.abs(dy)) {
      if (dx > 0 && col < SIZE - 1) target = swipe.cell + 1;
      if (dx < 0 && col > 0) target = swipe.cell - 1;
    } else {
      if (dy > 0 && row < SIZE - 1) target = swipe.cell + SIZE;
      if (dy < 0 && row > 0) target = swipe.cell - SIZE;
    }
    if (target >= 0) trySwap(swipe.cell, target);
  };
  const onPointerUp = (cell) => {
    if (!swipeRef.current) return;
    swipeRef.current = null;
    press(cell);
  };

  const confirm = () =>
    Modal.confirm({
      title: t('结束本局'),
      content: t('结束后按当前成绩结算，不能继续。'),
      onOk: () => send('finish'),
    });
  const shownFinish =
    finish && finish.session === round?.session_id ? finish : null;
  const result =
    round?.result &&
    skillResultMessage(round.result, t, info?.symbol, { byLevel: true });
  const levels = info?.levels;

  return (
    <GameLayout
      title={t('宝石消除')}
      loading={loading}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={info?.symbol}
      side={
        info && (
          <SkillSide
            info={info}
            symbol={info.symbol}
            scoreName={t('难度')}
            levelNames={names}
            extra={
              <SideBox label={t('操作说明')}>
                <RuleList
                  items={[
                    t('点一颗宝石再点相邻的一颗，或者直接朝相邻方向滑动来交换'),
                    t('交换后连成三颗及以上同色才算数，消掉后上方宝石落下'),
                    t('落下后又连上会连锁，每多一轮连锁得分倍数加一'),
                    t('四连留下直线宝石，消掉时清空整行或整列'),
                    t('五连或 L、T 形留下炸弹，消掉时炸开周围 3×3'),
                    t('步数用完之前分数达到目标就过关；没有可消的步时自动洗牌'),
                  ]}
                />
              </SideBox>
            }
            after={
              <LocalBestBox
                bests={bests}
                levels={names.map((name, index) => ({ key: index + 1, name }))}
                columns={['moves']}
              />
            }
          />
        )
      }
    >
      <div className='games-stage logic-stage match3-stage'>
        <div className='logic-heading'>
          <span className='logic-eyebrow'>{t('宝石矿洞')}</span>
          <h2>{t('宝石消除')}</h2>
          <p>{t('连成一线，引爆连锁')}</p>
        </div>
        <div className='logic-stats'>
          <span>
            {t('分数')}
            <strong
              className={animating ? 'match3-score is-rolling' : 'match3-score'}
            >
              {score}
            </strong>
          </span>
          <span>
            {t('目标')}
            <strong>{target || '-'}</strong>
          </span>
          <span>
            {t('剩余步数')}
            <strong
              className={
                playing && round?.moves_left <= 3
                  ? 'match3-moves is-low'
                  : 'match3-moves'
              }
            >
              {round ? round.moves_left : '-'}
            </strong>
          </span>
        </div>
        <div className='match3-progress' aria-hidden>
          <span style={{ width: `${progress * 100}%` }} />
        </div>
        <div className='match3-board-wrap'>
          <div
            className={`match3-board ${round?.won ? 'is-won' : ''} ${frame?.shuffling ? 'is-shuffling' : ''}`}
            role='grid'
            aria-label={t('宝石棋盘')}
            onPointerMove={onPointerMove}
            onPointerLeave={() => {
              swipeRef.current = null;
            }}
          >
            {board.map((gem, cell) => {
              const classes = ['match3-gem', `is-c${colorOf(gem)}`];
              const kind = kindOf(gem);
              if (kind === 1) classes.push('is-row');
              if (kind === 2) classes.push('is-col');
              if (kind === 3) classes.push('is-bomb');
              if (selected === cell) classes.push('is-selected');
              if (frame?.clearing?.has(cell)) classes.push('is-clearing');
              if (frame?.created?.has(cell)) classes.push('is-created');
              if (frame?.denied?.includes(cell)) classes.push('is-denied');
              const drop = frame?.drops?.[cell] || 0;
              if (drop) classes.push('is-falling');
              let style;
              if (frame?.swapped?.includes(cell)) {
                const moved = cell === frame.swapped[0] ? 1 : -1;
                const axis = frame.dir === 0 ? 'X' : 'Y';
                style = {
                  '--swap-from': `translate${axis}(calc(${moved} * (131.6% + var(--m3-gap))))`,
                };
                classes.push('is-swapping');
              } else if (drop) {
                style = { '--drop': drop };
              }
              return (
                <button
                  key={`${frame?.step ?? 'r'}-${cell}`}
                  type='button'
                  className={classes.join(' ')}
                  style={style}
                  disabled={!playing}
                  aria-label={t('第 {{row}} 行第 {{col}} 列', {
                    row: Math.floor(cell / SIZE) + 1,
                    col: (cell % SIZE) + 1,
                  })}
                  onPointerDown={(event) => onPointerDown(event, cell)}
                  onPointerUp={() => onPointerUp(cell)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      press(cell);
                    }
                  }}
                >
                  <span className='match3-jewel' />
                </button>
              );
            })}
            {frame?.beams?.map((beam) => (
              <span
                key={`${frame.step}-${beam.cell}`}
                className={`match3-beam is-k${beam.kind}`}
                style={{
                  '--row': Math.floor(beam.cell / SIZE),
                  '--col': beam.cell % SIZE,
                }}
                aria-hidden
              />
            ))}
            <Floaters items={floaters} />
          </div>
          {frame?.chain > 1 && frame?.clearing && (
            <div key={frame.step} className='match3-cheer' aria-hidden>
              <strong>{cheerOf(frame.chain, t)}</strong>
              <span>×{frame.chain}</span>
            </div>
          )}
        </div>
        {playing && (
          <p className='logic-help' aria-live='polite'>
            {busy || animating
              ? t('宝石下落中…')
              : t('点选或滑动相邻的宝石来交换')}
          </p>
        )}
        <div className='logic-controls'>
          {playing ? (
            <Button disabled={busy || animating} onClick={confirm}>
              {t('结束本局')}
            </Button>
          ) : (
            <>
              <Select
                value={difficulty}
                disabled={busy}
                onChange={setDifficulty}
                aria-label={t('难度')}
                optionList={names.map((name, index) => ({
                  label: levels?.[index + 1]
                    ? `${name} · ${levels[index + 1].target}`
                    : name,
                  value: index + 1,
                }))}
              />
              <Button
                theme='solid'
                loading={busy}
                disabled={!turnstile.ready || !checked}
                onClick={() => send('start')}
              >
                {round ? t('再来一局') : t('开始游戏')}
              </Button>
            </>
          )}
        </div>
        {!playing && <PlayTurnstile turnstile={turnstile} />}
        {shownFinish && (
          <div className='logic-finish' role='status'>
            <span>
              {t('达成目标，用了 {{moves}} 步', { moves: shownFinish.moves })}{' '}
              {localBestNote(shownFinish.improved, t)}
            </span>
          </div>
        )}
        {result && (
          <StageMessage kind={result.kind} note={result.note}>
            {result.text}
          </StageMessage>
        )}
        {error && (
          <>
            <StageMessage kind='error'>{error}</StageMessage>
            <Button disabled={busy} onClick={() => restore(round?.session_id)}>
              {t('重新读取对局')}
            </Button>
          </>
        )}
      </div>
    </GameLayout>
  );
}
