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

import React, { useState } from 'react';
import { Button } from '@douyinfe/semi-ui';
import { Anvil, Droplets, Flame, Wind } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CostChips, canAfford } from '../../sect/parts';
import CaveArt from '../art/CaveArt';
import Forge from './Forge';
import Furnace from './Furnace';
import { getRoomText, roomEffect } from './text';
import './cave.css';

const ROOM_ICONS = {
  quiet: Wind,
  spring: Droplets,
  furnace: Flame,
  forge: Anvil,
};

// 洞府的屋子：等级、现在的效果、下一级的效果与花费。等级受主角境界限制(room_cap)，到顶或境界不够时提示原因。
const Rooms = ({ info, hero, res, busy, act }) => {
  const { t } = useTranslation();
  const rooms = getRoomText(t);
  const cave = info.cave;
  return (
    <div className='cv-rooms'>
      {cave.rooms.map((room) => {
        const Icon = ROOM_ICONS[room];
        const level = hero.cave[room] || 0;
        const next = level + 1;
        const maxed = level >= cave.max_level;
        const capped = !maxed && next > hero.room_cap;
        const cost = cave.costs[room][level];
        return (
          <div key={room} className={`cv-room ${level ? '' : 'is-empty'}`}>
            <div className='cv-room-head'>
              <Icon size={18} />
              <strong>{rooms[room].name}</strong>
              <span className='cv-level'>
                {level ? t('{{n}} 级', { n: level }) : t('未开辟')}
              </span>
            </div>
            <p className='cv-room-desc'>{rooms[room].desc}</p>
            <p className='cv-room-effect'>
              {roomEffect(t, info, room, level)}
              {!maxed && (
                <span className='cv-next'>
                  {' → '}
                  {roomEffect(t, info, room, next)}
                </span>
              )}
            </p>
            {maxed ? (
              <span className='jn-muted'>{t('已到最高等级')}</span>
            ) : capped ? (
              <span className='jn-muted'>
                {t('境界不足：现在最多建到 {{n}} 级', { n: hero.room_cap })}
              </span>
            ) : (
              <div className='cv-room-foot'>
                <CostChips cost={cost} have={res} />
                <Button
                  size='small'
                  theme='solid'
                  disabled={busy || !canAfford(cost, res)}
                  onClick={() => act('room', { item: room })}
                >
                  {level ? t('扩建') : t('开辟')}
                </Button>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
};

// 洞府：宗主本人修行的地方。顶上一幅洞府图，开辟了的屋子点亮；下面分屋子、丹室、器室几块。
const Cave = ({ info, hero, res, rare, now, busy, act }) => {
  const { t } = useTranslation();
  const [view, setView] = useState('rooms');
  const views = [
    { key: 'rooms', label: t('屋子') },
    { key: 'furnace', label: t('丹室') },
    { key: 'forge', label: t('器室') },
  ];
  return (
    <div className='cv-cave'>
      <div className='cv-hero-art'>
        <CaveArt levels={hero.cave} />
      </div>
      <div className='cv-views' role='tablist'>
        {views.map(({ key, label }) => (
          <button
            type='button'
            role='tab'
            key={key}
            aria-selected={view === key}
            className={`cv-view ${view === key ? 'is-active' : ''}`}
            onClick={() => setView(key)}
          >
            {label}
          </button>
        ))}
      </div>
      {view === 'rooms' && (
        <>
          <p className='jn-muted'>
            {t(
              '洞府的屋子花宗门库房的资源扩建，境界越高能建得越高；洞府给的加成只在历练里生效，带不进九幽秘境',
            )}
          </p>
          <Rooms info={info} hero={hero} res={res} busy={busy} act={act} />
        </>
      )}
      {view === 'furnace' && (
        <Furnace
          info={info}
          hero={hero}
          res={res}
          rare={rare}
          now={now}
          busy={busy}
          act={act}
        />
      )}
      {view === 'forge' && (
        <Forge
          info={info}
          hero={hero}
          res={res}
          rare={rare}
          busy={busy}
          act={act}
        />
      )}
    </div>
  );
};

export default Cave;
