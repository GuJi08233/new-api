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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
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
import '../common/logic-puzzles.css';
import './flood.css';

const PALETTE = [
  '#ef4444', // 朱红
  '#f59e0b', // 琥珀
  '#10b981', // 翠绿
  '#3b82f6', // 钴蓝
  '#8b5cf6', // 紫罗兰
  '#ec4899', // 桃粉
];

function collectRegion(board, size) {
  if (!board?.length || !size) return new Set();
  const target = board[0];
  const visited = new Set([0]);
  const queue = [0];
  for (let head = 0; head < queue.length; head++) {
    const idx = queue[head];
    const x = idx % size;
    const y = Math.floor(idx / size);
    const neighbors = [];
    if (x > 0) neighbors.push(idx - 1);
    if (x + 1 < size) neighbors.push(idx + 1);
    if (y > 0) neighbors.push(idx - size);
    if (y + 1 < size) neighbors.push(idx + size);
    for (const next of neighbors) {
      if (!visited.has(next) && board[next] === target) {
        visited.add(next);
        queue.push(next);
      }
    }
  }
  return visited;
}

function previewGain(board, size, region, color) {
  if (!board?.length || board[0] === color) return 0;
  const nextBoard = [...board];
  region.forEach((idx) => {
    nextBoard[idx] = color;
  });
  return collectRegion(nextBoard, size).size - region.size;
}

export default function Flood() {
  const { t } = useTranslation();
  const names = ['8×8', '10×10', '12×12'];
  const { bests, record } = useLocalBest('flood');
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
  } = useStepPuzzle('flood', {
    onStep: (current, next) => {
      const sameRound =
        current && next && current.session_id === next.session_id;
      if (!sameRound) {
        if (next?.status === 'playing') playSound('start');
        return;
      }
      if (next.moves < current.moves) {
        playSound('click');
      } else if (next.moves > current.moves) {
        const gained = (next.captured || 0) - (current.captured || 0);
        playSound(gained >= 8 ? 'sparkle' : 'tick');
      }
      if (current.status !== 'playing' || !next.result) return;
      const won = !!next.won;
      const seconds = elapsedSeconds(next.session_id);
      setFinish(
        won
          ? {
              session: next.session_id,
              seconds,
              moves: next.moves,
              improved: record(next.difficulty, {
                time: seconds > 0 ? seconds : undefined,
                moves: next.moves,
              }),
            }
          : null,
      );
      playSkillFeedback(next.result, won);
    },
  });

  const board = round?.board || [];
  const size = round?.size || 8;
  const colors = round?.colors || PALETTE.length;
  const currentColor = board.length ? board[0] : -1;
  const totalCells = size * size;
  const region = useMemo(() => collectRegion(board, size), [board, size]);
  const captured = round?.captured ?? region.size;
  const progressPct = totalCells ? Math.round((captured / totalCells) * 100) : 0;

  const gains = useMemo(
    () =>
      Array.from({ length: colors }, (_, color) =>
        previewGain(board, size, region, color),
      ),
    [board, size, region, colors],
  );

  const pickColor = useCallback(
    (color) => {
      if (!playing || busy || color < 0 || color >= colors) return;
      if (color === currentColor) {
        playSound('error');
        return;
      }
      send('move', { color });
    },
    [playing, busy, colors, currentColor, send],
  );

  useEffect(() => {
    if (!playing) return;
    const onKeyDown = (event) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const idx = Number(event.key) - 1;
      if (idx >= 0 && idx < colors) {
        event.preventDefault();
        pickColor(idx);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [playing, colors, pickColor]);

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

  return (
    <GameLayout
      title={t('色块漫染')}
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
                    t('以左上角带白点的连通区域为起点，每次选择一种新颜色将整片区域染色'),
                    t('染色后会自动吞并所有上下左右相邻的同色色块'),
                    t('在步数上限内把整张棋盘染成同一颜色即可过关，每一盘都经过校验保证有解'),
                    t('可直接点击棋盘上的任意色块、下方色板，或按数字键 1–6 快速选色'),
                  ]}
                />
              </SideBox>
            }
            after={
              <LocalBestBox
                bests={bests}
                levels={names.map((name, index) => ({ key: index + 1, name }))}
              />
            }
          />
        )
      }
    >
      <div className='games-stage logic-stage flood-stage'>
        <div className='logic-heading'>
          <span className='logic-eyebrow'>{t('策略漫染')}</span>
          <h2>{t('色块漫染')}</h2>
          <p>{t('步步扩散，一色统合全盘')}</p>
        </div>
        <div className='logic-stats'>
          <span>
            {t('步数')}
            <strong>
              {round ? `${round.moves}/${round.max_moves}` : '-'}
            </strong>
          </span>
          <span>
            {t('剩余步数')}
            <strong
              className={
                round && round.moves_left <= 3 && !round.won
                  ? 'flood-low-moves'
                  : ''
              }
            >
              {round ? round.moves_left : '-'}
            </strong>
          </span>
          <span>
            {t('已占领')}
            <strong>{round ? `${progressPct}%` : '-'}</strong>
          </span>
        </div>

        {round && (
          <>
            <div className='flood-progress-wrap'>
              <div className='flood-progress-meta'>
                <span>
                  {t('领地进度 {{captured}} / {{total}} 格', {
                    captured,
                    total: totalCells,
                  })}
                </span>
                <span>{progressPct}%</span>
              </div>
              <div className='flood-progress-bar'>
                <div
                  className='flood-progress-fill'
                  style={{ width: `${progressPct}%` }}
                />
              </div>
            </div>

            <div
              className='flood-board'
              style={{ gridTemplateColumns: `repeat(${size}, minmax(0, 1fr))` }}
            >
              {board.map((color, idx) => (
                <button
                  key={idx}
                  type='button'
                  className={`flood-cell${region.has(idx) ? ' is-captured' : ''}${idx === 0 ? ' is-origin' : ''}`}
                  style={{ backgroundColor: PALETTE[color] || '#94a3b8' }}
                  disabled={!playing || busy || color === currentColor}
                  aria-label={t('选择颜色 {{index}}', { index: color + 1 })}
                  onClick={() => pickColor(color)}
                />
              ))}
            </div>

            <div className='flood-palette'>
              {Array.from({ length: colors }, (_, color) => {
                const isCurrent = color === currentColor;
                const gain = gains[color];
                return (
                  <button
                    key={color}
                    type='button'
                    className={`flood-swatch${isCurrent ? ' is-current' : ''}`}
                    style={{ backgroundColor: PALETTE[color] }}
                    disabled={!playing || busy || isCurrent}
                    onClick={() => pickColor(color)}
                  >
                    <span className='flood-swatch-key'>{color + 1}</span>
                    <span className='flood-swatch-gain'>
                      {isCurrent ? t('当前') : `+${gain}`}
                    </span>
                  </button>
                );
              })}
            </div>
          </>
        )}

        {error && <StageMessage type='error'>{error}</StageMessage>}
        {result && (
          <StageMessage type={round.won ? 'success' : 'info'}>
            {result}
            {shownFinish &&
              localBestNote(shownFinish.improved, {
                time: shownFinish.seconds,
                moves: shownFinish.moves,
                t,
              })}
          </StageMessage>
        )}

        <div className='logic-controls'>
          {playing ? (
            <>
              <Button
                icon={<Undo2 size={15} />}
                disabled={busy || !round?.moves}
                onClick={() => send('undo')}
              >
                {t('撤销')}
              </Button>
              <Button
                icon={<RotateCcw size={15} />}
                disabled={busy || !round?.moves}
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
                onChange={setDifficulty}
                disabled={busy}
                optionList={names.map((name, index) => {
                  const level = info?.levels?.[index + 1];
                  return {
                    value: index + 1,
                    label: level
                      ? t('{{name}} · 限 {{moves}} 步', {
                          name,
                          moves: level.max_moves,
                        })
                      : name,
                  };
                })}
              />
              <PlayTurnstile turnstile={turnstile} />
              <Button
                theme='solid'
                loading={busy}
                disabled={!checked || turnstile.blocked}
                onClick={() => send('start')}
              >
                {round ? t('再来一局') : t('开始游戏')}
              </Button>
              {!checked && (
                <Button onClick={restore} disabled={busy}>
                  {t('重新读取对局')}
                </Button>
              )}
            </>
          )}
        </div>
      </div>
    </GameLayout>
  );
}
