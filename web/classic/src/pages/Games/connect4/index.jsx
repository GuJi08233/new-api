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

import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import PvpGame from '../common/pvp/PvpGame';
import { BoardAssist, currentHint } from '../common/pvp/BoardAssist';
import { playSound } from '../common/sound';
import './connect4.css';

const COLS = 7;
const ROWS = 6;
const EMPTY_BOARD = '.'.repeat(COLS * ROWS);
const COLUMNS = Array.from({ length: COLS }, (_, col) => col);
const ROW_INDEXES = Array.from({ length: ROWS }, (_, row) => row);
// 自己投下的一子至少先单独显示这么久(落到最底下一行的时长)，再画对手的回应：人机对战里 AI 的一步和自己的一步是一起回来的。
const HOLD_MS = 460;

// 视图里 first 是先走的座位，执红；另一方执黄。
function discOf(state, seat) {
  return state?.first === seat ? 'r' : 'y';
}

// 投进 col 列的棋子落到哪一行(从上数)，这一列满了为 -1。
function landingRow(board, col) {
  for (let row = ROWS - 1; row >= 0; row--) {
    if (board[row * COLS + col] === '.') return row;
  }
  return -1;
}

// 这一次新落下的棋子，用来播放下落动画；棋子变少说明换了一局或者撤回了没被接受的一步，不播放。
function newDiscs(before, after) {
  if (after.replace(/\./g, '').length < before.replace(/\./g, '').length) {
    return [];
  }
  const placed = [];
  for (let index = 0; index < after.length; index++) {
    if (before[index] === '.' && after[index] !== '.') placed.push(index);
  }
  return placed;
}

// 四子棋盘：7 列 × 6 行。轮到自己时点一列投子，棋盘上方预览这一子；请求还没回来时先让这一子落下去(请求失败就撤掉)，
// 服务端的局面回来后以它为准。
const Connect4Board = ({
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
  const board = state?.board || EMPTY_BOARD;
  const mine = discOf(state, seat);
  const [pending, setPending] = useState(null);
  const [released, setReleased] = useState(null);
  const [hover, setHover] = useState(-1);
  useEffect(() => {
    if (!pending) return undefined;
    const timer = setTimeout(() => setReleased(pending), HOLD_MS);
    return () => clearTimeout(timer);
  }, [pending]);
  const waiting =
    pending &&
    ((busy && pending.from === board) ||
      (pending !== released && pending.from !== board))
      ? pending
      : null;
  const shown = waiting ? waiting.board : board;

  const [seen, setSeen] = useState({ board: shown, placed: [] });
  if (seen.board !== shown) {
    setSeen({ board: shown, placed: newDiscs(seen.board, shown) });
  }
  useEffect(() => {
    if (seen.placed.length > 0) playSound('drop');
  }, [seen]);
  const shownHint = waiting ? null : currentHint(hint, state);

  const playable = canMove && !waiting && !over;
  const line = new Set(
    waiting ? [] : (state?.line || []).map((c) => c.row * COLS + c.col),
  );
  let last = state?.last ? state.last.row * COLS + state.last.col : -1;
  if (waiting) last = waiting.index;

  const drop = (col) => {
    const row = landingRow(shown, col);
    if (!playable || row < 0) return;
    const index = row * COLS + col;
    setPending({
      index,
      from: board,
      board: `${board.slice(0, index)}${mine}${board.slice(index + 1)}`,
    });
    onMove({ col });
  };

  return (
    <div className='connect4-board-wrap'>
      <div className='connect4-slots' aria-hidden>
        {COLUMNS.map((col) => (
          <span key={col} className='connect4-slot'>
            {playable && hover === col && landingRow(shown, col) >= 0 && (
              <span className={`connect4-disc is-${mine} is-ghost`} />
            )}
            {shownHint?.x === col && hover !== col && (
              <span className='connect4-hint-arrow'>▼</span>
            )}
          </span>
        ))}
      </div>
      <div className='connect4-board' onMouseLeave={() => setHover(-1)}>
        {COLUMNS.map((col) => {
          const open = playable && landingRow(shown, col) >= 0;
          return (
            <button
              key={col}
              type='button'
              className='connect4-column'
              aria-disabled={!open}
              aria-label={t('投进第 {{col}} 列', { col: col + 1 })}
              onClick={() => drop(col)}
              onMouseMove={() => setHover(col)}
              onFocus={() => setHover(col)}
              onBlur={() => setHover(-1)}
            >
              {ROW_INDEXES.map((row) => {
                const index = row * COLS + col;
                const disc = shown[index];
                const classes = ['connect4-disc', `is-${disc}`];
                if (seen.placed.includes(index)) classes.push('is-new');
                if (line.has(index)) classes.push('is-line');
                if (index === last) classes.push('is-last');
                return (
                  <span key={row} className='connect4-cell'>
                    {disc !== '.' && (
                      <span
                        className={classes.join(' ')}
                        style={{ '--connect4-fall': row + 1 }}
                      />
                    )}
                  </span>
                );
              })}
            </button>
          );
        })}
      </div>
      <BoardAssist
        state={state}
        seat={seat}
        busy={busy}
        over={over}
        onUndo={onUndo}
        hintStatus={hintStatus}
        onHint={onHint}
        hintShown={!!shownHint}
      />
    </div>
  );
};

// 正常下完的一局为什么结束：连成四子，或者棋盘下满。
function connect4ResultNote(state, t) {
  if (!state?.over) return '';
  return state.winner < 0 ? t('棋盘下满，和棋') : t('连成四子');
}

const Connect4 = () => {
  const { t } = useTranslation();
  // 座位称呼：先走的一方执红，开局之前还不知道谁先走。
  const seatLabels = (state) => {
    if (!state) return [t('待定'), t('待定')];
    return [0, 1].map((seat) => {
      const disc = discOf(state, seat);
      return (
        <React.Fragment key={seat}>
          <span className={`connect4-seat-disc is-${disc}`} />
          {disc === 'r' ? t('执红') : t('执黄')}
        </React.Fragment>
      );
    });
  };
  return (
    <PvpGame
      gameKey='connect4'
      title={t('四子棋')}
      className='connect4-page'
      seatLabels={seatLabels}
      resultNote={({ state }) => connect4ResultNote(state, t)}
      rules={[
        t('7 列 × 6 行，选一列投子，棋子落到这一列最低的空位'),
        t('横、竖、斜任一方向连成四子获胜'),
        t('棋盘下满仍未分出胜负为和棋'),
        t('每局前 3 次悔棋免费，之后每次 1 游戏币；房间里悔棋要对方同意'),
        t('人机对战可以请电脑分析下一步，每局前 3 次提示免费'),
      ]}
      renderBoard={(props) => <Connect4Board {...props} />}
    />
  );
};

export default Connect4;
