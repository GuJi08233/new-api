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
import { Button, Tooltip } from '@douyinfe/semi-ui';
import { Volume2, VolumeX } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { playSound, useGameSound } from './sound';

// 游戏页共用的反馈效果：顶栏的音效开关、获胜时的彩带与盘面上飘起的得分字。彩带和飘字只是装饰，系统开启了减少动态效果时
// 不播放；表现游戏过程的动画(转盘、发牌、落子)不在这里。

const reducedMotion = () =>
  window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;

// 顶栏里的音效开关，所有游戏共用一个设置。
export const SoundToggle = () => {
  const { t } = useTranslation();
  const [on, toggle] = useGameSound();
  const label = on ? t('关闭音效') : t('打开音效');
  return (
    <Tooltip content={label}>
      <Button
        className='game-sound-toggle'
        theme='borderless'
        type='tertiary'
        aria-label={label}
        aria-pressed={on}
        icon={on ? <Volume2 size={16} /> : <VolumeX size={16} />}
        onClick={toggle}
      />
    </Tooltip>
  );
};

const CONFETTI_COLORS = [
  '#f59e0b',
  '#10b981',
  '#3b82f6',
  '#ec4899',
  '#8b5cf6',
  '#f43f5e',
];

// 从屏幕上方撒一阵彩带，约两秒后自己清理。strength 是彩带的数量倍数，大奖时撒得多一些。
export function celebrate(strength = 1) {
  if (reducedMotion() || typeof document === 'undefined') return;
  const canvas = document.createElement('canvas');
  canvas.className = 'game-confetti';
  canvas.setAttribute('aria-hidden', 'true');
  document.body.appendChild(canvas);
  const ratio = window.devicePixelRatio || 1;
  const width = window.innerWidth;
  const height = window.innerHeight;
  canvas.width = width * ratio;
  canvas.height = height * ratio;
  const ctx = canvas.getContext('2d');
  ctx.scale(ratio, ratio);
  const count = Math.round(110 * strength);
  const pieces = Array.from({ length: count }, () => ({
    x: width * (0.15 + Math.random() * 0.7),
    y: -20 - Math.random() * height * 0.25,
    vx: (Math.random() - 0.5) * 6,
    vy: 2 + Math.random() * 4,
    size: 5 + Math.random() * 6,
    angle: Math.random() * Math.PI,
    spin: (Math.random() - 0.5) * 0.3,
    color: CONFETTI_COLORS[Math.floor(Math.random() * CONFETTI_COLORS.length)],
  }));
  const started = performance.now();
  const frame = (now) => {
    const elapsed = now - started;
    ctx.clearRect(0, 0, width, height);
    ctx.globalAlpha = Math.max(0, Math.min(1, (2200 - elapsed) / 600));
    for (const piece of pieces) {
      piece.x += piece.vx;
      piece.y += piece.vy;
      piece.vy += 0.08;
      piece.vx *= 0.99;
      piece.angle += piece.spin;
      ctx.save();
      ctx.translate(piece.x, piece.y);
      ctx.rotate(piece.angle);
      ctx.fillStyle = piece.color;
      ctx.fillRect(
        -piece.size / 2,
        -piece.size / 4,
        piece.size,
        piece.size / 2,
      );
      ctx.restore();
    }
    if (elapsed < 2200) requestAnimationFrame(frame);
    else canvas.remove();
  };
  requestAnimationFrame(frame);
}

// 盘面上飘起来的得分字(+128、四消！)。add(text, { x, y, kind }) 在相对容器的百分比位置 x、y 飘出一行字，kind 换颜色
// (gain 绿、gold 金、bad 红)；约一秒后自动移除。容器要是 position: relative。
export function useFloaters() {
  const [items, setItems] = useState([]);
  const idRef = useRef(0);
  const timers = useRef(new Set());
  useEffect(
    () => () => {
      timers.current.forEach(clearTimeout);
    },
    [],
  );
  const add = useCallback((text, { x = 50, y = 40, kind = 'gain' } = {}) => {
    if (reducedMotion()) return;
    idRef.current += 1;
    const id = idRef.current;
    setItems((previous) => [...previous.slice(-6), { id, text, x, y, kind }]);
    const timer = setTimeout(() => {
      timers.current.delete(timer);
      setItems((previous) => previous.filter((item) => item.id !== id));
    }, 1000);
    timers.current.add(timer);
  }, []);
  return [items, add];
}

export const Floaters = ({ items }) => (
  <div className='game-floaters' aria-hidden>
    {items.map((item) => (
      <span
        key={item.id}
        className={`game-floater is-${item.kind}`}
        style={{ left: `${item.x}%`, top: `${item.y}%` }}
      >
        {item.text}
      </span>
    ))}
  </div>
);

// 对局结果的音效与彩带：同一局(id 不变)从没有结果变成 win、loss、draw 时播放一次。刷新页面看到已经结束的一局、换了一局
// 都不播放。big 为真时(押注对局赢了)彩带撒得多一些。
export function useOutcomeFeedback(id, outcome, { big = false } = {}) {
  const seen = useRef({ id: null, outcome: null });
  useEffect(() => {
    const previous = seen.current;
    seen.current = { id, outcome };
    if (previous.id !== id || previous.outcome || !outcome) return;
    if (outcome === 'win') {
      playSound(big ? 'bigWin' : 'win');
      celebrate(big ? 1.5 : 1);
    } else if (outcome === 'loss') {
      playSound('lose');
    } else {
      playSound('clear');
    }
  }, [id, outcome, big]);
}
