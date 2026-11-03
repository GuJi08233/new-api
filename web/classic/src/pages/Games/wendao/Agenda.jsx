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
  BookOpen,
  CalendarDays,
  Check,
  Castle,
  ChevronRight,
  ClipboardCheck,
  Compass,
  Flag,
  Flame,
  Landmark,
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
import { getUserIdFromLocalStorage } from '../../../helpers/utils';
import { readGameStorage, writeGameStorage } from '../common/gameStorage';
import { formatAmount, formatLeft } from '../sect/parts';
import { getRealmNames, getResourceNames, RESOURCES } from '../sect/text';
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
  stipend: Landmark,
  furnace_stoke: Flame,
  qi: Pill,
  disciple_break: Users,
  beast_play: PawPrint,
  lure: PawPrint,
  furnace_idle: Flame,
  garden_empty: Sprout,
  gather: Pickaxe,
  trial: CalendarDays,
  side: BookOpen,
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
    case 'stipend':
      return t('今天的宗门俸禄还没领');
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
    case 'side':
      return t('{{n}} 段支线可以接着走', { n });
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

// 入门指引的每一步：short 是步骤条上的短名，title 与 desc 是轮到这一步时的说明。说明里分几次讲清灵石、灵珠、道韵与
// 游戏币各是什么。标识与 pkg/wendao 的 GuideStep 一致。
export const getGuideText = (t) => ({
  hero: {
    short: t('选道途'),
    title: t('选一条道途'),
    desc: t(
      '体修、法修、剑修任选其一。你从散修做起：可以拜入正道宗门，也可以修到金丹以后开宗立派。',
    ),
  },
  win: {
    short: t('首胜'),
    title: t('在历练里打赢一场'),
    desc: t(
      '战斗用功法牌组。赢了可以从三张功法里收下一张，第一次通过一个节点给全部奖励。',
    ),
  },
  path: {
    short: t('择路'),
    title: t('拜入一个正道宗门'),
    desc: t(
      '到宗门页的身份里挑一家拜入：每天领俸禄，攒贡献换丹药，还有门派加成。也可以一直做散修，修到金丹以后开宗立派。',
    ),
  },
  build: {
    short: t('升建筑'),
    title: t('升级一座宗门建筑'),
    desc: t(
      '建筑每小时产出灵石、灵草、玄铁，离线最多补算 12 小时；升级花这些资源，主殿还要声望。',
    ),
  },
  dispatch: {
    short: t('派遣'),
    title: t('派弟子出去历练'),
    desc: t(
      '派遣回山带回资源与丹药，失败时会有弟子受伤；队伍回来以后到派遣页结算收获。',
    ),
  },
  garden: {
    short: t('种灵田'),
    title: t('在灵田种下一茬'),
    desc: t(
      '灵草、玄铁收了直接进库房；朱果、九叶灵芝、七彩莲是炼丹与祭炼要用的灵药。',
    ),
  },
  break: {
    short: t('筑基'),
    title: t('突破到筑基'),
    desc: t(
      '修到炼气九层圆满，带上第一章首领掉的筑基灵液突破。每个大境界开放一章新地图。',
    ),
  },
  abyss: {
    short: t('秘境'),
    title: t('走完一轮九幽秘境'),
    desc: t(
      '灵珠只在这一局里花，道韵是秘境自己的成长，都带不进历练。秘境无尽与宗门大比按规则发游戏币，其他玩法都不发。',
    ),
  },
});

// 入门指引收起、回来的说明看过，都按账号记在浏览器里。
const guideHiddenKey = (uid) => `wendao-guide-hidden:${uid}`;
const backSeenKey = (uid) => `wendao-back-seen:${uid}`;

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
      guide: getGuideText(t),
      resources: getResourceNames(t),
    }),
    [t],
  );
  const uid = getUserIdFromLocalStorage();
  const [expanded, setExpanded] = useState(false);
  // 收起或展开过就照那次的来；没动过时，主角已经筑基的老玩家默认收着，只留一枚「入门 n/7」。
  const [guideHidden, setGuideHidden] = useState(
    () => readGameStorage(guideHiddenKey(uid)).value,
  );
  const [backSeen, setBackSeen] = useState(
    () => readGameStorage(backSeenKey(uid)).value,
  );
  if (!agenda) return null;
  const { goal, todos } = agenda;
  const shown = expanded ? todos : todos.slice(0, SHOWN);
  const more = todos.length - shown.length;
  // 离开超过离线结算上限刚回来：说明离开了多久、只补算了多少；看过的那一次(按离开的时间认)不再提。
  const back =
    agenda.back && agenda.back.since !== backSeen ? agenda.back : null;
  const guide = agenda.guide || [];
  const next = guide.find((step) => !step.done);
  const doneSteps = guide.filter((step) => step.done).length;
  const guideCollapsed =
    guideHidden ?? guide.some((step) => step.key === 'break' && step.done);
  const toggleGuide = (hidden) => {
    writeGameStorage(guideHiddenKey(uid), hidden);
    setGuideHidden(hidden);
  };
  return (
    <div className='wd-agenda'>
      {back && (
        <div className='wd-back' role='status'>
          <span className='wd-back-text'>
            {t(
              '离开了 {{time}}，闭关修为与宗门产出最多补算 12 小时。这一次补算：{{gains}}',
              {
                time: formatLeft(back.at - back.since, t),
                gains:
                  RESOURCES.filter((key) => back.res?.[key] > 0)
                    .map(
                      (key) =>
                        `${text.resources[key]} +${formatAmount(back.res[key])}`,
                    )
                    .join(' · ') || t('没有'),
              },
            )}
          </span>
          <button
            type='button'
            className='wd-back-close'
            onClick={() => {
              writeGameStorage(backSeenKey(uid), back.since);
              setBackSeen(back.since);
            }}
          >
            {t('知道了')}
          </button>
        </div>
      )}
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
                    onGo({
                      part: todo.part,
                      tab: todo.tab,
                      view: todo.view,
                      node: todo.s,
                    })
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
      {next && guideCollapsed && (
        <button
          type='button'
          className='wd-todo-more'
          onClick={() => toggleGuide(false)}
        >
          {t('入门 {{done}}/{{total}}', {
            done: doneSteps,
            total: guide.length,
          })}
        </button>
      )}
      {next && !guideCollapsed && (
        <section className='wd-guide' aria-label={t('入门指引')}>
          <header className='wd-guide-head'>
            <strong>{t('入门指引')}</strong>
            <span className='wd-guide-count'>
              {doneSteps}/{guide.length}
            </span>
            <ol className='wd-guide-steps'>
              {guide.map((step) => (
                <li
                  key={step.key}
                  className={`wd-guide-step${step.done ? ' is-done' : ''}${step === next ? ' is-current' : ''}`}
                >
                  {step.done && <Check size={12} aria-hidden />}
                  {text.guide[step.key]?.short}
                </li>
              ))}
            </ol>
            <button
              type='button'
              className='wd-guide-hide'
              onClick={() => toggleGuide(true)}
            >
              {t('收起')}
            </button>
          </header>
          <div className='wd-guide-next'>
            <div>
              <strong>{text.guide[next.key]?.title}</strong>
              <p>{text.guide[next.key]?.desc}</p>
            </div>
            <button
              type='button'
              className='wd-guide-go'
              onClick={() => onGo({ part: next.part, tab: next.tab })}
            >
              {t('去做')}
              <ChevronRight size={14} aria-hidden />
            </button>
          </div>
        </section>
      )}
    </div>
  );
};

export default Agenda;
