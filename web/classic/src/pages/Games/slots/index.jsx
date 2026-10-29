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
import { ArrowLeft, Bot, BrainCircuit, Cpu, KeyRound, Zap } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../../helpers';
import { percent } from '../common/api';
import { SideBox } from '../common/SideBox';
import { WagerStats, playWagerFeedback } from '../common/wager';
import { AutoPlayControls, useAutoPlay } from '../common/autoPlay';
import { SoundToggle } from '../common/effects';
import { playSound } from '../common/sound';
import BetInput from '../common/BetInput';
import '../games.css';
import './slots.css';

// 符号标识与服务端 pkg/slots 一致，沿用水果机的叫法；页面上画成站点里常见的 AI 与接口元素：
// 7 是 AI，钻石是算力芯片，铃铛是模型，葡萄是机器人，橙子是密钥，柠檬是请求，樱桃是 Token 的 T(与数据看板的 Token 图标一致)。
const SYMBOL_GLYPHS = {
  seven: 'AI',
  diamond: <Cpu />,
  bell: <BrainCircuit />,
  grape: <Bot />,
  orange: <KeyRound />,
  lemon: <Zap />,
  cherry: 'T',
};

// 转轮动画。位置按格计，数值变小时转轮向下滚动，时间以秒计。点击后转轮先匀速转动；服务端给出停点后，
// 每个转轮等到离停点正好 STOP_DISTANCE 格时开始匀减速，减速的初速度与匀速一致，看不出衔接。
const SPIN_SPEED = 30;
const STOP_DISTANCE = 6;
const STOP_DURATION = (2 * STOP_DISTANCE) / SPIN_SPEED;
// 点击后至少转这么久第一个转轮才开始停，相邻两个转轮停下至少间隔 STOP_GAP。
const MIN_SPIN = 0.5;
const STOP_GAP = 0.3;
const BOUNCE_DURATION = 0.16;
const BOUNCE_DEPTH = 0.1;
const MAX_BLUR = 2;
// 派彩达到下注的这么多倍时按大奖展示。
const BIG_WIN_MULTIPLIER = 10;

const ILLUSTRATION_SIZE = { width: 150, height: 150 };

const mod = (value, period) => ((value % period) + period) % period;

// 转轮在 now 时刻的位置与速度：还没安排停下时一直匀速转动，之后依次是等待、匀减速和停下时的回弹。
function reelMotion(reel, startedAt, now) {
  const { plan } = reel;
  if (!plan || now < plan.decelAt) {
    return {
      position: reel.start - SPIN_SPEED * (now - startedAt),
      speed: SPIN_SPEED,
    };
  }
  const elapsed = now - plan.decelAt;
  if (elapsed < STOP_DURATION) {
    return {
      position:
        plan.decelFrom -
        (SPIN_SPEED * elapsed -
          (SPIN_SPEED * elapsed * elapsed) / (2 * STOP_DURATION)),
      speed: SPIN_SPEED * (1 - elapsed / STOP_DURATION),
    };
  }
  const settle = elapsed - STOP_DURATION;
  if (settle < BOUNCE_DURATION) {
    return {
      position:
        plan.final -
        BOUNCE_DEPTH * Math.sin((Math.PI * settle) / BOUNCE_DURATION),
      speed: 0,
    };
  }
  return { position: plan.final, speed: 0 };
}

// 收到结果后安排每个转轮停在 stops 上的时刻，保证从左到右依次停下。
function planStops(motion, stops, now) {
  let previousFinish = -Infinity;
  motion.reels.forEach((reel, index) => {
    const planAt = Math.max(
      now,
      motion.startedAt + MIN_SPIN,
      previousFinish + STOP_GAP - STOP_DURATION,
    );
    const position = reel.start - SPIN_SPEED * (planAt - motion.startedAt);
    // 再匀速转过 wait 格，正好离停点 STOP_DISTANCE 格。
    const wait = mod(position - stops[index] - STOP_DISTANCE, reel.size);
    const decelAt = planAt + wait / SPIN_SPEED;
    const decelFrom = position - wait;
    reel.plan = { decelAt, decelFrom, final: decelFrom - STOP_DISTANCE };
    previousFinish = decelAt + STOP_DURATION;
  });
}

// 每个转轮停下的那一刻响一声，最后一个转轮停下时声音更重。返回定时器，换下一次旋转或离开页面时清掉。
function scheduleStopSounds(motion, now) {
  return motion.reels.map((reel, index) =>
    setTimeout(
      () =>
        playSound(index === motion.reels.length - 1 ? 'drop' : 'place', {
          pitch: 1 + index * 0.1,
        }),
      Math.max((reel.plan.decelAt + STOP_DURATION - now) * 1000, 0),
    ),
  );
}

// 把转轮画在 position：条带上重复三遍符号，中线停在中间那一遍的 position 格上。
function renderReel(strip, position, speed, size) {
  if (!strip) return;
  const offset = mod(position, size) + size - 1;
  strip.style.transform = `translate3d(0, ${(-offset / (3 * size)) * 100}%, 0)`;
  strip.style.filter =
    speed > 0 ? `blur(${(speed / SPIN_SPEED) * MAX_BLUR}px)` : '';
}

// 逐帧驱动一次旋转，所有转轮停稳后调用 onSettled。
function animateSpin(motion, strips, onSettled) {
  const step = () => {
    const now = performance.now() / 1000;
    let settled = true;
    motion.reels.forEach((reel, index) => {
      const { position, speed } = reelMotion(reel, motion.startedAt, now);
      renderReel(strips[index], position, speed, reel.size);
      if (
        !reel.plan ||
        now < reel.plan.decelAt + STOP_DURATION + BOUNCE_DURATION
      ) {
        settled = false;
      }
    });
    if (settled) {
      onSettled();
      return;
    }
    motion.frame = requestAnimationFrame(step);
  };
  motion.frame = requestAnimationFrame(step);
}

const Slots = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const stripRefs = useRef([]);
  // 进行中的一次旋转：动画状态与服务端结果。渲染循环直接读写它，不经过 React 状态。
  const motionRef = useRef(null);
  // 转轮停下时所在的整格位置，下一次旋转从这里开始。
  const positionsRef = useRef([]);
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState('');
  const [rule, setRule] = useState(null);
  const [symbol, setSymbol] = useState('');
  const [balance, setBalance] = useState(0);
  const [bet, setBet] = useState(1);
  const [spinning, setSpinning] = useState(false);
  // 上一次旋转的结果 { result } 或失败原因 { error }。
  const [outcome, setOutcome] = useState(null);
  const auto = useAutoPlay();

  // refresh 为真时是旋转没有结果后重新读取：这时读不到就保留页面上的内容，失败原因已经显示在转轮下方。
  const loadInfo = useCallback(
    async (refresh = false) => {
      try {
        const res = await API.get('/api/game/slots');
        if (!res.data.success) {
          if (!refresh) setUnavailable(res.data.message);
          return;
        }
        const slots = res.data.data;
        // 第一次加载时转轮停在随机的位置，只为了好看。
        if (positionsRef.current.length === 0) {
          positionsRef.current = slots.reels.map((strip) =>
            Math.floor(Math.random() * strip.length),
          );
        }
        setRule(slots);
        setSymbol(slots.symbol || '');
        setBalance(slots.balance || 0);
        setBet((previous) => Math.min(previous, slots.max_bet));
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

  // 转轮条带渲染出来后、浏览器绘制之前画到当前位置，免得先闪一下条带的开头。
  useLayoutEffect(() => {
    if (!rule || motionRef.current) return;
    rule.reels.forEach((strip, index) => {
      renderReel(
        stripRefs.current[index],
        positionsRef.current[index],
        0,
        strip.length,
      );
    });
  }, [rule]);

  // 离开页面时停掉动画；还没回来的结果不再处理，余额以服务端为准。
  useEffect(
    () => () => {
      if (motionRef.current) {
        cancelAnimationFrame(motionRef.current.frame);
        motionRef.current.sounds?.forEach(clearTimeout);
      }
      motionRef.current = null;
    },
    [],
  );

  // 所有转轮停稳后才公布结果、更新余额，免得转轮还在转就剧透。
  const settleSpin = useCallback(
    (motion) => {
      if (motionRef.current !== motion) return;
      motionRef.current = null;
      positionsRef.current = motion.reels.map((reel) =>
        mod(Math.round(reel.plan.final), reel.size),
      );
      motion.reels.forEach((reel, index) => {
        renderReel(
          stripRefs.current[index],
          positionsRef.current[index],
          0,
          reel.size,
        );
      });
      const { result } = motion.response;
      if (result) {
        setBalance(result.balance);
        setRule((previous) => ({
          ...previous,
          net_today: result.net_today,
          plays_today: result.plays_today,
          best_win: Math.max(previous.best_win || 0, result.win),
        }));
      } else {
        // 旋转被拒绝或请求没有得到答复：页面上的余额与今日输赢可能已经过时(请求也可能已经结算)，重新读取。
        loadInfo(true);
      }
      setOutcome(motion.response);
      setSpinning(false);
      if (result) {
        playWagerFeedback({ ...result, bet: motion.bet });
        auto.after(true, {
          bigWin: result.multiplier >= BIG_WIN_MULTIPLIER,
        });
      } else {
        auto.after(false);
      }
    },
    [loadInfo, auto.after],
  );

  const spin = useCallback(async () => {
    if (!rule || motionRef.current) return;
    const motion = {
      startedAt: performance.now() / 1000,
      reels: rule.reels.map((strip, index) => ({
        start: positionsRef.current[index],
        size: strip.length,
        plan: null,
      })),
      frame: 0,
      response: null,
      bet,
    };
    motionRef.current = motion;
    playSound('start');
    setSpinning(true);
    setOutcome(null);
    // 转动过程就是开奖过程，系统要求减少动态效果时也照常播放，停在服务端选出的结果上。
    animateSpin(motion, stripRefs.current, () => settleSpin(motion));

    let response;
    try {
      const res = await API.post(
        '/api/game/slots/spin',
        { bet },
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
    // 没有结果时转轮转回原来的位置，不会停在一排看起来像中奖的图案上。
    const now = performance.now() / 1000;
    planStops(
      motion,
      response.result?.stops || motion.reels.map((reel) => reel.start),
      now,
    );
    motion.sounds = scheduleStopSounds(motion, now);
  }, [rule, bet, t, settleSpin]);

  const canSpin =
    !spinning && bet >= 0.01 && bet <= balance && bet <= rule?.max_bet;
  auto.bind(() => (canSpin ? spin() : auto.stop()));

  // 空格键旋转；焦点在按钮上时交给按钮自己处理，免得触发两次。
  useEffect(() => {
    const onKeyDown = (event) => {
      if (event.code !== 'Space' || event.target !== document.body) return;
      event.preventDefault();
      if (!event.repeat && canSpin) spin();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [canSpin, spin]);

  if (loading || unavailable) {
    return (
      <div className='games-page'>
        <div className='games-container slots-page'>
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

  const paytable = rule.paytable;
  const jackpot = paytable.find((pay) => pay.symbol === 'seven');
  const result = !spinning ? outcome?.result : null;
  let message = t('选好下注后点击旋转，也可以按空格键');
  let messageClass = '';
  if (spinning) {
    message = t('转动中…');
  } else if (outcome?.error) {
    message = outcome.error;
    messageClass = 'is-error';
  } else if (result && result.win === 0) {
    message = t('没有中奖，再试一次');
  } else if (result) {
    message = t('赢得 {{win}} {{symbol}}', { win: result.win, symbol });
    messageClass =
      result.multiplier >= BIG_WIN_MULTIPLIER ? 'is-win is-big-win' : 'is-win';
  }
  const hint = bet > balance ? t('游戏币余额不足') : '';
  const hitReels = result?.matched || 0;
  const bigWin = messageClass.includes('is-big-win');

  return (
    <div className='games-page'>
      <div className='games-glow' aria-hidden />
      <div className='games-container slots-page'>
        <div className='slots-topbar'>
          <Button
            theme='borderless'
            icon={<ArrowLeft size={16} />}
            onClick={() => navigate('/games')}
          >
            {t('游戏中心')}
          </Button>
          <h1 className='slots-title'>{t('老虎机')}</h1>
          <SoundToggle />
        </div>

        <div className='slots-layout'>
          <div className={bigWin ? 'games-stage is-big-win' : 'games-stage'}>
            {/* 头奖提示：三个 AI 连成一线的倍数。 */}
            {jackpot && (
              <div className='slots-jackpot' aria-hidden>
                {[0, 1, 2].map((index) => (
                  <span key={index} className='slots-tile symbol-seven'>
                    {SYMBOL_GLYPHS.seven}
                  </span>
                ))}
                <span className='slots-jackpot-multiplier'>
                  {`×${jackpot.three}`}
                </span>
              </div>
            )}

            <div className='slots-window'>
              {rule.reels.map((strip, index) => (
                <div
                  key={index}
                  className={
                    index < hitReels ? 'slots-reel is-hit' : 'slots-reel'
                  }
                >
                  <div
                    className='slots-strip'
                    ref={(element) => {
                      stripRefs.current[index] = element;
                    }}
                  >
                    {[0, 1, 2].map((copy) =>
                      strip.map((key, stop) => (
                        <div
                          key={`${copy}-${stop}`}
                          className={`slots-symbol symbol-${key}`}
                        >
                          <span className='slots-tile'>
                            {SYMBOL_GLYPHS[key] || '?'}
                          </span>
                        </div>
                      )),
                    )}
                  </div>
                </div>
              ))}
              <div className='slots-payline' aria-hidden />
            </div>

            <div
              className={`games-stage-message ${messageClass}`}
              aria-live='polite'
            >
              <span>{message}</span>
            </div>

            <div className='slots-controls'>
              <BetInput
                value={bet}
                min={rule.min_bet || 0.01}
                max={rule.max_bet}
                symbol={symbol}
                disabled={spinning}
                onChange={setBet}
              />
              <button
                type='button'
                className='games-play-button'
                disabled={!canSpin || auto.active}
                onClick={spin}
              >
                {spinning ? t('转动中…') : t('旋转')}
              </button>
              <AutoPlayControls auto={auto} disabled={!canSpin} />
              {hint && <div className='games-stage-hint'>{hint}</div>}
            </div>
          </div>

          <div className='slots-side'>
            <SideBox label={t('游戏币余额')} value={balance} unit={symbol} />
            <WagerStats info={rule} symbol={symbol} />
            <div className='slots-box'>
              <div className='slots-box-label'>{t('赔付表')}</div>
              <table className='slots-paytable'>
                <thead>
                  <tr>
                    <th>{t('符号')}</th>
                    <th>{t('三个相同')}</th>
                    <th>{t('前两个相同')}</th>
                  </tr>
                </thead>
                <tbody>
                  {paytable.map((pay) => (
                    <tr key={pay.symbol}>
                      <td>
                        <span
                          className={`slots-tile slots-pay-symbol symbol-${pay.symbol}`}
                        >
                          {SYMBOL_GLYPHS[pay.symbol] || pay.symbol}
                        </span>
                      </td>
                      <td>{pay.three ? `×${pay.three}` : '-'}</td>
                      <td>{pay.two ? `×${pay.two}` : '-'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <ul className='slots-rules'>
                <li>
                  {t(
                    '只看中线，从最左边的转轮起算；派彩 = 下注 × 倍数，已包含退回的下注',
                  )}
                </li>
                <li>
                  {t('理论返还率 {{rate}}，玩家长期是输的', {
                    rate: percent(rule.payback),
                  })}
                </li>
                <li>
                  {t('单次最多下注 {{max}} {{symbol}}，不限次数', {
                    max: rule.max_bet,
                    symbol,
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

export default Slots;
