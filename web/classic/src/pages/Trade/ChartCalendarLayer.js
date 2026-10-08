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
财经事件图层移植自 WhatIfIBought(https://github.com/mamawai/wtfibought)的 wiib-web/src/components/chart/EconMarkersLayer.ts，
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

const FONT = '700 10px system-ui, sans-serif';
const CHIP_H = 18;
const S = 10;
class ChartCalendarLayer {
  constructor(highOf) {
    /** 标记集合，CandleChart 拉完事件后赋值并调 update() */
    this.markers = [];
    /** 角标配色：纸底 + 灰描边灰图形，由 CandleChart 从 token 灌进来，切主题改完调 update() */
    this.palette = {
      fg: '#7a7e88',
      border: 'rgba(122,126,136,.45)',
      bg: '#fafaf7',
    };
    /** 每帧实测的角标矩形（pane 坐标，time → 矩形）：点击命中与弹窗定位都读它 */
    this.rects = /* @__PURE__ */ new Map();
    this.chartApi = null;
    this.seriesApi = null;
    this.highOf = highOf;
    this._views = [new PaneView(this)];
  }
  attached(p) {
    this.chartApi = p.chart;
    this.seriesApi = p.series;
    this._requestUpdate = p.requestUpdate;
  }
  detached() {
    this.chartApi = null;
    this.seriesApi = null;
    this._requestUpdate = void 0;
  }
  /** 改完 markers/palette 调它触发重绘 */
  update() {
    this._requestUpdate?.();
  }
  paneViews() {
    return this._views;
  }
  /** pane 坐标 → 命中的标记时间桶；没中 null。留 2px 容差，18px 的靶子指尖也点得中 */
  pick(x, y) {
    for (const [time, r] of this.rects) {
      if (
        x >= r.x - 2 &&
        x <= r.x + r.w + 2 &&
        y >= r.y - 2 &&
        y <= r.y + r.h + 2
      ) {
        const marker = this.markers.find((entry) => entry.time === time);
        return marker ? { events: marker.events, x: r.x, y: r.y, time } : null;
      }
    }
    return null;
  }
  /** 悬停变手型：告诉用户这个标记点得动（点击本体在 CandleChart 的 click 回调里） */
  hitTest(x, y) {
    const time = this.pick(x, y);
    if (time === null) return null;
    return {
      externalId: `econ:${time.time}`,
      zOrder: 'top',
      cursorStyle: 'pointer',
      hitTestPriority: 2,
    };
  }
}
class PaneView {
  constructor(layer) {
    this._r = new PaneRenderer(layer);
  }
  zOrder() {
    return 'top';
  }
  renderer() {
    return this._r;
  }
}
class PaneRenderer {
  constructor(layer) {
    this._layer = layer;
  }
  draw(target) {
    target.useMediaCoordinateSpace(({ context: c, mediaSize }) => {
      const L = this._layer;
      L.rects.clear();
      const chart = L.chartApi,
        series = L.seriesApi;
      if (!chart || !series || !L.markers.length) return;
      const ts = chart.timeScale();
      const { fg, border, bg } = L.palette;
      c.save();
      c.font = FONT;
      c.textBaseline = 'middle';
      c.textAlign = 'left';
      for (const m of L.markers) {
        const x = ts.timeToCoordinate(m.time);
        if (x === null || x < -20 || x > mediaSize.width + 20) continue;
        const hi = L.highOf(m.time);
        if (hi === null) continue;
        const yHigh = series.priceToCoordinate(hi);
        if (yHigh === null) continue;
        const label = m.events.length > 1 ? String(m.events.length) : '';
        const w = label ? CHIP_H + c.measureText(label).width + 6 : CHIP_H;
        const x0 = Math.round(x - w / 2);
        const y0 = Math.round(
          Math.min(
            Math.max(4, yHigh - CHIP_H - 8),
            mediaSize.height - CHIP_H - 4,
          ),
        );
        const stemTop = y0 + CHIP_H;
        if (yHigh - stemTop > 2) {
          c.strokeStyle = border;
          c.lineWidth = 1;
          c.beginPath();
          c.moveTo(x, stemTop);
          c.lineTo(x, Math.min(yHigh - 1, stemTop + 6));
          c.stroke();
        }
        c.beginPath();
        c.rect(x0, y0, w, CHIP_H);
        c.fillStyle = bg;
        c.fill();
        c.strokeStyle = border;
        c.lineWidth = 1;
        c.stroke();
        const gx = x0 + (CHIP_H - S) / 2,
          gy = y0 + (CHIP_H - S) / 2 + 1;
        c.strokeStyle = fg;
        c.lineWidth = 1.2;
        c.strokeRect(gx, gy, S, S - 1);
        c.beginPath();
        c.moveTo(gx, gy + 3);
        c.lineTo(gx + S, gy + 3);
        c.moveTo(gx + 3, gy - 2);
        c.lineTo(gx + 3, gy + 1);
        c.moveTo(gx + S - 3, gy - 2);
        c.lineTo(gx + S - 3, gy + 1);
        c.stroke();
        if (label) {
          c.fillStyle = fg;
          c.fillText(label, x0 + CHIP_H - 1, y0 + CHIP_H / 2 + 0.5);
        }
        L.rects.set(m.time, { x: x0, y: y0, w, h: CHIP_H });
      }
      c.restore();
    });
  }
}
export { ChartCalendarLayer };
