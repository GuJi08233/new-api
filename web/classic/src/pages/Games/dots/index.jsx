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
import { EDGES, SIZE, edgeEffect, edgeEnds } from './logic';
import './dots.css';

const EMPTY_EDGES = '0'.repeat(EDGES);
const EMPTY_OWNERS = '.'.repeat(SIZE * SIZE);
const EDGE_LIST = Array.from({ length: EDGES }, (_, edge) => edge);
const BOX_LIST = Array.from({ length: SIZE * SIZE }, (_, box) => box);
// 点与点的间距和棋盘留白(SVG 坐标)。
const GAP = 80;
const PAD = 24;
const VIEW = PAD * 2 + GAP * SIZE;
const at = ([r, c]) => [PAD + c * GAP, PAD + r * GAP];

// 视图里 first 是先走的座位，用蓝色；另一方用红色。格子的主人 a 是先走的一方，b 是后走的一方。
const colorOfSeat = (state, seat) => (state?.first === seat ? 'blue' : 'red');
const colorOfOwner = (owner) => (owner === 'a' ? 'blue' : 'red');

const DotsBoard = ({
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
  const edges = state?.edges || EMPTY_EDGES;
  const owners = state?.owners || EMPTY_OWNERS;
  const [pending, setPending] = useState(null);
  const [hover, setHover] = useState(-1);
  const waiting = pending && busy && pending.from === edges ? pending : null;
  const shown = waiting
    ? `${edges.slice(0, waiting.edge)}1${edges.slice(waiting.edge + 1)}`
    : edges;

  // 和上一次看到的局面比较：新连上的边描亮，新围下的格子配上得分音效。边变少说明换了一局或者悔棋，不算新边。
  const [seen, setSeen] = useState({ edges, owners, fresh: [] });
  if (seen.edges !== edges) {
    const grew = edges.split('1').length >= seen.edges.split('1').length;
    setSeen({
      edges,
      owners,
      fresh: grew
        ? EDGE_LIST.filter((e) => edges[e] === '1' && seen.edges[e] === '0')
        : [],
      scored:
        grew &&
        owners.replace(/\./g, '').length >
          seen.owners.replace(/\./g, '').length,
    });
  }
  useEffect(() => {
    if (!seen.fresh.length) return;
    playSound(seen.scored ? 'score' : 'place');
  }, [seen]);

  const shownHint = waiting ? null : currentHint(hint, state);
  const playable = canMove && !waiting && !over;
  const mine = colorOfSeat(state, seat);
  const effect = hover >= 0 && playable ? edgeEffect(shown, hover) : null;

  const draw = (edge) => {
    if (!playable || shown[edge] === '1') return;
    setPending({ edge, from: edges });
    setHover(-1);
    onMove({ edge });
  };

  return (
    <div className='dots-board-wrap'>
      <svg
        className={`dots-board is-${mine}`}
        viewBox={`0 0 ${VIEW} ${VIEW}`}
        role='group'
        aria-label={t('点格棋棋盘')}
        onMouseLeave={() => setHover(-1)}
      >
        {BOX_LIST.map((box) => {
          const owner = owners[box];
          if (owner === '.') return null;
          const [x, y] = at([Math.floor(box / SIZE), box % SIZE]);
          const fresh = state?.claimed?.includes(box);
          return (
            <g
              key={`b${box}`}
              className={`dots-box is-${colorOfOwner(owner)} ${fresh ? 'is-new' : ''}`}
            >
              <rect
                x={x + 4}
                y={y + 4}
                width={GAP - 8}
                height={GAP - 8}
                rx={8}
              />
              <text x={x + GAP / 2} y={y + GAP / 2}>
                {owner === 'a' ? '●' : '■'}
              </text>
            </g>
          );
        })}
        {EDGE_LIST.map((edge) => {
          const [[r1, c1], [r2, c2]] = edgeEnds(edge);
          const [x1, y1] = at([r1, c1]);
          const [x2, y2] = at([r2, c2]);
          const on = shown[edge] === '1';
          const classes = ['dots-edge'];
          if (on) classes.push('is-on');
          if (on && seen.fresh.includes(edge)) classes.push('is-fresh');
          if (on && (state?.last === edge || waiting?.edge === edge))
            classes.push('is-last');
          if (!on && hover === edge) classes.push('is-hover');
          if (!on && shownHint?.x === edge) classes.push('is-hint');
          const open = playable && !on;
          return (
            <g
              key={`e${edge}`}
              className={classes.join(' ')}
              role='button'
              tabIndex={open ? 0 : -1}
              aria-disabled={!open}
              aria-label={t(
                '连第 {{row}} 行第 {{col}} 个点到第 {{row2}} 行第 {{col2}} 个点',
                {
                  row: r1 + 1,
                  col: c1 + 1,
                  row2: r2 + 1,
                  col2: c2 + 1,
                },
              )}
              onClick={() => draw(edge)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault();
                  draw(edge);
                }
              }}
              onMouseEnter={() => open && setHover(edge)}
              onFocus={() => open && setHover(edge)}
              onBlur={() => setHover(-1)}
            >
              <line className='dots-edge-hit' x1={x1} y1={y1} x2={x2} y2={y2} />
              <line
                className='dots-edge-line'
                x1={x1}
                y1={y1}
                x2={x2}
                y2={y2}
              />
            </g>
          );
        })}
        {Array.from({ length: (SIZE + 1) * (SIZE + 1) }, (_, i) => {
          const [x, y] = at([Math.floor(i / (SIZE + 1)), i % (SIZE + 1)]);
          return (
            <circle key={`d${i}`} className='dots-dot' cx={x} cy={y} r={6} />
          );
        })}
      </svg>
      <p className='dots-effect' aria-live='polite'>
        {effect?.capture
          ? t('这条边能围下 {{count}} 个格子，还能接着走', {
              count: effect.capture,
            })
          : effect?.gift
            ? t('小心：这条边会让对方围下格子')
            : ''}
      </p>
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

// 正常下完的一局为什么结束：所有格子都有了主人，按格子多少定胜负。
function dotsResultNote(state, t) {
  if (!state?.over) return '';
  const [a, b] = state.scores || [0, 0];
  return state.winner < 0
    ? t('格子 {{a}} 比 {{b}}，和棋', { a, b })
    : t('格子 {{a}} 比 {{b}}', { a, b });
}

const Dots = () => {
  const { t } = useTranslation();
  // 座位称呼：先走的一方用蓝色，另一方用红色；后面跟着已经占下的格子数。
  const seatLabels = (state) => {
    if (!state) return [t('待定'), t('待定')];
    return [0, 1].map((seat) => {
      const color = colorOfSeat(state, seat);
      return (
        <React.Fragment key={seat}>
          <span className={`dots-seat-mark is-${color}`} />
          {color === 'blue' ? t('蓝方') : t('红方')}
          <strong className='dots-seat-score'>
            {state.scores?.[seat] ?? 0}
          </strong>
        </React.Fragment>
      );
    });
  };
  return (
    <PvpGame
      gameKey='dots'
      title={t('点格棋')}
      className='dots-page'
      seatLabels={seatLabels}
      resultNote={({ state }) => dotsResultNote(state, t)}
      rules={[
        t('4 × 4 个格子，双方轮流在相邻两点之间连一条边'),
        t('连上一个格子的第四条边就占下这个格子，并且接着再连一条'),
        t('所有边连完后占格多的一方获胜，一样多为和棋'),
        t('鼠标停在边上会提示这一步能不能占格、会不会送格'),
        t('每局前 3 次悔棋免费，之后每次 1 游戏币；房间里悔棋要对方同意'),
        t('人机对战可以请电脑分析下一步，每局前 3 次提示免费'),
      ]}
      renderBoard={(props) => <DotsBoard {...props} />}
    />
  );
};

export default Dots;
