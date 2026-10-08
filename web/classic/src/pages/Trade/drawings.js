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

/*
画线工具的数据模型、坐标换算、磁吸与几何移植自 WhatIfIBought(https://github.com/mamawai/wtfibought)的
wiib-web/src/lib/chartDrawings.ts，按 MIT 许可证使用：

MIT License

Copyright (c) 2026 mamawai

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

// K 线画线的纯逻辑：数据模型、存取、坐标换算、磁吸与几何命中，不碰 DOM 和画布(渲染在 DrawingLayer.js，交互在 useDrawings.js)。
//
// 锚点只存 K 线的开盘时刻(UTC 秒)与价格，不存图表的逻辑下标：往左加载历史时前面插进 K 线，所有下标都会右移，存下标的话
// 线会整体漂走；存时刻则每一帧现查下标，换周期也钉在同一时刻同一价位。时刻用真实的 UTC 秒而不是图表横轴的时间(按本地时区
// 平移过)，换了时区画线也不会错位。
//
// 图形种类：trend 趋势线、ray 射线、hray 水平射线、arrow 箭头、hline 水平线、vline 垂直线、channel 平行通道、rect 矩形、
// fib 斐波那契回撤、fibext 斐波那契扩展、long/short 多头/空头仓位(入场、止损、止盈三价)、range 价格区间、text 文字。
// 每个图形是 { id, kind, pts: [{ t, p }], color, text?, width?, dash? }：hline 只用 p，vline 只用 t；hray、text 一个点；
// trend、ray、arrow、rect、fib、range 两个点；channel 三个点(基线两端与平行线经过的点)；fibext 三个点(趋势起点、终点与
// 回撤落点)；long/short 三个点依次是入场、止损、止盈，止盈点的时刻总是等于止损点的时刻(区间右缘)。

// 属性条可选的线宽与颜色，第一个颜色是默认的蓝色。
export const LINE_WIDTHS = [1, 2, 3];
export const DRAW_PALETTE = [
  '#2962ff',
  '#f23645',
  '#089981',
  '#ff9800',
  '#9c27b0',
  '#00bcd4',
  '#e91e63',
  '#787b86',
];
export const DRAW_COLOR = '#2962ff';
// 仓位工具的盈亏区与价格区间的跌色，与 TradingView 一致。
export const GAIN_COLOR = '#089981';
export const LOSS_COLOR = '#f23645';

// STYLE_CAPS 是每种图形在属性条上能改什么：斐波那契、仓位与价格区间的颜色有含义(档位色、盈亏色、涨跌色)，不给改颜色和线型；
// 文字只能改颜色。
export const STYLE_CAPS = {
  trend: { color: true, line: true },
  ray: { color: true, line: true },
  hray: { color: true, line: true },
  arrow: { color: true, line: true },
  hline: { color: true, line: true },
  vline: { color: true, line: true },
  channel: { color: true, line: true },
  rect: { color: true, line: true },
  fib: { color: false, line: false },
  fibext: { color: false, line: false },
  long: { color: false, line: false },
  short: { color: false, line: false },
  range: { color: false, line: false },
  text: { color: true, line: false },
};

// dashPattern 是线型对应的 setLineDash 参数，间隔随线宽放大，粗线的点线不会糊成实线。
export function dashPattern(dash, width) {
  if (dash === 'dashed') return [width * 5, width * 3];
  if (dash === 'dotted') return [width, width * 2];
  return [];
}

// PLACE_POINTS 是每种图形要点几下(仓位工具点入场与止损两下，止盈按盈亏比生成)。
export const PLACE_POINTS = {
  trend: 2,
  ray: 2,
  hray: 1,
  arrow: 2,
  hline: 1,
  vline: 1,
  channel: 3,
  rect: 2,
  fib: 2,
  fibext: 3,
  long: 2,
  short: 2,
  range: 2,
  text: 1,
};

// POSITION_RR 是仓位工具默认的盈亏比：定好止损后止盈按 2:1 生成，之后可以拖动止盈点修改。
export const POSITION_RR = 2;

// 仓位工具始终把止损放在亏损侧、止盈放在盈利侧，拖过入场价时按原距离翻回正确的一侧。
export function normalizePositionPoints(kind, pts) {
  if ((kind !== 'long' && kind !== 'short') || pts.length < 3) return pts;
  const [entry, stop, target] = pts;
  const direction = kind === 'long' ? 1 : -1;
  return [
    entry,
    { t: stop.t, p: entry.p - direction * Math.abs(entry.p - stop.p) },
    { t: stop.t, p: entry.p + direction * Math.abs(target.p - entry.p) },
  ];
}

// finalizePoints 在点完之后补上生成的止盈点，初始盈亏比为 POSITION_RR。
export function finalizePoints(kind, pts) {
  if ((kind === 'long' || kind === 'short') && pts.length >= 2) {
    const [entry, stop] = pts;
    return normalizePositionPoints(kind, [
      entry,
      stop,
      { t: stop.t, p: entry.p + (entry.p - stop.p) * POSITION_RR },
    ]);
  }
  return pts;
}

// 斐波那契回撤与扩展的档位和配色，与 TradingView 的习惯一致：两端灰色，中间由暖到冷。
export const FIB_LEVELS = [0, 0.236, 0.382, 0.5, 0.618, 0.786, 1];
export const FIB_COLORS = [
  '#787b86',
  '#f23645',
  '#ff9800',
  '#4caf50',
  '#089981',
  '#00bcd4',
  '#787b86',
];
export const FIBEXT_LEVELS = [0, 0.382, 0.618, 1, 1.272, 1.618, 2, 2.618];
export const FIBEXT_COLORS = [
  '#787b86',
  '#ff9800',
  '#4caf50',
  '#787b86',
  '#00bcd4',
  '#2962ff',
  '#9c27b0',
  '#e91e63',
];

// 交互的像素阈值：线身的命中半径(手指比鼠标粗，8 在电脑上不误触、手机上也点得中)、端点手柄的命中半径(比线身大，想拖端点时
// 优先于拖整条线)、磁吸半径(超出就不吸，否则没法落在两个价位中间)。
export const HIT_LINE = 8;
export const HIT_HANDLE = 11;
export const MAGNET_PX = 12;

// formatSpan 把秒数显示成最大的两个单位：3d 4h、2h 15m、45m，价格区间工具用。
export function formatSpan(seconds) {
  const s = Math.abs(Math.round(seconds));
  const d = Math.floor(s / 86_400);
  const h = Math.floor((s % 86_400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`;
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  return `${m}m`;
}

// 画线按账户、市场与交易对存在浏览器里，不分周期。旧的无账户键不迁移，避免同浏览器内串到别人的画线。
const storageKey = (market, symbol, userId) =>
  userId
    ? `trade-draw:v1:${encodeURIComponent(userId)}:${encodeURIComponent(market)}:${encodeURIComponent(symbol)}`
    : null;

export function loadDrawings(market, symbol, userId) {
  try {
    const key = storageKey(market, symbol, userId);
    if (!key) return [];
    const list = JSON.parse(localStorage.getItem(key));
    if (!Array.isArray(list)) return [];
    const drawings = [];
    const ids = new Set();
    // 存档可能来自旧版本、手工修改或不完整写入。坏图形逐条跳过，不能让一项打断整个图表渲染。
    for (const d of list) {
      if (
        !d ||
        typeof d.id !== 'string' ||
        !d.id ||
        ids.has(d.id) ||
        typeof d.kind !== 'string' ||
        !Object.hasOwn(PLACE_POINTS, d.kind) ||
        !Array.isArray(d.pts)
      )
        continue;
      const count =
        d.kind === 'long' || d.kind === 'short' ? 3 : PLACE_POINTS[d.kind];
      if (
        d.pts.length !== count ||
        d.pts.some((p) => !p || !Number.isFinite(p.t) || !Number.isFinite(p.p))
      )
        continue;
      ids.add(d.id);
      drawings.push({
        id: d.id,
        kind: d.kind,
        pts: normalizePositionPoints(
          d.kind,
          d.pts.map(({ t, p }) => ({ t, p })),
        ),
        color: /^#[0-9a-f]{6}$/i.test(d.color) ? d.color : DRAW_COLOR,
        width: LINE_WIDTHS.includes(d.width) ? d.width : 1,
        dash: ['solid', 'dashed', 'dotted'].includes(d.dash) ? d.dash : 'solid',
        ...(d.kind === 'text'
          ? { text: typeof d.text === 'string' ? d.text : '' }
          : {}),
      });
    }
    return drawings;
  } catch {
    // 无痕模式不让读写或者存了坏数据：当作没画过，不影响看盘。
    return [];
  }
}

export function saveDrawings(market, symbol, drawings, userId) {
  try {
    const key = storageKey(market, symbol, userId);
    if (!key) return;
    if (drawings.length) localStorage.setItem(key, JSON.stringify(drawings));
    else localStorage.removeItem(key);
  } catch {
    // 存满了或不让写：画线留在内存里照样能用，只是刷新后没了。
  }
}

let sequence = 0;
export const newDrawingId = () =>
  `d${Date.now().toString(36)}${(sequence++).toString(36)}`;

// barSec 是一根 K 线开盘时刻的 UTC 秒，锚点的时刻用它。
export const barSec = (bar) => bar.openMs / 1000;

// 图表的上下文 ctx：bars() 是当前的 K 线(按时间升序)，idx() 是开盘时刻(UTC 秒)到下标的表，bucketSec 是周期的秒数，
// timeScale 与 series 是图表的时间轴和蜡烛线。K 线与下标表用函数取：图层比任何一帧都活得久，实时推送和加载历史都会换掉数组。

// timeToLogical 把时刻换成图表的逻辑下标(可以带小数)：正好是某根 K 线就查表；在已加载的范围里却对不上(换周期时最常见，
// 1 小时上画的点在 4 小时图上多半落在两根之间)就二分找到夹住它的两根，按实际间隔插值(行情可能缺根，硬套周期会越来越偏)；
// 超出两头(趋势线延伸到未来)按周期外推。
export function timeToLogical(t, ctx) {
  const bars = ctx.bars();
  if (!bars.length) return null;
  const i = ctx.idx().get(t);
  if (i !== undefined) return i;
  const last = bars.length - 1;
  if (t <= barSec(bars[0])) return (t - barSec(bars[0])) / ctx.bucketSec;
  if (t >= barSec(bars[last]))
    return last + (t - barSec(bars[last])) / ctx.bucketSec;
  let lo = 0;
  let hi = last;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if (barSec(bars[mid]) <= t) lo = mid;
    else hi = mid;
  }
  const span = barSec(bars[hi]) - barSec(bars[lo]);
  return span > 0 ? lo + (t - barSec(bars[lo])) / span : lo;
}

// logicalToX 把带小数的逻辑下标换成横坐标。不能把小数直接交给 logicalToCoordinate：图表库遇到非整数下标会静默返回 0，
// 卡在两根之间的锚点会全部堆到最左边。取相邻两个整数下标的坐标插值。
function logicalToX(logical, ctx) {
  const i = Math.floor(logical);
  const a = ctx.timeScale.logicalToCoordinate(i);
  if (a === null) return null;
  const fraction = logical - i;
  if (fraction === 0) return a;
  const b = ctx.timeScale.logicalToCoordinate(i + 1);
  return b === null ? a : a + fraction * (b - a);
}

// logicalToTime 把(取整后的)逻辑下标换回时刻，是 timeToLogical 的逆运算，超出两头按周期外推。
export function logicalToTime(i, ctx) {
  const bars = ctx.bars();
  if (!bars.length) return null;
  if (i >= 0 && i < bars.length) return barSec(bars[i]);
  const last = bars.length - 1;
  return i < 0
    ? barSec(bars[0]) + i * ctx.bucketSec
    : barSec(bars[last]) + (i - last) * ctx.bucketSec;
}

// anchorToPoint 把锚点换成画布上的像素坐标；图表还没布局好、算不出来时返回 null，调用方这一帧跳过。
export function anchorToPoint(anchor, ctx) {
  const logical = timeToLogical(anchor.t, ctx);
  if (logical === null) return null;
  const x = logicalToX(logical, ctx);
  const y = ctx.series.priceToCoordinate(anchor.p);
  return Number.isFinite(x) && Number.isFinite(y) ? { x, y } : null;
}

// coordToTime 把横坐标换成时刻，取整到整根 K 线，横向不会落在半根上。
export function coordToTime(x, ctx) {
  const logical = ctx.timeScale.coordinateToLogical(x);
  return logical === null ? null : logicalToTime(Math.round(logical), ctx);
}

// magnetPrice 把落点吸到同一根 K 线的开、高、低、收里像素距离最近的那个，超过 MAGNET_PX 就不吸、按原位置落点。snapped 表示
// 吸上了，渲染层据此画提示。价格轴还没准备好时返回 null，调用方放弃这次落点(硬塞 0 会画出一条钉在零轴的线)。
export function magnetPrice(t, y, ctx) {
  const free = ctx.series.coordinateToPrice(y);
  if (free === null) return null;
  const fallback = { p: free, snapped: false };
  const i = ctx.idx().get(t);
  if (i === undefined) return fallback;
  const bar = ctx.bars()[i];
  let best = MAGNET_PX;
  let bestPrice = null;
  for (const price of [bar.open, bar.high, bar.low, bar.close]) {
    const cy = ctx.series.priceToCoordinate(price);
    if (cy === null) continue;
    const distance = Math.abs(cy - y);
    if (distance < best) {
      best = distance;
      bestPrice = price;
    }
  }
  return bestPrice === null ? fallback : { p: bestPrice, snapped: true };
}

// rayEnd 是从 p0 经过 p1 一直延伸到画布边缘(宽 w、高 h)的终点：两个方向各自到边的参数取小的那个。
export function rayEnd(p0, p1, w, h) {
  const dx = p1.x - p0.x;
  const dy = p1.y - p0.y;
  if (dx === 0 && dy === 0) return p1;
  const tx = dx > 0 ? (w - p0.x) / dx : dx < 0 ? -p0.x / dx : Infinity;
  const ty = dy > 0 ? (h - p0.y) / dy : dy < 0 ? -p0.y / dy : Infinity;
  const t = Math.max(0, Math.min(tx, ty));
  return { x: p0.x + dx * t, y: p0.y + dy * t };
}

// pointInPoly 判断点是否在多边形内(射线法)，平行通道点内部拖动整体时用。
export function pointInPoly(px, py, poly) {
  let inside = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const a = poly[i];
    const b = poly[j];
    if (
      a.y > py !== b.y > py &&
      px < ((b.x - a.x) * (py - a.y)) / (b.y - a.y) + a.x
    )
      inside = !inside;
  }
  return inside;
}

// distToSegment 是点到线段的像素距离，判断有没有点中线身。
export function distToSegment(px, py, x1, y1, x2, y2) {
  const dx = x2 - x1;
  const dy = y2 - y1;
  const len2 = dx * dx + dy * dy;
  if (len2 === 0) return Math.hypot(px - x1, py - y1);
  const t = Math.max(0, Math.min(1, ((px - x1) * dx + (py - y1) * dy) / len2));
  return Math.hypot(px - (x1 + t * dx), py - (y1 + t * dy));
}
