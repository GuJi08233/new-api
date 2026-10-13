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

import { useSearchParams } from 'react-router-dom';

export const riskInteger = (value, fallback, min, max) => {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= min && parsed <= max
    ? parsed
    : fallback;
};

// 使用独立前缀保留各管理页的筛选和页码，切换标签或分享地址后仍能恢复。
export default function useRiskFilters(prefix, defaults) {
  const [params, setParams] = useSearchParams();
  const values = Object.fromEntries(
    Object.entries(defaults).map(([key, fallback]) => [
      key,
      params.get(`${prefix}_${key}`) ?? String(fallback),
    ]),
  );
  const update = (patch) => {
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (!Object.hasOwn(patch, 'p')) next.delete(`${prefix}_p`);
      for (const [key, value] of Object.entries(patch)) {
        if (value === '' || value === null) next.delete(`${prefix}_${key}`);
        else next.set(`${prefix}_${key}`, String(value));
      }
      return next;
    });
  };
  return [values, update];
}
