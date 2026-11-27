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

function calcChangePercent(price, open) {
  const p = Number(price);
  const o = Number(open);
  if (!Number.isFinite(p) || !Number.isFinite(o) || o <= 0) return null;
  return ((p - o) / o) * 100;
}

function floorQtyToStep(value, step) {
  const number = Number(value);
  const size = Number(step);
  if (!Number.isFinite(number) || number <= 0) return '';
  if (!Number.isFinite(size) || size <= 0) return String(number);
  const text = String(step);
  const dot = text.indexOf('.');
  const digits =
    dot < 0 ? 0 : Math.max(text.replace(/0+$/, '').length - dot - 1, 0);
  const units = Math.floor(number / size + 1e-9);
  if (units <= 0) return '';
  return (units * size).toFixed(digits);
}

export const PRESET_BASKETS = [
  {
    id: 'ai_llm_giants',
    name: '大模型前沿五巨头',
    description: '一键配置全球头部大模型实验室，把握 AI 范式跃迁红利',
    tag: '大模型',
    color: 'violet',
    legs: [
      { ticker: 'ANT', weight: 25 },
      { ticker: 'OPENAI', weight: 25 },
      { ticker: 'ZHIPU', weight: 18 },
      { ticker: 'MINIMAX', weight: 16 },
      { ticker: 'MOONSHOT', weight: 16 },
    ],
  },
  {
    id: 'ai_compute_core',
    name: 'AI 算力芯片核心',
    description: '涵盖 GPU 设计、先进制程代工、光刻机与高带宽算力龙头',
    tag: '半导体',
    color: 'indigo',
    legs: [
      { ticker: 'NVDA', weight: 30 },
      { ticker: 'TSM', weight: 20 },
      { ticker: 'AVGO', weight: 20 },
      { ticker: 'ASML', weight: 15 },
      { ticker: 'AMD', weight: 15 },
    ],
  },
  {
    id: 'us_ai_mag7',
    name: '美股 AI 科技七巨头',
    description: '聚焦美股最具护城河的七大科技与 AI 平台巨擘',
    tag: '科技巨头',
    color: 'blue',
    legs: [
      { ticker: 'NVDA', weight: 20 },
      { ticker: 'MSFT', weight: 18 },
      { ticker: 'GOOGL', weight: 16 },
      { ticker: 'AAPL', weight: 14 },
      { ticker: 'AMZN', weight: 12 },
      { ticker: 'META', weight: 10 },
      { ticker: 'TSLA', weight: 10 },
    ],
  },
  {
    id: 'china_ai_tech',
    name: '中国 AI 与硬科技',
    description: '横跨国产大模型、互联网巨头、光模块与人形机器人赛道',
    tag: '中国 AI',
    color: 'cyan',
    legs: [
      { ticker: 'TENCENT', weight: 25 },
      { ticker: 'BABA', weight: 25 },
      { ticker: 'ZHIPU', weight: 18 },
      { ticker: 'ZHONGJI', weight: 17 },
      { ticker: 'UNITREE', weight: 15 },
    ],
  },
  {
    id: 'crypto_bluechip',
    name: '加密蓝筹三剑客',
    description: '比特币稳健压舱，以太坊与索拉纳捕捉生态弹性',
    tag: '加密货币',
    color: 'amber',
    legs: [
      { ticker: 'BTC', weight: 50 },
      { ticker: 'ETH', weight: 30 },
      { ticker: 'SOL', weight: 20 },
    ],
  },
  {
    id: 'macro_hedge',
    name: '宏观避险与抗通胀',
    description: '黄金、比特币与原油跨周期组合，对冲宏观波动',
    tag: '宏观对冲',
    color: 'orange',
    legs: [
      { ticker: 'XAU', weight: 45 },
      { ticker: 'BTC', weight: 35 },
      { ticker: 'CL', weight: 20 },
    ],
  },
];

const CUSTOM_BASKETS_KEY = 'trade-custom-baskets:v1';

export function readCustomBaskets() {
  try {
    const raw = window.localStorage.getItem(CUSTOM_BASKETS_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (item) =>
        item &&
        typeof item.id === 'string' &&
        typeof item.name === 'string' &&
        Array.isArray(item.legs) &&
        item.legs.length >= 2,
    );
  } catch {
    return [];
  }
}

export function writeCustomBaskets(list) {
  try {
    window.localStorage.setItem(CUSTOM_BASKETS_KEY, JSON.stringify(list));
  } catch {
    // 忽略本地存储异常
  }
}

// 规范化权重使总和精确为 100%，避免浮点误差。
export function normalizeWeights(legs) {
  const positive = legs.filter((leg) => Number(leg.weight) > 0);
  const sum = positive.reduce((acc, leg) => acc + Number(leg.weight), 0);
  if (!positive.length || !(sum > 0)) return [];
  const raw = positive.map((leg) => ({
    ...leg,
    weight: Math.round((Number(leg.weight) / sum) * 1000) / 10,
  }));
  const roundedSum = raw.reduce((acc, leg) => acc + leg.weight, 0);
  const diff = Math.round((100 - roundedSum) * 10) / 10;
  if (diff !== 0) {
    raw[0].weight = Math.round((raw[0].weight + diff) * 10) / 10;
  }
  return raw;
}

// 计算主题组合的实时加权涨跌幅、现货/合约可执行性与合约可用最大杠杆。
export function evaluateBasket(
  basket,
  spotByTicker,
  futuresByTicker,
  spotQuotes = {},
  futuresQuotes = {},
) {
  const legs = normalizeWeights(basket.legs || []);
  let weightedSum = 0;
  let weightWithQuote = 0;
  let canSpot = legs.length > 0;
  let canFutures = legs.length > 0;
  let maxFuturesLeverage = Infinity;

  const enriched = legs.map((leg) => {
    const spotItem = spotByTicker.get(leg.ticker) || null;
    const futuresItem = futuresByTicker.get(leg.ticker) || null;
    const spotQuote = spotItem
      ? spotQuotes[spotItem.symbol] || spotItem.quote
      : null;
    const futuresQuote = futuresItem
      ? futuresQuotes[futuresItem.symbol] || futuresItem.quote
      : null;

    if (!spotItem || spotItem.open === false) canSpot = false;
    if (!futuresItem || futuresItem.open === false) {
      canFutures = false;
    } else if (futuresItem.max_leverage) {
      maxFuturesLeverage = Math.min(
        maxFuturesLeverage,
        futuresItem.max_leverage,
      );
    }

    const activeQuote = spotQuote || futuresQuote || null;
    const change = activeQuote
      ? calcChangePercent(activeQuote.price, activeQuote.open)
      : null;
    if (change !== null) {
      weightedSum += change * leg.weight;
      weightWithQuote += leg.weight;
    }

    return {
      ...leg,
      spotItem,
      futuresItem,
      spotQuote,
      futuresQuote,
      price: activeQuote?.price || '',
      change,
    };
  });

  return {
    legs: enriched,
    weightedChange: weightWithQuote > 0 ? weightedSum / weightWithQuote : null,
    canSpot,
    canFutures,
    maxFuturesLeverage: Number.isFinite(maxFuturesLeverage)
      ? Math.max(1, maxFuturesLeverage)
      : 1,
  };
}

// 根据用户输入的总资金与模式生成每一笔可提交的下单计划，并在发单前检查最小名义金额和步长。
export function buildBasketOrderPlan({
  legs,
  mode,
  side = 'long',
  marginMode = 'cross',
  leverage = 5,
  totalUsdt = 0,
}) {
  const total = Number(totalUsdt) || 0;
  if (!(total > 0) || !legs.length) {
    return { items: [], valid: false, reason: 'invalid_total' };
  }

  let allValid = true;
  const items = legs.map((leg) => {
    const allocUsd = Math.floor(((total * leg.weight) / 100) * 100) / 100;
    if (mode === 'spot') {
      const item = leg.spotItem;
      const minNotional = Number(item?.rules?.min_notional || 0);
      const price = Number(leg.spotQuote?.ask || leg.spotQuote?.price || 0);
      let error = '';
      if (!item || item.open === false) {
        error = 'spot_closed';
      } else if (!(price > 0)) {
        error = 'no_price';
      } else if (allocUsd < minNotional) {
        error = 'below_min_notional';
      }
      if (error) allValid = false;
      return {
        ticker: leg.ticker,
        weight: leg.weight,
        symbol: item?.symbol || '',
        allocUsd,
        price,
        minNotional,
        error,
        endpoint: '/api/trade/orders',
        payload: {
          symbol: item?.symbol || '',
          side: 'buy',
          type: 'market',
          amount: allocUsd.toFixed(2),
          leverage: 1,
          borrow: false,
        },
      };
    }

    const item = leg.futuresItem;
    const quote = leg.futuresQuote;
    const price = Number(
      (side === 'long' ? quote?.ask : quote?.bid) || quote?.price || 0,
    );
    const step = item?.rules?.step_size;
    const minQty = Number(item?.rules?.min_qty || 0);
    const minNotional = Number(item?.rules?.min_notional || 0);
    const lev = Math.max(
      1,
      Math.min(Number(leverage) || 1, item?.max_leverage || 1),
    );
    const notionalTarget = allocUsd * lev;
    const qtyText =
      price > 0 ? floorQtyToStep(notionalTarget / price, step) : '';
    const qty = Number(qtyText) || 0;
    const notional = qty * price;
    let error = '';
    if (!item || item.open === false) {
      error = 'futures_closed';
    } else if (!(price > 0)) {
      error = 'no_price';
    } else if (!(qty > 0) || qty < minQty) {
      error = 'below_min_qty';
    } else if (notional < minNotional) {
      error = 'below_min_notional';
    }
    if (error) allValid = false;
    return {
      ticker: leg.ticker,
      weight: leg.weight,
      symbol: item?.symbol || '',
      allocUsd,
      price,
      qty: qtyText,
      notional,
      leverage: lev,
      minNotional,
      error,
      endpoint: '/api/trade/futures/orders',
      payload: {
        symbol: item?.symbol || '',
        side,
        action: 'open',
        type: 'market',
        margin_mode: marginMode,
        leverage: lev,
        qty: qtyText,
        take_profits: [],
        stop_losses: [],
      },
    };
  });

  return { items, valid: allValid };
}
