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
  buildFundingArbPlan,
  buildGridOrderPlan,
  rankFundingArbOpportunities,
} from './strategies';

describe('steady yield strategies', () => {
  test('ranks positive funding rate arbitrage pairs and builds delta-neutral hedge', () => {
    const spots = [
      {
        symbol: 'BTCUSDT',
        ticker: 'BTC',
        open: true,
        rules: { step_size: '0.0001', min_qty: '0.0001', min_notional: '5' },
      },
      {
        symbol: 'NVDABUSDT',
        ticker: 'NVDA',
        open: true,
        rules: { step_size: '0.01', min_qty: '0.01', min_notional: '5' },
      },
    ];
    const futures = [
      {
        symbol: 'BTCUSDT',
        ticker: 'BTC',
        open: true,
        rules: { step_size: '0.001', min_qty: '0.001', min_notional: '5' },
      },
      {
        symbol: 'NVDAUSDT',
        ticker: 'NVDA',
        open: true,
        rules: { step_size: '0.1', min_qty: '0.1', min_notional: '5' },
      },
    ];

    const ranked = rankFundingArbOpportunities(
      spots,
      futures,
      {
        BTCUSDT: { price: '100000', ask: '100000' },
        NVDABUSDT: { price: '140', ask: '140' },
      },
      {
        BTCUSDT: { price: '100100', bid: '100100', funding_rate: '0.0001' },
        NVDAUSDT: { price: '140.5', bid: '140.5', funding_rate: '0.0005' },
      },
    );
    expect(ranked.length).toBe(2);
    expect(ranked[0].ticker).toBe('NVDA');
    expect(ranked[0].aprPct).toBeCloseTo(54.75, 2);

    const plan = buildFundingArbPlan({
      opportunity: ranked[0],
      totalUsdt: 300,
      leverage: 1,
    });
    expect(plan.valid).toBe(true);
    expect(plan.spotPayload.qty).toBe(plan.futuresPayload.qty);
    expect(plan.futuresPayload.side).toBe('short');
    expect(plan.futuresPayload.margin_mode).toBe('isolated');
    expect(plan.dailyIncome).toBeGreaterThan(0);
  });

  test('builds valid spot and neutral futures grid limit orders', () => {
    const item = {
      symbol: 'ETHUSDT',
      ticker: 'ETH',
      rules: {
        tick_size: '0.01',
        step_size: '0.001',
        min_qty: '0.001',
        min_notional: '5',
      },
    };

    const spotGrid = buildGridOrderPlan({
      market: 'spot',
      symbolItem: item,
      currentPrice: 3000,
      lowerPrice: 2700,
      upperPrice: 3300,
      gridCount: 6,
      totalUsdt: 300,
      feeBps: 10,
    });
    expect(spotGrid.valid).toBe(true);
    expect(spotGrid.orders.length).toBe(3); // 2700, 2800, 2900 below 3000
    expect(spotGrid.orders.every((o) => o.payload.side === 'buy')).toBe(true);
    expect(spotGrid.profitPerGridPct).toBeGreaterThan(2);

    const futGrid = buildGridOrderPlan({
      market: 'futures',
      symbolItem: item,
      currentPrice: 3000,
      lowerPrice: 2700,
      upperPrice: 3300,
      gridCount: 6,
      totalUsdt: 300,
      leverage: 5,
      feeBps: 2,
    });
    expect(futGrid.valid).toBe(true);
    expect(futGrid.orders.some((o) => o.payload.side === 'long')).toBe(true);
    expect(futGrid.orders.some((o) => o.payload.side === 'short')).toBe(true);
  });
});
