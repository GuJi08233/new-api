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

import React from 'react';
import { ink, pinePath, ridge, scatter, stroke } from './ink';

// 五章的水墨底图：青岚山、赤霞原、北冥寒渊、万妖林、九幽。画布 800×520，铺满容器(多出的部分裁掉)。每章的色调来自
// wendao.css 里 .wd-scene.is-chN 的 --wd-tint、--wd-tint-2。

const W = 800;
const H = 520;

const Sky = ({ id }) => (
  <>
    <defs>
      <linearGradient id={`${id}-sky`} x1='0' y1='0' x2='0' y2='1'>
        <stop offset='0' style={{ stopColor: 'var(--wd-paper)' }} />
        <stop offset='1' style={{ stopColor: 'var(--wd-paper-2)' }} />
      </linearGradient>
      <linearGradient id='wd-mist' x1='0' y1='0' x2='0' y2='1'>
        <stop
          offset='0'
          style={{ stopColor: 'var(--wd-mist)', stopOpacity: 0 }}
        />
        <stop
          offset='0.5'
          style={{ stopColor: 'var(--wd-mist)', stopOpacity: 1 }}
        />
        <stop
          offset='1'
          style={{ stopColor: 'var(--wd-mist)', stopOpacity: 0 }}
        />
      </linearGradient>
      <linearGradient id={`${id}-wash`} x1='0' y1='0' x2='0' y2='1'>
        <stop
          offset='0'
          style={{ stopColor: 'var(--wd-tint)', stopOpacity: 0.22 }}
        />
        <stop
          offset='1'
          style={{ stopColor: 'var(--wd-tint)', stopOpacity: 0 }}
        />
      </linearGradient>
    </defs>
    <rect width={W} height={H} fill={`url(#${id}-sky)`} />
    <rect width={W} height={H * 0.6} fill={`url(#${id}-wash)`} />
  </>
);

// 一道雾：上下两头淡出的白带。
const Mist = ({ y, h = 26, opacity = 1 }) => (
  <rect
    x='0'
    y={y - h / 2}
    width={W}
    height={h * 2}
    fill='url(#wd-mist)'
    opacity={opacity}
  />
);

const Qinglan = () => (
  <>
    <Sky id='wd1' />
    <circle cx='590' cy='96' r='30' style={ink('red', 0.78)} />
    <path
      d={ridge({ y: 210, amp: 120, seed: 40, sharp: 1.2 })}
      style={ink('tint', 0.35)}
    />
    <Mist y={200} h={34} />
    <path
      d={ridge({ y: 300, amp: 110, seed: 260, sharp: 0.8 })}
      style={ink('mtn-2', 0.85)}
    />
    <Mist y={300} h={28} opacity={0.9} />
    <path d={ridge({ y: 400, amp: 70, seed: 520 })} style={ink('mtn-3', 0.9)} />
    {/* 雾隐谷的瀑布 */}
    <path
      d='M470 150 C468 200 474 250 470 300'
      style={stroke('ink-3', 3, 0.55)}
    />
    {/* 山门牌坊 */}
    <g style={ink('ink', 0.85)} transform='translate(90 0)'>
      <rect x='110' y='420' width='6' height='54' />
      <rect x='160' y='420' width='6' height='54' />
      <path d='M96 420 L180 420 L174 410 L102 410 Z' />
      <path d='M90 410 Q138 392 186 410 L180 404 Q138 388 96 404 Z' />
    </g>
    {[150, 182, 600, 630, 662].map((x, i) => (
      <path
        key={x}
        d={pinePath(x, 500 - (i % 2) * 8, 1.3 - (i % 3) * 0.2)}
        style={ink('ink', 0.8)}
      />
    ))}
    <Mist y={470} h={50} opacity={0.6} />
  </>
);

const Chixia = () => (
  <>
    <Sky id='wd2' />
    <circle cx='220' cy='110' r='38' style={ink('red', 0.55)} />
    <path d={ridge({ y: 250, amp: 60, seed: 90 })} style={ink('tint', 0.3)} />
    {/* 火云岭的火山与烟 */}
    <path d='M430 310 L540 120 L580 128 L700 310 Z' style={ink('mtn-3', 0.9)} />
    <path d='M540 122 L560 150 L580 126' style={stroke('red', 4, 0.8)} />
    <path
      d='M560 118 C530 80 600 70 570 40 C550 20 600 10 620 0'
      style={stroke('ink-3', 14, 0.25)}
    />
    <Mist y={290} h={24} opacity={0.8} />
    <path d={ridge({ y: 380, amp: 40, seed: 330 })} style={ink('tint', 0.55)} />
    {/* 熔岩河 */}
    <path
      d='M0 450 C140 420 220 470 340 440 C460 410 560 470 800 430'
      style={stroke('red', 6, 0.55)}
    />
    <path
      d={ridge({ y: 470, amp: 22, seed: 610 })}
      style={ink('mtn-3', 0.85)}
    />
    {/* 枯树 */}
    <g style={stroke('ink', 3, 0.8)}>
      <path d='M170 500 L176 430 M176 450 L156 420 M175 440 L198 410 M180 430 L192 400' />
      <path d='M640 500 L636 440 M636 460 L660 430 M634 450 L618 425' />
    </g>
  </>
);

const Hanyuan = () => {
  const snow = scatter(46, 7, W, H * 0.8);
  return (
    <>
      <Sky id='wd3' />
      <circle cx='400' cy='80' r='26' style={ink('paper', 0.9)} />
      <circle cx='400' cy='80' r='26' style={stroke('tint', 2, 0.6)} />
      <path
        d={ridge({ y: 220, amp: 90, seed: 150, sharp: 2 })}
        style={ink('tint', 0.28)}
      />
      {/* 两侧冰崖，中间是深渊 */}
      <path
        d='M0 520 L0 180 L80 220 L120 170 L200 260 L250 240 L330 520 Z'
        style={ink('mtn-2', 0.9)}
      />
      <path
        d='M800 520 L800 160 L720 210 L680 160 L600 250 L560 230 L470 520 Z'
        style={ink('mtn-2', 0.9)}
      />
      <defs>
        <linearGradient id='wd3-abyss' x1='0' y1='0' x2='0' y2='1'>
          <stop
            offset='0'
            style={{ stopColor: 'var(--wd-ink)', stopOpacity: 0 }}
          />
          <stop
            offset='1'
            style={{ stopColor: 'var(--wd-ink)', stopOpacity: 0.75 }}
          />
        </linearGradient>
      </defs>
      <path d='M330 520 L250 240 L560 230 L470 520 Z' fill='url(#wd3-abyss)' />
      <g style={stroke('paper', 2, 0.8)}>
        <path d='M80 222 L86 250 M120 172 L124 200 M200 262 L204 290 M680 162 L684 190 M720 212 L724 238' />
      </g>
      <Mist y={300} h={30} opacity={0.6} />
      {snow.map((p, i) => (
        <circle
          key={i}
          cx={p.x}
          cy={p.y}
          r={0.8 + p.r * 1.8}
          style={ink('paper', 0.8)}
        />
      ))}
    </>
  );
};

const Wanyao = () => {
  const eyes = scatter(7, 21, W, 200);
  return (
    <>
      <Sky id='wd4' />
      <path d={ridge({ y: 200, amp: 50, seed: 70 })} style={ink('tint', 0.3)} />
      {/* 古木：粗干与伞形树冠 */}
      {[
        [170, 46, 120],
        [300, 30, 170],
        [560, 40, 130],
        [640, 54, 110],
      ].map(([x, w, top]) => (
        <g key={x}>
          <path
            d={`M${x - w / 2} 520 C${x - w / 3} 400 ${x - w / 4} 300 ${x - 6} ${top + 40} L${x + 6} ${top + 40} C${x + w / 4} 300 ${x + w / 3} 400 ${x + w / 2} 520 Z`}
            style={ink('mtn-3', 0.85)}
          />
          <ellipse
            cx={x}
            cy={top}
            rx={w * 2.2}
            ry={w * 0.9}
            style={ink('tint', 0.55)}
          />
          <ellipse
            cx={x - w}
            cy={top + 20}
            rx={w * 1.4}
            ry={w * 0.6}
            style={ink('tint', 0.4)}
          />
        </g>
      ))}
      {/* 藤蔓 */}
      <g style={stroke('ink', 2, 0.5)}>
        <path d='M120 140 C160 260 200 220 240 330' />
        <path d='M480 120 C520 220 500 280 540 360' />
        <path d='M650 160 C620 240 680 300 660 400' />
      </g>
      <Mist y={380} h={40} opacity={0.7} />
      {eyes.map((p, i) => (
        <g key={i} style={ink('red', 0.75)}>
          <circle cx={p.x} cy={260 + p.y} r='2.2' />
          <circle cx={p.x + 9} cy={260 + p.y} r='2.2' />
        </g>
      ))}
      <path
        d={ridge({ y: 490, amp: 18, seed: 400 })}
        style={ink('ink', 0.75)}
      />
    </>
  );
};

const Jiuyou = () => {
  const motes = scatter(30, 33, W, H);
  return (
    <>
      <Sky id='wd5' />
      <rect width={W} height={H} style={ink('tint', 0.35)} />
      {/* 倒悬之城：从天上垂下来的楼阁 */}
      <g style={ink('ink', 0.8)}>
        {[120, 230, 360, 470, 600, 700].map((x, i) => {
          const h = 90 + ((i * 37) % 80);
          return (
            <g key={x}>
              <rect x={x - 3} y='0' width='6' height={h - 30} />
              <path
                d={`M${x - 34} ${h - 30} L${x + 34} ${h - 30} L${x + 24} ${h - 18} L${x - 24} ${h - 18} Z`}
              />
              <rect x={x - 18} y={h - 18} width='36' height='26' />
              <path
                d={`M${x - 28} ${h + 8} L${x + 28} ${h + 8} L${x} ${h + 30} Z`}
              />
            </g>
          );
        })}
      </g>
      {/* 黑月 */}
      <circle cx='400' cy='250' r='60' style={ink('ink', 0.85)} />
      <circle cx='400' cy='250' r='64' style={stroke('red', 2, 0.6)} />
      {/* 死气之海 */}
      <path
        d='M0 430 C120 410 200 450 320 430 C440 410 540 450 800 420 L800 520 L0 520 Z'
        style={ink('ink', 0.7)}
      />
      <path
        d='M0 460 C140 440 260 480 400 460 C540 440 660 480 800 455'
        style={stroke('tint', 2, 0.6)}
      />
      {motes.map((p, i) => (
        <circle
          key={i}
          cx={p.x}
          cy={p.y}
          r={1 + p.r * 2}
          style={ink('red', 0.25 + p.r * 0.4)}
        />
      ))}
    </>
  );
};

const SCENES = [Qinglan, Chixia, Hanyuan, Wanyao, Jiuyou];

// ChapterScene 是第 chapter 章(从 0 起)的底图。
const ChapterScene = ({ chapter, className = '' }) => {
  const Scene = SCENES[chapter] || Qinglan;
  return (
    <svg
      className={`wd-scene is-ch${chapter + 1} ${className}`}
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio='xMidYMid slice'
      aria-hidden
    >
      <Scene />
    </svg>
  );
};

export default ChapterScene;
