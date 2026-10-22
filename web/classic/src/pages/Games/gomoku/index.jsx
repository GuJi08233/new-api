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

import React, { useEffect, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import PvpGame from '../common/pvp/PvpGame';
import './gomoku.css';

const SIZE = 15;
const EMPTY_BOARD = '.'.repeat(SIZE * SIZE);
// 星位：四角的星与天元，格子编号 y*15+x。
const STAR_POINTS = [48, 56, 112, 168, 176];
const GRID_LINES = Array.from({ length: SIZE }, (_, index) => index + 0.5);
// 自己落下的一子至少先单独显示这么久(落子动画的时长)，再画对手的回应：人机对战里 AI 的一步和自己的一步是一起回来的。
const HOLD_MS = 260;

// 视图里 black 是执黑的座位，另一方执白。
function stoneOf(state, seat) {
  return state?.black === seat ? 'b' : 'w';
}

// 指针落在棋盘上的哪个交叉点，出了棋盘为 -1。
function pointerSquare(event) {
  const rect = event.currentTarget.getBoundingClientRect();
  const x = Math.floor(((event.clientX - rect.left) / rect.width) * SIZE);
  const y = Math.floor(((event.clientY - rect.top) / rect.height) * SIZE);
  if (x < 0 || y < 0 || x >= SIZE || y >= SIZE) return -1;
  return y * SIZE + x;
}

// 这一次新落下的棋子，用来播放落子动画；棋子变少说明换了一局或者撤回了没被接受的一步，不播放。
function newStones(before, after) {
  if (after.replace(/\./g, '').length < before.replace(/\./g, '').length) {
    return [];
  }
  const placed = [];
  for (let index = 0; index < after.length; index++) {
    if (before[index] === '.' && after[index] !== '.') placed.push(index);
  }
  return placed;
}

// 五子棋盘：15×15 交叉点。轮到自己时鼠标悬停处预览棋子、点击落子；触屏上先点一下选中、再点同一处落子，免得点歪。
// 落子请求还没回来时先把这一子画上去(请求失败就撤掉)，服务端的局面回来后以它为准。
const GomokuBoard = ({ state, seat, canMove, busy, over, onMove }) => {
  const { t } = useTranslation();
  const board = state?.board || EMPTY_BOARD;
  const mine = stoneOf(state, seat);
  const [pending, setPending] = useState(null);
  const [released, setReleased] = useState(null);
  const [hover, setHover] = useState(-1);
  const [selected, setSelected] = useState(-1);
  const [keyboardSquare, setKeyboardSquare] = useState(112);
  const [keyboardActive, setKeyboardActive] = useState(false);
  const boardId = useId();
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
    setSeen({ board: shown, placed: newStones(seen.board, shown) });
    setSelected(-1);
  }

  const playable = canMove && !waiting && !over;
  const target = keyboardActive
    ? keyboardSquare
    : selected >= 0
      ? selected
      : hover;
  const ghost = playable && target >= 0 && shown[target] === '.' ? target : -1;
  const line = new Set(
    waiting ? [] : (state?.line || []).map((p) => p.y * SIZE + p.x),
  );
  let last = state?.last ? state.last.y * SIZE + state.last.x : -1;
  if (waiting) last = waiting.square;

  const handleMove = (event) => {
    if (event.pointerType !== 'mouse') return;
    const square = pointerSquare(event);
    if (square !== hover) setHover(square);
  };

  const place = (square) => {
    if (!playable || square < 0 || shown[square] !== '.') return;
    setSelected(-1);
    setPending({
      square,
      from: board,
      board: `${board.slice(0, square)}${mine}${board.slice(square + 1)}`,
    });
    onMove({ x: square % SIZE, y: Math.floor(square / SIZE) });
  };

  const handleUp = (event) => {
    if (!playable || (event.pointerType === 'mouse' && event.button !== 0)) {
      return;
    }
    const square = pointerSquare(event);
    if (square < 0 || shown[square] !== '.') return;
    if (event.pointerType !== 'mouse' && square !== selected) {
      setSelected(square);
      return;
    }
    place(square);
  };

  const handleKeyDown = (event) => {
    if (event.altKey || event.ctrlKey || event.metaKey) return;
    const row = Math.floor(keyboardSquare / SIZE);
    const col = keyboardSquare % SIZE;
    const next = {
      ArrowLeft: row * SIZE + Math.max(col - 1, 0),
      ArrowRight: row * SIZE + Math.min(col + 1, SIZE - 1),
      ArrowUp: Math.max(row - 1, 0) * SIZE + col,
      ArrowDown: Math.min(row + 1, SIZE - 1) * SIZE + col,
      Home: row * SIZE,
      End: row * SIZE + SIZE - 1,
    }[event.key];
    if (next !== undefined) {
      event.preventDefault();
      setKeyboardActive(true);
      setKeyboardSquare(next);
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      setKeyboardActive(true);
      if (!event.repeat) place(keyboardSquare);
    }
  };

  const stones = [];
  for (let index = 0; index < shown.length; index++) {
    const stone = shown[index];
    if (stone === '.') continue;
    const cx = (index % SIZE) + 0.5;
    const cy = Math.floor(index / SIZE) + 0.5;
    stones.push(
      <g key={index}>
        <circle
          cx={cx}
          cy={cy}
          r={0.43}
          className={`gomoku-stone is-${stone} ${seen.placed.includes(index) ? 'is-new' : ''}`}
        />
        {line.has(index) && (
          <circle cx={cx} cy={cy} r={0.47} className='gomoku-line-ring' />
        )}
        {index === last && (
          <circle cx={cx} cy={cy} r={0.11} className='gomoku-last' />
        )}
      </g>,
    );
  }

  return (
    <div className='gomoku-board-wrap'>
      <svg
        className={playable ? 'gomoku-board is-playable' : 'gomoku-board'}
        viewBox={`0 0 ${SIZE} ${SIZE}`}
        role='grid'
        tabIndex={0}
        aria-label={t('五子棋')}
        aria-rowcount={SIZE}
        aria-colcount={SIZE}
        aria-activedescendant={`${boardId}-${keyboardSquare}`}
        aria-describedby={`${boardId}-help`}
        onKeyDown={handleKeyDown}
        onBlur={() => setKeyboardActive(false)}
        onPointerDown={() => setKeyboardActive(false)}
        onPointerMove={handleMove}
        onPointerLeave={() => setHover(-1)}
        onPointerUp={handleUp}
      >
        <g className='gomoku-grid'>
          {GRID_LINES.map((offset) => (
            <React.Fragment key={offset}>
              <line x1={0.5} y1={offset} x2={SIZE - 0.5} y2={offset} />
              <line x1={offset} y1={0.5} x2={offset} y2={SIZE - 0.5} />
            </React.Fragment>
          ))}
        </g>
        {STAR_POINTS.map((index) => (
          <circle
            key={index}
            className='gomoku-star'
            cx={(index % SIZE) + 0.5}
            cy={Math.floor(index / SIZE) + 0.5}
            r={0.1}
          />
        ))}
        {stones}
        {GRID_LINES.map((_, row) => (
          <g key={row} role='row' aria-rowindex={row + 1}>
            {GRID_LINES.map((_, col) => {
              const square = row * SIZE + col;
              const stone =
                shown[square] === 'b'
                  ? t('黑子')
                  : shown[square] === 'w'
                    ? t('白子')
                    : t('空位');
              return (
                <rect
                  key={col}
                  id={`${boardId}-${square}`}
                  role='gridcell'
                  aria-colindex={col + 1}
                  aria-label={t('第 {{row}} 行，第 {{col}} 列，{{stone}}', {
                    row: row + 1,
                    col: col + 1,
                    stone,
                  })}
                  aria-disabled={!playable || shown[square] !== '.'}
                  x={col}
                  y={row}
                  width={1}
                  height={1}
                  className={
                    keyboardActive && square === keyboardSquare
                      ? 'gomoku-focus-cell is-active'
                      : 'gomoku-focus-cell'
                  }
                />
              );
            })}
          </g>
        ))}
        {ghost >= 0 && (
          <circle
            cx={(ghost % SIZE) + 0.5}
            cy={Math.floor(ghost / SIZE) + 0.5}
            r={0.43}
            className={`gomoku-stone is-${mine} is-ghost`}
          />
        )}
      </svg>
      <div className='gomoku-hint'>
        {ghost >= 0 && ghost === selected && t('再点一次同一位置落子')}
      </div>
      <div id={`${boardId}-help`} className='gomoku-keyboard-help'>
        {t('方向键移动，Enter 或空格落子')}
      </div>
    </div>
  );
};

// 正常下完的一局为什么结束：连成五子，或者棋盘下满。
function gomokuResultNote(state, t) {
  if (!state?.over) return '';
  return state.winner < 0 ? t('棋盘下满，和棋') : t('连成五子');
}

const Gomoku = () => {
  const { t } = useTranslation();
  // 座位称呼：执黑还是执白由视图里的 black 决定，开局之前还不知道。
  const seatLabels = (state) => {
    if (!state) return [t('待定'), t('待定')];
    return [0, 1].map((seat) => {
      const stone = stoneOf(state, seat);
      return (
        <React.Fragment key={seat}>
          <span className={`gomoku-seat-stone is-${stone}`} />
          {stone === 'b' ? t('执黑') : t('执白')}
        </React.Fragment>
      );
    });
  };
  return (
    <PvpGame
      gameKey='gomoku'
      title={t('五子棋')}
      className='gomoku-page'
      seatLabels={seatLabels}
      resultNote={({ state }) => gomokuResultNote(state, t)}
      rules={[
        t('15×15 棋盘，黑先，双方轮流在交叉点上落子'),
        t('横、竖、斜任一方向连成五子或以上获胜，没有禁手'),
        t('棋盘下满仍未分出胜负为和棋'),
      ]}
      renderBoard={(props) => <GomokuBoard {...props} />}
    />
  );
};

export default Gomoku;
