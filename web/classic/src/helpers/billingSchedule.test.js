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
import {
  analyzeExprMultiplier,
  analyzeTierSchedule,
  splitTieredExprBranches,
} from './billingSchedule';

const base =
  'hour("UTC") >= 12 ? tier("peak", p * 10) : tier("off_peak", p * 5)';
const now = new Date('2026-09-20T01:00:00Z');

describe('含外层乘数的分档价格', () => {
  test.each([
    [`(${base}) * (param("service_tier") == "priority" ? 2 : 1)`, 1, 2],
    [`(${base}) * (hour("UTC") < 6 ? 0.5 : 1)`, 0.5, 0.5],
    [`2 * (${base})`, 2, 2],
    [`((${base}) * 2) * 3`, 6, 6],
  ])('保留生效档位并单独计算倍率：%s', (expr, min, max) => {
    const branches = splitTieredExprBranches(expr);
    const schedule = analyzeTierSchedule(
      branches.map((branch) => ({ condExpr: branch.condition })),
      (text) => text,
      now,
    );
    expect(branches[schedule.activeIndex].label).toBe('off_peak');
    expect(schedule.scheduleLabels[1]).toBe('每天 00:00-12:00');
    expect(analyzeExprMultiplier(expr, now)).toEqual({
      min,
      max,
      resolved: true,
    });
  });

  test('依赖请求用量的档位不能误标为当前第一档', () => {
    const expr =
      '(len > 1000 ? tier("large", p * 10) : tier("small", p * 5)) * 2';
    const branches = splitTieredExprBranches(expr);
    const schedule = analyzeTierSchedule(
      branches.map((branch) => ({ condExpr: branch.condition })),
      (text) => text,
      now,
    );
    expect(schedule.activeIndex).toBe(-1);
  });
});
