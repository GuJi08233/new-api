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

import React, { useEffect } from 'react';
import { Button } from '@douyinfe/semi-ui';
import { Compass, Map, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CostChips, realmLabel, useSectText } from './parts';
import { REGION_IDS } from '../wendao/duel/text';

const OutpostCard = ({ outpost, sect, now, busy, act, text, t }) => {
  const region = outpost.region;
  const regionText = text.regions[REGION_IDS[region]];
  const members = (outpost.members || [])
    .map((id) => sect.disciples.find((d) => d.id === id))
    .filter(Boolean);
  const hall =
    sect.buildings.find((building) => building.key === 'hall')?.level || 0;
  const candidates = sect.disciples.filter(
    (d) =>
      !d.mission &&
      !d.outpost &&
      d.injured <= now &&
      d.realm >= (outpost.min_realm || 1) &&
      !outpost.busy?.includes(d.id),
  );
  const canBuild =
    outpost.reached &&
    !outpost.built &&
    hall >= outpost.hall_need &&
    outpost.cost &&
    Object.entries(outpost.cost).every(
      ([key, value]) => (sect.res?.[key] || 0) >= value,
    );
  return (
    <section
      id={`sc-outpost-${region}`}
      className={`sc-outpost${outpost.built ? ' is-built' : ''}`}
    >
      <header className='sc-outpost-head'>
        <div>
          <h3>
            <Map size={16} /> {regionText?.name}
          </h3>
          <p className='sc-muted'>
            {outpost.reached
              ? t('已到达，可在这里经营下院')
              : t('先在历练中通过这个地域的任意节点')}
          </p>
        </div>
        {outpost.built && <span className='sc-chip is-stone'>{t('已建')}</span>}
      </header>
      {!outpost.built ? (
        <div className='sc-outpost-build'>
          <p className='sc-building-desc'>
            {t(
              '主殿 {{hall}} 级后可建一座下院，最多驻守 {{slots}} 名筑基弟子。',
              {
                hall: outpost.hall_need || 3,
                slots: outpost.slots || 2,
              },
            )}
          </p>
          <CostChips cost={outpost.cost} have={sect.res} />
          <Button
            size='small'
            theme='solid'
            disabled={busy || !canBuild}
            onClick={() => act('build', { region })}
          >
            {outpost.reached ? t('建起下院') : t('尚未到达')}
          </Button>
        </div>
      ) : (
        <>
          <div className='sc-outpost-rate'>
            <span>{t('下院每小时产出')}</span>
            <span>
              {t('灵石 {{stone}} · 灵草 {{herb}} · 玄铁 {{ore}}', {
                stone: Number(outpost.rate?.stone || 0).toLocaleString(
                  undefined,
                  { maximumFractionDigits: 2 },
                ),
                herb: Number(outpost.rate?.herb || 0).toLocaleString(
                  undefined,
                  { maximumFractionDigits: 2 },
                ),
                ore: Number(outpost.rate?.ore || 0).toLocaleString(undefined, {
                  maximumFractionDigits: 2,
                }),
              })}
            </span>
          </div>
          <div className='sc-outpost-members'>
            <h4>
              <Users size={15} />{' '}
              {t('驻守 {{n}} / {{slots}}', {
                n: members.length,
                slots: outpost.slots || 2,
              })}
            </h4>
            {members.map((d) => (
              <div className='sc-outpost-member' key={d.id}>
                <span>
                  {text.name(d.name)} · {realmLabel(text, t, d.realm, d.layer)}
                </span>
                <Button
                  size='small'
                  theme='borderless'
                  disabled={busy}
                  onClick={() =>
                    act('recall', {
                      region,
                      disciple: d.id,
                      token: d.outpost_token,
                    })
                  }
                >
                  {t('召回')}
                </Button>
              </div>
            ))}
            {members.length === 0 && (
              <p className='sc-muted'>{t('暂无驻守弟子，不产生下院资源。')}</p>
            )}
          </div>
          {members.length < (outpost.slots || 2) && (
            <div className='sc-outpost-station'>
              <h4>
                <Users size={15} /> {t('可驻守弟子')}
              </h4>
              {candidates.length === 0 ? (
                <p className='sc-hint'>
                  {t(
                    '需要空闲、未受伤且已筑基的弟子；正在斗法的弟子需等结束。',
                  )}
                </p>
              ) : (
                candidates.map((d) => (
                  <div className='sc-outpost-member' key={d.id}>
                    <span>
                      {text.name(d.name)} ·{' '}
                      {realmLabel(text, t, d.realm, d.layer)}
                    </span>
                    <Button
                      size='small'
                      theme='solid'
                      disabled={busy}
                      onClick={() => act('station', { region, disciple: d.id })}
                    >
                      {t('派驻')}
                    </Button>
                  </div>
                ))
              )}
            </div>
          )}
          <p className='sc-hint'>
            {t(
              '驻守弟子半速修炼，资源并入本宗；普通差事、服丹、突破和出战需先召回。',
            )}
          </p>
        </>
      )}
    </section>
  );
};

export default function Outposts({ outposts, sect, now, busy, act, focus }) {
  const { t } = useTranslation();
  const text = useSectText();
  const ready = !!outposts;
  useEffect(() => {
    if (!ready || focus?.tab !== 'outposts' || !focus.region) return;
    document
      .getElementById(`sc-outpost-${focus.region}`)
      ?.scrollIntoView({ block: 'center' });
  }, [ready, focus]);
  if (!outposts || !sect) {
    return <p className='sc-hint'>{t('开宗立派后才能经营下院。')}</p>;
  }
  return (
    <div className='sc-outposts'>
      <div className='sc-heading'>
        <Compass size={16} />
        {t('异地下院')}
        <span className='sc-muted'>
          {t('建院后永久保留；驻守资源统一进入本宗库房。')}
        </span>
      </div>
      <div className='sc-outpost-total'>
        <strong>{t('下院合计')}</strong>
        <span>
          {t('灵石 {{stone}} · 灵草 {{herb}} · 玄铁 {{ore}}', {
            stone: Number(outposts.total?.stone || 0).toLocaleString(
              undefined,
              { maximumFractionDigits: 2 },
            ),
            herb: Number(outposts.total?.herb || 0).toLocaleString(undefined, {
              maximumFractionDigits: 2,
            }),
            ore: Number(outposts.total?.ore || 0).toLocaleString(undefined, {
              maximumFractionDigits: 2,
            }),
          })}
        </span>
        <span className='sc-muted'>{t('每小时')}</span>
      </div>
      {outposts.regions?.map((region) => (
        <OutpostCard
          key={region.region}
          outpost={{
            ...region,
            hall_need: outposts.hall_need,
            min_realm: outposts.min_realm,
            slots: outposts.slots,
            busy: outposts.busy,
          }}
          sect={sect}
          now={now}
          busy={busy}
          act={act}
          text={text}
          t={t}
        />
      ))}
    </div>
  );
}
