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

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, test } from 'node:test';
import { DrawingLayer } from './DrawingLayer.js';
import {
  anchorToPoint,
  coordToTime,
  DRAW_COLOR,
  finalizePoints,
  loadDrawings,
  magnetPrice,
  normalizePositionPoints,
  rayEnd,
  saveDrawings,
  timeToLogical,
} from './drawings.js';

function chartContext(times = [0, 60, 180], bucketSec = 60) {
  let bars;
  let index;
  let origin = 40;
  const setTimes = (next) => {
    bars = next.map((time) => ({
      openMs: time * 1000,
      open: 100,
      high: 110,
      low: 90,
      close: 105,
    }));
    index = new Map(next.map((time, i) => [time, i]));
  };
  setTimes(times);
  return {
    ctx: {
      bars: () => bars,
      idx: () => index,
      bucketSec,
      timeScale: {
        // lightweight-charts 5.2 对非整数 logical 返回 0，需由画线层插值。
        logicalToCoordinate: (i) => (Number.isInteger(i) ? origin + i * 20 : 0),
        coordinateToLogical: (x) => Math.round((x - origin) / 20),
      },
      series: {
        priceToCoordinate: (p) => 300 - p,
        coordinateToPrice: (y) => 300 - y,
      },
    },
    setTimes,
    setOrigin: (value) => {
      origin = value;
    },
  };
}

function drawing(
  id,
  kind = 'trend',
  pts = [
    { t: 0, p: 200 },
    { t: 180, p: 200 },
  ],
) {
  return { id, kind, pts, color: DRAW_COLOR, width: 1, dash: 'solid' };
}

describe('画线存储', () => {
  let entries;
  let previous;
  beforeEach(() => {
    entries = new Map();
    previous = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: {
        getItem: (key) => entries.get(key) ?? null,
        setItem: (key, value) => entries.set(key, value),
        removeItem: (key) => entries.delete(key),
      },
    });
  });
  afterEach(() => {
    if (previous) Object.defineProperty(globalThis, 'localStorage', previous);
    else delete globalThis.localStorage;
  });

  test('按账户、市场和交易对隔离，删除只影响当前图表', () => {
    const first = [drawing('first')];
    const second = [drawing('second')];
    saveDrawings('spot', 'BTCUSDT', first, 1);
    saveDrawings('spot', 'BTCUSDT', second, 2);
    saveDrawings('futures', 'BTCUSDT', second, 1);
    saveDrawings('spot', 'ETHUSDT', second, 1);
    assert.deepEqual(loadDrawings('spot', 'BTCUSDT', 1), first);
    assert.deepEqual(loadDrawings('spot', 'BTCUSDT', 2), second);
    saveDrawings('spot', 'BTCUSDT', [], 1);
    assert.deepEqual(loadDrawings('spot', 'BTCUSDT', 1), []);
    assert.deepEqual(loadDrawings('spot', 'BTCUSDT', 2), second);
    assert.deepEqual(loadDrawings('futures', 'BTCUSDT', 1), second);
    assert.deepEqual(loadDrawings('spot', 'ETHUSDT', 1), second);
  });

  test('未登录不读写，无账户旧存档不泄露给登录账户', () => {
    entries.set('trade-draw:spot:BTCUSDT', JSON.stringify([drawing('legacy')]));
    saveDrawings('spot', 'BTCUSDT', [drawing('anonymous')]);
    assert.equal(entries.size, 1);
    assert.deepEqual(loadDrawings('spot', 'BTCUSDT'), []);
    assert.deepEqual(loadDrawings('spot', 'BTCUSDT', 1), []);
  });

  test('损坏图形逐条跳过，保留有效图形并恢复默认样式', () => {
    const valid = drawing('valid');
    saveDrawings(
      'spot',
      'BTCUSDT',
      [
        null,
        { ...valid, id: 'unknown', kind: 'toString' },
        { ...valid, id: 'array-kind', kind: ['trend'] },
        { ...valid, id: 'missing-point', pts: [{ t: 0, p: 1 }] },
        {
          ...valid,
          id: 'invalid-price',
          pts: [
            { t: 0, p: Infinity },
            { t: 60, p: 1 },
          ],
        },
        { ...valid, color: 'red', width: -2, dash: 'unknown' },
        { ...valid, color: '#ffffff' },
      ],
      1,
    );
    assert.deepEqual(loadDrawings('spot', 'BTCUSDT', 1), [valid]);
    const key = [...entries.keys()][0];
    entries.set(key, '{broken');
    assert.deepEqual(loadDrawings('spot', 'BTCUSDT', 1), []);
  });

  test('浏览器禁止持久化时保持操作可用', () => {
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get() {
        throw new Error('storage disabled');
      },
    });
    assert.deepEqual(loadDrawings('spot', 'BTCUSDT', 1), []);
    assert.doesNotThrow(() =>
      saveDrawings('spot', 'BTCUSDT', [drawing('a')], 1),
    );
  });
});

describe('画线坐标与磁吸', () => {
  test('缺失 K 线、跨周期锚点按实际间隔插值，未来锚点按周期外推', () => {
    const { ctx } = chartContext();
    assert.equal(timeToLogical(120, ctx), 1.5);
    assert.deepEqual(anchorToPoint({ t: 120, p: 100 }, ctx), { x: 70, y: 200 });
    assert.equal(timeToLogical(-60, ctx), -1);
    assert.equal(timeToLogical(240, ctx), 3);
    assert.equal(coordToTime(100, ctx), 240);
    const hourly = chartContext([0, 3600, 7200], 3600);
    assert.deepEqual(anchorToPoint({ t: 1800, p: 100 }, hourly.ctx), {
      x: 50,
      y: 200,
    });
  });

  test('前插历史并保持可视区域后，绝对时间锚点不漂移', () => {
    const { ctx, setTimes, setOrigin } = chartContext();
    const anchor = { t: 60, p: 105 };
    const before = anchorToPoint(anchor, ctx);
    setTimes([-120, -60, 0, 60, 180]);
    setOrigin(0);
    assert.equal(timeToLogical(anchor.t, ctx), 3);
    assert.deepEqual(anchorToPoint(anchor, ctx), before);
  });

  test('磁吸取当前 K 线最近的 OHLC，范围外或未来位置保留自由价格', () => {
    const { ctx } = chartContext();
    assert.deepEqual(magnetPrice(60, 193, ctx), { p: 105, snapped: true });
    assert.deepEqual(magnetPrice(60, 230, ctx), { p: 70, snapped: false });
    assert.deepEqual(magnetPrice(240, 193, ctx), { p: 107, snapped: false });
    ctx.series.coordinateToPrice = () => null;
    assert.equal(magnetPrice(60, 193, ctx), null);
  });
});

test('多空工具保持正确盈亏方向、初始盈亏比和区间右端', () => {
  const entry = { t: 0, p: 100 };
  assert.deepEqual(finalizePoints('long', [entry, { t: 60, p: 110 }]), [
    entry,
    { t: 60, p: 90 },
    { t: 60, p: 120 },
  ]);
  assert.deepEqual(finalizePoints('short', [entry, { t: 60, p: 90 }]), [
    entry,
    { t: 60, p: 110 },
    { t: 60, p: 80 },
  ]);
  assert.deepEqual(
    normalizePositionPoints('long', [
      entry,
      { t: 60, p: 110 },
      { t: 180, p: 70 },
    ]),
    [entry, { t: 60, p: 90 }, { t: 60, p: 130 }],
  );
});

describe('画线命中与轴标签', () => {
  test('重叠图形优先最上层，端点可拖动，隐藏和绘制模式不抢交互', () => {
    const { ctx } = chartContext();
    const layer = new DrawingLayer(ctx, { decimals: 2, fmtTime: String });
    layer.drawings = [drawing('bottom'), drawing('top')];
    assert.deepEqual(layer.pick(60, 100), { id: 'top', pt: -1 });
    assert.deepEqual(layer.pick(40, 100), { id: 'top', pt: 0 });
    assert.equal(layer.hitTest(40, 100).externalId, 'top#0');
    layer.interactive = false;
    assert.equal(layer.hitTest(40, 100), null);
    layer.hidden = true;
    assert.equal(layer.pick(60, 100), null);
  });

  test('射线延伸部分可选，起点背后的区域不可选', () => {
    const { ctx } = chartContext();
    const layer = new DrawingLayer(ctx, {});
    layer.width = 300;
    layer.height = 300;
    layer.drawings = [drawing('ray', 'ray')];
    assert.deepEqual(layer.pick(200, 100), { id: 'ray', pt: -1 });
    assert.equal(layer.pick(20, 100), null);
    assert.deepEqual(rayEnd({ x: 20, y: 20 }, { x: 20, y: 40 }, 300, 300), {
      x: 20,
      y: 300,
    });
  });

  test('价格标签随拖动更新，缩放后坐标重新计算，隐藏时移除', () => {
    const { ctx } = chartContext();
    const layer = new DrawingLayer(ctx, { decimals: 2 });
    layer.height = 300;
    layer.drawings = [drawing('level', 'hline', [{ t: 0, p: 100 }])];
    let [view] = layer.priceAxisViews();
    assert.equal(view.text(), '100.00');
    assert.equal(view.coordinate(), 200);
    layer.drawings[0].pts[0].p = 110;
    [view] = layer.priceAxisViews();
    assert.equal(view.text(), '110.00');
    ctx.series.priceToCoordinate = (p) => 500 - p * 2;
    assert.equal(view.coordinate(), 280);
    assert.equal(view.visible(), true);
    layer.hidden = true;
    assert.deepEqual(layer.priceAxisViews(), []);
  });
});
