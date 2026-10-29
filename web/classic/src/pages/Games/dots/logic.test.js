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
import { boxEdges, EDGES, edgeBoxes, edgeEffect, edgeEnds } from './logic';

const drawn = (list) => {
  const edges = Array(EDGES).fill('0');
  for (const e of list) edges[e] = '1';
  return edges.join('');
};

describe('dots board', () => {
  test('numbers edges like the server', () => {
    expect(EDGES).toBe(40);
    expect(boxEdges(0)).toEqual([0, 4, 20, 21]);
    expect(boxEdges(15)).toEqual([15, 19, 38, 39]);
    expect(edgeEnds(20)).toEqual([
      [0, 0],
      [1, 0],
    ]);
    expect(edgeBoxes(4)).toEqual([0, 4]);
    expect(edgeBoxes(0)).toEqual([0]);
  });

  test('tells captures from gifts', () => {
    expect(edgeEffect(drawn([0, 4, 20]), 21)).toEqual({ capture: 1, gift: 0 });
    expect(edgeEffect(drawn([0, 20]), 4)).toEqual({ capture: 0, gift: 1 });
    expect(edgeEffect(drawn([]), 4)).toEqual({ capture: 0, gift: 0 });
  });
});
