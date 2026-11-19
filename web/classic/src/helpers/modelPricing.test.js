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
  calculateModelPrice,
  getModelPriceSortValue,
  resolvePricingGroup,
  sortPricingModels,
} from './modelPricing';

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

describe('模型广场排序', () => {
  const groupRatio = { default: 1, vip: 0.5, free: 0 };

  test('resolvePricingGroup 按选中分组或最低可用倍率分组解析', () => {
    const model = { enable_groups: ['default', 'vip'] };
    expect(resolvePricingGroup(model, 'default', groupRatio)).toEqual({
      usedGroup: 'default',
      usedGroupRatio: 1,
    });
    expect(resolvePricingGroup(model, 'all', groupRatio)).toEqual({
      usedGroup: 'vip',
      usedGroupRatio: 0.5,
    });
    expect(
      resolvePricingGroup(
        { enable_groups: ['default', 'free'] },
        'all',
        groupRatio,
      ),
    ).toEqual({
      usedGroup: 'free',
      usedGroupRatio: 0,
    });
  });

  test('按输入价格排序并将无固定价格或按次模型置后', () => {
    const models = [
      {
        model_name: 'per-call',
        quota_type: 1,
        model_price: 0.1,
        enable_groups: ['default'],
      },
      {
        model_name: 'expensive',
        quota_type: 0,
        model_ratio: 5,
        completion_ratio: 2,
        enable_groups: ['default'],
      },
      {
        model_name: 'tiered',
        quota_type: 0,
        model_ratio: 1,
        billing_mode: 'tiered_expr',
        enable_groups: ['default'],
      },
      {
        model_name: 'cheap',
        quota_type: 0,
        model_ratio: 1,
        completion_ratio: 3,
        enable_groups: ['default'],
      },
    ];

    expect(
      sortPricingModels(models, {
        sortBy: 'input_price',
        sortDirection: 'asc',
        selectedGroup: 'default',
        groupRatio,
      }).map((m) => m.model_name),
    ).toEqual(['cheap', 'expensive', 'per-call', 'tiered']);

    expect(
      sortPricingModels(models, {
        sortBy: 'input_price',
        sortDirection: 'desc',
        selectedGroup: 'default',
        groupRatio,
      }).map((m) => m.model_name),
    ).toEqual(['expensive', 'cheap', 'per-call', 'tiered']);
  });

  test('按模型名称自然排序并支持输出与单次价格计算', () => {
    const models = [
      { model_name: 'gpt-10' },
      { model_name: 'gpt-2' },
      { model_name: 'claude-3' },
    ];
    expect(
      sortPricingModels(models, {
        sortBy: 'model_name',
        sortDirection: 'asc',
      }).map((m) => m.model_name),
    ).toEqual(['claude-3', 'gpt-2', 'gpt-10']);

    const tokenModel = {
      model_name: 'm1',
      quota_type: 0,
      model_ratio: 2,
      completion_ratio: 3,
      enable_groups: ['vip'],
    };
    expect(
      getModelPriceSortValue(tokenModel, 'output_price', 'vip', groupRatio),
    ).toBe(6);
    expect(
      getModelPriceSortValue(tokenModel, 'per_call_price', 'vip', groupRatio),
    ).toBeNull();

    const callModel = {
      model_name: 'm2',
      quota_type: 1,
      model_price: 0.4,
      enable_groups: ['vip'],
    };
    expect(
      getModelPriceSortValue(callModel, 'per_call_price', 'vip', groupRatio),
    ).toBeCloseTo(0.2);
  });
});

