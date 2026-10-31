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
import { Button } from '@douyinfe/semi-ui';
import {
  BookOpen,
  Building2,
  FlaskConical,
  Hammer,
  Landmark,
  Leaf,
  Sparkles,
  Swords,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  CostChips,
  canAfford,
  eventSentence,
  formatAgo,
  formatLeft,
  useSectText,
} from './parts';

const BUILDING_ICONS = {
  hall: Landmark,
  field: Leaf,
  mine: Hammer,
  array: Sparkles,
  alchemy: FlaskConical,
  library: BookOpen,
};

// 当前的奇遇：经过、两个选项的花费与收获；要比斗的选项写出对手与出战弟子的战力。
const EventCard = ({ sect, now, busy, act }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const event = sect.event;
  if (!event) {
    const left = sect.next_event_at - now;
    return (
      <div className='sc-event is-quiet'>
        <Sparkles size={18} />
        <span>
          {left > 0
            ? t('山中无事。下一件奇遇大约在 {{time}} 后', {
                time: formatLeft(left, t),
              })
            : t('山中无事，奇遇随时可能出现')}
        </span>
      </div>
    );
  }
  const info = text.events[event.key];
  const target = sect.disciples.find((d) => d.id === event.target);
  const fighter = sect.disciples.find((d) => d.id === event.fighter);
  return (
    <div className='sc-event'>
      <div className='sc-event-head'>
        <Sparkles size={18} />
        <strong>{info?.name}</strong>
        <span className='sc-muted'>
          {formatAgo(Math.max(0, now - event.at), t)}
        </span>
      </div>
      <p>
        {typeof info?.desc === 'function'
          ? info.desc(target ? text.name(target.name) : '')
          : info?.desc}
      </p>
      {event.threat > 0 && (
        <p className='sc-event-fight'>
          <Swords size={14} />
          {fighter
            ? t('对手战力 {{threat}}，出战的是{{name}}(战力 {{power}})', {
                threat: event.threat,
                name: text.name(fighter.name),
                power: fighter.power,
              })
            : t('对手战力 {{threat}}，宗门里没有能出战的弟子', {
                threat: event.threat,
              })}
        </p>
      )}
      <div className='sc-event-choices'>
        {[0, 1].map((choice) => (
          <div key={choice} className='sc-event-choice'>
            <Button
              theme={choice === 0 ? 'solid' : 'light'}
              disabled={busy || !event.choices[choice]}
              onClick={() => act('event', { choice })}
            >
              {info?.choices[choice]}
            </Button>
            <span className='sc-event-effects'>
              <CostChips cost={event.costs[choice]} have={sect.res} sign='-' />
              <CostChips cost={event.gains[choice]} sign='+' />
              {info?.notes[choice] && (
                <span className='sc-muted'>{info.notes[choice]}</span>
              )}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
};

const Overview = ({ sect, now, busy, act }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const hall = sect.buildings.find((b) => b.key === 'hall')?.level || 1;
  return (
    <div className='sc-overview'>
      <EventCard sect={sect} now={now} busy={busy} act={act} />
      <h4 className='sc-heading'>
        <Building2 size={16} />
        {t('宗门建筑')}
      </h4>
      <div className='sc-buildings'>
        {sect.buildings.map((b) => {
          const Icon = BUILDING_ICONS[b.key];
          const info = text.buildings[b.key];
          const affordable = b.cost && canAfford(b.cost, sect.res);
          return (
            <div
              key={b.key}
              className={`sc-building${b.level === 0 ? ' is-empty' : ''}`}
            >
              <div className='sc-building-head'>
                <Icon size={18} />
                <strong>{info?.name}</strong>
                <span className='sc-level'>
                  {b.level > 0
                    ? t('{{level}} 级', { level: b.level })
                    : t('未建')}
                </span>
              </div>
              <p className='sc-building-desc'>{info?.desc}</p>
              <div className='sc-building-foot'>
                {b.cost ? (
                  <>
                    <CostChips cost={b.cost} have={sect.res} />
                    <Button
                      size='small'
                      theme='solid'
                      disabled={busy || b.blocked || !affordable}
                      onClick={() => act('upgrade', { building: b.key })}
                    >
                      {b.level === 0 ? t('建造') : t('升级')}
                    </Button>
                  </>
                ) : (
                  <span className='sc-muted'>{t('已满级')}</span>
                )}
              </div>
              {b.blocked && (
                <span className='sc-hint'>
                  {t('先把主殿升到 {{level}} 级', { level: b.level + 1 })}
                </span>
              )}
            </div>
          );
        })}
      </div>
      <h4 className='sc-heading'>{t('宗门大事记')}</h4>
      {sect.log.length === 0 ? (
        <p className='sc-muted'>{t('宗门初立，还没有大事')}</p>
      ) : (
        <ul className='sc-log'>
          {[...sect.log].reverse().map((ev, index) => (
            <li key={`${ev.at}-${index}`}>
              <span className='sc-muted'>{formatAgo(now - ev.at, t)}</span>
              <span>{eventSentence(ev, text, t)}</span>
            </li>
          ))}
        </ul>
      )}
      <p className='sc-muted sc-foot-note'>
        {t('主殿 {{hall}} 级 · 同级门派战力约 {{power}}', {
          hall,
          power: sect.hall_power,
        })}
      </p>
    </div>
  );
};

export default Overview;
