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

import React, { useMemo, useState } from 'react';
import {
  CalendarDays,
  Castle,
  ChevronRight,
  ClipboardCheck,
  Compass,
  Flag,
  Flame,
  Pickaxe,
  Pill,
  PawPrint,
  Sparkles,
  Sprout,
  Trophy,
  Users,
  Zap,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getRealmNames } from '../sect/text';
import { getChapterText, getKeyText, getNodeText } from './journey/text';

// 问道页顶上的一条：主角眼下的主目标，和三个板块里现在就能处理的几件事(GET /api/game/wendao/agenda，按急迫程度排好)。
// 点一下跳到对应的板块与页签；能不能做仍以那边的按钮与服务端为准。

// 顶上先摆三件，其余的点「还有几件」展开。
const SHOWN = 3;

const TODO_ICONS = {
  event: Sparkles,
  furnace_done: Flame,
  missions: Flag,
  garden: Sprout,
  hero_break: Zap,
  daily: ClipboardCheck,
  furnace_stoke: Flame,
  qi: Pill,
  disciple_break: Users,
  beast_play: PawPrint,
  lure: PawPrint,
  furnace_idle: Flame,
  garden_empty: Sprout,
  gather: Pickaxe,
  trial: CalendarDays,
  abyss_reward: Castle,
  tournament_reward: Trophy,
};

const todoText = (todo, t) => {
  const n = todo.n;
  switch (todo.key) {
    case 'event':
      return t('有一件奇遇等你拿主意');
    case 'furnace_done':
      return t('{{n}} 炉丹出炉了', { n });
    case 'missions':
      return t('{{n}} 支派遣回山了', { n });
    case 'garden':
      return t('{{n}} 块灵田熟了', { n });
    case 'hero_break':
      return t('机缘在手，可以突破了');
    case 'daily':
      return t('功课有 {{n}} 份奖励可领', { n });
    case 'furnace_stoke':
      return t('{{n}} 炉丹可以调火', { n });
    case 'qi':
      return t('可以服丹增长修为');
    case 'disciple_break':
      return t('{{n}} 名弟子可以突破', { n });
    case 'beast_play':
      return t('{{n}} 只灵兽今天还没嬉戏', { n });
    case 'lure':
      return t('兽苑可以引兽了');
    case 'furnace_idle':
      return t('{{n}} 座丹炉空着', { n });
    case 'garden_empty':
      return t('{{n}} 块灵田空着', { n });
    case 'gather':
      return t('{{n}} 处采集点可以去', { n });
    case 'trial':
      return t('周天试炼还有 {{n}} 关没过', { n });
    case 'abyss_reward':
      return t('今天的秘境奖励还能领 {{n}} 次', { n });
    case 'tournament_reward':
      return t('今天的大比奖励还能领 {{n}} 次', { n });
    default:
      return '';
  }
};

const goalText = (goal, t, text) => {
  switch (goal.key) {
    case 'create':
      return t('选一条道途，开始历练');
    case 'battle':
      return t('有一场仗还没打完');
    case 'node':
      return t('{{chapter}}：{{name}}', {
        chapter: text.chapters[goal.n]?.name,
        name: text.nodes[goal.s]?.name,
      });
    case 'cultivate':
      return t('闭关修到{{realm}}九层圆满（现在第 {{layer}} 层）', {
        realm: text.realms[goal.n],
        layer: goal.m,
      });
    case 'break':
      return t('带上{{key}}，突破{{realm}}', {
        key: text.keys[goal.s]?.name,
        realm: text.realms[goal.n],
      });
    case 'steps':
      return t('登问心阶第 {{n}} 级', { n: goal.n });
    case 'summit':
      return t('问心阶已经登顶，宗门与秘境里还有许多事可做');
    default:
      return '';
  }
};

// target 是点了以后去哪里：板块、页签、页签里的分区与地图上的节点。
const goalTarget = (goal) => ({
  part: goal.part,
  tab: goal.tab,
  node: goal.key === 'node' ? goal.s : undefined,
});

const Agenda = ({ agenda, onGo }) => {
  const { t } = useTranslation();
  const text = useMemo(
    () => ({
      chapters: getChapterText(t),
      nodes: getNodeText(t),
      keys: getKeyText(t),
      realms: getRealmNames(t),
    }),
    [t],
  );
  const [expanded, setExpanded] = useState(false);
  if (!agenda) return null;
  const { goal, todos } = agenda;
  const shown = expanded ? todos : todos.slice(0, SHOWN);
  const more = todos.length - shown.length;
  return (
    <div className='wd-agenda'>
      <button
        type='button'
        className='wd-goal'
        onClick={() => onGo(goalTarget(goal))}
      >
        <Compass size={16} aria-hidden />
        <span className='wd-goal-label'>{t('当前目标')}</span>
        <strong className='wd-goal-text'>{goalText(goal, t, text)}</strong>
        <ChevronRight size={16} aria-hidden className='wd-goal-go' />
      </button>
      {todos.length > 0 && (
        <ul className='wd-todos' aria-label={t('待办')}>
          {shown.map((todo) => {
            const Icon = TODO_ICONS[todo.key] || Sparkles;
            return (
              <li key={todo.key}>
                <button
                  type='button'
                  className={`wd-todo is-${todo.part}`}
                  onClick={() =>
                    onGo({ part: todo.part, tab: todo.tab, view: todo.view })
                  }
                >
                  <Icon size={14} aria-hidden />
                  <span>{todoText(todo, t)}</span>
                </button>
              </li>
            );
          })}
          {more > 0 && (
            <li>
              <button
                type='button'
                className='wd-todo-more'
                onClick={() => setExpanded(true)}
              >
                {t('还有 {{n}} 件', { n: more })}
              </button>
            </li>
          )}
        </ul>
      )}
    </div>
  );
};

export default Agenda;
