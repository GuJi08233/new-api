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
画线图层移植自 WhatIfIBought(https://github.com/mamawai/wtfibought)的 wiib-web/src/components/chart/DrawingLayer.ts，
按 MIT 许可证使用：

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

import {
  anchorToPoint,
  coordToTime,
  dashPattern,
  distToSegment,
  DRAW_COLOR,
  FIB_COLORS,
  FIB_LEVELS,
  FIBEXT_COLORS,
  FIBEXT_LEVELS,
  formatSpan,
  GAIN_COLOR,
  HIT_HANDLE,
  HIT_LINE,
  LOSS_COLOR,
  pointInPoly,
  POSITION_RR,
  rayEnd,
  timeToLogical,
} from './drawings.js';

// 画线图层：作为蜡烛线的 series primitive 挂上去，负责画出图形、判断点中了哪个，并在价格轴、时间轴上挂标签(只有 series
// primitive 有轴标签的钩子)。drawings、selectedId、pending、snap、cursor 等状态是公开字段，由 useDrawings 直接改，改完调
// update() 重绘：这些字段随鼠标移动每一帧都在变，不走不可变数据。

// 端点手柄的半边长、标签字体、标签文字色(底色是半透明灰，亮暗主题都看得清)。
const HANDLE = 4;
const FONT = '600 11px ui-monospace, Consolas, monospace';
const CHIP_FG = '#e6e8ee';
// 斐波那契相邻档位的纵向间距小于它就不画标签(选中时照画)，免得互相盖住；价格区间的框比它扁或窄就不画那根量尺。
const FIB_LABEL_MIN_GAP = 13;
const RANGE_RULER_MIN = 16;
const READOUT_LINE = 15;

// handle 画端点手柄：白底彩边的小方块，压在线上也看得见。
function handle(c, x, y, color) {
  c.fillStyle = '#fff';
  c.strokeStyle = color;
  c.lineWidth = 1.5;
  c.beginPath();
  c.rect(x - HANDLE, y - HANDLE, HANDLE * 2, HANDLE * 2);
  c.fill();
  c.stroke();
}

// box 是圆角矩形的路径；老 Safari 没有 roundRect，退化成直角，免得抛异常把整张图打断。
function box(c, x, y, w, h, r) {
  c.beginPath();
  if (c.roundRect) c.roundRect(x, y, w, h, r);
  else c.rect(x, y, w, h);
}

// chip 是小标签：半透明灰底加降低不透明度的文字，斐波那契的一排标签常挂在图上，不能太抢眼。align 为 right 时 x 是右边界。
function chip(c, x, y, text, fg = CHIP_FG, align = 'left') {
  c.font = FONT;
  const w = c.measureText(text).width + 8;
  const h = 15;
  const left = align === 'right' ? x - w : align === 'center' ? x - w / 2 : x;
  c.save();
  c.fillStyle = 'rgba(127,131,142,.18)';
  box(c, left, y - h / 2, w, h, 3);
  c.fill();
  c.globalAlpha = 0.72;
  c.fillStyle = fg;
  c.textBaseline = 'middle';
  c.textAlign = 'left';
  c.fillText(text, left + 4, y + 0.5);
  c.restore();
}

function seg(c, a, b) {
  c.beginPath();
  c.moveTo(a.x, a.y);
  c.lineTo(b.x, b.y);
  c.stroke();
}

// hairPx 是 w 个 CSS 像素宽的线占几个物理像素，与图表库自己的线取法一致：向下取整，至少 1。
function hairPx(ratio, w = 1) {
  return Math.max(1, Math.floor(w * ratio));
}

// crisp 把横线、竖线的线心对齐到物理像素格，屏幕缩放 125%、150% 时 1 像素的线不会糊成两格。
function crisp(v, ratio, w = 1) {
  const px = hairPx(ratio, w);
  return (Math.round(v * ratio - px / 2) + px / 2) / ratio;
}

// arrow 画带箭头的线段，实心三角在 b 端；线身停在三角底边，粗线不会从箭尖戳出来。
function arrow(c, a, b, size) {
  const angle = Math.atan2(b.y - a.y, b.x - a.x);
  const length = Math.hypot(b.x - a.x, b.y - a.y);
  const body = Math.max(0, length - size * 0.8);
  seg(c, a, {
    x: a.x + Math.cos(angle) * body,
    y: a.y + Math.sin(angle) * body,
  });
  c.beginPath();
  c.moveTo(b.x, b.y);
  c.lineTo(
    b.x - size * Math.cos(angle - Math.PI / 7),
    b.y - size * Math.sin(angle - Math.PI / 7),
  );
  c.lineTo(
    b.x - size * Math.cos(angle + Math.PI / 7),
    b.y - size * Math.sin(angle + Math.PI / 7),
  );
  c.closePath();
  c.fill();
}

function readoutSize(c, lines) {
  c.font = FONT;
  return {
    w: Math.max(...lines.map((line) => c.measureText(line).width)) + 12,
    h: lines.length * READOUT_LINE + 6,
  };
}

// readout 画实底白字的读数框(价格区间用)，(x, y) 是左上角。
function readout(c, lines, color, x, y) {
  const { w, h } = readoutSize(c, lines);
  c.save();
  c.fillStyle = alpha(color, 0.9);
  box(c, x, y, w, h, 4);
  c.fill();
  c.fillStyle = '#fff';
  c.textBaseline = 'middle';
  c.textAlign = 'center';
  lines.forEach((line, i) =>
    c.fillText(line, x + w / 2, y + 3 + READOUT_LINE * (i + 0.5)),
  );
  c.restore();
}

// alpha 把 #rrggbb 换成带透明度的 rgba。
function alpha(hex, a) {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`;
}

// AxisView 是价格轴或时间轴上的一个标签。坐标与文字每一帧现算，缩放平移时标签自己跟着走；锚点不在可视范围就不显示
// (坐标丢到画布外没用，图表库会把标签贴在轴的边上)。limit 是这根轴的长度。
class AxisView {
  constructor(coord, text, color, limit) {
    this._coord = coord;
    this._text = text;
    this._color = color;
    this._limit = limit;
  }

  coordinate() {
    return this._coord() ?? -1000;
  }

  visible() {
    const c = this._coord();
    return c !== null && c >= 0 && c <= this._limit();
  }

  text() {
    return this._text();
  }

  textColor() {
    return '#fff';
  }

  backColor() {
    return this._color;
  }
}

// channelPair 是平行通道第二条线的两端：基线整条上下平移到经过 p2，时间跨度与基线相同(同 TradingView)。基线两点在同一根
// K 线上时没法上下平移，改成左右平移。
function channelPair(p) {
  const dx = p[1].x - p[0].x;
  const dy = p[1].y - p[0].y;
  if (dx === 0 && dy === 0) return null;
  if (dx !== 0) {
    const offset = p[2].y - (p[0].y + ((p[2].x - p[0].x) / dx) * dy);
    return [
      { x: p[0].x, y: p[0].y + offset },
      { x: p[1].x, y: p[1].y + offset },
    ];
  }
  const offset = p[2].x - p[0].x;
  return [
    { x: p[0].x + offset, y: p[0].y },
    { x: p[1].x + offset, y: p[1].y },
  ];
}

// drawColor 是图形画出来的主色：价格区间按涨跌(涨蓝跌红)，其余是存的颜色，轴标签与它一致。
function drawColor(d) {
  if (d.kind === 'range')
    return d.pts[1].p >= d.pts[0].p ? DRAW_COLOR : LOSS_COLOR;
  return d.color;
}

// fibPrices 是斐波那契回撤各档的价格，同 TradingView：第一点(波段起点)是 1，第二点(波段终点)是 0，各档是从终点往回撤的比例。
// 在价格空间插值(对数价格轴下与像素插值不等价)。
function fibPrices(d) {
  const start = d.pts[0].p;
  const end = d.pts[1].p;
  return FIB_LEVELS.map((level) => end + (start - end) * level);
}

// fibextPrices 是斐波那契扩展各档的价格 = 回撤落点 + 趋势幅度 × 档位。
function fibextPrices(d) {
  const [a, b, r] = d.pts;
  return FIBEXT_LEVELS.map((level) => r.p + (b.p - a.p) * level);
}

class PaneRenderer {
  constructor(layer) {
    this._layer = layer;
  }

  draw(target) {
    const L = this._layer;
    target.useBitmapCoordinateSpace((scope) => {
      L.hpr = scope.horizontalPixelRatio;
      L.vpr = scope.verticalPixelRatio;
    });
    target.useMediaCoordinateSpace(({ context: c, mediaSize }) => {
      L.width = mediaSize.width;
      L.height = mediaSize.height;
      L.textBoxes.clear();
      c.save();
      // 隐藏只是不画，数据不动。
      if (!L.hidden) {
        for (const d of L.drawings)
          this._one(c, d, d.id === L.selectedId, false);
        if (L.pending) this._one(c, L.pending, false, true);
      }
      // 触屏的落点十字不受隐藏影响，正在画的时候必须看得见。
      this._cursor(c);
      this._snap(c);
      c.restore();
    });
  }

  // _cursor 画触屏绘制模式的落点十字：贯穿整图的一横一竖虚线与交叉点的实心圆。
  _cursor(c) {
    const L = this._layer;
    const cursor = L.cursor;
    if (!cursor) return;
    const color = L.pending?.color ?? DRAW_COLOR;
    c.save();
    c.setLineDash([4, 4]);
    c.lineWidth = 1;
    c.strokeStyle = alpha(color, 0.9);
    c.beginPath();
    c.moveTo(cursor.x, 0);
    c.lineTo(cursor.x, L.height);
    c.moveTo(0, cursor.y);
    c.lineTo(L.width, cursor.y);
    c.stroke();
    c.setLineDash([]);
    c.fillStyle = color;
    c.beginPath();
    c.arc(cursor.x, cursor.y, 3, 0, Math.PI * 2);
    c.fill();
    c.restore();
  }

  // _snap 画磁吸提示：一个空心圆，表示这一下会吸到这根 K 线的开高低收之一。
  _snap(c) {
    const L = this._layer;
    if (!L.snap) return;
    const p = anchorToPoint(L.snap, L.ctx);
    if (!p) return;
    c.setLineDash([]);
    c.strokeStyle = '#fff';
    c.lineWidth = 2;
    c.beginPath();
    c.arc(p.x, p.y, 4.5, 0, Math.PI * 2);
    c.stroke();
    c.strokeStyle = L.pending?.color ?? DRAW_COLOR;
    c.lineWidth = 1;
    c.stroke();
  }

  _one(c, d, selected, preview) {
    const L = this._layer;
    // 正在改字的文字标注由输入框盖在原处，画布上先不画它。
    if (d.id === L.editingId) return;
    // 选中不加粗，看手柄；还没画完的用虚线，与已有图形区分。
    c.lineWidth = d.width ?? 1;
    c.setLineDash(preview ? [5, 4] : dashPattern(d.dash, c.lineWidth));
    c.strokeStyle = d.color;
    c.fillStyle = d.color;
    if (d.kind === 'hline') {
      this._hline(c, d, selected);
      return;
    }
    if (d.kind === 'vline') {
      this._vline(c, d, selected);
      return;
    }
    const pts = d.pts.map((anchor) => anchorToPoint(anchor, L.ctx));
    if (pts.some((p) => p === null)) return;
    switch (d.kind) {
      case 'trend':
        seg(c, pts[0], pts[1]);
        if (selected) this._handles(c, d.color, pts[0], pts[1]);
        break;
      case 'ray':
        seg(c, pts[0], rayEnd(pts[0], pts[1], L.width, L.height));
        if (selected) this._handles(c, d.color, pts[0], pts[1]);
        break;
      case 'hray':
        this._hray(c, d, pts, selected);
        break;
      case 'arrow':
        arrow(c, pts[0], pts[1], 6 + 3 * (d.width ?? 1));
        if (selected) this._handles(c, d.color, pts[0], pts[1]);
        break;
      case 'channel':
        this._channel(c, d, pts, selected);
        break;
      case 'rect':
        this._rect(c, d, pts, selected);
        break;
      case 'fib':
        this._fib(c, d, pts, selected);
        break;
      case 'fibext':
        this._fibext(c, d, pts, selected);
        break;
      case 'long':
      case 'short':
        this._position(c, d, pts, selected);
        break;
      case 'range':
        this._range(c, d, pts, selected);
        break;
      case 'text':
        this._text(c, d, pts[0], selected);
        break;
      default:
    }
  }

  _handles(c, color, ...pts) {
    c.setLineDash([]);
    for (const q of pts) handle(c, q.x, q.y, color);
  }

  _hline(c, d, selected) {
    const L = this._layer;
    const raw = L.ctx.series.priceToCoordinate(d.pts[0].p);
    if (raw === null) return;
    const w = d.width ?? 1;
    const y = crisp(raw, L.vpr, w);
    c.lineWidth = hairPx(L.vpr, w) / L.vpr;
    seg(c, { x: 0, y }, { x: L.width, y });
    // 手柄画在点下去的那根 K 线上。
    if (!selected) return;
    const a = anchorToPoint(d.pts[0], L.ctx);
    if (a && a.x >= 0 && a.x <= L.width)
      this._handles(c, d.color, { x: a.x, y });
  }

  _vline(c, d, selected) {
    const L = this._layer;
    const a = anchorToPoint(d.pts[0], L.ctx);
    if (!a) return;
    const w = d.width ?? 1;
    const x = crisp(a.x, L.hpr, w);
    c.lineWidth = hairPx(L.hpr, w) / L.hpr;
    seg(c, { x, y: 0 }, { x, y: L.height });
    if (selected) this._handles(c, d.color, a);
  }

  // _hray 画水平射线：从锚点那根 K 线往右到图边。
  _hray(c, d, p, selected) {
    const L = this._layer;
    const w = d.width ?? 1;
    const y = crisp(p[0].y, L.vpr, w);
    c.lineWidth = hairPx(L.vpr, w) / L.vpr;
    seg(c, { x: p[0].x, y }, { x: L.width, y });
    if (selected) this._handles(c, d.color, p[0]);
  }

  // _channel 画平行通道：p0-p1 是基线，第二条线经过 p2 与基线平行，两线之间淡色填充，中间一条虚线中轴。平行在像素空间算：
  // 时间轴不等距或对数价格轴时，价格空间的平行画出来反而是歪的。
  _channel(c, d, p, selected) {
    seg(c, p[0], p[1]);
    if (p.length >= 3) {
      const q = channelPair(p);
      if (q) {
        const [q0, q1] = q;
        c.save();
        c.fillStyle = alpha(d.color, 0.08);
        c.beginPath();
        c.moveTo(p[0].x, p[0].y);
        c.lineTo(p[1].x, p[1].y);
        c.lineTo(q1.x, q1.y);
        c.lineTo(q0.x, q0.y);
        c.closePath();
        c.fill();
        c.restore();
        seg(c, q0, q1);
        c.save();
        c.setLineDash([3, 4]);
        c.lineWidth = 1;
        seg(
          c,
          { x: (p[0].x + q0.x) / 2, y: (p[0].y + q0.y) / 2 },
          { x: (p[1].x + q1.x) / 2, y: (p[1].y + q1.y) / 2 },
        );
        c.restore();
      }
    }
    if (selected) this._handles(c, d.color, ...p);
  }

  // _rect 画矩形：四条边对齐物理像素，底色是边的颜色调淡。
  _rect(c, d, p, selected) {
    const L = this._layer;
    const w = d.width ?? 1;
    const x0 = crisp(Math.min(p[0].x, p[1].x), L.hpr, w);
    const x1 = crisp(Math.max(p[0].x, p[1].x), L.hpr, w);
    const y0 = crisp(Math.min(p[0].y, p[1].y), L.vpr, w);
    const y1 = crisp(Math.max(p[0].y, p[1].y), L.vpr, w);
    c.fillStyle = alpha(d.color, 0.08);
    c.fillRect(x0, y0, x1 - x0, y1 - y0);
    c.lineWidth = hairPx(L.hpr, w) / L.hpr;
    c.strokeRect(x0, y0, x1 - x0, y1 - y0);
    if (selected) this._handles(c, d.color, p[0], p[1]);
  }

  // _fib 画斐波那契回撤：各档从两点的左端延伸到画布右边，0.382 到 0.618 之间淡色填充(最常盯的一段)。
  _fib(c, d, p, selected) {
    const L = this._layer;
    const x0 = Math.min(p[0].x, p[1].x);
    const prices = fibPrices(d);
    const goldenA = L.ctx.series.priceToCoordinate(prices[2]);
    const goldenB = L.ctx.series.priceToCoordinate(prices[4]);
    if (goldenA !== null && goldenB !== null) {
      c.fillStyle = 'rgba(8,153,129,.07)';
      c.fillRect(
        x0,
        Math.min(goldenA, goldenB),
        L.width - x0,
        Math.abs(goldenB - goldenA),
      );
    }
    this._levels(c, x0, prices, FIB_LEVELS, FIB_COLORS, selected);
    if (selected) this._handles(c, d.color, p[0], p[1]);
  }

  // _fibext 画斐波那契扩展：p0 到 p1 是一段趋势，p2 是回撤落点；三点连一条虚线折线，各档从 p2 往右延伸到图边。
  _fibext(c, d, p, selected) {
    c.save();
    c.setLineDash([4, 4]);
    c.lineWidth = 1;
    c.strokeStyle = FIBEXT_COLORS[0];
    seg(c, p[0], p[1]);
    if (p.length >= 3) seg(c, p[1], p[2]);
    c.restore();
    if (p.length >= 3)
      this._levels(
        c,
        p[2].x,
        fibextPrices(d),
        FIBEXT_LEVELS,
        FIBEXT_COLORS,
        selected,
      );
    if (selected) this._handles(c, d.color, ...p);
  }

  // _levels 画斐波那契的档位线并在左端挂上"档位% 价格"，相邻两档挤在一起时只画线不画字(选中时照画)。
  _levels(c, x0, prices, levels, colors, selected) {
    const L = this._layer;
    const ys = prices.map((price) => L.ctx.series.priceToCoordinate(price));
    const valid = ys.filter((y) => y !== null).sort((a, b) => a - b);
    const gap =
      valid.length < 2
        ? Infinity
        : valid
            .slice(1)
            .reduce((min, y, i) => Math.min(min, y - valid[i]), Infinity);
    const showLabel = selected || gap >= FIB_LABEL_MIN_GAP;
    c.lineWidth = selected ? 1.6 : 1.1;
    ys.forEach((y, i) => {
      if (y === null) return;
      c.strokeStyle = colors[i];
      seg(c, { x: x0, y }, { x: L.width, y });
      // 起点滚出左边时标签贴着左边，不跟着跑出去。
      if (showLabel) {
        chip(
          c,
          Math.max(x0, 0) + 4,
          y - 9,
          `${(levels[i] * 100).toFixed(1)}% ${prices[i].toFixed(L.opts.decimals)}`,
          colors[i],
        );
      }
    });
  }

  // _position 画多头、空头仓位：入场线、盈利区(绿)与亏损区(红)，横跨入场到止损两点的时间；止盈、止损标签贴在各自区间的
  // 内侧，入场线上挂盈亏比。还没点止损时只画一段线。
  _position(c, d, p, selected) {
    const L = this._layer;
    if (p.length < 3) {
      seg(c, p[0], p[1]);
      return;
    }
    const [entry, stop, target] = d.pts;
    const xL = Math.min(p[0].x, p[1].x);
    const xR = Math.max(p[0].x, p[1].x);
    const yE = p[0].y;
    const yS = p[1].y;
    const yT = p[2].y;
    const digits = L.opts.decimals;
    const risk = Math.abs(entry.p - stop.p);
    const reward = Math.abs(target.p - entry.p);
    const rr = risk > 0 ? reward / risk : POSITION_RR;
    const percent = (delta) =>
      entry.p > 0
        ? `${delta >= 0 ? '+' : ''}${((delta / entry.p) * 100).toFixed(2)}%`
        : '';
    c.save();
    c.setLineDash([]);
    c.fillStyle = alpha(GAIN_COLOR, 0.12);
    c.fillRect(xL, Math.min(yE, yT), xR - xL, Math.abs(yT - yE));
    c.fillStyle = alpha(LOSS_COLOR, 0.12);
    c.fillRect(xL, Math.min(yE, yS), xR - xL, Math.abs(yS - yE));
    c.lineWidth = 1;
    c.strokeStyle = GAIN_COLOR;
    seg(c, { x: xL, y: yT }, { x: xR, y: yT });
    c.strokeStyle = LOSS_COLOR;
    seg(c, { x: xL, y: yS }, { x: xR, y: yS });
    c.restore();
    c.lineWidth = selected ? 2 : 1.5;
    seg(c, { x: xL, y: yE }, { x: xR, y: yE });
    const inward = (y) => y + (yE >= y ? 9 : -9);
    const t = L.opts.t;
    chip(
      c,
      xR - 4,
      inward(yT),
      `${t('止盈')} ${target.p.toFixed(digits)} ${percent(target.p - entry.p)}`,
      GAIN_COLOR,
      'right',
    );
    chip(
      c,
      xR - 4,
      inward(yS),
      `${t('止损')} ${stop.p.toFixed(digits)} ${percent(stop.p - entry.p)}`,
      LOSS_COLOR,
      'right',
    );
    chip(
      c,
      xL + 4,
      yE - 9,
      `${d.kind === 'long' ? t('多头') : t('空头')} ${entry.p.toFixed(digits)} · RR ${rr.toFixed(2)}`,
      d.color,
    );
    if (selected) this._handles(c, d.color, p[0], p[1], p[2]);
  }

  // _range 画价格区间(从第一点量到第二点)：淡底框与一竖一横两根带箭头的量尺；读数框挂在终点那一侧(涨挂在框上、跌挂在框下)，
  // 收在图内。颜色按方向，涨蓝跌红。
  _range(c, d, p, selected) {
    const L = this._layer;
    const [a, b] = d.pts;
    const color = drawColor(d);
    const x0 = crisp(Math.min(p[0].x, p[1].x), L.hpr);
    const x1 = crisp(Math.max(p[0].x, p[1].x), L.hpr);
    const y0 = crisp(Math.min(p[0].y, p[1].y), L.vpr);
    const y1 = crisp(Math.max(p[0].y, p[1].y), L.vpr);
    c.save();
    c.setLineDash([]);
    c.fillStyle = alpha(color, 0.12);
    c.fillRect(x0, y0, x1 - x0, y1 - y0);
    c.strokeStyle = color;
    c.fillStyle = color;
    c.lineWidth = hairPx(L.hpr) / L.hpr;
    const mx = crisp((p[0].x + p[1].x) / 2, L.hpr);
    const my = crisp((p[0].y + p[1].y) / 2, L.vpr);
    if (y1 - y0 >= RANGE_RULER_MIN)
      arrow(c, { x: mx, y: p[0].y }, { x: mx, y: p[1].y }, 7);
    if (x1 - x0 >= RANGE_RULER_MIN)
      arrow(c, { x: p[0].x, y: my }, { x: p[1].x, y: my }, 7);
    c.restore();
    // 整个滚出画面就不挂读数框：框会被夹回图边，还能点中一个看不见的区间。
    if (x1 < 0 || x0 > L.width || y1 < 0 || y0 > L.height) {
      L.textBoxes.delete(d.id);
      if (selected) this._handles(c, color, p[0], p[1]);
      return;
    }
    const delta = b.p - a.p;
    const l0 = timeToLogical(a.t, L.ctx);
    const l1 = timeToLogical(b.t, L.ctx);
    const count =
      l0 !== null && l1 !== null ? Math.abs(Math.round(l1 - l0)) : 0;
    const sign = delta >= 0 ? '+' : '';
    const lines = [
      `${sign}${delta.toFixed(L.opts.decimals)} (${sign}${a.p ? ((delta / a.p) * 100).toFixed(2) : '0.00'}%)`,
      `${L.opts.t('{{count}} 根', { count })} · ${formatSpan(b.t - a.t)}`,
    ];
    const { w, h } = readoutSize(c, lines);
    const gap = 6;
    const left = Math.min(Math.max((x0 + x1) / 2 - w / 2, 2), L.width - w - 2);
    const top = Math.min(
      Math.max(p[1].y <= p[0].y ? y0 - gap - h : y1 + gap, 2),
      L.height - h - 2,
    );
    readout(c, lines, color, left, top);
    // 读数框在区间外面，点它也要能选中。
    L.textBoxes.set(d.id, { x: left, y: top, w, h });
    if (selected) this._handles(c, color, p[0], p[1]);
  }

  // _text 画文字标注：锚点一个实心点，右侧是文字；文字框的实测尺寸存下来给命中判断用(中文宽度靠估算会差很多)。
  _text(c, d, p, selected) {
    const L = this._layer;
    const text = d.text ?? '';
    c.font = FONT;
    const w = c.measureText(text).width + 12;
    const h = 19;
    const rect = { x: p.x + 9, y: p.y - h / 2, w, h };
    L.textBoxes.set(d.id, rect);
    c.setLineDash([]);
    c.fillStyle = d.color;
    c.beginPath();
    c.arc(p.x, p.y, 3, 0, Math.PI * 2);
    c.fill();
    // 文字直接浮在图上，选中时画一圈虚线框；文字带一圈暗影，叠在同色的蜡烛上也看得清。
    if (selected) {
      c.strokeStyle = d.color;
      c.lineWidth = 1;
      c.setLineDash([4, 3]);
      box(c, rect.x, rect.y, w, h, 4);
      c.stroke();
      c.setLineDash([]);
    }
    c.fillStyle = d.color;
    c.textBaseline = 'middle';
    c.textAlign = 'left';
    c.shadowColor = 'rgba(0,0,0,.5)';
    c.shadowBlur = 3;
    c.fillText(text, rect.x + 6, p.y + 0.5);
    c.shadowBlur = 0;
    c.shadowColor = 'transparent';
  }
}

class PaneView {
  constructor(layer) {
    this._renderer = new PaneRenderer(layer);
  }

  // 压在蜡烛与指标线之上。
  zOrder() {
    return 'top';
  }

  renderer() {
    return this._renderer;
  }
}

// DrawingLayer 是挂在蜡烛线上的画线图层。opts 是 { decimals 价格位数, fmtTime(UTC 秒) 时间轴标签, t 翻译 }。
export class DrawingLayer {
  constructor(ctx, opts) {
    this.ctx = ctx;
    this.opts = opts;
    this.drawings = [];
    this.selectedId = null;
    // pending 是画到一半的图形(虚线预览)，snap 是当前磁吸到的点，cursor 是触屏绘制模式的落点十字(画布像素坐标)。
    this.pending = null;
    this.snap = null;
    this.cursor = null;
    // interactive 为假时不做命中判断：画新线时划过旧线不该把光标变成移动，也不该把旧线报成悬停目标。
    this.interactive = true;
    // hidden 隐藏全部画线(画、命中、轴标签一起藏)，数据不动；editingId 是正在改字的文字标注。
    this.hidden = false;
    this.editingId = null;
    // 最近一次绘制时的画布尺寸与物理像素比：水平线、射线延伸到边缘，细线对齐像素都要用。
    this.width = 0;
    this.height = 0;
    this.hpr = 1;
    this.vpr = 1;
    // 文字框、价格区间读数框的实测矩形，命中判断读它。
    this.textBoxes = new Map();
    // 同一个数组引用：图表库按引用缓存视图，每帧新建会打掉缓存。
    this._views = [new PaneView(this)];
    this._requestUpdate = null;
    this._priceViews = [];
    this._priceSig = '';
    this._timeViews = [];
    this._timeSig = '';
  }

  attached({ requestUpdate }) {
    this._requestUpdate = requestUpdate;
  }

  detached() {
    this._requestUpdate = null;
  }

  // update 在改完状态后触发重绘。
  update() {
    this._requestUpdate?.();
  }

  paneViews() {
    return this._views;
  }

  // pick 从最上面(最后画的)往下找第一个点中的图形，返回 { id, pt }：pt 为 -1 表示点中线身或内部(拖整体)，否则是第几个锚点
  // (拖端点)。端点优先于线身，否则手柄被线身盖住永远拖不动；面状图形点内部也算点中。
  pick(x, y) {
    if (this.hidden) return null;
    const near = (a, b) => distToSegment(x, y, a.x, a.y, b.x, b.y) <= HIT_LINE;
    for (let i = this.drawings.length - 1; i >= 0; i--) {
      const d = this.drawings[i];
      if (d.kind === 'hline') {
        const ly = this.ctx.series.priceToCoordinate(d.pts[0].p);
        if (ly !== null && Math.abs(y - ly) <= HIT_LINE)
          return { id: d.id, pt: -1 };
        continue;
      }
      const p = d.pts.map((anchor) => anchorToPoint(anchor, this.ctx));
      if (p.some((q) => q === null)) continue;
      if (d.kind === 'vline') {
        if (Math.abs(x - p[0].x) <= HIT_LINE) return { id: d.id, pt: -1 };
        continue;
      }
      const onHandle = p.findIndex(
        (q) => Math.hypot(x - q.x, y - q.y) <= HIT_HANDLE,
      );
      if (onHandle >= 0) return { id: d.id, pt: onHandle };
      const whole = { id: d.id, pt: -1 };
      switch (d.kind) {
        case 'trend':
        case 'arrow':
          if (near(p[0], p[1])) return whole;
          break;
        case 'ray':
          if (near(p[0], rayEnd(p[0], p[1], this.width, this.height)))
            return whole;
          break;
        case 'hray':
          if (Math.abs(y - p[0].y) <= HIT_LINE && x >= p[0].x - HIT_LINE)
            return whole;
          break;
        case 'channel': {
          if (near(p[0], p[1])) return whole;
          const q = p.length >= 3 ? channelPair(p) : null;
          if (
            q &&
            (near(q[0], q[1]) || pointInPoly(x, y, [p[0], p[1], q[1], q[0]]))
          )
            return whole;
          break;
        }
        case 'rect':
        case 'range': {
          const x0 = Math.min(p[0].x, p[1].x) - HIT_LINE;
          const x1 = Math.max(p[0].x, p[1].x) + HIT_LINE;
          const y0 = Math.min(p[0].y, p[1].y) - HIT_LINE;
          const y1 = Math.max(p[0].y, p[1].y) + HIT_LINE;
          if (x >= x0 && x <= x1 && y >= y0 && y <= y1) return whole;
          const b = d.kind === 'range' ? this.textBoxes.get(d.id) : undefined;
          if (b && x >= b.x && x <= b.x + b.w && y >= b.y && y <= b.y + b.h)
            return whole;
          break;
        }
        case 'long':
        case 'short': {
          if (p.length < 3) break;
          const x0 = Math.min(p[0].x, p[1].x) - HIT_LINE;
          const x1 = Math.max(p[0].x, p[1].x) + HIT_LINE;
          const y0 = Math.min(p[0].y, p[1].y, p[2].y) - HIT_LINE;
          const y1 = Math.max(p[0].y, p[1].y, p[2].y) + HIT_LINE;
          if (x >= x0 && x <= x1 && y >= y0 && y <= y1) return whole;
          break;
        }
        case 'fib': {
          const x0 = Math.min(p[0].x, p[1].x);
          if (
            x >= x0 - HIT_LINE &&
            x <= this.width &&
            this._onLevel(y, fibPrices(d))
          )
            return whole;
          break;
        }
        case 'fibext': {
          if (near(p[0], p[1]) || (p.length >= 3 && near(p[1], p[2])))
            return whole;
          if (
            p.length >= 3 &&
            x >= p[2].x - HIT_LINE &&
            x <= this.width &&
            this._onLevel(y, fibextPrices(d))
          )
            return whole;
          break;
        }
        default: {
          // 文字：按实测的文字框判断；还没画过、没量过时退回锚点附近，至少能选中删掉。
          const b = this.textBoxes.get(d.id);
          if (
            b
              ? x >= b.x && x <= b.x + b.w && y >= b.y && y <= b.y + b.h
              : Math.hypot(x - p[0].x, y - p[0].y) <= HIT_HANDLE
          )
            return whole;
        }
      }
    }
    return null;
  }

  // _onLevel 判断纵坐标 y 是否落在某一档线上(斐波那契回撤与扩展用)。
  _onLevel(y, prices) {
    return prices.some((price) => {
      const ly = this.ctx.series.priceToCoordinate(price);
      return ly !== null && Math.abs(y - ly) <= HIT_LINE;
    });
  }

  hitTest(x, y) {
    if (!this.interactive) return null;
    const p = this.pick(x, y);
    if (!p) return null;
    return {
      externalId: p.pt >= 0 ? `${p.id}#${p.pt}` : p.id,
      zOrder: 'top',
      cursorStyle: p.pt >= 0 ? 'grab' : 'move',
      // 端点是点状命中，优先于线。
      hitTestPriority: p.pt >= 0 ? 2 : 1,
    };
  }

  // priceAxisViews 是价格轴上的标签：水平线、水平射线常显；选中的图形把各锚点的价格挂上去(斐波那契各档、仓位三价按各自的
  // 颜色)；触屏的落点十字显示交叉点的价格。
  priceAxisViews() {
    const items = [];
    for (const d of this.hidden ? [] : this.drawings) {
      if (d.kind === 'hline' || d.kind === 'hray')
        items.push({ p: d.pts[0].p, color: d.color });
      else if (d.id !== this.selectedId) continue;
      else if (d.kind === 'fib')
        fibPrices(d).forEach((p, i) => items.push({ p, color: FIB_COLORS[i] }));
      else if (d.kind === 'fibext')
        fibextPrices(d).forEach((p, i) =>
          items.push({ p, color: FIBEXT_COLORS[i] }),
        );
      else if (d.kind === 'long' || d.kind === 'short')
        d.pts.forEach((anchor, i) =>
          items.push({
            p: anchor.p,
            color: i === 1 ? LOSS_COLOR : i === 2 ? GAIN_COLOR : d.color,
          }),
        );
      else if (d.kind !== 'vline' && d.kind !== 'text') {
        const color = drawColor(d);
        d.pts.forEach((anchor) => items.push({ p: anchor.p, color }));
      }
    }
    const specs = items.map((item) => ({
      key: `${item.p}|${item.color}`,
      make: () =>
        new AxisView(
          () => this.ctx.series.priceToCoordinate(item.p),
          () => item.p.toFixed(this.opts.decimals),
          item.color,
          () => this.height,
        ),
    }));
    if (this.cursor) {
      specs.push({
        key: 'cursor',
        make: () =>
          new AxisView(
            () => this.cursor?.y ?? null,
            () => {
              const p = this.cursor
                ? this.ctx.series.coordinateToPrice(this.cursor.y)
                : null;
              return p === null ? '' : p.toFixed(this.opts.decimals);
            },
            DRAW_COLOR,
            () => this.height,
          ),
      });
    }
    return this._sync(specs, '_priceViews', '_priceSig');
  }

  // timeAxisViews 是时间轴上的标签：垂直线常显；选中的多点图形挂各锚点的时刻；触屏的落点十字显示交叉点的时刻。
  timeAxisViews() {
    const items = [];
    for (const d of this.hidden ? [] : this.drawings) {
      if (d.kind === 'vline') items.push({ a: d.pts[0], color: d.color });
      else if (
        d.id === this.selectedId &&
        d.kind !== 'hline' &&
        d.kind !== 'text'
      ) {
        // 仓位的止盈点与止损点同一时刻，去重。
        const color = drawColor(d);
        for (const a of d.pts)
          if (!items.some((item) => item.a.t === a.t && item.color === color))
            items.push({ a, color });
      }
    }
    const specs = items.map((item) => ({
      key: `${item.a.t}|${item.color}`,
      make: () =>
        new AxisView(
          () => anchorToPoint(item.a, this.ctx)?.x ?? null,
          () => this.opts.fmtTime(item.a.t),
          item.color,
          () => this.width,
        ),
    }));
    if (this.cursor) {
      specs.push({
        key: 'cursor',
        make: () =>
          new AxisView(
            () => this.cursor?.x ?? null,
            () => {
              const t = this.cursor
                ? coordToTime(this.cursor.x, this.ctx)
                : null;
              return t === null ? '' : this.opts.fmtTime(t);
            },
            DRAW_COLOR,
            () => this.width,
          ),
      });
    }
    return this._sync(specs, '_timeViews', '_timeSig');
  }

  // _sync 在标签集合没变时返回同一个数组(理由同 paneViews)，坐标每帧现算，缩放平移不用重建。
  _sync(specs, field, sigField) {
    const sig = specs.map((spec) => spec.key).join('~');
    if (sig !== this[sigField]) {
      this[sigField] = sig;
      this[field] = specs.map((spec) => spec.make());
    }
    return this[field];
  }
}
