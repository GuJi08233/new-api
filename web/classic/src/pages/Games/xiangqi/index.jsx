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

import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import PvpGame from '../common/pvp/PvpGame';
import './xiangqi.css';

// 棋盘按 SVG 的单位画：交叉点间距 CELL，四周留 MARGIN 给边框和边线上的棋子。
const CELL = 60;
const MARGIN = 34;
const WIDTH = CELL * 8 + MARGIN * 2;
const HEIGHT = CELL * 9 + MARGIN * 2;
// 连续这么多步(双方合计)没有吃子判和；过半之后在台面上提示。
const QUIET_DRAW_PLIES = 120;

// 棋子字母对应的字：红方大写、黑方小写。棋子上的字和楚河汉界一样是棋具本身的图案，各种语言都一样。
const PIECE_GLYPHS = {
  K: '帅',
  A: '仕',
  B: '相',
  N: '马',
  R: '车',
  C: '炮',
  P: '兵',
  k: '将',
  a: '士',
  b: '象',
  n: '马',
  r: '车',
  c: '炮',
  p: '卒',
};

// 交叉点在屏幕上第 col 列、第 row 行(从左上角数)的 SVG 坐标。
const pointX = (col) => MARGIN + col * CELL;
const pointY = (row) => MARGIN + row * CELL;

// 十条横线、九条竖线(中间七条在河界断开)和两个九宫的斜线。
const GRID_PATH = [
  ...Array.from(
    { length: 10 },
    (_, row) => `M${pointX(0)} ${pointY(row)}H${pointX(8)}`,
  ),
  ...Array.from({ length: 9 }, (_, col) =>
    col === 0 || col === 8
      ? `M${pointX(col)} ${pointY(0)}V${pointY(9)}`
      : `M${pointX(col)} ${pointY(0)}V${pointY(4)}M${pointX(col)} ${pointY(5)}V${pointY(9)}`,
  ),
  ...[0, 7].map(
    (top) =>
      `M${pointX(3)} ${pointY(top)}L${pointX(5)} ${pointY(top + 2)}M${pointX(5)} ${pointY(top)}L${pointX(3)} ${pointY(top + 2)}`,
  ),
].join('');

// 炮与兵卒起始位置上的定位花：每个点四角各一个直角，靠边的点只画里侧。棋盘旋转 180 度后位置不变。
const MARK_PATH = [
  [1, 2],
  [7, 2],
  [1, 7],
  [7, 7],
  ...[0, 2, 4, 6, 8].flatMap((col) => [
    [col, 3],
    [col, 6],
  ]),
]
  .flatMap(([col, row]) =>
    [-1, 1].flatMap((sx) =>
      (col === 0 && sx < 0) || (col === 8 && sx > 0)
        ? []
        : [-1, 1].map(
            (sy) =>
              `M${pointX(col) + sx * 5} ${pointY(row) + sy * 15}V${pointY(row) + sy * 5}H${pointX(col) + sx * 15}`,
          ),
    ),
  )
  .join('');

// 棋盘上一个交叉点(一格大小)的位置，百分比相对于棋盘。
const pointStyle = (col, row) => ({
  left: `${((pointX(col) - CELL / 2) / WIDTH) * 100}%`,
  top: `${((pointY(row) - CELL / 2) / HEIGHT) * 100}%`,
});

const samePoint = (a, b) => a[0] === b[0] && a[1] === b[1];

// 棋盘上的一个棋子：圆盘、内圈与字，halo 是选中、最后一手、将军时的外圈。
const PieceArt = ({ letter }) => (
  <svg className='xiangqi-piece' viewBox='0 0 100 100' aria-hidden>
    <circle cx='50' cy='50' r='47' className='xiangqi-piece-halo' />
    <circle cx='50' cy='50' r='42' className='xiangqi-piece-disc' />
    <circle cx='50' cy='50' r='35' className='xiangqi-piece-ring' />
    <text
      x='50'
      y='50'
      dy='0.36em'
      textAnchor='middle'
      className='xiangqi-piece-glyph'
    >
      {PIECE_GLYPHS[letter]}
    </text>
  </svg>
);

// 一局正常下完的原因(引擎视图的 result)：棋盘上方与结算提示都用它。
function xiangqiResultText(result, t) {
  return {
    checkmate: t('将死'),
    stalemate: t('困毙：无子可走判负'),
    no_capture: t('连续 120 步没有吃子，和棋'),
    move_limit: t('总步数达到 300 步，和棋'),
  }[result];
}

// 棋盘：看的人一方在下面。轮到自己走时点自己的棋子选中，再点高亮的落点走棋；标出最后一手与被将军的将帅。
const XiangqiBoard = ({ state, seat, canMove, over, onMove }) => {
  const { t } = useTranslation();
  // 选中的棋子与选中时的步数：局面一变(自己或对手走了一步)，选中自然失效。
  const [selected, setSelected] = useState(null);

  const flip = !!state && seat !== state.red_seat;
  // 棋盘坐标 (x,y) 在屏幕上的列与行：红方的 y=0 一侧在下面，执黑时整个棋盘旋转 180 度。
  const toScreen = ([x, y]) => (flip ? [8 - x, y] : [x, 9 - y]);
  const legal = canMove ? state?.legal || [] : [];
  const active =
    selected && selected.plies === state?.plies && canMove
      ? selected.point
      : null;
  const targets = active
    ? legal.filter((move) => samePoint(move.from, active))
    : [];
  const last = state?.last;
  // 被将军的是轮到走的一方的将帅。
  const checkedKing = state?.check ? (state.turn === 'red' ? 'K' : 'k') : '';

  const move = (to) => {
    setSelected(null);
    onMove({ from: active, to });
  };

  const points = [];
  (state?.board || []).forEach((line, y) => {
    [...line].forEach((letter, x) => {
      if (letter === '.') return;
      const point = [x, y];
      const [col, row] = toScreen(point);
      const movable = legal.some((item) => samePoint(item.from, point));
      const target = targets.some((item) => samePoint(item.to, point));
      const moved = last && samePoint(last.to, point);
      const classes = [
        'xiangqi-point',
        letter === letter.toUpperCase() ? 'is-red' : 'is-black',
        movable && 'is-movable',
        active && samePoint(active, point) && 'is-selected',
        target && 'is-target',
        moved && 'is-last',
        letter === checkedKing && 'is-check',
      ]
        .filter(Boolean)
        .join(' ');
      const style = pointStyle(col, row);
      if (moved) {
        // 刚走到这里的棋子从出发点滑过来，偏移按格数算。
        const [fromCol, fromRow] = toScreen(last.from);
        style['--xiangqi-dx'] = fromCol - col;
        style['--xiangqi-dy'] = fromRow - row;
      }
      // 刚走的棋子带上步数作 key，下一手落在同一格时也重新播放滑动。
      const key = moved ? `m${x}-${y}-${state.plies}` : `p${x}-${y}`;
      if (!movable && !target) {
        points.push(
          <div key={key} className={classes} style={style}>
            <PieceArt letter={letter} />
          </div>,
        );
        return;
      }
      points.push(
        <button
          key={key}
          type='button'
          className={classes}
          style={style}
          aria-label={PIECE_GLYPHS[letter]}
          aria-pressed={
            movable ? !!active && samePoint(active, point) : undefined
          }
          onClick={(event) => {
            event.stopPropagation();
            if (target) {
              move(point);
              return;
            }
            setSelected(
              active && samePoint(active, point)
                ? null
                : { point, plies: state.plies },
            );
          }}
        >
          <PieceArt letter={letter} />
        </button>,
      );
    });
  });
  targets
    .filter((item) => state.board[item.to[1]][item.to[0]] === '.')
    .forEach((item) => {
      const [col, row] = toScreen(item.to);
      points.push(
        <button
          key={`t${item.to[0]}-${item.to[1]}`}
          type='button'
          className='xiangqi-point xiangqi-target'
          style={pointStyle(col, row)}
          aria-label={t('走到这里')}
          onClick={(event) => {
            event.stopPropagation();
            move(item.to);
          }}
        >
          <span className='xiangqi-dot' />
        </button>,
      );
    });
  if (last) {
    const [col, row] = toScreen(last.from);
    points.push(
      <span
        key='from'
        className='xiangqi-point xiangqi-from'
        style={pointStyle(col, row)}
        aria-hidden
      />,
    );
  }

  const myColor = flip ? 'black' : 'red';
  return (
    <div className='xiangqi-stage'>
      {state && (
        <div className='game-hud xiangqi-hud'>
          <span className={`xiangqi-side is-${myColor}`}>
            {myColor === 'red' ? t('你执红') : t('你执黑')}
          </span>
          <span>
            {t('步数')}
            <strong>{state.plies}</strong>
          </span>
          {!state.over && state.quiet * 2 >= QUIET_DRAW_PLIES && (
            <span>
              {t('未吃子')}
              <strong>{`${state.quiet} / ${QUIET_DRAW_PLIES}`}</strong>
            </span>
          )}
          {state.check && !over && (
            <span className='xiangqi-check'>{t('将军！')}</span>
          )}
          {over && state.result && (
            <span className='xiangqi-result'>
              {xiangqiResultText(state.result, t)}
            </span>
          )}
        </div>
      )}
      <div
        className='xiangqi-board'
        style={{
          aspectRatio: `${WIDTH} / ${HEIGHT}`,
          '--xiangqi-cell-w': `${(CELL / WIDTH) * 100}%`,
          '--xiangqi-cell-h': `${(CELL / HEIGHT) * 100}%`,
        }}
        onClick={() => setSelected(null)}
      >
        <svg
          className='xiangqi-lines'
          viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
          aria-hidden
        >
          <rect
            className='xiangqi-frame'
            x={MARGIN - 9}
            y={MARGIN - 9}
            width={CELL * 8 + 18}
            height={CELL * 9 + 18}
            rx='3'
          />
          <rect
            className='xiangqi-edge'
            x={MARGIN}
            y={MARGIN}
            width={CELL * 8}
            height={CELL * 9}
          />
          <path className='xiangqi-grid' d={GRID_PATH} />
          <path className='xiangqi-marks' d={MARK_PATH} />
          <text
            className='xiangqi-river'
            x={pointX(2)}
            y={pointY(4.5)}
            dy='0.36em'
            textAnchor='middle'
          >
            楚 河
          </text>
          <text
            className='xiangqi-river'
            x={pointX(6)}
            y={pointY(4.5)}
            dy='0.36em'
            textAnchor='middle'
          >
            汉 界
          </text>
        </svg>
        {points}
      </div>
    </div>
  );
};

const Xiangqi = () => {
  const { t } = useTranslation();
  return (
    <PvpGame
      gameKey='xiangqi'
      title={getGameMetaMap(t).xiangqi.name}
      className='xiangqi-page'
      seatLabels={(state) =>
        state
          ? [0, 1].map((index) =>
              index === state.red_seat ? t('执红') : t('执黑'),
            )
          : [t('房主'), t('对手')]
      }
      statusText={({ state, myTurn }) => {
        if (!state?.check) return undefined;
        return myTurn ? t('将军！轮到你应将') : t('将军！等待对手应将');
      }}
      resultNote={({ state }) =>
        state?.over ? xiangqiResultText(state.result, t) : ''
      }
      rules={[
        t('先走的一方执红，红先黑后，轮流走一步'),
        t('帅将不出九宫，双方的帅将不能在同一列上直接照面'),
        t(
          '马走日、蹩马腿，相象走田、塞象眼且不过河，炮隔一个子吃子，兵卒过河后才能横走',
        ),
        t('不能送将；将死对方或让对方无子可走（困毙）就赢了'),
        t(
          '连续 120 步（双方合计）没有吃子，或总步数达到 300 步，判和；长将不判负',
        ),
      ]}
      renderBoard={({ state, seat, canMove, over, onMove }) => (
        <XiangqiBoard
          state={state}
          seat={seat}
          canMove={canMove}
          over={over}
          onMove={onMove}
        />
      )}
    />
  );
};

export default Xiangqi;
