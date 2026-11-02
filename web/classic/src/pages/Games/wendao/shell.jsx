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

import React, { createContext, useCallback, useContext, useMemo } from 'react';
import { Castle, Footprints, MountainSnow } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import GameLayout from '../common/GameLayout';
import Agenda from './Agenda';
import './wendao.css';

// 问道页的外壳：一次读出整个问道的规则(GET /api/game/wendao)，三个板块各取自己的那一份。板块页面用 WendaoLayout
// 代替 GameLayout，顶上多一排板块切换和一条「当前目标与待办」。
export const WendaoContext = createContext(null);

// useWendaoShell 是外壳给板块页面的东西：focus 是从待办点过来时要打开的页签({ part, tab, view, node, seq })，
// 板块页面按它切页签；refreshAgenda 在操作成功后调用，让顶上的待办跟着变。
export function useWendaoShell() {
  const { focus, refreshAgenda } = useContext(WendaoContext);
  return { focus, refreshAgenda };
}

export const WENDAO_PARTS = [
  { key: 'journey', Icon: Footprints },
  { key: 'sect', Icon: MountainSnow },
  { key: 'abyss', Icon: Castle },
];

export const getPartNames = (t) => ({
  journey: t('历练'),
  sect: t('宗门'),
  abyss: t('秘境'),
});

// useWendaoPart 返回一个板块的规则：板块自己的字段盖在公共字段(余额、游戏币符号)上。patch 把一次操作返回的新进度并回
// 这个板块，余额同时更新到公共字段。
export function useWendaoPart(part) {
  const shell = useContext(WendaoContext);
  const { info: all, setInfo, reload } = shell;
  const info = useMemo(
    () => (all ? { ...all, ...all[part] } : null),
    [all, part],
  );
  const patch = useCallback(
    (fields) =>
      setInfo((previous) =>
        previous
          ? {
              ...previous,
              balance: fields.balance ?? previous.balance,
              [part]: { ...previous[part], ...fields },
            }
          : previous,
      ),
    [setInfo, part],
  );
  return { info, patch, reload };
}

// 板块切换：别的板块里有待办时，按钮上标出件数。
const PartNav = () => {
  const { t } = useTranslation();
  const { part, setPart, parts, agenda } = useContext(WendaoContext);
  const names = getPartNames(t);
  const pending = {};
  for (const todo of agenda?.todos || []) {
    pending[todo.part] = (pending[todo.part] || 0) + 1;
  }
  return (
    <nav className='wd-parts' aria-label={t('问道')}>
      {parts.map(({ key, Icon }) => (
        <button
          type='button'
          key={key}
          className={`wd-part ${part === key ? 'is-active' : ''}`}
          aria-current={part === key ? 'page' : undefined}
          onClick={() => setPart(key)}
        >
          <Icon size={16} />
          <span>{names[key]}</span>
          {part !== key && pending[key] > 0 && (
            <span
              className='wd-part-badge'
              aria-label={t('{{n}} 件待办', { n: pending[key] })}
            >
              {pending[key]}
            </span>
          )}
        </button>
      ))}
    </nav>
  );
};

// WendaoLayout 是板块页面的外框：loading 为真时显示加载中；side 是右边的信息栏。
export const WendaoLayout = ({ loading, className = '', side, children }) => {
  const { t } = useTranslation();
  const { info, agenda, go } = useContext(WendaoContext);
  return (
    <GameLayout
      title={t('问道')}
      loading={loading}
      balance={info?.balance}
      symbol={info?.symbol}
      className={`wd-page ${className}`}
      side={side}
    >
      <PartNav />
      <Agenda agenda={agenda} onGo={go} />
      {children}
    </GameLayout>
  );
};
