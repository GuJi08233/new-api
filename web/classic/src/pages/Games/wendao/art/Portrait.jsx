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

// 人物半身像：头、发式、衣袍与几件标志性的配饰按人物拼出来，画布 120×140。人物见 pkg/wendao/story.md。

// 每个人物：发式、发色、衣袍颜色、胡须与配饰。
const CAST = {
  hero_warrior: {
    hair: 'topknot',
    robe: 'robe-dark',
    band: true,
    sash: true,
    wide: true,
  },
  hero_mage: { hair: 'long', robe: 'robe-white', pin: true, trim: true },
  hero_ranger: { hair: 'ponytail', robe: 'robe-blue', sword: true },
  master: {
    hair: 'bun',
    hairColor: 'white-hair',
    robe: 'robe-white',
    beard: 'long',
    beardColor: 'white-hair',
    crown: true,
  },
  qinghe: { hair: 'braids', robe: 'robe-green', leaf: true },
  linmu: {
    hair: 'short',
    robe: 'robe-dark',
    hat: true,
    sword: true,
    scarf: true,
  },
  laohan: {
    hair: 'short',
    robe: 'robe-blue',
    hood: true,
    beard: 'short',
    beardColor: 'white-hair',
  },
  mayor: { hair: 'cap', robe: 'robe-brown', moustache: true },
  chiyan: { hair: 'flame', robe: 'robe-red', horns: true, eyes: 'red' },
  taixu: { hair: 'bun', robe: 'robe-white', beard: 'long', tallCrown: true },
  taixu_fallen: {
    hair: 'bun',
    robe: 'robe-dark',
    beard: 'long',
    tallCrown: true,
    eyes: 'red',
  },
  master_soul: {
    hair: 'bun',
    hairColor: 'white-hair',
    robe: 'robe-white',
    beard: 'long',
    beardColor: 'white-hair',
    crown: true,
    ghost: true,
  },
  yaohuang: {
    hair: 'long',
    robe: 'robe-dark',
    ears: true,
    eyes: 'red',
    trim: true,
  },
  patriarch: {
    hair: 'none',
    robe: 'robe-dark',
    cowl: true,
    eyes: 'red',
    noFace: true,
  },
};

const Hair = ({ c }) => {
  const color = c.hairColor || 'ink';
  switch (c.hair) {
    case 'topknot':
      return (
        <g style={ink(color)}>
          <path d='M38 60 C38 34 82 34 82 60 C76 48 44 48 38 60 Z' />
          <ellipse cx='60' cy='34' rx='8' ry='7' />
        </g>
      );
    case 'long':
      return (
        <g style={ink(color)}>
          <path d='M36 64 C34 32 86 32 84 64 L88 112 L78 108 L76 66 C70 52 50 52 44 66 L42 108 L32 112 Z' />
        </g>
      );
    case 'ponytail':
      return (
        <g style={ink(color)}>
          <path d='M38 60 C38 34 82 34 82 60 C76 48 44 48 38 60 Z' />
          <path d='M78 40 C98 44 100 70 92 92 C90 74 86 56 76 46 Z' />
        </g>
      );
    case 'bun':
      return (
        <g style={ink(color)}>
          <path d='M38 60 C38 36 82 36 82 60 C76 50 44 50 38 60 Z' />
          <circle cx='60' cy='36' r='9' />
        </g>
      );
    case 'braids':
      return (
        <g style={ink(color)}>
          <path d='M38 62 C38 34 82 34 82 62 C74 46 46 46 38 62 Z' />
          <path d='M38 62 C34 80 36 96 40 110 L46 108 C42 94 42 80 44 66 Z' />
          <path d='M82 62 C86 80 84 96 80 110 L74 108 C78 94 78 80 76 66 Z' />
        </g>
      );
    case 'short':
      return (
        <path
          d='M38 60 C38 36 82 36 82 60 C76 50 44 50 38 60 Z'
          style={ink(color)}
        />
      );
    case 'cap':
      return (
        <g style={ink(color)}>
          <path d='M38 56 C38 38 82 38 82 56 Z' />
          <rect x='36' y='54' width='48' height='5' rx='2' />
        </g>
      );
    case 'flame':
      return (
        <path
          d='M36 62 C30 40 40 30 46 38 C46 24 56 20 60 30 C64 18 76 24 74 36 C82 30 90 42 84 62 C78 48 42 48 36 62 Z'
          style={ink('red')}
        />
      );
    default:
      return null;
  }
};

const Portrait = ({ who, className = '' }) => {
  const c = CAST[who] || CAST.hero_warrior;
  const eye = c.eyes === 'red' ? 'red' : 'ink';
  const shoulder = c.wide ? 6 : 0;
  return (
    <svg
      className={`wd-portrait ${className}`}
      viewBox='0 0 120 140'
      aria-hidden
    >
      {c.ghost && (
        <ellipse cx='60' cy='72' rx='54' ry='64' style={ink('acc-ice', 0.22)} />
      )}
      <g opacity={c.ghost ? 0.62 : undefined}>
        {/* 衣袍与领口 */}
        <path
          d={`M${8 - shoulder} 140 C${12 - shoulder} 108 38 96 60 96 C82 96 ${108 + shoulder} 108 ${112 + shoulder} 140 Z`}
          style={ink(c.robe)}
        />
        <path d='M46 98 L60 120 L74 98' style={stroke('paper', 3, 0.85)} />
        {c.trim && (
          <path d='M44 100 L60 124 L76 100' style={stroke('red', 2, 0.8)} />
        )}
        {c.sash && <path d='M30 128 L90 120' style={stroke('red', 6, 0.85)} />}
        {c.scarf && (
          <path
            d='M42 96 C52 104 68 104 78 96 L80 104 C68 112 52 112 40 104 Z'
            style={ink('mtn-2')}
          />
        )}
        {c.sword && (
          <g>
            <path d='M86 70 L104 30' style={stroke('ink', 4)} />
            <path d='M82 76 L92 66' style={stroke('red', 3)} />
          </g>
        )}
        {/* 兜帽在头后面 */}
        {c.hood && (
          <path
            d='M30 72 C26 30 94 30 90 72 L86 98 L34 98 Z'
            style={ink('mtn-2')}
          />
        )}
        {c.cowl && (
          <path
            d='M28 100 C20 40 100 40 92 100 L84 104 L36 104 Z'
            style={ink('ink')}
          />
        )}
        {c.ears && (
          <g style={ink('ink')}>
            <path d='M40 44 L34 18 L52 38 Z' />
            <path d='M80 44 L86 18 L68 38 Z' />
          </g>
        )}
        {c.horns && (
          <g style={ink('ink')}>
            <path d='M42 46 C30 38 28 22 34 14 C36 28 44 36 48 42 Z' />
            <path d='M78 46 C90 38 92 22 86 14 C84 28 76 36 72 42 Z' />
          </g>
        )}
        {/* 脖子与脸 */}
        <rect
          x='53'
          y='80'
          width='14'
          height='18'
          style={ink(c.noFace ? 'ink' : 'skin')}
        />
        <ellipse
          cx='60'
          cy='64'
          rx='21'
          ry='24'
          style={ink(c.noFace ? 'ink' : 'skin')}
        />
        {!c.noFace && (
          <g style={stroke('ink', 1.6, 0.75)}>
            <path d='M47 57 L55 56 M65 56 L73 57' />
            <path d='M57 74 Q60 76 63 74' />
          </g>
        )}
        <g style={c.eyes === 'red' ? ink(eye) : stroke(eye, 2.2)}>
          {c.eyes === 'red' ? (
            <>
              <path d='M47 63 L55 61 L54 65 Z' />
              <path d='M73 63 L65 61 L66 65 Z' />
            </>
          ) : (
            <path d='M48 63 L54 63 M66 63 L72 63' />
          )}
        </g>
        <Hair c={c} />
        {c.band && (
          <path d='M39 50 C50 44 70 44 81 50' style={stroke('red', 4)} />
        )}
        {c.pin && <path d='M70 38 L90 30' style={stroke('red', 2.5)} />}
        {c.leaf && (
          <path
            d='M74 44 C82 36 92 40 88 48 C84 52 76 50 74 44 Z'
            style={ink('robe-green')}
          />
        )}
        {c.crown && (
          <rect
            x='54'
            y='24'
            width='12'
            height='5'
            rx='1'
            style={ink('red', 0.9)}
          />
        )}
        {c.tallCrown && (
          <g>
            <path
              d='M48 36 L48 24 L54 28 L60 16 L66 28 L72 24 L72 36 Z'
              style={ink('ink')}
            />
            <path d='M44 30 L76 30' style={stroke('red', 2)} />
          </g>
        )}
        {c.hat && (
          <g style={ink('robe-brown')}>
            <path d='M20 54 L60 26 L100 54 Q60 62 20 54 Z' />
          </g>
        )}
        {c.beard === 'long' && (
          <path
            d='M46 74 C48 92 54 108 60 118 C66 108 72 92 74 74 C68 82 52 82 46 74 Z'
            style={ink(c.beardColor || 'ink')}
          />
        )}
        {c.beard === 'short' && (
          <path
            d='M44 72 C48 88 72 88 76 72 C70 80 50 80 44 72 Z'
            style={ink(c.beardColor || 'ink')}
          />
        )}
        {c.moustache && (
          <path
            d='M50 71 C56 67 64 67 70 71 C64 70 56 70 50 71 Z'
            style={ink('ink')}
          />
        )}
      </g>
    </svg>
  );
};

export default Portrait;
