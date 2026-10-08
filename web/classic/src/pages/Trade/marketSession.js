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

// 标的市场的交易时段，照 Binance 美股代币与 TradFi 合约页的时段图。时段按市场所在时区的墙钟定义：from/to 是相对交易日
// 当天 0 点的分钟数，可以是负的(前一天)，夏令时随时区自动换算。只按星期算，不含节假日。
const H = (hours, minutes = 0) => hours * 60 + minutes;
const WEEKDAYS = [1, 2, 3, 4, 5];

const MARKET_SESSIONS = {
  // 美股：周日 20:00(美东)起夜盘，接盘前、盘中、盘后，到周五 20:00 收。
  us: {
    tz: 'America/New_York',
    segments: [
      { kind: 'overnight', from: H(20) - H(24), to: H(4) },
      { kind: 'pre', from: H(4), to: H(9, 30) },
      { kind: 'open', from: H(9, 30), to: H(16) },
      { kind: 'post', from: H(16), to: H(20) },
    ],
  },
  // 韩国交易所：连续竞价 09:00–15:20，之后是收盘集合竞价，不算盘中。韩国没有夏令时。
  krx: {
    tz: 'Asia/Seoul',
    segments: [{ kind: 'open', from: H(9), to: H(15, 20) }],
  },
  // CME Globex 的黄金与原油期货：周日 17:00(芝加哥)开到周五 16:00，每天 16:00–17:00 休息一小时。
  cme: {
    tz: 'America/Chicago',
    segments: [{ kind: 'open', from: H(17) - H(24), to: H(16) }],
  },
};

// 图例按一天里的先后排。
const SESSION_ORDER = ['pre', 'open', 'post', 'overnight', 'closed'];

const formatters = new Map();

// partsIn 是 ts 时刻在 tz 时区的墙钟。
function partsIn(ts, tz) {
  let format = formatters.get(tz);
  if (!format) {
    format = new Intl.DateTimeFormat('en-US', {
      timeZone: tz,
      hourCycle: 'h23',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
    formatters.set(tz, format);
  }
  const parts = {};
  format.formatToParts(new Date(ts)).forEach(({ type, value }) => {
    parts[type] = Number(value);
  });
  return parts;
}

// wallToUtc 把 tz 时区的墙钟(按 UTC 编码的毫秒数)换成真正的时刻，再算一遍修正夏令时切换的那天。
function wallToUtc(wall, tz) {
  const offset = (ts) => {
    const second = Math.floor(ts / 1000) * 1000;
    const p = partsIn(second, tz);
    return (
      Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second) - second
    );
  };
  const first = wall - offset(wall);
  return wall - offset(first);
}

// sessionTimeline 是 now 前后一周多的时段，按时间排好，交易时段之间的空档补成 closed，首尾相接。
function sessionTimeline(id, now) {
  const market = MARKET_SESSIONS[id];
  if (!market) return [];
  const today = partsIn(now, market.tz);
  const segments = [];
  for (let offset = -8; offset <= 8; offset++) {
    const day = Date.UTC(today.year, today.month - 1, today.day + offset);
    if (!WEEKDAYS.includes(new Date(day).getUTCDay())) continue;
    market.segments.forEach((segment) => {
      segments.push({
        kind: segment.kind,
        start: wallToUtc(day + segment.from * 60000, market.tz),
        end: wallToUtc(day + segment.to * 60000, market.tz),
      });
    });
  }
  segments.sort((a, b) => a.start - b.start);
  const timeline = [];
  segments.forEach((segment) => {
    const previous = timeline[timeline.length - 1];
    if (previous && segment.start > previous.end) {
      timeline.push({
        kind: 'closed',
        start: previous.end,
        end: segment.start,
      });
    }
    timeline.push(segment);
  });
  return timeline;
}

// sessionKinds 是这个市场会出现的时段，按图例顺序。
export function sessionKinds(id) {
  const market = MARKET_SESSIONS[id];
  if (!market) return [];
  const kinds = new Set(market.segments.map((segment) => segment.kind));
  kinds.add('closed');
  return SESSION_ORDER.filter((kind) => kinds.has(kind));
}

// sessionState 是 now 所在的时段；targetAt 在盘中是收盘时刻，其余时段是下一次开盘时刻。
export function sessionState(id, now) {
  const timeline = sessionTimeline(id, now);
  const current = timeline.find(
    (segment) => segment.start <= now && now < segment.end,
  );
  if (!current) return null;
  const nextOpen = timeline.find(
    (segment) => segment.kind === 'open' && segment.start > now,
  );
  return {
    ...current,
    targetAt:
      current.kind === 'open' || !nextOpen ? current.end : nextOpen.start,
  };
}

// sessionWeek 是本地时间这一周(周一到周日)每天的交易时段，单位是当天 0 点起的分钟数；ticks 是这一周各交易时段的起止
// 落在本地时间的分钟数，作时间轴刻度。
export function sessionWeek(id, now) {
  const timeline = sessionTimeline(id, now);
  const today = new Date(now);
  const year = today.getFullYear();
  const month = today.getMonth();
  const monday = today.getDate() - ((today.getDay() + 6) % 7);
  const rows = [];
  const ticks = new Set();
  for (let index = 0; index < 7; index++) {
    const dayStart = new Date(year, month, monday + index).getTime();
    const dayEnd = new Date(year, month, monday + index + 1).getTime();
    const bars = [];
    timeline.forEach((segment) => {
      if (segment.kind === 'closed') return;
      if (segment.end <= dayStart || segment.start >= dayEnd) return;
      bars.push({
        kind: segment.kind,
        from: (Math.max(segment.start, dayStart) - dayStart) / 60000,
        to: Math.min((Math.min(segment.end, dayEnd) - dayStart) / 60000, 1440),
      });
      [segment.start, segment.end].forEach((ts) => {
        if (ts <= dayStart || ts >= dayEnd) return;
        const time = new Date(ts);
        ticks.add(time.getHours() * 60 + time.getMinutes());
      });
    });
    rows.push({ dayStart, bars });
  }
  return { rows, ticks: [...ticks].sort((a, b) => a - b) };
}
