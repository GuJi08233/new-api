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

import React, { useMemo, useState } from 'react';
import { Button, Modal, RadioGroup, Radio, Select } from '@douyinfe/semi-ui';
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
import { boardLines, lineMatches } from './logic';
import '../common/logic-puzzles.css';
import './nonogram.css';

const SIZES = [5, 8, 10];

export default function Nonogram() {
  const { t } = useTranslation();
  const names = ['5×5', '8×8', '10×10'];
  const { bests, record } = useLocalBest('nonogram');
  // 打叉只是给自己看的记号，不提交；换一局时清空。
  const [marks, setMarks] = useState({ session: 0, cells: {} });
  const [tool, setTool] = useState('fill');
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
  } = useStepPuzzle('nonogram', {
    onStep: (current, next) => {
      const sameRound =
        current && next && current.session_id === next.session_id;
      if (!sameRound) {
        if (next?.status === 'playing') playSound('start');
        return;
      }
      if (next.moves !== current.moves) {
        playSound(next.moves < current.moves ? 'click' : 'place');
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
  const filled = round?.filled || Array(size * size).fill(false);
  const rowClues = round?.rows || Array(size).fill([]);
  const colClues = round?.cols || Array(size).fill([]);
  const { rows, cols } = useMemo(
    () => boardLines(filled, size),
    [filled, size],
  );
  const marked = marks.session === round?.session_id ? marks.cells : {};
  const doneRows = rows.map((line, i) => lineMatches(line, rowClues[i]));
  const doneCols = cols.map((line, i) => lineMatches(line, colClues[i]));
  const longest = Math.max(1, ...colClues.map((clue) => clue.length));

  const toggleMark = (cell) => {
    playSound('tick');
    setMarks({
      session: round.session_id,
      cells: { ...marked, [cell]: !marked[cell] },
    });
  };
  const press = (cell, alternate) => {
    if (!playing || busy) return;
    const marking = (tool === 'mark') !== alternate;
    if (marking) {
      if (!filled[cell]) toggleMark(cell);
      return;
    }
    if (marked[cell]) return;
    send('move', { cell });
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
      title={t('数织')}
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
                      '行左侧、列上方的数字是这一行(列)里连续涂黑的段长，按顺序排列',
                    ),
                    t('每段之间至少隔一个空格；符合提示的行列会变成绿色'),
                    t('「打叉」只是给自己的记号，右键或切换工具即可打叉'),
                    t('所有行列都符合提示即过关，每道题都能一步步推出来'),
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
      <div className='games-stage logic-stage nonogram-stage'>
        <div className='logic-heading'>
          <span className='logic-eyebrow'>{t('逻辑挑战')}</span>
          <h2>{t('数织')}</h2>
          <p>{t('数字是线索，图案藏在格子里')}</p>
        </div>
        <div className='logic-stats'>
          <span>
            {t('步数')}
            <strong>{round?.moves || 0}</strong>
          </span>
          <span>
            {t('已完成行列')}
            <strong>
              {doneRows.filter(Boolean).length +
                doneCols.filter(Boolean).length}
              /{size * 2}
            </strong>
          </span>
          <span>
            {t('难度')}
            <strong>{names[difficulty - 1]}</strong>
          </span>
        </div>
        <div className='nonogram-scroll'>
          <div
            className={`nonogram-board ${round?.won ? 'is-won' : ''}`}
            style={{ '--nonogram-size': size, '--nonogram-clues': longest }}
            aria-label={t('数织棋盘')}
            onContextMenu={(event) => event.preventDefault()}
          >
            <span className='nonogram-corner' />
            {colClues.map((clue, col) => (
              <span
                key={`c${col}`}
                className={`nonogram-col-clue ${doneCols[col] ? 'is-done' : ''}`}
              >
                {(clue.length ? clue : [0]).map((n, i) => (
                  <b key={i}>{n}</b>
                ))}
              </span>
            ))}
            {rowClues.map((clue, row) => (
              <React.Fragment key={`r${row}`}>
                <span
                  className={`nonogram-row-clue ${doneRows[row] ? 'is-done' : ''}`}
                >
                  {(clue.length ? clue : [0]).map((n, i) => (
                    <b key={i}>{n}</b>
                  ))}
                </span>
                {Array.from({ length: size }, (_, col) => {
                  const cell = row * size + col;
                  const on = filled[cell];
                  return (
                    <button
                      key={cell}
                      type='button'
                      className={`nonogram-cell ${on ? 'is-on' : ''} ${marked[cell] && !on ? 'is-marked' : ''} ${col % 5 === 4 && col < size - 1 ? 'is-edge-right' : ''} ${row % 5 === 4 && row < size - 1 ? 'is-edge-bottom' : ''} ${row === 0 ? 'is-top' : ''} ${col === 0 ? 'is-left' : ''}`}
                      disabled={!playing}
                      aria-pressed={on}
                      aria-label={t('第 {{row}} 行第 {{col}} 列，{{state}}', {
                        row: row + 1,
                        col: col + 1,
                        state: on
                          ? t('已涂黑')
                          : marked[cell]
                            ? t('已打叉')
                            : t('空白'),
                      })}
                      onClick={() => press(cell, false)}
                      onContextMenu={(event) => {
                        event.preventDefault();
                        press(cell, true);
                      }}
                    />
                  );
                })}
              </React.Fragment>
            ))}
          </div>
        </div>
        {playing && (
          <div className='nonogram-tools'>
            <RadioGroup
              type='button'
              value={tool}
              onChange={(event) => setTool(event.target.value)}
              aria-label={t('工具')}
            >
              <Radio value='fill'>{t('涂黑')}</Radio>
              <Radio value='mark'>{t('打叉')}</Radio>
            </RadioGroup>
          </div>
        )}
        {playing && (
          <p className='logic-help' aria-live='polite'>
            {busy ? t('正在保存…') : t('点格子涂黑，再点一次取消；右键打叉')}
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
                ? t('图案完成，用时 {{seconds}} 秒', {
                    seconds: shownFinish.seconds,
                  })
                : t('图案完成')}{' '}
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
