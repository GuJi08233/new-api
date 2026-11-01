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

// 灵田里的作物：一垄土，上面按生长阶段画出幼苗、半大的苗与长成的作物。stage 0 是空地，1 刚发芽，2 长到一半，3 熟了。
// 熟了的样子每种作物不同；颜色都取自水墨变量，深色主题自动换色。

const Soil = () => (
  <>
    <ellipse cx='40' cy='68' rx='30' ry='7' style={ink('paper-3')} />
    <path d='M14 68 Q40 58 66 68' style={stroke('ink-3', 1.4, 0.7)} />
    <path d='M22 71 Q40 66 58 71' style={stroke('ink-3', 1, 0.45)} />
  </>
);

const Leaf = ({ x, y, angle, size = 1, color = 'acc-green' }) => (
  <path
    d={`M0 0 Q${6 * size} ${-5 * size} ${12 * size} 0 Q${6 * size} ${3 * size} 0 0 Z`}
    transform={`translate(${x} ${y}) rotate(${angle})`}
    style={ink(color, 0.9)}
  />
);

const Sprout = () => (
  <>
    <path d='M40 66 L40 56' style={stroke('acc-green', 2)} />
    <Leaf x={40} y={57} angle={-150} size={0.8} />
    <Leaf x={40} y={57} angle={-30} size={0.8} />
  </>
);

const Young = () => (
  <>
    <path d='M40 66 Q38 52 40 40' style={stroke('acc-green', 2.2)} />
    <Leaf x={39} y={56} angle={-160} />
    <Leaf x={40} y={50} angle={-20} />
    <Leaf x={39} y={44} angle={-150} size={0.8} />
    <Leaf x={40} y={41} angle={-40} size={0.7} />
  </>
);

const RIPE = {
  qingcao: () => (
    <>
      {[-18, -10, -3, 4, 11, 18].map((dx, i) => (
        <path
          key={i}
          d={`M${40 + dx / 3} 66 Q${40 + dx} ${48 - (i % 2) * 6} ${40 + dx * 1.4} ${30 + (i % 3) * 4}`}
          style={stroke('acc-green', 2.4, 0.85)}
        />
      ))}
      <circle cx='29' cy='34' r='1.6' style={ink('acc-ice', 0.8)} />
      <circle cx='52' cy='31' r='1.4' style={ink('acc-ice', 0.8)} />
    </>
  ),
  xueshen: () => (
    <>
      <path d='M40 66 L40 38' style={stroke('acc-green', 2)} />
      {[-150, -110, -70, -30].map((a, i) => (
        <Leaf key={i} x={40} y={44 + (i % 2) * 6} angle={a} size={1.2} />
      ))}
      {[
        [38, 32],
        [42, 32],
        [40, 28],
        [36, 29],
        [44, 29],
      ].map(([cx, cy], i) => (
        <circle key={i} cx={cx} cy={cy} r='2.6' style={ink('robe-red')} />
      ))}
    </>
  ),
  tieteng: () => (
    <>
      <path
        d='M40 66 Q26 56 34 46 Q44 36 34 28 Q28 22 36 16'
        style={stroke('acc-stone', 2.6)}
      />
      <path
        d='M40 66 Q54 58 48 48 Q42 40 52 34'
        style={stroke('acc-stone', 2.2)}
      />
      <Leaf x={34} y={46} angle={-170} color='robe-dark' />
      <Leaf x={36} y={28} angle={-20} color='robe-dark' />
      <Leaf x={48} y={48} angle={-10} color='robe-dark' />
      <Leaf x={52} y={34} angle={-160} size={0.8} color='robe-dark' />
      <circle cx='36' cy='16' r='2.2' style={ink('acc-stone')} />
    </>
  ),
  zhuguo: () => (
    <>
      <path d='M40 66 L40 40' style={stroke('robe-brown', 3)} />
      <ellipse cx='40' cy='32' rx='18' ry='13' style={ink('acc-green', 0.75)} />
      <ellipse cx='34' cy='28' rx='9' ry='7' style={ink('acc-green', 0.55)} />
      {[
        [32, 36],
        [46, 34],
        [40, 26],
        [50, 28],
      ].map(([cx, cy], i) => (
        <circle key={i} cx={cx} cy={cy} r='3.4' style={ink('acc-fire')} />
      ))}
    </>
  ),
  lingzhi: () => (
    <>
      <path d='M40 66 Q38 56 40 46' style={stroke('robe-brown', 3)} />
      <path d='M30 54 Q32 50 36 52' style={stroke('robe-brown', 2)} />
      <path d='M24 46 Q40 30 56 46 Q40 42 24 46 Z' style={ink('robe-brown')} />
      <path d='M28 44 Q40 36 52 44' style={stroke('acc-fire', 1.2, 0.6)} />
      <path
        d='M20 56 Q30 46 40 56 Q30 53 20 56 Z'
        style={ink('robe-red', 0.85)}
      />
      <path
        d='M42 60 Q50 52 58 60 Q50 57 42 60 Z'
        style={ink('robe-red', 0.75)}
      />
    </>
  ),
  lotus: () => (
    <>
      <ellipse cx='40' cy='64' rx='20' ry='4' style={ink('acc-green', 0.6)} />
      <path d='M40 62 L40 44' style={stroke('acc-green', 2)} />
      {[-60, -30, 0, 30, 60].map((a, i) => (
        <path
          key={i}
          d='M0 0 Q-5 -10 0 -18 Q5 -10 0 0 Z'
          transform={`translate(40 44) rotate(${a})`}
          style={ink(i % 2 ? 'acc-violet' : 'red', 0.8)}
        />
      ))}
      <circle cx='40' cy='42' r='3' style={ink('acc-fire', 0.9)} />
      <circle cx='22' cy='30' r='1.2' style={ink('acc-violet', 0.7)} />
      <circle cx='58' cy='26' r='1.4' style={ink('acc-ice', 0.7)} />
    </>
  ),
};

const CropArt = ({ crop, stage }) => {
  const Ripe = RIPE[crop];
  return (
    <svg viewBox='8 12 64 64' className='wd-crop' aria-hidden='true'>
      <Soil />
      {stage === 1 && <Sprout />}
      {stage === 2 && <Young />}
      {stage === 3 && Ripe && <Ripe />}
    </svg>
  );
};

export default CropArt;
