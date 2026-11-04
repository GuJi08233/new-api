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

const PERIOD_UNIT_SECONDS = {
  w: 604800n,
  d: 86400n,
  h: 3600n,
  m: 60n,
  s: 1n,
};

export function parseSubscriptionPeriodDuration(input) {
  if (typeof input !== 'string' || !input.trim()) return null;

  const source = input.trim().toLowerCase();
  const parts = /(\d+(?:\.\d+)?|\.\d+)\s*([wdhms])\s*/gy;
  let consumed = 0;
  let totalSeconds = 0n;
  let match;
  while ((match = parts.exec(source)) !== null) {
    const [whole, fraction = ''] = match[1].split('.');
    const scale = 10n ** BigInt(fraction.length);
    const numerator =
      BigInt(`${whole || '0'}${fraction}`) * PERIOD_UNIT_SECONDS[match[2]];

    // Parse decimal quantities exactly so 0.07h is accepted as 252 seconds,
    // while fractional seconds are never rounded into a different period.
    if (numerator % scale !== 0n) return null;
    totalSeconds += numerator / scale;
    if (totalSeconds > BigInt(Number.MAX_SAFE_INTEGER)) return null;
    consumed = match.index + match[0].length;
  }

  if (consumed !== source.length || totalSeconds <= 0n) return null;
  return Number(totalSeconds);
}

export function formatSubscriptionPeriodDuration(seconds) {
  if (!Number.isSafeInteger(seconds) || seconds <= 0) return '';

  let remaining = seconds;
  const parts = [];
  for (const [unit, unitSeconds] of [
    ['d', 86400],
    ['h', 3600],
    ['m', 60],
    ['s', 1],
  ]) {
    const amount = Math.floor(remaining / unitSeconds);
    if (amount === 0) continue;
    parts.push(`${amount}${unit}`);
    remaining %= unitSeconds;
  }
  return parts.join('');
}
