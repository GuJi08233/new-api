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
import { Modal } from '@douyinfe/semi-ui';
import {
  Anchor,
  Apple,
  Bell,
  Bird,
  Cat,
  Cherry,
  Cloud,
  Crown,
  Fish,
  Flame,
  Flower2,
  Gem,
  Ghost,
  Heart,
  Leaf,
  Moon,
  Play,
  Rocket,
  RotateCcw,
  Snowflake,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameLevelNames, getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gameGet, gamePost, withTurnstile } from '../common/api';
import {
  formatClock,
  GameClock,
  levelReward,
  playSkillFeedback,
  rewardsLeftText,
  skillResultMessage,
  skillRuleSummary,
  SkillSide,
} from '../common/skill';
import { useGameInfo } from '../common/useGameInfo';
import { PlayTurnstile, usePlayTurnstile } from '../common/usePlayTurnstile';
import { Floaters, useFloaters } from '../common/effects';
import { LocalBestBox, localBestNote } from '../common/LocalBest';
import { useLocalBest } from '../common/useLocalBest';
import { playSound } from '../common/sound';
import './memory.css';

const STATE_URL = '/api/game/memory/state';
// 没配上的两张亮这么久再翻回去；这期间点下一张会先把它们翻回去。
const MISS_SHOW_MS = 900;

// 图案编号 0..17 对应的图标，颜色见 memory.css 的 memory-symbol-*。
const SYMBOL_ICONS = [
  Apple,
  Anchor,
  Bell,
  Bird,
  Cat,
  Cherry,
  Cloud,
  Crown,
  Gem,
  Fish,
  Leaf,
  Flower2,
  Ghost,
  Heart,
  Moon,
  Rocket,
  Snowflake,
  Flame,
];

const Memory = () => {
  const { t } = useTranslation();
  const { loading, unavailable, info, patch, reload } = useGameInfo('memory');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const meta = getGameMetaMap(t).memory;
  const levelNames = getGameLevelNames(t).memory;
  const symbol = info?.symbol || '';
  const levels = info?.levels || [];

  const [difficulty, setDifficulty] = useState(1);
  // 进行中或刚结束的一局：{ sessionId, difficulty, rows, cols, pairs, moveLimit, since }。
  const [game, setGame] = useState(null);
  // 每张牌的图案，没见过的为 null；只有服务端回应过的牌才知道。
  const [symbols, setSymbols] = useState([]);
  const [matched, setMatched] = useState(() => new Set());
  // 朝上、还没配上的牌：这一步的第一张，或者没配上、正亮着的两张。
  const [faceUp, setFaceUp] = useState([]);
  const [missing, setMissing] = useState(false);
  const [moves, setMoves] = useState(0);
  // 一局结束：{ status: won | lost, at }。
  const [end, setEnd] = useState(null);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [checked, setChecked] = useState(false);
  const busyRef = useRef(false);
  // 最新的局面版本：收到回应之后、页面重绘之前的点击也要带上它。
  const versionRef = useRef(0);
  const missTimerRef = useRef(null);
  // 连续配上的对数：没配上就清零，只用来飘字鼓励，不影响成绩。
  const [streak, setStreak] = useState(0);
  const [floaters, float] = useFloaters();
  const { bests, record } = useLocalBest('memory');

  const hideMiss = useCallback(() => {
    clearTimeout(missTimerRef.current);
    missTimerRef.current = null;
    setFaceUp([]);
    setMissing(false);
  }, []);

  const restore = useCallback(
    (view) => {
      const known = new Array(view.rows * view.cols).fill(null);
      view.matched.forEach(([index, face]) => {
        known[index] = face;
      });
      if (view.face_up) known[view.face_up.index] = view.face_up.symbol;
      hideMiss();
      versionRef.current = view.version;
      setGame({
        sessionId: view.session_id,
        difficulty: view.difficulty,
        rows: view.rows,
        cols: view.cols,
        pairs: view.pairs,
        moveLimit: view.move_limit,
        since: Date.now() - view.elapsed * 1000,
      });
      setSymbols(known);
      setMatched(new Set(view.matched.map(([index]) => index)));
      setFaceUp(view.face_up ? [view.face_up.index] : []);
      setMoves(view.moves);
      setDifficulty(view.difficulty);
      setStreak(0);
      setEnd(null);
      setResult(null);
    },
    [hideMiss],
  );

  const clearBoard = () => {
    hideMiss();
    setGame(null);
    setSymbols([]);
    setMatched(new Set());
    setMoves(0);
    setEnd(null);
  };

  // 刷新页面后接着玩进行中的一局。
  useEffect(() => {
    let alive = true;
    gameGet(STATE_URL, t).then((response) => {
      if (!alive) return;
      if (response.data?.session) restore(response.data.session);
      setChecked(true);
    });
    return () => {
      alive = false;
    };
  }, [restore, t]);

  useEffect(() => () => clearTimeout(missTimerRef.current), []);

  const settle = (settled, status, finalMoves = moves) => {
    clearTimeout(missTimerRef.current);
    missTimerRef.current = null;
    setMissing(false);
    const at = Date.now();
    const improved =
      status === 'won' && game
        ? record(game.difficulty, {
            moves: finalMoves,
            time: Math.round((at - game.since) / 1000),
          })
        : null;
    playSkillFeedback(settled, status === 'won');
    setEnd({ status, at, improved });
    setResult(settled);
    patch({
      balance: settled.balance,
      progress: settled.progress,
      best_score: settled.best_score,
    });
  };

  // 局面和服务端对不上时(版本冲突、网络中断、这局已被新开的一局作废)重新读取进行中的一局。
  const resync = async () => {
    const response = await gameGet(STATE_URL, t);
    if (response.data?.session) {
      restore(response.data.session);
    } else if (response.data) {
      clearBoard();
    }
    reload(true);
  };

  const start = async () => {
    if (busyRef.current || !turnstile.ready) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      withTurnstile('/api/game/memory/start', turnstile.take()),
      { difficulty },
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

  const playing = !!game && !end;

  const flip = async (index) => {
    if (busyRef.current || !playing || matched.has(index)) return;
    // 这一步已经翻开的第一张不能再翻；没配上的两张还亮着时，点哪张都先把它们翻回去再翻。
    if (!missing && faceUp.includes(index)) return;
    if (missing) hideMiss();
    busyRef.current = true;
    setBusy(true);
    setError('');
    try {
      const response = await gamePost(
        '/api/game/memory/flip',
        { session_id: game.sessionId, version: versionRef.current, index },
        t,
      );
      if (response.error) {
        setError(response.error);
        await resync();
        return;
      }
      const data = response.data;
      // 这局已经结算过(例如在别的标签页里玩完了)：服务端直接回应第一次的结算结果。
      if (data.status === undefined) {
        settle(data, data.detail?.won ? 'won' : 'lost');
        return;
      }
      versionRef.current = data.version;
      setMoves(data.moves);
      setSymbols((previous) => {
        const next = previous.slice();
        next[index] = data.symbol;
        if (data.previous) next[data.previous.index] = data.previous.symbol;
        return next;
      });
      if (data.first) {
        playSound('flip');
        setFaceUp([index]);
      } else if (data.matched) {
        const next = streak + 1;
        setStreak(next);
        playSound('merge', { pitch: 1 + Math.min(next - 1, 8) * 0.08 });
        if (next >= 2) {
          float(t('连对 ×{{count}}', { count: next }), {
            y: 45,
            kind: next >= 4 ? 'gold' : 'gain',
          });
        }
        setMatched((previous) =>
          new Set(previous).add(index).add(data.previous.index),
        );
        setFaceUp([]);
      } else {
        setStreak(0);
        playSound('flip', { pitch: 0.7 });
        setFaceUp([data.previous.index, index]);
        setMissing(true);
        if (data.status === 'playing') {
          missTimerRef.current = setTimeout(hideMiss, MISS_SHOW_MS);
        }
      }
      if (data.status === 'playing') return;
      // 结束后公开整副牌。
      setSymbols(data.layout);
      settle(data.result, data.status, data.moves);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const confirmRestart = () => {
    if (moves === 0 && faceUp.length === 0) {
      clearBoard();
      return;
    }
    Modal.confirm({
      title: t('重新选择难度'),
      content: t('这一局不会结算，开始新的一局后就作废了。'),
      onOk: () => {
        clearBoard();
        setError('');
      },
    });
  };

  const selected = levels.find((level) => level.difficulty === difficulty);
  const rows = game?.rows || selected?.rows || 4;
  const cols = game?.cols || selected?.cols || 4;
  const moveLimit = game?.moveLimit || selected?.move_limit || 0;
  const pairs = game?.pairs || selected?.pairs || 0;
  const cards = game ? symbols : new Array(rows * cols).fill(null);

  const leftText = rewardsLeftText(info?.rule, info?.progress, t);

  let outcome = null;
  if (end?.status === 'won') {
    outcome = {
      kind: 'win',
      text: t('全部配对成功，用了 {{moves}} 步', { moves }),
      note: localBestNote(end.improved, t),
    };
  } else if (end) {
    outcome = {
      kind: 'loss',
      text: t('步数用完了，还有 {{left}} 对没配上', {
        left: pairs - matched.size / 2,
      }),
    };
  }
  let reward = null;
  if (end?.status === 'won' && result) {
    reward = skillResultMessage(result, t, symbol, { byLevel: true });
  }

  const side = info && (
    <SkillSide
      info={info}
      symbol={symbol}
      scoreName={meta.scoreName}
      levelNames={levelNames}
      extra={
        <SideBox label={t('操作说明')}>
          <RuleList
            items={[
              t('每一步翻开两张牌，图案相同就配上了，不同就翻回去'),
              t('在步数上限之内配完所有对就赢了，用完步数这局结束'),
              t('没配上的两张会亮一会儿，直接点下一张可以跳过'),
            ]}
          />
        </SideBox>
      }
      after={
        <LocalBestBox
          bests={bests}
          levels={levels.map((level) => ({
            key: level.difficulty,
            name: levelNames[level.difficulty - 1],
          }))}
        />
      }
    />
  );

  return (
    <GameLayout
      title={meta.name}
      loading={loading}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={symbol}
      side={side}
      className='memory-page'
    >
      <div className='games-stage'>
        <div className='game-hud'>
          <span>
            {t('步数')}
            <strong
              className={
                playing && moveLimit - moves <= 3 ? 'memory-hud-low' : ''
              }
            >
              {moves} / {moveLimit}
            </strong>
          </span>
          <span>
            {t('已配对')}
            <strong>
              {matched.size / 2} / {pairs}
            </strong>
          </span>
          <span>
            {t('连对')}
            <strong>{streak}</strong>
          </span>
          <span>
            {t('用时')}
            <strong>
              {game ? (
                <GameClock since={game.since} stoppedAt={end?.at} />
              ) : (
                formatClock(0)
              )}
            </strong>
          </span>
        </div>

        <div
          className={`memory-board is-cols-${cols} ${playing ? 'is-playing' : ''}`}
          style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
        >
          {cards.map((face, index) => {
            const isMatched = matched.has(index);
            // 结束后翻开知道图案的牌(正常结束时是整副牌)。
            const up =
              isMatched || faceUp.includes(index) || (!!end && face !== null);
            const Icon = face === null ? null : SYMBOL_ICONS[face];
            const classes = ['memory-card'];
            if (up) classes.push('is-up');
            if (isMatched) classes.push('is-matched');
            if (missing && faceUp.includes(index)) classes.push('is-miss');
            if (end && !isMatched) classes.push('is-unmatched');
            return (
              <button
                key={index}
                type='button'
                tabIndex={playing ? 0 : -1}
                className={classes.join(' ')}
                disabled={
                  busy ||
                  !playing ||
                  isMatched ||
                  (!missing && faceUp.includes(index))
                }
                aria-busy={busy}
                aria-label={t('第 {{row}} 行第 {{col}} 列，{{state}}', {
                  row: Math.floor(index / cols) + 1,
                  col: (index % cols) + 1,
                  state: up
                    ? `${t('图案 {{number}}', { number: face + 1 })}${isMatched ? `，${t('已配对')}` : ''}`
                    : t('未翻开'),
                })}
                onClick={() => flip(index)}
              >
                <span className='memory-card-inner'>
                  <span className='memory-card-back' />
                  <span className={`memory-card-front memory-symbol-${face}`}>
                    {Icon && <Icon />}
                  </span>
                </span>
              </button>
            );
          })}
          <Floaters items={floaters} />
        </div>

        {busy && playing && (
          <div role='status' className='games-stage-counter'>
            {t('正在翻牌，请稍候')}
          </div>
        )}
        {outcome && (
          <StageMessage kind={outcome.kind} note={outcome.note}>
            {outcome.text}
          </StageMessage>
        )}
        {reward && (
          <StageMessage kind={reward.kind} note={reward.note}>
            {reward.text}
          </StageMessage>
        )}
        {error && <StageMessage kind='error'>{error}</StageMessage>}

        {playing && (
          <div className='game-controls'>
            <button
              type='button'
              className='game-chip'
              disabled={busy}
              onClick={confirmRestart}
            >
              <RotateCcw size={14} className='memory-chip-icon' />
              {t('重新选择难度')}
            </button>
          </div>
        )}

        {!playing && checked && (
          <div className='game-controls memory-start'>
            <p className='memory-summary'>
              {skillRuleSummary(info, t, symbol, meta.scoreName, levelNames)}
            </p>
            <div className='game-chips'>
              {levels.map((level) => {
                const coins = levelReward(info?.rule?.tiers, level.difficulty);
                return (
                  <button
                    key={level.difficulty}
                    type='button'
                    className={
                      level.difficulty === difficulty
                        ? 'game-chip is-active'
                        : 'game-chip'
                    }
                    onClick={() => setDifficulty(level.difficulty)}
                  >
                    {levelNames[level.difficulty - 1]}
                    <span className='memory-chip-note'>
                      {t('{{pairs}} 对 · {{moves}} 步', {
                        pairs: level.pairs,
                        moves: level.move_limit,
                      })}
                    </span>
                    <span className='memory-chip-reward'>
                      {coins > 0 ? `+${coins} ${symbol}` : t('不发奖')}
                    </span>
                  </button>
                );
              })}
            </div>
            <button
              type='button'
              className='games-play-button is-free'
              disabled={busy || !turnstile.ready}
              onClick={start}
            >
              <Play size={16} className='memory-play-icon' />
              {busy ? t('正在开局…') : end ? t('再来一局') : t('开始游戏')}
            </button>
            <PlayTurnstile turnstile={turnstile} />
            <div className='games-stage-counter'>{leftText}</div>
          </div>
        )}
      </div>
    </GameLayout>
  );
};

export default Memory;
