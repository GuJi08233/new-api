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
import { useTranslation } from 'react-i18next';

// 桌面上的下注格：注型与号码(与 pkg/roulette 的 Bet 一致：单号、打、列带号码，其他注型为 0)，以及宽屏横排(h)与
// 窄屏竖排(v)时所在的网格 [列, 行, 跨几列, 跨几行]。横排是标准桌面：0 在最左，号码 3 行 12 列，右边三格是列，
// 下面两排是打与一赔一的外围注；竖排把它转过来，号码 12 行 3 列，外围注排在右边两列。
export const SPOTS = [
  { type: 'straight', value: 0, h: [1, 1, 1, 3], v: [1, 1, 3, 1] },
  ...Array.from({ length: 36 }, (_, index) => {
    const column = (index % 3) + 1;
    const group = Math.floor(index / 3) + 1;
    return {
      type: 'straight',
      value: index + 1,
      h: [group + 1, 4 - column, 1, 1],
      v: [column, group + 1, 1, 1],
    };
  }),
  ...[1, 2, 3].map((value) => ({
    type: 'column',
    value,
    h: [14, 4 - value, 1, 1],
    v: [value, 14, 1, 1],
  })),
  ...[1, 2, 3].map((value) => ({
    type: 'dozen',
    value,
    h: [4 * value - 2, 4, 4, 1],
    v: [4, 4 * value - 2, 1, 4],
  })),
  ...['low', 'even', 'red', 'black', 'odd', 'high'].map((type, index) => ({
    type,
    value: 0,
    h: [2 * index + 2, 5, 2, 1],
    v: [5, 2 * index + 2, 1, 2],
  })),
].map((spot) => ({ ...spot, key: `${spot.type}-${spot.value}` }));

// 一注的名字：号码、红黑、单双、区间或第几列。
function betName(spot, t) {
  switch (spot.type) {
    case 'straight':
      return String(spot.value);
    case 'red':
      return t('红色');
    case 'black':
      return t('黑色');
    case 'odd':
      return t('单数');
    case 'even':
      return t('双数');
    case 'low':
      return '1–18';
    case 'high':
      return '19–36';
    case 'dozen':
      return `${spot.value * 12 - 11}–${spot.value * 12}`;
    default:
      return t('第 {{value}} 列', { value: spot.value });
  }
}

// 下注桌面。bets 是每格押的游戏币(键是注型-号码)，wins 是上一次旋转每一注的派彩(桌面改动后为空)，hit 是开出的号码。
const RouletteTable = ({ colors, bets, wins, hit, disabled, onPlace }) => {
  const { t } = useTranslation();
  return (
    <div className='roulette-table'>
      {SPOTS.map((spot) => {
        const amount = bets[spot.key] || 0;
        const name = betName(spot, t);
        // 号码格按颜色着色；打与一赔一的外围注是跨几格的长条(is-group)；红、黑两格用号码的颜色。
        let className = 'roulette-spot';
        if (spot.type === 'straight') {
          className += ` is-number is-${colors[spot.value]}`;
        } else if (spot.type === 'column') {
          className += ' is-outside';
        } else if (spot.type === 'red' || spot.type === 'black') {
          className += ` is-group is-${spot.type}`;
        } else {
          className += ' is-group is-outside';
        }
        if (spot.type === 'straight' && spot.value === hit)
          className += ' is-hit';
        if (amount > 0 && wins) {
          className += wins[spot.key] > 0 ? ' is-won' : ' is-lost';
        }
        const [hc, hr, hcs, hrs] = spot.h;
        const [vc, vr, vcs, vrs] = spot.v;
        return (
          <button
            key={spot.key}
            type='button'
            className={className}
            style={{
              '--hc': hc,
              '--hr': hr,
              '--hcs': hcs,
              '--hrs': hrs,
              '--vc': vc,
              '--vr': vr,
              '--vcs': vcs,
              '--vrs': vrs,
            }}
            disabled={disabled}
            aria-label={
              amount > 0
                ? t('{{name}}，已下注 {{amount}}', { name, amount })
                : name
            }
            onClick={() => onPlace(spot)}
          >
            <span className='roulette-spot-label'>
              {spot.type === 'column' ? '2:1' : name}
            </span>
            {amount > 0 && <span className='roulette-stake'>{amount}</span>}
          </button>
        );
      })}
    </div>
  );
};

export default RouletteTable;
