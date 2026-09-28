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
  forwardRef,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';

// 欧式单零轮盘上号码的排列(从正上方起顺时针)，只用来画轮盘；开出哪个号码由服务端决定。
const WHEEL_ORDER = [
  0, 32, 15, 19, 4, 21, 2, 25, 17, 34, 6, 27, 13, 36, 11, 30, 8, 23, 10, 5, 24,
  16, 33, 1, 20, 14, 31, 9, 22, 18, 29, 7, 28, 12, 35, 3, 26,
];
const POCKET_SWEEP = 360 / WHEEL_ORDER.length;

// 轮盘画在以圆心为原点、边长 200 的坐标系里：最外圈是小球滚动的轨道，里面一圈是号码格，小球停在号码格靠内的一侧。
const RIM_RADIUS = 99;
const TRACK_RADIUS = 91;
const POCKET_OUTER = 84;
const POCKET_INNER = 54;
const LABEL_RADIUS = 76;
const REST_RADIUS = 62;
const BALL_RADIUS = 4.2;

// 旋转动画。角度以度计、顺时针为正，时间以秒计。点击后轮盘顺时针匀速转、小球逆时针沿轨道滚(刚开始的 LAUNCH 秒从号码格
// 回到轨道)；收到开出的号码后，先转够 MIN_SPIN 秒，再在 STOP_DURATION 秒内一起减速停下，减速进行到 DROP_FROM 时小球开始
// 落进号码格。
const WHEEL_SPEED = 90;
const BALL_SPEED = 480;
const LAUNCH = 0.3;
const MIN_SPIN = 0.5;
const STOP_DURATION = 3;
const DROP_FROM = 0.6;

const mod = (value, period) => ((value % period) + period) % period;

// 从正上方起顺时针转过 degrees 度、离圆心 radius 处的坐标。
function polar(radius, degrees) {
  const radians = (degrees * Math.PI) / 180;
  return [
    (radius * Math.sin(radians)).toFixed(3),
    (-radius * Math.cos(radians)).toFixed(3),
  ];
}

// 第 index 个号码格的扇环。
function pocketPath(index) {
  const from = index * POCKET_SWEEP;
  const to = from + POCKET_SWEEP;
  const [x1, y1] = polar(POCKET_OUTER, from);
  const [x2, y2] = polar(POCKET_OUTER, to);
  const [x3, y3] = polar(POCKET_INNER, to);
  const [x4, y4] = polar(POCKET_INNER, from);
  return `M${x1} ${y1}A${POCKET_OUTER} ${POCKET_OUTER} 0 0 1 ${x2} ${y2}L${x3} ${y3}A${POCKET_INNER} ${POCKET_INNER} 0 0 0 ${x4} ${y4}Z`;
}

// 旋转进行到 now 时轮盘的角度、小球的角度与小球离圆心的距离。还没安排停下时两者匀速转动；安排停下后轮盘匀减速，
// 小球的位移是一条三次曲线：初速度与匀速滚动时相同、停下时速度为 0，总位移由 planLanding 算好，正好停在开出的号码格上。
function spinPose(motion, now) {
  const { start, plan } = motion;
  if (!plan || now < plan.decelAt) {
    const elapsed = now - motion.startedAt;
    const launch = Math.min(elapsed / LAUNCH, 1);
    return {
      wheel: start.wheel + WHEEL_SPEED * elapsed,
      ball: start.ball - BALL_SPEED * elapsed,
      radius: start.radius + (TRACK_RADIUS - start.radius) * launch,
    };
  }
  const progress = Math.min((now - plan.decelAt) / STOP_DURATION, 1);
  const drop = Math.max((progress - DROP_FROM) / (1 - DROP_FROM), 0);
  return {
    wheel:
      plan.from.wheel +
      WHEEL_SPEED * STOP_DURATION * (progress - (progress * progress) / 2),
    ball:
      plan.from.ball -
      (BALL_SPEED * STOP_DURATION * progress +
        plan.square * progress * progress +
        plan.cube * progress * progress * progress),
    radius:
      TRACK_RADIUS -
      (TRACK_RADIUS - REST_RADIUS) * drop * drop * (3 - 2 * drop),
  };
}

// 收到开出的号码后安排停下：小球最后停在第 pocket 个号码格的正中。匀减速时小球会滚过 natural 度，取与它相差不到半圈、
// 又正好停在目标上的位移；这个范围内三次曲线的速度一直不小于 0，小球不会倒着滚。
function planLanding(motion, pocket, now) {
  const decelAt = Math.max(now, motion.startedAt + MIN_SPIN);
  const from = spinPose({ ...motion, plan: null }, decelAt);
  const wheelEnd = from.wheel + (WHEEL_SPEED * STOP_DURATION) / 2;
  const target = wheelEnd + (pocket + 0.5) * POCKET_SWEEP;
  const natural = (BALL_SPEED * STOP_DURATION) / 2;
  const travel = natural + mod(from.ball - target - natural + 180, 360) - 180;
  motion.plan = {
    decelAt,
    from,
    square: 3 * travel - 2 * BALL_SPEED * STOP_DURATION,
    cube: BALL_SPEED * STOP_DURATION - 2 * travel,
    endsAt: decelAt + STOP_DURATION,
  };
}

function renderPose(disc, ball, pose) {
  disc?.setAttribute('transform', `rotate(${pose.wheel.toFixed(2)})`);
  ball?.setAttribute(
    'transform',
    `rotate(${pose.ball.toFixed(2)}) translate(0 ${(-pose.radius).toFixed(2)})`,
  );
}

const reducedMotion = () =>
  !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;

// 轮盘与小球。页面通过 ref 调用 start() 开始旋转，land(number) 让小球落进开出的号码格(返回停稳时完成的 Promise)，
// stop() 在没有结果时停下并收起小球。动画逐帧直接改 SVG 的 transform，不经过 React 状态。
// colors[n] 是号码 n 的颜色，number 是已经公布的号码(高亮这一格并显示在中心)，没有时为 null。
const RouletteWheel = forwardRef(({ colors, number, label }, ref) => {
  const discRef = useRef(null);
  const ballRef = useRef(null);
  // 轮盘与小球停下时的姿态，下一次旋转从这里开始。
  const poseRef = useRef({ wheel: 0, ball: 0, radius: TRACK_RADIUS });
  // 进行中的一次旋转：开始的时刻与姿态、停下的安排、动画帧与停稳时的回调。
  const motionRef = useRef(null);
  // 第一次旋转之前不显示小球；没有结果时收起。
  const [ballShown, setBallShown] = useState(false);

  // 渲染出来后、浏览器绘制之前摆好初始姿态。
  useLayoutEffect(() => {
    renderPose(discRef.current, ballRef.current, poseRef.current);
  }, []);

  // 离开页面时停掉动画；等着停稳的 Promise 不再完成。
  useEffect(
    () => () => {
      if (motionRef.current) cancelAnimationFrame(motionRef.current.frame);
      motionRef.current = null;
    },
    [],
  );

  useImperativeHandle(
    ref,
    () => ({
      start() {
        if (motionRef.current) cancelAnimationFrame(motionRef.current.frame);
        const motion = {
          startedAt: performance.now() / 1000,
          start: { ...poseRef.current },
          plan: null,
          frame: 0,
          animated: !reducedMotion(),
          onLanded: null,
        };
        motionRef.current = motion;
        // 不播动画时等结果出来再放小球，免得它停在上一次的号码格里像是已经开出。
        setBallShown(motion.animated);
        if (!motion.animated) return;
        const step = () => {
          const now = performance.now() / 1000;
          if (motion.plan && now >= motion.plan.endsAt) {
            const pose = spinPose(motion, motion.plan.endsAt);
            poseRef.current = {
              wheel: mod(pose.wheel, 360),
              ball: mod(pose.ball, 360),
              radius: pose.radius,
            };
            renderPose(discRef.current, ballRef.current, poseRef.current);
            motionRef.current = null;
            motion.onLanded();
            return;
          }
          renderPose(discRef.current, ballRef.current, spinPose(motion, now));
          motion.frame = requestAnimationFrame(step);
        };
        motion.frame = requestAnimationFrame(step);
      },
      land(result) {
        const motion = motionRef.current;
        const pocket = WHEEL_ORDER.indexOf(result);
        if (!motion || !motion.animated) {
          // 不播动画：小球直接放进开出的号码格。
          if (motion) cancelAnimationFrame(motion.frame);
          motionRef.current = null;
          const wheel = poseRef.current.wheel;
          poseRef.current = {
            wheel,
            ball: mod(wheel + (pocket + 0.5) * POCKET_SWEEP, 360),
            radius: REST_RADIUS,
          };
          renderPose(discRef.current, ballRef.current, poseRef.current);
          setBallShown(true);
          return Promise.resolve();
        }
        planLanding(motion, pocket, performance.now() / 1000);
        return new Promise((resolve) => {
          motion.onLanded = resolve;
        });
      },
      stop() {
        const motion = motionRef.current;
        motionRef.current = null;
        setBallShown(false);
        if (!motion) return;
        cancelAnimationFrame(motion.frame);
        // 轮盘停在当前角度，小球收起，下次从轨道上开始滚。
        const pose = motion.animated
          ? spinPose(motion, performance.now() / 1000)
          : motion.start;
        poseRef.current = {
          wheel: mod(pose.wheel, 360),
          ball: mod(pose.ball, 360),
          radius: TRACK_RADIUS,
        };
        renderPose(discRef.current, ballRef.current, poseRef.current);
      },
    }),
    [],
  );

  const hit = number === null || number === undefined ? -1 : number;
  return (
    <div className='roulette-wheel'>
      <svg
        viewBox='-100 -100 200 200'
        className={hit >= 0 ? 'has-hit' : undefined}
        role='img'
        aria-label={label}
      >
        <circle r={RIM_RADIUS} className='roulette-rim' />
        <circle r={TRACK_RADIUS} className='roulette-track' />
        <g ref={discRef}>
          {WHEEL_ORDER.map((value, index) => (
            <g
              key={value}
              className={`roulette-pocket is-${colors[value]}${value === hit ? ' is-hit' : ''}`}
            >
              <path d={pocketPath(index)} />
              <text
                transform={`rotate(${((index + 0.5) * POCKET_SWEEP).toFixed(3)}) translate(0 ${-LABEL_RADIUS})`}
                textAnchor='middle'
                dominantBaseline='central'
              >
                {value}
              </text>
            </g>
          ))}
          <circle r={POCKET_INNER} className='roulette-cone' />
          <circle r={POCKET_INNER - 12} className='roulette-hub' />
        </g>
        <g
          ref={ballRef}
          className={ballShown ? 'roulette-ball' : 'roulette-ball is-hidden'}
        >
          <circle r={BALL_RADIUS} />
        </g>
      </svg>
      {hit >= 0 && (
        <div className={`roulette-result is-${colors[hit]}`} aria-hidden>
          {hit}
        </div>
      )}
    </div>
  );
});

RouletteWheel.displayName = 'RouletteWheel';

export default RouletteWheel;
