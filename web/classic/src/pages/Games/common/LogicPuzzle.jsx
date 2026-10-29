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

import React, { useMemo, useRef, useState } from 'react';
import { Button, Modal, Select } from '@douyinfe/semi-ui';
import { Lightbulb, RotateCcw, Star, Undo2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import GameLayout from './GameLayout';
import { RuleList, SideBox, StageMessage } from './SideBox';
import { playSkillFeedback, SkillSide, skillResultMessage } from './skill';
import { LocalBestBox, localBestNote } from './LocalBest';
import { useLocalBest } from './useLocalBest';
import { lightsMinimum } from './lightsout';
import { playSound } from './sound';
import { useStepPuzzle } from './useStepPuzzle';
import { PlayTurnstile } from './usePlayTurnstile';
import './logic-puzzles.css';

// 过关的评价：用了理论最少步数得三星，不超过一倍半得两星，其余一星。不知道最少步数时不评。
export function starsFor(moves, minimum) {
  if (!(minimum > 0) || !(moves > 0)) return 0;
  if (moves <= minimum) return 3;
  if (moves <= Math.ceil(minimum * 1.5)) return 2;
  return 1;
}

// 这两款公开盘面的解谜游戏每步由服务端保存，刷新与请求响应丢失后均从同一对局恢复。
export default function LogicPuzzle({ gameKey }) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState(null);
  const lights = gameKey === 'lightsout';
  const { bests, record } = useLocalBest(gameKey);
  // 熄灯开局时的最少步数：只有从第 0 步看到的盘面才知道，刷新到一半的对局不评星。按对局编号记。
  const startMinimumRef = useRef({ session: 0, minimum: 0 });
  const [finish, setFinish] = useState(null);
  const names = [t('简单'), t('普通'), t('困难')];
  const title = lights ? t('熄灯挑战') : t('汉诺塔');
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
  } = useStepPuzzle(gameKey, {
    // 和上一个局面比较，给这一步配上音效；同一局刚结束时评星、记本机最佳、放彩带。
    onStep: (current, next) => {
      setSelected(null);
      const sameRound =
        current && next && current.session_id === next.session_id;
      if (sameRound && next.moves !== current.moves) {
        if (next.moves < current.moves) playSound('click');
        else playSound(lights ? 'reveal' : 'place');
      } else if (next && !sameRound && next.status === 'playing') {
        playSound('start');
      }
      if (!sameRound || current.status !== 'playing' || !next.result) return;
      const won = !!next.result.detail?.won || !!next.won;
      const minimum = lights
        ? startMinimumRef.current.session === next.session_id
          ? startMinimumRef.current.minimum
          : 0
        : next.minimum_moves;
      setFinish(
        won
          ? {
              session: next.session_id,
              stars: starsFor(next.moves, minimum),
              improved: record(next.difficulty, { moves: next.moves }),
            }
          : null,
      );
      playSkillFeedback(next.result, won);
    },
  });

  const confirm = (action) =>
    Modal.confirm({
      title: action === 'reset' ? t('重置当前棋盘') : t('结束本局'),
      content:
        action === 'reset'
          ? t('回到本局初始盘面，已走步骤会清空。')
          : t('结束后按当前成绩结算，不能继续。'),
      onOk: () => send(action),
    });
  const size = round?.size || 3;
  const board = round?.board || [
    false,
    true,
    false,
    true,
    true,
    true,
    false,
    true,
    false,
  ];
  const towers = round?.towers || [[3, 2, 1], [], []];
  // 熄灯盘面现在最少还要按几下，每一步之后重新算。
  const lightsLeft = useMemo(
    () => (lights && round ? lightsMinimum(round.board, round.size) : -1),
    [lights, round],
  );
  if (
    lights &&
    round?.status === 'playing' &&
    round.moves === 0 &&
    startMinimumRef.current.session !== round.session_id
  ) {
    startMinimumRef.current = {
      session: round.session_id,
      minimum: lightsLeft,
    };
  }
  const shownFinish =
    finish && finish.session === round?.session_id ? finish : null;
  const result =
    round?.result &&
    skillResultMessage(round.result, t, info?.symbol, { byLevel: true });

  return (
    <GameLayout
      title={title}
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
                  items={
                    lights
                      ? [
                          t('点击一盏灯，会同时切换它和上下左右相邻灯的状态'),
                          t('让所有灯熄灭即可过关，每个初始盘面都保证有解'),
                          t('可以撤销或重置；每一步都会保存，刷新后可继续'),
                          t(
                            '「最少还需」是当前盘面最少还要按几下；用开局时的最少步数过关得三星',
                          ),
                        ]
                      : [
                          t(
                            '先选择有圆盘的塔，再选择目标塔，一次移动最上面的一个圆盘',
                          ),
                          t(
                            '大圆盘不能放在小圆盘上，把所有圆盘移到最右侧即可过关',
                          ),
                          t('可以撤销或重置；每一步都会保存，刷新后可继续'),
                          t('用理论最少步数过关得三星'),
                        ]
                  }
                />
              </SideBox>
            }
            after={
              <LocalBestBox
                bests={bests}
                levels={names.map((name, index) => ({ key: index + 1, name }))}
                columns={['moves']}
              />
            }
          />
        )
      }
    >
      <div
        className={`games-stage logic-stage ${lights ? 'is-lights' : 'is-hanoi'}`}
      >
        <div className='logic-heading'>
          <span className='logic-eyebrow'>{t('逻辑挑战')}</span>
          <h2>{title}</h2>
          <p>
            {lights
              ? t('一步点亮思路，一步熄灭灯光')
              : t('在三座塔之间，找到有序的解法')}
          </p>
        </div>
        <div className='logic-stats'>
          <span>
            {t('步数')}
            <strong>{round?.moves || 0}</strong>
          </span>
          <span>
            {lights ? t('亮灯') : t('理论最少步数')}
            <strong>
              {lights
                ? board.filter(Boolean).length
                : round?.minimum_moves || 7}
            </strong>
          </span>
          {lights && (
            <span>
              {t('最少还需')}
              <strong>{lightsLeft >= 0 ? lightsLeft : '-'}</strong>
            </span>
          )}
          <span>
            {t('难度')}
            <strong>{names[difficulty - 1]}</strong>
          </span>
        </div>
        {lights ? (
          <div
            className='lights-grid'
            style={{ '--lights-size': size }}
            aria-label={t('熄灯棋盘')}
          >
            {board.map((on, cell) => (
              <button
                key={cell}
                type='button'
                className={`lights-cell ${on ? 'is-on' : ''}`}
                disabled={!playing || busy}
                aria-pressed={on}
                aria-label={t('第 {{row}} 行第 {{col}} 列，{{state}}', {
                  row: Math.floor(cell / size) + 1,
                  col: (cell % size) + 1,
                  state: on ? t('亮灯') : t('熄灭'),
                })}
                onClick={() => send('move', { cell })}
              >
                <Lightbulb size={28} />
              </button>
            ))}
          </div>
        ) : (
          <div className='hanoi-board' aria-label={t('汉诺塔棋盘')}>
            {towers.map((tower, index) => {
              const source = selected === null ? null : towers[selected];
              const invalidTarget =
                selected !== null &&
                selected !== index &&
                tower.length > 0 &&
                source[source.length - 1] > tower[tower.length - 1];
              return (
                <button
                  key={index}
                  type='button'
                  className={`hanoi-tower ${selected === index ? 'is-selected' : ''}`}
                  disabled={
                    !playing ||
                    busy ||
                    invalidTarget ||
                    (selected === null && tower.length === 0)
                  }
                  aria-pressed={selected === index}
                  aria-label={t('第 {{index}} 座塔，{{count}} 个圆盘', {
                    index: index + 1,
                    count: tower.length,
                  })}
                  onClick={() => {
                    if (selected === null) setSelected(index);
                    else if (selected === index) setSelected(null);
                    else send('move', { from: selected, to: index });
                  }}
                >
                  <span className='hanoi-pole' />
                  <span className='hanoi-disks'>
                    {[...tower].reverse().map((disk) => (
                      <span
                        key={disk}
                        className='hanoi-disk'
                        style={{
                          width: `${30 + disk * 12}%`,
                          '--disk-hue': 180 + disk * 24,
                        }}
                      >
                        {disk}
                      </span>
                    ))}
                  </span>
                  <span className='hanoi-label'>
                    {index === 2 ? t('目标') : String(index + 1)}
                  </span>
                </button>
              );
            })}
          </div>
        )}
        {playing && (
          <p className='logic-help' aria-live='polite'>
            {busy
              ? t('正在保存…')
              : lights
                ? t('点击任意灯格切换状态')
                : selected === null
                  ? t('选择要移动圆盘的塔')
                  : t('选择目标塔，再点原塔可取消')}
          </p>
        )}
        <div className='logic-controls'>
          {playing ? (
            <>
              <Button
                icon={<Undo2 size={16} />}
                disabled={busy || !round.moves}
                onClick={() => send('undo')}
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
                  label: `${name} · ${lights ? `${index + 3}×${index + 3}` : t('{{count}} 个圆盘', { count: index + 3 })}`,
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
              {t('过关，用了 {{moves}} 步', { moves: round.moves })}{' '}
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
