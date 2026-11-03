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
import { useTranslation } from 'react-i18next';
import { formatAmount } from '../../sect/parts';
import { useWendaoShell } from '../shell';
import { REGION_IDS, getRegionText } from '../duel/text';

// 灵脉只是自然闭关的一个倍率，修为随主角同步结清，离座不另发一份奖励。
export default function MeditationPanel({
  meditation,
  region,
  travel,
  blocked,
  riftActive,
  busy,
  onStart,
  onLeave,
}) {
  const { t } = useTranslation();
  const { go } = useWendaoShell();
  if (!meditation) return null;
  const regions = getRegionText(t);
  const active = meditation.active;
  const selected = active?.region ?? region;
  const bonus = active?.bonus_pct ?? meditation.bonuses?.[selected];
  const rate = active?.rate ?? meditation.rates?.[selected];
  const available = meditation.available?.[region];
  return (
    <section className='df-card'>
      <div className='df-title'>{t('灵脉打坐')}</div>
      <p className='df-lead'>
        {t(
          '在已到达地域的灵脉打坐，持续提高自然闭关速度；每次离线补算最多 12 小时。',
        )}
      </p>
      <p className='df-muted'>
        {t(
          '加成只用于自然闭关，不提高丹药或游历收益。打坐期间不能游历或进入斗法；离座后可切换地域。',
        )}
      </p>
      {active ? (
        <>
          <div className='dl-heading'>
            {t('正在{{region}}灵脉打坐', {
              region: regions[REGION_IDS[selected]]?.name,
            })}
          </div>
          <p className='df-lead'>
            {t('基础闭关 {{base}}/时 × (1 + 灵脉 {{pct}}%) = {{rate}}/时', {
              base: formatAmount(active.base_rate),
              pct: bonus,
              rate: formatAmount(rate),
            })}
          </p>
          <p className='df-muted'>
            {t('修为已随闭关自动结算，离座不会重复领取。')}
          </p>
          <div className='df-actions'>
            <Button
              theme='solid'
              loading={busy}
              disabled={busy}
              onClick={() => onLeave(active.token)}
            >
              {t('离开灵脉')}
            </Button>
          </div>
        </>
      ) : (
        <>
          <p className='df-lead'>
            {t('灵脉加成 +{{pct}}% · 自然闭关 {{rate}}/时', {
              pct: bonus,
              rate: formatAmount(rate),
            })}
          </p>
          {!available && (
            <p className='df-muted'>
              {t('先在历练中通过这个地域的任意节点，才能使用这里的灵脉。')}
            </p>
          )}
          {travel && (
            <p className='df-muted'>{t('先结束游历，再到灵脉打坐。')}</p>
          )}
          {blocked && (
            <p className='df-muted'>
              {t('先完成或退出进行中的斗法，再开始打坐。')}
            </p>
          )}
          {riftActive && (
            <div className='dl-active'>
              <span>
                {t('九幽秘境还有一轮未结束，先打完或放弃这一轮再来打坐。')}
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
              disabled={busy || !available || !!travel || blocked || riftActive}
              onClick={() => onStart(region)}
            >
              {t('在{{region}}灵脉打坐', {
                region: regions[REGION_IDS[region]]?.name,
              })}
            </Button>
          </div>
        </>
      )}
    </section>
  );
}
