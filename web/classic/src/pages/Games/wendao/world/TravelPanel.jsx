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

import React, { useEffect, useState } from 'react';
import { Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { CostChips, formatAmount, formatLeft } from '../../sect/parts';
import { useWendaoShell } from '../shell';
import { REGION_IDS, getRegionText } from '../duel/text';

// 收益始终展示服务端核算值；本地只推进时间，每分钟由世界页刷新收益。
export default function TravelPanel({
  travel,
  region,
  receivedAt,
  blocked,
  riftActive,
  busy,
  loot,
  onStart,
  onFinish,
}) {
  const { t } = useTranslation();
  const { go } = useWendaoShell();
  const [, setClock] = useState(0);
  const active = travel?.active;
  const token = active?.token;
  useEffect(() => {
    if (!token) return undefined;
    const timer = setInterval(() => setClock((n) => n + 1), 1000);
    return () => clearInterval(timer);
  }, [token]);
  if (!travel) return null;
  const regions = getRegionText(t);
  const elapsed = active
    ? Math.min(
        active.cap_seconds,
        active.elapsed + Math.max(0, (Date.now() - receivedAt) / 1000),
      )
    : 0;
  const rate = active?.rate || travel.rates?.[region];
  const available = travel.available?.[region];
  const capped = active && elapsed >= active.cap_seconds;
  return (
    <section className='df-card'>
      <div className='df-title'>{t('游历')}</div>
      <p className='df-lead'>
        {t(
          '选择已到达的地域游历，结束时领取修为与材料；每次最多积累 12 小时，刷新不会重置上限。',
        )}
      </p>
      <p className='df-muted'>
        {t(
          '收益按出发时的实力固定，途中养成下次生效。游历时暂停闭关，不能进入历练、契约或九幽秘境斗法。',
        )}
      </p>
      {active ? (
        <>
          <div className='dl-heading'>
            {t('正在{{region}}游历', {
              region: regions[REGION_IDS[active.region]]?.name,
            })}
          </div>
          <p className='df-muted'>
            {capped
              ? t('收益已达 12 小时上限，请结束游历领取。')
              : t('已积累 {{time}}，距收益上限还有 {{left}}', {
                  time: formatLeft(elapsed, t),
                  left: formatLeft(active.cap_seconds - elapsed, t),
                })}
          </p>
          <div
            className='df-bar'
            role='progressbar'
            aria-label={t('游历')}
            aria-valuemin={0}
            aria-valuemax={active.cap_seconds}
            aria-valuenow={Math.floor(elapsed)}
          >
            <span
              className='df-bar-fill is-hp'
              style={{ width: `${(100 * elapsed) / active.cap_seconds}%` }}
            />
            <span className='df-bar-text'>{t('待领取收益（每分钟同步）')}</span>
          </div>
          <div className='df-tags'>
            <span className='df-chip'>
              {t('修为 +{{n}}', { n: formatAmount(active.exp) })}
            </span>
            <CostChips cost={active.res} sign='+' />
          </div>
          <p className='df-muted'>
            {t('修为在本境界圆满处停止，材料照常积累。')}
          </p>
          <div className='df-actions'>
            <Button
              theme='solid'
              loading={busy}
              disabled={busy}
              onClick={() => onFinish(active.token)}
            >
              {t('结束游历并领取')}
            </Button>
          </div>
        </>
      ) : (
        <>
          {!available && (
            <p className='df-muted'>
              {t('先在历练中通过这个地域的任意节点，才能到这里游历。')}
            </p>
          )}
          {blocked && (
            <p className='df-muted'>
              {t('先完成或退出进行中的斗法，再开始游历。')}
            </p>
          )}
          {riftActive && (
            <div className='dl-active'>
              <span>
                {t('九幽秘境还有一轮未结束，先打完或放弃这一轮再来游历。')}
              </span>
              <Button size='small' onClick={() => go({ part: 'abyss' })}>
                {t('去秘境')}
              </Button>
            </div>
          )}
          <div className='df-actions'>
            <Button
              theme='solid'
              loading={busy}
              disabled={busy || !available || blocked || riftActive}
              onClick={() => onStart(region)}
            >
              {t('在{{region}}开始游历', {
                region: regions[REGION_IDS[region]]?.name,
              })}
            </Button>
          </div>
        </>
      )}
      {rate && (
        <p className='df-muted'>
          {t(
            '每小时：修为 {{exp}} · 灵石 {{stone}} · 灵草 {{herb}} · 玄铁 {{ore}}',
            {
              exp: formatAmount(rate.exp),
              stone: rate.res?.stone || 0,
              herb: rate.res?.herb || 0,
              ore: rate.res?.ore || 0,
            },
          )}
        </p>
      )}
      {loot && (
        <div className='dl-active' role='status'>
          <span>
            {t('游历已结束，修为 +{{n}}，材料已放入储物处。', {
              n: formatAmount(loot.exp),
            })}
          </span>
          <CostChips cost={loot.res} sign='+' />
        </div>
      )}
    </section>
  );
}
