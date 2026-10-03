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
技术指标计算移植自 WhatIfIBought(https://github.com/mamawai/wtfibought)的 wiib-web/src/lib/indicators.ts，
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

// K 线副图与叠加线的指标：EMA 以前 period 期的 SMA 起步，RSI 用 Wilder 平滑，MACD 柱乘 2(国内画法)，
// 布林带用总体标准差。每个函数返回与输入等长的数组，预热不足的位置为 null，调用方按下标对齐 K 线即可。

// maSeries 是简单移动平均，用滑动窗口累加。
export function maSeries(data, period) {
  const out = new Array(data.length).fill(null);
  if (data.length < period) return out;
  let sum = 0;
  for (let i = 0; i < period; i++) sum += data[i];
  out[period - 1] = sum / period;
  for (let i = period; i < data.length; i++) {
    sum += data[i] - data[i - period];
    out[i] = sum / period;
  }
  return out;
}

// bollSeries 是布林带：中轨为 SMA(period)，上下轨为中轨 ± mult × 总体标准差。
export function bollSeries(closes, period = 20, mult = 2) {
  const n = closes.length;
  const mid = maSeries(closes, period);
  const upper = new Array(n).fill(null);
  const lower = new Array(n).fill(null);
  for (let i = period - 1; i < n; i++) {
    const m = mid[i];
    let sumSq = 0;
    for (let j = i - period + 1; j <= i; j++) {
      const d = closes[j] - m;
      sumSq += d * d;
    }
    const band = Math.sqrt(sumSq / period) * mult;
    upper[i] = m + band;
    lower[i] = m - band;
  }
  return { upper, mid, lower };
}

// emaSeries 是指数移动平均，k = 2/(period+1)，以前 period 期的 SMA 起步。
export function emaSeries(data, period) {
  const out = new Array(data.length).fill(null);
  if (data.length < period) return out;
  const k = 2 / (period + 1);
  let sum = 0;
  for (let i = 0; i < period; i++) sum += data[i];
  let current = sum / period;
  out[period - 1] = current;
  for (let i = period; i < data.length; i++) {
    current = data[i] * k + current * (1 - k);
    out[i] = current;
  }
  return out;
}

function rsiValue(avgGain, avgLoss) {
  if (avgLoss === 0) return 100;
  return 100 - 100 / (1 + avgGain / avgLoss);
}

// rsiSeries 是 Wilder 定义的 RSI，第 period 个下标开始有值。
export function rsiSeries(closes, period) {
  const out = new Array(closes.length).fill(null);
  if (closes.length < period + 1) return out;
  let avgGain = 0;
  let avgLoss = 0;
  for (let i = 1; i <= period; i++) {
    const d = closes[i] - closes[i - 1];
    if (d > 0) avgGain += d;
    else avgLoss += -d;
  }
  avgGain /= period;
  avgLoss /= period;
  out[period] = rsiValue(avgGain, avgLoss);
  const p1 = period - 1;
  for (let i = period + 1; i < closes.length; i++) {
    const d = closes[i] - closes[i - 1];
    if (d > 0) {
      avgGain = (avgGain * p1 + d) / period;
      avgLoss = (avgLoss * p1) / period;
    } else {
      avgGain = (avgGain * p1) / period;
      avgLoss = (avgLoss * p1 + -d) / period;
    }
    out[i] = rsiValue(avgGain, avgLoss);
  }
  return out;
}

// macdSeries 返回 DIF、DEA 与柱(乘 2)。DIF 从下标 slow-1 起有值，DEA 与柱从 slow+signal-2 起有值。
export function macdSeries(closes, fast = 12, slow = 26, signal = 9) {
  const n = closes.length;
  const emaFast = emaSeries(closes, fast);
  const emaSlow = emaSeries(closes, slow);
  const dif = new Array(n).fill(null);
  for (let i = 0; i < n; i++) {
    if (emaFast[i] !== null && emaSlow[i] !== null)
      dif[i] = emaFast[i] - emaSlow[i];
  }
  const dea = new Array(n).fill(null);
  const deaCompact = emaSeries(dif.slice(slow - 1), signal);
  for (let i = 0; i < deaCompact.length; i++) dea[slow - 1 + i] = deaCompact[i];
  const hist = new Array(n).fill(null);
  for (let i = 0; i < n; i++) {
    if (dif[i] !== null && dea[i] !== null) hist[i] = (dif[i] - dea[i]) * 2;
  }
  return { dif, dea, hist };
}
