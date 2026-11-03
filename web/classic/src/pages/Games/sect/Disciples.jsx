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
import { Button, Modal, Select } from '@douyinfe/semi-ui';
import { RefreshCw, UserPlus, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { JOBS } from './text';
import { useWendaoShell } from '../wendao/shell';
import { REGION_IDS } from '../wendao/duel/text';
import LineageDetails from './LineageDetails';
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
  if (d.outpost) {
    return {
      kind: 'away',
      label: t('驻守{{region}}下院', {
        region: text.regions[REGION_IDS[d.outpost]]?.name,
      }),
    };
  }
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
  const { go } = useWendaoShell();
  const home = !d.mission && !d.outpost && d.injured <= now;
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
      <LineageDetails
        disciple={d}
        disciples={sect.disciples}
        name={text.name}
        t={t}
      />
      {d.outpost > 0 && (
        <p className='sc-hint'>
          {t('驻守时半速修炼，普通差事、服丹、突破与出战需先召回。')}
          <Button
            size='small'
            theme='borderless'
            onClick={() =>
              go({ part: 'sect', tab: 'outposts', region: d.outpost })
            }
          >
            {t('前往下院')}
          </Button>
        </p>
      )}
      <div className='sc-disciple-actions'>
        <Select
          size='small'
          value={d.job}
          disabled={busy || !!d.outpost}
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
              disabled={busy || !!d.outpost}
              onClick={() => act('breakthrough', { disciple: d.id })}
            >
              {t('突破 {{chance}}%', { chance: d.break_chance })}
            </Button>
            {pills.break > 0 && (
              <Button
                size='small'
                theme='solid'
                type='warning'
                disabled={busy || !!d.outpost}
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
        {pills.qi > 0 && !d.mission && !d.outpost && !(d.break_chance > 0) && (
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
        {pills.peiyuan > 0 &&
          !d.mission &&
          !d.outpost &&
          !(d.break_chance > 0) && (
            <Button
              size='small'
              disabled={busy || qiWait > 0}
              onClick={() =>
                act('use_pill', { disciple: d.id, pill: 'peiyuan' })
              }
            >
              {qiWait > 0
                ? t('培元丹 {{time}}', { time: formatLeft(qiWait, t) })
                : t('服培元丹')}
            </Button>
          )}
        {pills.heal > 0 && d.injured > now && (
          <Button
            size='small'
            disabled={busy || !!d.outpost}
            onClick={() => act('use_pill', { disciple: d.id, pill: 'heal' })}
          >
            {t('服回春丹')}
          </Button>
        )}
        {pills.marrow > 0 && d.talent < 5 && (
          <Button
            size='small'
            disabled={busy || !!d.outpost}
            onClick={() => act('use_pill', { disciple: d.id, pill: 'marrow' })}
          >
            {t('服洗髓丹')}
          </Button>
        )}
        <span className='sc-spacer' />
        {!d.mission && !d.outpost && sect.disciples.length > 1 && (
          <Button
            size='small'
            type='tertiary'
            theme='borderless'
            disabled={busy || !!d.outpost}
            onClick={confirmDismiss}
          >
            {t('下山')}
          </Button>
        )}
      </div>
    </div>
  );
};

const CandidateCard = ({
  candidate,
  index,
  sect,
  lineage,
  now,
  busy,
  act,
  actLineage,
}) => {
  const { t } = useTranslation();
  const text = useSectText();
  const [teacherId, setTeacherId] = useState(null);
  const teachers = (lineage?.teachers || [])
    .filter((teacher) => teacher.students.length < lineage.slots)
    .map((teacher) => sect.disciples.find((d) => d.id === teacher.id))
    .filter(
      (d) =>
        d &&
        !d.mission &&
        !d.outpost &&
        d.injured <= now &&
        d.realm >= lineage.min_realm &&
        !lineage.busy?.includes(d.id),
    );
  const teacher = teachers.find((d) => d.id === teacherId);
  const full = sect.disciples.length >= sect.cap;
  const affordable = canAfford(candidate.cost, sect.res);
  return (
    <div className='sc-recruit'>
      <div className='sc-disciple-head'>
        <strong>{text.name(candidate.name)}</strong>
        <ElementTag element={candidate.element} />
        <TalentTag talent={candidate.talent} />
        <TraitTag trait={candidate.trait} />
      </div>
      <span className='sc-muted'>
        {realmLabel(text, t, 0, candidate.layer)} ·{' '}
        {t('战力 {{power}}', { power: candidate.power })}
      </span>
      <div className='sc-building-foot'>
        <CostChips cost={candidate.cost} have={sect.res} />
        <Button
          size='small'
          theme='solid'
          disabled={busy || full || !affordable}
          onClick={() => act('recruit', { index })}
        >
          {t('收为弟子')}
        </Button>
      </div>
      {lineage && (
        <div className='sc-lineage-recruit'>
          <Select
            aria-label={t('选择授业师长')}
            placeholder={t('选择授业师长')}
            value={teacher?.id ?? null}
            disabled={busy || teachers.length === 0}
            onChange={setTeacherId}
            optionList={teachers.map((d) => ({
              value: d.id,
              label: `${text.name(d.name)} · ${realmLabel(text, t, d.realm, d.layer)}`,
            }))}
            style={{ width: '100%' }}
          />
          <Button
            size='small'
            theme='light'
            disabled={busy || full || !affordable || !teacher || !candidate.sum}
            onClick={() =>
              actLineage('recruit', {
                index,
                teacher: teacher.id,
                candidate_sum: candidate.sum,
              })
            }
          >
            {t('收为亲传')}
          </Button>
          {teachers.length === 0 && (
            <p className='sc-muted'>
              {t('暂无可收徒的师长，请等金丹弟子空闲并回到本宗。')}
            </p>
          )}
        </div>
      )}
    </div>
  );
};

const Disciples = ({ sect, lineage, now, elapsed, busy, act, actLineage }) => {
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
      {lineage && (
        <p className='sc-muted'>
          {t(
            '金丹以上师长最多收 {{slots}} 名在宗亲传，占用原有弟子名额并支付相同招募费。',
            { slots: lineage.slots },
          )}{' '}
          {t(
            '师徒都在本宗、未受伤且师长境界更高时，徒弟自然修为 +{{pct}}%；丹药与派遣修为不加成。',
            { pct: lineage.bonus_pct },
          )}
        </p>
      )}
      <div className='sc-recruits'>
        {sect.recruits.map((c, index) =>
          c ? (
            <CandidateCard
              key={c.sum || index}
              candidate={c}
              index={index}
              sect={sect}
              lineage={lineage}
              now={now}
              busy={busy}
              act={act}
              actLineage={actLineage}
            />
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
