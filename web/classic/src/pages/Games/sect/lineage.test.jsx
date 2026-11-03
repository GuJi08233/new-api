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
import LineageDetails from './LineageDetails';

const t = (key, values = {}) =>
  key.replace(/{{(\w+)}}/g, (_, key) => String(values[key]));
const name = (parts) => parts.join('-');

describe('师承展示', () => {
  test('旧弟子显示第一代，普通招募不显示授业状态', () => {
    const html = renderToStaticMarkup(
      <LineageDetails disciple={{ id: 1 }} disciples={[]} name={name} t={t} />,
    );
    expect(html).toContain('第 1 代弟子');
    expect(html).not.toContain('授业');
    expect(html).not.toContain('师承');
  });

  test('师长离宗后仍显示姓名快照和代数，不声称仍有授业加成', () => {
    const html = renderToStaticMarkup(
      <LineageDetails
        disciple={{
          teacher_id: 7,
          teacher_name: [1, 2, 3],
          generation: 3,
          teaching_pct: 0,
        }}
        disciples={[]}
        name={name}
        t={t}
      />,
    );
    expect(html).toContain('第 3 代弟子');
    expect(html).toContain('师承：1-2-3');
    expect(html).toContain('师长已离宗，师承记录保留');
    expect(html).not.toContain('自然修为 +');
  });

  test('只展示服务端当前生效的自然修为加成，暂停时不显示有效增益', () => {
    const render = (teaching_pct) =>
      renderToStaticMarkup(
        <LineageDetails
          disciple={{ teacher_id: 7, generation: 2, teaching_pct }}
          disciples={[{ id: 7, name: [4, 5, -1] }]}
          name={name}
          t={t}
        />,
      );
    expect(render(15)).toContain('授业生效，自然修为 +15%');
    expect(render(15)).toContain('师承：4-5--1');
    expect(render(0)).toContain('授业暂未生效');
    expect(render(0)).not.toContain('自然修为 +');
  });
});
