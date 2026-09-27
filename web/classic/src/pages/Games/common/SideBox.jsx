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

// 信息栏里的一格：标题、一个大号数字(可带单位)、一行说明，或者自定义内容。
export const SideBox = ({ label, value, unit, note, children }) => (
  <div className='game-box'>
    {label && <div className='game-box-label'>{label}</div>}
    {value !== undefined && (
      <div className='game-box-value'>
        {value}
        {unit && <span>{unit}</span>}
      </div>
    )}
    {note && <div className='game-box-note'>{note}</div>}
    {children}
  </div>
);

// 规则说明的列表，空的条目不显示。
export const RuleList = ({ items }) => {
  const shown = items.filter(Boolean);
  if (shown.length === 0) return null;
  return (
    <ul className='game-rules'>
      {shown.map((item, index) => (
        <li key={index}>{item}</li>
      ))}
    </ul>
  );
};

// 台面下方的结果提示条。kind 为 win、big-win、loss、error 或空。
export const StageMessage = ({ kind = '', note, children }) => (
  <div
    className={`games-stage-message ${kind ? `is-${kind}` : ''} ${kind === 'big-win' ? 'is-win' : ''}`}
    aria-live='polite'
  >
    <span>{children}</span>
    {note && <span className='games-stage-message-note'>{note}</span>}
  </div>
);
