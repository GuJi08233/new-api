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
import { useSectText } from '../../sect/parts';
import {
  REGION_IDS,
  getDuelKindNames,
  getRegionText,
  getTierNames,
} from './text';
import './duel.css';

// 一张契约的报酬写成一行：宗门资源，宗门任务另有贡献。
const rewardLine = (contract, text, t) => {
  const parts = ['stone', 'herb', 'ore']
    .filter((key) => contract.reward?.[key] > 0)
    .map((key) => `${text.resources[key]} ${contract.reward[key]}`);
  if (contract.merit > 0) {
    parts.unshift(t('贡献 +{{n}}', { n: contract.merit }));
  }
  return parts.join(' · ');
};

// ContractList 是一排契约(悬赏或宗门任务)：难度、地域、报酬、还剩几次挑战，和「接下」。open 是每个地域主角够不够得着，
// 不够的灰掉；有一场进行中、做完了或次数用完时不能接。
export function ContractList({
  contracts,
  open,
  active,
  busy,
  maxTries,
  onStart,
}) {
  const { t } = useTranslation();
  const text = useSectText();
  const regions = getRegionText(t);
  const tiers = getTierNames(t);
  if (!contracts?.length) return null;
  return (
    <div className='dl-list'>
      {contracts.map((c) => {
        const region = regions[REGION_IDS[c.region]];
        const reachable = open?.[c.region];
        const spent = c.done || c.tries <= 0;
        return (
          <div key={c.id} className={`dl-row ${c.done ? 'is-done' : ''}`}>
            <span className='dl-tier'>{tiers[c.tier - 1]}</span>
            <span className='dl-body'>
              <strong>{region?.name}</strong>
              <small>{rewardLine(c, text, t)}</small>
            </span>
            <span className='dl-tries'>
              {c.done
                ? t('已完成')
                : t('挑战 {{n}}/{{max}}', { n: c.tries, max: maxTries })}
            </span>
            <Button
              size='small'
              theme='solid'
              disabled={busy || !!active || spent || !reachable}
              onClick={() => onStart(c)}
            >
              {t('接下')}
            </Button>
          </div>
        );
      })}
    </div>
  );
}

// ActiveDuelNotice 是读到一场没打完的契约时的提示：页面关掉重开以后种子拿不回来了，只能放弃(用掉的挑战不退)。
export function ActiveDuelNotice({ active, busy, onGiveUp }) {
  const { t } = useTranslation();
  const kinds = getDuelKindNames(t);
  if (!active) return null;
  return (
    <div className='dl-active'>
      <span>
        {t(
          '有一场{{kind}}没打完。刷新或离开页面以后只能放弃，用掉的挑战不退。',
          {
            kind: kinds[active.kind],
          },
        )}
      </span>
      <Button size='small' disabled={busy} onClick={onGiveUp}>
        {t('放弃这一场')}
      </Button>
    </div>
  );
}
