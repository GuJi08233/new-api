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

function decimalsFromStep(step) {
  if (!step) return 2;
  const text = String(step);
  const dot = text.indexOf('.');
  if (dot < 0) return 0;
  return Math.max(text.replace(/0+$/, '').length - dot - 1, 0);
}

function floorStep(value, step) {
  const num = Number(value);
  const size = Number(step);
  if (!Number.isFinite(num) || num <= 0) return '';
  if (!Number.isFinite(size) || size <= 0) return String(num);
  const digits = decimalsFromStep(step);
  const units = Math.floor(num / size + 1e-9);
  if (units <= 0) return '';
  return (units * size).toFixed(digits);
}

function roundStep(value, step) {
  const num = Number(value);
  const size = Number(step);
  if (!Number.isFinite(num) || num <= 0) return '';
  if (!Number.isFinite(size) || size <= 0) return String(num);
  const digits = decimalsFromStep(step);
  const units = Math.round(num / size);
  if (units <= 0) return '';
  return (units * size).toFixed(digits);
}

// 扫描所有同时开放现货与永续合约的品种，按正资金费率年化收益从高到低排序。
export function rankFundingArbOpportunities(
  spotSymbols = [],
  futuresSymbols = [],
  spotQuotes = {},
  futuresQuotes = {},
) {
  const spotByTicker = new Map(
    spotSymbols
      .filter((s) => s.open !== false)
      .map((item) => [item.ticker, item]),
  );
  const items = [];
  for (const fut of futuresSymbols) {
    if (fut.open === false) continue;
    const spot = spotByTicker.get(fut.ticker);
    if (!spot) continue;
    const sq = spotQuotes[spot.symbol] || spot.quote || {};
    const fq = futuresQuotes[fut.symbol] || fut.quote || {};
    const spotPrice = Number(sq.ask || sq.price || 0);
    const futuresPrice = Number(fq.bid || fq.mark || fq.price || 0);
    const fundingRate = Number(fq.funding_rate);
    const hasFunding = Number.isFinite(fundingRate);
    // Binance 默认每 8 小时结算一次(一天 3 次)，按 3*365 计算年化参考 APR。
    const aprPct = hasFunding ? fundingRate * 3 * 365 * 100 : 0;
    const dailyPer1000 = hasFunding ? fundingRate * 3 * 1000 : 0;
    const basisPct =
      spotPrice > 0 && futuresPrice > 0
        ? ((futuresPrice - spotPrice) / spotPrice) * 100
        : 0;
    items.push({
      ticker: fut.ticker,
      spot,
      futures: fut,
      spotPrice,
      futuresPrice,
      fundingRate: hasFunding ? fundingRate : null,
      nextFundingTime: Number(fq.next_funding_time || 0),
      aprPct,
      dailyPer1000,
      basisPct,
    });
  }
  return items.sort(
    (a, b) => (b.fundingRate ?? -999) - (a.fundingRate ?? -999),
  );
}

// 计算 Delta 中性资金费率套利建仓计划：
// 现货买入名义额 = 合约做空名义额 = N；所需资金 = N (现货) + N / lev (合约逐仓保证金) = totalUsdt
// => N = totalUsdt * lev / (lev + 1)。
export function buildFundingArbPlan({
  opportunity,
  totalUsdt,
  leverage = 1,
  spotFeeBps = 10,
  futuresTakerFeeBps = 4,
}) {
  const total = Number(totalUsdt) || 0;
  const lev = Math.max(1, Math.min(3, Math.round(Number(leverage) || 1)));
  if (!opportunity || !(total > 0)) {
    return { valid: false, reason: 'invalid_input' };
  }
  const { spot, futures, spotPrice, futuresPrice, fundingRate } = opportunity;
  if (!(spotPrice > 0) || !(futuresPrice > 0)) {
    return { valid: false, reason: 'no_price' };
  }
  const feeReserve = 1 + (spotFeeBps + futuresTakerFeeBps) / 10000;
  const targetNotional = (total / feeReserve) * (lev / (lev + 1));
  const commonStep =
    Number(futures.rules?.step_size || 0) >= Number(spot.rules?.step_size || 0)
      ? futures.rules?.step_size
      : spot.rules?.step_size;
  const qtyText = floorStep(targetNotional / futuresPrice, commonStep);
  const qty = Number(qtyText) || 0;
  const spotNotional = qty * spotPrice;
  const futuresNotional = qty * futuresPrice;
  const futuresMargin = futuresNotional / lev;
  const estFees =
    spotNotional * (spotFeeBps / 10000) +
    futuresNotional * (futuresTakerFeeBps / 10000);
  const minSpot = Number(spot.rules?.min_notional || 0);
  const minFut = Number(futures.rules?.min_notional || 0);
  const minFutQty = Number(futures.rules?.min_qty || 0);
  const minSpotQty = Number(spot.rules?.min_qty || 0);

  let error = '';
  if (!(qty > 0) || qty < minFutQty || qty < minSpotQty) {
    error = 'below_min_qty';
  } else if (spotNotional < minSpot || futuresNotional < minFut) {
    error = 'below_min_notional';
  }

  const dailyIncome =
    fundingRate !== null ? futuresNotional * fundingRate * 3 : 0;
  const breakevenDays = dailyIncome > 0 ? (estFees * 2) / dailyIncome : null;

  return {
    valid: !error,
    error,
    qty: qtyText,
    leverage: lev,
    spotNotional,
    futuresNotional,
    futuresMargin,
    estFees,
    dailyIncome,
    breakevenDays,
    spotPayload: {
      symbol: spot.symbol,
      side: 'buy',
      type: 'market',
      qty: qtyText,
      leverage: 1,
      borrow: false,
    },
    futuresPayload: {
      symbol: futures.symbol,
      side: 'short',
      action: 'open',
      type: 'market',
      margin_mode: 'isolated',
      leverage: lev,
      qty: qtyText,
      take_profits: [],
      stop_losses: [],
    },
  };
}

// 生成现货/合约网格挂单计划：将价格区间等差或等比切分为 count 格，在当前价下方布限价买单(现货)或双向限价单(合约)。
export function buildGridOrderPlan({
  market = 'spot',
  symbolItem,
  currentPrice,
  lowerPrice,
  upperPrice,
  gridCount = 6,
  mode = 'arithmetic',
  totalUsdt = 100,
  leverage = 3,
  feeBps = 10,
}) {
  const lower = Number(lowerPrice) || 0;
  const upper = Number(upperPrice) || 0;
  const current = Number(currentPrice) || 0;
  const count = Math.max(3, Math.min(16, Math.round(Number(gridCount) || 6)));
  const total = Number(totalUsdt) || 0;
  const lev =
    market === 'futures'
      ? Math.max(1, Math.min(20, Math.round(Number(leverage) || 1)))
      : 1;

  if (
    !symbolItem ||
    !(lower > 0) ||
    !(upper > lower) ||
    !(current > 0) ||
    !(total > 0)
  ) {
    return {
      valid: false,
      error: 'invalid_params',
      orders: [],
      profitPerGridPct: 0,
    };
  }
  const tick = symbolItem.rules?.tick_size;
  const step = symbolItem.rules?.step_size;
  const minNotional = Number(symbolItem.rules?.min_notional || 0);
  const minQty = Number(symbolItem.rules?.min_qty || 0);

  // 生成网格价格线 (count + 1 条线形成 count 个格子)
  const prices = [];
  for (let i = 0; i <= count; i++) {
    const raw =
      mode === 'geometric'
        ? lower * Math.pow(upper / lower, i / count)
        : lower + ((upper - lower) * i) / count;
    const rounded = roundStep(raw, tick);
    if (rounded && !prices.includes(rounded)) {
      prices.push(rounded);
    }
  }
  if (prices.length < 3) {
    return {
      valid: false,
      error: 'grid_too_dense',
      orders: [],
      profitPerGridPct: 0,
    };
  }

  // 现货模式仅在当前价下方挂限价买单(低吸)；合约中性网格在当前价下方挂开多限价单、上方挂开空限价单。
  const rawLevels = [];
  for (const priceStr of prices) {
    const p = Number(priceStr);
    // 跳过紧贴市价 0.05% 内的价位，避免立刻吃单
    if (Math.abs(p - current) / current < 0.0005) continue;
    // 后端限价单带 50%~150% 价格保护带校验
    if (p < current * 0.52 || p > current * 1.48) continue;
    if (market === 'spot') {
      if (p < current)
        rawLevels.push({ side: 'buy', price: priceStr, numPrice: p });
    } else {
      rawLevels.push({
        side: p < current ? 'long' : 'short',
        price: priceStr,
        numPrice: p,
      });
    }
  }

  if (!rawLevels.length) {
    return {
      valid: false,
      error: 'no_levels_in_band',
      orders: [],
      profitPerGridPct: 0,
    };
  }

  const perOrderBudget = total / rawLevels.length;
  const feeRate = feeBps / 10000;
  let allValid = true;
  const orders = rawLevels.map((lvl) => {
    const notionalBudget =
      market === 'spot'
        ? perOrderBudget / (1 + feeRate)
        : (perOrderBudget * lev) / (1 + feeRate * lev);
    const qtyText = floorStep(notionalBudget / lvl.numPrice, step);
    const qty = Number(qtyText) || 0;
    const notional = qty * lvl.numPrice;
    let error = '';
    if (!(qty > 0) || qty < minQty) error = 'below_min_qty';
    else if (notional < minNotional) error = 'below_min_notional';
    if (error) allValid = false;

    const payload =
      market === 'spot'
        ? {
            symbol: symbolItem.symbol,
            side: 'buy',
            type: 'limit',
            price: lvl.price,
            qty: qtyText,
            leverage: 1,
            borrow: false,
          }
        : {
            symbol: symbolItem.symbol,
            side: lvl.side,
            action: 'open',
            type: 'limit',
            margin_mode: 'isolated',
            leverage: lev,
            price: lvl.price,
            qty: qtyText,
            take_profits: [],
            stop_losses: [],
          };

    return {
      ...lvl,
      qty: qtyText,
      notional,
      error,
      endpoint:
        market === 'spot' ? '/api/trade/orders' : '/api/trade/futures/orders',
      payload,
    };
  });

  const avgSpacingPct =
    mode === 'geometric'
      ? (Math.pow(upper / lower, 1 / count) - 1) * 100
      : ((upper - lower) / count / ((upper + lower) / 2)) * 100;
  const netProfitPerGridPct = avgSpacingPct - feeRate * 2 * 100;

  return {
    valid: allValid,
    error: allValid ? '' : 'some_orders_invalid',
    orders,
    avgSpacingPct,
    profitPerGridPct: netProfitPerGridPct,
    feeWarning: netProfitPerGridPct <= 0,
  };
}
