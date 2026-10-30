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
import './watersort.css';

// 试管里的颜色，按服务端的颜色编号(从 1 起)取。
const LIQUIDS = [
  '',
  '#ef4444',
  '#3b82f6',
  '#22c55e',
  '#eab308',
  '#a855f7',
  '#f97316',
  '#06b6d4',
  '#ec4899',
];
// 倾倒动画的时长，和 watersort.css 里的过渡一致。
const POUR_MS = 520;

const topRun = (tube) => {
  let n = 0;
  for (
    let i = tube.length - 1;
    i >= 0 && tube[i] === tube[tube.length - 1];
    i--
  )
    n++;
  return n;
};

// 能从 from 倒进 to 的层数，倒不了时为 0。规则与服务端一致，只用来提前挡住无效的点击。
const pourable = (tubes, from, to, capacity) => {
  if (from === to) return 0;
  const source = tubes[from];
  const target = tubes[to];
  if (!source.length || target.length >= capacity) return 0;
  if (target.length && target[target.length - 1] !== source[source.length - 1])
    return 0;
  return Math.min(topRun(source), capacity - target.length);
};

const isDone = (tube, capacity) =>
  tube.length === capacity && tube.every((layer) => layer === tube[0]);

export default function Watersort() {
  const { t } = useTranslation();
  const names = [t('4 种颜色'), t('6 种颜色'), t('8 种颜色')];
  const { bests, record } = useLocalBest('watersort');
  const [selected, setSelected] = useState(-1);
  // 正在播放的倾倒：倒出的试管、倒进的试管、颜色、位移(像素)，以及目标在右边(向右倾倒)还是左边。
  const [pouring, setPouring] = useState(null);
  const [finish, setFinish] = useState(null);
  const tubeRefs = useRef([]);
  const timerRef = useRef(0);
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
  } = useStepPuzzle('watersort', {
    onStep: (current, next) => {
      const sameRound =
        current && next && current.session_id === next.session_id;
      if (!sameRound) {
        if (next?.status === 'playing') playSound('start');
        return;
      }
      if (next.moves < current.moves) playSound('click');
      else if (next.moves > current.moves) {
        const capacity = next.capacity || 4;
        const done = (tubes) =>
          tubes.filter((tube) => isDone(tube, capacity)).length;
        if (done(next.tubes) > done(current.tubes)) playSound('sparkle');
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

  useEffect(() => () => clearTimeout(timerRef.current), []);
  useEffect(() => setSelected(-1), [round?.session_id]);

  const capacity = round?.capacity || 4;
  const tubes = round?.tubes || [];

  const pour = (from, to) => {
    const source = tubeRefs.current[from]?.getBoundingClientRect();
    const target = tubeRefs.current[to]?.getBoundingClientRect();
    const tube = tubes[from];
    setPouring({
      from,
      to,
      color: LIQUIDS[tube[tube.length - 1]],
      dx: source && target ? target.left - source.left : 0,
      dy: source && target ? target.top - source.top : 0,
      rightward: target && source ? target.left > source.left : true,
    });
    playSound('pour');
    clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => setPouring(null), POUR_MS);
    send('move', { from, to });
  };

  const press = (index) => {
    if (!playing || busy || pouring) return;
    if (selected < 0) {
      if (!tubes[index].length || isDone(tubes[index], capacity)) {
        playSound('error');
        return;
      }
      playSound('tick');
      setSelected(index);
      return;
    }
    if (selected === index) {
      setSelected(-1);
      return;
    }
    if (!pourable(tubes, selected, index, capacity)) {
      // 倒不进去：改选这一支(如果它能倒出东西)。
      if (tubes[index].length && !isDone(tubes[index], capacity)) {
        playSound('tick');
        setSelected(index);
      } else playSound('error');
      return;
    }
    setSelected(-1);
    pour(selected, index);
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
  const doneCount = tubes.filter((tube) => isDone(tube, capacity)).length;
  const colorCount = tubes.reduce((n, tube) => n + tube.length, 0) / capacity;

  return (
    <GameLayout
      title={t('倒水排序')}
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
                    t('先点一支试管拿起来，再点另一支把顶上的颜色倒进去'),
                    t('只能倒进空试管，或者顶层颜色相同、还有空位的试管'),
                    t('顶上连着的同色会一起倒，倒不下的留在原处'),
                    t('每支试管只装一种颜色(或者空着)就过关，每一盘都有解'),
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
      <div className='games-stage logic-stage watersort-stage'>
        <div className='logic-heading'>
          <span className='logic-eyebrow'>{t('色彩实验室')}</span>
          <h2>{t('倒水排序')}</h2>
          <p>{t('一支试管，一种颜色')}</p>
        </div>
        <div className='logic-stats'>
          <span>
            {t('步数')}
            <strong>{round?.moves || 0}</strong>
          </span>
          <span>
            {t('已排好')}
            <strong>
              {doneCount}/{colorCount || '-'}
            </strong>
          </span>
          <span>
            {t('难度')}
            <strong>{names[difficulty - 1]}</strong>
          </span>
        </div>
        <div
          className={`watersort-rack ${round?.won ? 'is-won' : ''}`}
          role='group'
          aria-label={t('试管架')}
        >
          {tubes.map((tube, index) => {
            const done = isDone(tube, capacity);
            const lifted = selected === index;
            const tilting = pouring?.from === index;
            const receiving = pouring?.to === index;
            const classes = ['watersort-tube'];
            if (lifted) classes.push('is-selected');
            if (done) classes.push('is-done');
            if (tilting)
              classes.push(
                pouring.rightward ? 'is-pouring-right' : 'is-pouring-left',
              );
            if (receiving) classes.push('is-receiving');
            const style = tilting
              ? {
                  '--pour-x': `${pouring.dx + (pouring.rightward ? -26 : 26)}px`,
                  '--pour-y': `${pouring.dy - 70}px`,
                }
              : undefined;
            return (
              <button
                key={index}
                type='button'
                ref={(node) => {
                  tubeRefs.current[index] = node;
                }}
                className={classes.join(' ')}
                style={style}
                disabled={!playing}
                aria-pressed={lifted}
                aria-label={t('第 {{index}} 支试管，装了 {{count}} 层', {
                  index: index + 1,
                  count: tube.length,
                })}
                onClick={() => press(index)}
              >
                <span className='watersort-glass'>
                  {Array.from({ length: capacity }, (_, level) => {
                    const color = tube[level];
                    return (
                      <span
                        key={level}
                        className={`watersort-layer ${color ? 'is-filled' : ''} ${level === tube.length - 1 ? 'is-top' : ''}`}
                        style={
                          color ? { '--liquid': LIQUIDS[color] } : undefined
                        }
                      />
                    );
                  })}
                  {done && <span className='watersort-cork' aria-hidden />}
                </span>
                {receiving && pouring && (
                  <span
                    className='watersort-stream'
                    style={{ '--liquid': pouring.color }}
                    aria-hidden
                  />
                )}
              </button>
            );
          })}
        </div>
        {playing && (
          <p className='logic-help' aria-live='polite'>
            {busy
              ? t('正在保存…')
              : round?.stuck
                ? t('已经没有能倒的了，撤销几步或者重置再试')
                : selected >= 0
                  ? t('选好了，再点要倒进去的试管')
                  : t('点一支试管拿起来')}
          </p>
        )}
        <div className='logic-controls'>
          {playing ? (
            <>
              <Button
                icon={<Undo2 size={16} />}
                disabled={busy || !round.moves}
                onClick={() => {
                  setSelected(-1);
                  send('undo');
                }}
              >
                {t('撤销')}
              </Button>
              <Button
                icon={<RotateCcw size={16} />}
                disabled={busy || !round.moves}
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
                ? t('全部排好，用了 {{moves}} 步、{{seconds}} 秒', {
                    moves: shownFinish.moves,
                    seconds: shownFinish.seconds,
                  })
                : t('全部排好，用了 {{moves}} 步', {
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
