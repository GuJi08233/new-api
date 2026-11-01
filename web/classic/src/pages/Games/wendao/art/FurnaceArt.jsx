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

// 丹炉：三足鼎，炉下的火按火候大小不同(文火小、武火旺)，炉口按进度冒烟；炼好了炉口放出一圈金光；idle 是空炉，不生火。
// 颜色取自水墨变量。
const FurnaceArt = ({
  fire = 'steady',
  done = false,
  idle = false,
  progress = 0,
}) => {
  const flame = { gentle: 0.7, steady: 1, fierce: 1.35 }[fire] || 1;
  const smoke = Math.min(Math.max(progress, 0.15), 1);
  return (
    <svg
      className='wd-furnace-art'
      viewBox='0 0 80 80'
      role='img'
      aria-hidden='true'
    >
      {done && (
        <circle
          className='wd-furnace-glow'
          cx='40'
          cy='26'
          r='18'
          style={ink('acc-fire', 0.25)}
        />
      )}
      {!done && !idle && (
        <g className='wd-cave-smoke' style={{ opacity: smoke }}>
          <path
            d='M36 22 Q32 14 38 8 Q44 2 39 -4'
            style={stroke('ink-3', 1.6, 0.6)}
          />
          <path d='M44 22 Q49 15 44 9' style={stroke('ink-3', 1.2, 0.5)} />
        </g>
      )}
      {!idle && (
        <g transform={`translate(40 74) scale(${flame}) translate(-40 -74)`}>
          <g className='wd-cave-fire'>
            <path
              d='M28 74 Q31 62 35 69 Q38 56 42 68 Q46 58 49 69 Q52 63 54 74 Z'
              style={ink('acc-fire', done ? 0.35 : 0.85)}
            />
          </g>
        </g>
      )}
      <path
        d='M24 74 L28 58 M56 74 L52 58 M40 74 L40 60'
        style={stroke('ink', 2.4)}
      />
      <path
        d='M18 30 Q40 22 62 30 L58 54 Q40 62 22 54 Z'
        style={ink('mtn-3')}
      />
      <path d='M18 30 L62 30' style={stroke('ink', 3)} />
      <path
        d='M26 26 Q22 18 18 22 M54 26 Q58 18 62 22'
        style={stroke('ink', 2.2)}
      />
      <path d='M28 42 Q40 48 52 42' style={stroke('ink-3', 1.2, 0.8)} />
      <circle
        cx='40'
        cy='40'
        r='3'
        style={ink(done ? 'acc-fire' : 'paper-3')}
      />
    </svg>
  );
};

export default FurnaceArt;
