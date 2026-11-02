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
import { combatLog } from './text';

// 用中文原文当译文，插值照填。
const t = (key, values = {}) =>
  key.replace(/\{\{(\w+)\}\}/g, (_, name) => String(values[name]));

const names = {
  enemy: (i) => ['山魈', '枯骨'][i],
  card: (id) => ({ w_strike: '崩拳', w_defend: '金钟罩' })[id],
  status: (id) => ({ burn: '真火', weak: '虚弱', thorns: '反震' })[id],
  potion: (id) => ({ fire: '火符' })[id],
  item: (id) => ({ ward: '护命符' })[id],
  relic: (id) => ({ phoenix: '涅槃羽' })[id],
  beast: (kind) => ({ baihu: '白虎' })[kind],
};

const render = (lines) =>
  lines.map((line) =>
    [line.cause, line.effects.join('，')].filter(Boolean).join('：'),
  );

describe('combatLog', () => {
  // 结束回合这一步(事件顺序与 pkg/dungeon 的 endTurn 一致)：灵兽先出手，回合结束效果，敌人各自出手(真火先烧)，然后是
  // 新回合与回合开始的效果；每件起因一行，变化跟在后面，连着一样的变化合成「×N」。
  test('an end of turn reads as one line per actor', () => {
    const lines = combatLog(
      [
        { k: 'beast', t: -1, n: 10, s: 'baihu' },
        { k: 'hit', t: 1, n: 10, s: 'blocked' },
        { k: 'end', t: -1, n: 1 },
        { k: 'block', t: -1, n: 4 },
        { k: 'hit', t: 0, n: 3, s: 'burn' },
        { k: 'act', t: 0, n: 1, s: 'claw' },
        { k: 'hit', t: -1, n: 7 },
        { k: 'apply', t: -1, n: 1, s: 'weak' },
        { k: 'act', t: 1, n: 0, s: 'guard' },
        { k: 'hit', t: -1, n: 0 },
        { k: 'hit', t: -1, n: 0 },
        { k: 'block', t: 1, n: 6 },
        { k: 'turn', t: -1, n: 2 },
        { k: 'hit', t: -1, n: 2, s: 'burn' },
      ],
      names,
      t,
    );
    expect(render(lines)).toEqual([
      '白虎出手：枯骨 −10',
      '回合结束：你 护体 +4',
      '山魈受真火灼烧：山魈 −3',
      '山魈出手：你 −7，你 虚弱 +1',
      '枯骨出手：你 挡下了 ×2，枯骨 护体 +6',
      '第 2 回合：你 真火 −2',
    ]);
    expect(lines.map((line) => line.turn)).toEqual([0, 0, 0, 0, 0, 2]);
    expect(lines.map((line) => line.quiet)).toEqual([
      false,
      true,
      false,
      false,
      false,
      true,
    ]);
  });

  test('cards, potions and items name their target', () => {
    const lines = combatLog(
      [
        { k: 'play', t: 1, n: 0, s: 'w_strike' },
        { k: 'dodge', t: 1, n: 0 },
        { k: 'play', t: -1, n: 0, s: 'w_defend' },
        { k: 'block', t: -1, n: 5 },
        { k: 'potion', t: 0, n: 0, s: 'fire' },
        { k: 'hit', t: 0, n: 20 },
        { k: 'die', t: 0, n: 0 },
        { k: 'item', t: -1, n: 1, s: 'ward' },
      ],
      names,
      t,
    );
    expect(render(lines)).toEqual([
      '施展崩拳 → 枯骨：枯骨 闪开了',
      '施展金钟罩：你 护体 +5',
      '用火符 → 山魈：山魈 −20，山魈 倒下',
      '用护命符',
    ]);
  });

  // 开战时法宝的效果记在第 1 回合名下，起因之前的变化单独成一行；复活写明是哪件东西救的。
  test('effects before any cause and revives', () => {
    const lines = combatLog(
      [
        { k: 'apply', t: 0, n: 1, s: 'weak' },
        { k: 'turn', t: -1, n: 1 },
        { k: 'block', t: -1, n: 8 },
        { k: 'act', t: 0, n: 0, s: 'bite' },
        { k: 'hit', t: -1, n: 12, s: '' },
        { k: 'revive', t: -1, n: 24, s: 'phoenix' },
        { k: 'hit', t: -1, n: 30, s: '' },
        { k: 'revive', t: -1, n: 40, s: 'ward' },
      ],
      names,
      t,
    );
    expect(render(lines)).toEqual([
      '山魈 虚弱 +1',
      '第 1 回合：你 护体 +8',
      '山魈出手：你 −12，涅槃羽生效，生命回到 24，你 −30，护命符生效，生命回到 40',
    ]);
  });
});
