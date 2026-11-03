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

export function formatAmount(value) {
  const n = Math.floor(value || 0);
  if (n >= 1000000) return `${(n / 1000000).toFixed(n >= 10000000 ? 0 : 1)}M`;
  if (n >= 100000) return `${Math.floor(n / 1000)}k`;
  return n.toLocaleString();
}

// 倒计时：一小时以内写成"12 分 30 秒"，以上写成"3 小时 20 分"。
export function formatLeft(seconds, t) {
  const left = Math.max(0, Math.ceil(seconds));
  if (left < 60) return t('{{count}} 秒', { count: left });
  if (left < 3600) {
    return t('{{minutes}} 分 {{seconds}} 秒', {
      minutes: Math.floor(left / 60),
      seconds: left % 60,
    });
  }
  if (left < 86400) {
    return t('{{hours}} 小时 {{minutes}} 分', {
      hours: Math.floor(left / 3600),
      minutes: Math.floor((left % 3600) / 60),
    });
  }
  return t('{{days}} 天 {{hours}} 小时', {
    days: Math.floor(left / 86400),
    hours: Math.floor((left % 86400) / 3600),
  });
}
