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
import { Button } from '@douyinfe/semi-ui';
import {
  Copy,
  Handshake,
  Lightbulb,
  Undo2,
  Volume2,
  VolumeX,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import { copy, showError, showSuccess } from '../../../helpers';
import PvpGame from '../common/pvp/PvpGame';
import { formatCoins } from '../common/api';
import { formatEvaluation, hintForPlies, hintMarker } from './analysis';
import {
  PIECE_GLYPHS,
  formatXiangqiRecord,
  getXiangqiCaptures,
  groupXiangqiHistory,
  recentXiangqiRounds,
  squareLabel,
  xiangqiMoveLabel,
} from './records';
import { useXiangqiSounds } from './useXiangqiSounds';
import './xiangqi.css';

// 棋盘按 SVG 的单位画：交叉点间距 CELL，四周留 MARGIN 给边框和边线上的棋子。格子按视口高度收窄，棋盘才能整块放进
// 手机横屏与矮窗口；宽高比跟着格子一起变，棋子仍然落在交叉点上。
const CELL = 60;
const MARGIN = 52;
const WIDTH = CELL * 8 + MARGIN * 2;
const HEIGHT = CELL * 9 + MARGIN * 2;

// 连续这么多步(双方合计)没有吃子判和；过半之后在台面上提示。
const QUIET_DRAW_PLIES = 120;
// 棋谱默认只显示最近这么多回合(手机上整页不必为长棋谱拉长)，展开后显示全部。
const RECENT_ROUNDS = 6;

const CHINESE_FILES = '一二三四五六七八九';

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
    repetition: t('同一局面出现三次，和棋'),
    perpetual_check: t('长将判负'),
    agreement: t('双方同意和棋'),
  }[result];
}

// 棋盘：看的人一方在下面。轮到自己走时点自己的棋子选中，再点高亮的落点走棋；标出最后一手与被将军的将帅。
const XiangqiBoard = ({
  state,
  gameId,
  seat,
  canMove,
  over,
  hint,
  hintStatus,
  onHint,
  onMove,
  onUndo,
  onDraw,
  busy,
}) => {
  const { t } = useTranslation();
  // 选中的棋子与选中时的步数：局面一变(自己或对手走了一步)，选中自然失效。
  const [selected, setSelected] = useState(null);
  const [showAllRounds, setShowAllRounds] = useState(false);
  const historyScroll = useRef(null);
  const { soundEnabled, toggleSound, unlockSound } = useXiangqiSounds(
    state,
    gameId,
  );

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
  const history = state?.history || [];
  const rounds = groupXiangqiHistory(history);
  // 只收起回合数很多的棋谱；刚开局不必给一个"展开"按钮。
  const canCollapse = rounds.length > RECENT_ROUNDS;
  const visibleRounds = recentXiangqiRounds(
    rounds,
    RECENT_ROUNDS,
    showAllRounds,
  );
  const captures = getXiangqiCaptures(history);
  const lastMoves = state?.last_moves || {};
  const myColor = flip ? 'black' : 'red';
  // 被将军的是轮到走的一方的将帅。
  const checkedKing = state?.check ? (state.turn === 'red' ? 'K' : 'k') : '';
  // 提示只在它分析的那个局面上有效：走了一步或悔棋之后局面变了，旧的分析就不能再标在棋盘上。
  const shownHint = hintForPlies(hint, state);
  const marker = hintMarker(shownHint);
  // 同一个局面分析出来的着法总是一样的，已经提示过就不再让玩家为同一个答案再付一次钱。
  const hintAvailable =
    !!onHint && !!hintStatus?.available && canMove && !shownHint;
  const hintCost = hintStatus?.cost ?? 0;

  useEffect(() => {
    if (historyScroll.current) {
      historyScroll.current.scrollTop = historyScroll.current.scrollHeight;
    }
  }, [state?.plies]);

  const copyRecord = async () => {
    const lines = [
      `${t('中国象棋')} · ${t('红方')} / ${t('黑方')}`,
      ...(state.plies > history.length
        ? [t('仅显示第 {{ply}} 步起的记录', { ply: history[0].ply })]
        : []),
      formatXiangqiRecord(history, (piece) =>
        t('吃{{piece}}', { piece: PIECE_GLYPHS[piece] }),
      ),
    ];
    if (await copy(lines.join('\n'))) showSuccess(t('复制成功'));
    else showError(t('复制失败，请手动选择文本复制'));
  };

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
      const color = letter === letter.toUpperCase() ? 'red' : 'black';
      const classes = [
        'xiangqi-point',
        `is-${color}`,
        movable && 'is-movable',
        active && samePoint(active, point) && 'is-selected',
        target && 'is-target',
        moved && 'is-last',
        lastMoves[color] &&
          samePoint(lastMoves[color].to, point) &&
          'is-side-last',
        marker && samePoint(marker.from, point) && 'is-hint-from',
        marker && samePoint(marker.to, point) && 'is-hint-to',
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
  // 提示的落点可能是空位，那里没有棋子、也还没被选中，单独画一个标记，否则棋盘上看不出该走到哪里。
  const hintTo =
    marker && state?.board ? state.board[marker.to[1]]?.[marker.to[0]] : null;
  if (
    marker &&
    hintTo === '.' &&
    !targets.some((item) => samePoint(item.to, marker.to))
  ) {
    const [col, row] = toScreen(marker.to);
    points.push(
      <div
        key={`hint-${marker.to[0]}-${marker.to[1]}`}
        className='xiangqi-point xiangqi-hint-point'
        style={pointStyle(col, row)}
        aria-hidden
      />,
    );
  }
  return (
    <div
      className='xiangqi-stage'
      onPointerDownCapture={unlockSound}
      onKeyDownCapture={unlockSound}
    >
      <div className='xiangqi-sound-controls'>
        <Button
          size='small'
          theme='borderless'
          icon={soundEnabled ? <Volume2 size={15} /> : <VolumeX size={15} />}
          aria-pressed={soundEnabled}
          onClick={toggleSound}
        >
          {soundEnabled ? t('音效已开启') : t('音效已关闭')}
        </Button>
      </div>
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
          {!over && state.repeat >= 2 && (
            <span className='xiangqi-check'>
              {t('局面已重复两次，再出现一次判局')}
            </span>
          )}
          {over && state.result && (
            <span className='xiangqi-result'>
              {xiangqiResultText(state.result, t)}
            </span>
          )}
        </div>
      )}
      {!over && state?.undo && (
        <div className='xiangqi-undo'>
          {state.undo.pending_by === seat && (
            <span className='xiangqi-undo-note'>
              {t('已提出悔棋，等对方回应')}
            </span>
          )}
          {state.undo.pending_by >= 0 && state.undo.pending_by !== seat && (
            <>
              <span className='xiangqi-undo-note'>{t('对方想悔棋')}</span>
              <Button
                size='small'
                theme='solid'
                type='primary'
                disabled={busy}
                onClick={() => onUndo(true)}
              >
                {t('同意')}
              </Button>
              <Button
                size='small'
                disabled={busy}
                onClick={() => onUndo(false)}
              >
                {t('拒绝')}
              </Button>
            </>
          )}
          {state.undo.pending_by < 0 && !state.undo.declined && (
            <Button
              size='small'
              icon={<Undo2 size={14} />}
              disabled={busy || !state.undo.available}
              onClick={() => onUndo()}
            >
              {state.undo.free_left > 0
                ? t('悔棋（免费 {{left}} 次）', {
                    left: state.undo.free_left,
                  })
                : t('悔棋（{{cost}} 游戏币）', {
                    cost: formatCoins(state.undo.cost),
                  })}
            </Button>
          )}
          {state.undo.pending_by < 0 && state.undo.declined && (
            <span className='xiangqi-undo-note'>{t('对方拒绝了悔棋')}</span>
          )}
          {state.undo.draw_by === seat && (
            <span className='xiangqi-undo-note'>
              {t('已提出和棋，等对方回应')}
            </span>
          )}
          {state.undo.draw_by >= 0 && state.undo.draw_by !== seat && (
            <>
              <span className='xiangqi-undo-note'>{t('对方想和棋')}</span>
              <Button
                size='small'
                theme='solid'
                type='primary'
                disabled={busy}
                onClick={() => onDraw(true)}
              >
                {t('同意')}
              </Button>
              <Button
                size='small'
                disabled={busy}
                onClick={() => onDraw(false)}
              >
                {t('拒绝')}
              </Button>
            </>
          )}
          {state.undo.draw_by < 0 && state.undo.can_offer && (
            <Button
              size='small'
              icon={<Handshake size={14} />}
              disabled={busy}
              onClick={() => onDraw()}
            >
              {t('求和')}
            </Button>
          )}
          {onHint && hintStatus && (
            <Button
              size='small'
              icon={<Lightbulb size={14} />}
              disabled={busy || !hintAvailable}
              onClick={onHint}
            >
              {hintStatus.free_left > 0
                ? t('提示（免费 {{left}} 次）', {
                    left: hintStatus.free_left,
                  })
                : t('提示（{{cost}} 游戏币）', {
                    cost: formatCoins(hintCost),
                  })}
            </Button>
          )}
        </div>
      )}
      {shownHint && (
        <div className='xiangqi-hint' role='status'>
          <span className='xiangqi-hint-title'>
            <Lightbulb size={14} aria-hidden />
            {t('推荐着法')}
          </span>
          <span className='xiangqi-hint-move'>
            {shownHint.line?.notation || t('看棋盘上的提示标记')}
          </span>
          <span className='xiangqi-hint-eval'>
            {formatEvaluation(shownHint.line?.score, shownHint.line?.mate, t)}
          </span>
          {(shownHint.lines || []).length > 1 && (
            <ol className='xiangqi-hint-lines'>
              {shownHint.lines.slice(1).map((line, index) => (
                <li key={`${line.from}-${line.to}-${index}`}>
                  <span className='xiangqi-hint-move'>{line.notation}</span>
                  <span>{formatEvaluation(line.score, line.mate, t)}</span>
                </li>
              ))}
            </ol>
          )}
        </div>
      )}
      {state && (
        <div className='xiangqi-last-moves'>
          {[myColor, myColor === 'red' ? 'black' : 'red'].map((color) => {
            const previous = lastMoves[color];
            return (
              <div key={color} className={`xiangqi-last-move is-${color}`}>
                <span className='xiangqi-move-side'>
                  <i aria-hidden />
                  {color === myColor ? t('己方上一步') : t('对方上一步')}
                </span>
                {previous ? (
                  <span className='xiangqi-move-detail'>
                    <span className='xiangqi-move-notation'>
                      {xiangqiMoveLabel(previous)}
                    </span>
                    {previous.captured && (
                      <span className='xiangqi-history-capture'>
                        {t('吃{{piece}}', {
                          piece: PIECE_GLYPHS[previous.captured],
                        })}
                      </span>
                    )}
                  </span>
                ) : (
                  <span className='xiangqi-move-empty'>
                    {state.plies > history.length
                      ? t('早期行棋未记录')
                      : t('尚未行棋')}
                  </span>
                )}
              </div>
            );
          })}
        </div>
      )}
      {/* 棋盘宽高比与格子尺寸由 CSS 算：宽高比随视口高度收窄，--xiangqi-cell-* 决定每个交叉点占多大一块。 */}
      <div
        className='xiangqi-board'
        style={{
          '--xiangqi-cells-w': (CELL * 8 + MARGIN * 2) / CELL,
          '--xiangqi-cells-h': (CELL * 9 + MARGIN * 2) / CELL,
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
          {['red', 'black'].flatMap((color) =>
            Array.from({ length: 9 }, (_, x) => {
              const [col] = toScreen([x, 0]);
              const below = color === myColor;
              return (
                <text
                  key={`${color}-file-${x}`}
                  className={`xiangqi-coordinate is-${color}`}
                  x={pointX(col)}
                  y={below ? HEIGHT - 10 : 22}
                  textAnchor='middle'
                >
                  {color === 'red' ? CHINESE_FILES[8 - x] : x + 1}
                </text>
              );
            }),
          )}
          {['red', 'black'].map((color) => {
            const previous = lastMoves[color];
            if (!previous) return null;
            const [fromCol, fromRow] = toScreen(previous.from);
            const [toCol, toRow] = toScreen(previous.to);
            // 两方先后落在同一点时，用不同尺寸保留双方的标记。
            const radius = color === 'red' ? 28 : 32;
            return (
              <g key={color} className={`xiangqi-move-marker is-${color}`}>
                <circle
                  className='xiangqi-move-from'
                  cx={pointX(fromCol)}
                  cy={pointY(fromRow)}
                  r={radius}
                />
                <rect
                  className='xiangqi-move-to'
                  x={pointX(toCol) - radius}
                  y={pointY(toRow) - radius}
                  width={radius * 2}
                  height={radius * 2}
                  rx='7'
                />
              </g>
            );
          })}
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
      {state && (
        <>
          <div className='xiangqi-coordinate-note'>
            <span>{t('起点虚线，落点实线')}</span>
            <span>{t('双方路数均从各自右侧起算')}</span>
          </div>
          <section className='xiangqi-captures' aria-label={t('吃子记录')}>
            <div className='xiangqi-history-heading'>
              <h3>{t('吃子记录')}</h3>
              {state.plies > history.length && (
                <span className='xiangqi-captures-note'>
                  {t('仅统计已记录的吃子')}
                </span>
              )}
            </div>
            <div className='xiangqi-captured-sides'>
              {['red', 'black'].map((color) => (
                <div
                  key={color}
                  className={`xiangqi-captured-side is-${color}`}
                >
                  <div className='xiangqi-captured-heading'>
                    <span className='xiangqi-move-side'>
                      <i aria-hidden />
                      {color === 'red' ? t('红方吃子') : t('黑方吃子')}
                    </span>
                    <span>
                      {t('共 {{count}} 子', { count: captures[color].length })}
                    </span>
                  </div>
                  {captures[color].length > 0 ? (
                    <ol className='xiangqi-captured-pieces'>
                      {captures[color].map((entry) => {
                        const description = t(
                          '第 {{round}} 回合：{{move}}，吃{{piece}}',
                          {
                            round: Math.ceil(entry.ply / 2),
                            move: xiangqiMoveLabel(entry),
                            piece: PIECE_GLYPHS[entry.captured],
                          },
                        );
                        return (
                          <li
                            key={entry.ply}
                            title={description}
                            aria-label={description}
                          >
                            <span
                              className={`xiangqi-captured-piece is-${color === 'red' ? 'black' : 'red'}`}
                            >
                              {PIECE_GLYPHS[entry.captured]}
                            </span>
                          </li>
                        );
                      })}
                    </ol>
                  ) : (
                    <p className='xiangqi-captured-empty'>{t('尚无吃子')}</p>
                  )}
                </div>
              ))}
            </div>
          </section>
          <section className='xiangqi-history' aria-label={t('行棋记录')}>
            <div className='xiangqi-history-heading'>
              <h3>{t('行棋记录')}</h3>
              <div className='xiangqi-history-actions'>
                <span>{t('共 {{count}} 步', { count: history.length })}</span>
                <Button
                  size='small'
                  theme='borderless'
                  icon={<Copy size={14} />}
                  disabled={history.length === 0}
                  onClick={copyRecord}
                >
                  {t('复制棋谱')}
                </Button>
              </div>
            </div>
            <p className='xiangqi-notation-note'>
              {t('红方用中文数字，黑方用阿拉伯数字，进退以行棋方为准')}
            </p>
            {state.plies > history.length && (
              <p className='xiangqi-history-note'>
                {history.length > 0
                  ? t('仅显示第 {{ply}} 步起的记录', { ply: history[0].ply })
                  : t('早期行棋未记录')}
              </p>
            )}
            {history.length > 0 ? (
              <div className='xiangqi-history-scroll' ref={historyScroll}>
                <table className='xiangqi-history-table'>
                  <thead>
                    <tr>
                      <th scope='col'>{t('回合')}</th>
                      <th scope='col' className='is-red'>
                        {t('红方')}
                      </th>
                      <th scope='col' className='is-black'>
                        {t('黑方')}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {visibleRounds.map((round) => (
                      <tr
                        key={round.number}
                        className={
                          round.number === Math.ceil(state.plies / 2)
                            ? 'is-current'
                            : undefined
                        }
                      >
                        <th scope='row'>{round.number}</th>
                        {['red', 'black'].map((color) => {
                          const entry = round[color];
                          return (
                            <td key={color} className={`is-${color}`}>
                              {entry ? (
                                <>
                                  <span
                                    className='xiangqi-history-notation'
                                    title={`${squareLabel(entry.from)} → ${squareLabel(entry.to)}`}
                                  >
                                    {xiangqiMoveLabel(entry)}
                                  </span>
                                  {entry.captured && (
                                    <span className='xiangqi-history-capture'>
                                      {t('吃{{piece}}', {
                                        piece: PIECE_GLYPHS[entry.captured],
                                      })}
                                    </span>
                                  )}
                                </>
                              ) : (
                                <span className='xiangqi-move-empty'>—</span>
                              )}
                            </td>
                          );
                        })}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className='xiangqi-history-note'>{t('暂无行棋记录')}</p>
            )}
            {canCollapse && (
              <div className='xiangqi-history-more'>
                {!showAllRounds && (
                  <span className='xiangqi-history-note'>
                    {t('只显示最近 {{count}} 回合', { count: RECENT_ROUNDS })}
                  </span>
                )}
                <Button
                  size='small'
                  theme='borderless'
                  onClick={() => setShowAllRounds((shown) => !shown)}
                >
                  {showAllRounds
                    ? t('只看最近 {{count}} 回合', { count: RECENT_ROUNDS })
                    : t('展开全部 {{count}} 回合', { count: rounds.length })}
                </Button>
              </div>
            )}
          </section>
        </>
      )}
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
        t('同一局面出现三次：一方一直在将军时长将的一方判负，否则和棋'),
        t('连续 120 步（双方合计）没有吃子，或总步数达到 300 步，也判和'),
        t(
          '每人每局可以免费悔棋 3 次，之后每次 1 个游戏币；房间对局要对方同意，人机对战由电脑直接同意',
        ),
        t(
          '轮到自己时可以向对手提议和棋，对手同意就按和棋结算；人机对战里电脑只在没占优时同意',
        ),
        t(
          '人机对战里可以让电脑提示一步：每人每局免费 3 次，之后每次 1 个游戏币；押注对局不提供提示',
        ),
      ]}
      renderBoard={({
        state,
        sessionId,
        roomId,
        seat,
        canMove,
        busy,
        over,
        hint,
        hintStatus,
        onHint,
        onMove,
        onUndo,
        onDraw,
      }) => (
        <XiangqiBoard
          state={state}
          gameId={sessionId || (roomId != null ? `room:${roomId}` : undefined)}
          seat={seat}
          canMove={canMove}
          busy={busy}
          over={over}
          hint={hint}
          hintStatus={hintStatus}
          onHint={onHint}
          onMove={onMove}
          onUndo={onUndo}
          onDraw={onDraw}
        />
      )}
    />
  );
};

export default Xiangqi;
