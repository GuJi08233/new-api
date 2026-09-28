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

import React, {
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react';
import { UNSAFE_NavigationContext, useNavigate } from 'react-router-dom';
import { Button, Empty, Modal, Spin } from '@douyinfe/semi-ui';
import {
  IllustrationConstruction,
  IllustrationConstructionDark,
} from '@douyinfe/semi-illustrations';
import {
  Archive,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ChevronsDown,
  Flag,
  Pause,
  Play,
  RotateCcw,
  RotateCw,
  Trophy,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import Turnstile from 'react-turnstile';
import { API, showError } from '../../../helpers';
import {
  ACTION,
  HEIGHT,
  InputRecorder,
  MAX_TICKS,
  PIECE_CELLS,
  PREVIEW_SIZE,
  TICKS_PER_SECOND,
  TetrisGame,
  WIDTH,
} from './engine';
import {
  playTimeNote,
  rewardsLeft,
  skillResultMessage,
  skillRuleSummary,
} from '../common/skill';
import '../games.css';
import './tetris.css';

const TICK_MS = 1000 / TICKS_PER_SECOND;
// 页面卡顿或切回前台时，一帧最多补推 0.25 秒，多出的时间直接丢弃：游戏内时长只会慢于真实时间，
// 服务端据此拒绝快于真实时间的记录。
const MAX_FRAME_MS = 250;
// 按住左右键 10 帧后开始连续移动，之后每 2 帧一格；按住下键每 2 帧软降一格。
const DAS_TICKS = 10;
const ARR_TICKS = 2;
const SOFT_DROP_TICKS = 2;

const CELL = 30;
const PREVIEW_CELL = 16;
const PREVIEW_SLOT_HEIGHT = PREVIEW_CELL * 3;
const PREVIEW_WIDTH = PREVIEW_CELL * 5;
// 依次为 I O T S Z J L，沿用各方块约定俗成的色相，取游戏中心共用的配色。
const PIECE_COLORS = [
  '#06b6d4',
  '#f59e0b',
  '#8b5cf6',
  '#10b981',
  '#f43f5e',
  '#3b82f6',
  '#f97316',
];

// 按 KeyboardEvent.code 匹配，不受输入法和键盘布局影响。
const KEY_BINDINGS = {
  ArrowLeft: 'left',
  ArrowRight: 'right',
  ArrowDown: 'down',
  ArrowUp: 'rotateCW',
  KeyX: 'rotateCW',
  KeyZ: 'rotateCCW',
  ControlLeft: 'rotateCCW',
  ControlRight: 'rotateCCW',
  Space: 'hardDrop',
  KeyC: 'hold',
  ShiftLeft: 'hold',
  ShiftRight: 'hold',
};

const PRESS_ACTIONS = {
  rotateCW: ACTION.ROTATE_CW,
  rotateCCW: ACTION.ROTATE_CCW,
  hardDrop: ACTION.HARD_DROP,
  hold: ACTION.HOLD,
};

const ILLUSTRATION_SIZE = { width: 150, height: 150 };

// 按下一个控制键：单次操作排到下一帧执行；左右和下键同时开始计算按住的帧数，由 advanceTick 自动重复。
function pressControl(run, control) {
  if (control === 'left' || control === 'right') {
    if (run.held[control]) return;
    run.held[control] = true;
    const dir = control === 'left' ? -1 : 1;
    run.shift = { dir, ticks: 0 };
    run.pending.push(dir < 0 ? ACTION.LEFT : ACTION.RIGHT);
    return;
  }
  if (control === 'down') {
    if (run.softDrop.held) return;
    run.softDrop = { held: true, ticks: 0 };
    run.pending.push(ACTION.SOFT_DROP);
    return;
  }
  run.pending.push(PRESS_ACTIONS[control]);
}

// 松开一个控制键。松开当前方向时另一个方向仍按着，就改为向那一边移动。
function releaseControl(run, control) {
  if (control === 'down') {
    run.softDrop = { held: false, ticks: 0 };
    return;
  }
  if (control !== 'left' && control !== 'right') return;
  run.held[control] = false;
  const dir = control === 'left' ? -1 : 1;
  if (run.shift.dir !== dir) return;
  const other = control === 'left' ? 'right' : 'left';
  if (!run.held[other]) {
    run.shift = { dir: 0, ticks: 0 };
    return;
  }
  run.shift = { dir: -dir, ticks: 0 };
  run.pending.push(-dir < 0 ? ACTION.LEFT : ACTION.RIGHT);
}

function releaseAllControls(run) {
  run.pending = [];
  run.held = { left: false, right: false };
  run.shift = { dir: 0, ticks: 0 };
  run.softDrop = { held: false, ticks: 0 };
}

// 推进一帧：先执行这一帧新按下的操作，再加上按住键的自动重复，全部写进上传给服务端的操作记录。
function advanceTick(run) {
  const actions = run.pending.splice(0);
  const { shift, softDrop } = run;
  if (shift.dir !== 0) {
    shift.ticks++;
    if (
      shift.ticks >= DAS_TICKS &&
      (shift.ticks - DAS_TICKS) % ARR_TICKS === 0
    ) {
      actions.push(shift.dir < 0 ? ACTION.LEFT : ACTION.RIGHT);
    }
  }
  if (softDrop.held) {
    softDrop.ticks++;
    if (softDrop.ticks % SOFT_DROP_TICKS === 0) {
      actions.push(ACTION.SOFT_DROP);
    }
  }
  for (const action of actions) {
    run.recorder.record(run.game.ticks, action);
  }
  run.game.tick(actions);
}

// 按设备像素比设置画布分辨率并清空，返回按 CSS 像素绘制的上下文。
function prepareCanvas(canvas, width, height) {
  const ratio = window.devicePixelRatio || 1;
  if (canvas.width !== width * ratio || canvas.height !== height * ratio) {
    canvas.width = width * ratio;
    canvas.height = height * ratio;
  }
  const ctx = canvas.getContext('2d');
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  ctx.clearRect(0, 0, width, height);
  return ctx;
}

// 方块画成圆角小块，顶部一道高光，与游戏中心其他游戏的图标块一致。
function drawCell(ctx, x, y, size, color, alpha) {
  const radius = size * 0.2;
  ctx.globalAlpha = alpha;
  ctx.fillStyle = color;
  ctx.beginPath();
  ctx.roundRect(x + 1, y + 1, size - 2, size - 2, radius);
  ctx.fill();
  ctx.fillStyle = 'rgba(255, 255, 255, 0.22)';
  ctx.beginPath();
  ctx.roundRect(x + 1, y + 1, size - 2, Math.max(2, Math.round(size * 0.2)), [
    radius,
    radius,
    0,
    0,
  ]);
  ctx.fill();
  ctx.globalAlpha = 1;
}

// 画出盘面、落点虚影和当前方块；game 为空时只画网格。
function drawBoard(canvas, game) {
  const width = WIDTH * CELL;
  const height = HEIGHT * CELL;
  const ctx = prepareCanvas(canvas, width, height);
  ctx.strokeStyle = 'rgba(127, 127, 127, 0.16)';
  ctx.lineWidth = 1;
  ctx.beginPath();
  for (let col = 1; col < WIDTH; col++) {
    ctx.moveTo(col * CELL + 0.5, 0);
    ctx.lineTo(col * CELL + 0.5, height);
  }
  for (let row = 1; row < HEIGHT; row++) {
    ctx.moveTo(0, row * CELL + 0.5);
    ctx.lineTo(width, row * CELL + 0.5);
  }
  ctx.stroke();
  if (!game) return;

  game.board.forEach((line, row) => {
    line.forEach((cell, col) => {
      if (cell !== 0) {
        drawCell(ctx, col * CELL, row * CELL, CELL, PIECE_COLORS[cell - 1], 1);
      }
    });
  });
  if (game.over) return;
  const color = PIECE_COLORS[game.piece];
  ctx.strokeStyle = color;
  ctx.fillStyle = color;
  for (const [x, y] of game.cells(game.ghostY())) {
    ctx.beginPath();
    ctx.roundRect(
      x * CELL + 1.5,
      y * CELL + 1.5,
      CELL - 3,
      CELL - 3,
      CELL * 0.2,
    );
    ctx.globalAlpha = 0.18;
    ctx.fill();
    ctx.globalAlpha = 0.7;
    ctx.stroke();
  }
  ctx.globalAlpha = 1;
  for (const [x, y] of game.cells()) {
    drawCell(ctx, x * CELL, y * CELL, CELL, color, 1);
  }
}

// 在竖排的格位里逐个居中画出方块，暂存区与预览队列共用；负数表示空位。
function drawPreview(canvas, pieces, slots, dimmed) {
  const ctx = prepareCanvas(canvas, PREVIEW_WIDTH, PREVIEW_SLOT_HEIGHT * slots);
  pieces.forEach((piece, index) => {
    if (piece < 0) return;
    const cells = PIECE_CELLS[piece][0];
    const xs = cells.map(([x]) => x);
    const ys = cells.map(([, y]) => y);
    const minX = Math.min(...xs);
    const minY = Math.min(...ys);
    const spanX = Math.max(...xs) - minX + 1;
    const spanY = Math.max(...ys) - minY + 1;
    const offsetX = (PREVIEW_WIDTH - spanX * PREVIEW_CELL) / 2;
    const offsetY =
      index * PREVIEW_SLOT_HEIGHT +
      (PREVIEW_SLOT_HEIGHT - spanY * PREVIEW_CELL) / 2;
    for (const [x, y] of cells) {
      drawCell(
        ctx,
        offsetX + (x - minX) * PREVIEW_CELL,
        offsetY + (y - minY) * PREVIEW_CELL,
        PREVIEW_CELL,
        PIECE_COLORS[piece],
        dimmed ? 0.35 : 1,
      );
    }
  });
}

const formatNumber = (value) => Number(value || 0).toLocaleString();

const Tetris = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { navigator } = useContext(UNSAFE_NavigationContext);
  const boardRef = useRef(null);
  const holdRef = useRef(null);
  const nextRef = useRef(null);
  // 当前一局的全部可变状态：引擎、操作记录与按键状态。渲染循环直接读写它，不经过 React 状态。
  const runRef = useRef(null);
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState('');
  const [rule, setRule] = useState(null);
  const [symbol, setSymbol] = useState('');
  const [phase, setPhase] = useState('idle');
  const [stats, setStats] = useState({ score: 0, lines: 0, level: 1 });
  const [result, setResult] = useState(null);
  const [submitError, setSubmitError] = useState('');
  // 开局人机验证：每局用一个新令牌，开局请求发出后换一个组件实例重新取令牌。
  const [playTurnstile, setPlayTurnstile] = useState({ required: false });
  const [turnstileToken, setTurnstileToken] = useState('');
  const [turnstileKey, setTurnstileKey] = useState(0);
  const [turnstileFailed, setTurnstileFailed] = useState(false);
  // 结束、离开的确认框打开期间忽略游戏按键，免得在确认框后面按 P 继续玩。
  const confirmingRef = useRef(false);

  useEffect(() => {
    const load = async () => {
      try {
        const res = await API.get('/api/game/tetris');
        if (!res.data.success) {
          setUnavailable(res.data.message);
          return;
        }
        // rule 是游戏页信息：领奖规则 rule、今日进度 progress、最高分 best_score 等。
        setRule(res.data.data);
        setSymbol(res.data.data.symbol || '');
        setPlayTurnstile(res.data.data.play_turnstile || { required: false });
      } catch (error) {
        // 被风控封禁的 IP 收到 403，显示服务端给出的原因。
        setUnavailable(
          error?.response?.data?.message || t('加载失败，请刷新重试'),
        );
      } finally {
        setLoading(false);
      }
    };
    load();
  }, [t]);

  // 还没开局或放弃本局后，画出空盘面。
  useEffect(() => {
    if (phase !== 'idle' || !boardRef.current) return;
    drawBoard(boardRef.current, null);
    drawPreview(holdRef.current, [], 1, false);
    drawPreview(nextRef.current, [], PREVIEW_SIZE, false);
  }, [loading, rule, phase]);

  const submitResult = useCallback(async () => {
    const run = runRef.current;
    // 同一局只提交一次；网络出错时放开，让玩家重新提交。
    if (run.submitting) return;
    run.submitting = true;
    releaseAllControls(run);
    setPhase('submitting');
    setSubmitError('');
    try {
      const res = await API.post(
        '/api/game/tetris/finish',
        {
          session_id: run.sessionId,
          ticks: run.game.ticks,
          inputs: run.recorder.inputs,
        },
        { skipErrorHandler: true },
      );
      if (!res.data.success) {
        // 服务端明确拒绝(对局过期、记录无效等)，重新提交也不会成功。
        setResult({ rejected: res.data.message });
        setPhase('result');
        return;
      }
      const data = res.data.data;
      setResult({
        ...data,
        lines: data.detail?.lines || 0,
        level: data.detail?.level || 1,
        over: run.game.over,
        localScore: run.game.score,
        newRecord: data.score > 0 && data.score > run.previousBest,
      });
      setRule((previous) => ({
        ...previous,
        rule: data.rule,
        progress: data.progress,
        free_daily_limit: data.free_daily_limit,
        best_score: data.best_score,
      }));
      setPhase('result');
    } catch (error) {
      const status = error?.response?.status;
      // 登录已过期：交给全局处理，跳转到登录页。
      if (status === 401) {
        showError(error);
        return;
      }
      // 服务端给出了拒绝原因(例如 IP 已被风控封禁)，重新提交也不会成功。
      const reason = error?.response?.data?.message;
      if (status !== 429 && status < 500 && reason) {
        setResult({ rejected: reason });
        setPhase('result');
        return;
      }
      // 网络错误、限流或服务端出错：整局记录还在，可以重新提交；已经结算过的话，服务端会返回第一次的结果。
      run.submitting = false;
      setSubmitError(
        status === 429
          ? t('请求过于频繁，请稍后重新提交')
          : t('网络异常，结算没有完成，可以重新提交'),
      );
      setPhase('error');
    }
  }, [t]);

  const startGame = async () => {
    const token = turnstileToken;
    if (playTurnstile.required && !token) return;
    setPhase('starting');
    // 令牌只能用一次，无论开局成败都换一个新的。
    setTurnstileToken('');
    setTurnstileKey((key) => key + 1);
    try {
      const res = await API.post(
        playTurnstile.required
          ? `/api/game/tetris/start?turnstile=${encodeURIComponent(token)}`
          : '/api/game/tetris/start',
      );
      if (!res.data.success) {
        showError(res.data.message);
        setPhase('idle');
        return;
      }
      const { session_id: sessionId, seed } = res.data.data;
      runRef.current = {
        sessionId,
        game: new TetrisGame(seed),
        recorder: new InputRecorder(),
        pending: [],
        held: { left: false, right: false },
        shift: { dir: 0, ticks: 0 },
        softDrop: { held: false, ticks: 0 },
        previousBest: rule?.best_score || 0,
      };
      setStats({ score: 0, lines: 0, level: 1 });
      setResult(null);
      // 开始按钮保留焦点时，空格键会再次触发它。
      document.activeElement?.blur?.();
      setPhase('playing');
    } catch (error) {
      setPhase('idle');
    }
  };

  const pauseGame = useCallback(() => {
    if (runRef.current) releaseAllControls(runRef.current);
    setPhase((current) => (current === 'playing' ? 'paused' : current));
  }, []);

  // 暂停后弹出确认框，确认框关闭前游戏按键不起作用。
  const confirmWhilePaused = useCallback(
    (options) => {
      pauseGame();
      confirmingRef.current = true;
      Modal.confirm({
        ...options,
        afterClose: () => {
          confirmingRef.current = false;
        },
      });
    },
    [pauseGame],
  );

  const endGame = () => {
    confirmWhilePaused({
      title: t('结束本局'),
      content: t('结束后按当前成绩结算，不能继续。'),
      onOk: () => {
        submitResult();
      },
    });
  };

  // 对局进行中离开本页，这一局就不会结算：返回按钮、顶栏和侧栏的站内链接、浏览器的后退与前进都先确认一次。
  // 站点用的是 BrowserRouter，没有 useBlocker，对局期间由这里接管路由的跳转；浏览器后退、前进时地址已经变了，
  // 先把本页压回去，确认离开再退到玩家要去的那一页。刷新和关闭页面由 beforeunload 确认。
  const inProgress = phase === 'playing' || phase === 'paused';
  useEffect(() => {
    if (!inProgress) return undefined;
    const { push, replace, go } = navigator;
    const here = `${window.location.pathname}${window.location.search}${window.location.hash}`;
    let leaving = false;
    const confirmLeave = (leave) => {
      if (confirmingRef.current) return;
      confirmWhilePaused({
        title: t('离开游戏'),
        content: t('本局尚未结算，离开后成绩作废。'),
        onOk: () => {
          leaving = true;
          leave();
        },
      });
    };
    navigator.push = (...args) => confirmLeave(() => push(...args));
    navigator.replace = (...args) => confirmLeave(() => replace(...args));
    navigator.go = (delta) => confirmLeave(() => go(delta));
    const onPopState = () => {
      if (leaving) return;
      push(here);
      confirmLeave(() => go(-1));
    };
    window.addEventListener('popstate', onPopState);
    return () => {
      navigator.push = push;
      navigator.replace = replace;
      navigator.go = go;
      window.removeEventListener('popstate', onPopState);
    };
  }, [inProgress, navigator, confirmWhilePaused, t]);

  // 渲染循环：按真实时间以每秒 60 帧推进，游戏结束或到达时长上限时提交结算。
  useEffect(() => {
    if (phase !== 'playing') return undefined;
    const run = runRef.current;
    let frame = 0;
    let last = performance.now();
    let elapsed = 0;
    const loop = (now) => {
      elapsed += Math.min(now - last, MAX_FRAME_MS);
      last = now;
      const { game } = run;
      while (elapsed >= TICK_MS && !game.over && game.ticks < MAX_TICKS) {
        advanceTick(run);
        elapsed -= TICK_MS;
      }
      drawBoard(boardRef.current, game);
      drawPreview(holdRef.current, [game.hold], 1, game.holdUsed);
      drawPreview(nextRef.current, game.preview(), PREVIEW_SIZE, false);
      setStats((previous) =>
        previous.score === game.score &&
        previous.lines === game.lines &&
        previous.level === game.level
          ? previous
          : { score: game.score, lines: game.lines, level: game.level },
      );
      if (game.over || game.ticks >= MAX_TICKS) {
        submitResult();
        return;
      }
      frame = requestAnimationFrame(loop);
    };
    frame = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(frame);
  }, [phase, submitResult]);

  useEffect(() => {
    if (phase !== 'playing' && phase !== 'paused') return undefined;
    const onKeyDown = (event) => {
      if (confirmingRef.current) return;
      if (event.code === 'KeyP' && !event.repeat) {
        event.preventDefault();
        setPhase((current) => (current === 'playing' ? 'paused' : 'playing'));
        releaseAllControls(runRef.current);
        return;
      }
      if (event.code === 'Escape') {
        pauseGame();
        return;
      }
      const control = KEY_BINDINGS[event.code];
      if (!control) return;
      event.preventDefault();
      if (event.repeat || phase !== 'playing') return;
      pressControl(runRef.current, control);
    };
    const onKeyUp = (event) => {
      const control = KEY_BINDINGS[event.code];
      if (!control) return;
      event.preventDefault();
      releaseControl(runRef.current, control);
    };
    // 切到别的窗口或标签页时自动暂停，暂停期间盘面被遮住。
    const onVisibilityChange = () => {
      if (document.hidden) pauseGame();
    };
    // 游戏进行中关闭或刷新页面，本局就不会结算，先让浏览器确认一次。
    const onBeforeUnload = (event) => {
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('keyup', onKeyUp);
    window.addEventListener('blur', pauseGame);
    window.addEventListener('beforeunload', onBeforeUnload);
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('keyup', onKeyUp);
      window.removeEventListener('blur', pauseGame);
      window.removeEventListener('beforeunload', onBeforeUnload);
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [phase, pauseGame]);

  // 一局刚结束时玩家常常还在按方向键和空格，这些键不能把页面滚走；焦点在按钮上时保留按钮自己的行为。
  useEffect(() => {
    if (phase === 'idle' || phase === 'playing' || phase === 'paused') {
      return undefined;
    }
    const onKeyDown = (event) => {
      if (KEY_BINDINGS[event.code] && event.target === document.body) {
        event.preventDefault();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [phase]);

  if (loading || unavailable) {
    return (
      <div className='games-page'>
        <div className='games-container tetris-page'>
          {loading ? (
            <Spin spinning size='large' />
          ) : (
            <Empty
              image={<IllustrationConstruction style={ILLUSTRATION_SIZE} />}
              darkModeImage={
                <IllustrationConstructionDark style={ILLUSTRATION_SIZE} />
              }
              title={unavailable}
            >
              <Button onClick={() => navigate('/games')}>
                {t('返回游戏中心')}
              </Button>
            </Empty>
          )}
        </div>
      </div>
    );
  }

  const ruleText = skillRuleSummary(rule, t, symbol, t('分数'));
  const left = rewardsLeft(rule.rule, rule.progress);
  let leftText = t('今天还能领 {{left}} 次', { left });
  if (rule.rule?.daily_rewards === 0) {
    leftText = t('这个游戏暂不发放奖励');
  } else if (left === 0) {
    leftText = t('今天的领奖次数已用完，还可以继续玩');
  }
  const timeNote = playTimeNote(rule.rule, rule.progress, t);
  const rewardMessage =
    result && !result.rejected ? skillResultMessage(result, t, symbol) : null;

  const waitingTurnstile = playTurnstile.required && !turnstileToken;
  let turnstileHint = '';
  if (playTurnstile.required && turnstileFailed) {
    turnstileHint = t('人机验证加载失败，请刷新页面重试');
  } else if (waitingTurnstile) {
    turnstileHint = t(
      '开局前需要完成人机验证，需要操作时验证框会出现在页面下方',
    );
  }

  let overlay = null;
  if (phase === 'idle') {
    overlay = (
      <div className='tetris-overlay'>
        <div className='tetris-overlay-title'>{t('俄罗斯方块')}</div>
        <p className='tetris-overlay-text'>{ruleText}</p>
        <Button
          theme='solid'
          size='large'
          icon={<Play size={16} />}
          disabled={waitingTurnstile}
          onClick={startGame}
        >
          {t('开始游戏')}
        </Button>
        {turnstileHint && (
          <div className='tetris-overlay-note'>{turnstileHint}</div>
        )}
      </div>
    );
  } else if (phase === 'starting' || phase === 'submitting') {
    overlay = (
      <div className='tetris-overlay'>
        <Spin size='large' />
        <p className='tetris-overlay-text'>
          {phase === 'starting' ? t('正在开局…') : t('正在结算…')}
        </p>
      </div>
    );
  } else if (phase === 'paused') {
    overlay = (
      <div className='tetris-overlay tetris-overlay-solid'>
        <div className='tetris-overlay-title'>{t('已暂停')}</div>
        <Button
          theme='solid'
          icon={<Play size={16} />}
          onClick={() => setPhase('playing')}
        >
          {t('继续游戏')}
        </Button>
        <Button icon={<Flag size={16} />} onClick={endGame}>
          {t('结束本局')}
        </Button>
      </div>
    );
  } else if (phase === 'error') {
    overlay = (
      <div className='tetris-overlay'>
        <div className='tetris-overlay-title'>{t('结算未完成')}</div>
        <p className='tetris-overlay-text'>{submitError}</p>
        <Button theme='solid' onClick={submitResult}>
          {t('重新提交')}
        </Button>
        <Button onClick={() => setPhase('idle')}>{t('放弃本局')}</Button>
      </div>
    );
  } else if (phase === 'result') {
    overlay = (
      <div className='tetris-overlay'>
        {result.rejected ? (
          <>
            <div className='tetris-overlay-title'>{t('本局未能结算')}</div>
            <p className='tetris-overlay-text'>{result.rejected}</p>
          </>
        ) : (
          <>
            <div className='tetris-overlay-title'>
              {result.over ? t('游戏结束') : t('本局结束')}
            </div>
            {result.newRecord && (
              <div className='tetris-record'>
                <Trophy size={14} />
                {t('新纪录')}
              </div>
            )}
            <div className='tetris-result-score'>
              {formatNumber(result.score)}
            </div>
            <div className='tetris-overlay-text'>
              {t('消除 {{lines}} 行 · 等级 {{level}}', {
                lines: result.lines,
                level: result.level,
              })}
            </div>
            <div
              className={
                result.reward > 0
                  ? 'tetris-result-reward is-earned'
                  : 'tetris-result-reward'
              }
            >
              {rewardMessage.text}
            </div>
            {rewardMessage.note && (
              <div className='tetris-overlay-note'>{rewardMessage.note}</div>
            )}
            {result.score !== result.localScore && (
              <div className='tetris-overlay-note'>
                {t('成绩以服务器重放结果为准')}
              </div>
            )}
          </>
        )}
        <Button
          theme='solid'
          size='large'
          icon={<Play size={16} />}
          disabled={waitingTurnstile}
          onClick={startGame}
        >
          {t('再来一局')}
        </Button>
        {turnstileHint && (
          <div className='tetris-overlay-note'>{turnstileHint}</div>
        )}
      </div>
    );
  }

  const touchControls = [
    ['left', <ArrowLeft size={22} />, t('左移')],
    ['down', <ArrowDown size={22} />, t('软降')],
    ['right', <ArrowRight size={22} />, t('右移')],
    ['hardDrop', <ChevronsDown size={22} />, t('硬降')],
    ['rotateCCW', <RotateCcw size={22} />, t('逆时针旋转')],
    ['rotateCW', <RotateCw size={22} />, t('顺时针旋转')],
    ['hold', <Archive size={22} />, t('暂存')],
  ];

  return (
    <div className='games-page'>
      <div className='games-glow' aria-hidden />
      <div className='games-container tetris-page'>
        <div className='tetris-topbar'>
          <Button
            theme='borderless'
            icon={<ArrowLeft size={16} />}
            onClick={() => navigate('/games')}
          >
            {t('游戏中心')}
          </Button>
          <h1 className='tetris-title'>{t('俄罗斯方块')}</h1>
        </div>

        <div className='tetris-layout'>
          <div className='tetris-panel tetris-left'>
            <div className='tetris-box'>
              <div className='tetris-box-label'>{t('暂存')}</div>
              <canvas ref={holdRef} className='tetris-preview' />
            </div>
            <div className='tetris-box tetris-stats'>
              <div>
                <span>{t('分数')}</span>
                <strong>{formatNumber(stats.score)}</strong>
              </div>
              <div>
                <span>{t('消除行数')}</span>
                <strong>{stats.lines}</strong>
              </div>
              <div>
                <span>{t('等级')}</span>
                <strong>{stats.level}</strong>
              </div>
              <div>
                <span>{t('最高分')}</span>
                <strong>{formatNumber(rule.best_score)}</strong>
              </div>
            </div>
          </div>

          <div className='tetris-board'>
            <canvas ref={boardRef} />
            {overlay}
          </div>

          <div className='tetris-panel tetris-right'>
            <div className='tetris-box'>
              <div className='tetris-box-label'>{t('下一个')}</div>
              <canvas ref={nextRef} className='tetris-preview' />
            </div>
            <div className='tetris-box'>
              <div className='tetris-box-label'>{t('今日奖励')}</div>
              <div className='tetris-reward-value'>
                {rule.progress?.earned_today || 0}
                <span>{symbol}</span>
              </div>
              <div className='tetris-reward-rule'>{leftText}</div>
              {timeNote && <div className='tetris-reward-rule'>{timeNote}</div>}
              <div className='tetris-reward-rule'>{ruleText}</div>
            </div>
            {phase === 'playing' && (
              <div className='tetris-actions'>
                <Button icon={<Pause size={14} />} onClick={pauseGame}>
                  {t('暂停')}
                </Button>
                <Button icon={<Flag size={14} />} onClick={endGame}>
                  {t('结束本局')}
                </Button>
              </div>
            )}
            <div className='tetris-box tetris-keys'>
              <div className='tetris-box-label'>{t('操作说明')}</div>
              <ul>
                <li>
                  <span>
                    <kbd>←</kbd>
                    <kbd>→</kbd>
                  </span>
                  {t('左右移动')}
                </li>
                <li>
                  <span>
                    <kbd>↓</kbd>
                  </span>
                  {t('软降')}
                </li>
                <li>
                  <span>
                    <kbd>{t('空格')}</kbd>
                  </span>
                  {t('硬降')}
                </li>
                <li>
                  <span>
                    <kbd>↑</kbd>
                    <kbd>X</kbd>
                  </span>
                  {t('顺时针旋转')}
                </li>
                <li>
                  <span>
                    <kbd>Z</kbd>
                    <kbd>Ctrl</kbd>
                  </span>
                  {t('逆时针旋转')}
                </li>
                <li>
                  <span>
                    <kbd>C</kbd>
                    <kbd>Shift</kbd>
                  </span>
                  {t('暂存')}
                </li>
                <li>
                  <span>
                    <kbd>P</kbd>
                    <kbd>Esc</kbd>
                  </span>
                  {t('暂停')}
                </li>
              </ul>
            </div>
          </div>
        </div>

        <div className='tetris-touch'>
          {touchControls.map(([control, icon, label]) => (
            <button
              key={control}
              type='button'
              className='tetris-touch-button'
              aria-label={label}
              onPointerDown={(event) => {
                event.preventDefault();
                if (phase === 'playing') pressControl(runRef.current, control);
              }}
              onPointerUp={() => {
                if (runRef.current) releaseControl(runRef.current, control);
              }}
              onPointerLeave={() => {
                if (runRef.current) releaseControl(runRef.current, control);
              }}
              onPointerCancel={() => {
                if (runRef.current) releaseControl(runRef.current, control);
              }}
              onContextMenu={(event) => event.preventDefault()}
            >
              {icon}
            </button>
          ))}
        </div>

        {/* 平时不可见，Cloudflare 需要玩家操作时才在这里展开。 */}
        {playTurnstile.required && (
          <div className='tetris-turnstile'>
            <Turnstile
              key={turnstileKey}
              sitekey={playTurnstile.site_key}
              appearance='interaction-only'
              onVerify={(token) => {
                setTurnstileToken(token);
                setTurnstileFailed(false);
              }}
              onExpire={() => setTurnstileToken('')}
              onError={() => {
                setTurnstileToken('');
                setTurnstileFailed(true);
              }}
            />
          </div>
        )}
      </div>
    </div>
  );
};

export default Tetris;
