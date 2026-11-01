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
import { Check, Gift, ListTodo, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Bar, CostChips } from './parts';
import { getDailyTaskText } from './text';

// 三件功课都领了奖励、宝箱还没开。
export function dailyChestReady(daily) {
  return (
    !!daily &&
    !daily.chest &&
    daily.tasks.length > 0 &&
    daily.tasks.every((task) => task.claimed)
  );
}

// 有做完还没领的功课，或者宝箱能开了：宗门页签上打点提醒。
export function dailyPending(daily) {
  if (!daily) return false;
  return (
    dailyChestReady(daily) ||
    daily.tasks.some((task) => !task.claimed && task.have >= task.need)
  );
}

// 每日功课：今天的三件小事、做到几次、领奖与换一件；三件都领了开宝箱。
const DailyPanel = ({ sect, busy, act }) => {
  const { t } = useTranslation();
  const daily = sect.daily;
  if (!daily || daily.tasks.length === 0) return null;
  const names = getDailyTaskText(t);
  const chestReady = dailyChestReady(daily);
  return (
    <div className='sc-daily'>
      <h4 className='sc-heading'>
        <ListTodo size={16} />
        {t('每日功课')}
        {daily.streak > 0 && (
          <span className='sc-muted'>
            {t('已连续 {{n}} 天开宝箱', { n: daily.streak })}
          </span>
        )}
      </h4>
      <div className='sc-daily-list'>
        {daily.tasks.map((task, index) => {
          const done = task.have >= task.need;
          return (
            <div
              key={`${task.key}-${index}`}
              className={`sc-daily-task${task.claimed ? ' is-claimed' : done ? ' is-done' : ''}`}
            >
              <strong>{names[task.key]?.(task.need)}</strong>
              <Bar
                value={task.have}
                max={task.need}
                kind={done ? 'peak' : ''}
              />
              <span className='sc-muted'>
                {task.have} / {task.need}
              </span>
              <CostChips cost={daily.reward} />
              <span className='sc-spacer' />
              {task.claimed ? (
                <span className='sc-daily-claimed'>
                  <Check size={14} />
                  {t('已领')}
                </span>
              ) : done ? (
                <Button
                  size='small'
                  theme='solid'
                  disabled={busy}
                  onClick={() => act('daily_claim', { index })}
                >
                  {t('领取')}
                </Button>
              ) : (
                daily.rerolls_left > 0 && (
                  <Button
                    size='small'
                    type='tertiary'
                    theme='borderless'
                    icon={<RefreshCw size={13} />}
                    disabled={busy}
                    onClick={() => act('daily_reroll', { index })}
                  >
                    {t('换一件')}
                  </Button>
                )
              )}
            </div>
          );
        })}
      </div>
      <div className='sc-daily-chest'>
        <Gift size={16} />
        <span className='sc-muted'>
          {t(
            '三件都领了可以开宝箱：聚气丹与一份灵药；连续 {{n}} 天开宝箱另得一颗洗髓丹',
            { n: daily.streak_bonus },
          )}
        </span>
        <span className='sc-spacer' />
        <Button
          size='small'
          theme={chestReady ? 'solid' : 'light'}
          type='warning'
          disabled={busy || !chestReady}
          onClick={() => act('daily_chest')}
        >
          {daily.chest ? t('今天的宝箱已开') : t('开宝箱')}
        </Button>
      </div>
    </div>
  );
};

export default DailyPanel;
