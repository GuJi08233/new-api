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

import { useEffect, useRef, useState } from 'react';
import { SSE } from 'sse.js';
import { API, getUserIdFromLocalStorage, showError } from '../../helpers';

// 模拟盘页面共用的请求、行情推送与格式化工具。金额在接口里都是额度单位，页面按 quota_per_unit 换成 USDT 显示：
// 1 USDT = 1 美元额度。

// 把请求结果换成 { data } 或 { error }：服务端的拒绝原因原样给用户看，登录过期交给全局处理，限流与网络错误给出通用提示。
function tradeResult(promise, t) {
  return promise.then(
    (res) =>
      res.data.success ? { data: res.data.data } : { error: res.data.message },
    (error) => {
      const status = error?.response?.status;
      if (status === 401) {
        showError(error);
        return { error: t('登录已过期，请重新登录') };
      }
      if (status === 429) {
        return { error: t('操作太快了，请稍后再试') };
      }
      return {
        error: error?.response?.data?.message || t('网络异常，请稍后重试'),
      };
    },
  );
}

export function tradeGet(url, t, params) {
  return tradeResult(API.get(url, { params, skipErrorHandler: true }), t);
}

export function tradePost(url, body, t) {
  return tradeResult(API.post(url, body, { skipErrorHandler: true }), t);
}

// useTradeStream 订阅行情推送(SSE)：symbols 的 24 小时行情，kline 那个交易对的 1 分钟 K 线，book 那个交易对的盘口；
// futures 为真时订阅合约，另有 symbols 的标记价格与资金费率(mark)。断线后按 2、4、8…最多 30 秒重连。回调用 ref 保存，
// 换回调不会重连。
export function useTradeStream(
  { symbols, kline, book, futures = false, enabled = true },
  handlers,
) {
  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;
  const [connected, setConnected] = useState(false);
  const key = `${futures ? 'futures' : 'spot'}|${(symbols || []).join(',')}|${kline || ''}|${book || ''}`;

  useEffect(() => {
    if (!enabled || !symbols?.length) return undefined;
    let source = null;
    let timer = null;
    let delay = 2000;
    let stopped = false;
    const dispatch = (name) => (event) => {
      let data;
      try {
        data = JSON.parse(event.data);
      } catch {
        return;
      }
      if (name === 'status') {
        setConnected(!!data.connected);
      }
      handlersRef.current?.[name]?.(data);
    };
    const connect = () => {
      const params = new URLSearchParams({ symbols: symbols.join(',') });
      if (kline) params.set('kline', kline);
      if (book) params.set('book', book);
      if (futures) params.set('market', 'futures');
      source = new SSE(`/api/trade/stream?${params.toString()}`, {
        headers: { 'New-Api-User': getUserIdFromLocalStorage() },
        method: 'GET',
      });
      ['status', 'ticker', 'kline', 'book', 'mark'].forEach((name) =>
        source.addEventListener(name, dispatch(name)),
      );
      source.addEventListener('open', () => {
        delay = 2000;
      });
      const retry = () => {
        setConnected(false);
        if (stopped || timer) return;
        timer = setTimeout(() => {
          timer = null;
          connect();
        }, delay);
        delay = Math.min(delay * 2, 30000);
      };
      source.addEventListener('error', retry);
      source.addEventListener('readystatechange', (event) => {
        if (event.readyState === 2) retry();
      });
    };
    connect();
    return () => {
      stopped = true;
      clearTimeout(timer);
      source?.close();
    };
    // key 涵盖了 futures、symbols、kline 与 book。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, enabled]);

  return connected;
}

// toUsdt 把额度单位换成 USDT 数值。
export function toUsdt(quota, perUnit) {
  if (!perUnit) return 0;
  return Number(quota || 0) / perUnit;
}

// formatUsdt 按 USDT 显示额度单位的金额，默认两位小数，带千分位。
export function formatUsdt(quota, perUnit, digits = 2) {
  return toUsdt(quota, perUnit).toLocaleString(undefined, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  });
}

// formatSignedUsdt 是带正负号的 USDT 金额，用于盈亏。
export function formatSignedUsdt(quota, perUnit, digits = 2) {
  const value = toUsdt(quota, perUnit);
  const text = Math.abs(value).toLocaleString(undefined, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  });
  if (value > 0) return `+${text}`;
  if (value < 0) return `-${text}`;
  return text;
}

// decimalsOf 是步长(例如 "0.001"、"1")的小数位数，价格与数量按交易对的精度显示。
export function decimalsOf(step) {
  if (!step) return 2;
  const text = String(step);
  const dot = text.indexOf('.');
  if (dot < 0) return 0;
  const trimmed = text.replace(/0+$/, '');
  return Math.max(trimmed.length - dot - 1, 0);
}

// formatPrice 按位数显示价格；没有给位数时按价格大小自动取。
export function formatPrice(value, digits) {
  const number = Number(value);
  if (!Number.isFinite(number) || value === '' || value === null) return '--';
  let fraction = digits;
  if (fraction === undefined) {
    if (number >= 1000) fraction = 2;
    else if (number >= 1) fraction = 4;
    else fraction = 6;
  }
  return number.toLocaleString(undefined, {
    minimumFractionDigits: fraction,
    maximumFractionDigits: fraction,
  });
}

// formatQty 显示数量，去掉多余的零。
export function formatQty(value, digits = 8) {
  const number = Number(value);
  if (!Number.isFinite(number)) return '--';
  return number.toLocaleString(undefined, { maximumFractionDigits: digits });
}

// formatCompact 用 K/M/B 缩写大数，成交额用。
export function formatCompact(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return '--';
  const abs = Math.abs(number);
  if (abs >= 1e9) return `${(number / 1e9).toFixed(2)}B`;
  if (abs >= 1e6) return `${(number / 1e6).toFixed(2)}M`;
  if (abs >= 1e3) return `${(number / 1e3).toFixed(2)}K`;
  return number.toFixed(2);
}

// changePercent 是 24 小时涨跌幅(%)，开盘价无效时为 null。
export function changePercent(price, open) {
  const p = Number(price);
  const o = Number(open);
  if (!Number.isFinite(p) || !Number.isFinite(o) || o <= 0) return null;
  return ((p - o) / o) * 100;
}

// formatFundingRate 把资金费率(小数)显示成百分比，保留 4 位小数，与 Binance 一致。
export function formatFundingRate(rate) {
  const number = Number(rate);
  if (rate === undefined || rate === '' || !Number.isFinite(number))
    return '--';
  return `${(number * 100).toFixed(4)}%`;
}

// bracketFor 是名义价值 notional(USDT)所在的风险限额档位，带上从 1 开始的档位序号：floor ≤ notional < cap，超出最后一档时
// 按最后一档。没有档位时返回 null。
export function bracketFor(brackets, notional) {
  if (!brackets?.length) return null;
  const index = brackets.findIndex(
    (bracket) => notional >= bracket.floor && notional < bracket.cap,
  );
  const i = index >= 0 ? index : brackets.length - 1;
  return { ...brackets[i], tier: i + 1 };
}

// estimateLiquidationPrice 是合约仓位的预估强平价：保证金 margin 加浮动盈亏等于维持保证金时的标记价格，金额都是 USDT。
// 维持保证金按强平时名义价值所在的档位算，与后端相同：逐档求出多仓 (开仓价值 - 保证金 - 速算数) / (数量 × (1 - 维持保证金率))、
// 空仓 (开仓价值 + 保证金 + 速算数) / (数量 × (1 + 维持保证金率))，取名义价值正好落在这一档里的那个。全仓的 margin 是撑着
// 这个仓位的权益。不会强平时返回 0。
export function estimateLiquidationPrice(
  side,
  entryValue,
  qty,
  margin,
  brackets,
) {
  if (!(qty > 0) || !brackets?.length) return 0;
  const candidate = (bracket) =>
    side === 'short'
      ? (entryValue + margin + bracket.maintAmount) / (qty * (1 + bracket.mmr))
      : (entryValue - margin - bracket.maintAmount) / (qty * (1 - bracket.mmr));
  let price = candidate(bracketFor(brackets, entryValue));
  for (let i = 0; i < brackets.length; i += 1) {
    const tierPrice = candidate(brackets[i]);
    const notional = tierPrice * qty;
    if (
      notional >= brackets[i].floor &&
      (notional < brackets[i].cap || i === brackets.length - 1)
    ) {
      price = tierPrice;
      break;
    }
  }
  return price > 0 ? price : 0;
}

// 合约的风险限额档位在一次页面访问里只读一次。
const bracketsCache = new Map();

// useFuturesBrackets 读一个合约的风险限额档位(名义价值区间、最高杠杆、维持保证金率与速算数，都换成数值)与现在能选的最高杠杆。
export function useFuturesBrackets(symbol, t) {
  const [info, setInfo] = useState(() => bracketsCache.get(symbol) || null);

  useEffect(() => {
    if (!symbol) return undefined;
    const cached = bracketsCache.get(symbol);
    if (cached) {
      setInfo(cached);
      return undefined;
    }
    let alive = true;
    tradeGet('/api/trade/futures/brackets', t, { symbol }).then((res) => {
      if (!alive || !res.data) return;
      const next = {
        maxLeverage: res.data.max_leverage,
        brackets: (res.data.brackets || []).map((bracket) => ({
          floor: Number(bracket.floor),
          cap: Number(bracket.cap),
          maxLeverage: bracket.max_leverage,
          mmr: Number(bracket.mmr),
          maintAmount: Number(bracket.maint_amount),
        })),
      };
      bracketsCache.set(symbol, next);
      setInfo(next);
    });
    return () => {
      alive = false;
    };
  }, [symbol, t]);

  return info;
}

// leverageMarks 是杠杆滑块上的刻度：两端加上中间整齐的倍数(最高超过 50 倍时每 25 倍一个，超过 20 倍时每 10 倍，否则每 5 倍)，
// 离两端不到全程十分之一的不标，免得文字挤在一起。
export function leverageMarks(min, max) {
  const step = max > 50 ? 25 : max > 20 ? 10 : 5;
  const gap = (max - min) / 10;
  const marks = { [min]: `${min}x`, [max]: `${max}x` };
  for (let value = step; value < max; value += step) {
    if (value - min >= gap && max - value >= gap) marks[value] = `${value}x`;
  }
  return marks;
}

// formatDuration 把秒数显示成最大的两个单位：3天4时、5时12分、8分30秒、42秒。
export function formatDuration(seconds, t) {
  const total = Math.max(Math.floor(Number(seconds) || 0), 0);
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (days > 0) return t('{{days}}天{{hours}}时', { days, hours });
  if (hours > 0) return t('{{hours}}时{{minutes}}分', { hours, minutes });
  if (minutes > 0)
    return t('{{minutes}}分{{seconds}}秒', { minutes, seconds: total % 60 });
  return t('{{seconds}}秒', { seconds: total });
}

// trendClass 是涨跌的文字颜色：涨绿跌红，与 K 线一致。
export function trendClass(value) {
  if (value > 0) return 'trade-up';
  if (value < 0) return 'trade-down';
  return '';
}

// floorToStep 把数量按步长向下取整，返回字符串，避免浮点误差传给后端。
export function floorToStep(value, step) {
  const number = Number(value);
  const size = Number(step);
  if (!Number.isFinite(number) || number <= 0) return '';
  if (!Number.isFinite(size) || size <= 0) return String(number);
  const digits = decimalsOf(step);
  const units = Math.floor(number / size + 1e-9);
  return (units * size).toFixed(digits);
}

// 图表库按 UTC 显示时间，平移本地时区的偏移量后横轴就是本地时间。
export function chartTime(ms) {
  return Math.floor(ms / 1000) - new Date(ms).getTimezoneOffset() * 60;
}

// cssColor 读 Semi 主题的颜色变量，读不到时用 fallback。
export function cssColor(name, fallback) {
  const value = getComputedStyle(document.body).getPropertyValue(name).trim();
  return value || fallback;
}

// withAlpha 把颜色的透明度乘上 alpha。Semi 的颜色变量本身可能带透明度(边框色是 0.08)，要在它的基础上调，不能直接覆盖。
export function withAlpha(color, alpha) {
  const match = color.match(/rgba?\(([^)]+)\)/);
  if (match) {
    const [r, g, b, a = '1'] = match[1].split(',').map((part) => part.trim());
    return `rgba(${r},${g},${b},${Number(a) * alpha})`;
  }
  if (color.startsWith('#') && color.length === 7) {
    const value = parseInt(color.slice(1), 16);
    return `rgba(${(value >> 16) & 255},${(value >> 8) & 255},${value & 255},${alpha})`;
  }
  return color;
}
