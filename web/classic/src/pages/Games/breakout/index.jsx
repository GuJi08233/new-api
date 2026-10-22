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
import { Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import GameLayout from '../common/GameLayout';
import { usePendingReplay } from '../common/usePendingReplay';
import PendingReplay from '../common/PendingReplay';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gamePost, withTurnstile } from '../common/api';
import { SkillSide, skillResultMessage, formatClock } from '../common/skill';
import { useGameInfo } from '../common/useGameInfo';
import { PlayTurnstile, usePlayTurnstile } from '../common/usePlayTurnstile';
import {
  createGame,
  step,
  WIDTH,
  HEIGHT,
  FPS,
  MAX_TICKS,
  PADDLE_Y,
  RADIUS,
  COLUMNS,
  ROWS,
  BRICK_WIDTH,
  BRICK_HEIGHT,
  BRICK_LEFT,
  BRICK_TOP,
  BRICK_GAP,
} from './engine';
import '../common/puzzles.css';

const COLORS = [
  '#fb7185',
  '#fb923c',
  '#fbbf24',
  '#34d399',
  '#38bdf8',
  '#a78bfa',
];

function drawGame(ctx, game) {
  ctx.fillStyle = '#0b1326';
  ctx.fillRect(0, 0, WIDTH, HEIGHT);
  ctx.strokeStyle = '#ffffff08';
  ctx.lineWidth = 1;
  for (let x = 0; x < WIDTH; x += 28) {
    ctx.beginPath();
    ctx.moveTo(x, 0);
    ctx.lineTo(x, HEIGHT);
    ctx.stroke();
  }
  for (let y = 0; y < HEIGHT; y += 28) {
    ctx.beginPath();
    ctx.moveTo(0, y);
    ctx.lineTo(WIDTH, y);
    ctx.stroke();
  }
  game.bricks.forEach((hp, i) => {
    if (!hp) return;
    const x = BRICK_LEFT + (i % COLUMNS) * (BRICK_WIDTH + BRICK_GAP);
    const y = BRICK_TOP + Math.floor(i / COLUMNS) * (BRICK_HEIGHT + BRICK_GAP);
    ctx.fillStyle = COLORS[Math.floor(i / COLUMNS)];
    ctx.beginPath();
    ctx.roundRect(x, y, BRICK_WIDTH, BRICK_HEIGHT, 4);
    ctx.fill();
    if (hp > 1) {
      ctx.fillStyle = '#ffffff88';
      ctx.fillRect(x + 4, y + 3, BRICK_WIDTH - 8, 2);
    }
    ctx.fillStyle = '#0b1326';
    ctx.textAlign = 'center';
    ctx.font = '12px sans-serif';
    if (i % 13 === 0) ctx.fillText('●●', x + BRICK_WIDTH / 2, y + 14);
    else if (i % 11 === 0) ctx.fillText('↔', x + BRICK_WIDTH / 2, y + 14);
  });
  const half = game.wide > 0 ? 56 : 36;
  ctx.fillStyle = game.wide > 0 ? '#fbbf24' : '#7dd3fc';
  ctx.shadowColor = ctx.fillStyle;
  ctx.shadowBlur = 12;
  ctx.beginPath();
  ctx.roundRect(game.paddle - half, PADDLE_Y, half * 2, 10, 5);
  ctx.fill();
  ctx.fillStyle = '#fff';
  game.balls.forEach((ball) => {
    ctx.beginPath();
    ctx.arc(ball.x, ball.y, RADIUS, 0, Math.PI * 2);
    ctx.fill();
  });
  ctx.shadowBlur = 0;
  if (game.wait > 0 && !game.over) {
    ctx.fillStyle = '#ffffffaa';
    ctx.font = 'bold 28px sans-serif';
    ctx.fillText(String(Math.ceil(game.wait / FPS)), WIDTH / 2, 310);
  }
}

const Breakout = () => {
  const { t } = useTranslation();
  const { info, loading, unavailable, patch, reload } = useGameInfo('breakout');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const [phase, setPhase] = useState('idle');
  const [session, setSession] = useState(null);
  const [hud, setHud] = useState({ score: 0, lives: 3, broken: 0, ticks: 0 });
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const canvasRef = useRef(null);
  const engineRef = useRef(null);
  const inputsRef = useRef([]);
  const targetRef = useRef(WIDTH / 2);
  const keysRef = useRef(new Set());
  const busyRef = useRef(false);
  const finishRef = useRef(null);
  // sessionRef 与 submittedRef 给离开本页时的结算用：这一局有没有交过成绩(或被服务端明确拒绝)。
  const sessionRef = useRef(null);
  const submittedRef = useRef(false);
  const pointerRef = useRef(null);
  const replay = usePendingReplay('breakout', {
    snapshot: () =>
      !submittedRef.current && sessionRef.current && inputsRef.current.length
        ? {
            session_id: sessionRef.current.session_id,
            inputs: inputsRef.current,
          }
        : null,
    onSettled: () => reload(true),
  });
  if (!engineRef.current) engineRef.current = createGame(1);

  const finish = async () => {
    if (
      busyRef.current ||
      submittedRef.current ||
      !session ||
      !inputsRef.current.length
    )
      return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    setPhase('ended');
    const game = engineRef.current;
    setHud({
      score: game.score,
      lives: game.lives,
      broken: game.broken,
      ticks: game.ticks,
    });
    const response = await replay.send({
      session_id: session.session_id,
      inputs: inputsRef.current,
    });
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      // 没有得到答复、服务端暂时出错或被限流时整局记录还在，留在这里重新提交；服务端明确拒绝的一局重交也不会成功，
      // 回到可以开新局的状态。
      if (!response.network && !response.limited) {
        submittedRef.current = true;
        setPhase('done');
      }
      return;
    }
    submittedRef.current = true;
    setResult(response.data);
    setPhase('done');
    patch({
      balance: response.data.balance,
      progress: response.data.progress,
      best_score: response.data.best_score,
    });
  };
  finishRef.current = finish;

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return undefined;
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = WIDTH * ratio;
    canvas.height = HEIGHT * ratio;
    const ctx = canvas.getContext('2d');
    if (!ctx) return undefined;
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    drawGame(ctx, engineRef.current);
    if (phase !== 'playing') return undefined;
    let frame,
      last = 0,
      accumulator = 0;
    const animate = (timestamp) => {
      if (last) accumulator += Math.min(250, timestamp - last);
      last = timestamp;
      const game = engineRef.current;
      while (accumulator >= 1000 / FPS && !game.over) {
        const left =
          keysRef.current.has('ArrowLeft') || keysRef.current.has('a');
        const right =
          keysRef.current.has('ArrowRight') || keysRef.current.has('d');
        if (left !== right)
          targetRef.current = Math.max(
            0,
            Math.min(WIDTH, targetRef.current + (right ? 7 : -7)),
          );
        const target = Math.round(targetRef.current);
        inputsRef.current.push(target);
        step(game, target);
        accumulator -= 1000 / FPS;
      }
      drawGame(ctx, game);
      if (game.ticks % 12 === 0 || game.over)
        setHud({
          score: game.score,
          lives: game.lives,
          broken: game.broken,
          ticks: game.ticks,
        });
      if (game.over) {
        finishRef.current();
        return;
      }
      frame = requestAnimationFrame(animate);
    };
    frame = requestAnimationFrame(animate);
    return () => cancelAnimationFrame(frame);
  }, [loading, unavailable, phase]);

  useEffect(() => {
    const down = (event) => {
      if (
        phase !== 'playing' ||
        event.ctrlKey ||
        event.metaKey ||
        event.altKey ||
        event.target.closest?.('input,textarea,[contenteditable="true"]')
      )
        return;
      const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
      if (['ArrowLeft', 'ArrowRight', 'a', 'd'].includes(key)) {
        event.preventDefault();
        keysRef.current.add(key);
      }
    };
    const up = (event) =>
      keysRef.current.delete(
        event.key.length === 1 ? event.key.toLowerCase() : event.key,
      );
    const pause = () => {
      keysRef.current.clear();
      setPhase((previous) => (previous === 'playing' ? 'paused' : previous));
    };
    const hidden = () => {
      if (document.hidden) pause();
    };
    // 刷新或关闭页面前提醒一下：页面没了，进行中的一局就交不上了。
    const beforeUnload = (event) => {
      if (
        (phase !== 'playing' && phase !== 'paused') ||
        !inputsRef.current.length
      )
        return;
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('keydown', down);
    window.addEventListener('keyup', up);
    window.addEventListener('blur', pause);
    window.addEventListener('beforeunload', beforeUnload);
    document.addEventListener('visibilitychange', hidden);
    return () => {
      window.removeEventListener('keydown', down);
      window.removeEventListener('keyup', up);
      window.removeEventListener('blur', pause);
      window.removeEventListener('beforeunload', beforeUnload);
      document.removeEventListener('visibilitychange', hidden);
    };
  }, [phase]);

  const start = async () => {
    if (busyRef.current || !turnstile.ready) return;
    if (replay.hasPending()) {
      replay.release();
      return;
    }
    busyRef.current = true;
    setBusy(true);
    setError('');
    const response = await gamePost(
      withTurnstile('/api/game/breakout/start', turnstile.take()),
      {},
      t,
    );
    busyRef.current = false;
    setBusy(false);
    if (response.error) {
      setError(response.error);
      return;
    }
    engineRef.current = createGame(response.data.seed);
    sessionRef.current = response.data;
    replay.beginSession(response.data.session_id);
    submittedRef.current = false;
    inputsRef.current = [];
    targetRef.current = WIDTH / 2;
    keysRef.current.clear();
    setSession(response.data);
    setResult(null);
    setHud({ score: 0, lives: 3, broken: 0, ticks: 0 });
    setPhase('playing');
    canvasRef.current?.focus();
  };

  const pointer = (event) => {
    if (
      phase !== 'playing' ||
      event.isPrimary === false ||
      (pointerRef.current !== null && pointerRef.current !== event.pointerId)
    )
      return;
    if (event.type === 'pointerdown') pointerRef.current = event.pointerId;
    const bounds = event.currentTarget.getBoundingClientRect();
    targetRef.current = Math.max(
      0,
      Math.min(WIDTH, ((event.clientX - bounds.left) / bounds.width) * WIDTH),
    );
    if (event.type === 'pointerdown')
      event.currentTarget.setPointerCapture(event.pointerId);
  };
  const reward = result && skillResultMessage(result, t, info?.symbol);
  const active = phase === 'playing' || phase === 'paused';

  return (
    <GameLayout
      title={t('打砖块')}
      loading={loading}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={info?.symbol}
      side={
        info && (
          <SkillSide
            info={info}
            symbol={info.symbol}
            scoreName={t('分数')}
            extra={
              <SideBox label={t('操作说明')}>
                <RuleList
                  items={[
                    t('移动鼠标、手指或左右方向键控制挡板，接住小球击碎砖块'),
                    t('双球砖块增加小球，箭头砖块让挡板加宽十五秒'),
                    t('三条生命，全部小球落下才会扣除一条；每局最多五分钟'),
                    t('击中砖块得五分，击碎再得十分，清空棋盘额外得一百分'),
                  ]}
                />
              </SideBox>
            }
          />
        )
      }
    >
      <PendingReplay replay={replay} />
      <div className='games-stage'>
        <div className='game-hud'>
          <span>
            {t('分数')}
            <strong>{hud.score}</strong>
          </span>
          <span>
            {t('生命')}
            <strong>{hud.lives}</strong>
          </span>
          <span>
            {t('砖块')}
            <strong>
              {hud.broken}/{ROWS * COLUMNS}
            </strong>
          </span>
          <span>
            {t('剩余时间')}
            <strong>{formatClock((MAX_TICKS - hud.ticks) / FPS)}</strong>
          </span>
        </div>
        <div className='breakout-screen'>
          <canvas
            ref={canvasRef}
            tabIndex={0}
            aria-label={t('打砖块游戏区域')}
            onPointerDown={pointer}
            onPointerMove={pointer}
            onPointerUp={(event) => {
              if (pointerRef.current === event.pointerId)
                pointerRef.current = null;
            }}
            onPointerCancel={(event) => {
              if (pointerRef.current === event.pointerId)
                pointerRef.current = null;
            }}
          />
          {phase !== 'playing' && (
            <div className='breakout-overlay'>
              {phase === 'paused'
                ? t('已暂停')
                : phase === 'idle'
                  ? t('准备接球！')
                  : engineRef.current.won
                    ? t('全部消除，挑战成功！')
                    : t('本局结束，再试一次吧')}
            </div>
          )}
        </div>
        <div className='puzzle-toolbar'>
          {(phase === 'idle' || phase === 'done') && (
            <Button
              theme='solid'
              loading={busy}
              disabled={!turnstile.ready || replay.pending.length > 0}
              onClick={start}
            >
              {t('开始游戏')}
            </Button>
          )}
          {active && (
            <>
              <Button
                onClick={() =>
                  setPhase(phase === 'playing' ? 'paused' : 'playing')
                }
              >
                {phase === 'playing' ? t('暂停') : t('继续')}
              </Button>
              <Button
                disabled={!inputsRef.current.length}
                loading={busy}
                onClick={finish}
              >
                {t('结束并结算')}
              </Button>
            </>
          )}
          {phase === 'ended' && (
            <Button loading={busy} onClick={finish}>
              {t('提交成绩')}
            </Button>
          )}
        </div>
        {(phase === 'idle' || phase === 'done') && (
          <PlayTurnstile turnstile={turnstile} />
        )}
        {(error || reward) && (
          <StageMessage
            kind={error ? 'error' : reward?.kind}
            note={reward?.note}
          >
            {error || reward?.text}
          </StageMessage>
        )}
      </div>
    </GameLayout>
  );
};

export default Breakout;
