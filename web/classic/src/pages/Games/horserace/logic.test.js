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
import { planRace, progressAt } from './logic';

describe('planRace', () => {
  test('horses cross the line in the server order and only run forward', () => {
    for (let seed = 1; seed <= 200; seed++) {
      const order = [3, 0, 5, 1, 4, 2];
      const plans = planRace(order, seed);
      const finishes = order.map((horse) => plans[horse].finish);
      expect([...finishes].sort((a, b) => a - b)).toEqual(finishes);
      for (const plan of plans) {
        let previous = 0;
        for (let t = 0; t <= plan.finish; t += 50) {
          const p = progressAt(plan, t);
          expect(p).toBeGreaterThanOrEqual(previous - 1e-9);
          previous = p;
        }
        expect(progressAt(plan, plan.finish)).toBe(1);
        expect(progressAt(plan, 0)).toBe(0);
      }
      // 第一名冲线时，其他马都还没到。
      const first = plans[order[0]].finish;
      for (const horse of order.slice(1)) {
        expect(progressAt(plans[horse], first)).toBeLessThan(1);
      }
    }
  });
});
