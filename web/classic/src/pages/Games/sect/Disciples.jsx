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
import { Button, Modal, Select } from '@douyinfe/semi-ui';
import { RefreshCw, UserPlus, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { JOBS } from './text';
import {
  Bar,
  CostChips,
  ElementTag,
  TalentTag,
  TraitTag,
  canAfford,
  formatLeft,
  realmLabel,
  useSectText,
} from './parts';

const RECRUIT_REFRESH_COST = { stone: 50 };

// 弟子现在在做什么：历练途中、受伤、修到瓶颈或者按差事修炼。
function discipleStatus(d, sect, now, text, t) {
  if (d.mission) {
    const mission = sect.missions.find((m) => m.id === d.mission);
    const name = text.missions[mission?.key]?.name;
    if (mission && mission.end <= now)
      return { kind: 'ready', label: t('{{name}}归来，待结算', { name }) };
    return {
      kind: 'away',
      label: t('{{name}}途中，还要 {{time}}', {
        name,
        time: formatLeft((mission?.end || now) - now, t),
      }),
    };
  }
  if (d.injured > now) {
    return {
      kind: 'hurt',
      label: t('受伤，{{time}} 后痊愈', {
        time: formatLeft(d.injured - now, t),
      }),
    };
  }
  if (d.break_chance > 0)
    return { kind: 'peak', label: t('第九层圆满，可以突破') };
  return { kind: '', label: text.jobs[d.job] };
}

const DiscipleCard = ({ d, sect, now, elapsed, busy, act }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const pills = sect.pills || {};
  const home = !d.mission && d.injured <= now;
  const status = discipleStatus(d, sect, now, text, t);
  // 两次同步之间按修炼速度把修为条往前推，到下一次同步时由服务端校准。
  const exp = Math.min(d.need, d.exp + (d.rate * elapsed) / 3600);
  const qiWait = d.qi_ready > now ? d.qi_ready - now : 0;

  const confirmDismiss = () =>
    Modal.confirm({
      title: t('让{{name}}下山', { name: text.name(d.name) }),
      content: t('下山之后就不再是本门弟子了，修为也带走。'),
      onOk: () => act('dismiss', { disciple: d.id }),
    });

  return (
    <div className={`sc-disciple${status.kind ? ` is-${status.kind}` : ''}`}>
      <div className='sc-disciple-head'>
        <strong>{text.name(d.name)}</strong>
        <ElementTag element={d.element} />
        <TalentTag talent={d.talent} />
        <TraitTag trait={d.trait} />
        <span className='sc-power'>
          {t('战力 {{power}}', { power: d.power })}
        </span>
      </div>
      <div className='sc-disciple-realm'>
        <span>{realmLabel(text, t, d.realm, d.layer)}</span>
        <Bar value={exp} max={d.need} kind={d.break_chance > 0 ? 'peak' : ''} />
        <span className='sc-muted'>
          {Math.floor(exp)} / {d.need}
          {d.rate > 0 && ` · +${d.rate}/${t('时')}`}
        </span>
      </div>
      <div className={`sc-status is-${status.kind || 'idle'}`}>
        {status.label}
      </div>
      <div className='sc-disciple-actions'>
        <Select
          size='small'
          value={d.job}
          disabled={busy}
          onChange={(job) => act('assign', { disciple: d.id, job })}
          optionList={JOBS.map((job) => ({
            value: job,
            label: text.jobs[job],
          }))}
          style={{ minWidth: 128, maxWidth: 200 }}
        />
        {d.break_chance > 0 && home && (
          <>
            <Button
              size='small'
              theme='solid'
              type='warning'
              disabled={busy}
              onClick={() => act('breakthrough', { disciple: d.id })}
            >
              {t('突破 {{chance}}%', { chance: d.break_chance })}
            </Button>
            {pills.break > 0 && (
              <Button
                size='small'
                theme='solid'
                type='warning'
                disabled={busy}
                onClick={() =>
                  act('breakthrough', { disciple: d.id, use_pill: true })
                }
              >
                {t('服破境丹突破 {{chance}}%', {
                  chance: Math.min(95, d.break_chance + 25),
                })}
              </Button>
            )}
          </>
        )}
        {pills.qi > 0 && !d.mission && !(d.break_chance > 0) && (
          <Button
            size='small'
            disabled={busy || qiWait > 0}
            onClick={() => act('use_pill', { disciple: d.id, pill: 'qi' })}
          >
            {qiWait > 0
              ? t('聚气丹 {{time}}', { time: formatLeft(qiWait, t) })
              : t('服聚气丹')}
          </Button>
        )}
        {pills.peiyuan > 0 && !d.mission && !(d.break_chance > 0) && (
          <Button
            size='small'
            disabled={busy || qiWait > 0}
            onClick={() => act('use_pill', { disciple: d.id, pill: 'peiyuan' })}
          >
            {qiWait > 0
              ? t('培元丹 {{time}}', { time: formatLeft(qiWait, t) })
              : t('服培元丹')}
          </Button>
        )}
        {pills.heal > 0 && d.injured > now && (
          <Button
            size='small'
            disabled={busy}
            onClick={() => act('use_pill', { disciple: d.id, pill: 'heal' })}
          >
            {t('服回春丹')}
          </Button>
        )}
        {pills.marrow > 0 && d.talent < 5 && (
          <Button
            size='small'
            disabled={busy}
            onClick={() => act('use_pill', { disciple: d.id, pill: 'marrow' })}
          >
            {t('服洗髓丹')}
          </Button>
        )}
        <span className='sc-spacer' />
        {!d.mission && sect.disciples.length > 1 && (
          <Button
            size='small'
            type='tertiary'
            theme='borderless'
            disabled={busy}
            onClick={confirmDismiss}
          >
            {t('下山')}
          </Button>
        )}
      </div>
    </div>
  );
};

const Disciples = ({ sect, now, elapsed, busy, act }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const full = sect.disciples.length >= sect.cap;
  return (
    <div className='sc-disciples'>
      <h4 className='sc-heading'>
        <Users size={16} />
        {t('弟子 {{n}} / {{cap}}', { n: sect.disciples.length, cap: sect.cap })}
        <span className='sc-muted'>
          {t('总战力 {{power}}', { power: sect.power })}
        </span>
      </h4>
      <div className='sc-disciple-list'>
        {sect.disciples.map((d) => (
          <DiscipleCard
            key={d.id}
            d={d}
            sect={sect}
            now={now}
            elapsed={elapsed}
            busy={busy}
            act={act}
          />
        ))}
      </div>
      <h4 className='sc-heading'>
        <UserPlus size={16} />
        {t('招收弟子')}
        <span className='sc-muted'>
          {t('{{time}} 后换一批', {
            time: formatLeft(sect.recruit_at - now, t),
          })}
        </span>
        <span className='sc-spacer' />
        <Button
          size='small'
          icon={<RefreshCw size={14} />}
          disabled={busy || !canAfford(RECRUIT_REFRESH_COST, sect.res)}
          onClick={() => act('refresh')}
        >
          <CostChips cost={RECRUIT_REFRESH_COST} />
        </Button>
      </h4>
      {full && (
        <p className='sc-hint'>
          {t('弟子已满，升级主殿可以多收弟子，或者让一名弟子下山')}
        </p>
      )}
      <div className='sc-recruits'>
        {sect.recruits.map((c, index) =>
          c ? (
            <div key={index} className='sc-recruit'>
              <div className='sc-disciple-head'>
                <strong>{text.name(c.name)}</strong>
                <ElementTag element={c.element} />
                <TalentTag talent={c.talent} />
                <TraitTag trait={c.trait} />
              </div>
              <span className='sc-muted'>
                {realmLabel(text, t, 0, c.layer)} ·{' '}
                {t('战力 {{power}}', { power: c.power })}
              </span>
              <div className='sc-building-foot'>
                <CostChips cost={c.cost} have={sect.res} />
                <Button
                  size='small'
                  theme='solid'
                  disabled={busy || full || !canAfford(c.cost, sect.res)}
                  onClick={() => act('recruit', { index })}
                >
                  {t('收为弟子')}
                </Button>
              </div>
            </div>
          ) : (
            <div key={index} className='sc-recruit is-empty'>
              {t('已收入门下')}
            </div>
          ),
        )}
      </div>
    </div>
  );
};

export default Disciples;
