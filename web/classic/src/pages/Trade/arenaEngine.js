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

export const ARENA_INITIAL_CASH = 10000;
export const ARENA_FEE_RATE = 0.0004;
export const ARENA_WARMUP_BARS = 40;
export const ARENA_PLAY_BARS = 50;
export const ARENA_STATS_STORAGE_KEY = 'trade-arena-stats:v1';

// 确定性伪随机(用于离线演练行情)，行情不可用时仍能完整体验盲测训练。
export function generateSyntheticCandles(
  count = 120,
  seed = 42,
  startPrice = 120,
) {
  let state = seed >>> 0 || 1;
  const rand = () => {
    state = (state * 1664525 + 1013904223) >>> 0;
    return state / 4294967296;
  };
  const nowSec = Math.floor(Date.now() / 1000) - count * 900;
  let price = startPrice;
  let drift = 0.0004;
  const out = [];
  for (let i = 0; i < count; i++) {
    if (i % 18 === 0) {
      drift = (rand() - 0.48) * 0.004;
    }
    const shock = (rand() - 0.5) * 0.022 + drift;
    const open = price;
    const close = Math.max(1, open * (1 + shock));
    const wickUp = Math.abs(rand() * 0.009) * Math.max(open, close);
    const wickDown = Math.abs(rand() * 0.009) * Math.min(open, close);
    const high = Math.max(open, close) + wickUp;
    const low = Math.max(0.5, Math.min(open, close) - wickDown);
    const volume = Math.round(
      (500 + rand() * 3500) * (1 + Math.abs(shock) * 40),
    );
    out.push({
      time: nowSec + i * 900,
      open: Number(open.toFixed(4)),
      high: Number(high.toFixed(4)),
      low: Number(low.toFixed(4)),
      close: Number(close.toFixed(4)),
      volume,
    });
    price = close;
  }
  return out;
}

export function createArenaState(
  candles,
  {
    initialCash = ARENA_INITIAL_CASH,
    feeRate = ARENA_FEE_RATE,
    warmup = ARENA_WARMUP_BARS,
  } = {},
) {
  const safeWarmup = Math.max(1, Math.min(warmup, candles.length - 1));
  const cursor = safeWarmup - 1;
  return {
    candles,
    initialCash,
    feeRate,
    warmup: safeWarmup,
    cursor,
    startPrice: candles[cursor]?.close || 1,
    balance: initialCash,
    position: null,
    trades: [],
    markers: [],
    peakEquity: initialCash,
    maxDrawdown: 0,
    liquidated: false,
    finished: false,
  };
}

export function calcLiquidationPrice(position) {
  if (!position || !(position.qty > 0)) return 0;
  const mmr = 0.005;
  const raw =
    position.side === 'long'
      ? (position.entryPrice * position.qty - position.margin) /
        (position.qty * (1 - mmr))
      : (position.entryPrice * position.qty + position.margin) /
        (position.qty * (1 + mmr));
  return raw > 0 ? raw : 0;
}

export function calcArenaMetrics(state) {
  const bar = state.candles[state.cursor];
  const price = bar?.close || 0;
  const pos = state.position;
  let unrealizedPnl = 0;
  let roe = 0;
  let liqPrice = 0;
  if (pos && pos.qty > 0) {
    const dir = pos.side === 'long' ? 1 : -1;
    unrealizedPnl = (price - pos.entryPrice) * pos.qty * dir;
    roe = pos.margin > 0 ? (unrealizedPnl / pos.margin) * 100 : 0;
    liqPrice = calcLiquidationPrice(pos);
  }
  const equity = Math.max(
    0,
    state.balance + (pos ? pos.margin + unrealizedPnl : 0),
  );
  const totalReturnPct =
    state.initialCash > 0
      ? ((equity - state.initialCash) / state.initialCash) * 100
      : 0;
  const remainingBars = Math.max(0, state.candles.length - 1 - state.cursor);
  const totalPlayBars = Math.max(1, state.candles.length - state.warmup);
  const elapsedBars = Math.max(0, state.cursor - (state.warmup - 1));

  return {
    price,
    bar,
    equity,
    unrealizedPnl,
    roe,
    liqPrice,
    totalReturnPct,
    remainingBars,
    totalPlayBars,
    elapsedBars,
  };
}

export function closeArenaPosition(
  state,
  overridePrice = null,
  reason = 'close',
) {
  if (!state.position) return state;
  const bar = state.candles[state.cursor];
  const exitPrice = overridePrice ?? bar.close;
  const pos = state.position;
  const dir = pos.side === 'long' ? 1 : -1;
  const grossPnl = (exitPrice - pos.entryPrice) * pos.qty * dir;
  const fee =
    reason === 'liquidation' ? 0 : exitPrice * pos.qty * state.feeRate;
  const netPnl = reason === 'liquidation' ? -pos.margin : grossPnl - fee;
  const returned =
    reason === 'liquidation' ? 0 : Math.max(0, pos.margin + netPnl);
  const nextBalance = state.balance + returned;
  const roe = pos.margin > 0 ? (netPnl / pos.margin) * 100 : 0;

  const trade = {
    id: state.trades.length + 1,
    side: pos.side,
    leverage: pos.leverage,
    entryPrice: pos.entryPrice,
    exitPrice,
    margin: pos.margin,
    pnl: netPnl,
    roe,
    reason,
    barIndex: state.cursor,
    time: bar.time,
  };

  const marker = {
    time: bar.time,
    position: pos.side === 'long' ? 'aboveBar' : 'belowBar',
    color: reason === 'liquidation' ? '#ef4444' : '#f59e0b',
    shape: 'circle',
    text: reason === 'liquidation' ? 'LIQ' : '平',
  };

  const peakEquity = Math.max(state.peakEquity, nextBalance);
  const dd =
    peakEquity > 0 ? ((peakEquity - nextBalance) / peakEquity) * 100 : 0;

  return {
    ...state,
    balance: nextBalance,
    position: null,
    trades: [...state.trades, trade],
    markers: [...state.markers, marker],
    peakEquity,
    maxDrawdown: Math.max(state.maxDrawdown, dd),
    liquidated: state.liquidated || reason === 'liquidation',
  };
}

export function openArenaPosition(
  state,
  { side, leverage = 5, fraction = 0.5 },
) {
  if (state.finished) return state;
  let working = state;
  if (working.position && working.position.side !== side) {
    working = closeArenaPosition(working, null, 'reverse');
  }
  const bar = working.candles[working.cursor];
  const price = bar?.close || 0;
  if (!(price > 0) || !(working.balance > 1)) return working;

  const lev = Math.max(1, Math.min(20, Math.round(Number(leverage) || 1)));
  const frac = Math.max(0.05, Math.min(1, Number(fraction) || 0.5));
  const spend = working.balance * frac;
  // 留出手续费，确保 spend = margin + notional * feeRate
  const margin = spend / (1 + lev * working.feeRate);
  if (!(margin > 0.5)) return working;
  const notional = margin * lev;
  const fee = notional * working.feeRate;
  const qty = notional / price;
  const nextBalance = Math.max(0, working.balance - margin - fee);

  let nextPos;
  if (working.position && working.position.side === side) {
    const prev = working.position;
    const totalQty = prev.qty + qty;
    const avgEntry = (prev.entryPrice * prev.qty + price * qty) / totalQty;
    const totalMargin = prev.margin + margin;
    nextPos = {
      side,
      leverage: Math.round((totalQty * avgEntry) / totalMargin) || lev,
      entryPrice: avgEntry,
      qty: totalQty,
      margin: totalMargin,
    };
  } else {
    nextPos = {
      side,
      leverage: lev,
      entryPrice: price,
      qty,
      margin,
    };
  }

  const marker = {
    time: bar.time,
    position: side === 'long' ? 'belowBar' : 'aboveBar',
    color: side === 'long' ? '#10b981' : '#ef4444',
    shape: side === 'long' ? 'arrowUp' : 'arrowDown',
    text: `${side === 'long' ? '多' : '空'} ${lev}x`,
  };

  return {
    ...working,
    balance: nextBalance,
    position: nextPos,
    markers: [...working.markers, marker],
  };
}

export function stepArenaCandle(state) {
  if (state.finished) return state;
  if (state.cursor >= state.candles.length - 1) {
    const settled = closeArenaPosition(state, null, 'finish');
    return { ...settled, finished: true };
  }

  let next = { ...state, cursor: state.cursor + 1 };
  const bar = next.candles[next.cursor];

  // 检查本根 K 线盘中高低点是否触发强平
  if (next.position) {
    const liq = calcLiquidationPrice(next.position);
    const hitLiq =
      liq > 0 &&
      (next.position.side === 'long' ? bar.low <= liq : bar.high >= liq);
    if (hitLiq) {
      next = closeArenaPosition(next, liq, 'liquidation');
      if (next.balance <= 1) {
        return { ...next, finished: true };
      }
    }
  }

  const metrics = calcArenaMetrics(next);
  const peakEquity = Math.max(next.peakEquity, metrics.equity);
  const dd =
    peakEquity > 0 ? ((peakEquity - metrics.equity) / peakEquity) * 100 : 0;
  next = {
    ...next,
    peakEquity,
    maxDrawdown: Math.max(next.maxDrawdown, dd),
  };

  if (next.cursor >= next.candles.length - 1) {
    const settled = closeArenaPosition(next, bar.close, 'finish');
    return { ...settled, finished: true };
  }
  return next;
}

export function finishArenaEarly(state) {
  if (state.finished) return state;
  const settled = closeArenaPosition(state, null, 'finish');
  return { ...settled, finished: true };
}

export function gradeArenaResult(state) {
  const metrics = calcArenaMetrics(state);
  const endPrice = state.candles[state.cursor]?.close || state.startPrice;
  const benchmarkPct =
    state.startPrice > 0
      ? ((endPrice - state.startPrice) / state.startPrice) * 100
      : 0;
  const returnPct = metrics.totalReturnPct;
  const alphaPct = returnPct - benchmarkPct;
  const wins = state.trades.filter((tr) => tr.pnl > 0).length;
  const winRate =
    state.trades.length > 0 ? (wins / state.trades.length) * 100 : 0;

  let grade = 'C';
  if (returnPct >= 35 && state.maxDrawdown < 35) grade = 'S+';
  else if (returnPct >= 18) grade = 'S';
  else if (returnPct >= 6) grade = 'A';
  else if (returnPct >= 0) grade = 'B';
  else if (returnPct >= -15) grade = 'C';
  else grade = 'D';

  return {
    grade,
    returnPct,
    benchmarkPct,
    alphaPct,
    winRate,
    tradeCount: state.trades.length,
    maxDrawdown: state.maxDrawdown,
    finalEquity: metrics.equity,
  };
}

export function readArenaCareerStats() {
  try {
    const raw = window.localStorage.getItem(ARENA_STATS_STORAGE_KEY);
    const parsed = raw ? JSON.parse(raw) : null;
    if (!parsed || typeof parsed !== 'object') {
      return { rounds: 0, wins: 0, bestReturn: 0, history: [] };
    }
    return {
      rounds: Number(parsed.rounds || 0),
      wins: Number(parsed.wins || 0),
      bestReturn: Number(parsed.bestReturn || 0),
      history: Array.isArray(parsed.history) ? parsed.history : [],
    };
  } catch {
    return { rounds: 0, wins: 0, bestReturn: 0, history: [] };
  }
}

export function recordArenaRound(roundSummary) {
  const prev = readArenaCareerStats();
  const next = {
    rounds: prev.rounds + 1,
    wins: prev.wins + (roundSummary.returnPct > 0 ? 1 : 0),
    bestReturn:
      prev.rounds === 0
        ? roundSummary.returnPct
        : Math.max(prev.bestReturn, roundSummary.returnPct),
    history: [roundSummary, ...prev.history].slice(0, 10),
  };
  try {
    window.localStorage.setItem(ARENA_STATS_STORAGE_KEY, JSON.stringify(next));
  } catch {
    // 忽略本地存储异常
  }
  return next;
}
