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
import { useTranslation } from 'react-i18next';
import { SideBox } from './SideBox';
import { formatClock } from './skill';

// 信息栏里的本机最佳：每个难度一行，列出最短用时与最少步数(columns 决定显示哪几项)。还没过关的难度显示"-"。
export const LocalBestBox = ({
  bests,
  levels,
  columns = ['time', 'moves'],
}) => {
  const { t } = useTranslation();
  const titles = { time: t('最短用时'), moves: t('最少步数') };
  const format = (name, value) => {
    if (value === undefined) return '-';
    return name === 'time' ? formatClock(value) : value;
  };
  return (
    <SideBox label={t('本机最佳')}>
      <table className='game-table'>
        <thead>
          <tr>
            <th>{t('难度')}</th>
            {columns.map((name) => (
              <th key={name}>{titles[name]}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {levels.map(({ key, name }) => (
            <tr key={key}>
              <td>{name}</td>
              {columns.map((column) => (
                <td key={column}>{format(column, bests[key]?.[column])}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <div className='game-box-note'>{t('只记在这个浏览器里，不影响奖励')}</div>
    </SideBox>
  );
};

// 过关提示里的一句纪录说明：哪几项刷新了本机最佳。没有刷新时为空。
export function localBestNote(improved, t) {
  if (!improved) return '';
  if (improved.time && improved.moves) return t('刷新了本机最短用时与最少步数');
  if (improved.time) return t('刷新了本机最短用时');
  if (improved.moves) return t('刷新了本机最少步数');
  return '';
}
