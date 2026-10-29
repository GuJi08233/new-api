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
import { SkipForward } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import PvpGame from '../common/pvp/PvpGame';
import { BoardAssist, currentHint } from '../common/pvp/BoardAssist';
import { playSound } from '../common/sound';
import './reversi.css';

const SIZE = 8;
const SQUARES = Array.from({ length: SIZE * SIZE }, (_, index) => index);
// 自己这一步(落子与翻子)至少先单独显示这么久，再画对手的回应：人机对战里 AI 的一步和自己的一步是一起回来的。
const HOLD_MS = 520;
// 开局之前先画标准开局：中央 d4、e5 白，d5、e4 黑。
const OPENING_BOARD = `${'.'.repeat(27)}wb${'.'.repeat(6)}bw${'.'.repeat(27)}`;
const DIRECTIONS = [
  [1, 0],
  [-1, 0],
  [0, 1],
  [0, -1],
  [1, 1],
  [1, -1],
  [-1, 1],
  [-1, -1],
];

// 视图里 black 是执黑的座位，另一方执白。
function discOf(state, seat) {
  return state?.black === seat ? 'b' : 'w';
}

// disc 在 square 落子之后的棋盘：八个方向上夹住的对方棋子翻成自己的颜色。只用来在请求回来之前先画出自己这一步，
// 局面以服务端返回的为准。
function playedBoard(board, square, disc) {
  const cells = board.split('');
  cells[square] = disc;
  for (const [dx, dy] of DIRECTIONS) {
    const run = [];
    let x = (square % SIZE) + dx;
    let y = Math.floor(square / SIZE) + dy;
    while (
      x >= 0 &&
      x < SIZE &&
      y >= 0 &&
      y < SIZE &&
      cells[y * SIZE + x] !== '.'
    ) {
      if (cells[y * SIZE + x] === disc) {
        run.forEach((index) => {
          cells[index] = disc;
        });
        break;
      }
      run.push(y * SIZE + x);
      x += dx;
      y += dy;
    }
  }
  return cells.join('');
}

// 两个局面之间新落下的子与翻过来的子，用来播放落子与翻子动画。子变少说明换了一局或者撤回了没被接受的一步，不播放。
function boardChanges(before, after) {
  const changes = { placed: [], flipped: [] };
  if (after.replace(/\./g, '').length < before.replace(/\./g, '').length) {
    return changes;
  }
  for (let index = 0; index < after.length; index++) {
    if (before[index] === after[index]) continue;
    if (before[index] === '.') changes.placed.push(index);
    else if (after[index] !== '.') changes.flipped.push(index);
  }
  return changes;
}

// 黑白棋盘：8×8。轮到自己时标出能下的格子(来自视图的 legal)，悬停预览；请求还没回来时先画出自己这一步和翻过来的子
// (请求失败就撤掉)，服务端的局面回来后以它为准。上方是双方子数和"无子可下、跳过"的提示。
const ReversiBoard = ({
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
  const board = state?.board || OPENING_BOARD;
  const mine = discOf(state, seat);
  const theirs = mine === 'b' ? 'w' : 'b';
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

  const [seen, setSeen] = useState({ board: shown, placed: [], flipped: [] });
  if (seen.board !== shown) {
    setSeen({ board: shown, ...boardChanges(seen.board, shown) });
  }
  // 落子一声，翻得越多翻子声越高。
  useEffect(() => {
    if (seen.placed.length === 0) return;
    playSound('place');
    if (seen.flipped.length > 0) {
      playSound('flip', {
        pitch: 1 + Math.min(seen.flipped.length, 10) * 0.05,
      });
    }
  }, [seen]);
  const shownHint = waiting ? null : currentHint(hint, state);
  const hintSquare = shownHint ? shownHint.y * SIZE + shownHint.x : -1;

  const playable = canMove && !waiting && !over;
  const legal = new Set(
    playable ? (state?.legal || []).map((p) => p.y * SIZE + p.x) : [],
  );
  let last = state?.last ? state.last.y * SIZE + state.last.x : -1;
  if (waiting) last = waiting.square;

  // 对手一次连下了几子，说明中间轮到自己时无子可下、被跳过了(人机对战里 AI 会在一个请求里连着走完)。
  const streak = seen.placed.filter((index) => shown[index] === theirs).length;
  let notice = '';
  if (state && !state.over && !waiting && !over) {
    if (streak >= 2) {
      notice = t('你无子可下，对手连走了 {{count}} 步', { count: streak });
    } else if (state.passed === seat) {
      notice = t('你无子可下，由对手接着走');
    } else if (state.passed === 1 - seat) {
      notice = t('对手无子可下，你接着走');
    }
  }

  const play = (square) => {
    if (!legal.has(square)) return;
    setPending({
      square,
      board: playedBoard(board, square, mine),
      from: board,
    });
    onMove({ x: square % SIZE, y: Math.floor(square / SIZE) });
  };

  const myDiscs = shown.split(mine).length - 1;
  const theirDiscs = shown.split(theirs).length - 1;
  return (
    <div className='reversi-board-wrap'>
      <div className='game-hud reversi-hud'>
        <span className='reversi-count'>
          <span className={`reversi-count-disc is-${mine}`} />
          {t('我')}
          <strong>{myDiscs}</strong>
        </span>
        <span className='reversi-count'>
          <span className={`reversi-count-disc is-${theirs}`} />
          {t('对手')}
          <strong>{theirDiscs}</strong>
        </span>
      </div>
      <div className='reversi-notice' aria-live='polite'>
        {notice && (
          <>
            <SkipForward size={14} />
            {notice}
          </>
        )}
      </div>
      <div className='reversi-board' onMouseLeave={() => setHover(-1)}>
        {SQUARES.map((index) => {
          const disc = shown[index];
          const open = legal.has(index);
          const classes = ['reversi-disc', `is-${disc}`];
          if (seen.placed.includes(index)) classes.push('is-new');
          if (seen.flipped.includes(index)) classes.push('is-flip');
          return (
            <button
              key={index}
              type='button'
              className={open ? 'reversi-cell is-legal' : 'reversi-cell'}
              aria-disabled={!open}
              aria-label={`${String.fromCharCode(97 + (index % SIZE))}${Math.floor(index / SIZE) + 1}`}
              onClick={() => play(index)}
              onMouseMove={() => setHover(index)}
            >
              {disc !== '.' && <span className={classes.join(' ')} />}
              {disc === '.' && open && (
                <span
                  className={
                    hover === index
                      ? `reversi-disc is-${mine} is-ghost`
                      : 'reversi-hint'
                  }
                />
              )}
              {index === last && <span className='reversi-last' />}
              {index === hintSquare && <span className='board-hint-cell' />}
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

// 正常下完的一局按子数定胜负，结果下面写上终局的子数。
function reversiResultNote(state, t) {
  if (!state?.over) return '';
  return t('黑 {{black}} : 白 {{white}}', {
    black: state.black_discs,
    white: state.white_discs,
  });
}

const Reversi = () => {
  const { t } = useTranslation();
  // 座位称呼：先走的一方执黑，开局之前还不知道谁先走。
  const seatLabels = (state) => {
    if (!state) return [t('待定'), t('待定')];
    return [0, 1].map((seat) => {
      const disc = discOf(state, seat);
      return (
        <React.Fragment key={seat}>
          <span className={`reversi-count-disc is-${disc}`} />
          {disc === 'b' ? t('执黑') : t('执白')}
        </React.Fragment>
      );
    });
  };
  return (
    <PvpGame
      gameKey='reversi'
      title={t('黑白棋')}
      className='reversi-page'
      seatLabels={seatLabels}
      resultNote={({ state }) => reversiResultNote(state, t)}
      rules={[
        t('8×8 棋盘，黑先；落子必须夹住并翻转对方至少一子'),
        t('一方无子可下时自动跳过，由对方接着走'),
        t('双方都无子可下时结束，子多者胜，子数相同为和棋'),
        t('每局前 3 次悔棋免费，之后每次 1 游戏币；房间里悔棋要对方同意'),
        t('人机对战可以请电脑分析下一步，每局前 3 次提示免费'),
      ]}
      renderBoard={(props) => <ReversiBoard {...props} />}
    />
  );
};

export default Reversi;
