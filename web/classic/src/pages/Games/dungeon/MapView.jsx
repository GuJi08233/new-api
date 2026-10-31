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
import { Tooltip } from '@douyinfe/semi-ui';
import {
  CircleHelp,
  Crown,
  Gift,
  Skull,
  Store,
  Swords,
  Tent,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getNodeNames } from './text';

// 与 pkg/dungeon 一致：每幕 8 层、每层最多 4 列，最上面是首领。
const ROWS = 8;
const COLS = 4;

const NODE_ICONS = {
  combat: Swords,
  elite: Skull,
  boss: Crown,
  shop: Store,
  rest: Tent,
  event: CircleHelp,
  treasure: Gift,
};

// 节点在路线图里的位置(百分比)：第一层在最下面，首领在最上面。
const position = (node) => ({
  x: ((node.col + 0.5) / COLS) * 100,
  y: 100 - ((node.row + 0.5) / (ROWS + 1)) * 100,
});

// 这一幕的路线图：走过的路线点亮，下一步能去的节点闪动，点一下就出发。
const MapView = ({ run, busy, onPick }) => {
  const { t } = useTranslation();
  const names = getNodeNames(t);
  const choices = new Set(run.choices);
  const visited = new Set(run.path);
  const pos = run.path.length ? run.path[run.path.length - 1] : -1;
  const walked = new Set(
    run.path.slice(1).map((to, index) => `${run.path[index]}-${to}`),
  );
  return (
    <div className='dg-map-wrap'>
      <div className='dg-map-title'>
        {t('第 {{act}} 幕', { act: run.act })}
        <span>
          {choices.size
            ? t('选择下一个要去的地点')
            : t('沿着路线前进，最终击败首领')}
        </span>
      </div>
      <div className='dg-map' role='group' aria-label={t('路线图')}>
        <svg
          className='dg-map-lines'
          viewBox='0 0 100 100'
          preserveAspectRatio='none'
          aria-hidden
        >
          {run.map.map((node, from) =>
            node.next.map((to) => {
              const a = position(node);
              const b = position(run.map[to]);
              const key = `${from}-${to}`;
              const open = from === pos && choices.has(to);
              return (
                <line
                  key={key}
                  x1={a.x}
                  y1={a.y}
                  x2={b.x}
                  y2={b.y}
                  className={
                    walked.has(key) ? 'is-walked' : open ? 'is-open' : ''
                  }
                  vectorEffect='non-scaling-stroke'
                />
              );
            }),
          )}
        </svg>
        {run.map.map((node, id) => {
          const Icon = NODE_ICONS[node.type] || Swords;
          const { x, y } = position(node);
          const classes = [
            'dg-node',
            `is-${node.type}`,
            choices.has(id) ? 'is-choice' : '',
            visited.has(id) ? 'is-visited' : '',
            id === pos ? 'is-current' : '',
          ];
          return (
            <Tooltip key={id} content={names[node.type]}>
              <button
                type='button'
                className={classes.join(' ')}
                style={{ left: `${x}%`, top: `${y}%` }}
                disabled={busy || !choices.has(id)}
                aria-label={names[node.type]}
                onClick={() => onPick(id)}
              >
                <Icon size={node.type === 'boss' ? 26 : 18} />
              </button>
            </Tooltip>
          );
        })}
      </div>
      <div className='dg-map-legend'>
        {Object.entries(NODE_ICONS).map(([type, Icon]) => (
          <span key={type} className={`is-${type}`}>
            <Icon size={14} />
            {names[type]}
          </span>
        ))}
      </div>
    </div>
  );
};

export default MapView;
