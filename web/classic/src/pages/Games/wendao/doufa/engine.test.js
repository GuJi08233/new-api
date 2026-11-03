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
import golden from '../../../../../../../pkg/doufa/testdata/golden.json';
import goContent from '../../../../../../../pkg/doufa/content.json';
import {
  BTN_ATTACK,
  BTN_DODGE,
  content,
  createFight,
  decodeInputs,
  encodeInputs,
  pack,
  step,
  summary,
  trialSetup,
} from './engine';

// 黄金回放是这份规则跑出来、Go 重放核对过的记录：改了规则而记录没重新生成(或 Go 没跟着改)，这里或 Go 的测试会失败。
describe('doufa engine', () => {
  test('replays every golden record to the same summary and per-tick hash', () => {
    expect(golden.length).toBeGreaterThan(0);
    for (const record of golden) {
      const fight = createFight(record.seed, record.setup);
      expect(fight).not.toBeNull();
      for (const input of decodeInputs(record.inputs)) step(fight, input);
      expect({ name: record.name, ...summary(fight) }).toEqual({
        name: record.name,
        ...record.summary,
      });
    }
  });

  test('reads the same content as the server', () => {
    expect(content).toEqual(goContent);
  });

  test('round-trips inputs through the transfer format', () => {
    const inputs = [
      0,
      0,
      0,
      pack(3, -1, BTN_ATTACK),
      pack(3, -1, BTN_ATTACK),
      pack(63, 12, BTN_ATTACK | BTN_DODGE),
      0,
    ];
    expect(decodeInputs(encodeInputs(inputs))).toEqual(inputs);
    expect(encodeInputs([])).toBe('');
  });

  test('rejects an unknown class or room', () => {
    expect(
      createFight(1, { ...trialSetup('warrior'), cls: 'monk' }),
    ).toBeNull();
    expect(
      createFight(1, { ...trialSetup('warrior'), room: 'cave' }),
    ).toBeNull();
    expect(
      createFight(1, { ...trialSetup('warrior'), weapons: ['halberd'] }),
    ).toBeNull();
  });
});
