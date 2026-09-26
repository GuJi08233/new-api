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
import { useNavigate } from 'react-router-dom';
import { Button, Empty, Spin } from '@douyinfe/semi-ui';
import {
  IllustrationConstruction,
  IllustrationConstructionDark,
} from '@douyinfe/semi-illustrations';
import { ArrowLeft } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../../helpers';
import '../games.css';
import './plinko.css';

// 钉板的坐标：相邻两颗钉子横向相距 PEG_GAP，相邻两排纵向相距 ROW_GAP。第 r 排(从 0 起)有 r + 3 颗钉子，
// 小球每碰到一排钉子就向左或向右偏半个钉距，最后一排下面是 rows + 1 个格子。横向位置以钉距为单位、以中线为 0。
const PEG_GAP = 32;
const ROW_GAP = 30;
const PEG_TOP = 46;
const PEG_RADIUS = 3.6;
const BALL_RADIUS = 7;
const BALL_START_Y = 12;
const SLOT_OFFSET = 20;
const SLOT_HEIGHT = 30;
const SLOT_GAP = 3;
const BOARD_PADDING = 10;

// 小球动画的时长(秒)：从顶端落到第一排、在相邻两排之间、从最后一排落进格子。每碰到一颗钉子弹起 HOP 高。
const FIRST_FALL = 0.28;
const ROW_FALL = 0.17;
const LAST_FALL = 0.24;
const HOP = 7;
// 钉板上同时最多有这么多颗球。
const MAX_FLYING = 8;
// 派彩达到花费的这么多倍时按大奖展示。
const BIG_WIN_MULTIPLIER = 10;
const HISTORY_SIZE = 12;

// 格子从正中往两边由黄到红取色，下标是离正中的格数。
const SLOT_COLORS = [
  { fill: '#fde047', text: '#422006' },
  { fill: '#fcd34d', text: '#422006' },
  { fill: '#fbbf24', text: '#422006' },
  { fill: '#f59e0b', text: '#431407' },
  { fill: '#f97316', text: '#431407' },
  { fill: '#ef4444', text: '#ffffff' },
  { fill: '#dc2626', text: '#ffffff' },
];
const SVG_NS = 'http://www.w3.org/2000/svg';

const ILLUSTRATION_SIZE = { width: 150, height: 150 };

const pegX = (width, position) => width / 2 + position * PEG_GAP;
const pegY = (row) => PEG_TOP + row * ROW_GAP;
// 小球压在一颗钉子正上方时球心的高度。
const touchY = (row) => pegY(row) - PEG_RADIUS - BALL_RADIUS;
const slotTop = (rows) => pegY(rows - 1) + SLOT_OFFSET;
const slotColor = (slot, rows) =>
  SLOT_COLORS[Math.min(Math.abs(slot - rows / 2), SLOT_COLORS.length - 1)];

// 1 币档的格子写加减多少个游戏币，倍率档写派彩是投入的几倍。
function payoutLabel(tier, payout) {
  if (tier.delta) {
    const net = payout - tier.cost;
    return net > 0 ? `+${net}` : `${net}`;
  }
  return `${Number((payout / tier.stake).toFixed(2))}×`;
}

// 概率按落点权重的百分比显示，去掉多余的零。
const formatPercent = (weight, total) =>
  `${Number(((weight * 100) / total).toFixed(2))}%`;

// 小球沿服务端给出的路径落下的途经点与每一段的时长：从顶端落到第一排钉子上，之后每碰到一颗钉子向右(1)或向左(0)
// 偏半个钉距，落到下一排的钉子上，碰过最后一排后落进格子正中。
function flightPlan(path, rows) {
  const points = [{ x: 0, y: BALL_START_Y }];
  const durations = [FIRST_FALL];
  let position = 0;
  path.forEach((right, row) => {
    points.push({ x: position, y: touchY(row) });
    position += right ? 0.5 : -0.5;
    durations.push(row === rows - 1 ? LAST_FALL : ROW_FALL);
  });
  points.push({ x: position, y: slotTop(rows) + SLOT_HEIGHT / 2 });
  return { points, durations };
}

// 小球起落 elapsed 秒时所在的位置与第几段。横向匀速；纵向逐段加速下落，碰过钉子的那几段先弹起一点。
function ballPosition(plan, elapsed) {
  const { points, durations } = plan;
  let start = 0;
  for (let segment = 0; segment < durations.length; segment++) {
    if (elapsed < start + durations[segment]) {
      const progress = (elapsed - start) / durations[segment];
      const from = points[segment];
      const to = points[segment + 1];
      const hop = segment === 0 ? 0 : HOP * 4 * progress * (1 - progress);
      return {
        x: from.x + (to.x - from.x) * progress,
        y: from.y + (to.y - from.y) * progress * progress - hop,
        segment,
      };
    }
    start += durations[segment];
  }
  return { ...points[points.length - 1], segment: durations.length };
}

// 逐帧推进钉板上的小球：碰到的钉子闪一下，落进格子的球交给 onLand。钉板上没有球时停下。
function animateBalls(stage, onLand) {
  if (stage.frame) return;
  const step = () => {
    const now = performance.now() / 1000;
    const width = Number(stage.board.dataset.width);
    for (const ball of [...stage.balls]) {
      const position = ballPosition(ball.plan, now - ball.startedAt);
      ball.element.setAttribute('cx', pegX(width, position.x).toFixed(2));
      ball.element.setAttribute('cy', position.y.toFixed(2));
      // 进入第 segment 段时刚碰过第 segment - 1 排的钉子。
      for (
        let segment = ball.segment + 1;
        segment <= position.segment;
        segment++
      ) {
        const row = segment - 1;
        if (row < 0 || row >= ball.plan.points.length - 2) continue;
        const peg = ball.plan.points[segment].x + (row + 2) / 2;
        stage.board.querySelector(`[data-peg='${row}-${peg}']`)?.animate?.(
          [
            { transform: 'scale(1.9)', fill: '#fde047' },
            { transform: 'scale(1)', fill: '#e2e8f0' },
          ],
          { duration: 220, easing: 'ease-out' },
        );
      }
      ball.segment = position.segment;
      if (position.segment >= ball.plan.durations.length) {
        stage.balls = stage.balls.filter((item) => item !== ball);
        ball.element.remove();
        onLand(ball);
      }
    }
    stage.frame = stage.balls.length ? requestAnimationFrame(step) : 0;
  };
  stage.frame = requestAnimationFrame(step);
}

// 更新某一档的进度。
function patchTier(rule, stake, patch) {
  return {
    ...rule,
    tiers: rule.tiers.map((tier) =>
      tier.stake === stake ? { ...tier, ...patch } : tier,
    ),
  };
}

const Plinko = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const boardRef = useRef(null);
  const ballLayerRef = useRef(null);
  // 钉板上正在落下的球与动画帧。渲染循环直接读写它，不经过 React 状态。
  const stageRef = useRef({ balls: [], frame: 0, board: null });
  const aliveRef = useRef(true);
  // 同一时间只发一个投球请求：服务端依次结算，每颗球带回的余额也就依次更新。连点时状态还没来得及刷新，用它挡住。
  const requestingRef = useRef(false);
  const nextBallRef = useRef(0);
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState('');
  const [rule, setRule] = useState(null);
  const [stake, setStake] = useState(null);
  const [symbol, setSymbol] = useState('');
  // balance 是服务端最新的余额，据此判断还能不能投；shownBalance 等球落进格子才更新，免得还在落就剧透。
  const [balance, setBalance] = useState(0);
  const [shownBalance, setShownBalance] = useState(0);
  const [requesting, setRequesting] = useState(false);
  const [flying, setFlying] = useState(0);
  // 最近落下的一颗球 { result, tier } 或投球失败的原因 { error }。
  const [outcome, setOutcome] = useState(null);
  const [history, setHistory] = useState([]);

  // refresh 为真时是投球没有结果后重新读取：这时读不到就保留页面上的内容，失败原因已经显示在钉板下方。
  const loadInfo = useCallback(
    async (refresh = false) => {
      try {
        const res = await API.get('/api/game/self');
        if (!res.data.success) {
          setUnavailable(res.data.message);
          return;
        }
        const plinko = (res.data.data.games || []).find(
          (game) => game.key === 'plinko',
        );
        if (!plinko) {
          setUnavailable(t('Plinko 暂未开放'));
          return;
        }
        setRule(plinko);
        // 默认选第一个开放的档。
        setStake(
          (previous) =>
            previous ??
            (
              plinko.tiers.find((tier) => tier.daily_drops > 0) ||
              plinko.tiers[0]
            ).stake,
        );
        setSymbol(res.data.data.token?.symbol || '');
        setBalance(res.data.data.account?.balance || 0);
        setShownBalance(res.data.data.account?.balance || 0);
      } catch (error) {
        if (refresh) return;
        // 被风控封禁的 IP 收到 403，显示服务端给出的原因。
        setUnavailable(
          error?.response?.data?.message || t('加载失败，请刷新重试'),
        );
      } finally {
        setLoading(false);
      }
    },
    [t],
  );

  useEffect(() => {
    loadInfo();
  }, [loadInfo]);

  // 离开页面时停掉动画；还没回来的结果不再处理，余额以服务端为准。开发模式下会先卸载再挂载一次，挂载时要重新标记。
  useEffect(() => {
    const stage = stageRef.current;
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
      cancelAnimationFrame(stage.frame);
      stage.frame = 0;
      stage.balls = [];
    };
  }, []);

  // 球落进格子才公布结果、更新余额和今日输赢。
  const landBall = useCallback((ball) => {
    const { result, tier } = ball;
    setShownBalance(result.balance);
    setRule((previous) =>
      patchTier(previous, tier.stake, { earned_today: result.earned_today }),
    );
    setOutcome({ result, tier });
    setHistory((previous) =>
      [
        {
          id: ball.id,
          label: payoutLabel(tier, result.payout),
          slot: result.slot,
        },
        ...previous,
      ].slice(0, HISTORY_SIZE),
    );
    setFlying((count) => count - 1);
    boardRef.current?.querySelector(`[data-slot='${result.slot}']`)?.animate?.(
      [
        { transform: 'translateY(5px)', filter: 'brightness(1.35)' },
        { transform: 'translateY(0)', filter: 'brightness(1)' },
      ],
      { duration: 450, easing: 'ease-out' },
    );
  }, []);

  const tier = rule?.tiers.find((item) => item.stake === stake);
  const playsLeft = tier ? Math.max(tier.daily_drops - tier.plays_today, 0) : 0;
  const canDrop =
    !!tier &&
    !requesting &&
    flying < MAX_FLYING &&
    playsLeft > 0 &&
    balance >= tier.cost;

  const drop = useCallback(async () => {
    if (!canDrop || requestingRef.current) return;
    const droppedTier = tier;
    requestingRef.current = true;
    setRequesting(true);
    // 按钮保留焦点时，空格键会再次触发它。
    document.activeElement?.blur?.();
    let response;
    try {
      const res = await API.post(
        '/api/game/plinko/drop',
        { stake: droppedTier.stake },
        { skipErrorHandler: true },
      );
      response = res.data.success
        ? { result: res.data.data }
        : { error: res.data.message };
    } catch (error) {
      const status = error?.response?.status;
      let message = t('网络异常，本次结果以游戏币明细为准');
      if (status === 401) {
        // 登录已过期：交给全局处理，跳转到登录页。
        showError(error);
      } else if (status === 429) {
        message = t('投得太快了，请稍后再试');
      } else if (status < 500 && error.response.data?.message) {
        // 服务端给出了拒绝原因，例如 IP 已被风控封禁。
        message = error.response.data.message;
      }
      response = { error: message };
    }
    requestingRef.current = false;
    if (!aliveRef.current) return;
    setRequesting(false);
    if (response.error) {
      // 投球被拒绝或请求没有得到答复：页面上的余额与次数可能已经过时(请求也可能已经结算)，重新读取。
      setOutcome(response);
      loadInfo(true);
      return;
    }
    const { result } = response;
    // 次数和能不能再投以服务端为准，余额的显示等球落下再更新。
    setBalance(result.balance);
    setRule((previous) =>
      patchTier(previous, result.stake, {
        plays_today: result.plays_today,
        daily_drops: result.daily_drops,
      }),
    );
    setOutcome((previous) => (previous?.error ? null : previous));
    setFlying((count) => count + 1);
    const ball = {
      id: nextBallRef.current++,
      result,
      tier: droppedTier,
      plan: flightPlan(result.path, rule.rows),
      startedAt: performance.now() / 1000,
      segment: 0,
    };
    const reducedMotion = window.matchMedia?.(
      '(prefers-reduced-motion: reduce)',
    ).matches;
    if (reducedMotion || !boardRef.current || !ballLayerRef.current) {
      landBall(ball);
      return;
    }
    ball.element = document.createElementNS(SVG_NS, 'circle');
    ball.element.setAttribute('r', BALL_RADIUS);
    ball.element.setAttribute('class', 'plinko-ball');
    ball.element.setAttribute(
      'cx',
      pegX(Number(boardRef.current.dataset.width), 0),
    );
    ball.element.setAttribute('cy', BALL_START_Y);
    ballLayerRef.current.appendChild(ball.element);
    const stage = stageRef.current;
    stage.board = boardRef.current;
    stage.balls.push(ball);
    animateBalls(stage, landBall);
  }, [canDrop, tier, rule, t, loadInfo, landBall]);

  // 空格键投球；焦点在按钮上时交给按钮自己处理，免得触发两次。
  useEffect(() => {
    const onKeyDown = (event) => {
      if (event.code !== 'Space' || event.target !== document.body) return;
      event.preventDefault();
      if (!event.repeat) drop();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [drop]);

  if (loading || unavailable) {
    return (
      <div className='games-page'>
        <div className='games-container plinko-page'>
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

  const { rows, weights } = rule;
  const width = (rows + 3) * PEG_GAP;
  const height = slotTop(rows) + SLOT_HEIGHT + BOARD_PADDING;
  const totalWeight = weights.reduce((sum, weight) => sum + weight, 0);
  // 同一派彩的几格合并公示，从高到低列出。
  const odds = [...new Set(tier.payouts)]
    .sort((a, b) => b - a)
    .map((payout) => ({
      payout,
      slot: tier.payouts.indexOf(payout),
      weight: tier.payouts.reduce(
        (sum, value, slot) => (value === payout ? sum + weights[slot] : sum),
        0,
      ),
    }));
  const earnedToday = rule.tiers.reduce(
    (sum, item) => sum + item.earned_today,
    0,
  );
  const busy = requesting || flying > 0;

  const result = outcome?.result;
  let message = t('选好一档，点击按钮投球，也可以按空格键');
  let messageClass = '';
  if (outcome?.error) {
    message = outcome.error;
    messageClass = 'is-error';
  } else if (result) {
    const net = result.payout - outcome.tier.cost;
    if (!outcome.tier.delta) {
      message = t('{{multiplier}} 倍，派彩 {{payout}} {{symbol}}', {
        multiplier: Number((result.payout / outcome.tier.stake).toFixed(2)),
        payout: result.payout,
        symbol,
      });
    } else if (net > 0) {
      message = t('赢得 {{win}} {{symbol}}', { win: net, symbol });
    } else {
      message = t('输掉 {{loss}} {{symbol}}', { loss: -net, symbol });
    }
    if (net > 0) {
      messageClass =
        result.payout >= BIG_WIN_MULTIPLIER * outcome.tier.cost
          ? 'is-win is-big-win'
          : 'is-win';
    } else if (net < 0) {
      messageClass = 'is-loss';
    }
  }
  const bigWin = messageClass.includes('is-big-win');

  let hint = '';
  if (tier.daily_drops === 0) {
    hint = t('这一档暂未开放');
  } else if (playsLeft === 0) {
    hint = t('今天这一档的次数已经用完，明天再来');
  } else if (balance < tier.cost) {
    hint = tier.delta
      ? t('一颗球最多扣 {{cost}} 个游戏币，余额至少要有 {{cost}} {{symbol}}', {
          cost: tier.cost,
          symbol,
        })
      : t('游戏币余额不足');
  }

  const earned = earnedToday > 0 ? `+${earnedToday}` : earnedToday;
  const averageLoss = Number((tier.cost * (1 - tier.payback)).toFixed(2));

  return (
    <div className='games-page'>
      <div className='games-glow' aria-hidden />
      <div className='games-container plinko-page'>
        <div className='plinko-topbar'>
          <Button
            theme='borderless'
            icon={<ArrowLeft size={16} />}
            onClick={() => navigate('/games')}
          >
            {t('游戏中心')}
          </Button>
          <h1 className='plinko-title'>{t('Plinko')}</h1>
        </div>

        <div className='plinko-layout'>
          <div className={bigWin ? 'plinko-stage is-big-win' : 'plinko-stage'}>
            <div className='plinko-tiers' role='group'>
              {rule.tiers.map((item) => (
                <button
                  key={item.stake}
                  type='button'
                  className={
                    item.stake === stake
                      ? 'plinko-tier is-active'
                      : 'plinko-tier'
                  }
                  disabled={busy || item.daily_drops === 0}
                  aria-pressed={item.stake === stake}
                  onClick={() => setStake(item.stake)}
                >
                  <span className='plinko-tier-stake'>
                    {item.stake} {symbol}
                  </span>
                  <span className='plinko-tier-note'>
                    {item.daily_drops > 0
                      ? t('今日剩 {{count}} 颗', {
                          count: Math.max(
                            item.daily_drops - item.plays_today,
                            0,
                          ),
                        })
                      : t('未开放')}
                  </span>
                </button>
              ))}
            </div>

            <svg
              ref={boardRef}
              className='plinko-board'
              viewBox={`0 0 ${width} ${height}`}
              data-width={width}
              aria-hidden
            >
              {Array.from({ length: rows }, (_, row) =>
                Array.from({ length: row + 3 }, (__, peg) => (
                  <circle
                    key={`${row}-${peg}`}
                    data-peg={`${row}-${peg}`}
                    className='plinko-peg'
                    cx={pegX(width, peg - (row + 2) / 2)}
                    cy={pegY(row)}
                    r={PEG_RADIUS}
                  />
                )),
              )}
              {tier.payouts.map((payout, slot) => {
                const x = pegX(width, slot - rows / 2);
                return (
                  <g key={slot} data-slot={slot} className='plinko-slot'>
                    <rect
                      x={x - (PEG_GAP - SLOT_GAP) / 2}
                      y={slotTop(rows)}
                      width={PEG_GAP - SLOT_GAP}
                      height={SLOT_HEIGHT}
                      rx='5'
                      fill={slotColor(slot, rows).fill}
                    />
                    <text
                      x={x}
                      y={slotTop(rows) + SLOT_HEIGHT / 2 + 4}
                      textAnchor='middle'
                      className='plinko-slot-label'
                      fill={slotColor(slot, rows).text}
                    >
                      {payoutLabel(tier, payout)}
                    </text>
                  </g>
                );
              })}
              <g ref={ballLayerRef} />
            </svg>

            <div
              className={`plinko-message ${messageClass}`}
              aria-live='polite'
            >
              {message}
            </div>

            <div className='plinko-controls'>
              <button
                type='button'
                className='plinko-drop'
                disabled={!canDrop}
                onClick={drop}
              >
                {tier.delta
                  ? t('投一颗球')
                  : t('投 {{stake}} {{symbol}}', { stake: tier.stake, symbol })}
              </button>
              <div className='plinko-counter'>
                {t('今日已投 {{plays}} / {{limit}} 颗', {
                  plays: tier.plays_today,
                  limit: tier.daily_drops,
                })}
              </div>
              {hint && <div className='plinko-hint'>{hint}</div>}
            </div>
          </div>

          <div className='plinko-side'>
            <div className='plinko-box'>
              <div className='plinko-box-label'>{t('游戏币余额')}</div>
              <div className='plinko-box-value'>
                {shownBalance}
                <span>{symbol}</span>
              </div>
            </div>
            <div className='plinko-box'>
              <div className='plinko-box-label'>{t('今日输赢')}</div>
              <div className='plinko-box-value'>
                {earned}
                <span>{symbol}</span>
              </div>
              <div className='plinko-box-note'>{t('三档合计')}</div>
              {history.length > 0 && (
                <div className='plinko-history'>
                  {history.map((item) => (
                    <span
                      key={item.id}
                      className='plinko-history-item'
                      style={{
                        background: slotColor(item.slot, rows).fill,
                        color: slotColor(item.slot, rows).text,
                      }}
                    >
                      {item.label}
                    </span>
                  ))}
                </div>
              )}
            </div>
            <div className='plinko-box'>
              <div className='plinko-box-label'>
                {t('{{stake}} 币档概率公示', { stake: tier.stake })}
              </div>
              <table className='plinko-odds'>
                <thead>
                  <tr>
                    <th>{tier.delta ? t('加减') : t('倍数')}</th>
                    <th>{t('派彩')}</th>
                    <th>{t('概率')}</th>
                  </tr>
                </thead>
                <tbody>
                  {odds.map((item) => (
                    <tr key={item.payout}>
                      <td>
                        <span
                          className='plinko-odds-swatch'
                          style={{
                            background: slotColor(item.slot, rows).fill,
                          }}
                        />
                        {payoutLabel(tier, item.payout)}
                      </td>
                      <td>
                        {item.payout} {symbol}
                      </td>
                      <td>{formatPercent(item.weight, totalWeight)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <ul className='plinko-rules'>
                <li>
                  {t(
                    '钉板共 {{rows}} 排，小球每碰到一排钉子往左往右的机会各一半，落进各格的概率以上表为准',
                    { rows },
                  )}
                </li>
                {tier.delta ? (
                  <li>
                    {t(
                      '格子上的数直接加到余额上，负数就是扣掉，一颗球最多扣 {{cost}} 个',
                      { cost: tier.cost },
                    )}
                  </li>
                ) : (
                  <li>{t('派彩 = 投入 × 格子上的倍数，含投入本身')}</li>
                )}
                <li>
                  {tier.delta
                    ? t('平均每颗球输 {{loss}} {{symbol}}', {
                        loss: averageLoss,
                        symbol,
                      })
                    : t('理论返还率 {{rate}}%', {
                        rate: Number((tier.payback * 100).toFixed(2)),
                      })}
                </li>
                <li>
                  {t('这一档每人每天最多投 {{limit}} 颗，三档分别计算', {
                    limit: tier.daily_drops,
                  })}
                </li>
              </ul>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Plinko;
