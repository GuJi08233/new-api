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
import { Bomb, Flag, Play, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameLevelNames, getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gameGet, gamePost, withTurnstile } from '../common/api';
import {
  SkillSide,
  rewardsLeft,
  skillResultMessage,
  skillRuleSummary,
} from '../common/skill';
import { useGameInfo } from '../common/useGameInfo';
import { usePlayTurnstile } from '../common/usePlayTurnstile';
import './minesweeper.css';

const STATE_URL = '/api/game/minesweeper/state';
// 旗子只是玩家自己的记号，服务端不存：按对局存在浏览器里，刷新页面后接着用。
const FLAGS_KEY = 'game-minesweeper-flags';
const HIDDEN = -1;
const LONG_PRESS_MS = 450;
// 长按插旗之后紧跟着的点击与系统菜单不再算数。
const PRESS_GUARD_MS = 600;

// 难度的奖励：成绩达到的最高一档，与服务端的领奖规则一致。
function levelReward(tiers, level) {
  let coins = 0;
  for (const tier of tiers || []) {
    if (level >= tier.score) coins = tier.coins;
  }
  return coins;
}

// 用时写成 分:秒，一小时以上带上小时。
function formatClock(seconds) {
  const total = Math.max(0, Math.floor(seconds));
  const minutes = String(Math.floor((total % 3600) / 60)).padStart(2, '0');
  const rest = String(total % 60).padStart(2, '0');
  const hours = Math.floor(total / 3600);
  return hours > 0 ? `${hours}:${minutes}:${rest}` : `${minutes}:${rest}`;
}

// 一格周围在雷区之内的格子。
function neighbors(index, width, height) {
  const x = index % width;
  const y = Math.floor(index / width);
  const around = [];
  for (let dy = -1; dy <= 1; dy++) {
    for (let dx = -1; dx <= 1; dx++) {
      const nx = x + dx;
      const ny = y + dy;
      if ((dx || dy) && nx >= 0 && nx < width && ny >= 0 && ny < height) {
        around.push(ny * width + nx);
      }
    }
  }
  return around;
}

// 浏览器里存的旗子，只认同一局的。
function savedFlags(sessionId) {
  try {
    const saved = JSON.parse(localStorage.getItem(FLAGS_KEY) || 'null');
    return saved?.session_id === sessionId ? saved.flags : [];
  } catch {
    return [];
  }
}

// 对局用时，每秒走一格；stoppedAt 不为空时停在那一刻。since 是本地时钟上的开局时刻。
const GameClock = ({ since, stoppedAt }) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (stoppedAt) return undefined;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [stoppedAt]);
  return formatClock(((stoppedAt || now) - since) / 1000);
};

const Minesweeper = () => {
  const { t } = useTranslation();
  const { loading, unavailable, info, patch, reload } =
    useGameInfo('minesweeper');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const meta = getGameMetaMap(t).minesweeper;
  const levelNames = getGameLevelNames(t).minesweeper;
  const symbol = info?.symbol || '';
  const levels = info?.levels || [];

  const [difficulty, setDifficulty] = useState(1);
  // 进行中或刚结束的一局：{ sessionId, difficulty, width, height, mines, since }。
  const [game, setGame] = useState(null);
  // 每格周围的雷数，没翻开为 HIDDEN。
  const [cells, setCells] = useState([]);
  const [flags, setFlags] = useState(() => new Set());
  // 一局结束：{ status, mines(雷所在的格子), exploded(踩到的那一格), at(结束时刻) }。
  const [end, setEnd] = useState(null);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [checked, setChecked] = useState(false);
  const [flagMode, setFlagMode] = useState(false);
  const busyRef = useRef(false);
  // 最新的局面版本：收到回应之后、页面重绘之前的点击也要带上它。
  const versionRef = useRef(0);
  const pressRef = useRef(null);
  const guardUntilRef = useRef(0);

  const restore = useCallback((view) => {
    const next = new Array(view.width * view.height).fill(HIDDEN);
    for (const [x, y, count] of view.revealed) next[y * view.width + x] = count;
    versionRef.current = view.version;
    setGame({
      sessionId: view.session_id,
      difficulty: view.difficulty,
      width: view.width,
      height: view.height,
      mines: view.mines,
      since: Date.now() - view.elapsed * 1000,
    });
    setCells(next);
    setFlags(
      new Set(
        savedFlags(view.session_id).filter((index) => next[index] === HIDDEN),
      ),
    );
    setDifficulty(view.difficulty);
    setEnd(null);
    setResult(null);
  }, []);

  const clearBoard = () => {
    setGame(null);
    setCells([]);
    setFlags(new Set());
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
  useEffect(() => {
    if (!sessionId || over) return;
    localStorage.setItem(
      FLAGS_KEY,
      JSON.stringify({ session_id: sessionId, flags: [...flags] }),
    );
  }, [sessionId, over, flags]);

  useEffect(
    () => () => {
      if (pressRef.current) clearTimeout(pressRef.current.timer);
    },
    [],
  );

  const settle = (settled, finished) => {
    setEnd({ ...finished, at: Date.now() });
    setResult(settled);
    patch({
      balance: settled.balance,
      progress: settled.progress,
      best_score: settled.best_score,
    });
    localStorage.removeItem(FLAGS_KEY);
  };

  // 局面和服务端对不上时(版本冲突、网络中断、这局已被新开的一局作废)重新读取进行中的一局。
  const resync = async () => {
    const response = await gameGet(STATE_URL, t);
    if (response.data?.session) {
      restore(response.data.session);
    } else if (response.data) {
      clearBoard();
    }
    reload(true);
  };

  const start = async () => {
    if (busyRef.current || !turnstile.ready) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      withTurnstile('/api/game/minesweeper/start', turnstile.take()),
      { difficulty },
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      return;
    }
    localStorage.removeItem(FLAGS_KEY);
    restore(response.data);
    setFlagMode(false);
  };

  const reveal = async (targets) => {
    if (busyRef.current || !game || end) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const { width } = game;
    const response = await gamePost(
      '/api/game/minesweeper/reveal',
      {
        session_id: game.sessionId,
        version: versionRef.current,
        cells: targets.map((index) => [
          index % width,
          Math.floor(index / width),
        ]),
      },
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      resync();
      return;
    }
    const data = response.data;
    // 这局已经结算过(例如在别的标签页里玩完了)：服务端直接回应第一次的结算结果。
    if (data.status === undefined) {
      settle(data, { status: data.detail?.won ? 'won' : 'lost' });
      return;
    }
    versionRef.current = data.version;
    const opened = data.revealed.map(([x, y]) => y * width + x);
    setCells((previous) => {
      const next = previous.slice();
      data.revealed.forEach(([x, y, count]) => {
        next[y * width + x] = count;
      });
      return next;
    });
    // 连锁展开会翻开插错旗的空格，旗子跟着拿掉。
    setFlags((previous) => {
      if (!opened.some((index) => previous.has(index))) return previous;
      const next = new Set(previous);
      opened.forEach((index) => next.delete(index));
      return next;
    });
    if (data.status === 'playing') return;
    settle(data.result, {
      status: data.status,
      mines: new Set(data.mine_cells.map(([x, y]) => y * width + x)),
      exploded: data.exploded
        ? data.exploded[1] * width + data.exploded[0]
        : null,
    });
  };

  const toggleFlag = (index) => {
    setFlags((previous) => {
      const next = new Set(previous);
      if (next.has(index)) {
        next.delete(index);
      } else {
        next.add(index);
      }
      return next;
    });
  };

  const playing = !!game && !end;
  const cellAt = (event) => {
    const cell = event.target.closest('[data-index]');
    return cell ? Number(cell.dataset.index) : null;
  };

  const onBoardClick = (event) => {
    const index = cellAt(event);
    if (index === null || !playing || Date.now() < guardUntilRef.current) {
      return;
    }
    if (cells[index] === HIDDEN) {
      if (flagMode) {
        toggleFlag(index);
      } else if (!flags.has(index)) {
        reveal([index]);
      }
      return;
    }
    // 点一个数字：周围插的旗正好等于它时，一次翻开周围其余的格子。
    const count = cells[index];
    const around = neighbors(index, game.width, game.height);
    const flagged = around.filter((near) => flags.has(near)).length;
    const targets = around.filter(
      (near) => cells[near] === HIDDEN && !flags.has(near),
    );
    if (count > 0 && flagged === count && targets.length > 0) {
      reveal(targets);
    }
  };

  const onBoardContextMenu = (event) => {
    event.preventDefault();
    // 手机长按也会弹出系统菜单，那一下已经由长按插过旗了。
    if (pressRef.current || Date.now() < guardUntilRef.current) return;
    const index = cellAt(event);
    if (index !== null && playing && cells[index] === HIDDEN) {
      toggleFlag(index);
    }
  };

  const onBoardPointerDown = (event) => {
    if (event.pointerType !== 'touch' || !playing) return;
    const index = cellAt(event);
    if (index === null || cells[index] !== HIDDEN) return;
    const press = { index };
    press.timer = setTimeout(() => {
      guardUntilRef.current = Date.now() + PRESS_GUARD_MS;
      pressRef.current = null;
      toggleFlag(index);
    }, LONG_PRESS_MS);
    pressRef.current = press;
  };

  const cancelPress = () => {
    if (!pressRef.current) return;
    clearTimeout(pressRef.current.timer);
    pressRef.current = null;
  };

  const confirmRestart = () => {
    if (!cells.some((count) => count !== HIDDEN)) {
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
  const width = game?.width || selected?.width || 9;
  const height = game?.height || selected?.height || 9;
  const boardLevel = game?.difficulty || difficulty;
  const shownCells = game ? cells : new Array(width * height).fill(HIDDEN);

  const cellView = (index) => {
    const count = shownCells[index];
    const flagged = flags.has(index);
    if (end?.mines?.has(index)) {
      if (index === end.exploded) {
        return { className: 'is-exploded', icon: <Bomb /> };
      }
      if (end.status === 'won' || flagged) {
        return { className: 'is-flag', icon: <Flag /> };
      }
      return { className: 'is-mine', icon: <Bomb /> };
    }
    if (count !== HIDDEN) {
      return {
        className: `is-open is-n${count}`,
        icon: count > 0 ? count : null,
      };
    }
    if (flagged) {
      // 输了之后公开了雷的位置，插在空格上的旗就是插错了。
      const wrong = end?.mines && end.status === 'lost';
      return {
        className: wrong ? 'is-flag is-wrong' : 'is-flag',
        icon: <Flag />,
      };
    }
    return { className: '', icon: null };
  };

  // 赢了之后所有的雷都算插上了旗。
  const minesLeft =
    end?.status === 'won'
      ? 0
      : (game?.mines ?? selected?.mines ?? 0) - flags.size;
  const left = rewardsLeft(info?.rule, info?.progress);
  let leftText = t('今天还能领 {{left}} 次', { left });
  if (info?.rule?.daily_rewards === 0) {
    leftText = t('这个游戏暂不发放奖励');
  } else if (left === 0) {
    leftText = t('今天的领奖次数已用完，还可以继续玩');
  }

  let outcome = null;
  if (end?.status === 'won') {
    outcome = {
      kind: 'win',
      text: t('扫雷成功，用时 {{time}}', {
        time: formatClock((end.at - game.since) / 1000),
      }),
    };
  } else if (end) {
    outcome = { kind: 'loss', text: t('踩到雷了，这局结束') };
  }
  let reward = null;
  if (end?.status === 'won' && result) {
    reward = skillResultMessage(result, t, symbol);
    // 赢了却没达到第一档：这个难度本来就不发奖，说清楚要挑战更高的难度。
    if (reward.kind === 'loss') {
      reward = {
        kind: '',
        text: t('这个难度不发奖励，赢下更高的难度才有奖励'),
      };
    }
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
              t('点击翻开格子，第一下一定不会踩雷'),
              t('右键或长按插旗；手机上也可以打开插旗模式再点'),
              t('点一个周围旗子已经插够的数字，一次翻开它周围其余的格子'),
              t('翻开所有不是雷的格子就赢了，踩到雷这局结束'),
            ]}
          />
        </SideBox>
      }
    />
  );

  const boardClass = [
    'minesweeper-board',
    `is-level-${boardLevel}`,
    playing ? 'is-playing' : '',
    playing && flagMode ? 'is-flag-mode' : '',
  ].join(' ');

  return (
    <GameLayout
      title={meta.name}
      loading={loading}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={symbol}
      side={side}
      className='minesweeper-page'
    >
      <div className='games-stage'>
        <div className='game-hud'>
          <span>
            {t('剩余雷数')}
            <strong>{minesLeft}</strong>
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

        <div className='minesweeper-scroll'>
          <div
            className={boardClass}
            style={{
              gridTemplateColumns: `repeat(${width}, var(--minesweeper-cell))`,
            }}
            onClick={onBoardClick}
            onContextMenu={onBoardContextMenu}
            onPointerDown={onBoardPointerDown}
            onPointerUp={cancelPress}
            onPointerLeave={cancelPress}
            onPointerCancel={cancelPress}
          >
            {shownCells.map((count, index) => {
              const view = cellView(index);
              return (
                <button
                  key={index}
                  type='button'
                  tabIndex={playing ? 0 : -1}
                  data-index={index}
                  className={`minesweeper-cell ${view.className}`}
                >
                  {view.icon}
                </button>
              );
            })}
          </div>
        </div>

        {outcome && (
          <StageMessage kind={outcome.kind}>{outcome.text}</StageMessage>
        )}
        {reward && (
          <StageMessage kind={reward.kind} note={reward.note}>
            {reward.text}
          </StageMessage>
        )}
        {error && <StageMessage kind='error'>{error}</StageMessage>}

        {playing && (
          <div className='game-controls'>
            <button
              type='button'
              className={flagMode ? 'game-chip is-active' : 'game-chip'}
              aria-pressed={flagMode}
              onClick={() => setFlagMode((previous) => !previous)}
            >
              <Flag size={14} className='minesweeper-chip-icon' />
              {t('插旗模式')}
            </button>
            <button
              type='button'
              className='game-chip'
              disabled={busy}
              onClick={confirmRestart}
            >
              <RotateCcw size={14} className='minesweeper-chip-icon' />
              {t('重新选择难度')}
            </button>
          </div>
        )}

        {!playing && checked && (
          <div className='game-controls minesweeper-start'>
            <p className='minesweeper-summary'>
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
                    <span className='minesweeper-chip-note'>
                      {t('{{width}}×{{height}} · {{mines}} 雷', {
                        width: level.width,
                        height: level.height,
                        mines: level.mines,
                      })}
                    </span>
                    <span className='minesweeper-chip-reward'>
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
              <Play size={16} className='minesweeper-play-icon' />
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

export default Minesweeper;
