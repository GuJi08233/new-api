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

import { describe, expect, test } from 'bun:test';
// 规则导出的内容清单，由 pkg/wendao 的 TestContentManifestIsCurrent 生成并核对与规则一致。
import content from '../../../../../../pkg/wendao/testdata/content.json';
import { getEnemyNames, getPotionText, getRelicText } from '../dungeon/text';
import { getCropText, getPillText, getRareText } from '../sect/text';
import { hasEnemyArt } from './art/EnemyArt';
import { hasPortrait } from './art/Portrait';
import {
  getBaitText,
  getBeastText,
  getElixirText,
  getRefineKindText,
  getRoomText,
} from './cave/text';
import {
  getCast,
  getChapterText,
  getKeyText,
  getNodeText,
  getSignText,
} from './journey/text';

// 只核对文案在不在，不用真的翻译。
const t = (key) => key;

const nodes = getNodeText(t);
const cast = getCast(t);
const enemyNames = getEnemyNames(t);

// 每条检查都把问题收成一张单子，失败时一次看到全部缺漏。
describe('问道的内容在前端都有文案与画像', () => {
  test('每个节点都有名字、地图坐标，剧情节点有对白、其他节点有说明，说话的人都在人物表里', () => {
    const problems = [];
    for (const node of content.nodes) {
      const text = nodes[node.id];
      if (!text) {
        problems.push(`${node.id}: 没有文案`);
        continue;
      }
      if (!text.name) problems.push(`${node.id}: 没有名字`);
      const { x, y } = text;
      if (!(x >= 4 && x <= 96 && y >= 4 && y <= 94)) {
        problems.push(`${node.id}: 坐标 ${x},${y} 不在底图里`);
      }
      if (node.kind === 'story' && !text.lines?.length) {
        problems.push(`${node.id}: 剧情节点没有对白`);
      }
      if (node.kind !== 'story' && !text.desc) {
        problems.push(`${node.id}: 没有说明`);
      }
      for (const line of [...(text.lines || []), ...(text.intro || [])]) {
        if (!cast[line.who])
          problems.push(`${node.id}: 不认识说话的人 ${line.who}`);
        if (!line.text) problems.push(`${node.id}: 有一句空对白`);
      }
    }
    expect(problems).toEqual([]);
  });

  test('没有规则里不存在的节点文案', () => {
    const known = new Set(content.nodes.map((node) => node.id));
    expect(Object.keys(nodes).filter((id) => !known.has(id))).toEqual([]);
  });

  test('同一章的节点在地图上不挤在一起', () => {
    // 坐标是底图的百分比。底图 800×520，按 720px 宽算，横向 8 个单位约 58px、纵向 11 个单位约 51px，大约是一个节点
    // (圆点加下面的名字)占的地方，两个节点横竖都靠得比这还近就会叠在一起。
    const problems = [];
    const shown = content.nodes.filter((node) => nodes[node.id]);
    for (let i = 0; i < shown.length; i++) {
      for (let j = i + 1; j < shown.length; j++) {
        const [a, b] = [shown[i], shown[j]];
        if (a.chapter !== b.chapter) continue;
        const [p, q] = [nodes[a.id], nodes[b.id]];
        if (Math.abs(p.x - q.x) < 8 && Math.abs(p.y - q.y) < 11) {
          problems.push(`${a.id}(${p.x},${p.y}) 与 ${b.id}(${q.x},${q.y})`);
        }
      }
    }
    expect(problems).toEqual([]);
  });

  test('节点与问心阶上的敌人都有名字与画像', () => {
    const enemies = new Set([
      ...content.nodes.flatMap((node) => node.enemies || []),
      ...content.step_enemies,
    ]);
    const problems = [];
    for (const id of enemies) {
      if (!enemyNames[id]) problems.push(`${id}: 没有名字`);
      if (!hasEnemyArt(id)) problems.push(`${id}: 没有画像`);
    }
    expect(problems).toEqual([]);
  });

  test('剧情里的人物都有立绘或敌人画像', () => {
    const problems = [];
    for (const [who, person] of Object.entries(cast)) {
      if (person.portrait && !hasPortrait(person.portrait)) {
        problems.push(`${who}: 没有立绘 ${person.portrait}`);
      }
      if (person.enemy && !hasEnemyArt(person.enemy)) {
        problems.push(`${who}: 没有敌人画像 ${person.enemy}`);
      }
    }
    expect(problems).toEqual([]);
  });

  test('章节、机缘、奖励的法宝与丹符都有文案', () => {
    expect(getChapterText(t)).toHaveLength(content.chapters);
    const keys = getKeyText(t);
    const relics = getRelicText(t);
    const potions = getPotionText(t);
    const problems = [];
    for (const key of content.break_keys) {
      if (!keys[key]?.name) problems.push(`机缘 ${key}`);
    }
    for (const node of content.nodes) {
      if (node.key && !keys[node.key]?.name) problems.push(`机缘 ${node.key}`);
      if (node.relic && !relics[node.relic]?.name) {
        problems.push(`法宝 ${node.relic}`);
      }
      for (const potion of node.potions || []) {
        if (!potions[potion]?.name) problems.push(`丹符 ${potion}`);
      }
    }
    for (const potion of content.potions) {
      if (!potions[potion]?.name) problems.push(`丹符 ${potion}`);
    }
    expect(problems).toEqual([]);
  });

  test('洞府、祭炼路数、丹方、灵兽、饵料、丹药、灵药、灵种与天象都有文案', () => {
    const tables = {
      signs: getSignText(t),
      rooms: getRoomText(t),
      refine_kinds: getRefineKindText(t, {}),
      elixirs: getElixirText(t, {}, 0),
      beasts: getBeastText(t),
      baits: getBaitText(t),
      pills: getPillText(t),
      crops: getCropText(t),
    };
    const problems = [];
    for (const [table, text] of Object.entries(tables)) {
      for (const id of content[table]) {
        if (!text[id]?.name) problems.push(`${table}: ${id}`);
      }
    }
    const rares = getRareText(t);
    for (const id of content.rares) {
      if (!rares[id]) problems.push(`rares: ${id}`);
    }
    expect(problems).toEqual([]);
  });
});
