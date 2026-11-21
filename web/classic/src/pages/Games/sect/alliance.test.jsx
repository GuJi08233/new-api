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
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, test } from 'bun:test';
import AllianceBossPanel, { previewBossBonus } from './AllianceBossPanel';

const t = (key, values = {}) =>
  key.replace(/{{(\w+)}}/g, (_, name) => String(values[name] ?? `{{${name}}}`));

const resources = { stone: '灵石', herb: '灵草', ore: '玄铁', rep: '声望' };

describe('仙盟周首领机制与协作目标', () => {
  test('三套轮换机制按阵容、血量与同盟人数计算预览增伤', () => {
    expect(
      previewBossBonus({ kind: 'iron_shell', hp: 1000, max_hp: 1000 }, [
        { element: 0 },
        { element: 1 },
      ]),
    ).toBe(0);
    expect(
      previewBossBonus({ kind: 'iron_shell', hp: 1000, max_hp: 1000 }, [
        { element: 0 },
        { element: 1 },
        { element: 2 },
      ]),
    ).toBe(20);
    expect(
      previewBossBonus({ kind: 'blood_fury', hp: 500, max_hp: 1000 }, [
        { element: 0 },
      ]),
    ).toBe(0);
    expect(
      previewBossBonus({ kind: 'blood_fury', hp: 499, max_hp: 1000 }, [
        { element: 0 },
      ]),
    ).toBe(25);
    expect(
      previewBossBonus(
        { kind: 'spirit_tide', hp: 1000, max_hp: 1000, other_attackers: 5 },
        [{ element: 0 }, { element: 1 }],
      ),
    ).toBe(40);
  });

  test('展示周首领机制、全盟阶段、个人周目标与讨伐反馈', () => {
    const html = renderToStaticMarkup(
      <AllianceBossPanel
        boss={{
          kind: 'spirit_tide',
          level: 2,
          hp: 650,
          max_hp: 1000,
          attackers: 2,
          other_attackers: 1,
          my_attacks: 3,
          stages: [
            {
              stage: 1,
              damage_pct: 30,
              stone: 240,
              rep: 24,
              pills: 0,
              reached: true,
              earned: true,
            },
            {
              stage: 2,
              damage_pct: 60,
              stone: 400,
              rep: 40,
              pills: 0,
              reached: false,
              earned: false,
            },
            {
              stage: 3,
              damage_pct: 100,
              stone: 800,
              rep: 80,
              pills: 1,
              reached: false,
              earned: false,
            },
          ],
          personal: [
            {
              id: 1,
              attacks: 1,
              stone: 200,
              rep: 20,
              pills: 0,
              done: true,
              earned: true,
            },
            {
              id: 2,
              attacks: 3,
              stone: 400,
              rep: 40,
              pills: 0,
              done: true,
              earned: true,
            },
            {
              id: 3,
              attacks: 5,
              stone: 700,
              rep: 70,
              pills: 1,
              done: false,
              earned: false,
            },
          ],
        }}
        selectedDisciples={[{ element: 0 }, { element: 2 }]}
        lastAttack={{
          damage: 360,
          bonus_pct: 20,
          killed: false,
          reward_stone: 490,
          reward_rep: 49,
          reward_pills: 0,
        }}
        resources={resources}
        breakPillName='破境丹'
        t={t}
      />,
    );
    expect(html).toContain('聚灵古蛟');
    expect(html).toContain('灵潮共鸣');
    expect(html).toContain('本周已有 2 人出战');
    expect(html).toContain('当前阵容可触发机制增伤 +20%');
    expect(html).toContain('本次讨伐造成 360 伤害');
    expect(html).toContain('机制增伤 +20%');
    expect(html).toContain('全盟协作目标');
    expect(html).toContain('削血 30%');
    expect(html).toContain('击杀妖王');
    expect(html).toContain('个人周目标（本周已讨伐 3 次）');
    expect(html).toContain('本周讨伐 5 次');
    expect(html).toContain('破境丹 ×1');
  });
});
