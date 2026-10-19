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
import { formatEvaluation, hintForPlies, hintMarker } from './analysis';

// 测试里的 t 直接返回带变量的模板，和页面上的调用方式一致。
const t = (text, values = {}) =>
  text.replace(/\{\{(\w+)\}\}/g, (_, key) => String(values[key]));

describe('局面分析的说明文字', () => {
  test('杀棋按步数说，正数是自己将死对方', () => {
    expect(formatEvaluation(0, 3, t)).toBe('约 3 步内将死对方');
    expect(formatEvaluation(0, -2, t)).toBe('约 2 步内被将死');
  });

  test('分差大时换成约几个子，五分以内说成均势', () => {
    expect(formatEvaluation(540, 0, t)).toBe('优势约 2.0 个子');
    expect(formatEvaluation(-810, 0, t)).toBe('落后约 3.0 个子');
    expect(formatEvaluation(120, 0, t)).toBe('大致均势');
    expect(formatEvaluation(-149, 0, t)).toBe('大致均势');
    expect(formatEvaluation(0, 0, t)).toBe('大致均势');
  });

  test('分数缺失时不编造结论', () => {
    expect(formatEvaluation(Number.NaN, 0, t)).toBe('');
    expect(formatEvaluation(undefined, 0, t)).toBe('');
  });
});

describe('提示只在当时的局面上有效', () => {
  const state = { plies: 12 };
  const hint = { plies: 12, line: { from: [1, 2], to: [4, 2] } };

  test('走了一步或悔棋之后，旧提示作废', () => {
    expect(hintForPlies(hint, state)).toBe(hint);
    expect(hintForPlies(hint, { plies: 13 })).toBeNull();
    expect(hintForPlies(hint, { plies: 10 })).toBeNull();
    expect(hintForPlies(hint, null)).toBeNull();
    expect(hintForPlies(null, state)).toBeNull();
  });

  test('棋盘上标出起点与落点，内容不全时什么也不标', () => {
    expect(hintMarker(hint)).toEqual({ from: [1, 2], to: [4, 2] });
    expect(hintMarker(null)).toBeNull();
    expect(hintMarker({ plies: 12 })).toBeNull();
    expect(hintMarker({ line: { from: [1, 2] } })).toBeNull();
  });
});
