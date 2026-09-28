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

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Select } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gameGet, gamePost, withTurnstile } from '../common/api';
import { formatClock, SkillSide, skillResultMessage } from '../common/skill';
import { useGameInfo } from '../common/useGameInfo';
import { usePlayTurnstile } from '../common/usePlayTurnstile';
import { getGameLevelNames } from '../../../constants/game.constants';
import '../common/puzzles.css';

const SYMBOLS = [
  '🍒',
  '🍋',
  '🍇',
  '🍉',
  '🍓',
  '🥝',
  '🍑',
  '🍍',
  '🍎',
  '🥥',
  '🫐',
  '🍊',
];

const Pairlink = () => {
  const { t } = useTranslation();
  const { info, loading, unavailable, patch, reload } = useGameInfo('pairlink');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const [game, setGame] = useState(null);
  const [difficulty, setDifficulty] = useState(1);
  const [selected, setSelected] = useState(-1);
  const [hint, setHint] = useState([]);
  const [path, setPath] = useState([]);
  const [now, setNow] = useState(Date.now);
  const [busy, setBusy] = useState(false);
  const [checked, setChecked] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState(null);
  const [notice, setNotice] = useState('');
  const busyRef = useRef(false);
  const actionRef = useRef(null);
  const expiryRef = useRef(null);
  const names = getGameLevelNames(t).pairlink;
  const playing = game?.status === 'playing';

  const accept = useCallback((view) => {
    setGame({ ...view, since: Date.now() - view.elapsed * 1000 });
    setNow(Date.now());
    setDifficulty(view.difficulty);
    setSelected(-1);
  }, []);

  useEffect(() => {
    let alive = true;
    gameGet('/api/game/pairlink/state', t).then((response) => {
      if (!alive) return;
      if (response.data?.session) accept(response.data.session);
      if (response.error) setError(response.error);
      setChecked(true);
    });
    return () => {
      alive = false;
    };
  }, [accept, t]);

  useEffect(() => {
    if (!playing) return undefined;
    const timer = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(timer);
  }, [playing]);

  useEffect(() => {
    if (!path.length) return undefined;
    const timer = setTimeout(() => setPath([]), 650);
    return () => clearTimeout(timer);
  }, [path]);

  const settle = (value) => {
    setResult(value);
    patch({
      balance: value.balance,
      progress: value.progress,
      best_score: value.best_score,
    });
  };

  const start = async () => {
    if (busyRef.current || !turnstile.ready) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      withTurnstile('/api/game/pairlink/start', turnstile.take()),
      { difficulty },
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      return;
    }
    accept(response.data);
    setResult(null);
    setHint([]);
    setPath([]);
    setNotice('');
  };

  const action = async (kind, first, second) => {
    if (!game || !playing || busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    setNotice('');
    setHint([]);
    const response = await gamePost(
      '/api/game/pairlink/action',
      {
        session_id: game.session_id,
        version: game.version,
        action: kind,
        first,
        second,
      },
      t,
    );
    if (response.error) {
      setError(response.error);
      setSelected(-1);
      const restored = await gameGet('/api/game/pairlink/state', t);
      if (restored.data?.session) accept(restored.data.session);
      else if (restored.data) {
        setGame((previous) => previous && { ...previous, status: 'finished' });
        setNotice(t('对局已结束，奖励以游戏币明细为准'));
        reload(true);
      }
    } else if (response.data.board) {
      accept(response.data);
      setPath(response.data.path || []);
      setHint(response.data.hint || []);
      if (response.data.shuffled) setNotice(t('牌面已重新排列'));
      if (response.data.result) settle(response.data.result);
    } else {
      settle(response.data);
      setGame((previous) => ({ ...previous, status: 'finished' }));
    }
    busyRef.current = false;
    setBusy(false);
  };
  actionRef.current = action;
  const left = game
    ? Math.max(
        0,
        game.seconds - Math.floor(Math.max(0, now - game.since) / 1000),
      )
    : 0;
  useEffect(() => {
    if (
      playing &&
      left === 0 &&
      !busy &&
      expiryRef.current !== game.session_id
    ) {
      expiryRef.current = game.session_id;
      actionRef.current('finish');
    }
  }, [playing, left, busy, game?.session_id]);

  const choose = (index) => {
    if (!playing || busy) return;
    if (selected === index) {
      setSelected(-1);
      return;
    }
    if (selected < 0) setSelected(index);
    else action('match', selected, index);
  };

  const message =
    result && skillResultMessage(result, t, info?.symbol, { byLevel: true });
  const side = info && (
    <SkillSide
      info={info}
      symbol={info.symbol}
      levelNames={names}
      scoreName={t('难度')}
      extra={
        <SideBox label={t('操作说明')}>
          <RuleList
            items={[
              t('选择两个相同图案，连线最多拐两次弯，可以绕过棋盘外沿'),
              t('限时内消除所有图案即可获胜，每局有三次提示和三次洗牌'),
              t('没有可消除的图案时会自动重新排列'),
            ]}
          />
        </SideBox>
      }
    />
  );

  return (
    <GameLayout
      title={t('连连看')}
      loading={loading || !checked}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={info?.symbol}
      side={side}
    >
      <div className='games-stage'>
        <div className='game-hud'>
          <span>
            {t('剩余配对')}
            <strong>{game?.remaining ?? '-'}</strong>
          </span>
          <span>
            {t('剩余时间')}
            <strong>{formatClock(left)}</strong>
          </span>
        </div>
        {!game && (
          <p className='puzzle-intro'>
            {t('眼疾手快，找到可以连起来的相同图案')}
          </p>
        )}
        {game && (
          <div className='puzzle-board'>
            <div
              className='pairlink-grid'
              style={{ '--cols': game.cols + 2, '--rows': game.rows + 2 }}
            >
              {Array.from(
                { length: (game.cols + 2) * (game.rows + 2) },
                (_, cell) => {
                  const row = Math.floor(cell / (game.cols + 2)) - 1;
                  const col = (cell % (game.cols + 2)) - 1;
                  const index = row * game.cols + col;
                  const tile =
                    row >= 0 && row < game.rows && col >= 0 && col < game.cols
                      ? game.board[index]
                      : 0;
                  return tile ? (
                    <button
                      key={cell}
                      type='button'
                      className={`pairlink-tile${selected === index ? ' is-selected' : ''}${hint.includes(index) ? ' is-hint' : ''}`}
                      disabled={!playing || busy}
                      aria-label={t('图案 {{number}}', { number: tile })}
                      aria-pressed={selected === index}
                      onClick={() => choose(index)}
                    >
                      {SYMBOLS[tile - 1]}
                    </button>
                  ) : (
                    <span key={cell} />
                  );
                },
              )}
              {path.length > 0 && (
                <svg
                  className='pairlink-path'
                  aria-hidden='true'
                  viewBox={`0 0 ${game.cols + 2} ${game.rows + 2}`}
                  preserveAspectRatio='none'
                >
                  <polyline
                    points={path
                      .map((point) => `${point.col + 0.5},${point.row + 0.5}`)
                      .join(' ')}
                    fill='none'
                    stroke='#34d399'
                    strokeWidth='.08'
                    strokeLinejoin='round'
                    strokeLinecap='round'
                  />
                </svg>
              )}
            </div>
          </div>
        )}
        <div className='puzzle-toolbar'>
          {playing ? (
            <>
              <Button
                disabled={busy || !game.hints}
                onClick={() => action('hint')}
              >
                {t('提示')} ({game.hints})
              </Button>
              <Button
                disabled={busy || !game.shuffles}
                onClick={() => action('shuffle')}
              >
                {t('洗牌')} ({game.shuffles})
              </Button>
              <Button disabled={busy} onClick={() => action('finish')}>
                {t('结束并结算')}
              </Button>
            </>
          ) : (
            <>
              <Select
                value={difficulty}
                onChange={setDifficulty}
                optionList={names.map((label, i) => ({ label, value: i + 1 }))}
              />
              <Button
                theme='solid'
                loading={busy}
                disabled={!turnstile.ready}
                onClick={start}
              >
                {t('开始游戏')}
              </Button>
            </>
          )}
        </div>
        {!playing && turnstile.widget}
        {(error || message || notice) && (
          <StageMessage
            kind={error ? 'error' : message?.kind}
            note={message?.note}
          >
            {error || message?.text || notice}
          </StageMessage>
        )}
        {game?.status === 'won' && (
          <p className='puzzle-intro'>{t('全部消除，挑战成功！')}</p>
        )}
        {game?.status === 'lost' && (
          <p className='puzzle-intro'>{t('本局结束，再试一次吧')}</p>
        )}
      </div>
    </GameLayout>
  );
};

export default Pairlink;
