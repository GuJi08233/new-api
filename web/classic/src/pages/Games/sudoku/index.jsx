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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { Modal } from '@douyinfe/semi-ui';
import { Eraser, Flag, PencilLine, Play, Send } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameLevelNames, getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gameGet, gamePost, withTurnstile } from '../common/api';
import {
  formatClock,
  GameClock,
  levelReward,
  rewardsLeft,
  skillResultMessage,
  skillRuleSummary,
  SkillSide,
} from '../common/skill';
import { useGameInfo } from '../common/useGameInfo';
import { usePlayTurnstile } from '../common/usePlayTurnstile';
import './sudoku.css';

const STATE_URL = '/api/game/sudoku/state';
// 填到一半的数字和笔记只在浏览器里：按对局存进 localStorage，刷新页面后接着填。
const PROGRESS_KEY = 'game-sudoku-progress';
const DIGITS = [1, 2, 3, 4, 5, 6, 7, 8, 9];
const EMPTY_GRID = new Array(81).fill(0);
const ARROW_STEPS = { ArrowUp: -9, ArrowDown: 9, ArrowLeft: -1, ArrowRight: 1 };

// 每一格同一行、同一列、同一宫的其他 20 格。
const PEERS = Array.from({ length: 81 }, (_, index) => {
  const row = Math.floor(index / 9);
  const col = index % 9;
  const boxRow = row - (row % 3);
  const boxCol = col - (col % 3);
  const peers = new Set();
  for (let i = 0; i < 9; i++) {
    peers.add(row * 9 + i);
    peers.add(i * 9 + col);
    peers.add((boxRow + Math.floor(i / 3)) * 9 + boxCol + (i % 3));
  }
  peers.delete(index);
  return [...peers];
});

// 与同行、同列或同宫的某一格填了同一个数字的格子。盘面是公开的，冲突在本地就能看出来。
function conflictsOf(values) {
  const conflicts = new Set();
  values.forEach((value, index) => {
    if (value && PEERS[index].some((peer) => values[peer] === value)) {
      conflicts.add(index);
    }
  });
  return conflicts;
}

// 浏览器里存的这一局的进度，只认同一局的。
function savedProgress(sessionId) {
  try {
    const saved = JSON.parse(localStorage.getItem(PROGRESS_KEY) || 'null');
    return saved?.session_id === sessionId ? saved : null;
  } catch {
    return null;
  }
}

const Sudoku = () => {
  const { t } = useTranslation();
  const { loading, unavailable, info, patch, reload } = useGameInfo('sudoku');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const meta = getGameMetaMap(t).sudoku;
  const levelNames = getGameLevelNames(t).sudoku;
  const symbol = info?.symbol || '';
  const levels = info?.levels || [];

  const [difficulty, setDifficulty] = useState(1);
  // 进行中或刚结束的一局：{ sessionId, difficulty, puzzle(81 个字符，'0' 是空格), since }。
  const [game, setGame] = useState(null);
  const [values, setValues] = useState(EMPTY_GRID);
  // 每格的笔记，第 d 位表示记下了数字 d。
  const [notes, setNotes] = useState(EMPTY_GRID);
  const [selected, setSelected] = useState(null);
  const [notesMode, setNotesMode] = useState(false);
  // 一局结束：{ status: solved | abandoned, at }。
  const [end, setEnd] = useState(null);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [checked, setChecked] = useState(false);
  const busyRef = useRef(false);
  const keyRef = useRef(null);

  const restore = useCallback((view) => {
    const given = [...view.puzzle].map(Number);
    const saved = savedProgress(view.session_id);
    // 题目给出的数字总以服务端为准，存下来的只补空格。
    const filled = given.map(
      (digit, index) => digit || Number(saved?.values?.[index]) || 0,
    );
    setGame({
      sessionId: view.session_id,
      difficulty: view.difficulty,
      puzzle: view.puzzle,
      since: Date.now() - view.elapsed * 1000,
    });
    setValues(filled);
    setNotes(saved?.notes?.length === 81 ? saved.notes : EMPTY_GRID);
    setSelected(null);
    setDifficulty(view.difficulty);
    setEnd(null);
    setResult(null);
  }, []);

  const clearBoard = () => {
    setGame(null);
    setValues(EMPTY_GRID);
    setNotes(EMPTY_GRID);
    setSelected(null);
    setEnd(null);
  };

  // 刷新页面后接着玩进行中的一局。
  useEffect(() => {
    let alive = true;
    gameGet(STATE_URL, t).then((response) => {
      if (!alive) return;
      if (response.data?.session) restore(response.data.session);
      setChecked(true);
    });
    return () => {
      alive = false;
    };
  }, [restore, t]);

  const sessionId = game?.sessionId;
  const over = !!end;
  const playing = !!game && !end;
  useEffect(() => {
    if (!sessionId || over) return;
    localStorage.setItem(
      PROGRESS_KEY,
      JSON.stringify({ session_id: sessionId, values: values.join(''), notes }),
    );
  }, [sessionId, over, values, notes]);

  const conflicts = useMemo(() => conflictsOf(values), [values]);
  const full = values.every(Boolean);
  const puzzle = game?.puzzle || '';
  const isGiven = (index) => !!puzzle[index] && puzzle[index] !== '0';

  const settle = (settled) => {
    setEnd({
      status: settled.detail?.solved ? 'solved' : 'abandoned',
      at: Date.now(),
    });
    setResult(settled);
    setSelected(null);
    patch({
      balance: settled.balance,
      progress: settled.progress,
      best_score: settled.best_score,
    });
    localStorage.removeItem(PROGRESS_KEY);
  };

  // 对局已被新开的一局作废、或者网络中断之后，重新读取进行中的一局。
  const resync = async () => {
    const response = await gameGet(STATE_URL, t);
    if (response.data?.session) {
      if (response.data.session.session_id !== sessionId) {
        restore(response.data.session);
      }
    } else if (response.data) {
      clearBoard();
    }
    reload(true);
  };

  // 交卷与放弃都结算这一局；同一局重复提交时服务端回应第一次的结算结果，照样当作结束。
  const send = async (url, body) => {
    if (busyRef.current || !game) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      url,
      { session_id: game.sessionId, ...body },
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      resync();
      return;
    }
    settle(response.data);
  };

  const start = async () => {
    if (busyRef.current || !turnstile.ready) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      withTurnstile('/api/game/sudoku/start', turnstile.take()),
      { difficulty },
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      return;
    }
    localStorage.removeItem(PROGRESS_KEY);
    restore(response.data);
    setNotesMode(false);
  };

  // 在选中的格子里填数字(0 是擦掉)。笔记模式下切换这个数字的笔记；填上数字时同行同列同宫里这个数字的笔记一并去掉。
  // 填满且没有冲突就是解出来了，直接交卷。
  const write = (digit) => {
    if (!playing || busy || selected === null || isGiven(selected)) return;
    if (notesMode && digit > 0) {
      if (values[selected]) return;
      setNotes((previous) => {
        const next = previous.slice();
        next[selected] ^= 1 << digit;
        return next;
      });
      return;
    }
    const next = values.slice();
    next[selected] = digit;
    setValues(next);
    setNotes((previous) => {
      const nextNotes = previous.slice();
      nextNotes[selected] = 0;
      if (digit > 0) {
        PEERS[selected].forEach((peer) => {
          nextNotes[peer] &= ~(1 << digit);
        });
      }
      return nextNotes;
    });
    if (digit > 0 && next.every(Boolean) && conflictsOf(next).size === 0) {
      send('/api/game/sudoku/finish', { grid: next.join('') });
    }
  };

  const confirmAbandon = () => {
    Modal.confirm({
      title: t('放弃这一局'),
      content: t('放弃后这一局按没有解出来结算，不发奖励。'),
      onOk: () => send('/api/game/sudoku/abandon', {}),
    });
  };

  // 键盘：数字填数，退格、Delete 或 0 擦掉，方向键移动，N 切换笔记模式。
  const onKey = (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.target.closest?.('input, textarea, [contenteditable="true"]')) {
      return;
    }
    if (/^[1-9]$/.test(event.key)) {
      event.preventDefault();
      write(Number(event.key));
    } else if (['Backspace', 'Delete', '0'].includes(event.key)) {
      event.preventDefault();
      write(0);
    } else if (ARROW_STEPS[event.key]) {
      event.preventDefault();
      const step = ARROW_STEPS[event.key];
      const current = selected ?? 40;
      const moved = current + step;
      // 左右移动不跨行。
      const sameRow =
        Math.abs(step) === 9 ||
        Math.floor(moved / 9) === Math.floor(current / 9);
      if (selected === null) {
        setSelected(current);
      } else if (moved >= 0 && moved < 81 && sameRow) {
        setSelected(moved);
      }
    } else if (event.key === 'n' || event.key === 'N') {
      event.preventDefault();
      setNotesMode((previous) => !previous);
    }
  };
  useEffect(() => {
    keyRef.current = onKey;
  });
  useEffect(() => {
    if (!playing) return undefined;
    const onKeyDown = (event) => keyRef.current(event);
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [playing]);

  const selectedValue = selected !== null ? values[selected] : 0;
  const placed = DIGITS.map(
    (digit) => values.filter((value) => value === digit).length,
  );

  const cellClass = (value, index) => {
    const row = Math.floor(index / 9);
    const col = index % 9;
    const classes = ['sudoku-cell'];
    if (col % 3 === 2 && col < 8) classes.push('is-box-right');
    if (row % 3 === 2 && row < 8) classes.push('is-box-bottom');
    if (isGiven(index)) classes.push('is-given');
    if (index === selected) {
      classes.push('is-selected');
    } else if (selected !== null && PEERS[selected].includes(index)) {
      classes.push('is-peer');
    }
    if (value && value === selectedValue && index !== selected) {
      classes.push('is-same');
    }
    if (conflicts.has(index)) classes.push('is-conflict');
    return classes.join(' ');
  };

  const left = rewardsLeft(info?.rule, info?.progress);
  let leftText = t('今天还能领 {{left}} 次', { left });
  if (info?.rule?.daily_rewards === 0) {
    leftText = t('这个游戏暂不发放奖励');
  } else if (left === 0) {
    leftText = t('今天的领奖次数已用完，还可以继续玩');
  }

  let outcome = null;
  if (end?.status === 'solved') {
    outcome = {
      kind: 'win',
      text: t('解出来了，用时 {{time}}', {
        time: formatClock((end.at - game.since) / 1000),
      }),
    };
  } else if (end) {
    outcome = { kind: '', text: t('已放弃这一局') };
  }
  let reward = null;
  if (end?.status === 'solved' && result) {
    reward = skillResultMessage(result, t, symbol, { byLevel: true });
  }

  const side = info && (
    <SkillSide
      info={info}
      symbol={symbol}
      scoreName={meta.scoreName}
      levelNames={levelNames}
      extra={
        <SideBox label={t('操作说明')}>
          <RuleList
            items={[
              t('选中一格，用下方数字键或键盘 1-9 填数，退格键擦掉'),
              t('打开笔记模式(键盘 N)可以在格子里记下候选数字'),
              t('同行、同列或同宫里重复的数字标成红色'),
              t('填满且没有冲突就自动交卷'),
            ]}
          />
        </SideBox>
      }
    />
  );

  return (
    <GameLayout
      title={meta.name}
      loading={loading}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={symbol}
      side={side}
      className='sudoku-page'
    >
      <div className='games-stage'>
        <div className='game-hud'>
          <span>
            {t('难度')}
            <strong>{levelNames[(game?.difficulty || difficulty) - 1]}</strong>
          </span>
          <span>
            {t('用时')}
            <strong>
              {game ? (
                <GameClock since={game.since} stoppedAt={end?.at} />
              ) : (
                formatClock(0)
              )}
            </strong>
          </span>
        </div>

        <div className={playing ? 'sudoku-board is-playing' : 'sudoku-board'}>
          {values.map((value, index) => (
            <button
              key={index}
              type='button'
              tabIndex={playing ? 0 : -1}
              className={cellClass(value, index)}
              onClick={() => playing && setSelected(index)}
            >
              {value > 0 && value}
              {value === 0 && notes[index] > 0 && (
                <span className='sudoku-notes'>
                  {DIGITS.map((digit) => (
                    <span key={digit}>
                      {notes[index] & (1 << digit) ? digit : ''}
                    </span>
                  ))}
                </span>
              )}
            </button>
          ))}
        </div>

        {playing && (
          <div className='sudoku-pad'>
            {DIGITS.map((digit) => (
              <button
                key={digit}
                type='button'
                className={
                  placed[digit - 1] >= 9
                    ? 'sudoku-pad-key is-done'
                    : 'sudoku-pad-key'
                }
                disabled={busy}
                onClick={() => write(digit)}
              >
                {digit}
              </button>
            ))}
          </div>
        )}

        {outcome && (
          <StageMessage kind={outcome.kind}>{outcome.text}</StageMessage>
        )}
        {reward && (
          <StageMessage kind={reward.kind} note={reward.note}>
            {reward.text}
          </StageMessage>
        )}
        {playing && full && conflicts.size > 0 && (
          <StageMessage kind='error'>
            {t('填满了，但还有标红的冲突')}
          </StageMessage>
        )}
        {error && <StageMessage kind='error'>{error}</StageMessage>}

        {playing && (
          <div className='game-controls'>
            <button
              type='button'
              className='game-chip'
              disabled={busy}
              onClick={() => write(0)}
            >
              <Eraser size={14} className='sudoku-chip-icon' />
              {t('擦除')}
            </button>
            <button
              type='button'
              className={notesMode ? 'game-chip is-active' : 'game-chip'}
              aria-pressed={notesMode}
              onClick={() => setNotesMode((previous) => !previous)}
            >
              <PencilLine size={14} className='sudoku-chip-icon' />
              {t('笔记')}
            </button>
            {full && (
              <button
                type='button'
                className='game-chip'
                disabled={busy || conflicts.size > 0}
                onClick={() =>
                  send('/api/game/sudoku/finish', { grid: values.join('') })
                }
              >
                <Send size={14} className='sudoku-chip-icon' />
                {busy ? t('正在结算…') : t('交卷')}
              </button>
            )}
            <button
              type='button'
              className='game-chip'
              disabled={busy}
              onClick={confirmAbandon}
            >
              <Flag size={14} className='sudoku-chip-icon' />
              {t('放弃这一局')}
            </button>
          </div>
        )}

        {!playing && checked && (
          <div className='game-controls sudoku-start'>
            <p className='sudoku-summary'>
              {skillRuleSummary(info, t, symbol, meta.scoreName, levelNames)}
            </p>
            <div className='game-chips'>
              {levels.map((level) => {
                const coins = levelReward(info?.rule?.tiers, level.difficulty);
                return (
                  <button
                    key={level.difficulty}
                    type='button'
                    className={
                      level.difficulty === difficulty
                        ? 'game-chip is-active'
                        : 'game-chip'
                    }
                    onClick={() => setDifficulty(level.difficulty)}
                  >
                    {levelNames[level.difficulty - 1]}
                    <span className='sudoku-chip-note'>
                      {t('给出 {{min}}-{{max}} 个数', {
                        min: level.min_givens,
                        max: level.max_givens,
                      })}
                    </span>
                    <span className='sudoku-chip-reward'>
                      {coins > 0 ? `+${coins} ${symbol}` : t('不发奖')}
                    </span>
                  </button>
                );
              })}
            </div>
            <button
              type='button'
              className='games-play-button is-free'
              disabled={busy || !turnstile.ready}
              onClick={start}
            >
              <Play size={16} className='sudoku-play-icon' />
              {busy ? t('正在开局…') : end ? t('再来一局') : t('开始游戏')}
            </button>
            {turnstile.widget}
            {turnstile.failed && (
              <div className='games-stage-hint'>
                {t('人机验证加载失败，请刷新页面重试')}
              </div>
            )}
            <div className='games-stage-counter'>{leftText}</div>
          </div>
        )}
      </div>
    </GameLayout>
  );
};

export default Sudoku;
