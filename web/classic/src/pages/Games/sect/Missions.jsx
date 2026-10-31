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
import { Button, Checkbox, Modal } from '@douyinfe/semi-ui';
import { Clock, Compass, Lock } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  Bar,
  CostChips,
  ElementTag,
  formatLeft,
  pillName,
  realmLabel,
  useSectText,
} from './parts';

// 历练成功率，与 pkg/sect.MissionChance 一致：队伍总战力等于推荐战力时七成，按比例增减，五行相合每人 +10%，
// 藏经阁每级 +2%，夹在 10%~95%。
function missionChance(spec, team, library) {
  const total = team.reduce((sum, d) => sum + d.power, 0);
  let chance = Math.round((70 * total) / spec.power);
  chance += team.filter((d) => d.element === spec.element).length * 10;
  chance += library * 2;
  return Math.min(Math.max(chance, 10), 95);
}

function formatHours(seconds, t) {
  const hours = seconds / 3600;
  if (hours < 1)
    return t('{{count}} 分钟', { count: Math.round(seconds / 60) });
  return t('{{count}} 小时', { count: hours });
}

const ActiveMission = ({ mission, sect, now, busy, act }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const ready = mission.end <= now;
  const team = mission.team
    .map((id) => sect.disciples.find((d) => d.id === id))
    .filter(Boolean);
  const confirmRecall = () =>
    Modal.confirm({
      title: t('召回队伍'),
      content: t('提前召回的队伍没有任何收获。'),
      onOk: () => act('recall', { id: mission.id }),
    });
  return (
    <div className={`sc-mission is-active${ready ? ' is-ready' : ''}`}>
      <div className='sc-mission-head'>
        <Compass size={16} />
        <strong>{text.missions[mission.key]?.name}</strong>
        <span className='sc-muted'>
          {team.map((d) => text.name(d.name)).join('、')}
        </span>
      </div>
      <Bar
        value={now - mission.start}
        max={mission.end - mission.start}
        kind={ready ? 'peak' : ''}
      />
      <div className='sc-building-foot'>
        <span className='sc-muted'>
          {ready
            ? t('队伍已经归来')
            : t('还要 {{time}}', { time: formatLeft(mission.end - now, t) })}
        </span>
        {ready ? (
          <Button
            size='small'
            theme='solid'
            disabled={busy}
            onClick={() => act('claim', { id: mission.id })}
          >
            {t('结算收获')}
          </Button>
        ) : (
          <Button
            size='small'
            type='tertiary'
            disabled={busy}
            onClick={confirmRecall}
          >
            {t('召回')}
          </Button>
        )}
      </div>
    </div>
  );
};

const MissionCard = ({ spec, sect, now, busy, act, library }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const [picking, setPicking] = useState(false);
  const [team, setTeam] = useState([]);
  const running = sect.missions.some((m) => m.key === spec.key);
  const slotsFull = sect.missions.length >= sect.mission_slots;
  const home = sect.disciples.filter((d) => !d.mission && d.injured <= now);
  const chosen = home.filter((d) => team.includes(d.id));
  const info = text.missions[spec.key];

  const open = () => {
    // 默认挑战力最高、五行相合的弟子。
    const sorted = [...home].sort(
      (a, b) =>
        b.power +
        (b.element === spec.element ? 1e6 : 0) -
        (a.power + (a.element === spec.element ? 1e6 : 0)),
    );
    setTeam(sorted.slice(0, spec.size).map((d) => d.id));
    setPicking(true);
  };
  const toggle = (id) =>
    setTeam((previous) =>
      previous.includes(id)
        ? previous.filter((x) => x !== id)
        : previous.length < spec.size
          ? [...previous, id]
          : previous,
    );
  const dispatch = async () => {
    if (await act('dispatch', { mission: spec.key, team })) setPicking(false);
  };

  return (
    <div className={`sc-mission${spec.unlocked ? '' : ' is-locked'}`}>
      <div className='sc-mission-head'>
        {spec.unlocked ? <Compass size={16} /> : <Lock size={16} />}
        <strong>{info?.name}</strong>
        {spec.element >= 0 && <ElementTag element={spec.element} />}
        <span className='sc-muted'>
          <Clock size={13} /> {formatHours(spec.seconds, t)} ·{' '}
          {t('最多 {{n}} 人', { n: spec.size })} ·{' '}
          {t('推荐战力 {{power}}', { power: spec.power })}
        </span>
      </div>
      <p className='sc-building-desc'>{info?.desc}</p>
      <div className='sc-mission-reward'>
        <CostChips cost={spec.reward} sign='+' />
        {spec.drops?.map((drop) => (
          <span key={drop.pill} className='sc-chip is-pill'>
            {pillName(text, drop.pill)} {drop.pct}%
          </span>
        ))}
      </div>
      {!spec.unlocked ? (
        <span className='sc-hint'>
          {t('主殿 {{level}} 级解锁', { level: spec.hall })}
        </span>
      ) : picking ? (
        <div className='sc-picker'>
          {home.length === 0 && (
            <span className='sc-hint'>{t('宗门里没有空闲的弟子')}</span>
          )}
          {home.map((d) => (
            <Checkbox
              key={d.id}
              checked={team.includes(d.id)}
              onChange={() => toggle(d.id)}
            >
              <span className='sc-pick-name'>
                {text.name(d.name)}
                <ElementTag element={d.element} />
                <span className='sc-muted'>
                  {realmLabel(text, t, d.realm, d.layer)} ·{' '}
                  {t('战力 {{power}}', { power: d.power })}
                </span>
              </span>
            </Checkbox>
          ))}
          <div className='sc-building-foot'>
            <span>
              {t('成功率 {{chance}}%', {
                chance: chosen.length
                  ? missionChance(spec, chosen, library)
                  : 0,
              })}
            </span>
            <span className='sc-spacer' />
            <Button size='small' onClick={() => setPicking(false)}>
              {t('取消')}
            </Button>
            <Button
              size='small'
              theme='solid'
              disabled={busy || chosen.length === 0}
              onClick={dispatch}
            >
              {t('出发')}
            </Button>
          </div>
        </div>
      ) : (
        <div className='sc-building-foot'>
          <span className='sc-muted'>
            {running
              ? t('已经有一队在这里历练')
              : slotsFull
                ? t('派出的队伍已满')
                : ''}
          </span>
          <Button
            size='small'
            theme='solid'
            disabled={busy || running || slotsFull || home.length === 0}
            onClick={open}
          >
            {t('派遣')}
          </Button>
        </div>
      )}
    </div>
  );
};

const Missions = ({ sect, now, busy, act }) => {
  const { t } = useTranslation();
  const library = sect.buildings.find((b) => b.key === 'library')?.level || 0;
  return (
    <div className='sc-missions'>
      <h4 className='sc-heading'>
        <Compass size={16} />
        {t('历练队伍 {{n}} / {{slots}}', {
          n: sect.missions.length,
          slots: sect.mission_slots,
        })}
      </h4>
      {sect.missions.length === 0 ? (
        <p className='sc-muted'>
          {t('派弟子外出历练：成功带回资源与丹药，失败时有弟子受伤')}
        </p>
      ) : (
        <div className='sc-mission-list'>
          {sect.missions.map((m) => (
            <ActiveMission
              key={m.id}
              mission={m}
              sect={sect}
              now={now}
              busy={busy}
              act={act}
            />
          ))}
        </div>
      )}
      <h4 className='sc-heading'>{t('历练去处')}</h4>
      <div className='sc-mission-list'>
        {sect.catalog.map((spec) => (
          <MissionCard
            key={spec.key}
            spec={spec}
            sect={sect}
            now={now}
            busy={busy}
            act={act}
            library={library}
          />
        ))}
      </div>
    </div>
  );
};

export default Missions;
