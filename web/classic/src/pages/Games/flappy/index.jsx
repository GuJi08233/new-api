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
import { Flag, Pause, Play, RotateCcw, Trophy } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API } from '../../../helpers';
import { getGameMetaMap } from '../../../constants';
import { useActualTheme } from '../../../context/Theme';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { formatNumber, gamePost, withTurnstile } from '../common/api';
import {
  SkillSide,
  rewardsLeft,
  skillResultMessage,
  skillRuleSummary,
} from '../common/skill';
import { useGameInfo } from '../common/useGameInfo';
import { usePlayTurnstile } from '../common/usePlayTurnstile';
import {
  BIRD_HEIGHT,
  BIRD_START_Y,
  BIRD_WIDTH,
  BIRD_X,
  FlapRecorder,
  FlappyGame,
  GROUND_Y,
  HUMAN_FLAP_LIMIT,
  MAX_TICKS,
  PIPE_GAP,
  PIPE_SPEED,
  PIPE_WIDTH,
  SCALE,
  SCREEN_HEIGHT,
  SCREEN_WIDTH,
  TICKS_PER_SECOND,
} from './engine';
import './flappy.css';

const START_URL = '/api/game/flappy/start';
const FINISH_URL = '/api/game/flappy/finish';

const TICK_MS = 1000 / TICKS_PER_SECOND;
// 页面卡顿或切回前台时，一帧最多补推 0.25 秒，多出的时间直接丢弃：游戏内时长只会慢于真实时间，
// 服务端据此拒绝快于真实时间的记录。
const MAX_FRAME_MS = 250;
// 按 KeyboardEvent.code 匹配，不受输入法和键盘布局影响。
const FLAP_KEYS = ['Space', 'ArrowUp', 'KeyW'];
const PAUSE_KEYS = ['KeyP', 'Escape'];
// 管道口比管身宽一点，更像管道。
const CAP_HEIGHT = 18;
const CAP_OVERHANG = 3;
// 远处的云：只是装饰，起点与大小固定。
const CLOUDS = [
  [40, 70, 1],
  [170, 130, 0.8],
  [260, 50, 1.2],
];

// 画布颜色按明暗主题各一套。鸟用固定的明黄色，两套主题下都看得清。
const PALETTES = {
  light: {
    sky: '#e0f2fe',
    cloud: 'rgba(255, 255, 255, 0.9)',
    pipe: '#22c55e',
    cap: '#16a34a',
    shine: 'rgba(255, 255, 255, 0.24)',
    ground: '#eadfbf',
    grass: '#86efac',
    stripe: 'rgba(120, 94, 40, 0.12)',
  },
  dark: {
    sky: '#0f172a',
    cloud: 'rgba(148, 163, 184, 0.12)',
    pipe: '#16a34a',
    cap: '#15803d',
    shine: 'rgba(255, 255, 255, 0.1)',
    ground: '#1e293b',
    grass: '#166534',
    stripe: 'rgba(148, 163, 184, 0.1)',
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

// 一对管道：上下两截管身，缺口一侧各有一个稍宽的管口。坐标换算成像素。
function drawPipe(ctx, pipe, palette) {
  const x = pipe.x / SCALE;
  const top = pipe.gapTop / SCALE;
  const bottom = top + PIPE_GAP;
  ctx.fillStyle = palette.pipe;
  ctx.fillRect(x, 0, PIPE_WIDTH, top - CAP_HEIGHT);
  ctx.fillRect(x, bottom + CAP_HEIGHT, PIPE_WIDTH, GROUND_Y - bottom);
  ctx.fillStyle = palette.shine;
  ctx.fillRect(x + 6, 0, 6, top - CAP_HEIGHT);
  ctx.fillRect(x + 6, bottom + CAP_HEIGHT, 6, GROUND_Y - bottom);
  ctx.fillStyle = palette.cap;
  const capWidth = PIPE_WIDTH + 2 * CAP_OVERHANG;
  ctx.beginPath();
  ctx.roundRect(x - CAP_OVERHANG, top - CAP_HEIGHT, capWidth, CAP_HEIGHT, 4);
  ctx.roundRect(x - CAP_OVERHANG, bottom, capWidth, CAP_HEIGHT, 4);
  ctx.fill();
}

// 鸟画在碰撞盒里：圆身子、翅膀、眼睛和嘴，随速度抬头或低头。
function drawBird(ctx, x, y, angle) {
  ctx.save();
  ctx.translate(x + BIRD_WIDTH / 2, y + BIRD_HEIGHT / 2);
  ctx.rotate(angle);
  ctx.fillStyle = '#fbbf24';
  ctx.strokeStyle = '#b45309';
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.ellipse(0, 0, BIRD_WIDTH / 2 - 1, BIRD_HEIGHT / 2 - 1, 0, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();
  ctx.fillStyle = '#f59e0b';
  ctx.beginPath();
  ctx.ellipse(-5, 2, 8, 5, -0.3, 0, Math.PI * 2);
  ctx.fill();
  ctx.fillStyle = '#ffffff';
  ctx.beginPath();
  ctx.arc(7, -4, 5, 0, Math.PI * 2);
  ctx.fill();
  ctx.fillStyle = '#1f2937';
  ctx.beginPath();
  ctx.arc(8.5, -4, 2.2, 0, Math.PI * 2);
  ctx.fill();
  ctx.fillStyle = '#f97316';
  ctx.beginPath();
  ctx.moveTo(12, 0);
  ctx.lineTo(20, 2.5);
  ctx.lineTo(12, 6);
  ctx.closePath();
  ctx.fill();
  ctx.restore();
}

// 画出一帧：天空、云、管道、地面、鸟与分数。game 为空时画开局前的画面。
// 第一次拍翅膀之前鸟上下浮动，只是动画，不影响引擎里的位置。
function drawScene(canvas, game, now, palette) {
  if (!canvas) return;
  const ctx = prepareCanvas(canvas, SCREEN_WIDTH, SCREEN_HEIGHT);
  const ticks = game?.ticks || 0;
  ctx.fillStyle = palette.sky;
  ctx.fillRect(0, 0, SCREEN_WIDTH, GROUND_Y);

  ctx.fillStyle = palette.cloud;
  for (const [cloudX, cloudY, size] of CLOUDS) {
    const span = SCREEN_WIDTH + 120;
    const x = ((((cloudX - ticks * 0.3) % span) + span) % span) - 60;
    ctx.beginPath();
    ctx.arc(x, cloudY, 16 * size, 0, Math.PI * 2);
    ctx.arc(x + 18 * size, cloudY - 8 * size, 20 * size, 0, Math.PI * 2);
    ctx.arc(x + 40 * size, cloudY, 15 * size, 0, Math.PI * 2);
    ctx.fill();
  }

  for (const pipe of game?.pipes || []) drawPipe(ctx, pipe, palette);

  ctx.fillStyle = palette.ground;
  ctx.fillRect(0, GROUND_Y, SCREEN_WIDTH, SCREEN_HEIGHT - GROUND_Y);
  ctx.fillStyle = palette.stripe;
  const offset = (ticks * PIPE_SPEED) % 24;
  for (let x = -offset - 24; x < SCREEN_WIDTH + 24; x += 24) {
    ctx.beginPath();
    ctx.moveTo(x, GROUND_Y + 10);
    ctx.lineTo(x + 12, GROUND_Y + 10);
    ctx.lineTo(x + 2, SCREEN_HEIGHT);
    ctx.lineTo(x - 10, SCREEN_HEIGHT);
    ctx.closePath();
    ctx.fill();
  }
  ctx.fillStyle = palette.grass;
  ctx.fillRect(0, GROUND_Y, SCREEN_WIDTH, 10);

  if (!game || !game.started) {
    const hover = game ? Math.sin(now / 180) * 4 : 0;
    drawBird(ctx, BIRD_X, BIRD_START_Y + hover, 0);
    return;
  }
  const angle = Math.max(-0.45, Math.min(1.2, game.vy / 700));
  drawBird(ctx, BIRD_X, game.y / SCALE, angle);
  ctx.font = '700 40px system-ui, -apple-system, sans-serif';
  ctx.textAlign = 'center';
  ctx.lineJoin = 'round';
  ctx.lineWidth = 5;
  ctx.strokeStyle = 'rgba(15, 23, 42, 0.5)';
  ctx.strokeText(String(game.score), SCREEN_WIDTH / 2, 64);
  ctx.fillStyle = '#ffffff';
  ctx.fillText(String(game.score), SCREEN_WIDTH / 2, 64);
}

// 结算请求：服务端从开局的种子重放推进过的帧数与每次拍翅膀的帧号，成绩以重放为准。
function finishPayload(run) {
  return {
    session_id: run.sessionId,
    ticks: run.game.ticks,
    flaps: run.recorder.flaps,
  };
}

const Flappy = () => {
  const { t } = useTranslation();
  const meta = getGameMetaMap(t).flappy;
  const { loading, unavailable, info, patch, reload } = useGameInfo('flappy');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const palette = PALETTES[useActualTheme() === 'dark' ? 'dark' : 'light'];
  // idle 还没开局，starting 开局中，ready 鸟悬停着等第一次拍翅膀，playing 对局中，paused 暂停，
  // submitting 结算中，result 已结算(或被拒绝)，error 结算没有得到答复。
  const [phase, setPhase] = useState('idle');
  const [result, setResult] = useState(null);
  const [notice, setNotice] = useState(null);
  const canvasRef = useRef(null);
  // 当前一局：会话、引擎、拍翅膀记录、下一帧要不要拍与还没用完的时间。渲染循环直接读写它，不经过 React 状态。
  const runRef = useRef(null);
  const paletteRef = useRef(palette);
  // 确认框打开期间不响应游戏按键，免得在确认框后面继续游戏。
  const confirmingRef = useRef(false);
  const mountedRef = useRef(false);
  const symbol = info?.symbol || '';
  const running = phase === 'ready' || phase === 'playing';
  const active = running || phase === 'paused';

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

  // 拍一下翅膀，在下一帧生效；一帧里拍几下都只算一下。第一次拍翅膀让鸟起飞。
  const flap = useCallback(() => {
    const run = runRef.current;
    if (!run || run.game.over) return;
    run.flap = true;
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
    runRef.current = {
      sessionId: response.data.session_id,
      game: new FlappyGame(response.data.seed),
      recorder: new FlapRecorder(),
      flap: false,
      elapsed: 0,
      previousBest: info?.best_score || 0,
    };
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

  // 渲染循环：固定每秒 60 帧，按真实时间推进；开局前也在转，画出鸟的悬停。撞上了或到了时长上限就提交结算。
  useEffect(() => {
    if (!running) return undefined;
    const run = runRef.current;
    let frame = 0;
    let last = performance.now();
    const loop = (now) => {
      run.elapsed += Math.min(now - last, MAX_FRAME_MS);
      last = now;
      const { game } = run;
      while (run.elapsed >= TICK_MS && !game.over && game.ticks < MAX_TICKS) {
        run.elapsed -= TICK_MS;
        const flapped = run.flap;
        run.flap = false;
        if (flapped) run.recorder.record(game.ticks);
        game.tick(flapped);
      }
      drawScene(canvasRef.current, game, now, paletteRef.current);
      if (game.over || game.ticks >= MAX_TICKS) {
        submit();
        return;
      }
      frame = requestAnimationFrame(loop);
    };
    frame = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(frame);
  }, [running, submit]);

  // 不在推进时画一次当前的画面，窗口大小或主题变了再画一次。
  useEffect(() => {
    if (loading || running) return undefined;
    const draw = () =>
      drawScene(canvasRef.current, runRef.current?.game, 0, palette);
    draw();
    window.addEventListener('resize', draw);
    return () => window.removeEventListener('resize', draw);
  }, [loading, running, phase, palette]);

  // 按键：空格、↑ 或 W 拍翅膀，P 与 Esc 暂停、P 继续，对局中这些键都不滚动页面，按住不放的自动重复不算。
  // 撞上之后玩家常常还在连按空格，焦点不在控件上时也不让它们把页面滚走。
  useEffect(() => {
    if (phase === 'idle') return undefined;
    const onKeyDown = (event) => {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      const flapping = FLAP_KEYS.includes(event.code);
      if (!flapping && !PAUSE_KEYS.includes(event.code)) return;
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
      if (event.repeat || confirmingRef.current) return;
      if (flapping) {
        if (running) flap();
        return;
      }
      if (event.code === 'KeyP' && phase === 'paused') {
        setPhase('playing');
        return;
      }
      pause();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [phase, active, running, flap, pause]);

  // 对局中切到别的窗口或标签页时自动暂停；刷新或关闭页面时这局来不及结算，先让浏览器确认一次。
  useEffect(() => {
    if (!active) return undefined;
    const onVisibilityChange = () => {
      if (document.hidden) pause();
    };
    const onBeforeUnload = (event) => {
      if (!runRef.current?.game.ticks) return;
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
      if (!run || run.submitting || run.submitted || run.game.ticks === 0) {
        return;
      }
      API.post(FINISH_URL, finishPayload(run), {
        skipErrorHandler: true,
      }).catch(() => {});
    };
  }, []);

  const left = rewardsLeft(info?.rule, info?.progress);
  let leftText = t('今天还能领 {{left}} 次', { left });
  if (info?.rule?.daily_rewards === 0) {
    leftText = t('这个游戏暂不发放奖励');
  } else if (left === 0) {
    leftText = t('今天的领奖次数已用完，还可以继续玩');
  }

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
        text: t('点一下扑一次翅膀，穿过管道之间的缝隙'),
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
      <div className='flappy-overlay is-hint'>
        <div className='flappy-overlay-title'>{t('准备起飞')}</div>
        <div className='flappy-overlay-text'>
          {t('点击画面或按空格拍一下翅膀，计时从这一下开始')}
        </div>
      </div>
    );
  } else if (phase === 'paused') {
    overlay = (
      <div className='flappy-overlay'>
        <div className='flappy-overlay-title'>{t('已暂停')}</div>
      </div>
    );
  } else if (phase === 'submitting') {
    overlay = (
      <div className='flappy-overlay'>
        <Spin />
        <div className='flappy-overlay-text'>{t('正在结算…')}</div>
      </div>
    );
  } else if (phase === 'error') {
    overlay = (
      <div className='flappy-overlay'>
        <div className='flappy-overlay-title'>{t('结算未完成')}</div>
      </div>
    );
  } else if (phase === 'result' && result.rejected) {
    overlay = (
      <div className='flappy-overlay'>
        <div className='flappy-overlay-title'>{t('本局未能结算')}</div>
      </div>
    );
  } else if (phase === 'result') {
    overlay = (
      <div className='flappy-overlay is-result'>
        <div className='flappy-overlay-title'>
          {result.over ? t('游戏结束') : t('本局结束')}
        </div>
        {result.newRecord && (
          <div className='flappy-record'>
            <Trophy size={14} />
            {t('新纪录')}
          </div>
        )}
        <div className='flappy-overlay-score'>{formatNumber(result.score)}</div>
        <div className='flappy-overlay-text'>{meta.scoreName}</div>
        {result.score !== result.localScore && (
          <div className='flappy-overlay-text'>
            {t('成绩以服务器重放结果为准')}
          </div>
        )}
      </div>
    );
  }

  let startLabel = t('开始游戏');
  if (phase === 'starting') {
    startLabel = t('正在开局…');
  } else if (phase === 'result') {
    startLabel = t('再来一局');
  }

  let controls = null;
  if (running) {
    controls = (
      <Button
        icon={<Pause size={14} />}
        disabled={phase === 'ready'}
        onClick={pause}
      >
        {t('暂停')}
      </Button>
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
  } else if (phase === 'error') {
    controls = (
      <>
        <Button theme='solid' icon={<RotateCcw size={14} />} onClick={submit}>
          {t('重新提交')}
        </Button>
        <Button onClick={abandon}>{t('放弃本局')}</Button>
      </>
    );
  } else if (phase !== 'submitting') {
    controls = (
      <>
        <button
          type='button'
          className='games-play-button is-free'
          disabled={phase === 'starting' || !turnstile.ready}
          onClick={start}
        >
          {startLabel}
        </button>
        {turnstile.widget}
        {turnstile.failed && (
          <div className='games-stage-hint'>
            {t('人机验证加载失败，请刷新页面重试')}
          </div>
        )}
        {!turnstile.ready && !turnstile.failed && (
          <div className='games-stage-hint'>
            {t('开局前需要完成人机验证，需要操作时验证框会出现在页面下方')}
          </div>
        )}
        <div className='games-stage-counter'>{leftText}</div>
      </>
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
              t('空格、↑ 或点击画面拍一下翅膀，第一次拍翅膀开始计时'),
              t('穿过一道管道得 1 分，碰到管道、地面或飞出顶部就结束'),
              t('P 或 Esc 暂停，切到别的页面会自动暂停'),
              t('1 秒内拍翅膀超过 {{limit}} 次会被判定为脚本，不发奖励', {
                limit: HUMAN_FLAP_LIMIT,
              }),
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
      className='flappy-page'
    >
      <div className='games-stage'>
        <div
          className={running ? 'flappy-board is-running' : 'flappy-board'}
          onPointerDown={(event) => {
            if (!running) return;
            event.preventDefault();
            flap();
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

export default Flappy;
