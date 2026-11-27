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

import { describe, expect, test } from 'bun:test';
import {
  TRADE_SECTOR_MAP,
  TRADE_SYMBOL_META,
  formatSymbolLabel,
  getSymbolName,
  matchesSymbolQuery,
  resolveSymbolMeta,
} from './symbols';

const BACKEND_TICKERS = [
  'BTC',
  'ETH',
  'SOL',
  'BNB',
  'XRP',
  'DOGE',
  'ZEC',
  'HYPE',
  'NVDA',
  'TSLA',
  'AMD',
  'MU',
  'SNDK',
  'CRCL',
  'MSTR',
  'SPCX',
  'QQQ',
  'SOXL',
  'SKHYNIX',
  'KORU',
  'ANT',
  'OPENAI',
  'ZHIPU',
  'MINIMAX',
  'MOONSHOT',
  'NBIS',
  'CRWV',
  'CBRS',
  'UNITREE',
  'ZHONGJI',
  'CXMT',
  'GOOGL',
  'MSFT',
  'META',
  'AMZN',
  'AAPL',
  'PLTR',
  'ORCL',
  'BABA',
  'TENCENT',
  'AVGO',
  'TSM',
  'ARM',
  'INTC',
  'ASML',
  'XAU',
  'CL',
];

describe('trade symbol metadata', () => {
  test('covers all 47 backend symbols with valid sectors and names', () => {
    for (const ticker of BACKEND_TICKERS) {
      const meta = TRADE_SYMBOL_META[ticker];
      expect(meta).toBeDefined();
      expect(meta.name.length).toBeGreaterThan(0);
      expect(meta.fullName.length).toBeGreaterThan(0);
      expect(TRADE_SECTOR_MAP.has(meta.sector)).toBe(true);
      expect(resolveSymbolMeta(`${ticker}USDT`)?.ticker).toBe(ticker);
      expect(resolveSymbolMeta(`${ticker}BUSDT`)?.ticker).toBe(ticker);
    }
    expect(Object.keys(TRADE_SYMBOL_META).length).toBe(BACKEND_TICKERS.length);
  });

  test('resolves intuitive Chinese and full names cleanly', () => {
    const identity = (key) => key;
    expect(getSymbolName('NVDABUSDT', identity)).toBe('英伟达');
    expect(getSymbolName('ANTUSDT', identity)).toBe('Anthropic');
    expect(getSymbolName('OPENAIUSDT', identity)).toBe('OpenAI');
    expect(getSymbolName('GOOGLBUSDT', identity)).toBe('谷歌');
    expect(formatSymbolLabel('NVDABUSDT', identity)).toBe('英伟达 (NVDA)');
    expect(formatSymbolLabel('OPENAIUSDT', identity)).toBe('OpenAI');
    expect(formatSymbolLabel('METABUSDT', identity)).toBe('Meta');
  });

  test('matches Chinese names, English full names, aliases, and tickers', () => {
    const identity = (key) => key;
    const ant = { symbol: 'ANTBUSDT', ticker: 'ANT' };
    const nvda = { symbol: 'NVDABUSDT', ticker: 'NVDA' };
    const moonshot = { symbol: 'MOONSHOTUSDT', ticker: 'MOONSHOT' };
    const googl = { symbol: 'GOOGLBUSDT', ticker: 'GOOGL' };

    expect(matchesSymbolQuery(ant, 'anthropic', identity)).toBe(true);
    expect(matchesSymbolQuery(ant, '克劳德', identity)).toBe(true);
    expect(matchesSymbolQuery(ant, 'claude', identity)).toBe(true);
    expect(matchesSymbolQuery(nvda, '英伟达', identity)).toBe(true);
    expect(matchesSymbolQuery(nvda, 'nvidia', identity)).toBe(true);
    expect(matchesSymbolQuery(moonshot, 'kimi', identity)).toBe(true);
    expect(matchesSymbolQuery(moonshot, '月之暗面', identity)).toBe(true);
    expect(matchesSymbolQuery(googl, '谷歌', identity)).toBe(true);
    expect(matchesSymbolQuery(googl, 'google', identity)).toBe(true);
    expect(matchesSymbolQuery(googl, '英伟达', identity)).toBe(false);
  });
});
