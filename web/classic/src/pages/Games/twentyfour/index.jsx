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
import { Button, Modal, Select, Tag } from '@douyinfe/semi-ui';
import { Delete, SkipForward } from 'lucide-react';
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
import { RANK_LABELS, SUIT_ICONS, isRedSuit } from '../common/cards';
import { checkAnswer, evaluateExpression, formatFraction } from './expr';
import '../common/logic-puzzles.css';
import './twentyfour.css';

const OPERATORS = ['+', '−', '×', '÷', '(', ')'];
const KEY_OPERATORS = {
  '+': '+',
  '-': '−',
  '*': '×',
  x: '×',
  '/': '÷',
  '(': '(',
  ')': ')',
};

// 算式由一串记号组成：点牌放进这张牌的点数，点运算符放进运算符。每张牌只能用一次。两张牌紧挨着时用空格隔开，
// 不会被读成一个两位数(服务端同样判为写法不对)。
const expressionText = (tokens, hand) =>
  tokens
    .map((token, index) => {
      if (token.card === undefined) return token.op;
      const gap = tokens[index - 1]?.card !== undefined ? ' ' : '';
      return `${gap}${hand[token.card]}`;
    })
    .join('');

export default function Twentyfour() {
  const { t } = useTranslation();
  const names = [t('简单'), t('普通'), t('困难')];
  const { bests, record } = useLocalBest('twentyfour');
  const [tokens, setTokens] = useState([]);
  const [hint, setHint] = useState('');
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
  } = useStepPuzzle('twentyfour', {
    onStep: (current, next) => {
      const sameRound =
        current && next && current.session_id === next.session_id;
      if (!sameRound) {
        setTokens([]);
        setHint('');
        if (next?.status === 'playing') playSound('start');
        return;
      }
      if (next.moves !== current.moves) {
        setTokens([]);
        setHint('');
        const last = next.history?.[next.history.length - 1];
        if (last && !last.skipped && !next.result) playSound('combo');
        else if (last?.skipped) playSound('flip');
      }
      if (current.status !== 'playing' || !next.result) return;
      const won = !!next.won;
      const seconds = elapsedSeconds(next.session_id);
      setFinish(
        won
          ? {
              session: next.session_id,
              improved:
                seconds > 0 && record(next.difficulty, { time: seconds }),
            }
          : null,
      );
      playSkillFeedback(next.result, won);
    },
  });

  const hand = round?.hand || [];
  const used = new Set(
    tokens
      .filter((token) => token.card !== undefined)
      .map((token) => token.card),
  );
  const text = expressionText(tokens, hand);
  const preview = tokens.length ? evaluateExpression(text) : null;
  const complete = hand.length === 4 && used.size === 4;
  const verdict = complete ? checkAnswer(text, hand) : null;

  const push = (token) => {
    if (!playing || busy) return;
    if (token.card !== undefined && used.has(token.card)) return;
    playSound(token.card === undefined ? 'click' : 'flip');
    setHint('');
    setTokens((list) => [...list, token]);
  };
  const pop = () => {
    setHint('');
    setTokens((list) => list.slice(0, -1));
  };
  const submit = () => {
    if (!playing || busy || !complete) return;
    if (!verdict.ok) {
      playSound('error');
      setHint(
        verdict.reason === 'value'
          ? t('算出来是 {{value}}，不是 24', {
              value: formatFraction(verdict.parsed.value),
            })
          : t('算式写法不对，检查一下括号和运算符'),
      );
      return;
    }
    send('answer', { answer: text });
  };
  const skip = () =>
    Modal.confirm({
      title: t('跳过这一题'),
      content: t(
        '跳过后会公布一种解法，这一题不计入已解；本局还能跳过 {{count}} 次。',
        {
          count: round.skips_left,
        },
      ),
      onOk: () => send('skip'),
    });

  // 键盘：数字键放入还没用过的同点数牌(10 以上请点牌)，运算符键、退格与回车。
  useEffect(() => {
    if (!playing) return undefined;
    const onKey = (event) => {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      if (event.target?.closest?.('input, textarea, .semi-modal')) return;
      const op = KEY_OPERATORS[event.key];
      if (op) {
        event.preventDefault();
        push({ op });
        return;
      }
      if (event.key === 'Backspace') {
        event.preventDefault();
        pop();
        return;
      }
      if (event.key === 'Enter') {
        event.preventDefault();
        submit();
        return;
      }
      const digit = Number(event.key);
      if (event.key.length === 1 && digit >= 1 && digit <= 9) {
        const card = hand.findIndex(
          (value, index) => value === digit && !used.has(index),
        );
        if (card >= 0) {
          event.preventDefault();
          push({ card });
        }
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
  const history = round?.history || [];
  const total = round?.hands_per_round || 5;

  return (
    <GameLayout
      title={t('24 点')}
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
                      '四张牌每张用一次，用加减乘除和括号算出 24；A 是 1，J、Q、K 是 11、12、13',
                    ),
                    t('点牌和运算符拼出算式，也可以用键盘输入，回车提交'),
                    t('允许出现分数，例如 8 ÷ (3 − 8 ÷ 3)'),
                    t(
                      '一局五题，解出全部即过关；每局可以跳过两题，跳过的题会公布一种解法',
                    ),
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
      <div className='games-stage logic-stage twentyfour-stage'>
        <div className='logic-heading'>
          <span className='logic-eyebrow'>{t('数字挑战')}</span>
          <h2>{t('24 点')}</h2>
          <p>{t('四张牌，一个目标')}</p>
        </div>
        <div className='logic-stats'>
          <span>
            {t('已解')}
            <strong>
              {round?.solved || 0}/{total}
            </strong>
          </span>
          <span>
            {t('可跳过')}
            <strong>{round?.skips_left ?? 2}</strong>
          </span>
          <span>
            {t('难度')}
            <strong>{names[difficulty - 1]}</strong>
          </span>
        </div>
        <div
          className='twentyfour-progress'
          aria-label={t('本局进度')}
          role='list'
        >
          {Array.from({ length: total }, (_, index) => {
            const done = history.filter((item) => !item.skipped).length;
            return (
              <span
                key={index}
                role='listitem'
                className={index < done ? 'is-done' : ''}
              />
            );
          })}
        </div>
        <div className='twentyfour-cards'>
          {(hand.length ? hand : [3, 3, 8, 8]).map((value, index) => {
            const Suit = SUIT_ICONS[index % 4];
            return (
              <button
                key={`${round?.moves}-${index}`}
                type='button'
                className={`twentyfour-card ${isRedSuit(index % 4) ? 'is-red' : ''} ${used.has(index) ? 'is-used' : ''}`}
                disabled={!playing || busy || used.has(index)}
                aria-label={t('牌 {{value}}', { value })}
                onClick={() => push({ card: index })}
              >
                <span className='twentyfour-rank'>
                  {RANK_LABELS[value - 1]}
                </span>
                <Suit size={22} />
                {value > 10 && <small>{value}</small>}
              </button>
            );
          })}
        </div>
        {playing && (
          <div
            className={`twentyfour-expression ${verdict?.ok ? 'is-ok' : ''}`}
            aria-live='polite'
          >
            <span>{text || t('点牌和运算符拼出算式')}</span>
            {preview && <em>= {formatFraction(preview.value)}</em>}
          </div>
        )}
        {playing && (
          <>
            <div className='twentyfour-keys'>
              {OPERATORS.map((op) => (
                <button
                  key={op}
                  type='button'
                  disabled={busy}
                  onClick={() => push({ op })}
                >
                  {op}
                </button>
              ))}
              <button
                type='button'
                disabled={busy || !tokens.length}
                aria-label={t('退格')}
                onClick={pop}
              >
                <Delete size={18} />
              </button>
            </div>
            <p className='logic-help'>{busy ? t('正在保存…') : hint}</p>
          </>
        )}
        <div className='logic-controls'>
          {playing ? (
            <>
              <Button
                theme='solid'
                disabled={busy || !complete}
                onClick={submit}
              >
                {t('提交')}
              </Button>
              <Button
                disabled={busy || !tokens.length}
                onClick={() => setTokens([])}
              >
                {t('清空')}
              </Button>
              <Button
                icon={<SkipForward size={16} />}
                disabled={busy || !round.skips_left}
                onClick={skip}
              >
                {t('跳过')}
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
              {t('五题全部解出')} {localBestNote(shownFinish.improved, t)}
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
        {history.length > 0 && (
          <ol className='twentyfour-history'>
            {history.map((item, index) => (
              <li key={index}>
                <span className='twentyfour-history-hand'>
                  {item.hand.map((value) => RANK_LABELS[value - 1]).join(' ')}
                </span>
                <span className='twentyfour-history-answer'>
                  {item.answer} = 24
                </span>
                {item.skipped ? (
                  <Tag size='small' color='grey'>
                    {t('已跳过')}
                  </Tag>
                ) : (
                  <Tag size='small' color='green'>
                    {t('已解')}
                  </Tag>
                )}
              </li>
            ))}
          </ol>
        )}
      </div>
    </GameLayout>
  );
}
