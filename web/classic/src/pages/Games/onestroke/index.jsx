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

import React, { useRef, useState } from 'react';
import { Button, Modal, Select } from '@douyinfe/semi-ui';
import { RotateCcw, Undo2 } from 'lucide-react';
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
import { playSound } from '../common/sound';
import { useStepPuzzle } from '../common/useStepPuzzle';
import { PlayTurnstile } from '../common/usePlayTurnstile';
import { adjacent, planStroke } from './logic';
import '../common/logic-puzzles.css';
import './onestroke.css';

const SIZES = [5, 6, 7];

export default function Onestroke() {
  const { t } = useTranslation();
  const names = ['5×5', '6×6', '7×7'];
  const { bests, record } = useLocalBest('onestroke');
  // 手指或鼠标正在画的路线(还没提交)；为空时显示服务端的路线。
  const [draft, setDraft] = useState(null);
  const drawingRef = useRef(false);
  const [finish, setFinish] = useState(null);
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
  } = useStepPuzzle('onestroke', {
    onStep: (current, next) => {
      const sameRound =
        current && next && current.session_id === next.session_id;
      if (!sameRound) {
        if (next?.status === 'playing') playSound('start');
        return;
      }
      if (current.status !== 'playing' || !next.result) return;
      const won = !!next.won;
      const seconds = elapsedSeconds(next.session_id);
      setFinish(
        won
          ? {
              session: next.session_id,
              seconds,
              improved:
                seconds > 0 && record(next.difficulty, { time: seconds }),
            }
          : null,
      );
      playSkillFeedback(next.result, won);
    },
  });

  const size = round?.size || SIZES[difficulty - 1];
  const stones = round?.stones || Array(size * size).fill(false);
  const committed = round?.path || [];
  const path = draft || committed;
  const open = round?.open || 0;
  const order = new Map(path.map((cell, index) => [cell, index]));
  const head = path[path.length - 1];

  // 把正在画的路线延伸到 cell：走到末端相邻的空格就加上，退回上一格就收回一格。
  const extend = (cell, current) => {
    if (cell === undefined || cell === current[current.length - 1])
      return current;
    if (current.length > 1 && cell === current[current.length - 2]) {
      playSound('tick', { pitch: 0.8 });
      return current.slice(0, -1);
    }
    if (
      stones[cell] ||
      current.includes(cell) ||
      !adjacent(size, current[current.length - 1], cell)
    )
      return current;
    playSound('tick', { pitch: 1 + current.length / open });
    return [...current, cell];
  };

  const cellAt = (event) => {
    const node = document
      .elementFromPoint(event.clientX, event.clientY)
      ?.closest?.('[data-cell]');
    return node ? Number(node.dataset.cell) : undefined;
  };

  const commit = async (next) => {
    const plan = planStroke(committed, next);
    if (!plan.undo && !plan.cells.length) {
      setDraft(null);
      return;
    }
    if (plan.undo && !(await send('undo', { steps: plan.undo }))) {
      setDraft(null);
      return;
    }
    if (plan.cells.length) {
      if (!plan.undo) playSound('place');
      await send('move', { cells: plan.cells });
    }
    setDraft(null);
  };

  const onPointerDown = (event) => {
    if (!playing || busy) return;
    const cell = cellAt(event);
    if (cell === undefined) return;
    // 点在已经走过的格子上：退回到这一格，接着从这里画。
    if (order.has(cell)) {
      drawingRef.current = true;
      setDraft(committed.slice(0, order.get(cell) + 1));
      return;
    }
    if (!adjacent(size, head, cell)) return;
    drawingRef.current = true;
    setDraft(extend(cell, committed));
  };
  const onPointerMove = (event) => {
    if (!drawingRef.current) return;
    const cell = cellAt(event);
    setDraft((current) => extend(cell, current || committed));
  };
  const onPointerUp = () => {
    if (!drawingRef.current) return;
    drawingRef.current = false;
    if (draft) commit(draft);
  };

  const confirm = (action) =>
    Modal.confirm({
      title: action === 'reset' ? t('重置当前棋盘') : t('结束本局'),
      content:
        action === 'reset'
          ? t('回到本局初始盘面，已走步骤会清空。')
          : t('结束后按当前成绩结算，不能继续。'),
      onOk: () => send(action),
    });
  const shownFinish =
    finish && finish.session === round?.session_id ? finish : null;
  const result =
    round?.result &&
    skillResultMessage(round.result, t, info?.symbol, { byLevel: true });

  // 路线的折线：格子中心按百分比排布，SVG 的视口是 size × size。
  const points = path
    .map((cell) => `${(cell % size) + 0.5},${Math.floor(cell / size) + 0.5}`)
    .join(' ');
  const stuck = !draft && round?.stuck;

  return (
    <GameLayout
      title={t('一笔画')}
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
                    t('从发光的起点出发，按住拖动，经过的格子连成一条线'),
                    t('只能走上下左右相邻、没走过的空格，石头不能走'),
                    t('拖回上一格可以收回，点一下走过的格子就退回那里'),
                    t('一笔走遍所有空格就过关，每一盘都至少有一种走法'),
                  ]}
                />
              </SideBox>
            }
            after={
              <LocalBestBox
                bests={bests}
                levels={names.map((name, index) => ({ key: index + 1, name }))}
                columns={['time']}
              />
            }
          />
        )
      }
    >
      <div className='games-stage logic-stage onestroke-stage'>
        <div className='logic-heading'>
          <span className='logic-eyebrow'>{t('路线挑战')}</span>
          <h2>{t('一笔画')}</h2>
          <p>{t('不回头，不重复，一笔走到底')}</p>
        </div>
        <div className='logic-stats'>
          <span>
            {t('已走')}
            <strong>
              {path.length}/{open || '-'}
            </strong>
          </span>
          <span>
            {t('难度')}
            <strong>{names[difficulty - 1]}</strong>
          </span>
        </div>
        <div
          className={`onestroke-board ${round?.won ? 'is-won' : ''} ${stuck ? 'is-stuck' : ''}`}
          style={{ '--stroke-size': size }}
          role='group'
          aria-label={t('一笔画棋盘')}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
          onPointerLeave={onPointerUp}
        >
          {stones.map((stone, cell) => {
            const index = order.get(cell);
            const classes = ['onestroke-cell'];
            if (stone) classes.push('is-stone');
            if (index !== undefined) classes.push('is-path');
            if (index === 0) classes.push('is-start');
            if (cell === head) classes.push('is-head');
            if (
              playing &&
              !stone &&
              index === undefined &&
              adjacent(size, head, cell)
            )
              classes.push('is-next');
            return (
              <span
                key={cell}
                data-cell={stone ? undefined : cell}
                className={classes.join(' ')}
                style={
                  index !== undefined
                    ? {
                        '--stroke-hue': 190 + (index / Math.max(open, 1)) * 140,
                      }
                    : undefined
                }
              />
            );
          })}
          <svg
            className='onestroke-line'
            viewBox={`0 0 ${size} ${size}`}
            preserveAspectRatio='none'
            aria-hidden
          >
            <polyline points={points} />
          </svg>
        </div>
        {playing && (
          <p className='logic-help' aria-live='polite'>
            {busy
              ? t('正在保存…')
              : stuck
                ? t('走进死路了，点一下走过的格子退回去')
                : t('按住拖动画线，松手后保存')}
          </p>
        )}
        <div className='logic-controls'>
          {playing ? (
            <>
              <Button
                icon={<Undo2 size={16} />}
                disabled={busy || committed.length < 2}
                onClick={() => send('undo')}
              >
                {t('撤销')}
              </Button>
              <Button
                icon={<RotateCcw size={16} />}
                disabled={busy || committed.length < 2}
                onClick={() => confirm('reset')}
              >
                {t('重置')}
              </Button>
              <Button disabled={busy} onClick={() => confirm('finish')}>
                {t('结束本局')}
              </Button>
            </>
          ) : (
            <>
              <Select
                value={difficulty}
                disabled={busy}
                onChange={setDifficulty}
                aria-label={t('难度')}
                optionList={names.map((name, index) => ({
                  label: name,
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
              {shownFinish.seconds > 0
                ? t('一笔画成，用时 {{seconds}} 秒', {
                    seconds: shownFinish.seconds,
                  })
                : t('一笔画成')}{' '}
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
