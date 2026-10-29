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
import { InputNumber } from '@douyinfe/semi-ui';
import { Flame, Rocket } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import BetInput from '../common/BetInput';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { formatCoins, gameGet } from '../common/api';
import {
  WagerStats,
  playWagerFeedback,
  wagerResultMessage,
} from '../common/wager';
import { playSound } from '../common/sound';
import { useGameInfo } from '../common/useGameInfo';
import { useWagerRound } from '../common/useWagerRound';
import './crash.css';

// 倍数都是百分之一倍的整数(250 就是 2.50×)，与服务端一致。
const MIN_POINT = 100;
const RECENT_LIMIT = 10;
// 长轮询出错之后隔多久再试，免得服务端出问题时连续请求。
const RETRY_MS = 3000;
// 自动兑现的几个常用倍数(×)。
const AUTO_PRESETS = [1.5, 2, 5, 10];

const CHART_WIDTH = 600;
const CHART_HEIGHT = 300;
// 曲线离图边留出的空白，火箭和开局即爆的火焰不会被裁掉。
const CHART_PAD = { left: 14, right: 28, top: 22, bottom: 18 };
const CURVE_POINTS = 60;
// 横轴至少显示这么长的时间，刚起飞时曲线不会挤在左下角。
const MIN_CHART_MS = 8000;
const GUIDE_STEPS = [0.2, 0.5, 1, 2, 5, 10, 20, 50, 100, 200, 500];

// 倍数的时间表：开局后 elapsed 毫秒是 floor(100·e^(growth·elapsed))，与服务端的公式相同，只用来播放动画，结果以服务端为准。
function multiplierAt(elapsed, growth) {
  if (elapsed <= 0) return MIN_POINT;
  return Math.floor(MIN_POINT * Math.exp(growth * elapsed));
}

// 倍数第一次达到 multiplier 的时刻(开局后的毫秒数)。
function timeOf(multiplier, growth) {
  return multiplier <= MIN_POINT
    ? 0
    : Math.log(multiplier / MIN_POINT) / growth;
}

// 百分之一倍写成 2.50× 的样子。
function formatMultiplier(multiplier) {
  return `${(multiplier / 100).toFixed(2)}×`;
}

// 纵轴的参考线：按倍数的范围取一个间隔，图上保持两三条线。
function guideLines(top) {
  const step = GUIDE_STEPS.find((value) => (top - 1) / value <= 4) || 1000;
  const lines = [];
  for (let value = step * Math.ceil(1.001 / step); value < top; value += step) {
    lines.push(value);
  }
  return lines;
}

// 飞行曲线：from 到 to 毫秒之间的一段，按 scale 换成图上的坐标。
function curvePath(from, to, growth, scale) {
  const points = [];
  for (let i = 0; i <= CURVE_POINTS; i++) {
    const ms = from + ((to - from) * i) / CURVE_POINTS;
    points.push(
      `${scale.x(ms).toFixed(1)} ${scale.y(Math.exp(growth * ms)).toFixed(1)}`,
    );
  }
  return `M${points.join('L')}`;
}

// 火箭的飞行图：横轴是开局以来的时间，纵轴是倍数，都随飞行自动放大。飞行中按服务器时间(本地时钟加上 offset)每帧推进，
// 兑现按钮显示按当前倍数能拿到的派彩；结束后画出整条曲线：爆了的一局到爆点为止，兑现的一局标出兑现的位置，后面淡淡地画到爆点。
const CrashFlight = ({
  round,
  growth,
  maxPoint,
  offset,
  symbol,
  busy,
  onCashout,
}) => {
  const { t } = useTranslation();
  const flying = round?.status === 'playing';
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!flying) return undefined;
    let last = 0;
    let frame = requestAnimationFrame(function tick() {
      const next = Date.now();
      if (next - last >= 33) {
        last = next;
        setNow(next);
      }
      frame = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(frame);
  }, [flying]);

  let state = 'idle';
  let end = 0;
  let multiplier = MIN_POINT;
  if (flying) {
    state = 'flying';
    end = Math.max(now + offset - round.started_at_ms, 0);
    multiplier = Math.min(multiplierAt(end, growth), maxPoint);
  } else if (round) {
    state = round.cashout_multiplier > 0 ? 'cashed' : 'crashed';
    end = timeOf(round.crash_point, growth);
    multiplier = round.crash_point;
  }
  const cashedAt =
    state === 'cashed' ? timeOf(round.cashout_multiplier, growth) : 0;

  const top = Math.max((multiplier / 100) * 1.2, 2);
  const xMax = Math.max(end * 1.15, MIN_CHART_MS);
  const plotWidth = CHART_WIDTH - CHART_PAD.left - CHART_PAD.right;
  const plotHeight = CHART_HEIGHT - CHART_PAD.top - CHART_PAD.bottom;
  const scale = {
    x: (ms) => CHART_PAD.left + (ms / xMax) * plotWidth,
    y: (value) =>
      CHART_HEIGHT - CHART_PAD.bottom - ((value - 1) / (top - 1)) * plotHeight,
  };
  const tipX = scale.x(end);
  const tipY = scale.y(Math.exp(growth * end));
  // 曲线在尖端的方向，火箭沿着它飞；图标本身朝右上方 45°。
  const slope = Math.atan2(
    (-growth * Math.exp(growth * end) * plotHeight) / (top - 1),
    plotWidth / xMax,
  );
  const angle = (slope * 180) / Math.PI + 45;
  const lit = state === 'cashed' ? cashedAt : end;
  const flightPath = lit > 0 ? curvePath(0, lit, growth, scale) : '';
  const guides = guideLines(top);
  const autoLine =
    flying && round.auto_cashout > 0 && round.auto_cashout / 100 < top
      ? scale.y(round.auto_cashout / 100)
      : null;

  let readout = formatMultiplier(multiplier);
  let caption = t('等待起飞');
  if (state === 'flying') {
    caption =
      round.auto_cashout > 0
        ? t('自动兑现 {{multiplier}}', {
            multiplier: formatMultiplier(round.auto_cashout),
          })
        : t('飞行中');
  } else if (state === 'crashed') {
    caption = t('爆炸了');
  } else if (state === 'cashed') {
    readout = formatMultiplier(round.cashout_multiplier);
    caption = t('已兑现，爆点 {{point}}', {
      point: formatMultiplier(round.crash_point),
    });
  }

  return (
    <>
      <div className={`crash-chart is-${state}`}>
        <svg
          viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`}
          preserveAspectRatio='none'
          aria-hidden
        >
          {guides.map((value) => (
            <line
              key={value}
              className='crash-guide'
              x1='0'
              x2={CHART_WIDTH}
              y1={scale.y(value)}
              y2={scale.y(value)}
            />
          ))}
          {autoLine !== null && (
            <line
              className='crash-auto-line'
              x1='0'
              x2={CHART_WIDTH}
              y1={autoLine}
              y2={autoLine}
            />
          )}
          {state !== 'idle' && lit > 0 && (
            <>
              <path
                className='crash-area'
                d={`${flightPath}L${scale.x(lit).toFixed(1)} ${scale.y(1)}L${scale.x(0)} ${scale.y(1)}Z`}
              />
              <path className='crash-curve' d={flightPath} />
            </>
          )}
          {state === 'cashed' && end > cashedAt && (
            <path
              className='crash-curve is-after'
              d={curvePath(cashedAt, end, growth, scale)}
            />
          )}
          {state === 'cashed' && (
            <circle
              className='crash-cashout-dot'
              cx={scale.x(cashedAt)}
              cy={scale.y(round.cashout_multiplier / 100)}
              r='6'
            />
          )}
        </svg>
        {guides.map((value) => (
          <span
            key={value}
            className='crash-guide-label'
            style={{ top: `${(scale.y(value) / CHART_HEIGHT) * 100}%` }}
          >
            {`${Number(value.toFixed(1))}×`}
          </span>
        ))}
        {state !== 'idle' && (
          <span
            className='crash-tip'
            style={{
              left: `${(tipX / CHART_WIDTH) * 100}%`,
              top: `${(tipY / CHART_HEIGHT) * 100}%`,
              transform: `translate(-50%, -50%) rotate(${flying ? angle : 0}deg)`,
            }}
          >
            {flying ? <Rocket /> : <Flame />}
          </span>
        )}
        <div className='crash-readout'>
          <span className='crash-readout-value'>{readout}</span>
          <span className='crash-readout-caption'>{caption}</span>
        </div>
      </div>
      {flying && (
        <div className='game-controls'>
          <button
            type='button'
            className='games-play-button crash-cashout'
            disabled={busy}
            onClick={onCashout}
          >
            {t('兑现 {{amount}} {{symbol}}', {
              amount: formatCoins((round.bet * multiplier) / 100),
              symbol,
            })}
          </button>
        </div>
      )}
    </>
  );
};

// 结算一局之后把它的爆点加到最近几局的最前面。
function recentAfterSettle(previous, round) {
  return {
    recent: [round.crash_point, ...(previous.recent || [])].slice(
      0,
      RECENT_LIMIT,
    ),
  };
}

// 一局结束后的提示：爆了输掉下注；兑现时说明在多少倍兑现、这一局的爆点。
function crashResultMessage(round, t, symbol) {
  const point = formatMultiplier(round.crash_point);
  if (!(round.cashout_multiplier > 0)) {
    return {
      kind: 'loss',
      text: t('火箭在 {{point}} 爆炸，输掉 {{bet}} {{symbol}}', {
        point,
        bet: round.bet,
        symbol,
      }),
    };
  }
  return {
    ...wagerResultMessage({ bet: round.bet, win: round.win }, t, symbol),
    note: t('在 {{multiplier}} 兑现，这一局的爆点是 {{point}}', {
      multiplier: formatMultiplier(round.cashout_multiplier),
      point,
    }),
  };
}

const Crash = () => {
  const { t } = useTranslation();
  const { loading, unavailable, info, setInfo, reload } = useGameInfo('crash');
  const { round, busy, error, apply, refresh, send } = useWagerRound(
    'crash',
    setInfo,
    reload,
    recentAfterSettle,
  );
  const [bet, setBet] = useState(1);
  // 自动兑现的倍数(×)，null 表示不自动兑现。
  const [auto, setAuto] = useState(null);
  // 服务器时钟减本地时钟(毫秒)：动画按服务器时间推进。
  const [offset, setOffset] = useState(0);
  // 起飞、爆炸、兑现各响一声。只在同一局从飞行中变成结束时揭晓，刷新页面读回来的一局不响。
  const seenRef = useRef({ session: 0, status: '' });
  useEffect(() => {
    if (!round) return;
    const previous = seenRef.current;
    if (previous.session !== round.session_id && round.status === 'playing') {
      playSound('start');
    }
    if (
      previous.session === round.session_id &&
      previous.status === 'playing' &&
      round.status === 'finished'
    ) {
      if (round.cashout_multiplier > 0) {
        playWagerFeedback({ bet: round.bet, win: round.win });
      } else {
        playSound('bomb');
      }
    }
    seenRef.current = { session: round.session_id, status: round.status };
  }, [round]);

  // 拿到游戏信息后读出没结束的一局：还在飞就接着播放，已经过了爆点或自动兑现的时刻，服务端在这时结算。
  const loaded = !!info;
  useEffect(() => {
    if (loaded) refresh();
  }, [loaded, refresh]);

  useEffect(() => {
    if (round?.server_time_ms) setOffset(round.server_time_ms - Date.now());
  }, [round]);

  // 火箭还在飞时一直长轮询：服务端等到这一局自己结束(爆炸或自动兑现)的那一刻才答复，最多等 25 秒；还在飞就接着等。
  const sessionId = round?.session_id;
  const flying = round?.status === 'playing';
  useEffect(() => {
    if (!flying) return undefined;
    let alive = true;
    let retry = 0;
    const controller = new AbortController();
    const poll = async () => {
      while (alive) {
        const response = await gameGet(
          `/api/game/crash/round?session_id=${sessionId}&wait=1`,
          t,
          { signal: controller.signal },
        );
        if (!alive) return;
        if (response.data) {
          if (!response.data.round) return;
          apply(response.data);
          if (response.data.round.status === 'playing') continue;
          // 这一局被别的请求(另一个标签页的兑现或查看)先结算了，响应里没有余额，重新读一次游戏信息。
          if (response.data.balance === undefined) reload(true);
          return;
        }
        await new Promise((resolve) => {
          retry = setTimeout(resolve, RETRY_MS);
        });
      }
    };
    poll();
    return () => {
      alive = false;
      controller.abort();
      clearTimeout(retry);
    };
  }, [flying, sessionId, t, apply, reload]);

  const meta = getGameMetaMap(t).crash;
  // 游戏关闭后只有还没结束一局的玩家能进来，玩完这一局就不能再开新局。
  const closed = info?.enabled === false;
  const symbol = info?.symbol || '';
  const balance = info?.balance || 0;
  const maxBet = Math.max(info?.max_bet || 1, 1);
  const growth = info?.growth || 0.00006;
  const minAuto = (info?.min_auto_cashout || 101) / 100;
  const maxAuto = (info?.max_multiplier || 100000) / 100;
  const stake = Math.min(bet, maxBet);
  const finished = round?.status === 'finished';
  const recent = info?.recent || [];

  let message = {
    kind: '',
    text: t('下注后起飞，在火箭爆炸之前兑现'),
  };
  if (flying) {
    message = {
      kind: '',
      text: t('倍数一直在涨，随时可能爆炸'),
    };
  } else if (finished) {
    message = crashResultMessage(round, t, symbol);
  }

  const start = () => {
    const autoCashout =
      auto === null
        ? 0
        : Math.min(
            Math.max(Math.round(auto * 100), minAuto * 100),
            maxAuto * 100,
          );
    send('start', { bet: stake, auto_cashout: autoCashout }, false);
  };

  // 空格键：飞行中兑现，没在飞时起飞。焦点在输入框或按钮上时交给它们自己处理。
  const keyRef = useRef(null);
  keyRef.current = (event) => {
    if (
      event.code !== 'Space' ||
      event.repeat ||
      event.target !== document.body
    ) {
      return;
    }
    event.preventDefault();
    if (busy) return;
    if (flying) send('cashout');
    else if (!closed && stake <= balance) start();
  };
  useEffect(() => {
    const onKeyDown = (event) => keyRef.current(event);
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  const side = info && (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox label={t('规则')}>
        <RuleList
          items={[
            t(
              '起飞后倍数从 1.00× 开始上升，火箭随时可能爆炸；爆炸之前兑现，按兑现那一刻的倍数派彩',
            ),
            t('爆点在开局时由服务端决定，最高 {{max}}，有时一起飞就爆炸', {
              max: formatMultiplier(info.max_multiplier || 100000),
            }),
            t(
              '可以设自动兑现：倍数涨到设定值时自动兑现，设定值高于这一局的爆点就等不到',
            ),
            t('派彩 = 下注 × 兑现倍数，可以有小数'),
            t('空格键起飞，飞行中按空格兑现'),
            t('理论返还率 {{rate}}%', { rate: info.rtp_percent }),
            t('单次最多下注 {{max}} {{symbol}}', { max: maxBet, symbol }),
          ]}
        />
      </SideBox>
    </>
  );

  return (
    <GameLayout
      title={meta.name}
      loading={loading}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={symbol}
      side={side}
      className='crash-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <div className='crash-recent' aria-label={t('最近的爆点')}>
          <span className='crash-recent-label'>{t('最近的爆点')}</span>
          {recent.length === 0 ? (
            <span className='crash-recent-empty'>{t('还没有玩过')}</span>
          ) : (
            recent.map((point, index) => {
              let tone = 'is-low';
              if (point >= 1000) tone = 'is-high';
              else if (point >= 200) tone = 'is-mid';
              return (
                <span key={index} className={`crash-recent-item ${tone}`}>
                  {formatMultiplier(point)}
                </span>
              );
            })
          )}
        </div>

        <CrashFlight
          round={round}
          growth={growth}
          maxPoint={info?.max_multiplier || 100000}
          offset={offset}
          symbol={symbol}
          busy={busy}
          onCashout={() => send('cashout')}
        />

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>
        {error && <StageMessage kind='error'>{error}</StageMessage>}

        {!flying && (
          <div className='game-controls'>
            <BetInput
              value={stake}
              max={maxBet}
              symbol={symbol}
              disabled={busy}
              onChange={setBet}
            />
            <div className='crash-auto'>
              <span className='crash-auto-label'>{t('自动兑现')}</span>
              <div className='game-chips'>
                <button
                  type='button'
                  className={
                    auto === null ? 'game-chip is-active' : 'game-chip'
                  }
                  disabled={busy}
                  onClick={() => setAuto(null)}
                >
                  {t('不自动')}
                </button>
                {AUTO_PRESETS.map((value) => (
                  <button
                    key={value}
                    type='button'
                    className={
                      value === auto ? 'game-chip is-active' : 'game-chip'
                    }
                    disabled={busy}
                    onClick={() => setAuto(value)}
                  >
                    {`${value}×`}
                  </button>
                ))}
              </div>
              <InputNumber
                value={auto}
                min={minAuto}
                max={maxAuto}
                step={0.1}
                precision={2}
                placeholder={t('不自动')}
                suffix='×'
                disabled={busy}
                style={{ width: 130 }}
                onChange={(value) =>
                  setAuto(typeof value === 'number' ? value : null)
                }
              />
            </div>
            <button
              type='button'
              className='games-play-button'
              disabled={busy || closed || stake > balance}
              onClick={start}
            >
              {finished ? t('再飞一次') : t('起飞')}
            </button>
            {closed && (
              <div className='games-stage-hint'>
                {t('这个游戏已经关闭，不能再开新的一局')}
              </div>
            )}
            {!closed && stake > balance && (
              <div className='games-stage-hint'>{t('游戏币余额不足')}</div>
            )}
          </div>
        )}
      </div>
    </GameLayout>
  );
};

export default Crash;
