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

// 敌人的水墨剪影：墨色身形，加一点本色(青泥的绿、火灵的赤、冰晶的青……)与朱砂的眼睛。画布 120×120，标识与
// pkg/dungeon 的敌人一致。九幽秘境与历练共用。

const Eyes = ({ x = 60, y = 50, gap = 9, r = 2.6, color = 'red' }) => (
  <g style={ink(color)}>
    <circle cx={x - gap} cy={y} r={r} />
    <circle cx={x + gap} cy={y} r={r} />
  </g>
);

const SlitEyes = ({ y = 50, gap = 10, color = 'red' }) => (
  <g style={ink(color)}>
    <path
      d={`M${60 - gap - 5} ${y - 2} L${60 - gap + 5} ${y + 1} L${60 - gap - 4} ${y + 2} Z`}
    />
    <path
      d={`M${60 + gap + 5} ${y - 2} L${60 + gap - 5} ${y + 1} L${60 + gap + 4} ${y + 2} Z`}
    />
  </g>
);

const ART = {
  slime: () => (
    <>
      <path
        d='M24 100 C18 70 36 40 60 40 C84 40 102 70 96 100 C86 96 80 104 72 100 C66 96 54 96 48 100 C40 104 32 96 24 100 Z'
        style={ink('acc-green', 0.85)}
      />
      <path d='M36 64 C42 52 52 48 60 48' style={stroke('paper', 3, 0.6)} />
      <Eyes y={70} gap={11} color='ink' r={3.2} />
    </>
  ),
  slime_king: () => (
    <>
      <path
        d='M14 104 C8 66 30 28 60 28 C90 28 112 66 106 104 C94 98 86 108 76 102 C68 98 52 98 44 102 C34 108 26 98 14 104 Z'
        style={ink('acc-green', 0.9)}
      />
      <path
        d='M40 30 L44 12 L52 24 L60 8 L68 24 L76 12 L80 30 Z'
        style={ink('ink', 0.85)}
      />
      <SlitEyes y={64} gap={14} />
      <path d='M44 82 Q60 92 76 82' style={stroke('ink', 3, 0.8)} />
    </>
  ),
  goblin: () => (
    <>
      <path
        d='M40 104 L44 76 C30 74 28 56 36 48 L24 40 L40 42 C44 30 76 30 80 42 L96 40 L84 48 C92 56 90 74 76 76 L80 104 L68 104 L60 86 L52 104 Z'
        style={ink('ink', 0.88)}
      />
      <path d='M30 80 C20 90 18 100 24 104' style={stroke('ink', 5, 0.88)} />
      <path d='M90 80 C100 90 102 100 96 104' style={stroke('ink', 5, 0.88)} />
      <Eyes y={54} gap={8} />
      <path
        d='M52 64 L56 68 L60 64 L64 68 L68 64'
        style={stroke('paper', 2, 0.8)}
      />
    </>
  ),
  goblin_chief: () => (
    <>
      <path
        d='M30 108 L36 72 C18 68 18 44 30 38 L16 26 L36 32 C42 18 78 18 84 32 L104 26 L90 38 C102 44 102 68 84 72 L90 108 L72 108 L60 88 L48 108 Z'
        style={ink('ink', 0.9)}
      />
      <path d='M34 36 C48 28 72 28 86 36' style={stroke('red', 5, 0.85)} />
      <path d='M98 60 L112 100' style={stroke('mtn-3', 9)} />
      <circle cx='98' cy='58' r='8' style={ink('mtn-3')} />
      <SlitEyes y={48} gap={10} />
      <path
        d='M48 60 L54 66 L60 60 L66 66 L72 60'
        style={stroke('paper', 2.5, 0.85)}
      />
    </>
  ),
  skeleton: () => (
    <>
      <g style={stroke('ink-2', 7)}>
        <path d='M60 62 L60 96 M44 70 L76 70 M46 78 L74 78 M48 86 L72 86 M52 96 L46 112 M68 96 L74 112 M44 70 L32 92 M76 70 L90 88' />
      </g>
      <g style={stroke('bone', 4)}>
        <path d='M60 62 L60 96 M44 70 L76 70 M46 78 L74 78 M48 86 L72 86 M52 96 L46 112 M68 96 L74 112 M44 70 L32 92 M76 70 L90 88' />
      </g>
      <path
        d='M42 40 C42 22 78 22 78 40 C78 52 72 56 70 62 L50 62 C48 56 42 52 42 40 Z'
        style={ink('bone')}
      />
      <Eyes y={42} gap={8} r={4} color='ink' />
      <path d='M90 88 L104 40' style={stroke('mtn-3', 3)} />
    </>
  ),
  moth: () => (
    <>
      <path
        d='M60 60 C40 26 10 30 12 54 C14 74 40 74 58 66 Z'
        style={ink('acc-violet', 0.7)}
      />
      <path
        d='M60 60 C80 26 110 30 108 54 C106 74 80 74 62 66 Z'
        style={ink('acc-violet', 0.7)}
      />
      <path
        d='M58 66 C40 74 26 96 40 100 C50 102 56 86 60 70 Z'
        style={ink('acc-violet', 0.5)}
      />
      <path
        d='M62 66 C80 74 94 96 80 100 C70 102 64 86 60 70 Z'
        style={ink('acc-violet', 0.5)}
      />
      <circle cx='32' cy='52' r='6' style={ink('red', 0.8)} />
      <circle cx='88' cy='52' r='6' style={ink('red', 0.8)} />
      <ellipse cx='60' cy='66' rx='5' ry='20' style={ink('ink')} />
      <path
        d='M58 48 C54 36 48 32 44 32 M62 48 C66 36 72 32 76 32'
        style={stroke('ink', 2)}
      />
    </>
  ),
  gargoyle: () => (
    <>
      <path
        d='M24 108 L28 70 C20 60 22 44 34 40 L30 22 L44 34 C52 30 68 30 76 34 L90 22 L86 40 C98 44 100 60 92 70 L96 108 Z'
        style={ink('acc-stone', 0.95)}
      />
      <path
        d='M10 60 L28 52 L30 76 Z M110 60 L92 52 L90 76 Z'
        style={ink('mtn-3')}
      />
      <g style={stroke('ink', 2, 0.5)}>
        <path d='M40 74 L52 88 L46 100 M80 74 L70 86 L76 98' />
      </g>
      <SlitEyes y={54} gap={11} />
    </>
  ),
  werewolf: () => (
    <>
      <path
        d='M30 110 L34 76 L22 60 L28 30 L44 48 C52 44 68 44 76 48 L92 30 L98 60 L86 76 L90 110 Z'
        style={ink('ink', 0.88)}
      />
      <path d='M48 70 L60 86 L72 70 Z' style={ink('mtn-3')} />
      <SlitEyes y={58} gap={11} />
      <path
        d='M52 84 L56 90 L60 84 L64 90 L68 84'
        style={stroke('paper', 2, 0.85)}
      />
    </>
  ),
  shade_mage: () => (
    <>
      <path
        d='M34 112 C30 80 36 56 44 40 C48 24 72 24 76 40 C84 56 90 80 86 112 C76 106 70 112 60 108 C50 112 44 106 34 112 Z'
        style={ink('ink', 0.9)}
      />
      <path
        d='M46 48 C50 40 70 40 74 48 L72 60 L48 60 Z'
        style={ink('paper', 0.12)}
      />
      <Eyes y={54} gap={7} color='acc-violet' />
      <circle cx='96' cy='70' r='10' style={ink('acc-violet', 0.75)} />
      <path d='M86 80 L76 96' style={stroke('ink', 3)} />
    </>
  ),
  lava_elemental: () => (
    <>
      <path
        d='M30 108 C22 84 32 66 40 60 C34 44 46 26 52 36 C54 20 66 16 66 32 C74 22 86 34 80 50 C92 58 98 84 90 108 Z'
        style={ink('acc-fire', 0.9)}
      />
      <path
        d='M44 92 C48 76 54 72 60 70 C66 72 72 76 76 92'
        style={stroke('paper', 3, 0.5)}
      />
      <Eyes y={66} gap={9} color='ink' r={3.4} />
      <path d='M30 108 L90 108' style={stroke('ink', 4, 0.8)} />
    </>
  ),
  beetle: () => (
    <>
      <g style={stroke('ink', 3)}>
        <path d='M36 70 L18 60 M36 80 L16 82 M38 90 L22 104 M84 70 L102 60 M84 80 L104 82 M82 90 L98 104' />
      </g>
      <ellipse cx='60' cy='80' rx='28' ry='24' style={ink('acc-stone', 0.95)} />
      <path d='M60 56 L60 104' style={stroke('ink', 2, 0.7)} />
      <g style={ink('ink')}>
        <path d='M44 60 L48 52 L52 60 Z M56 58 L60 48 L64 58 Z M68 60 L72 52 L76 60 Z' />
      </g>
      <ellipse cx='60' cy='52' rx='14' ry='9' style={ink('ink')} />
      <Eyes y={52} gap={6} r={2} />
      <path
        d='M52 46 C46 36 40 32 34 32 M68 46 C74 36 80 32 86 32'
        style={stroke('ink', 2)}
      />
    </>
  ),
  troll: () => (
    <>
      <path
        d='M18 112 L22 70 C10 60 14 40 30 38 C34 22 86 22 90 38 C106 40 110 60 98 70 L102 112 L76 112 L68 92 L52 92 L44 112 Z'
        style={ink('ink', 0.9)}
      />
      <path
        d='M30 38 C24 28 26 18 34 16 M90 38 C96 28 94 18 86 16'
        style={stroke('mtn-3', 5)}
      />
      <path d='M36 70 C48 80 72 80 84 70' style={stroke('acc-fire', 4, 0.7)} />
      <SlitEyes y={50} gap={12} />
      <path d='M48 60 L52 66 M72 60 L68 66' style={stroke('paper', 3)} />
    </>
  ),
  death_knight: () => (
    <>
      <path
        d='M28 112 L32 74 C20 66 22 52 32 50 L88 50 C98 52 100 66 88 74 L92 112 Z'
        style={ink('ink', 0.92)}
      />
      <path
        d='M40 52 C40 26 80 26 80 52 L76 66 L44 66 Z'
        style={ink('mtn-3')}
      />
      <path d='M44 46 L76 46' style={stroke('ink', 4)} />
      <path d='M60 28 C70 14 84 12 92 16' style={stroke('red', 4, 0.85)} />
      <Eyes y={50} gap={8} color='acc-ice' />
      <path d='M98 40 L98 108' style={stroke('mtn-2', 4)} />
      <path d='M90 50 L106 50' style={stroke('mtn-2', 4)} />
    </>
  ),
  flame_lord: () => (
    <>
      <path
        d='M20 116 C10 80 24 52 36 46 C28 28 38 10 50 22 C52 6 68 6 70 22 C82 10 92 28 84 46 C96 52 110 80 100 116 Z'
        style={ink('acc-fire', 0.85)}
      />
      <path
        d='M30 116 L34 78 C26 70 30 56 40 54 L80 54 C90 56 94 70 86 78 L90 116 Z'
        style={ink('ink', 0.92)}
      />
      <path
        d='M42 56 C28 46 26 30 32 20 C36 34 44 44 50 50 Z M78 56 C92 46 94 30 88 20 C84 34 76 44 70 50 Z'
        style={ink('ink')}
      />
      <SlitEyes y={68} gap={11} />
      <path d='M48 82 Q60 90 72 82' style={stroke('acc-fire', 3)} />
    </>
  ),
  abyss_guard: () => (
    <>
      <path
        d='M30 112 L34 72 L26 56 L94 56 L86 72 L90 112 Z'
        style={ink('ink', 0.92)}
      />
      <path d='M40 58 C38 30 82 30 80 58 Z' style={ink('mtn-3')} />
      <path d='M60 26 L60 58' style={stroke('ink', 3)} />
      <Eyes y={48} gap={8} color='acc-violet' />
      <path d='M100 20 L100 112' style={stroke('mtn-2', 4)} />
      <path
        d='M90 26 C100 16 110 20 112 34 C104 30 98 32 100 40'
        style={ink('mtn-2')}
      />
    </>
  ),
  void_walker: () => (
    <>
      <path
        d='M60 22 C84 22 92 50 88 72 C86 92 96 104 104 112 C88 110 80 100 74 112 C70 104 64 100 60 112 C56 100 50 104 46 112 C40 100 32 110 16 112 C24 104 34 92 32 72 C28 50 36 22 60 22 Z'
        style={ink('acc-violet', 0.55)}
      />
      <path
        d='M60 32 C76 32 80 52 78 66 L42 66 C40 52 44 32 60 32 Z'
        style={ink('ink', 0.85)}
      />
      <Eyes y={52} gap={8} color='paper' r={2.2} />
    </>
  ),
  soul_eater: () => (
    <>
      <path
        d='M20 70 C20 34 100 34 100 70 C100 92 84 106 60 106 C36 106 20 92 20 70 Z'
        style={ink('ink', 0.9)}
      />
      <path
        d='M34 72 C40 92 80 92 86 72 C78 80 42 80 34 72 Z'
        style={ink('red', 0.75)}
      />
      <g style={ink('paper', 0.9)}>
        <path d='M38 74 L42 84 L46 76 Z M52 78 L56 88 L60 78 Z M64 78 L68 88 L72 78 Z M76 74 L80 84 L84 76 Z' />
      </g>
      <SlitEyes y={54} gap={14} color='acc-violet' />
      <g style={stroke('ink', 3, 0.7)}>
        <path d='M26 90 C14 100 12 110 18 116 M94 90 C106 100 108 110 102 116' />
      </g>
    </>
  ),
  crystal_golem: () => (
    <>
      <path
        d='M30 112 L26 74 L36 50 L60 40 L84 50 L94 74 L90 112 Z'
        style={ink('acc-ice', 0.75)}
      />
      <g style={stroke('paper', 2, 0.7)}>
        <path d='M36 50 L60 70 L84 50 M60 70 L60 112 M26 74 L60 70 L94 74' />
      </g>
      <path
        d='M46 36 L60 12 L74 36 Z M24 52 L30 30 L40 46 Z M96 52 L90 30 L80 46 Z'
        style={ink('acc-ice', 0.9)}
      />
      <Eyes y={56} gap={9} color='ink' r={3} />
    </>
  ),
  abyss_drake: () => (
    <>
      <path
        d='M12 104 C30 112 52 96 50 80 C48 64 70 58 82 64 C96 70 104 58 98 44 C94 34 82 30 72 36 L64 28 L66 40 C56 44 50 52 52 60'
        style={stroke('ink', 14, 0.9)}
      />
      <path
        d='M72 36 L84 20 L82 38 Z M90 40 L106 30 L96 46 Z'
        style={ink('ink')}
      />
      <path
        d='M30 92 C40 84 44 76 46 66'
        style={stroke('acc-violet', 3, 0.7)}
      />
      <circle cx='88' cy='46' r='3' style={ink('red')} />
      <path d='M98 54 L112 60 M98 56 L110 66' style={stroke('mtn-3', 2)} />
    </>
  ),
  lich: () => (
    <>
      <path
        d='M30 114 C28 84 36 64 46 56 L74 56 C84 64 92 84 90 114 C80 108 70 114 60 110 C50 114 40 108 30 114 Z'
        style={ink('ink', 0.9)}
      />
      <path
        d='M44 40 C44 22 76 22 76 40 C76 52 70 56 68 60 L52 60 C50 56 44 52 44 40 Z'
        style={ink('bone')}
      />
      <path
        d='M42 26 L48 12 L54 22 L60 8 L66 22 L72 12 L78 26 Z'
        style={ink('red', 0.85)}
      />
      <Eyes y={40} gap={8} r={3.6} color='acc-ice' />
      <path d='M98 30 L98 112' style={stroke('bone', 4)} />
      <circle cx='98' cy='26' r='7' style={ink('acc-ice', 0.8)} />
    </>
  ),
  abyss_lord: () => (
    <>
      <path
        d='M6 118 C10 70 26 34 60 26 C94 34 110 70 114 118 C98 110 90 118 76 112 C68 118 52 118 44 112 C30 118 22 110 6 118 Z'
        style={ink('ink', 0.92)}
      />
      <path
        d='M36 70 C36 40 84 40 84 70 C78 62 42 62 36 70 Z'
        style={ink('acc-violet', 0.4)}
      />
      <SlitEyes y={62} gap={13} />
      <circle cx='60' cy='92' r='9' style={stroke('red', 2.5, 0.8)} />
      <g style={stroke('acc-violet', 2, 0.6)}>
        <path d='M20 40 C30 30 34 18 30 8 M100 40 C90 30 86 18 90 8' />
      </g>
    </>
  ),
  // —— 第四章 · 万妖林 ——
  treant: () => (
    <>
      <path
        d='M44 112 C46 92 40 70 44 52 L76 52 C80 70 74 92 76 112 L66 112 L60 96 L54 112 Z'
        style={ink('mtn-3', 0.95)}
      />
      <path
        d='M44 66 C30 62 20 68 14 84 M76 66 C90 62 100 68 106 84'
        style={stroke('mtn-3', 7)}
      />
      <path
        d='M14 84 L8 94 M14 84 L20 96 M106 84 L100 96 M106 84 L112 94'
        style={stroke('mtn-3', 3)}
      />
      <ellipse cx='60' cy='32' rx='36' ry='20' style={ink('acc-green', 0.85)} />
      <ellipse cx='32' cy='44' rx='18' ry='12' style={ink('acc-green', 0.7)} />
      <ellipse cx='88' cy='44' rx='18' ry='12' style={ink('acc-green', 0.7)} />
      <ellipse cx='60' cy='64' rx='13' ry='15' style={ink('ink', 0.92)} />
      <Eyes y={60} gap={6} r={2.4} />
      <path d='M53 72 Q60 77 67 72' style={stroke('paper', 2, 0.7)} />
      <path d='M48 82 L52 96 M72 84 L68 100' style={stroke('ink', 1.6, 0.5)} />
    </>
  ),
  viper: () => (
    <>
      <path
        d='M24 106 C8 92 28 78 52 88 C78 98 100 88 92 72 C86 58 58 62 58 46'
        style={stroke('acc-green', 15, 0.92)}
      />
      <path
        d='M24 106 C8 92 28 78 52 88 C78 98 100 88 92 72 C86 58 58 62 58 46'
        style={stroke('ink', 4, 0.4)}
      />
      <path
        d='M40 92 L46 88 L50 94 L44 98 Z M64 96 L70 92 L74 98 L68 102 Z M86 86 L92 82 L96 88 L90 92 Z'
        style={ink('ink', 0.6)}
      />
      <path
        d='M36 40 C32 20 84 20 80 40 C78 54 68 62 58 62 C48 62 38 54 36 40 Z'
        style={ink('acc-green')}
      />
      <path d='M44 38 C46 28 70 28 72 38' style={stroke('ink', 2.5, 0.55)} />
      <path
        d='M46 44 L56 47 L47 49 Z M70 44 L60 47 L69 49 Z'
        style={ink('red')}
      />
      <path
        d='M58 62 L58 72 M58 72 L52 80 M58 72 L64 80'
        style={stroke('red', 2)}
      />
    </>
  ),
  fox_guard: () => (
    <>
      <path
        d='M86 104 C110 100 114 72 100 58 C104 78 98 90 80 94 Z'
        style={ink('acc-fire', 0.85)}
      />
      <path
        d='M32 112 L36 78 L28 62 L92 62 L84 78 L88 112 Z'
        style={ink('ink', 0.92)}
      />
      <path d='M44 68 L76 68 L72 82 L48 82 Z' style={ink('acc-stone', 0.85)} />
      <path
        d='M34 52 L28 20 L50 40 Z M86 52 L92 20 L70 40 Z'
        style={ink('acc-fire', 0.92)}
      />
      <path
        d='M38 56 C38 32 82 32 82 56 C82 64 70 72 60 80 C50 72 38 64 38 56 Z'
        style={ink('acc-fire', 0.92)}
      />
      <path
        d='M50 66 L60 80 L70 66 C65 71 55 71 50 66 Z'
        style={ink('paper', 0.92)}
      />
      <SlitEyes y={52} gap={10} />
      <path d='M60 32 L60 18' style={stroke('red', 3)} />
      <path d='M102 22 L102 112' style={stroke('mtn-2', 4)} />
      <path d='M96 28 L102 12 L108 28 Z' style={ink('mtn-3')} />
    </>
  ),
  bear: () => (
    <>
      <path
        d='M18 112 C12 76 26 52 60 50 C94 52 108 76 102 112 Z'
        style={ink('ink', 0.92)}
      />
      <circle cx='36' cy='36' r='11' style={ink('ink', 0.92)} />
      <circle cx='84' cy='36' r='11' style={ink('ink', 0.92)} />
      <ellipse cx='60' cy='52' rx='28' ry='24' style={ink('ink', 0.96)} />
      <path
        d='M44 62 C44 54 76 54 76 62 C76 72 68 78 60 78 C52 78 44 72 44 62 Z'
        style={ink('bone', 0.82)}
      />
      <ellipse cx='60' cy='61' rx='5' ry='3.5' style={ink('ink')} />
      <Eyes y={46} gap={11} r={2.8} />
      <path d='M40 88 C46 100 74 100 80 88' style={stroke('mtn-3', 3, 0.7)} />
      <path
        d='M12 110 L16 96 L21 110 Z M23 110 L27 96 L32 110 Z M88 110 L93 96 L97 110 Z M99 110 L104 96 L108 110 Z'
        style={ink('bone')}
      />
      <path
        d='M106 70 C116 76 116 88 106 94 M102 94 L116 102'
        style={stroke('acc-stone', 4)}
      />
    </>
  ),
  spider: () => (
    <>
      <path d='M60 0 L60 44' style={stroke('ink-3', 1.5, 0.7)} />
      <g style={stroke('ink', 4)}>
        <path d='M48 62 C30 52 20 58 12 78 M46 70 C28 66 18 78 14 96 M48 78 C34 84 28 96 28 110 M52 84 C46 94 44 102 44 112' />
        <path d='M72 62 C90 52 100 58 108 78 M74 70 C92 66 102 78 106 96 M72 78 C86 84 92 96 92 110 M68 84 C74 94 76 102 76 112' />
      </g>
      <ellipse cx='60' cy='84' rx='22' ry='26' style={ink('ink', 0.94)} />
      <path
        d='M60 62 L60 108 M48 72 L72 72 M46 84 L74 84 M48 96 L72 96'
        style={stroke('acc-violet', 2, 0.75)}
      />
      <ellipse cx='60' cy='56' rx='15' ry='12' style={ink('mtn-3')} />
      <g style={ink('red')}>
        <circle cx='54' cy='52' r='2.4' />
        <circle cx='66' cy='52' r='2.4' />
        <circle cx='49' cy='57' r='1.8' />
        <circle cx='71' cy='57' r='1.8' />
      </g>
      <path d='M54 65 L50 74 M66 65 L70 74' style={stroke('bone', 2.5)} />
    </>
  ),
  fox_emperor: () => (
    <>
      <g style={stroke('acc-violet', 6, 0.8)}>
        <path d='M60 98 C20 96 6 64 12 28' />
        <path d='M60 98 C26 98 16 70 24 38' />
        <path d='M60 98 C34 98 28 76 36 48' />
        <path d='M60 98 C44 96 42 76 48 54' />
        <path d='M60 98 C76 96 78 76 72 54' />
        <path d='M60 98 C86 98 92 76 84 48' />
        <path d='M60 98 C94 98 104 70 96 38' />
        <path d='M60 98 C100 96 114 64 108 28' />
      </g>
      <g style={ink('acc-ice', 0.9)}>
        <circle cx='12' cy='26' r='4.5' />
        <circle cx='24' cy='36' r='4.5' />
        <circle cx='36' cy='46' r='4.5' />
        <circle cx='84' cy='46' r='4.5' />
        <circle cx='96' cy='36' r='4.5' />
        <circle cx='108' cy='26' r='4.5' />
      </g>
      <path
        d='M30 114 C32 92 44 82 60 80 C76 82 88 92 90 114 Z'
        style={ink('ink', 0.94)}
      />
      <path d='M48 82 L60 100 L72 82' style={stroke('red', 3, 0.85)} />
      <path
        d='M34 58 L28 14 L52 40 Z M86 58 L92 14 L68 40 Z'
        style={ink('ink')}
      />
      <path
        d='M38 58 C38 34 82 34 82 58 C82 68 72 80 60 88 C48 80 38 68 38 58 Z'
        style={ink('paper', 0.94)}
      />
      <path
        d='M38 52 C40 40 52 36 60 40 C68 36 80 40 82 52 C72 44 48 44 38 52 Z'
        style={ink('ink', 0.9)}
      />
      <path d='M60 34 L57 42 L63 42 Z' style={ink('red')} />
      <SlitEyes y={58} gap={12} />
      <path d='M54 76 L60 82 L66 76' style={stroke('ink', 2, 0.8)} />
    </>
  ),

  // —— 第五章 · 九幽深处 ——
  nether_hound: () => (
    <>
      <path
        d='M12 98 C10 78 24 62 46 60 C62 58 74 62 80 68 L80 92 L70 92 L68 82 C58 84 44 84 36 82 L32 106 L22 106 L24 94 Z'
        style={ink('ink', 0.93)}
      />
      <path
        d='M70 70 C72 50 88 42 100 46 C108 48 114 56 114 62 L102 64 L112 76 L96 74 C90 80 80 82 72 80 Z'
        style={ink('ink', 0.96)}
      />
      <path d='M82 48 L80 26 L96 44 Z' style={ink('ink')} />
      <path
        d='M98 65 L100 71 L104 66 M106 67 L108 73 L111 68'
        style={stroke('paper', 1.6, 0.9)}
      />
      <circle cx='94' cy='54' r='2.8' style={ink('red')} />
      <path
        d='M26 62 C22 48 34 44 30 32 C42 40 40 50 46 58 M48 58 C46 46 56 42 54 30 C64 40 60 50 66 58'
        style={stroke('acc-violet', 4, 0.85)}
      />
      <path d='M12 92 C2 88 0 76 6 70' style={stroke('ink', 5, 0.9)} />
    </>
  ),
  hungry_ghost: () => (
    <>
      <g style={stroke('ink-2', 5)}>
        <path d='M32 64 C18 72 14 88 18 100 M88 64 C102 72 106 88 102 100' />
        <path d='M48 100 L44 114 M72 100 L76 114' />
      </g>
      <path
        d='M60 42 C30 44 24 70 28 88 C32 106 88 106 92 88 C96 70 90 44 60 42 Z'
        style={ink('ink-3', 0.8)}
      />
      <path
        d='M40 64 C50 72 70 72 80 64 M38 76 C50 86 70 86 82 76'
        style={stroke('ink', 2, 0.55)}
      />
      <ellipse cx='60' cy='30' rx='13' ry='15' style={ink('bone')} />
      <Eyes y={27} gap={6} r={3.2} />
      <path d='M50 38 Q60 46 70 38 Z' style={ink('ink', 0.9)} />
      <path
        d='M53 38 L55 42 L57 38 M60 38 L62 43 L64 38'
        style={stroke('bone', 1.5)}
      />
    </>
  ),
  judge: () => (
    <>
      <path
        d='M24 114 C26 86 40 72 60 70 C80 72 94 86 96 114 Z'
        style={ink('robe-red', 0.95)}
      />
      <path d='M50 72 L60 92 L70 72' style={stroke('paper', 3, 0.85)} />
      <path d='M38 100 L82 100' style={stroke('ink', 3, 0.6)} />
      <ellipse cx='60' cy='52' rx='15' ry='17' style={ink('ink', 0.92)} />
      <path
        d='M50 62 C52 76 56 84 60 88 C64 84 68 76 70 62 C64 68 56 68 50 62 Z'
        style={ink('paper', 0.9)}
      />
      <path
        d='M44 42 L76 42 L72 28 L48 28 Z M44 38 L22 32 L22 40 L44 44 Z M76 38 L98 32 L98 40 L76 44 Z'
        style={ink('ink')}
      />
      <path d='M46 36 L74 36' style={stroke('red', 2.5)} />
      <SlitEyes y={52} gap={8} />
      <path d='M98 72 L106 38' style={stroke('mtn-3', 3)} />
      <path d='M104 44 L110 34 L100 38 Z' style={ink('red')} />
      <rect
        x='8'
        y='76'
        width='22'
        height='28'
        rx='2'
        style={ink('bone', 0.92)}
      />
      <path
        d='M13 84 L25 84 M13 91 L25 91 M13 98 L21 98'
        style={stroke('ink', 1.5, 0.7)}
      />
    </>
  ),
  soul_lord: () => (
    <>
      <path
        d='M12 114 C8 70 30 40 60 38 C90 40 112 70 108 114 C94 104 86 114 74 106 C64 114 54 114 46 106 C34 114 26 104 12 114 Z'
        style={ink('ink', 0.94)}
      />
      <path
        d='M34 46 C24 38 22 22 28 10 C30 24 38 32 44 40 Z M86 46 C96 38 98 22 92 10 C90 24 82 32 76 40 Z'
        style={ink('ink')}
      />
      <path
        d='M34 80 C44 102 76 102 86 80 C76 92 44 92 34 80 Z'
        style={ink('red', 0.82)}
      />
      <path
        d='M40 84 L44 94 L48 86 Z M52 90 L56 100 L60 92 Z M62 92 L66 100 L70 90 Z M72 86 L76 94 L80 84 Z'
        style={ink('paper', 0.92)}
      />
      <SlitEyes y={58} gap={16} color='acc-violet' />
      <g style={ink('acc-violet', 0.8)}>
        <circle cx='18' cy='50' r='7' />
        <circle cx='102' cy='46' r='7' />
        <circle cx='60' cy='18' r='6' />
      </g>
      <g style={ink('paper', 0.9)}>
        <circle cx='16' cy='49' r='1.4' />
        <circle cx='21' cy='49' r='1.4' />
        <circle cx='100' cy='45' r='1.4' />
        <circle cx='105' cy='45' r='1.4' />
        <circle cx='58' cy='17' r='1.3' />
        <circle cx='62' cy='17' r='1.3' />
      </g>
    </>
  ),
  taixu: () => (
    <>
      <path
        d='M12 66 C6 46 18 36 14 18 M108 66 C114 46 102 36 106 18'
        style={stroke('acc-violet', 3, 0.75)}
      />
      <path
        d='M24 114 C26 86 38 72 60 70 C82 72 94 86 96 114 Z'
        style={ink('robe-white')}
      />
      <path
        d='M24 114 L24 100 C34 106 40 96 48 102 C58 110 66 98 74 104 C84 110 90 100 96 100 L96 114 Z'
        style={ink('ink', 0.92)}
      />
      <path d='M52 72 L60 90 L68 72' style={stroke('ink', 2.5, 0.7)} />
      <ellipse cx='60' cy='52' rx='14' ry='17' style={ink('skin')} />
      <path
        d='M47 60 C49 78 55 90 60 96 C65 90 71 78 73 60 C67 68 53 68 47 60 Z'
        style={ink('white-hair')}
      />
      <path
        d='M46 38 L46 22 L53 28 L60 12 L67 28 L74 22 L74 38 Z'
        style={ink('ink')}
      />
      <path d='M44 33 L76 33' style={stroke('red', 2)} />
      <path
        d='M49 50 L56 52 L55 55 Z M71 50 L64 52 L65 55 Z'
        style={ink('red')}
      />
      <path d='M102 28 L102 108' style={stroke('mtn-2', 4)} />
      <path d='M95 38 L109 38' style={stroke('red', 3)} />
    </>
  ),
  patriarch: () => (
    <>
      <circle cx='60' cy='50' r='42' style={stroke('red', 2.5, 0.7)} />
      <circle cx='60' cy='50' r='36' style={ink('ink', 0.16)} />
      <path
        d='M6 118 C8 74 26 44 60 34 C94 44 112 74 114 118 C98 110 90 118 78 112 C68 118 52 118 42 112 C28 118 22 110 6 118 Z'
        style={ink('ink', 0.96)}
      />
      <path
        d='M44 26 L44 12 L52 20 L60 6 L68 20 L76 12 L76 26 Z'
        style={ink('ink')}
      />
      <path d='M42 24 L78 24' style={stroke('red', 2.5)} />
      <path
        d='M48 24 L48 34 M56 24 L56 32 M64 24 L64 32 M72 24 L72 34'
        style={stroke('red', 1.6, 0.85)}
      />
      <path
        d='M42 56 C42 38 78 38 78 56 C78 70 70 80 60 84 C50 80 42 70 42 56 Z'
        style={ink('bone', 0.94)}
      />
      <SlitEyes y={56} gap={9} />
      <path
        d='M52 72 L56 76 L60 72 L64 76 L68 72'
        style={stroke('ink', 2, 0.8)}
      />
      <circle cx='60' cy='98' r='11' style={ink('ink')} />
      <circle cx='60' cy='98' r='11' style={stroke('red', 2.5, 0.9)} />
      <path
        d='M30 100 C38 94 46 96 50 100 M90 100 C82 94 74 96 70 100'
        style={stroke('bone', 4)}
      />
      <g style={stroke('acc-violet', 2, 0.6)}>
        <path d='M18 80 C28 70 30 58 24 48 M102 80 C92 70 90 58 96 48' />
      </g>
    </>
  ),
  dummy: () => (
    <>
      <path d='M56 112 L56 34 L64 34 L64 112 Z' style={ink('mtn-3')} />
      <path d='M30 56 L90 56' style={stroke('mtn-3', 8)} />
      <circle cx='60' cy='30' r='14' style={ink('mtn-2')} />
      <path d='M46 112 L74 112' style={stroke('ink', 6)} />
      <g style={stroke('ink', 1.5, 0.5)}>
        <path d='M60 70 L60 100 M40 56 L44 56 M76 56 L80 56' />
      </g>
    </>
  ),
};

// EnemyArt 是一个敌人的画像；不认识的敌人画一团墨影。
const EnemyArt = ({ id, className = '' }) => {
  const Art = ART[id];
  return (
    <svg className={`wd-enemy ${className}`} viewBox='0 0 120 120' aria-hidden>
      <ellipse cx='60' cy='114' rx='40' ry='5' style={ink('ink', 0.12)} />
      {Art ? (
        <Art />
      ) : (
        <>
          <circle cx='60' cy='66' r='34' style={ink('ink', 0.8)} />
          <Eyes y={62} />
        </>
      )}
    </svg>
  );
};

export default EnemyArt;
