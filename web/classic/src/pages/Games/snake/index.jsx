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
import { Button, Modal, Spin } from '@douyinfe/semi-ui';
import {
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Flag,
  Pause,
  Play,
  Trophy,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API } from '../../../helpers';
import { getGameMetaMap } from '../../../constants';
import { useActualTheme } from '../../../context/Theme';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { formatNumber, gamePost, withTurnstile } from '../common/api';
import {
  ReplayRunControls,
  SkillSide,
  skillResultMessage,
  skillRuleSummary,
} from '../common/skill';
import { useGameInfo } from '../common/useGameInfo';
import { usePlayTurnstile } from '../common/usePlayTurnstile';
import {
  DIRECTION,
  HEIGHT,
  MAX_STEPS,
  NO_TURN,
  SnakeGame,
  TurnRecorder,
  WIDTH,
  isTurn,
} from './engine';
import './snake.css';

const START_URL = '/api/game/snake/start';
const FINISH_URL = '/api/game/snake/finish';

const CELL = 24;
const BOARD_PX = WIDTH * CELL;
// 页面卡顿或切回前台时，一帧最多补推 0.25 秒，多出的时间直接丢弃：游戏内时长只会慢于真实时间，
// 服务端据此拒绝快于真实时间的记录。
const MAX_FRAME_MS = 250;
// 连按的方向最多排这么多个，每一步用掉一个。
const QUEUE_LIMIT = 3;
// 手指或鼠标至少拖动这么远(像素)才算一次滑动。
const SWIPE_PX = 24;

// 按 KeyboardEvent.code 匹配，不受输入法和键盘布局影响。
const KEY_DIRECTIONS = {
  ArrowUp: DIRECTION.UP,
  KeyW: DIRECTION.UP,
  ArrowRight: DIRECTION.RIGHT,
  KeyD: DIRECTION.RIGHT,
  ArrowDown: DIRECTION.DOWN,
  KeyS: DIRECTION.DOWN,
  ArrowLeft: DIRECTION.LEFT,
  KeyA: DIRECTION.LEFT,
};

// 蛇头朝向对应的单位向量，按 DIRECTION 编号排列，用来摆眼睛。
const FACING = [
  [0, -1],
  [1, 0],
  [0, 1],
  [-1, 0],
];

// 画布颜色按明暗主题各一套，场地的格子随主题变淡或变深。
const PALETTES = {
  light: {
    grid: 'rgba(15, 23, 42, 0.045)',
    body: '#10b981',
    head: '#059669',
    dead: '#ef4444',
    food: '#f43f5e',
  },
  dark: {
    grid: 'rgba(255, 255, 255, 0.04)',
    body: '#34d399',
    head: '#10b981',
    dead: '#f87171',
    food: '#fb7185',
  },
};

// 按画布在页面上的实际大小与设备像素比设置分辨率，返回按逻辑坐标绘制的上下文。
function prepareCanvas(canvas, width, height) {
  const ratio =
    (window.devicePixelRatio || 1) * ((canvas.clientWidth || width) / width);
  const pixelWidth = Math.round(width * ratio);
  const pixelHeight = Math.round(height * ratio);
  if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
    canvas.width = pixelWidth;
    canvas.height = pixelHeight;
  }
  const ctx = canvas.getContext('2d');
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  ctx.clearRect(0, 0, width, height);
  return ctx;
}

// 画出场地、食物和蛇；game 为空时只画场地。蛇身每节是一个圆角方块，相邻两节之间补上缝隙，连成一条。
function drawBoard(canvas, game, palette) {
  if (!canvas) return;
  const ctx = prepareCanvas(canvas, BOARD_PX, BOARD_PX);
  ctx.fillStyle = palette.grid;
  for (let y = 0; y < HEIGHT; y++) {
    for (let x = y % 2; x < WIDTH; x += 2) {
      ctx.fillRect(x * CELL, y * CELL, CELL, CELL);
    }
  }
  if (!game) return;

  const food = game.foodCell();
  if (food) {
    ctx.fillStyle = palette.food;
    ctx.beginPath();
    ctx.arc(
      (food[0] + 0.5) * CELL,
      (food[1] + 0.5) * CELL,
      CELL * 0.32,
      0,
      Math.PI * 2,
    );
    ctx.fill();
  }

  const inset = 3;
  const size = CELL - 2 * inset;
  const cells = game.cells();
  ctx.fillStyle = palette.body;
  cells.forEach(([x, y], index) => {
    ctx.beginPath();
    ctx.roundRect(x * CELL + inset, y * CELL + inset, size, size, 6);
    ctx.fill();
    if (index === 0) return;
    const [px, py] = cells[index - 1];
    if (px !== x) {
      ctx.fillRect(
        Math.max(px, x) * CELL - inset,
        y * CELL + inset,
        2 * inset,
        size,
      );
    } else {
      ctx.fillRect(
        x * CELL + inset,
        Math.max(py, y) * CELL - inset,
        size,
        2 * inset,
      );
    }
  });

  const [hx, hy] = cells[cells.length - 1];
  ctx.fillStyle = game.over ? palette.dead : palette.head;
  ctx.beginPath();
  ctx.roundRect(hx * CELL + 2, hy * CELL + 2, CELL - 4, CELL - 4, 7);
  ctx.fill();
  const [fx, fy] = FACING[game.direction];
  const cx = (hx + 0.5) * CELL;
  const cy = (hy + 0.5) * CELL;
  for (const side of [-1, 1]) {
    const ex = cx + fx * CELL * 0.14 - fy * side * CELL * 0.2;
    const ey = cy + fy * CELL * 0.14 + fx * side * CELL * 0.2;
    ctx.fillStyle = '#ffffff';
    ctx.beginPath();
    ctx.arc(ex, ey, CELL * 0.13, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = '#0f172a';
    ctx.beginPath();
    ctx.arc(ex + fx * 1.2, ey + fy * 1.2, CELL * 0.065, 0, Math.PI * 2);
    ctx.fill();
  }
}

// 结算请求：服务端从开局的种子重放推进过的步数与每次生效的转向，成绩与游戏内时长以重放为准。
function finishPayload(run) {
  return {
    session_id: run.sessionId,
    steps: run.game.steps,
    inputs: run.recorder.inputs,
  };
}

const Snake = () => {
  const { t } = useTranslation();
  const meta = getGameMetaMap(t).snake;
  const { loading, unavailable, info, patch, reload } = useGameInfo('snake');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const palette = PALETTES[useActualTheme() === 'dark' ? 'dark' : 'light'];
  // idle 还没开局，starting 开局中，ready 等玩家按第一个方向，playing 对局中，paused 暂停，
  // submitting 结算中，result 已结算(或被拒绝)，error 结算没有得到答复。
  const [phase, setPhase] = useState('idle');
  const [stats, setStats] = useState({ score: 0, length: 0 });
  const [result, setResult] = useState(null);
  const [notice, setNotice] = useState(null);
  const canvasRef = useRef(null);
  // 当前一局：会话、引擎、转向记录、排队的方向与还没用完的时间。渲染循环直接读写它，不经过 React 状态。
  const runRef = useRef(null);
  const paletteRef = useRef(palette);
  const swipeRef = useRef(null);
  // 确认框打开期间不响应游戏按键，免得在确认框后面继续游戏。
  const confirmingRef = useRef(false);
  const mountedRef = useRef(false);
  const symbol = info?.symbol || '';
  const steering = phase === 'ready' || phase === 'playing';
  const active = steering || phase === 'paused';

  const submit = useCallback(async () => {
    const run = runRef.current;
    if (!run || run.submitting || run.submitted) return;
    run.submitting = true;
    setPhase('submitting');
    const response = await gamePost(FINISH_URL, finishPayload(run), t);
    run.submitting = false;
    if (!mountedRef.current || runRef.current !== run) return;
    // 没有得到答复或被限流时整局记录还在，可以重新提交；已经结算过的话，服务端会返回第一次的结果。
    if (response.network || response.limited) {
      setPhase('error');
      return;
    }
    run.submitted = true;
    if (response.error) {
      setResult({ rejected: response.error });
      setPhase('result');
      return;
    }
    const data = response.data;
    setResult({
      ...data,
      over: run.game.over,
      localScore: run.game.score,
      newRecord: data.score > 0 && data.score > run.previousBest,
    });
    patch({
      balance: data.balance,
      progress: data.progress,
      best_score: data.best_score,
    });
    setPhase('result');
  }, [patch, t]);

  // 玩家按下一个方向：与队尾的方向(没有排队时是蛇头当前的方向)相同或相反的不排，每一步用掉一个。
  // 开局后的第一个方向让蛇动起来。
  const steer = useCallback((direction) => {
    const run = runRef.current;
    if (!run || run.game.over) return;
    const { queue, game } = run;
    const last = queue.length > 0 ? queue[queue.length - 1] : game.direction;
    if (queue.length < QUEUE_LIMIT && isTurn(last, direction)) {
      queue.push(direction);
    }
    setPhase((current) => (current === 'ready' ? 'playing' : current));
  }, []);

  const pause = useCallback(() => {
    setPhase((current) => (current === 'playing' ? 'paused' : current));
  }, []);

  const start = async () => {
    if (phase === 'starting' || !turnstile.ready) return;
    setPhase('starting');
    setNotice(null);
    const response = await gamePost(
      withTurnstile(START_URL, turnstile.take()),
      {},
      t,
    );
    if (!mountedRef.current) return;
    if (response.error) {
      setNotice({ kind: 'error', text: response.error });
      setPhase(result ? 'result' : 'idle');
      return;
    }
    const game = new SnakeGame(response.data.seed);
    runRef.current = {
      sessionId: response.data.session_id,
      game,
      recorder: new TurnRecorder(),
      queue: [],
      elapsed: 0,
      previousBest: info?.best_score || 0,
    };
    setStats({ score: 0, length: game.length });
    setResult(null);
    // 开始按钮保留焦点时，空格和回车会再次触发它。
    document.activeElement?.blur?.();
    setPhase('ready');
  };

  const endGame = () => {
    pause();
    confirmingRef.current = true;
    Modal.confirm({
      title: t('结束本局'),
      content: t('结束后按当前成绩结算，不能继续。'),
      onOk: () => {
        submit();
      },
      afterClose: () => {
        confirmingRef.current = false;
      },
    });
  };

  // 放弃没有得到答复的结算。结算可能已经完成，重新读一次余额与今日进度。
  const abandon = () => {
    runRef.current = null;
    setPhase('idle');
    reload(true);
  };

  useEffect(() => {
    paletteRef.current = palette;
  }, [palette]);

  // 渲染循环：按真实时间推进，每一步的步长由引擎按得分给出；撞上了或到了步数上限就提交结算。
  useEffect(() => {
    if (phase !== 'playing') return undefined;
    const run = runRef.current;
    let frame = 0;
    let last = performance.now();
    const loop = (now) => {
      run.elapsed += Math.min(now - last, MAX_FRAME_MS);
      last = now;
      const { game } = run;
      while (
        !game.over &&
        game.steps < MAX_STEPS &&
        run.elapsed >= game.stepMs()
      ) {
        run.elapsed -= game.stepMs();
        const step = game.steps;
        const turn = run.queue.length > 0 ? run.queue.shift() : NO_TURN;
        if (game.step(turn)) run.recorder.record(step, turn);
      }
      drawBoard(canvasRef.current, game, paletteRef.current);
      setStats((previous) =>
        previous.score === game.score && previous.length === game.length
          ? previous
          : { score: game.score, length: game.length },
      );
      if (game.over || game.steps >= MAX_STEPS) {
        submit();
        return;
      }
      frame = requestAnimationFrame(loop);
    };
    frame = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(frame);
  }, [phase, submit]);

  // 不在推进时画一次当前的盘面，窗口大小或主题变了再画一次。
  useEffect(() => {
    if (loading || phase === 'playing') return undefined;
    const draw = () =>
      drawBoard(canvasRef.current, runRef.current?.game, palette);
    draw();
    window.addEventListener('resize', draw);
    return () => window.removeEventListener('resize', draw);
  }, [loading, phase, palette]);

  // 按键：方向键与 WASD 转向，空格或 P 暂停与继续，Esc 暂停，对局中这些键都不滚动页面。一局刚结束时玩家常常
  // 还在按，焦点不在控件上时也不让它们把页面滚走。
  useEffect(() => {
    if (phase === 'idle') return undefined;
    const onKeyDown = (event) => {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      const direction = KEY_DIRECTIONS[event.code];
      const toggle = event.code === 'Space' || event.code === 'KeyP';
      if (direction === undefined && !toggle && event.code !== 'Escape') {
        return;
      }
      if (!active) {
        if (event.target === document.body) event.preventDefault();
        return;
      }
      if (
        event.target.closest?.('input, textarea, select, [contenteditable]')
      ) {
        return;
      }
      event.preventDefault();
      if (confirmingRef.current) return;
      if (direction !== undefined) {
        if (steering) steer(direction);
        return;
      }
      if (event.code === 'Escape') {
        pause();
        return;
      }
      // 空格与 P：对局中暂停，暂停时继续，还没动起来时让蛇出发。
      if (!event.repeat) {
        setPhase((current) => {
          if (current === 'playing') return 'paused';
          return current === 'paused' || current === 'ready'
            ? 'playing'
            : current;
        });
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [phase, active, steering, steer, pause]);

  // 对局中切到别的窗口或标签页时自动暂停；刷新或关闭页面时这局来不及结算，先让浏览器确认一次。
  useEffect(() => {
    if (!active) return undefined;
    const onVisibilityChange = () => {
      if (document.hidden) pause();
    };
    const onBeforeUnload = (event) => {
      if (!runRef.current?.game.steps) return;
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('blur', pause);
    window.addEventListener('beforeunload', onBeforeUnload);
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => {
      window.removeEventListener('blur', pause);
      window.removeEventListener('beforeunload', onBeforeUnload);
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [active, pause]);

  // 离开本页时把进行中的一局按当前成绩结算，成绩不会白白作废。页面已经不在了，不等答复，结果以游戏币明细为准。
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      const run = runRef.current;
      if (!run || run.submitting || run.submitted || run.game.steps === 0) {
        return;
      }
      API.post(FINISH_URL, finishPayload(run), {
        skipErrorHandler: true,
      }).catch(() => {});
    };
  }, []);

  const onPointerDown = (event) => {
    if (!steering) return;
    swipeRef.current = {
      id: event.pointerId,
      x: event.clientX,
      y: event.clientY,
    };
    event.currentTarget.setPointerCapture?.(event.pointerId);
  };

  const onPointerUp = (event) => {
    const swipe = swipeRef.current;
    swipeRef.current = null;
    if (!steering || !swipe || swipe.id !== event.pointerId) return;
    const dx = event.clientX - swipe.x;
    const dy = event.clientY - swipe.y;
    if (Math.max(Math.abs(dx), Math.abs(dy)) < SWIPE_PX) return;
    if (Math.abs(dx) > Math.abs(dy)) {
      steer(dx > 0 ? DIRECTION.RIGHT : DIRECTION.LEFT);
    } else {
      steer(dy > 0 ? DIRECTION.DOWN : DIRECTION.UP);
    }
  };

  let message = notice;
  if (!message) {
    if (phase === 'error') {
      message = {
        kind: 'error',
        text: t('网络异常，结算没有完成，可以重新提交'),
      };
    } else if (phase === 'result') {
      message = result.rejected
        ? { kind: 'error', text: result.rejected }
        : skillResultMessage(result, t, symbol);
    } else if (active || phase === 'submitting') {
      message = {
        kind: '',
        text: t('吃到豆子会变长、变快，别撞墙也别咬到自己'),
      };
    } else {
      message = {
        kind: '',
        text: skillRuleSummary(info, t, symbol, meta.scoreName),
      };
    }
  }

  let overlay = null;
  if (phase === 'ready') {
    overlay = (
      <div className='snake-overlay is-hint'>
        <div className='snake-overlay-title'>{t('准备好了吗？')}</div>
        <div className='snake-overlay-text'>
          {t('按方向键或在场地上滑动，蛇就开始前进')}
        </div>
      </div>
    );
  } else if (phase === 'paused') {
    overlay = (
      <div className='snake-overlay'>
        <div className='snake-overlay-title'>{t('已暂停')}</div>
      </div>
    );
  } else if (phase === 'submitting') {
    overlay = (
      <div className='snake-overlay'>
        <Spin />
        <div className='snake-overlay-text'>{t('正在结算…')}</div>
      </div>
    );
  } else if (phase === 'error') {
    overlay = (
      <div className='snake-overlay'>
        <div className='snake-overlay-title'>{t('结算未完成')}</div>
      </div>
    );
  } else if (phase === 'result' && result.rejected) {
    overlay = (
      <div className='snake-overlay'>
        <div className='snake-overlay-title'>{t('本局未能结算')}</div>
      </div>
    );
  } else if (phase === 'result') {
    overlay = (
      <div className='snake-overlay is-result'>
        <div className='snake-overlay-title'>
          {result.over ? t('游戏结束') : t('本局结束')}
        </div>
        {result.newRecord && (
          <div className='snake-record'>
            <Trophy size={14} />
            {t('新纪录')}
          </div>
        )}
        <div className='snake-overlay-score'>{formatNumber(result.score)}</div>
        <div className='snake-overlay-text'>{meta.scoreName}</div>
        {result.score !== result.localScore && (
          <div className='snake-overlay-text'>
            {t('成绩以服务器重放结果为准')}
          </div>
        )}
      </div>
    );
  }

  const pad = [
    [DIRECTION.UP, 'is-up', <ArrowUp size={22} />, t('向上')],
    [DIRECTION.LEFT, 'is-left', <ArrowLeft size={22} />, t('向左')],
    [DIRECTION.RIGHT, 'is-right', <ArrowRight size={22} />, t('向右')],
    [DIRECTION.DOWN, 'is-down', <ArrowDown size={22} />, t('向下')],
  ];

  let controls = null;
  if (steering) {
    controls = (
      <>
        <Button
          icon={<Pause size={14} />}
          disabled={phase === 'ready'}
          onClick={pause}
        >
          {t('暂停')}
        </Button>
        <div className='snake-pad'>
          {pad.map(([direction, place, icon, label]) => (
            <button
              key={direction}
              type='button'
              className={`snake-pad-button ${place}`}
              aria-label={label}
              onPointerDown={(event) => {
                event.preventDefault();
                steer(direction);
              }}
              onContextMenu={(event) => event.preventDefault()}
            >
              {icon}
            </button>
          ))}
        </div>
      </>
    );
  } else if (phase === 'paused') {
    controls = (
      <>
        <Button
          theme='solid'
          icon={<Play size={14} />}
          onClick={() => setPhase('playing')}
        >
          {t('继续游戏')}
        </Button>
        <Button icon={<Flag size={14} />} onClick={endGame}>
          {t('结束本局')}
        </Button>
      </>
    );
  } else if (phase !== 'submitting') {
    controls = (
      <ReplayRunControls
        phase={phase}
        info={info}
        turnstile={turnstile}
        onStart={start}
        onResubmit={submit}
        onAbandon={abandon}
      />
    );
  }

  const side = info && (
    <SkillSide
      info={info}
      symbol={symbol}
      scoreName={meta.scoreName}
      extra={
        <SideBox label={t('操作说明')}>
          <RuleList
            items={[
              t('方向键或 WASD 转向，触屏上在场地滑动或用下方的方向键'),
              t('空格或 P 暂停，切到别的页面会自动暂停'),
              t('每吃到一颗豆子得 1 分、身体变长，蛇也越走越快'),
              t('撞到墙或自己的身体就结束'),
            ]}
          />
        </SideBox>
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
      className='snake-page'
    >
      <div className='games-stage'>
        <div className='game-hud'>
          <span>
            {meta.scoreName}
            <strong>{stats.score}</strong>
          </span>
          <span>
            {t('长度')}
            <strong>{stats.length || '-'}</strong>
          </span>
          <span>
            {t('最高分')}
            <strong>{info?.best_score || '-'}</strong>
          </span>
        </div>
        <div
          className={steering ? 'snake-board is-steering' : 'snake-board'}
          onPointerDown={onPointerDown}
          onPointerUp={onPointerUp}
          onPointerCancel={() => {
            swipeRef.current = null;
          }}
        >
          <canvas ref={canvasRef} />
          {overlay}
        </div>
        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>
        <div className='game-controls'>{controls}</div>
      </div>
    </GameLayout>
  );
};

export default Snake;
