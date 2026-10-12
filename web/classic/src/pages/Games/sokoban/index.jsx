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
import { Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  ArrowUp,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  Undo2,
  RotateCcw,
} from 'lucide-react';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gameGet, gamePost, withTurnstile } from '../common/api';
import { SkillSide, skillResultMessage, GameClock } from '../common/skill';
import { useGameInfo } from '../common/useGameInfo';
import { PlayTurnstile, usePlayTurnstile } from '../common/usePlayTurnstile';
import { movePuzzle, replayPuzzle, solved } from './engine';
import '../common/puzzles.css';

const STORAGE = 'game-sokoban-v1';
const KEYS = {
  ArrowUp: 'U',
  ArrowDown: 'D',
  ArrowLeft: 'L',
  ArrowRight: 'R',
  w: 'U',
  s: 'D',
  a: 'L',
  d: 'R',
};

const Sokoban = () => {
  const { t } = useTranslation();
  const { info, loading, unavailable, patch } = useGameInfo('sokoban');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const [session, setSession] = useState(null);
  const [puzzle, setPuzzle] = useState(null);
  const [moves, setMoves] = useState('');
  const [result, setResult] = useState(null);
  const [end, setEnd] = useState(null);
  const [busy, setBusy] = useState(false);
  const [checked, setChecked] = useState(false);
  const [error, setError] = useState('');
  const busyRef = useRef(false);
  const moveRef = useRef(null);
  const playing = !!session && !result;
  const won = puzzle && solved(puzzle);

  const restore = useCallback((view) => {
    let saved = '';
    try {
      const stored = JSON.parse(localStorage.getItem(STORAGE) || 'null');
      if (
        stored?.session_id === view.session_id &&
        typeof stored.moves === 'string' &&
        stored.moves.length <= 4096
      )
        saved = stored.moves;
    } catch {
      /* 本地存储不可用时仍可正常游戏。 */
    }
    const replayed = replayPuzzle(view.puzzle, saved);
    setSession({ ...view, since: Date.now() - view.elapsed * 1000 });
    setPuzzle(replayed.puzzle);
    setMoves(replayed.moves);
    setResult(null);
    setEnd(null);
  }, []);

  useEffect(() => {
    let alive = true;
    gameGet('/api/game/sokoban/state', t).then((response) => {
      if (!alive) return;
      if (response.data?.session) restore(response.data.session);
      if (response.error) setError(response.error);
      setChecked(true);
    });
    return () => {
      alive = false;
    };
  }, [restore, t]);

  useEffect(() => {
    if (!session || result) return;
    try {
      localStorage.setItem(
        STORAGE,
        JSON.stringify({ session_id: session.session_id, moves }),
      );
    } catch {
      /* 存储失败不打断对局。 */
    }
  }, [session, moves, result]);

  const submit = async (allMoves, abandon = false) => {
    if (busyRef.current || !session) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      '/api/game/sokoban/finish',
      { session_id: session.session_id, moves: allMoves, abandon },
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      // 服务端明确拒绝的一局(已过期、被另一个页面新开的一局作废)重交也不会成功：放下它，回到可以开新局的状态。
      // 没有得到答复、服务端暂时出错或被限流时留着，可以重新提交。
      if (!response.network && !response.limited) {
        setSession(null);
        try {
          localStorage.removeItem(STORAGE);
        } catch {
          /* 本地存储不可用时无需清理。 */
        }
      }
      return;
    }
    setResult(response.data);
    setEnd(Date.now());
    patch({
      balance: response.data.balance,
      progress: response.data.progress,
      best_score: response.data.best_score,
    });
    try {
      localStorage.removeItem(STORAGE);
    } catch {
      /* 成绩以服务端为准。 */
    }
  };

  const start = async () => {
    if (busyRef.current || !turnstile.ready) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      withTurnstile('/api/game/sokoban/start', turnstile.take()),
      {},
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      return;
    }
    restore(response.data);
  };

  const move = (direction) => {
    if (
      !playing ||
      busyRef.current ||
      won ||
      moves.length >= (info?.max_moves || 4096)
    )
      return;
    const next = movePuzzle(puzzle, direction);
    if (!next) return;
    const allMoves = moves + direction;
    setPuzzle(next);
    setMoves(allMoves);
    if (solved(next)) submit(allMoves);
  };
  moveRef.current = move;

  useEffect(() => {
    if (!playing) return undefined;
    const onKey = (event) => {
      if (
        event.ctrlKey ||
        event.metaKey ||
        event.altKey ||
        event.target.closest?.('input,textarea,[contenteditable="true"]')
      )
        return;
      const direction = KEYS[event.key] || KEYS[event.key.toLowerCase()];
      if (!direction) return;
      event.preventDefault();
      moveRef.current(direction);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [playing]);

  const rewind = (reset) => {
    if (!playing || busy || won) return;
    const replayed = replayPuzzle(
      session.puzzle,
      reset ? '' : moves.slice(0, -1),
    );
    setPuzzle(replayed.puzzle);
    setMoves(replayed.moves);
    setError('');
  };

  const reward = result && skillResultMessage(result, t, info?.symbol);
  return (
    <GameLayout
      title={t('每日推箱子')}
      loading={loading || !checked}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={info?.symbol}
      side={
        info && (
          <SkillSide
            info={info}
            symbol={info.symbol}
            scoreName={t('完成关卡')}
            extra={
              <SideBox label={t('操作说明')}>
                <RuleList
                  items={[
                    t('方向键或 WASD 移动，也可以点击相邻地面或下方按钮'),
                    t('箱子只能推不能拉，把所有箱子推到绿色目标上即可过关'),
                    t(
                      '每天一张可解地图，所有玩家挑战相同关卡；卡住时可以撤销或重置',
                    ),
                  ]}
                />
              </SideBox>
            }
          />
        )
      }
    >
      <div className='games-stage'>
        <div className='game-hud'>
          <span>
            {t('今日挑战')}
            <strong>{puzzle?.day || info?.day}</strong>
          </span>
          <span>
            {t('步数')}
            <strong>{moves.length}</strong>
          </span>
          <span>
            {t('用时')}
            <strong>
              {session ? (
                <GameClock since={session.since} stoppedAt={end} />
              ) : (
                '00:00'
              )}
            </strong>
          </span>
        </div>
        {!puzzle && (
          <p className='puzzle-intro'>
            {t('三只箱子，一张地图，试着用更少的步数完成')}
          </p>
        )}
        {puzzle && (
          <div className='puzzle-board'>
            <div className='sokoban-grid' style={{ '--cols': puzzle.width }}>
              {Array.from(
                { length: puzzle.width * puzzle.height },
                (_, index) => {
                  const wall = puzzle.walls.includes(index),
                    goal = puzzle.goals.includes(index),
                    box = puzzle.boxes.includes(index),
                    player = puzzle.player === index;
                  const direction = Object.entries(KEYS).find(
                    ([key, dir]) =>
                      key.startsWith('Arrow') &&
                      puzzle.player +
                        { U: -puzzle.width, D: puzzle.width, L: -1, R: 1 }[
                          dir
                        ] ===
                        index,
                  )?.[1];
                  return (
                    <button
                      type='button'
                      key={index}
                      className={`sokoban-cell${wall ? ' is-wall' : ''}${goal ? ' is-goal' : ''}`}
                      disabled={wall || !playing || busy || won || !direction}
                      aria-label={
                        player
                          ? t('玩家')
                          : box
                            ? t('箱子')
                            : goal
                              ? t('目标')
                              : t('地面')
                      }
                      onClick={() => direction && move(direction)}
                    >
                      {box && (
                        <span
                          className={`sokoban-box${goal ? ' is-home' : ''}`}
                        />
                      )}
                      {player && <span className='sokoban-player' />}
                    </button>
                  );
                },
              )}
            </div>
          </div>
        )}
        {playing && (
          <div className='puzzle-dpad'>
            <span />
            <Button
              aria-label={t('上')}
              icon={<ArrowUp />}
              disabled={busy || won}
              onClick={() => move('U')}
            />
            <span />
            <Button
              aria-label={t('左')}
              icon={<ArrowLeft />}
              disabled={busy || won}
              onClick={() => move('L')}
            />
            <Button
              aria-label={t('下')}
              icon={<ArrowDown />}
              disabled={busy || won}
              onClick={() => move('D')}
            />
            <Button
              aria-label={t('右')}
              icon={<ArrowRight />}
              disabled={busy || won}
              onClick={() => move('R')}
            />
          </div>
        )}
        <div className='puzzle-toolbar'>
          {playing ? (
            <>
              <Button
                icon={<Undo2 size={16} />}
                disabled={busy || !moves || won}
                onClick={() => rewind(false)}
              >
                {t('撤销')}
              </Button>
              <Button
                icon={<RotateCcw size={16} />}
                disabled={busy || !moves || won}
                onClick={() => rewind(true)}
              >
                {t('重置棋盘')}
              </Button>
              <Button loading={busy} onClick={() => submit(moves, !won)}>
                {won ? t('提交成绩') : t('结束并结算')}
              </Button>
            </>
          ) : (
            <Button
              theme='solid'
              loading={busy}
              disabled={!turnstile.ready}
              onClick={start}
            >
              {t('开始游戏')}
            </Button>
          )}
        </div>
        {moves.length >= (info?.max_moves || 4096) && (
          <p className='puzzle-intro'>
            {t('已达到步数上限，请撤销或重置棋盘')}
          </p>
        )}
        {!playing && <PlayTurnstile turnstile={turnstile} />}
        {(error || reward) && (
          <StageMessage
            kind={error ? 'error' : reward?.kind}
            note={reward?.note}
          >
            {error || reward?.text}
          </StageMessage>
        )}
        {won && <p className='puzzle-intro'>{t('所有箱子已归位！')}</p>}
      </div>
    </GameLayout>
  );
};

export default Sokoban;
