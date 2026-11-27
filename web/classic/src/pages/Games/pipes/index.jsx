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
import { Lock, LockOpen, RotateCcw, Star, Undo2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import {
  playSkillFeedback,
  SkillSide,
  skillResultMessage,
} from '../common/skill';
import { LocalBestBox, localBestNote } from '../common/LocalBest';
import { starsFor } from '../common/LogicPuzzle';
import { useLocalBest } from '../common/useLocalBest';
import { playSound } from '../common/sound';
import { useStepPuzzle } from '../common/useStepPuzzle';
import { PlayTurnstile } from '../common/usePlayTurnstile';
import '../common/logic-puzzles.css';
import './pipes.css';

// 管口方向的位与服务端 pkg/pipes 一致：上 1、右 2、下 4、左 8；后两项是这个管口在格子边上的坐标。
const ARMS = [
  [1, 50, 0],
  [2, 100, 50],
  [4, 50, 100],
  [8, 0, 50],
];

// 一格水管：从格子中心向每个管口画一段，水源画成水塔，只有一个管口的格子是出水口。
const PipeTile = ({ tile, source }) => {
  const arms = ARMS.filter(([bit]) => tile & bit);
  return (
    <svg viewBox='0 0 100 100' aria-hidden='true' className='pipes-svg'>
      {arms.map(([bit, x, y]) => (
        <line key={bit} className='pipes-arm' x1='50' y1='50' x2={x} y2={y} />
      ))}
      {arms.map(([bit, x, y]) => (
        <line key={bit} className='pipes-core' x1='50' y1='50' x2={x} y2={y} />
      ))}
      <circle className='pipes-joint' cx='50' cy='50' r='11' />
      {source ? (
        <>
          <circle className='pipes-source' cx='50' cy='50' r='27' />
          <circle className='pipes-source-ring' cx='50' cy='50' r='14' />
        </>
      ) : (
        arms.length === 1 && (
          <circle className='pipes-outlet' cx='50' cy='50' r='18' />
        )
      )}
    </svg>
  );
};

export default function Pipes() {
  const { t } = useTranslation();
  const names = ['5×5', '7×7', '9×9'];
  const { bests, record } = useLocalBest('pipes');
  const [finish, setFinish] = useState(null);
  // 刚转过的一格与方向，用来播放转动动画；seq 让同一格连转时动画重新开始。
  const [spin, setSpin] = useState({ cell: -1, clockwise: true, seq: 0 });
  // 锁定只是页面上的记号，防止误转已经接好的格子，不提交给服务端。按对局编号记，换一局自动失效。
  const [locks, setLocks] = useState({ session: 0, cells: [] });
  const [lockMode, setLockMode] = useState(false);
  const pendingRef = useRef(null);

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
  } = useStepPuzzle('pipes', {
    onStep: (current, next) => {
      const pending = pendingRef.current;
      pendingRef.current = null;
      const sameRound =
        current && next && current.session_id === next.session_id;
      if (!sameRound) {
        if (next?.status === 'playing') playSound('start');
        return;
      }
      if (next.moves < current.moves) {
        playSound('click');
      } else if (next.moves > current.moves) {
        if (pending && pending.cell === next.last)
          setSpin((prev) => ({
            cell: pending.cell,
            clockwise: pending.clockwise,
            seq: prev.seq + 1,
          }));
        const litBefore = (current.lit || []).filter(Boolean).length;
        const litAfter = (next.lit || []).filter(Boolean).length;
        playSound(litAfter > litBefore ? 'merge' : 'rotate');
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
              stars: starsFor(next.moves, next.par),
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

  const size = round?.size || 5;
  const tiles = round?.tiles || [];
  const lit = round?.lit || [];
  const total = size * size;
  const litCount = lit.filter(Boolean).length;
  const litPct = total ? Math.round((litCount / total) * 100) : 0;
  const lockedCells = locks.session === round?.session_id ? locks.cells : [];
  const source = round?.source ?? -1;
  const sourceX = source % size;
  const sourceY = Math.floor(source / size);

  const toggleLock = (cell) => {
    const cells = lockedCells.includes(cell)
      ? lockedCells.filter((item) => item !== cell)
      : [...lockedCells, cell];
    setLocks({ session: round?.session_id, cells });
  };

  const turn = (cell, clockwise) => {
    if (!playing || busy) return;
    if (lockedCells.includes(cell)) {
      playSound('error');
      return;
    }
    pendingRef.current = { cell, clockwise };
    send('move', { cell, dir: clockwise ? 0 : 1 });
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

  return (
    <GameLayout
      title={t('接水管')}
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
                    t(
                      '点击一格水管顺时针转 90 度，右键逆时针转；水从中央的水源流出',
                    ),
                    t(
                      '所有管口都要和相邻管口对上，不能朝向棋盘外，并让每一格都通上水才算过关',
                    ),
                    t(
                      '每一盘都由一整张管网打乱而来，保证有解；任何满足条件的接法都算过关',
                    ),
                    t(
                      '打开锁定模式或按住 Shift 点击，可以锁住已经接好的格子，防止误转',
                    ),
                    t('用不超过标准步数过关得三星'),
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
      <div className='games-stage logic-stage pipes-stage'>
        <div className='logic-heading'>
          <span className='logic-eyebrow'>{t('管网修复')}</span>
          <h2>{t('接水管')}</h2>
          <p>{t('转动管道，让清水流到每一个角落')}</p>
        </div>
        <div className='logic-stats'>
          <span>
            {t('步数')}
            <strong>{round ? round.moves : '-'}</strong>
          </span>
          <span>
            {t('标准步数')}
            <strong>{round ? round.par : '-'}</strong>
          </span>
          <span>
            {t('已通水')}
            <strong>{round ? `${litPct}%` : '-'}</strong>
          </span>
          <span>
            {t('漏水管口')}
            <strong className={round?.leaks > 0 ? 'pipes-leaky' : ''}>
              {round ? round.leaks : '-'}
            </strong>
          </span>
        </div>

        {round && (
          <div
            className={`pipes-board${round.won ? ' is-won' : ''}${lockMode ? ' is-lock-mode' : ''}`}
            style={{ '--pipes-size': size }}
            aria-label={t('接水管棋盘')}
          >
            {tiles.map((tile, cell) => {
              const x = cell % size;
              const y = Math.floor(cell / size);
              const locked = lockedCells.includes(cell);
              const spinning = spin.cell === cell;
              return (
                <button
                  key={cell}
                  type='button'
                  className={`pipes-cell${lit[cell] ? ' is-lit' : ''}${locked ? ' is-locked' : ''}`}
                  style={{
                    '--pipes-delay': `${(Math.abs(x - sourceX) + Math.abs(y - sourceY)) * 45}ms`,
                  }}
                  disabled={!playing}
                  aria-pressed={locked}
                  aria-label={t('第 {{row}} 行第 {{col}} 列，{{state}}', {
                    row: y + 1,
                    col: x + 1,
                    state: lit[cell] ? t('已通水') : t('未通水'),
                  })}
                  onClick={(event) => {
                    if (lockMode || event.shiftKey) toggleLock(cell);
                    else turn(cell, true);
                  }}
                  onContextMenu={(event) => {
                    event.preventDefault();
                    if (lockMode) toggleLock(cell);
                    else turn(cell, false);
                  }}
                >
                  <span
                    key={spinning ? `spin-${spin.seq}` : 'still'}
                    className={`pipes-tile${spinning ? (spin.clockwise ? ' spin-cw' : ' spin-ccw') : ''}`}
                  >
                    <PipeTile tile={tile} source={cell === source} />
                  </span>
                  {locked && <Lock className='pipes-lock-icon' size={12} />}
                </button>
              );
            })}
          </div>
        )}

        {playing && (
          <p className='logic-help' aria-live='polite'>
            {busy
              ? t('正在保存…')
              : lockMode
                ? t('锁定模式：点击格子锁住或解锁')
                : t('已通水 {{lit}} / {{total}} 格', {
                    lit: litCount,
                    total,
                  })}
          </p>
        )}

        <div className='logic-controls'>
          {playing ? (
            <>
              <Button
                icon={lockMode ? <Lock size={15} /> : <LockOpen size={15} />}
                theme={lockMode ? 'solid' : 'light'}
                aria-pressed={lockMode}
                onClick={() => setLockMode((value) => !value)}
              >
                {t('锁定模式')}
              </Button>
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
                aria-label={t('难度')}
                optionList={names.map((name, index) => ({
                  value: index + 1,
                  label: name,
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
            {shownFinish.stars > 0 && (
              <span
                className='logic-stars'
                aria-label={t('{{count}} 星', { count: shownFinish.stars })}
              >
                {[1, 2, 3].map((index) => (
                  <Star
                    key={index}
                    size={26}
                    className={index <= shownFinish.stars ? 'is-lit' : ''}
                  />
                ))}
              </span>
            )}
            <span>
              {shownFinish.seconds > 0
                ? t('全部通水，用了 {{moves}} 步、{{seconds}} 秒', {
                    moves: shownFinish.moves,
                    seconds: shownFinish.seconds,
                  })
                : t('全部通水，用了 {{moves}} 步', {
                    moves: shownFinish.moves,
                  })}{' '}
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
