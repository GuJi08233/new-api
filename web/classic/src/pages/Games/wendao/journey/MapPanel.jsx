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
import {
  BookOpen,
  Check,
  Crown,
  FlaskConical,
  Gem,
  KeyRound,
  Leaf,
  Lock,
  Skull,
  Sparkles,
  Swords,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useDungeonText, RelicChip } from '../../dungeon/parts';
import ChapterScene from '../art/Scene';
import EnemyArt from '../art/EnemyArt';
import { getEnemyNames } from '../../dungeon/text';
import {
  CostChips,
  formatAmount,
  formatLeft,
  useSectText,
} from '../../sect/parts';
import TeamPicker, { readyTeam } from './TeamPicker';
import {
  getChapterText,
  getKeyText,
  getNodeKindNames,
  getNodeText,
} from './text';

const KIND_ICONS = {
  story: BookOpen,
  combat: Swords,
  elite: Skull,
  boss: Crown,
  gather: Leaf,
};

// 节点在地图上的状态：走过、能去、采集冷却中、还没解锁。
function nodeState(node, hero, now) {
  const record = hero.nodes[node.id];
  const open = hero.open.includes(node.id);
  if (hero.ready[node.id] && hero.ready[node.id] > now) return 'cooling';
  if (open) return record?.clears ? 'repeat' : 'open';
  if (record?.clears) return 'done';
  return 'locked';
}

// 历练地图：一章一张图，节点之间连着前置关系的线；点一个节点在下面看详情、开战或前往。
const MapPanel = ({
  info,
  hero,
  now,
  busy,
  selected,
  onSelect,
  onVisit,
  onFight,
  onStory,
}) => {
  const { t } = useTranslation();
  const chapters = getChapterText(t);
  const texts = getNodeText(t);
  const nodes = info.nodes;
  const reached = Math.min(hero.realm, info.chapters - 1);
  const [chapter, setChapter] = useState(reached);
  useEffect(() => setChapter(reached), [reached]);

  const shown = nodes.filter((node) => node.chapter === chapter);
  const byId = Object.fromEntries(nodes.map((node) => [node.id, node]));
  const fallback =
    shown.find((node) => nodeState(node, hero, now) === 'open') ||
    shown.find((node) => hero.open.includes(node.id)) ||
    shown[0];
  const current =
    selected && byId[selected]?.chapter === chapter ? byId[selected] : fallback;

  return (
    <div className='jn-map-wrap'>
      <div className='jn-chapters'>
        {chapters.map((ch, index) => (
          <button
            type='button'
            key={index}
            className={`jn-chapter ${chapter === index ? 'is-active' : ''}`}
            disabled={index > reached}
            onClick={() => setChapter(index)}
          >
            {index > reached && <Lock size={12} />}
            {ch.name}
          </button>
        ))}
      </div>
      <div className={`jn-map is-ch${chapter + 1}`}>
        <ChapterScene chapter={chapter} className='jn-map-scene' />
        <svg
          className='jn-map-lines'
          viewBox='0 0 100 100'
          preserveAspectRatio='none'
          aria-hidden
        >
          {shown.flatMap((node) =>
            node.requires
              .filter((id) => byId[id]?.chapter === chapter)
              .map((id) => {
                const from = texts[id];
                const to = texts[node.id];
                const lit = (hero.nodes[id]?.clears || 0) > 0;
                return (
                  <line
                    key={`${id}-${node.id}`}
                    x1={from.x}
                    y1={from.y}
                    x2={to.x}
                    y2={to.y}
                    className={lit ? 'is-lit' : ''}
                  />
                );
              }),
          )}
        </svg>
        {shown.map((node) => {
          const state = nodeState(node, hero, now);
          const Icon = state === 'locked' ? Lock : KIND_ICONS[node.kind];
          const pos = texts[node.id];
          return (
            <button
              type='button'
              key={node.id}
              className={`jn-node is-${node.kind} is-${state} ${current?.id === node.id ? 'is-selected' : ''}`}
              style={{ left: `${pos.x}%`, top: `${pos.y}%` }}
              aria-label={pos.name}
              onClick={() => onSelect(node.id)}
            >
              <span className='jn-node-dot'>
                <Icon size={node.kind === 'boss' ? 20 : 16} />
                {state === 'done' || state === 'repeat' ? (
                  <Check size={11} className='jn-node-check' />
                ) : null}
              </span>
              <span className='jn-node-name'>{pos.name}</span>
            </button>
          );
        })}
      </div>
      {current && (
        <NodeDetail
          key={current.id}
          onStory={onStory}
          info={info}
          node={current}
          hero={hero}
          now={now}
          busy={busy}
          state={nodeState(current, hero, now)}
          onVisit={onVisit}
          onFight={onFight}
        />
      )}
    </div>
  );
};

const NodeDetail = ({
  onStory,
  info,
  node,
  hero,
  now,
  busy,
  state,
  onVisit,
  onFight,
}) => {
  const { t } = useTranslation();
  const [team, setTeam] = useState([]);
  const text = useDungeonText();
  const sectText = useSectText();
  const texts = getNodeText(t);
  const kinds = getNodeKindNames(t);
  const keys = getKeyText(t);
  const chapters = getChapterText(t);
  const enemyNames = getEnemyNames(t);
  const own = texts[node.id];
  const clears = hero.nodes[node.id]?.clears || 0;
  const first = clears === 0;
  const fights = ['combat', 'elite', 'boss'].includes(node.kind);
  const Icon = KIND_ICONS[node.kind];

  let lock = '';
  if (state === 'locked') {
    if (node.chapter > hero.realm) {
      lock = t('要先突破到{{realm}}才能进入{{chapter}}', {
        realm: sectText.realms[node.chapter],
        chapter: chapters[node.chapter]?.place,
      });
    } else {
      const missing = node.requires.filter(
        (id) => !(hero.nodes[id]?.clears > 0),
      );
      lock = t('先走过：{{nodes}}', {
        nodes: missing.map((id) => texts[id]?.name).join('、'),
      });
    }
  }

  const reward = first ? node.first : node.repeat;
  const repeatsLeft = hero.repeats_left;
  return (
    <section className={`jn-detail is-${node.kind}`}>
      <header>
        <Icon size={18} />
        <h4>{own.name}</h4>
        <span className='jn-kind'>{kinds[node.kind]}</span>
        {clears > 0 && (
          <span className='jn-muted'>
            {t('已通过 {{n}} 次', { n: clears })}
          </span>
        )}
      </header>
      <p className='jn-desc'>{own.desc || own.lines?.[0]?.text}</p>
      {fights && (
        <div className='jn-enemies'>
          {node.enemies.map((id, index) => (
            <span key={index} className='jn-enemy'>
              <span className='jn-enemy-art'>
                <EnemyArt id={id} />
              </span>
              {enemyNames[id]}
            </span>
          ))}
          <span className='jn-muted'>
            {t('生命 ×{{hp}}% · 伤害 ×{{dmg}}%', {
              hp: node.scale[0],
              dmg: node.scale[1],
            })}
          </span>
        </div>
      )}
      {(first || node.kind !== 'story') && (
        <div className='jn-rewards'>
          <span className='jn-reward-label'>
            {first ? t('首次奖励') : t('再次奖励')}
          </span>
          {node.exp > 0 && (
            <span className='jn-reward'>
              <Sparkles size={13} />
              {t('修为 +{{n}}', {
                n: formatAmount(first ? node.exp : Math.round(node.exp / 2)),
              })}
            </span>
          )}
          <CostChips cost={reward} sign='+' />
          {first && node.relic && <RelicChip id={node.relic} />}
          {first &&
            (node.potions || []).map((id, index) => (
              <span key={index} className='jn-reward'>
                <FlaskConical size={13} />
                {text.potions[id]?.name}
              </span>
            ))}
          {first && node.key && (
            <span className='jn-reward is-key'>
              <KeyRound size={13} />
              {keys[node.key]?.name}
            </span>
          )}
          {fights && (
            <span className='jn-reward'>
              <Gem size={13} />
              {t('功法三选一')}
            </span>
          )}
        </div>
      )}
      {fights && (state === 'open' || state === 'repeat') && (
        <TeamPicker
          companions={hero.companions}
          max={info.companion_max}
          team={team}
          onChange={setTeam}
        />
      )}
      {!first && node.kind !== 'story' && (
        <p className='jn-muted'>
          {repeatsLeft > 0
            ? t('今天还有 {{n}} 次重复通过有资源', { n: repeatsLeft })
            : t('今天重复通过的资源已经领完，修为照拿')}
        </p>
      )}
      <div className='jn-detail-actions'>
        {lock && <span className='jn-lock'>{lock}</span>}
        {state === 'cooling' && (
          <span className='jn-lock'>
            {t('{{left}}后可以再来', {
              left: formatLeft(hero.ready[node.id] - now, t),
            })}
          </span>
        )}
        {node.kind === 'story' && state === 'open' && (
          <Button
            theme='solid'
            disabled={busy}
            onClick={() =>
              onStory({
                lines: own.lines,
                chapter: node.chapter,
                title: own.name,
                doneLabel: t('继续'),
                onDone: () => onVisit(node.id),
              })
            }
          >
            {t('前往')}
          </Button>
        )}
        {node.kind === 'story' && state === 'done' && (
          <Button
            disabled={busy}
            onClick={() =>
              onStory({
                lines: own.lines,
                chapter: node.chapter,
                title: own.name,
                doneLabel: t('合上'),
              })
            }
          >
            {t('回顾')}
          </Button>
        )}
        {node.kind === 'gather' && (state === 'open' || state === 'repeat') && (
          <Button
            theme='solid'
            disabled={busy}
            onClick={() => onVisit(node.id)}
          >
            {t('采集')}
          </Button>
        )}
        {fights && (state === 'open' || state === 'repeat') && (
          <Button
            theme='solid'
            type={node.kind === 'boss' ? 'danger' : 'primary'}
            disabled={busy || hero.deck.length < 8}
            onClick={() => {
              const fight = () =>
                onFight(node.id, readyTeam(team, hero.companions));
              if (first && own.intro) {
                onStory({
                  lines: own.intro,
                  chapter: node.chapter,
                  title: own.name,
                  doneLabel: t('开战'),
                  onDone: fight,
                });
                return;
              }
              fight();
            }}
          >
            {first ? t('开战') : t('再战')}
          </Button>
        )}
      </div>
    </section>
  );
};

export default MapPanel;
