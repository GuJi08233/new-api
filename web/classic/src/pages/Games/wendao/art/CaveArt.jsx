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

// 洞府图：山崖上开出的一座洞府，里面从左到右是静室(蒲团与香炉)、灵泉(山石与泉池)、丹室(丹炉)与器室(剑架)。还没开辟的屋子只画
// 一层淡影，开辟了的点亮；等级越高，洞口飘出的灵雾越多。颜色都取自水墨变量，深色主题自动换色。

const Faint = ({ lit, children }) => (
  <g style={{ opacity: lit ? 1 : 0.22, transition: 'opacity .6s' }}>
    {children}
  </g>
);

const Quiet = ({ lit }) => (
  <Faint lit={lit}>
    <ellipse cx='92' cy='142' rx='17' ry='5' style={ink('robe-brown', 0.85)} />
    <ellipse cx='92' cy='140' rx='13' ry='3.4' style={ink('robe-red', 0.7)} />
    <path d='M118 142 L118 132 L130 132 L130 142 Z' style={ink('ink-2')} />
    <path d='M116 132 L132 132' style={stroke('ink', 2)} />
    {lit && (
      <g className='wd-cave-smoke'>
        <path
          d='M124 130 Q119 120 125 112 Q131 104 125 94'
          style={stroke('ink-3', 1.4, 0.6)}
        />
        <path d='M126 128 Q132 118 127 108' style={stroke('ink-3', 1, 0.45)} />
      </g>
    )}
  </Faint>
);

const Spring = ({ lit }) => (
  <Faint lit={lit}>
    <path
      d='M136 146 Q134 130 144 122 Q148 108 160 110 Q172 110 174 124 Q180 134 175 146 Z'
      style={ink('mtn-3', 0.9)}
    />
    <path d='M150 112 Q156 108 162 112' style={stroke('ink-3', 1.2, 0.7)} />
    <path
      d='M170 112 Q182 114 184 126 L186 140'
      style={stroke('acc-ice', 2.6, 0.85)}
    />
    <ellipse cx='188' cy='144' rx='24' ry='6' style={ink('acc-ice', 0.55)} />
    {lit && (
      <g className='wd-cave-ripple'>
        <ellipse
          cx='188'
          cy='144'
          rx='11'
          ry='2.8'
          style={stroke('paper', 1.2, 0.8)}
        />
        <ellipse
          cx='188'
          cy='144'
          rx='18'
          ry='4.4'
          style={stroke('paper', 1, 0.5)}
        />
      </g>
    )}
  </Faint>
);

const Furnace = ({ lit }) => (
  <Faint lit={lit}>
    {lit && (
      <g className='wd-cave-fire'>
        <path
          d='M232 146 Q236 134 240 142 Q244 130 248 142 Q252 134 256 146 Z'
          style={ink('acc-fire', 0.85)}
        />
      </g>
    )}
    <path d='M228 146 L232 132 M256 146 L252 132' style={stroke('ink', 2.2)} />
    <path
      d='M226 112 Q244 104 262 112 L258 132 Q244 138 230 132 Z'
      style={ink('mtn-3')}
    />
    <path d='M228 112 L260 112' style={stroke('ink', 2.4)} />
    <path d='M238 104 Q244 98 250 104' style={stroke('ink', 2)} />
    <circle
      cx='244'
      cy='122'
      r='3.4'
      style={ink('acc-fire', lit ? 0.9 : 0.3)}
    />
  </Faint>
);

const Forge = ({ lit }) => (
  <Faint lit={lit}>
    <path
      d='M296 146 L296 104 M334 146 L334 104'
      style={stroke('robe-brown', 3)}
    />
    <path
      d='M292 110 L338 110 M292 126 L338 126'
      style={stroke('robe-brown', 2.4)}
    />
    {[304, 315, 326].map((x, i) => (
      <g key={x}>
        <path
          d={`M${x} 100 L${x} 136`}
          style={stroke(lit ? 'acc-ice' : 'ink-3', 2.2, lit ? 0.95 : 0.6)}
        />
        <path d={`M${x - 4} 128 L${x + 4} 128`} style={stroke('ink', 2)} />
        {lit && i === 1 && (
          <circle
            className='wd-cave-glint'
            cx={x}
            cy={104}
            r='2.4'
            style={ink('paper')}
          />
        )}
      </g>
    ))}
  </Faint>
);

const CaveArt = ({ levels = {} }) => {
  const total = Object.values(levels).reduce((sum, n) => sum + (n || 0), 0);
  const wisps = Math.min(Math.ceil(total / 5), 6);
  return (
    <svg
      className='wd-cave-art'
      viewBox='0 0 400 170'
      role='img'
      aria-hidden='true'
      preserveAspectRatio='xMidYMid slice'
    >
      <rect width='400' height='170' style={ink('paper-2')} />
      <path
        d='M0 170 L0 60 Q40 30 80 34 Q120 6 170 14 Q220 0 260 18 Q310 8 350 30 Q380 40 400 58 L400 170 Z'
        style={ink('mtn-2', 0.75)}
      />
      <path
        d='M20 60 Q60 50 90 58 M300 36 Q330 32 360 46'
        style={stroke('ink-3', 1.2, 0.6)}
      />
      <path
        d='M56 170 L56 92 Q60 40 200 38 Q340 40 344 92 L344 170 Z'
        style={ink('paper-3')}
      />
      <path
        d='M56 170 L56 92 Q60 40 200 38 Q340 40 344 92 L344 170'
        style={stroke('ink-2', 3)}
      />
      <path d='M56 150 L344 150' style={stroke('ink-3', 1.2, 0.6)} />
      <Quiet lit={levels.quiet > 0} />
      <Spring lit={levels.spring > 0} />
      <Furnace lit={levels.furnace > 0} />
      <Forge lit={levels.forge > 0} />
      {Array.from({ length: wisps }, (_, i) => (
        <path
          key={i}
          className='wd-cave-wisp'
          style={{
            ...stroke('mist', 3, 0.7),
            animationDelay: `${i * 0.9}s`,
          }}
          d={`M${90 + i * 44} 52 Q${100 + i * 44} 40 ${92 + i * 44} 28 Q${86 + i * 44} 18 ${98 + i * 44} 8`}
        />
      ))}
      <path
        d='M10 170 L10 120 M10 128 Q2 120 0 124 M10 136 Q22 126 30 132'
        style={stroke('ink', 2.4)}
      />
      <path
        d='M390 170 L390 110 M390 120 Q378 112 372 118 M390 130 Q400 124 400 126'
        style={stroke('ink', 2.4)}
      />
    </svg>
  );
};

export default CaveArt;
