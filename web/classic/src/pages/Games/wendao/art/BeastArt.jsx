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
import { ink, stroke } from './ink';

// 灵兽：白虎、青鹿、玄龟、赤雀、石猿与九尾灵狐，水墨的小身形。stage 是进化的阶数：幼兽小一圈，成兽身后有一圈灵光，
// 圣兽灵光更亮、脚下多一朵祥云。egg 为真时画一颗还没孵出来的蛋。颜色都取自水墨变量，深色主题自动换色。

// 白色的身形要描一道墨线，才不会融进纸色里。
const outlined = (name, opacity) => ({
  ...ink(name, opacity),
  stroke: 'var(--wd-ink-2)',
  strokeWidth: 1.4,
  strokeLinejoin: 'round',
});

const Eye = ({ x, y }) => (
  <>
    <circle cx={x} cy={y} r='2.2' style={ink('ink')} />
    <circle cx={x + 0.7} cy={y - 0.7} r='0.7' style={ink('paper')} />
  </>
);

const BODIES = {
  baihu: () => (
    <>
      <path d='M74 52 Q90 50 88 34 Q87 28 82 30' style={stroke('ink-2', 6)} />
      <path
        d='M74 52 Q90 50 88 34 Q87 28 82 30'
        style={stroke('robe-white', 4)}
      />
      <path d='M83 46 L88 44 M86 38 L90 37' style={stroke('ink', 1.6)} />
      <path
        d='M26 62 Q22 44 36 39 Q52 34 66 38 Q78 44 76 62 Z'
        style={outlined('robe-white')}
      />
      {[46, 54, 62, 70].map((x) => (
        <path
          key={x}
          d={`M${x} 39 Q${x - 4} 45 ${x - 1} 50`}
          style={stroke('ink', 2.2, 0.85)}
        />
      ))}
      <path
        d='M28 62 L28 69 M39 62 L39 69 M60 62 L60 69 M71 62 L71 69'
        style={stroke('ink-2', 4.5)}
      />
      <path
        d='M21 22 L23 12 L31 19 Z M39 19 L47 12 L48 22 Z'
        style={outlined('robe-white')}
      />
      <circle cx='34' cy='32' r='14' style={outlined('robe-white')} />
      <path
        d='M24 22 L25 16 L29 20 M43 20 L46 16 L46 22'
        style={ink('robe-red', 0.6)}
      />
      <path
        d='M30 21 L34 25 L38 21 M24 30 L28 31 M44 30 L40 31 M25 35 L28 35 M43 35 L40 35'
        style={stroke('ink', 1.6)}
      />
      <Eye x={29} y={31} />
      <Eye x={39} y={31} />
      <ellipse cx='34' cy='38' rx='5' ry='3.4' style={ink('paper')} />
      <path d='M32 36 L36 36 L34 38 Z' style={ink('robe-red')} />
      <path d='M31 40 Q34 42 37 40' style={stroke('ink', 1.2)} />
    </>
  ),
  lu: () => (
    <>
      <path
        d='M30 58 Q28 40 46 38 Q64 36 70 46 Q74 56 68 60 Z'
        style={ink('acc-green', 0.85)}
      />
      <path
        d='M34 60 L33 72 M44 60 L44 72 M60 60 L61 72 M68 58 L70 72'
        style={stroke('ink-2', 2.6)}
      />
      <path d='M36 42 Q30 30 32 22' style={stroke('acc-green', 7)} />
      <ellipse cx='30' cy='20' rx='9' ry='7' style={ink('acc-green')} />
      <path
        d='M28 14 Q22 4 16 6 M22 8 Q20 2 24 0 M32 13 Q36 2 44 4 M38 6 Q40 0 36 -2'
        style={stroke('robe-brown', 2.2)}
      />
      {[48, 56, 62].map((x, i) => (
        <circle
          key={x}
          cx={x}
          cy={46 + (i % 2) * 4}
          r='1.8'
          style={ink('paper', 0.85)}
        />
      ))}
      <Eye x={27} y={19} />
      <circle cx='22' cy='23' r='1.4' style={ink('ink')} />
    </>
  ),
  gui: () => (
    <>
      <ellipse cx='50' cy='56' rx='30' ry='8' style={ink('acc-ice', 0.3)} />
      <path d='M22 56 Q26 30 50 28 Q74 30 78 56 Z' style={ink('mtn-3')} />
      <path
        d='M38 32 L44 44 L56 44 L62 32 M44 44 L40 56 M56 44 L60 56 M30 46 L40 50 M70 46 L60 50'
        style={stroke('acc-ice', 1.8, 0.9)}
      />
      <path
        d='M78 50 Q90 46 90 40 Q90 34 84 36 Q80 40 78 46 Z'
        style={ink('acc-green')}
      />
      <Eye x={86} y={40} />
      <path
        d='M26 56 L22 64 M40 58 L38 66 M62 58 L64 66 M74 56 L78 64'
        style={stroke('acc-green', 4)}
      />
      <path d='M22 52 Q14 54 12 50' style={stroke('acc-green', 3)} />
    </>
  ),
  que: () => (
    <>
      <path
        d='M30 50 Q36 34 52 34 Q64 36 66 48 Q60 60 44 60 Q34 58 30 50 Z'
        style={ink('acc-fire')}
      />
      <path d='M44 44 Q56 34 70 30 Q64 44 52 50 Z' style={ink('robe-red')} />
      <path
        d='M30 52 Q16 58 6 54 Q16 62 10 70 Q22 62 32 58'
        style={ink('acc-fire', 0.85)}
      />
      <circle cx='64' cy='34' r='9' style={ink('acc-fire')} />
      <path d='M60 26 Q62 16 68 18 Q66 22 66 26' style={ink('robe-red')} />
      <path d='M72 34 L80 36 L72 38 Z' style={ink('robe-brown')} />
      <Eye x={66} y={32} />
      <path d='M46 60 L44 68 M52 60 L54 68' style={stroke('ink-2', 2)} />
    </>
  ),
  yuan: () => (
    <>
      <path d='M30 64 Q26 40 50 36 Q72 40 70 64 Z' style={ink('acc-stone')} />
      <path
        d='M30 46 Q18 54 20 66 M70 46 Q82 54 80 66'
        style={stroke('acc-stone', 7)}
      />
      <circle cx='50' cy='28' r='13' style={ink('acc-stone')} />
      <ellipse cx='50' cy='31' rx='8' ry='7' style={ink('skin')} />
      <circle cx='36' cy='26' r='4' style={ink('acc-stone')} />
      <circle cx='64' cy='26' r='4' style={ink('acc-stone')} />
      <Eye x={46} y={28} />
      <Eye x={54} y={28} />
      <path d='M47 35 Q50 37 53 35' style={stroke('ink', 1.4)} />
      <path d='M38 50 L44 54 M60 48 L56 56' style={stroke('mtn-3', 1.6, 0.8)} />
      <path d='M38 64 L36 70 M62 64 L64 70' style={stroke('ink-2', 5)} />
    </>
  ),
  hu: () => (
    <>
      {[-70, -50, -30, -10, 10, 30, 50, 70, 90].map((a, k) => (
        <path
          key={a}
          d='M0 0 Q12 -6 28 -2 Q20 4 0 0 Z'
          transform={`translate(32 50) rotate(${180 + a / 1.6})`}
          style={outlined(k % 2 ? 'paper-3' : 'robe-white', 0.95)}
        />
      ))}
      <path
        d='M30 60 Q28 44 46 42 Q64 40 68 52 Q66 62 50 64 Q36 64 30 60 Z'
        style={outlined('robe-white')}
      />
      <path
        d='M36 62 L35 70 M46 64 L46 70 M58 62 L59 70 M64 60 L66 68'
        style={stroke('ink-2', 2.6)}
      />
      <path d='M60 46 Q64 36 70 32' style={stroke('ink-2', 9)} />
      <path d='M60 46 Q64 36 70 32' style={stroke('robe-white', 7)} />
      <path
        d='M66 28 L64 16 L72 24 Z M74 24 L80 14 L80 26 Z'
        style={outlined('robe-white')}
      />
      <path d='M66 26 L66 20 M76 23 L78 18' style={stroke('acc-fire', 1.6)} />
      <path
        d='M64 30 Q70 22 80 26 Q88 30 92 33 Q84 36 76 36 Q66 36 64 30 Z'
        style={outlined('robe-white')}
      />
      <circle cx='92' cy='33' r='1.6' style={ink('ink')} />
      <path d='M74 29 Q76 28 78 30' style={stroke('ink', 1.6)} />
      <path d='M72 33 Q76 35 80 33' style={stroke('acc-fire', 1.2, 0.8)} />
    </>
  ),
};

const Egg = () => (
  <g className='wd-beast-egg'>
    <ellipse cx='50' cy='66' rx='18' ry='4' style={ink('ink-3', 0.3)} />
    <path
      d='M50 18 Q70 20 70 46 Q70 66 50 66 Q30 66 30 46 Q30 20 50 18 Z'
      style={ink('paper-3')}
    />
    <path
      d='M50 18 Q70 20 70 46 Q70 66 50 66 Q30 66 30 46 Q30 20 50 18 Z'
      style={stroke('ink-2', 2)}
    />
    <path
      d='M36 40 Q42 36 46 42 M54 32 Q60 30 62 36 M40 54 Q46 52 50 56'
      style={stroke('acc-green', 2, 0.7)}
    />
  </g>
);

const BeastArt = ({ kind, stage = 0, egg = false, active = false }) => {
  const Body = BODIES[kind] || BODIES.baihu;
  const scale = [0.82, 0.94, 1][stage] ?? 1;
  return (
    <svg
      className={`wd-beast-art${active ? ' is-active' : ''}`}
      viewBox='0 -6 100 84'
      role='img'
      aria-hidden='true'
    >
      {egg ? (
        <Egg />
      ) : (
        <>
          {stage >= 1 && (
            <circle
              className='wd-beast-aura'
              cx='50'
              cy='40'
              r={stage >= 2 ? 38 : 32}
              style={ink(
                stage >= 2 ? 'acc-fire' : 'acc-ice',
                stage >= 2 ? 0.18 : 0.14,
              )}
            />
          )}
          {stage >= 2 && (
            <path
              d='M14 72 Q22 64 32 70 Q40 62 50 70 Q60 62 68 70 Q78 64 86 72 Z'
              style={ink('mist')}
            />
          )}
          <g
            className='wd-beast-body'
            transform={`translate(50 70) scale(${scale}) translate(-50 -70)`}
          >
            <Body />
          </g>
        </>
      )}
    </svg>
  );
};

export default BeastArt;
