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
import { formatAmount, formatLeft } from '../sect/format';
import { RESOURCES } from '../sect/text';
import { REGION_IDS } from './duel/text';

// 目标与待办共用跳转协议；地域零是青岚山，不能因为 falsy 被丢掉。
export function agendaTarget(item) {
  const target = { part: item.part };
  if (item.tab) target.tab = item.tab;
  if (item.view) target.view = item.view;
  if (item.part === 'journey' && item.s) target.node = item.s;
  if (item.part === 'world') {
    target.region = item.region ?? 0;
    if (item.key === 'travel' || item.key === 'meditation')
      target.view = item.key;
    else if (item.key === 'duel') target.view = 'bounties';
  }
  return target;
}

const reportResources = (res, text, t) =>
  RESOURCES.filter((key) => res?.[key] > 0)
    .map((key) => `${text.resources[key]} +${formatAmount(res[key])}`)
    .join(' · ') || t('没有');

// 已入账的自然闭关、宗门产出与整次游历待领取分开说明，不能把行程快照写成已经到账。
export function ReturnReport({ report, text, t, onClose }) {
  const cultivation = report.cultivation;
  const sect = report.sect;
  const travel = report.travel;
  const cap = formatLeft(report.cap_seconds, t);
  return (
    <div className='wd-back' role='status'>
      <div className='wd-back-text'>
        <strong>
          {t('离开了 {{time}}，这次回流结算如下：', {
            time: formatLeft(Math.max(report.at - report.since, 0), t),
          })}
        </strong>
        <ul className='wd-back-items'>
          {cultivation && (
            <li>
              <span className='wd-back-label'>
                {cultivation.kind === 'vein'
                  ? t('{{region}}灵脉闭关（已入账）', {
                      region:
                        text.regions[REGION_IDS[cultivation.region ?? 0]]?.name,
                    })
                  : t('洞府闭关（已入账）')}
              </span>
              <span>
                {t('补算 {{time}} · 修为 +{{n}}', {
                  time: formatLeft(cultivation.seconds, t),
                  n: formatAmount(cultivation.exp),
                })}
              </span>
              {cultivation.capped && (
                <em>{t('已达 {{time}}补算上限', { time: cap })}</em>
              )}
            </li>
          )}
          {sect && (
            <li>
              <span className='wd-back-label'>{t('宗门产出（已入账）')}</span>
              <span>
                {t('补算 {{time}} · {{gains}}', {
                  time: formatLeft(sect.seconds, t),
                  gains: reportResources(sect.res, text, t),
                })}
              </span>
              {sect.capped && (
                <em>{t('已达 {{time}}补算上限', { time: cap })}</em>
              )}
            </li>
          )}
          {travel && (
            <li>
              <span className='wd-back-label'>
                {t('{{region}}游历（累计待领取）', {
                  region: text.regions[REGION_IDS[travel.region ?? 0]]?.name,
                })}
              </span>
              <span>
                {t('累计 {{time}} · 修为 +{{n}} · {{gains}}', {
                  time: formatLeft(travel.elapsed, t),
                  n: formatAmount(travel.exp),
                  gains: reportResources(travel.res, text, t),
                })}
              </span>
              <em>
                {travel.capped
                  ? t('已达本次行程上限，结束游历后入账')
                  : t('结束游历后入账')}
              </em>
            </li>
          )}
          {!cultivation && !sect && !travel && (
            <li>{t('这次没有可入账或待领取的收益。')}</li>
          )}
        </ul>
      </div>
      <button type='button' className='wd-back-close' onClick={onClose}>
        {t('知道了')}
      </button>
    </div>
  );
}
