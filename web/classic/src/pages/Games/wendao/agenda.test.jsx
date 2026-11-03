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
import { ReturnReport, agendaTarget } from './AgendaReport';
import { getRegionText } from './duel/text';
import { getResourceNames } from '../sect/text';

const t = (key, values = {}) =>
  key.replace(/{{(\w+)}}/g, (_, name) => String(values[name] ?? `{{${name}}}`));
const text = { regions: getRegionText(t), resources: getResourceNames(t) };
const returned = {
  id: 'return-one',
  since: 1000,
  at: 1000 + 18 * 3600,
  cap_seconds: 12 * 3600,
  seen: false,
};

function renderReport(parts) {
  return renderToStaticMarkup(
    <ReturnReport report={{ ...returned, ...parts }} text={text} t={t} />,
  );
}

describe('问道回流说明', () => {
  test('洞府修为和宗门资源分别写明本次已入账与离线截断', () => {
    const html = renderReport({
      cultivation: { kind: 'cave', seconds: 43200, exp: 240, capped: true },
      sect: {
        seconds: 43200,
        res: { stone: 24, herb: 12, ore: 6 },
        capped: true,
      },
    });
    expect(html).toContain('离开了 18 小时 0 分');
    expect(html).toContain('洞府闭关（已入账）');
    expect(html).toContain('修为 +240');
    expect(html).toContain('宗门产出（已入账）');
    expect(html).toContain('灵石 +24 · 灵草 +12 · 玄铁 +6');
    expect(html).toContain('已达 12 小时 0 分补算上限');
    expect(html).not.toContain('累计待领取');
  });

  test('游历快照只显示累计待领，不会跟宗门到账行混成已发奖励', () => {
    const html = renderReport({
      sect: { seconds: 3600, res: { stone: 10 }, capped: false },
      travel: {
        region: 0,
        elapsed: 43200,
        exp: 480,
        res: { stone: 288, herb: 144, ore: 72 },
        capped: true,
      },
    });
    const rows = html.match(/<li>.*?<\/li>/g);
    const travel = rows.find((row) => row.includes('游历'));
    expect(travel).toContain('青岚山游历（累计待领取）');
    expect(travel).toContain('累计 12 小时 0 分 · 修为 +480');
    expect(travel).toContain('灵石 +288 · 灵草 +144 · 玄铁 +72');
    expect(travel).toContain('已达本次行程上限，结束游历后入账');
    expect(travel).not.toContain('已入账');
    expect(rows.find((row) => row.includes('宗门产出'))).toContain('已入账');
  });

  test('灵脉回流保留地域零和其他地域的自然闭关来源', () => {
    for (const [region, name] of [
      [undefined, '青岚山'],
      [2, '北冥寒渊'],
    ]) {
      const html = renderReport({
        cultivation: {
          kind: 'vein',
          region,
          seconds: 3600,
          exp: 33,
          capped: false,
        },
      });
      expect(html).toContain(`${name}灵脉闭关（已入账）`);
      expect(html).toContain('补算 1 小时 0 分 · 修为 +33');
      expect(html).not.toContain('洞府闭关');
      expect(html).not.toContain('补算上限');
    }
  });

  test('圆满时展示实际零修为，空报告给明确无收益说明', () => {
    const bottleneck = renderReport({
      cultivation: { kind: 'cave', seconds: 3600, exp: 0, capped: false },
      sect: { seconds: 3600, res: {}, capped: false },
    });
    expect(bottleneck).toContain('修为 +0');
    expect(bottleneck).toContain('补算 1 小时 0 分 · 没有');
    expect(bottleneck).not.toContain('undefined');
    expect(renderReport({})).toContain('这次没有可入账或待领取的收益。');
  });
});

describe('目标和待办定位', () => {
  test('世界目标与待办保留青岚山零号地域，并定位对应活动', () => {
    expect(agendaTarget({ key: 'travel', part: 'world' })).toEqual({
      part: 'world',
      region: 0,
      view: 'travel',
    });
    expect(
      agendaTarget({ key: 'meditation', part: 'world', region: 4 }),
    ).toEqual({
      part: 'world',
      region: 4,
      view: 'meditation',
    });
    expect(agendaTarget({ key: 'duel', part: 'world', region: 2 })).toEqual({
      part: 'world',
      region: 2,
      view: 'bounties',
    });
    expect(
      agendaTarget({
        key: 'bounties',
        part: 'world',
        region: 0,
        view: 'bounties',
      }),
    ).toEqual({
      part: 'world',
      region: 0,
      view: 'bounties',
    });
  });

  test('宗门契约与历练节点保持自己的页签和节点，不带世界地域', () => {
    expect(
      agendaTarget({ key: 'duel', part: 'sect', tab: 'overview', region: 0 }),
    ).toEqual({
      part: 'sect',
      tab: 'overview',
    });
    expect(
      agendaTarget({ key: 'node', part: 'journey', tab: 'map', s: 'c1_town' }),
    ).toEqual({
      part: 'journey',
      tab: 'map',
      node: 'c1_town',
    });
  });
});
