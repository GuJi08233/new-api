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
import { Button, Select, Tag } from '@douyinfe/semi-ui';
import {
  ArrowLeft,
  Droplets,
  Hand,
  Lock,
  Sprout,
  Users,
  Wheat,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { gameGet } from '../common/api';
import CropArt from '../wendao/art/CropArt';
import {
  Bar,
  CostChips,
  canAfford,
  formatAgo,
  formatLeft,
  pillName,
  useSectText,
} from './parts';

// 生长阶段：空地、刚发芽、长到一半、熟了。
function plotStage(plot, now) {
  if (!plot.crop) return 0;
  if (now >= plot.ripe) return 3;
  const total = Math.max(1, plot.ripe - plot.planted);
  return (now - plot.planted) / total >= 0.4 ? 2 : 1;
}

// 一茬作物收获什么：灵草玄铁写成资源，丹药与灵药写成名字和份数。
const CropYield = ({ crop }) => {
  const text = useSectText();
  if (crop.pill)
    return (
      <span className='sc-chip is-pill'>
        {pillName(text, crop.pill)} ×{crop.n}
      </span>
    );
  if (crop.rare)
    return (
      <span className='sc-chip is-rare'>
        {text.rares[crop.rare]} ×{crop.n}
      </span>
    );
  return <CostChips cost={crop.yield} />;
};

const PlotCard = ({ plot, index, now, crops, sect, busy, act, visit }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const stage = plotStage(plot, now);
  const name = plot.crop ? text.crops[plot.crop]?.name : t('空地');
  const field = sect?.buildings.find((b) => b.key === 'field')?.level || 0;
  const options = (crops || [])
    .filter((crop) => field >= crop.field)
    .map((crop) => ({
      value: crop.key,
      label: `${text.crops[crop.key]?.name} · ${formatLeft(crop.seconds, t)}`,
      disabled: !canAfford(crop.seed, sect?.res),
    }));
  return (
    <div className={`sc-plot is-stage${stage}`}>
      <CropArt crop={plot.crop} stage={stage} />
      <strong className='sc-plot-name'>{name}</strong>
      {stage > 0 && stage < 3 && (
        <>
          <Bar
            value={now - plot.planted}
            max={Math.max(1, plot.ripe - plot.planted)}
          />
          <span className='sc-muted'>
            {t('{{time}}后成熟', { time: formatLeft(plot.ripe - now, t) })}
          </span>
        </>
      )}
      {stage === 3 && (
        <span className='sc-plot-ripe'>
          {plot.taken > 0
            ? t('已熟 · 被摘走 {{pct}}%', { pct: plot.taken })
            : t('已熟，可以收获')}
        </span>
      )}
      {plot.waters > 0 && (
        <span className='sc-plot-drops' title={t('道友浇灌次数')}>
          {Array.from({ length: plot.waters }, (_, i) => (
            <Droplets key={i} size={12} />
          ))}
        </span>
      )}
      <div className='sc-plot-actions'>
        {visit ? (
          <>
            {plot.can_water && (
              <Button
                size='small'
                theme='solid'
                icon={<Droplets size={14} />}
                disabled={busy || visit.waterLeft <= 0}
                onClick={() => visit.onVisit(index, 'water')}
              >
                {t('浇灌')}
              </Button>
            )}
            {plot.can_pick && (
              <Button
                size='small'
                type='warning'
                icon={<Hand size={14} />}
                disabled={busy || visit.pickLeft <= 0}
                onClick={() => visit.onVisit(index, 'pick')}
              >
                {t('顺手摘取')}
              </Button>
            )}
          </>
        ) : stage === 0 ? (
          <Select
            size='small'
            placeholder={t('播种…')}
            value={undefined}
            disabled={busy || options.length === 0}
            optionList={options}
            onChange={(crop) => act('plant', { index, crop })}
            style={{ width: '100%' }}
          />
        ) : stage === 3 ? (
          <Button
            size='small'
            theme='solid'
            icon={<Wheat size={14} />}
            disabled={busy}
            onClick={() => act('harvest', { index })}
          >
            {t('收获')}
          </Button>
        ) : (
          <Button
            size='small'
            type='tertiary'
            theme='borderless'
            disabled={busy}
            onClick={() => act('uproot', { index })}
          >
            {t('铲除')}
          </Button>
        )}
      </div>
    </div>
  );
};

// 灵种图鉴：解锁等级、生长时间、种子钱与收成，收过几茬；解锁了的可以一键种满空地。
const SeedList = ({ sect, garden, field, empty, busy, act }) => {
  const { t } = useTranslation();
  const text = useSectText();
  return (
    <div className='sc-mission-list'>
      {garden.crops.map((crop) => {
        const unlocked = field >= crop.field;
        return (
          <div
            key={crop.key}
            className={`sc-recipe sc-seed${unlocked ? '' : ' is-locked'}`}
          >
            <CropArt crop={crop.key} stage={3} />
            <div className='sc-seed-body'>
              <div className='sc-mission-head'>
                {unlocked ? <Sprout size={16} /> : <Lock size={16} />}
                <strong>{text.crops[crop.key]?.name}</strong>
                <span className='sc-muted'>{formatLeft(crop.seconds, t)}</span>
                {garden.harvests?.[crop.key] > 0 && (
                  <span className='sc-stock'>
                    {t('收过 {{n}} 茬', { n: garden.harvests[crop.key] })}
                  </span>
                )}
              </div>
              <p className='sc-building-desc'>{text.crops[crop.key]?.desc}</p>
              {unlocked ? (
                <div className='sc-building-foot'>
                  <span className='sc-muted'>{t('种子')}</span>
                  <CostChips cost={crop.seed} have={sect.res} />
                  <span className='sc-muted'>{t('收成')}</span>
                  <CropYield crop={crop} />
                  <span className='sc-spacer' />
                  <Button
                    size='small'
                    disabled={
                      busy || empty === 0 || !canAfford(crop.seed, sect.res)
                    }
                    onClick={() => act('plant_all', { crop: crop.key })}
                  >
                    {t('种满空地')}
                  </Button>
                </div>
              ) : (
                <span className='sc-hint'>
                  {t('灵田 {{level}} 级解锁', { level: crop.field })}
                </span>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
};

// 串门：道友列表，点进去看对方的灵田，浇灌或摘取。
const Neighbors = ({ now, busy, post, onEvents, onSync }) => {
  const { t } = useTranslation();
  const [list, setList] = useState(null);
  const [left, setLeft] = useState({ water: 0, pick: 0 });
  const [visiting, setVisiting] = useState(null);
  const [loading, setLoading] = useState(false);

  const load = async () => {
    setLoading(true);
    const response = await gameGet('/api/game/wendao/sect/garden/neighbors', t);
    setLoading(false);
    if (!response.data) return;
    setList(response.data.neighbors || []);
    setLeft({
      water: response.data.water_left,
      pick: response.data.pick_left,
    });
  };

  const open = async (neighbor) => {
    setLoading(true);
    const response = await gameGet(
      `/api/game/wendao/sect/garden/visit?user_id=${neighbor.user_id}`,
      t,
    );
    setLoading(false);
    if (response.data) setVisiting(response.data);
  };

  const onVisit = async (index, kind) => {
    const data = await post('/api/game/wendao/sect/garden/visit', {
      user_id: visiting.user_id,
      index,
      kind,
    });
    if (!data) return;
    setVisiting((previous) => ({ ...previous, plots: data.plots }));
    setLeft({ water: data.water_left, pick: data.pick_left });
    onEvents(data.events || []);
    onSync();
  };

  const counters = (
    <span className='sc-muted'>
      {t('今天还能浇灌 {{water}} 次、摘取 {{pick}} 次', left)}
    </span>
  );

  if (visiting) {
    return (
      <>
        <h4 className='sc-heading'>
          <Button
            size='small'
            theme='borderless'
            icon={<ArrowLeft size={14} />}
            onClick={() => {
              setVisiting(null);
              load();
            }}
          >
            {t('返回')}
          </Button>
          {t('{{name}}的灵田', { name: visiting.name })}
          {counters}
        </h4>
        <div className='sc-plots'>
          {visiting.plots.map((plot, index) => (
            <PlotCard
              key={index}
              plot={plot}
              index={index}
              now={now}
              busy={busy}
              visit={{
                onVisit,
                waterLeft: left.water,
                pickLeft: left.pick,
              }}
            />
          ))}
        </div>
      </>
    );
  }

  return (
    <>
      <h4 className='sc-heading'>
        <Users size={16} />
        {t('道友的灵田')}
        {list && counters}
        <span className='sc-spacer' />
        <Button size='small' loading={loading} onClick={load}>
          {list ? t('刷新') : t('去串门')}
        </Button>
      </h4>
      {list && list.length === 0 && (
        <p className='sc-hint'>{t('还没有别的掌门开宗立派')}</p>
      )}
      {list && list.length > 0 && (
        <div className='sc-neighbors'>
          {list.map((neighbor) => (
            <div key={neighbor.user_id} className='sc-neighbor'>
              <strong>{neighbor.name}</strong>
              {neighbor.ally && (
                <Tag size='small' color='green'>
                  {t('同盟')}
                </Tag>
              )}
              <span className='sc-muted'>
                {t('主殿 {{n}} 级', { n: neighbor.hall })}
              </span>
              {neighbor.water > 0 && (
                <span className='sc-chip is-water'>
                  <Droplets size={12} />
                  {neighbor.water}
                </span>
              )}
              {neighbor.pick > 0 && (
                <span className='sc-chip is-pick'>
                  <Hand size={12} />
                  {neighbor.pick}
                </span>
              )}
              <span className='sc-spacer' />
              <Button
                size='small'
                disabled={loading}
                onClick={() => open(neighbor)}
              >
                {t('拜访')}
              </Button>
            </div>
          ))}
        </div>
      )}
      {!list && (
        <p className='sc-muted sc-garden-tip'>
          {t(
            '帮道友浇灌能让作物早熟、更容易丰收，还能收到谢礼；熟了半个时辰还没收的灵草玄铁可以顺手摘走一成',
          )}
        </p>
      )}
    </>
  );
};

const Garden = ({ sect, now, busy, act, post, onEvents, onSync }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const garden = sect.garden;
  const field = sect.buildings.find((b) => b.key === 'field')?.level || 0;
  const ripe = garden.plots.filter((plot) => plot.crop && now >= plot.ripe);
  const empty = garden.plots.filter((plot) => !plot.crop).length;
  return (
    <div className='sc-garden'>
      <h4 className='sc-heading'>
        <Sprout size={16} />
        {t('灵田 {{n}} 块 · 灵田 {{level}} 级', {
          n: garden.plots.length,
          level: field,
        })}
        <span className='sc-muted'>
          {t('丰收几率 {{pct}}%，道友每浇灌一次 +{{more}}%', {
            pct: garden.bumper,
            more: garden.water_bumper,
          })}
        </span>
        <span className='sc-spacer' />
        <Button
          size='small'
          theme='solid'
          icon={<Wheat size={14} />}
          disabled={busy || ripe.length === 0}
          onClick={() => act('harvest_all')}
        >
          {t('全部收获')}
        </Button>
      </h4>
      <div className='sc-plots'>
        {garden.plots.map((plot, index) => (
          <PlotCard
            key={index}
            plot={plot}
            index={index}
            now={now}
            crops={garden.crops}
            sect={sect}
            busy={busy}
            act={act}
          />
        ))}
      </div>
      {garden.log.length > 0 && (
        <>
          <h4 className='sc-heading'>{t('来访记录')}</h4>
          <ul className='sc-log'>
            {[...garden.log].reverse().map((visit, index) => (
              <li key={index}>
                <span className='sc-muted'>{formatAgo(now - visit.at, t)}</span>
                {visit.kind === 'water'
                  ? t('{{name}}给{{crop}}浇了水', {
                      name: visit.name,
                      crop: text.crops[visit.crop]?.name,
                    })
                  : t('{{name}}从{{crop}}里摘走了 {{n}}', {
                      name: visit.name,
                      crop: text.crops[visit.crop]?.name,
                      n: visit.n,
                    })}
              </li>
            ))}
          </ul>
        </>
      )}
      <Neighbors
        now={now}
        busy={busy}
        post={post}
        onEvents={onEvents}
        onSync={onSync}
      />
      <h4 className='sc-heading'>
        <Sprout size={16} />
        {t('灵种')}
      </h4>
      <SeedList
        sect={sect}
        garden={garden}
        field={field}
        empty={empty}
        busy={busy}
        act={act}
      />
    </div>
  );
};

export default Garden;
