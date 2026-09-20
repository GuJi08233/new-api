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
import { calculateModelPrice } from './modelPricing';

const pricing = {
  record: {
    model_name: 'example',
    enable_groups: ['default', 'vip'],
    model_ratio: 1,
    completion_ratio: 3,
    cache_ratio: 0.1,
    create_cache_ratio: 1.25,
    image_ratio: 2,
    audio_ratio: 10,
    audio_completion_ratio: 2,
    model_price: 5,
    quota_type: 0,
  },
  selectedGroup: 'vip',
  groupRatio: { default: 1, vip: 1 },
  tokenUnit: 'M',
  currency: 'USD',
  displayPrice: (value) => `$${value.toFixed(4)}`,
};

describe('分组免费定价', () => {
  test.each([
    ['group_model_ratio', 'inputPrice'],
    ['group_completion_ratio', 'completionPrice'],
    ['group_cache_ratio', 'cachePrice'],
    ['group_create_cache_ratio', 'createCachePrice'],
    ['group_image_ratio', 'imagePrice'],
    ['group_audio_ratio', 'audioInputPrice'],
    ['group_audio_completion_ratio', 'audioOutputPrice'],
  ])('%s 的显式零不回退到全局价格', (key, field) => {
    const result = calculateModelPrice({
      ...pricing,
      groupPricing: { [key]: { vip: { example: 0 } } },
    });
    expect(result[field]).toBe('$0.0000');
  });

  test('按次免费价格仍保留按次计费类型', () => {
    const result = calculateModelPrice({
      ...pricing,
      groupPricing: { group_model_price: { vip: { example: 0 } } },
    });
    expect(result.isPerToken).toBe(false);
    expect(result.price).toBe('$0.0000');
  });

  test('自动选择分组时保留免费分组', () => {
    const result = calculateModelPrice({
      ...pricing,
      selectedGroup: 'all',
      groupPricing: { group_model_ratio: { vip: { example: 0 } } },
    });
    expect(result.usedGroup).toBe('vip');
    expect(result.inputPrice).toBe('$0.0000');
  });

  test('仅覆盖输出倍率时仍继承全局输入价格', () => {
    const result = calculateModelPrice({
      ...pricing,
      groupPricing: { group_completion_ratio: { vip: { example: 0 } } },
    });
    expect(result.inputPrice).toBe('$2.0000');
    expect(result.completionPrice).toBe('$0.0000');
  });

  test('缺省倍率继续继承全局价格', () => {
    const result = calculateModelPrice({ ...pricing, groupPricing: {} });
    expect(result.inputPrice).toBe('$2.0000');
    expect(result.completionPrice).toBe('$6.0000');
  });
});
