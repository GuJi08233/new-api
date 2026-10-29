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
import { Button, Modal, Select } from '@douyinfe/semi-ui';
import { Delete } from 'lucide-react';
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
import { consistentWith } from './logic';
import '../common/logic-puzzles.css';
import './bulls.css';

const LEVELS = [
  { digits: 3, guesses: 8 },
  { digits: 4, guesses: 8 },
  { digits: 4, guesses: 6 },
];
const DIGITS = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '0'];
// 草稿纸上每个数字的记号依次切换：未标记、排除、确定在答案里。只记在页面上，不提交。
const NOTE_CYCLE = { '': 'out', out: 'in', in: '' };

export default function Bulls() {
  const { t } = useTranslation();
  const names = [t('3 位数'), t('4 位数'), t('4 位数 · 6 次')];
  const { bests, record } = useLocalBest('bulls');
  const [guess, setGuess] = useState('');
  const [notes, setNotes] = useState({});
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
  } = useStepPuzzle('bulls', {
    onStep: (current, next) => {
      const sameRound =
        current && next && current.session_id === next.session_id;
      if (!sameRound) {
        setGuess('');
        setNotes({});
        if (next?.status === 'playing') playSound('start');
        return;
      }
      if (next.moves > current.moves) {
        setGuess('');
        const last = next.history[next.history.length - 1];
        if (!next.result) playSound(last.a + last.b > 0 ? 'reveal' : 'hit');
      }
      if (current.status !== 'playing' || !next.result) return;
      const won = !!next.won;
      setFinish(
        won
          ? {
              session: next.session_id,
              improved: record(next.difficulty, { moves: next.moves }),
            }
          : null,
      );
      playSkillFeedback(next.result, won);
    },
  });

  const level = LEVELS[(round?.difficulty || difficulty) - 1];
  const digits = round?.digits || level.digits;
  const maxGuesses = round?.max_guesses || level.guesses;
  const history = round?.history || [];
  const ready = guess.length === digits;
  const consistent = ready && consistentWith(guess, history);

  const press = (digit) => {
    if (!playing || busy || guess.length >= digits || guess.includes(digit))
      return;
    playSound('click');
    setGuess(guess + digit);
  };
  const submit = () => {
    if (!playing || busy || !ready) return;
    send('answer', { answer: guess });
  };
  const note = (digit) =>
    setNotes((current) => ({
      ...current,
      [digit]: NOTE_CYCLE[current[digit] || ''],
    }));

  useEffect(() => {
    if (!playing) return undefined;
    const onKey = (event) => {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      if (event.target?.closest?.('input, textarea, .semi-modal')) return;
      if (/^[0-9]$/.test(event.key)) {
        event.preventDefault();
        press(event.key);
      } else if (event.key === 'Backspace') {
        event.preventDefault();
        setGuess((value) => value.slice(0, -1));
      } else if (event.key === 'Enter') {
        event.preventDefault();
        submit();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  const confirmFinish = () =>
    Modal.confirm({
      title: t('结束本局'),
      content: t('结束后按当前成绩结算，不能继续。'),
      onOk: () => send('finish'),
    });

  const shownFinish =
    finish && finish.session === round?.session_id ? finish : null;
  const result =
    round?.result &&
    skillResultMessage(round.result, t, info?.symbol, { byLevel: true });

  return (
    <GameLayout
      title={t('猜数字')}
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
                    t('答案由互不相同的数字组成，首位可以是 0'),
                    t('A 表示数字和位置都对，B 表示数字对但位置不对'),
                    t('「剩余可能」是与所有提示都相符的答案个数'),
                    t(
                      '草稿纸上的数字点一下标记排除，再点一下标记确定，只给自己看',
                    ),
                  ]}
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
      <div className='games-stage logic-stage bulls-stage'>
        <div className='logic-heading'>
          <span className='logic-eyebrow'>{t('推理挑战')}</span>
          <h2>{t('猜数字')}</h2>
          <p>{t('每一条提示都是线索')}</p>
        </div>
        <div className='logic-stats'>
          <span>
            {t('已猜')}
            <strong>
              {history.length}/{maxGuesses}
            </strong>
          </span>
          <span>
            {t('剩余可能')}
            <strong>{round?.candidates ?? '-'}</strong>
          </span>
          <span>
            {t('难度')}
            <strong>{names[difficulty - 1]}</strong>
          </span>
        </div>
        <ol className='bulls-history' aria-label={t('猜测记录')}>
          {Array.from({ length: maxGuesses }, (_, index) => {
            const item = history[index];
            const current = !item && index === history.length && playing;
            const shown = item?.guess || (current ? guess : '');
            return (
              <li
                key={index}
                className={`${item ? 'is-done' : ''} ${current ? 'is-current' : ''} ${item?.a === digits ? 'is-hit' : ''}`}
              >
                <span className='bulls-index'>{index + 1}</span>
                <span className='bulls-digits'>
                  {Array.from({ length: digits }, (_, d) => (
                    <span key={d}>{shown[d] || ''}</span>
                  ))}
                </span>
                <span className='bulls-feedback'>
                  {item && (
                    <>
                      <span className='bulls-pegs'>
                        {Array.from({ length: digits }, (_, peg) => (
                          <i
                            key={peg}
                            className={
                              peg < item.a
                                ? 'is-a'
                                : peg < item.a + item.b
                                  ? 'is-b'
                                  : ''
                            }
                          />
                        ))}
                      </span>
                      <strong>
                        {item.a}A{item.b}B
                      </strong>
                    </>
                  )}
                </span>
              </li>
            );
          })}
        </ol>
        {round?.secret && (
          <div className='bulls-secret' role='status'>
            {t('答案')}
            <strong>{round.secret}</strong>
          </div>
        )}
        {playing && (
          <>
            <div className='bulls-pad'>
              {DIGITS.map((digit) => (
                <button
                  key={digit}
                  type='button'
                  className={`is-${notes[digit] || 'none'}`}
                  disabled={busy || guess.includes(digit) || ready}
                  onClick={() => press(digit)}
                >
                  {digit}
                </button>
              ))}
              <button
                type='button'
                disabled={busy || !guess}
                aria-label={t('退格')}
                onClick={() => setGuess(guess.slice(0, -1))}
              >
                <Delete size={18} />
              </button>
            </div>
            <div className='bulls-notes'>
              <span>{t('草稿纸')}</span>
              {DIGITS.map((digit) => (
                <button
                  key={digit}
                  type='button'
                  className={`is-${notes[digit] || 'none'}`}
                  aria-label={t('标记数字 {{digit}}', { digit })}
                  onClick={() => note(digit)}
                >
                  {digit}
                </button>
              ))}
            </div>
            <p className='logic-help' aria-live='polite'>
              {busy
                ? t('正在保存…')
                : ready && !consistent
                  ? t('这个数和之前的提示对不上，确定要猜吗？')
                  : ''}
            </p>
          </>
        )}
        <div className='logic-controls'>
          {playing ? (
            <>
              <Button theme='solid' disabled={busy || !ready} onClick={submit}>
                {t('猜')}
              </Button>
              <Button disabled={busy} onClick={confirmFinish}>
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
              {t('第 {{count}} 次猜中', { count: round.moves })}{' '}
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
