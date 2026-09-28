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
import { Flag, RotateCcw, Trophy } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API } from '../../../helpers';
import { getGameMetaMap } from '../../../constants';
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
import { Game2048, MAX_MOVES, MOVE, MoveRecorder, SIZE } from './engine';
import './g2048.css';

const START_URL = '/api/game/2048/start';
const FINISH_URL = '/api/game/2048/finish';

// 按 KeyboardEvent.code 匹配，不受输入法和键盘布局影响。
const KEY_MOVES = {
  ArrowUp: MOVE.UP,
  KeyW: MOVE.UP,
  ArrowRight: MOVE.RIGHT,
  KeyD: MOVE.RIGHT,
  ArrowDown: MOVE.DOWN,
  KeyS: MOVE.DOWN,
  ArrowLeft: MOVE.LEFT,
  KeyA: MOVE.LEFT,
};
// 手指或鼠标至少拖动这么远(像素)才算一次滑动。
const SWIPE_PX = 24;
const CELLS = Array.from({ length: SIZE * SIZE }, (_, index) => index);

// 一局开始时盘面上的方块。newId 给每个方块一个不变的标识：同一个方块移动前后是同一个元素，CSS 过渡才画得出滑动。
function boardTiles(board, newId) {
  const tiles = [];
  board.forEach((value, index) => {
    if (value !== 0) tiles.push({ id: newId(), value, index, fresh: true });
  });
  return tiles;
}

// 按引擎给出的这一步(game.lastMove)推出方块的新位置：滑动的方块换个位置；合并的两块滑到一起后消失，
// 换成数值翻倍的新方块；最后加上新生成的方块。对不上盘面时(不应发生)按盘面重建。
function movedTiles(tiles, move, board, newId) {
  const byIndex = new Map();
  tiles.forEach((tile) => {
    if (!tile.gone) byIndex.set(tile.index, tile);
  });
  const next = [];
  const mergedAt = new Set();
  for (const { from, to, merged } of move.slides) {
    const tile = byIndex.get(from);
    if (!tile) return boardTiles(board, newId);
    next.push({ id: tile.id, value: tile.value, index: to, gone: merged });
    if (merged) mergedAt.add(to);
  }
  mergedAt.forEach((index) => {
    next.push({ id: newId(), value: board[index], index, merged: true });
  });
  if (move.spawned) {
    const { index, value } = move.spawned;
    next.push({ id: newId(), value, index, fresh: true });
  }
  return next;
}

// 方块的样式：配色按数值，字号按位数；新方块与合并出的方块各有一段动画，合并掉的方块滑到位后消失。
function tileClass(tile) {
  const tone = tile.value <= 2048 ? `is-v${tile.value}` : 'is-super';
  const digits = `is-d${Math.min(String(tile.value).length, 6)}`;
  let motion = '';
  if (tile.gone) {
    motion = 'is-gone';
  } else if (tile.merged) {
    motion = 'is-merged';
  } else if (tile.fresh) {
    motion = 'is-new';
  }
  return `g2048-tile ${tone} ${digits} ${motion}`;
}

// 结算请求：服务端从开局的种子重放这串移动，成绩以重放为准。
function finishPayload(run) {
  return { session_id: run.sessionId, moves: run.recorder.moves };
}

const G2048 = () => {
  const { t } = useTranslation();
  const meta = getGameMetaMap(t)['2048'];
  const { loading, unavailable, info, patch, reload } = useGameInfo('2048');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  // idle 还没开局，starting 开局中，playing 对局中，submitting 结算中，result 已结算(或被拒绝)，error 结算没有得到答复。
  const [phase, setPhase] = useState('idle');
  const [tiles, setTiles] = useState([]);
  const [stats, setStats] = useState({ score: 0, maxTile: 0, moves: 0 });
  const [result, setResult] = useState(null);
  const [notice, setNotice] = useState(null);
  // 当前一局：会话、引擎与移动记录。按键直接改它，不经过 React 状态。
  const runRef = useRef(null);
  const tileIdRef = useRef(0);
  const swipeRef = useRef(null);
  // 确认框打开期间不响应方向键，免得在确认框后面挪动方块。
  const confirmingRef = useRef(false);
  const mountedRef = useRef(false);
  const symbol = info?.symbol || '';
  const playing = phase === 'playing';

  const newTileId = useCallback(() => {
    tileIdRef.current += 1;
    return tileIdRef.current;
  }, []);

  const submit = useCallback(async () => {
    const run = runRef.current;
    if (!run || run.submitting || run.submitted) return;
    run.submitting = true;
    setPhase('submitting');
    const response = await gamePost(FINISH_URL, finishPayload(run), t);
    run.submitting = false;
    if (!mountedRef.current || runRef.current !== run) return;
    // 没有得到答复或被限流时整局记录还在，可以重新提交；已经结算过的话，服务端会返回第一次的结果。
    if (response.network || response.error === t('操作太快了，请稍后再试')) {
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

  const applyMove = useCallback(
    (direction) => {
      const run = runRef.current;
      if (!run || run.submitting || run.submitted || confirmingRef.current) {
        return;
      }
      const { game } = run;
      if (!game.move(direction)) return;
      run.recorder.record(direction);
      const move = game.lastMove;
      const board = game.board.slice();
      setTiles((previous) => movedTiles(previous, move, board, newTileId));
      setStats({ score: game.score, maxTile: game.maxTile, moves: game.moves });
      if (game.over || game.moves >= MAX_MOVES) submit();
    },
    [newTileId, submit],
  );

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
    const game = new Game2048(response.data.seed);
    runRef.current = {
      sessionId: response.data.session_id,
      game,
      recorder: new MoveRecorder(),
      previousBest: info?.best_score || 0,
    };
    setTiles(boardTiles(game.board, newTileId));
    setStats({ score: 0, maxTile: game.maxTile, moves: 0 });
    setResult(null);
    // 开始按钮保留焦点时，回车会再次触发它。
    document.activeElement?.blur?.();
    setPhase('playing');
  };

  const endGame = () => {
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
    setTiles([]);
    setPhase('idle');
    reload(true);
  };

  // 按键：对局中方向键与 WASD 移动，这些键和空格都不滚动页面。一局刚结束时玩家常常还在按，焦点不在控件上时
  // 也不让它们把页面滚走。对局中刷新或关闭页面，这局来不及结算，先让浏览器确认一次。
  useEffect(() => {
    if (phase === 'idle') return undefined;
    const onKeyDown = (event) => {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      const direction = KEY_MOVES[event.code];
      if (direction === undefined && event.code !== 'Space') return;
      if (!playing) {
        if (event.target === document.body) event.preventDefault();
        return;
      }
      if (
        event.target.closest?.('input, textarea, select, [contenteditable]')
      ) {
        return;
      }
      event.preventDefault();
      // 按住不放的自动重复不算：服务端限制每秒的有效移动数。
      if (direction !== undefined && !event.repeat) applyMove(direction);
    };
    const onBeforeUnload = (event) => {
      if (!playing || !runRef.current?.game.moves) return;
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('beforeunload', onBeforeUnload);
    };
  }, [phase, playing, applyMove]);

  // 离开本页时把进行中的一局按当前成绩结算，成绩不会白白作废。页面已经不在了，不等答复，结果以游戏币明细为准。
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      const run = runRef.current;
      if (!run || run.submitting || run.submitted || run.game.moves === 0) {
        return;
      }
      API.post(FINISH_URL, finishPayload(run), {
        skipErrorHandler: true,
      }).catch(() => {});
    };
  }, []);

  const onPointerDown = (event) => {
    if (!playing) return;
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
    if (!playing || !swipe || swipe.id !== event.pointerId) return;
    const dx = event.clientX - swipe.x;
    const dy = event.clientY - swipe.y;
    if (Math.max(Math.abs(dx), Math.abs(dy)) < SWIPE_PX) return;
    if (Math.abs(dx) > Math.abs(dy)) {
      applyMove(dx > 0 ? MOVE.RIGHT : MOVE.LEFT);
    } else {
      applyMove(dy > 0 ? MOVE.DOWN : MOVE.UP);
    }
  };

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
    } else if (playing || phase === 'submitting') {
      message = {
        kind: '',
        text: t('用方向键、WASD 或在盘面上滑动，合并相同的数字'),
      };
    } else {
      message = {
        kind: '',
        text: skillRuleSummary(info, t, symbol, meta.scoreName),
      };
    }
  }

  let overlay = null;
  if (phase === 'submitting') {
    overlay = (
      <div className='g2048-overlay'>
        <Spin />
        <div className='g2048-overlay-text'>{t('正在结算…')}</div>
      </div>
    );
  } else if (phase === 'error') {
    overlay = (
      <div className='g2048-overlay'>
        <div className='g2048-overlay-title'>{t('结算未完成')}</div>
      </div>
    );
  } else if (phase === 'result' && result.rejected) {
    overlay = (
      <div className='g2048-overlay'>
        <div className='g2048-overlay-title'>{t('本局未能结算')}</div>
      </div>
    );
  } else if (phase === 'result') {
    overlay = (
      <div className='g2048-overlay is-result'>
        <div className='g2048-overlay-title'>
          {result.over ? t('游戏结束') : t('本局结束')}
        </div>
        {result.newRecord && (
          <div className='g2048-record'>
            <Trophy size={14} />
            {t('新纪录')}
          </div>
        )}
        <div className='g2048-overlay-score'>{formatNumber(result.score)}</div>
        {result.score !== result.localScore && (
          <div className='g2048-overlay-text'>
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
  if (playing) {
    controls = (
      <Button icon={<Flag size={14} />} onClick={endGame}>
        {t('结束本局')}
      </Button>
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
              t('方向键或 WASD 把所有方块推向一边，触屏上在盘面滑动'),
              t('两个相同的数字碰在一起合成一个，得分加上合成的数字'),
              t('走不动时自动结算，也可以随时结束本局'),
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
      className='g2048-page'
    >
      <div className='games-stage'>
        <div className='game-hud'>
          <span>
            {t('分数')}
            <strong>{formatNumber(stats.score)}</strong>
          </span>
          <span>
            {t('最大方块')}
            <strong>{stats.maxTile || '-'}</strong>
          </span>
          <span>
            {t('步数')}
            <strong>{stats.moves}</strong>
          </span>
        </div>
        <div
          className={playing ? 'g2048-board is-playing' : 'g2048-board'}
          onPointerDown={onPointerDown}
          onPointerUp={onPointerUp}
          onPointerCancel={() => {
            swipeRef.current = null;
          }}
        >
          <div className='g2048-grid' aria-hidden>
            {CELLS.map((index) => (
              <div key={index} className='g2048-cell' />
            ))}
          </div>
          <div className='g2048-tiles'>
            {tiles.map((tile) => (
              <div
                key={tile.id}
                className={tileClass(tile)}
                style={{
                  '--g2048-row': Math.floor(tile.index / SIZE),
                  '--g2048-col': tile.index % SIZE,
                }}
              >
                <div className='g2048-tile-inner'>{tile.value}</div>
              </div>
            ))}
          </div>
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

export default G2048;
