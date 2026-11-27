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
  PRESET_BASKETS,
  buildBasketOrderPlan,
  evaluateBasket,
  normalizeWeights,
} from './baskets';

describe('trade thematic baskets', () => {
  test('all preset baskets normalize to exactly 100%', () => {
    for (const basket of PRESET_BASKETS) {
      const normalized = normalizeWeights(basket.legs);
      const sum = normalized.reduce((acc, leg) => acc + leg.weight, 0);
      expect(Math.round(sum * 10) / 10).toBe(100);
    }
  });

  test('evaluates live weighted 24h change and mode eligibility', () => {
    const spotMap = new Map([
      [
        'BTC',
        {
          symbol: 'BTCUSDT',
          ticker: 'BTC',
          open: true,
          rules: { step_size: '0.0001', min_notional: '5' },
        },
      ],
      [
        'ETH',
        {
          symbol: 'ETHUSDT',
          ticker: 'ETH',
          open: true,
          rules: { step_size: '0.001', min_notional: '5' },
        },
      ],
    ]);
    const futuresMap = new Map([
      [
        'BTC',
        {
          symbol: 'BTCUSDT',
          ticker: 'BTC',
          open: true,
          max_leverage: 20,
          rules: { step_size: '0.001', min_qty: '0.001', min_notional: '5' },
        },
      ],
      [
        'ETH',
        {
          symbol: 'ETHUSDT',
          ticker: 'ETH',
          open: true,
          max_leverage: 15,
          rules: { step_size: '0.01', min_qty: '0.01', min_notional: '5' },
        },
      ],
    ]);

    const evaluated = evaluateBasket(
      {
        legs: [
          { ticker: 'BTC', weight: 60 },
          { ticker: 'ETH', weight: 40 },
        ],
      },
      spotMap,
      futuresMap,
      {
        BTCUSDT: { price: '110', open: '100', ask: '110' },
        ETHUSDT: { price: '95', open: '100', ask: '95' },
      },
      {
        BTCUSDT: { price: '110', open: '100', ask: '110', bid: '109.9' },
        ETHUSDT: { price: '95', open: '100', ask: '95', bid: '94.9' },
      },
    );

    expect(evaluated.canSpot).toBe(true);
    expect(evaluated.canFutures).toBe(true);
    expect(evaluated.maxFuturesLeverage).toBe(15);
    // 60% * (+10%) + 40% * (-5%) = +4%
    expect(evaluated.weightedChange).toBeCloseTo(4, 5);

    const spotPlan = buildBasketOrderPlan({
      legs: evaluated.legs,
      mode: 'spot',
      totalUsdt: 100,
    });
    expect(spotPlan.valid).toBe(true);
    expect(spotPlan.items[0].payload.amount).toBe('60.00');
    expect(spotPlan.items[1].payload.amount).toBe('40.00');

    const futuresPlan = buildBasketOrderPlan({
      legs: evaluated.legs,
      mode: 'futures',
      side: 'long',
      marginMode: 'isolated',
      leverage: 10,
      totalUsdt: 100,
    });
    expect(futuresPlan.valid).toBe(true);
    expect(futuresPlan.items[0].payload.margin_mode).toBe('isolated');
    expect(futuresPlan.items[0].payload.leverage).toBe(10);
    expect(Number(futuresPlan.items[0].qty)).toBeGreaterThan(0);
  });

  test('flags legs below minimum notional before submitting orders', () => {
    const evaluated = evaluateBasket(
      { legs: [{ ticker: 'NVDA', weight: 100 }] },
      new Map([
        [
          'NVDA',
          {
            symbol: 'NVDABUSDT',
            ticker: 'NVDA',
            open: true,
            rules: { step_size: '0.01', min_notional: '10' },
          },
        ],
      ]),
      new Map(),
      { NVDABUSDT: { price: '140', open: '135', ask: '140' } },
      {},
    );
    const plan = buildBasketOrderPlan({
      legs: evaluated.legs,
      mode: 'spot',
      totalUsdt: 5,
    });
    expect(plan.valid).toBe(false);
    expect(plan.items[0].error).toBe('below_min_notional');
  });
});
