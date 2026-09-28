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
import { Modal } from '@douyinfe/semi-ui';
import { Play, RotateCcw, Send } from 'lucide-react';
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
import './puzzle15.css';

const STATE_URL = '/api/game/puzzle15/state';
// 走过的步骤只在浏览器里：按对局存进 localStorage，刷新页面后从开局盘面重放回来。
const MOVES_KEY = 'game-puzzle15-moves';
// 按方向键时方块往箭头的方向滑，也就是空格往反方向走(U 上、D 下、L 左、R 右是空格的走法)。
const KEY_MOVES = {
  ArrowLeft: 'R',
  ArrowRight: 'L',
  ArrowUp: 'D',
  ArrowDown: 'U',
  a: 'R',
  d: 'L',
  w: 'D',
  s: 'U',
};

// 空格往 direction 走一格之后的盘面，走出盘面时为 null。与服务端重放的规则一致。
function slide(board, size, direction) {
  const blank = board.indexOf(0);
  const x = blank % size;
  const y = Math.floor(blank / size);
  let next = -1;
  if (direction === 'U' && y > 0) next = blank - size;
  if (direction === 'D' && y < size - 1) next = blank + size;
  if (direction === 'L' && x > 0) next = blank - 1;
  if (direction === 'R' && x < size - 1) next = blank + 1;
  if (next < 0) return null;
  const moved = board.slice();
  moved[blank] = moved[next];
  moved[next] = 0;
  return moved;
}

// 按顺序走一串步骤，走不通的那一步之后都不算。返回走完的盘面与实际走了的步骤。
function slideAll(board, size, directions) {
  let current = board;
  let applied = '';
  for (const direction of directions) {
    const next = slide(current, size, direction);
    if (!next) break;
    current = next;
    applied += direction;
  }
  return { board: current, applied };
}

// 排好的盘面：1, 2, …, n²-1 依次排列，空格在最后。
function solvedBoard(size) {
  const count = size * size;
  return Array.from({ length: count }, (_, index) => (index + 1) % count);
}

function isSolved(board) {
  return board.every((tile, index) => tile === (index + 1) % board.length);
}

// 浏览器里存的这一局走过的步骤，只认同一局的。
function savedMoves(sessionId) {
  try {
    const saved = JSON.parse(localStorage.getItem(MOVES_KEY) || 'null');
    return saved?.session_id === sessionId ? saved.moves || '' : '';
  } catch {
    return '';
  }
}

const Puzzle15 = () => {
  const { t } = useTranslation();
  const { loading, unavailable, info, patch, reload } = useGameInfo('puzzle15');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const meta = getGameMetaMap(t).puzzle15;
  const levelNames = getGameLevelNames(t).puzzle15;
  const symbol = info?.symbol || '';
  const levels = info?.levels || [];
  const maxMoves = info?.max_moves || 0;

  const [difficulty, setDifficulty] = useState(1);
  // 进行中或刚结束的一局：{ sessionId, difficulty, size, since }。
  const [game, setGame] = useState(null);
  const [board, setBoard] = useState([]);
  // 空格走过的每一步，排好后整串交给服务端重放。
  const [moves, setMoves] = useState('');
  // 一局结束：{ at }。
  const [end, setEnd] = useState(null);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [checked, setChecked] = useState(false);
  const busyRef = useRef(false);
  const keyRef = useRef(null);

  const restore = useCallback((view) => {
    const replayed = slideAll(
      view.board,
      view.size,
      savedMoves(view.session_id),
    );
    setGame({
      sessionId: view.session_id,
      difficulty: view.difficulty,
      size: view.size,
      since: Date.now() - view.elapsed * 1000,
    });
    setBoard(replayed.board);
    setMoves(replayed.applied);
    setDifficulty(view.difficulty);
    setEnd(null);
    setResult(null);
  }, []);

  const clearBoard = () => {
    setGame(null);
    setBoard([]);
    setMoves('');
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
      MOVES_KEY,
      JSON.stringify({ session_id: sessionId, moves }),
    );
  }, [sessionId, over, moves]);

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

  // 排好之后交给服务端重放；同一局重复提交时服务端回应第一次的结算结果，照样当作结束。
  const submit = async (allMoves) => {
    if (busyRef.current || !game) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      '/api/game/puzzle15/finish',
      { session_id: game.sessionId, moves: allMoves },
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      resync();
      return;
    }
    const settled = response.data;
    setEnd({ at: Date.now() });
    setResult(settled);
    patch({
      balance: settled.balance,
      progress: settled.progress,
      best_score: settled.best_score,
    });
    localStorage.removeItem(MOVES_KEY);
  };

  const start = async () => {
    if (busyRef.current || !turnstile.ready) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      withTurnstile('/api/game/puzzle15/start', turnstile.take()),
      { difficulty },
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      return;
    }
    localStorage.removeItem(MOVES_KEY);
    restore(response.data);
  };

  // 空格按 directions 走，排好了就交卷。
  const move = (directions) => {
    if (!playing || busy || isSolved(board)) return;
    const moved = slideAll(board, game.size, directions);
    if (!moved.applied) return;
    const allMoves = moves + moved.applied;
    setBoard(moved.board);
    setMoves(allMoves);
    if (isSolved(moved.board)) submit(allMoves);
  };

  // 点和空格同一行或同一列的方块，它和空格之间的方块一起滑过去。
  const clickTile = (index) => {
    const { size } = game || {};
    if (!size) return;
    const blank = board.indexOf(0);
    const dx = (index % size) - (blank % size);
    const dy = Math.floor(index / size) - Math.floor(blank / size);
    if (dy === 0 && dx !== 0) {
      move((dx > 0 ? 'R' : 'L').repeat(Math.abs(dx)));
    } else if (dx === 0 && dy !== 0) {
      move((dy > 0 ? 'D' : 'U').repeat(Math.abs(dy)));
    }
  };

  const onKey = (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.target.closest?.('input, textarea, [contenteditable="true"]')) {
      return;
    }
    const direction =
      KEY_MOVES[event.key] || KEY_MOVES[event.key.toLowerCase()];
    if (!direction) return;
    event.preventDefault();
    move(direction);
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

  const confirmRestart = () => {
    if (!moves) {
      clearBoard();
      return;
    }
    Modal.confirm({
      title: t('重新选择难度'),
      content: t('这一局不会结算，开始新的一局后就作废了。'),
      onOk: () => {
        clearBoard();
        setError('');
      },
    });
  };

  const selected = levels.find((level) => level.difficulty === difficulty);
  const size = game?.size || selected?.size || 3;
  const shown = game ? board : solvedBoard(size);
  // 每块方块现在的位置。方块按编号固定顺序渲染，只改位置，滑动才有动画。
  const positions = [];
  shown.forEach((tile, index) => {
    positions[tile] = index;
  });
  const solvedNow = playing && isSolved(board);

  const left = rewardsLeft(info?.rule, info?.progress);
  let leftText = t('今天还能领 {{left}} 次', { left });
  if (info?.rule?.daily_rewards === 0) {
    leftText = t('这个游戏暂不发放奖励');
  } else if (left === 0) {
    leftText = t('今天的领奖次数已用完，还可以继续玩');
  }

  const outcome = end && {
    kind: 'win',
    text: t('排好了，{{moves}} 步，用时 {{time}}', {
      moves: moves.length,
      time: formatClock((end.at - game.since) / 1000),
    }),
  };
  let reward = null;
  if (end && result) {
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
              t('点和空格同一行或同一列的方块，它们会一起滑向空格'),
              t('键盘方向键或 WASD：方块往箭头的方向滑'),
              t('把数字按从小到大排好、空格留在右下角就赢了'),
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
      className='puzzle15-page'
    >
      <div className='games-stage'>
        <div className='game-hud'>
          <span>
            {t('步数')}
            <strong>{moves.length}</strong>
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

        <div
          className={
            playing ? 'puzzle15-board is-playing' : 'puzzle15-board is-idle'
          }
          style={{ '--puzzle15-size': size }}
        >
          {positions.slice(1).map((index, offset) => {
            const tile = offset + 1;
            const home = index === tile - 1;
            return (
              <button
                key={tile}
                type='button'
                tabIndex={playing ? 0 : -1}
                className={home ? 'puzzle15-tile is-home' : 'puzzle15-tile'}
                style={{
                  transform: `translate(${(index % size) * 100}%, ${Math.floor(index / size) * 100}%)`,
                }}
                onClick={() => playing && clickTile(index)}
              >
                <span className='puzzle15-tile-face'>{tile}</span>
              </button>
            );
          })}
        </div>

        {!game && checked && (
          <div className='games-stage-counter puzzle15-preview'>
            {t('开始前这里是排好的样子，开始后打乱')}
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
        {playing && maxMoves > 0 && moves.length > maxMoves && (
          <StageMessage kind='error'>
            {t('步数超过 {{max}}，这一局没法结算了，请重新开局', {
              max: maxMoves,
            })}
          </StageMessage>
        )}
        {error && <StageMessage kind='error'>{error}</StageMessage>}

        {playing && (
          <div className='game-controls'>
            {solvedNow && (
              <button
                type='button'
                className='game-chip'
                disabled={busy}
                onClick={() => submit(moves)}
              >
                <Send size={14} className='puzzle15-chip-icon' />
                {busy ? t('正在结算…') : t('交卷')}
              </button>
            )}
            <button
              type='button'
              className='game-chip'
              disabled={busy}
              onClick={confirmRestart}
            >
              <RotateCcw size={14} className='puzzle15-chip-icon' />
              {t('重新选择难度')}
            </button>
          </div>
        )}

        {!playing && checked && (
          <div className='game-controls puzzle15-start'>
            <p className='puzzle15-summary'>
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
                    <span className='puzzle15-chip-reward'>
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
              <Play size={16} className='puzzle15-play-icon' />
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

export default Puzzle15;
