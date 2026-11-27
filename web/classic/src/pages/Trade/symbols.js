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

// 模拟盘全部 47 个品种的直观名称、板块分类、品牌色与搜索别名，与后端 setting/operation_setting/trade_setting.go 保持同步。

export const TRADE_SECTORS = [
  { key: 'llm', label: '大模型', color: 'violet' },
  { key: 'ai_infra', label: 'AI 算力', color: 'cyan' },
  { key: 'semi', label: '半导体', color: 'indigo' },
  { key: 'tech', label: '科技巨头', color: 'blue' },
  { key: 'crypto', label: '加密货币', color: 'amber' },
  { key: 'commodity', label: '大宗商品', color: 'orange' },
];

export const TRADE_SECTOR_MAP = new Map(
  TRADE_SECTORS.map((sector) => [sector.key, sector]),
);

export const TRADE_SYMBOL_META = {
  // 加密货币
  BTC: {
    ticker: 'BTC',
    name: '比特币',
    fullName: 'Bitcoin',
    sector: 'crypto',
    accent: '#f7931a',
    badge: '₿',
    aliases: ['大饼', 'Bitcoin', 'BTC'],
  },
  ETH: {
    ticker: 'ETH',
    name: '以太坊',
    fullName: 'Ethereum',
    sector: 'crypto',
    accent: '#627eea',
    badge: 'Ξ',
    aliases: ['二饼', '以太币', 'Ethereum', 'ETH'],
  },
  SOL: {
    ticker: 'SOL',
    name: '索拉纳',
    fullName: 'Solana',
    sector: 'crypto',
    accent: '#9945ff',
    badge: 'SOL',
    aliases: ['Solana', 'SOL'],
  },
  BNB: {
    ticker: 'BNB',
    name: '币安币',
    fullName: 'BNB',
    sector: 'crypto',
    accent: '#f0b90b',
    badge: 'BNB',
    aliases: ['Binance Coin', '币安', 'BNB'],
  },
  XRP: {
    ticker: 'XRP',
    name: '瑞波币',
    fullName: 'XRP',
    sector: 'crypto',
    accent: '#3b82f6',
    badge: 'XRP',
    aliases: ['Ripple', '瑞波', 'XRP'],
  },
  DOGE: {
    ticker: 'DOGE',
    name: '狗狗币',
    fullName: 'Dogecoin',
    sector: 'crypto',
    accent: '#c2a633',
    badge: 'Ð',
    aliases: ['Dogecoin', '马斯克', 'DOGE'],
  },
  ZEC: {
    ticker: 'ZEC',
    name: '大零币',
    fullName: 'Zcash',
    sector: 'crypto',
    accent: '#eab308',
    badge: 'ZEC',
    aliases: ['Zcash', '隐私币', 'ZEC'],
  },
  HYPE: {
    ticker: 'HYPE',
    name: 'Hyperliquid',
    fullName: 'Hyperliquid',
    sector: 'crypto',
    accent: '#14b8a6',
    badge: 'HYP',
    aliases: ['Hyperliquid', 'HYPE'],
  },

  // 大模型 / AI 实验室
  ANT: {
    ticker: 'ANT',
    name: 'Anthropic',
    fullName: 'Anthropic',
    sector: 'llm',
    accent: '#d97757',
    badge: 'AN',
    aliases: ['Claude', '克劳德', 'Anthropic', 'ANT'],
  },
  OPENAI: {
    ticker: 'OPENAI',
    name: 'OpenAI',
    fullName: 'OpenAI',
    sector: 'llm',
    accent: '#10a37f',
    badge: 'AI',
    aliases: ['ChatGPT', 'GPT', '奥特曼', 'OpenAI'],
  },
  ZHIPU: {
    ticker: 'ZHIPU',
    name: '智谱 AI',
    fullName: 'Zhipu AI',
    sector: 'llm',
    accent: '#2563eb',
    badge: '智',
    aliases: ['智谱', '智谱清言', 'GLM', 'Zhipu'],
  },
  MINIMAX: {
    ticker: 'MINIMAX',
    name: 'MiniMax',
    fullName: 'MiniMax',
    sector: 'llm',
    accent: '#ec4899',
    badge: 'MM',
    aliases: ['稀宇科技', '海螺', 'MiniMax'],
  },
  MOONSHOT: {
    ticker: 'MOONSHOT',
    name: '月之暗面',
    fullName: 'Moonshot AI',
    sector: 'llm',
    accent: '#6366f1',
    badge: '月',
    aliases: ['Kimi', 'Moonshot', '月之暗面'],
  },

  // AI 算力云 / 专用芯片 / 具身智能
  NBIS: {
    ticker: 'NBIS',
    name: 'Nebius 云算力',
    fullName: 'Nebius Group',
    sector: 'ai_infra',
    accent: '#0ea5e9',
    badge: 'NB',
    aliases: ['Nebius', 'AI云', 'NBIS'],
  },
  CRWV: {
    ticker: 'CRWV',
    name: 'CoreWeave 算力',
    fullName: 'CoreWeave',
    sector: 'ai_infra',
    accent: '#06b6d4',
    badge: 'CW',
    aliases: ['CoreWeave', 'GPU云', 'CRWV'],
  },
  CBRS: {
    ticker: 'CBRS',
    name: 'Cerebras 芯片',
    fullName: 'Cerebras Systems',
    sector: 'ai_infra',
    accent: '#f97316',
    badge: 'CB',
    aliases: ['Cerebras', '晶圆级芯片', 'CBRS'],
  },
  UNITREE: {
    ticker: 'UNITREE',
    name: '宇树科技',
    fullName: 'Unitree Robotics',
    sector: 'ai_infra',
    accent: '#0891b2',
    badge: '宇',
    aliases: ['宇树', '机器人', '具身智能', 'Unitree'],
  },
  ZHONGJI: {
    ticker: 'ZHONGJI',
    name: '中际旭创',
    fullName: 'Zhongji Innolight',
    sector: 'ai_infra',
    accent: '#0284c7',
    badge: '旭',
    aliases: ['中际旭创', '光模块', 'Innolight'],
  },
  CXMT: {
    ticker: 'CXMT',
    name: '长鑫存储',
    fullName: 'ChangXin Memory',
    sector: 'ai_infra',
    accent: '#2563eb',
    badge: '鑫',
    aliases: ['长鑫', '存储芯片', 'DRAM', 'CXMT'],
  },

  // 半导体芯片
  NVDA: {
    ticker: 'NVDA',
    name: '英伟达',
    fullName: 'NVIDIA',
    sector: 'semi',
    accent: '#76b900',
    badge: 'NV',
    aliases: ['黄仁勋', '老黄', 'NVIDIA', 'Nvidia', '英伟达', 'NVDA'],
  },
  AMD: {
    ticker: 'AMD',
    name: '超威半导体',
    fullName: 'AMD',
    sector: 'semi',
    accent: '#ef4444',
    badge: 'AMD',
    aliases: ['苏姿丰', '苏妈', 'Advanced Micro Devices', 'AMD'],
  },
  TSM: {
    ticker: 'TSM',
    name: '台积电',
    fullName: 'TSMC',
    sector: 'semi',
    accent: '#dc2626',
    badge: '台',
    aliases: ['TSMC', '台湾积体电路', '台积电', 'TSM'],
  },
  AVGO: {
    ticker: 'AVGO',
    name: '博通',
    fullName: 'Broadcom',
    sector: 'semi',
    accent: '#cc092f',
    badge: 'AV',
    aliases: ['Broadcom', '博通', 'AVGO'],
  },
  ASML: {
    ticker: 'ASML',
    name: '阿斯麦',
    fullName: 'ASML Holding',
    sector: 'semi',
    accent: '#0f4c81',
    badge: 'AS',
    aliases: ['光刻机', 'ASML', '阿斯麦'],
  },
  ARM: {
    ticker: 'ARM',
    name: 'Arm 架构',
    fullName: 'Arm Holdings',
    sector: 'semi',
    accent: '#0091bd',
    badge: 'ARM',
    aliases: ['Arm Holdings', '安谋', 'ARM'],
  },
  INTC: {
    ticker: 'INTC',
    name: '英特尔',
    fullName: 'Intel',
    sector: 'semi',
    accent: '#0071c5',
    badge: 'IN',
    aliases: ['Intel', '英特尔', '牙膏厂', 'INTC'],
  },
  MU: {
    ticker: 'MU',
    name: '美光科技',
    fullName: 'Micron Technology',
    sector: 'semi',
    accent: '#2563eb',
    badge: 'MU',
    aliases: ['Micron', '美光', 'HBM', 'MU'],
  },
  SNDK: {
    ticker: 'SNDK',
    name: '闪迪',
    fullName: 'SanDisk',
    sector: 'semi',
    accent: '#e11d48',
    badge: 'SD',
    aliases: ['SanDisk', '闪迪', '存储', 'SNDK'],
  },
  SKHYNIX: {
    ticker: 'SKHYNIX',
    name: 'SK 海力士',
    fullName: 'SK Hynix',
    sector: 'semi',
    accent: '#ea580c',
    badge: 'SK',
    aliases: ['海力士', 'SK Hynix', 'HBM', '韩股'],
  },
  SOXL: {
    ticker: 'SOXL',
    name: '3倍做多半导体',
    fullName: 'Direxion Semiconductor Bull 3X',
    sector: 'semi',
    accent: '#7c3aed',
    badge: '3X',
    aliases: ['半导体ETF', 'ETF', 'SOXL'],
  },

  // 科技巨头 & 热门股票 / ETF
  GOOGL: {
    ticker: 'GOOGL',
    name: '谷歌',
    fullName: 'Alphabet (Google)',
    sector: 'tech',
    accent: '#4285f4',
    badge: 'G',
    aliases: ['Google', 'Alphabet', 'Gemini', '谷歌', 'GOOGL'],
  },
  MSFT: {
    ticker: 'MSFT',
    name: '微软',
    fullName: 'Microsoft',
    sector: 'tech',
    accent: '#00a4ef',
    badge: 'MS',
    aliases: ['Microsoft', 'Copilot', '微软', 'MSFT'],
  },
  META: {
    ticker: 'META',
    name: 'Meta',
    fullName: 'Meta Platforms',
    sector: 'tech',
    accent: '#0668e1',
    badge: 'M',
    aliases: ['Facebook', '脸书', '扎克伯格', 'Llama', 'Meta'],
  },
  AMZN: {
    ticker: 'AMZN',
    name: '亚马逊',
    fullName: 'Amazon',
    sector: 'tech',
    accent: '#ff9900',
    badge: 'AZ',
    aliases: ['Amazon', 'AWS', '亚马逊', 'AMZN'],
  },
  AAPL: {
    ticker: 'AAPL',
    name: '苹果',
    fullName: 'Apple',
    sector: 'tech',
    accent: '#64748b',
    badge: 'AP',
    aliases: ['Apple', 'iPhone', '苹果', 'AAPL'],
  },
  TSLA: {
    ticker: 'TSLA',
    name: '特斯拉',
    fullName: 'Tesla',
    sector: 'tech',
    accent: '#e31937',
    badge: 'TS',
    aliases: ['Tesla', '马斯克', '特斯拉', 'TSLA'],
  },
  PLTR: {
    ticker: 'PLTR',
    name: 'Palantir',
    fullName: 'Palantir Technologies',
    sector: 'tech',
    accent: '#475569',
    badge: 'PL',
    aliases: ['帕兰提尔', 'Palantir', 'PLTR'],
  },
  ORCL: {
    ticker: 'ORCL',
    name: '甲骨文',
    fullName: 'Oracle',
    sector: 'tech',
    accent: '#c74634',
    badge: 'OR',
    aliases: ['Oracle', '甲骨文', 'ORCL'],
  },
  BABA: {
    ticker: 'BABA',
    name: '阿里巴巴',
    fullName: 'Alibaba Group',
    sector: 'tech',
    accent: '#ff6a00',
    badge: '阿',
    aliases: ['Alibaba', '阿里', '通义千问', 'Qwen', 'BABA'],
  },
  TENCENT: {
    ticker: 'TENCENT',
    name: '腾讯',
    fullName: 'Tencent Holdings',
    sector: 'tech',
    accent: '#07c160',
    badge: '腾',
    aliases: ['Tencent', '腾讯控股', '微信', '混元'],
  },
  CRCL: {
    ticker: 'CRCL',
    name: 'Circle',
    fullName: 'Circle Internet Group',
    sector: 'tech',
    accent: '#0073e6',
    badge: 'CR',
    aliases: ['USDC', 'Circle', '稳定币', 'CRCL'],
  },
  MSTR: {
    ticker: 'MSTR',
    name: '微策略',
    fullName: 'Strategy (MicroStrategy)',
    sector: 'tech',
    accent: '#d9232e',
    badge: 'ST',
    aliases: ['Strategy', 'MicroStrategy', '微策略', 'MSTR'],
  },
  SPCX: {
    ticker: 'SPCX',
    name: 'SpaceX',
    fullName: 'SpaceX',
    sector: 'tech',
    accent: '#334155',
    badge: 'SX',
    aliases: ['太空探索', '星舰', 'Starlink', '马斯克', 'SpaceX', 'SPCX'],
  },
  QQQ: {
    ticker: 'QQQ',
    name: '纳指 100 ETF',
    fullName: 'Invesco QQQ Trust',
    sector: 'tech',
    accent: '#1d4ed8',
    badge: 'Q',
    aliases: ['纳斯达克', 'Nasdaq', 'ETF', 'QQQ'],
  },
  KORU: {
    ticker: 'KORU',
    name: '3倍做多韩国 ETF',
    fullName: 'Direxion MSCI South Korea Bull 3X',
    sector: 'tech',
    accent: '#0284c7',
    badge: 'KR',
    aliases: ['韩国ETF', 'ETF', 'KORU'],
  },

  // 大宗商品
  XAU: {
    ticker: 'XAU',
    name: '现货黄金',
    fullName: 'Gold',
    sector: 'commodity',
    accent: '#d97706',
    badge: 'Au',
    aliases: ['黄金', '金价', '贵金属', 'Gold', 'XAU'],
  },
  CL: {
    ticker: 'CL',
    name: 'WTI 原油',
    fullName: 'WTI Crude Oil',
    sector: 'commodity',
    accent: '#4b5563',
    badge: 'OIL',
    aliases: ['原油', '石油', 'Crude Oil', 'WTI', 'CL'],
  },
};

// 预建 O(1) 索引：支持用代码(NVDA)、现货(NVDABUSDT)或合约(NVDAUSDT)直接查到元数据。
const SYMBOL_LOOKUP = new Map();
for (const meta of Object.values(TRADE_SYMBOL_META)) {
  SYMBOL_LOOKUP.set(meta.ticker, meta);
  SYMBOL_LOOKUP.set(`${meta.ticker}USDT`, meta);
  SYMBOL_LOOKUP.set(`${meta.ticker}BUSDT`, meta);
}

export function extractTicker(input) {
  if (!input) return '';
  if (typeof input === 'object') {
    if (input.ticker) return String(input.ticker).toUpperCase();
    if (input.symbol) return extractTicker(input.symbol);
    return '';
  }
  const upper = String(input).trim().toUpperCase();
  const direct = SYMBOL_LOOKUP.get(upper);
  if (direct) return direct.ticker;
  if (upper.endsWith('BUSDT')) return upper.slice(0, -5);
  if (upper.endsWith('USDT')) return upper.slice(0, -4);
  return upper;
}

export function resolveSymbolMeta(input) {
  if (!input) return null;
  if (typeof input === 'object') {
    const byTicker = input.ticker
      ? SYMBOL_LOOKUP.get(String(input.ticker).toUpperCase())
      : null;
    if (byTicker) return byTicker;
    if (input.symbol) {
      return SYMBOL_LOOKUP.get(String(input.symbol).toUpperCase()) || null;
    }
    return null;
  }
  const upper = String(input).trim().toUpperCase();
  return SYMBOL_LOOKUP.get(upper) || null;
}

export function getSymbolName(input, t) {
  const meta = resolveSymbolMeta(input);
  if (!meta) return extractTicker(input) || String(input || '');
  return t ? t(meta.name) : meta.name;
}

export function getSymbolSector(input) {
  const meta = resolveSymbolMeta(input);
  if (!meta) return null;
  return TRADE_SECTOR_MAP.get(meta.sector) || null;
}

// formatSymbolLabel 组合直观名称与代码，例如 "英伟达 (NVDA)"；名称本身与代码一致(如 OpenAI、Meta)时不重复括号。
export function formatSymbolLabel(input, t) {
  const meta = resolveSymbolMeta(input);
  const ticker = meta?.ticker || extractTicker(input);
  if (!meta) return ticker || String(input || '');
  const displayName = t ? t(meta.name) : meta.name;
  if (displayName.toUpperCase() === ticker.toUpperCase()) {
    return displayName;
  }
  return `${displayName} (${ticker})`;
}

// matchesSymbolQuery 支持按中文名、英文全名、常用别名(Claude/ChatGPT/Kimi/黄金)、代码与原始交易对搜索。
export function matchesSymbolQuery(item, rawQuery, t) {
  const keyword = String(rawQuery || '')
    .trim()
    .toLowerCase();
  if (!keyword) return true;
  const meta = resolveSymbolMeta(item);
  const candidates = [
    typeof item === 'string' ? item : item?.symbol,
    typeof item === 'object' ? item?.ticker : '',
  ];
  if (meta) {
    candidates.push(meta.ticker, meta.name, meta.fullName);
    if (t) candidates.push(t(meta.name));
    const sector = TRADE_SECTOR_MAP.get(meta.sector);
    if (sector) {
      candidates.push(sector.label);
      if (t) candidates.push(t(sector.label));
    }
    if (meta.aliases) candidates.push(...meta.aliases);
  }
  return candidates.some(
    (field) => field && String(field).toLowerCase().includes(keyword),
  );
}

// 自选收藏读写：现货与合约分别存储，供行情列表、交易页顶部和快捷切换器共享。
const FAV_STORAGE_KEY = 'trade-favorites:v1';

export function favoritesStorageKey(futures = false) {
  return `${FAV_STORAGE_KEY}:${futures ? 'futures' : 'spot'}`;
}

export function readTradeFavorites(futures = false) {
  try {
    const raw = window.localStorage.getItem(favoritesStorageKey(futures));
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function writeTradeFavorites(futures = false, list = []) {
  try {
    window.localStorage.setItem(
      favoritesStorageKey(futures),
      JSON.stringify(list),
    );
  } catch {
    // 浏览器禁用本地存储时静默降级为会话内状态
  }
}
