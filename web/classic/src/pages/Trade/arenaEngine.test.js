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
  ARENA_INITIAL_CASH,
  calcArenaMetrics,
  closeArenaPosition,
  createArenaState,
  generateSyntheticCandles,
  gradeArenaResult,
  openArenaPosition,
  stepArenaCandle,
} from './arenaEngine';

function linearCandles(prices) {
  return prices.map((price, idx) => ({
    time: 1700000000 + idx * 900,
    open: price,
    high: price * 1.01,
    low: price * 0.99,
    close: price,
    volume: 1000,
  }));
}

describe('K-line blind replay arena engine', () => {
  test('profits on rising long and computes accurate return and grade', () => {
    const prices = [
      ...Array.from({ length: 12 }, () => 100),
      105,
      110,
      115,
      120,
    ];
    let state = createArenaState(linearCandles(prices), { warmup: 12 });
    state = openArenaPosition(state, {
      side: 'long',
      leverage: 5,
      fraction: 1,
    });
    expect(state.position?.side).toBe('long');
    expect(state.position?.leverage).toBe(5);

    while (!state.finished) {
      state = stepArenaCandle(state);
    }

    expect(state.finished).toBe(true);
    expect(state.position).toBeNull();
    expect(state.trades.length).toBe(1);
    expect(state.balance).toBeGreaterThan(ARENA_INITIAL_CASH * 1.9);

    const summary = gradeArenaResult(state);
    expect(summary.grade).toBe('S+');
    expect(summary.benchmarkPct).toBeCloseTo(20, 4);
    expect(summary.alphaPct).toBeGreaterThan(50);
  });

  test('triggers intrabar liquidation when price crashes past liq level', () => {
    const candles = linearCandles(Array.from({ length: 12 }, () => 100));
    candles.push({
      time: 1700000000 + 12 * 900,
      open: 100,
      high: 100,
      low: 80,
      close: 95,
      volume: 5000,
    });
    candles.push({
      time: 1700000000 + 13 * 900,
      open: 95,
      high: 96,
      low: 94,
      close: 95,
      volume: 1000,
    });
    let state = createArenaState(candles, { warmup: 12 });
    state = openArenaPosition(state, {
      side: 'long',
      leverage: 10,
      fraction: 1,
    });
    state = stepArenaCandle(state);

    expect(state.liquidated).toBe(true);
    expect(state.finished).toBe(true);
    expect(state.balance).toBeLessThanOrEqual(1);
    expect(state.trades[0].reason).toBe('liquidation');
  });

  test('reverses direction cleanly and generates deterministic synthetic candles', () => {
    const synthetic = generateSyntheticCandles(90, 123, 100);
    expect(synthetic.length).toBe(90);
    expect(synthetic[0].high).toBeGreaterThanOrEqual(synthetic[0].close);

    const prices = [...Array.from({ length: 12 }, () => 100), 110, 100, 90];
    let state = createArenaState(linearCandles(prices), { warmup: 12 });
    state = openArenaPosition(state, {
      side: 'long',
      leverage: 2,
      fraction: 0.5,
    });
    state = stepArenaCandle(state); // price 110
    state = openArenaPosition(state, {
      side: 'short',
      leverage: 2,
      fraction: 0.5,
    });
    expect(state.trades.length).toBe(1);
    expect(state.trades[0].reason).toBe('reverse');
    expect(state.trades[0].pnl).toBeGreaterThan(0);
    expect(state.position?.side).toBe('short');

    state = stepArenaCandle(state); // price 100
    const metrics = calcArenaMetrics(state);
    expect(metrics.unrealizedPnl).toBeGreaterThan(0);
    state = closeArenaPosition(state);
    expect(state.position).toBeNull();
    expect(state.trades.length).toBe(2);
  });
});
