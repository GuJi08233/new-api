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
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Empty, Spin } from '@douyinfe/semi-ui';
import {
  IllustrationConstruction,
  IllustrationConstructionDark,
} from '@douyinfe/semi-illustrations';
import { ArrowLeft } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, getLogo, showError } from '../../../helpers';
import { signed } from '../common/api';
import { usePlayTurnstile } from '../common/usePlayTurnstile';
import '../games.css';
import './wheel.css';

// 转盘动画。角度以度计，顺时针为正，时间以秒计。点击后转盘先匀速转动；服务端选出格子后开始匀减速，
// 减速的初速度与匀速一致，看不出衔接，减速期间至少再转 STOP_TURNS 圈才停在那一格。
const SPIN_SPEED = 600;
const STOP_TURNS = 2;
// 点击后至少转这么久才开始减速。
const MIN_SPIN = 0.6;
// 停下的位置离格子边缘至少留出格子宽度的这个比例，不会看起来压在分界线上。
const EDGE_MARGIN = 0.15;
// 派彩达到每次花费的这么多倍时按大奖展示。
const BIG_WIN_MULTIPLIER = 10;

// 转盘画在以圆心为原点、边长 200 的坐标系里。
const WHEEL_RADIUS = 92;
const LABEL_RADIUS = 62;
const TICK_RADIUS = 95.5;
// 奖品格按倍数从高到低取色，与老虎机的符号同一套配色；"谢谢参与"用随主题变化的灰色。
const PRIZE_COLORS = [
  { fill: '#7c3aed', text: '#ffffff' },
  { fill: '#ec4899', text: '#ffffff' },
  { fill: '#d97706', text: '#ffffff' },
  { fill: '#3b82f6', text: '#ffffff' },
  { fill: '#0891b2', text: '#ffffff' },
  { fill: '#059669', text: '#ffffff' },
  { fill: '#e11d48', text: '#ffffff' },
  { fill: '#4f46e5', text: '#ffffff' },
];
const EMPTY_COLOR = {
  fill: 'var(--semi-color-fill-1)',
  text: 'var(--semi-color-text-2)',
};

const ILLUSTRATION_SIZE = { width: 150, height: 150 };

const mod = (value, period) => ((value % period) + period) % period;

// 从指针方向(正上方)起顺时针转过 degrees 度、离圆心 radius 处的坐标。
function polar(radius, degrees) {
  const radians = (degrees * Math.PI) / 180;
  return [radius * Math.sin(radians), -radius * Math.cos(radians)];
}

// 第 index 格的扇形。格子从指针正下方起顺时针排列，每格 sweep 度。
function segmentPath(index, sweep) {
  const [x1, y1] = polar(WHEEL_RADIUS, index * sweep);
  const [x2, y2] = polar(WHEEL_RADIUS, (index + 1) * sweep);
  return `M0 0L${x1.toFixed(3)} ${y1.toFixed(3)}A${WHEEL_RADIUS} ${WHEEL_RADIUS} 0 0 1 ${x2.toFixed(3)} ${y2.toFixed(3)}Z`;
}

// 转盘转到这个角度时，指针指着第 segment 格里随机的一处。
function segmentAngle(segment, count) {
  const sweep = 360 / count;
  const offset = EDGE_MARGIN + Math.random() * (1 - 2 * EDGE_MARGIN);
  return mod(-(segment + offset) * sweep, 360);
}

// 转盘在 now 时刻的角度：还没安排停下时一直匀速转动，之后匀减速到停下。
function wheelAngle(motion, now) {
  const { plan } = motion;
  if (!plan || now < plan.decelAt) {
    return motion.start + SPIN_SPEED * (now - motion.startedAt);
  }
  const elapsed = Math.min(now - plan.decelAt, plan.duration);
  return (
    plan.decelFrom +
    SPIN_SPEED * elapsed -
    (SPIN_SPEED * elapsed * elapsed) / (2 * plan.duration)
  );
}

// 收到结果后安排停在 target 角度(按整圈取余)：先转够最短时间，再匀减速转过至少 STOP_TURNS 圈。
function planStop(motion, target, now) {
  const decelAt = Math.max(now, motion.startedAt + MIN_SPIN);
  const decelFrom = motion.start + SPIN_SPEED * (decelAt - motion.startedAt);
  const distance =
    STOP_TURNS * 360 + mod(target - decelFrom - STOP_TURNS * 360, 360);
  motion.plan = {
    decelAt,
    decelFrom,
    duration: (2 * distance) / SPIN_SPEED,
    final: decelFrom + distance,
  };
}

function renderWheel(disc, angle) {
  if (disc) disc.style.transform = `rotate(${angle}deg)`;
}

// 逐帧驱动一次旋转，停稳后调用 onSettled。
function animateSpin(motion, disc, onSettled) {
  const step = () => {
    const now = performance.now() / 1000;
    renderWheel(disc, wheelAngle(motion, now));
    if (motion.plan && now >= motion.plan.decelAt + motion.plan.duration) {
      onSettled();
      return;
    }
    motion.frame = requestAnimationFrame(step);
  };
  motion.frame = requestAnimationFrame(step);
}

// 概率按权重的百分比显示，去掉多余的零。
const formatPercent = (weight, total) =>
  `${Number(((weight * 100) / total).toFixed(2))}%`;

const Wheel = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const discRef = useRef(null);
  // 进行中的一次旋转：动画状态与服务端结果。渲染循环直接读写它，不经过 React 状态。
  const motionRef = useRef(null);
  const infoRequestRef = useRef(0);
  const dayEndsRef = useRef(0);
  // 转盘停下时的角度，下一次旋转从这里开始。
  const angleRef = useRef(null);
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState('');
  const [rule, setRule] = useState(null);
  const [symbol, setSymbol] = useState('');
  const [balance, setBalance] = useState(0);
  const [spinning, setSpinning] = useState(false);
  // 上一次旋转的结果 { result } 或失败原因 { error }。
  const [outcome, setOutcome] = useState(null);
  const [playTurnstile, setPlayTurnstile] = useState({ required: false });
  // 转盘中心显示站点 Logo，加载失败时改为显示游戏币符号。
  const [logoFailed, setLogoFailed] = useState(false);

  // refresh 为真时是旋转没有结果后重新读取：这时读不到就保留页面上的内容，失败原因已经显示在转盘下方。
  const loadInfo = useCallback(
    async (refresh = false) => {
      if (motionRef.current) return;
      const request = ++infoRequestRef.current;
      try {
        const res = await API.get('/api/game/wheel');
        if (request !== infoRequestRef.current || motionRef.current) return;
        if (!res.data.success) {
          if (!refresh) setUnavailable(res.data.message);
          return;
        }
        const wheel = res.data.data;
        dayEndsRef.current =
          Date.now() +
          Math.max(0, wheel.day_ends_at - wheel.server_time) * 1000;
        // 第一次加载时转盘停在随机的角度，只为了好看。
        if (angleRef.current === null) {
          angleRef.current = Math.random() * 360;
        }
        setRule(wheel);
        setSymbol(wheel.symbol || '');
        setBalance(wheel.balance || 0);
        setPlayTurnstile(wheel.play_turnstile || { required: false });
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

  useEffect(() => {
    const visible = () => {
      if (!document.hidden) loadInfo(true);
    };
    const timer = setInterval(() => {
      if (
        !document.hidden &&
        dayEndsRef.current &&
        Date.now() >= dayEndsRef.current
      )
        loadInfo(true);
    }, 1000);
    document.addEventListener('visibilitychange', visible);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', visible);
    };
  }, [loadInfo]);

  // 转盘渲染出来后、浏览器绘制之前转到当前角度，免得先闪一下初始角度。
  useLayoutEffect(() => {
    if (!rule || motionRef.current) return;
    renderWheel(discRef.current, angleRef.current);
  }, [rule]);

  // 离开页面时停掉动画；还没回来的结果不再处理，余额以服务端为准。
  useEffect(
    () => () => {
      if (motionRef.current) cancelAnimationFrame(motionRef.current.frame);
      motionRef.current = null;
    },
    [],
  );

  // 转盘停稳后才公布结果、更新余额和次数，免得还在转就剧透。
  const settleSpin = useCallback(
    (motion) => {
      if (motionRef.current !== motion) return;
      motionRef.current = null;
      angleRef.current = mod(motion.plan.final, 360);
      renderWheel(discRef.current, angleRef.current);
      const { result } = motion.response;
      if (result) {
        setBalance(result.balance);
        // 免费旋转的结果带着免费次数，付费旋转没有这些字段，沿用原来的值。
        setRule((previous) => ({
          ...previous,
          net_today: result.net_today,
          plays_today: result.plays_today,
          free_used_today: result.free_used_today ?? previous.free_used_today,
          ip_free_used_today:
            result.ip_free_used_today ?? previous.ip_free_used_today,
          best_win: Math.max(previous.best_win || 0, result.win),
        }));
      } else {
        // 旋转被拒绝或请求没有得到答复：页面上的余额与次数可能已经过时(请求也可能已经结算)，重新读取。
        loadInfo(true);
      }
      setOutcome(motion.response);
      setSpinning(false);
    },
    [loadInfo],
  );

  // 免费次数按账号和同一 IP 分别计算，两边都有剩余才能免费转。
  // 付费旋转不限次数。
  const freeLeft = rule
    ? Math.max(
        Math.min(
          rule.free_spins - rule.free_used_today,
          rule.free_spins - rule.ip_free_used_today,
        ),
        0,
      )
    : 0;
  const free = freeLeft > 0;
  const {
    required: needTurnstile,
    ready: turnstileReady,
    failed: turnstileFailed,
    take: takeTurnstile,
    widget: turnstileWidget,
  } = usePlayTurnstile(free ? playTurnstile : { required: false });
  const waitingTurnstile = !turnstileReady;
  const canSpin =
    !!rule &&
    !spinning &&
    (free ? !waitingTurnstile : balance >= rule.spin_cost);

  const spin = useCallback(async () => {
    if (!canSpin || motionRef.current) return;
    if (dayEndsRef.current && Date.now() >= dayEndsRef.current) {
      await loadInfo(true);
      return;
    }
    infoRequestRef.current += 1;
    const token = takeTurnstile();
    const motion = {
      startedAt: performance.now() / 1000,
      start: angleRef.current,
      plan: null,
      frame: 0,
      response: null,
    };
    motionRef.current = motion;
    setSpinning(true);
    setOutcome(null);
    // 按钮保留焦点时，空格键会再次触发它。
    document.activeElement?.blur?.();
    // 转动过程就是开奖过程，系统要求减少动态效果时也照常播放，停在服务端选出的结果上。
    animateSpin(motion, discRef.current, () => settleSpin(motion));

    let response;
    try {
      const res = await API.post(
        needTurnstile
          ? `/api/game/wheel/spin?turnstile=${encodeURIComponent(token)}`
          : '/api/game/wheel/spin',
        { free, day: rule.day },
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
        message = t('转得太快了，请稍后再试');
      } else if (status < 500 && error.response.data?.message) {
        // 服务端给出了拒绝原因，例如 IP 已被风控封禁。
        message = error.response.data.message;
      }
      response = { error: message };
    }
    if (motionRef.current !== motion) return;
    motion.response = response;
    // 没有结果时转回原来的角度，不会停在看起来中了奖的格子上。
    const target = response.result
      ? segmentAngle(response.result.segment, rule.segments.length)
      : motion.start;
    planStop(motion, target, performance.now() / 1000);
  }, [
    canSpin,
    free,
    needTurnstile,
    takeTurnstile,
    rule,
    t,
    settleSpin,
    loadInfo,
  ]);

  // 空格键旋转；焦点在按钮上时交给按钮自己处理，免得触发两次。
  useEffect(() => {
    const onKeyDown = (event) => {
      if (event.code !== 'Space' || event.target !== document.body) return;
      event.preventDefault();
      if (!event.repeat) spin();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [spin]);

  if (loading || unavailable) {
    return (
      <div className='games-page'>
        <div className='games-container wheel-page'>
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

  const { segments, spin_cost: cost } = rule;
  const sweep = 360 / segments.length;
  const totalWeight = segments.reduce((sum, item) => sum + item.weight, 0);
  const prizeRanks = [
    ...new Set(
      segments.filter((item) => item.multiplier > 0).map((i) => i.multiplier),
    ),
  ].sort((a, b) => b - a);
  const colorOf = (multiplier) =>
    multiplier > 0
      ? PRIZE_COLORS[prizeRanks.indexOf(multiplier) % PRIZE_COLORS.length]
      : EMPTY_COLOR;
  // 概率公示按奖品合并：同一倍数的几格权重相加，从高到低列出。
  const odds = [...prizeRanks, 0]
    .map((multiplier) => ({
      multiplier,
      weight: segments
        .filter((item) => item.multiplier === multiplier)
        .reduce((sum, item) => sum + item.weight, 0),
    }))
    .filter((item) => item.weight > 0);
  const rate = (rule.payback * 100).toFixed(2);

  const result = !spinning ? outcome?.result : null;
  const hitSegment = result && result.win > 0 ? result.segment : -1;
  let message = t('点击按钮转动转盘，也可以按空格键');
  let messageClass = '';
  if (spinning) {
    message = t('转动中…');
  } else if (outcome?.error) {
    message = outcome.error;
    messageClass = 'is-error';
  } else if (result && result.win === 0) {
    message = t('谢谢参与，下次好运');
  } else if (result && !result.free && result.multiplier === 1) {
    message = t('退回花费 {{win}} {{symbol}}', { win: result.win, symbol });
  } else if (result) {
    message = result.free
      ? t('获得 {{win}} {{symbol}}', { win: result.win, symbol })
      : t('赢得 {{win}} {{symbol}}', { win: result.win, symbol });
    messageClass =
      result.multiplier >= BIG_WIN_MULTIPLIER ? 'is-win is-big-win' : 'is-win';
  }
  const bigWin = messageClass.includes('is-big-win');

  let hint = '';
  if (needTurnstile && turnstileFailed) {
    hint = t('人机验证加载失败，请刷新页面重试');
  } else if (waitingTurnstile) {
    hint = t('免费旋转前需要完成人机验证，需要操作时验证框会出现在按钮下方');
  } else if (!free && balance < cost) {
    hint = t('游戏币余额不足');
  } else if (
    !free &&
    rule.free_used_today < rule.free_spins &&
    rule.ip_free_used_today >= rule.free_spins
  ) {
    hint = t('当前 IP 今天的免费次数已经用完');
  }

  const earned = signed(rule.net_today);

  return (
    <div className='games-page'>
      <div className='games-glow' aria-hidden />
      <div className='games-container wheel-page'>
        <div className='wheel-topbar'>
          <Button
            theme='borderless'
            icon={<ArrowLeft size={16} />}
            onClick={() => navigate('/games')}
          >
            {t('游戏中心')}
          </Button>
          <h1 className='wheel-title'>{t('大转盘')}</h1>
        </div>

        <div className='wheel-layout'>
          <div className={bigWin ? 'games-stage is-big-win' : 'games-stage'}>
            <div className='wheel-frame'>
              <div className='wheel-pointer' aria-hidden />
              <div className='wheel-dial'>
                <div className='wheel-disc' ref={discRef}>
                  {/* 中奖后其余格子变淡，像数据看板里的环形图聚焦一格。 */}
                  <svg
                    viewBox='-100 -100 200 200'
                    className={hitSegment >= 0 ? 'has-hit' : undefined}
                    aria-hidden
                  >
                    <circle r='99' className='wheel-rim' />
                    {segments.map((item, index) => {
                      const color = colorOf(item.multiplier);
                      const center = (index + 0.5) * sweep;
                      return (
                        <g
                          key={index}
                          className={
                            index === hitSegment
                              ? 'wheel-slice is-hit'
                              : 'wheel-slice'
                          }
                        >
                          <path
                            d={segmentPath(index, sweep)}
                            style={{ fill: color.fill }}
                            className='wheel-segment'
                          />
                          <g
                            transform={`rotate(${center})`}
                            style={{ fill: color.text }}
                          >
                            {item.multiplier > 0 ? (
                              <text
                                y={-LABEL_RADIUS}
                                textAnchor='middle'
                                className='wheel-label'
                              >
                                <tspan x='0' className='wheel-label-amount'>
                                  {cost * item.multiplier}
                                </tspan>
                                <tspan
                                  x='0'
                                  dy='13'
                                  className='wheel-label-unit'
                                >
                                  {symbol}
                                </tspan>
                              </text>
                            ) : (
                              <text
                                y={-LABEL_RADIUS + 4}
                                textAnchor='middle'
                                className='wheel-label wheel-label-empty'
                              >
                                {t('谢谢参与')}
                              </text>
                            )}
                          </g>
                        </g>
                      );
                    })}
                    {segments.map((_, index) => {
                      const [x, y] = polar(TICK_RADIUS, index * sweep);
                      return (
                        <circle
                          key={index}
                          cx={x.toFixed(3)}
                          cy={y.toFixed(3)}
                          r='1.6'
                          className='wheel-tick'
                        />
                      );
                    })}
                  </svg>
                </div>
                {/* 中心不随转盘转动。 */}
                <div className='wheel-hub' aria-hidden>
                  {logoFailed ? (
                    <span className='wheel-hub-symbol'>{symbol || '★'}</span>
                  ) : (
                    <img
                      src={getLogo()}
                      alt=''
                      onError={() => setLogoFailed(true)}
                    />
                  )}
                </div>
              </div>
            </div>

            <div
              className={`games-stage-message ${messageClass}`}
              aria-live='polite'
            >
              <span>{message}</span>
            </div>

            <div className='wheel-controls'>
              <button
                type='button'
                className={
                  free ? 'games-play-button is-free' : 'games-play-button'
                }
                disabled={!canSpin}
                onClick={spin}
              >
                {spinning && t('转动中…')}
                {!spinning && free && t('免费转一次')}
                {!spinning &&
                  !free &&
                  t('花 {{cost}} {{symbol}} 转一次', { cost, symbol })}
              </button>
              <div className='games-stage-counter'>
                {t('今日已转 {{plays}} 次', { plays: rule.plays_today })}
                {rule.free_spins > 0 &&
                  ` · ${t('免费剩余 {{count}} 次', { count: freeLeft })}`}
              </div>
              {hint && <div className='games-stage-hint'>{hint}</div>}
              {/* 平时不可见，Cloudflare 需要玩家操作时才在这里展开。 */}
              {needTurnstile && (
                <div className='wheel-turnstile'>{turnstileWidget}</div>
              )}
            </div>
          </div>

          <div className='wheel-side'>
            <div className='wheel-box'>
              <div className='wheel-box-label'>{t('游戏币余额')}</div>
              <div className='wheel-box-value'>
                {balance}
                <span>{symbol}</span>
              </div>
            </div>
            <div className='wheel-box'>
              <div className='wheel-box-label'>{t('今日输赢')}</div>
              <div className='wheel-box-value'>
                {earned}
                <span>{symbol}</span>
              </div>
              <div className='wheel-box-note'>
                {t('免费旋转的奖品加上付费旋转的输赢')}
              </div>
            </div>
            <div className='wheel-box'>
              <div className='wheel-box-label'>{t('概率公示')}</div>
              <table className='wheel-odds'>
                <thead>
                  <tr>
                    <th>{t('奖品')}</th>
                    <th>{t('概率')}</th>
                  </tr>
                </thead>
                <tbody>
                  {odds.map((item) => (
                    <tr key={item.multiplier}>
                      <td>
                        <span
                          className='wheel-odds-swatch'
                          style={{ background: colorOf(item.multiplier).fill }}
                        />
                        {item.multiplier > 0
                          ? `${cost * item.multiplier} ${symbol}`
                          : t('谢谢参与')}
                      </td>
                      <td>{formatPercent(item.weight, totalWeight)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <ul className='wheel-rules'>
                {rule.free_spins > 0 && (
                  <li>
                    {t('每天免费 {{count}} 次，同一 IP 下所有账号合计', {
                      count: rule.free_spins,
                    })}
                  </li>
                )}
                <li>
                  {t('免费次数用完后每次花 {{cost}} {{symbol}}，不限次数', {
                    cost,
                    symbol,
                  })}
                </li>
                <li>
                  {t('转到多少就发多少，奖品全额到账，不占技巧与人机奖励上限')}
                </li>
                <li>
                  {t(
                    '奖品 = 每次花费 × 格子的倍数，免费旋转同样按每次花费计算；每格被选中的概率以上表为准，与格子画得多大无关',
                  )}
                </li>
                <li>{t('付费旋转的理论返还率 {{rate}}%', { rate })}</li>
                {playTurnstile.required && rule.free_spins > 0 && (
                  <li>{t('免费旋转需要先通过人机验证')}</li>
                )}
              </ul>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Wheel;
