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

// 一次滑动只由开始它的指针结束。第二根手指不会覆盖起点，也不会清除第一根手指。
export function beginSwipe(current, event) {
  if (current || event.isPrimary === false) return current;
  return { id: event.pointerId, x: event.clientX, y: event.clientY };
}

export function finishSwipe(current, event, threshold) {
  if (!current || current.id !== event.pointerId)
    return { current, delta: null };
  const x = event.clientX - current.x;
  const y = event.clientY - current.y;
  return {
    current: null,
    delta: Math.max(Math.abs(x), Math.abs(y)) >= threshold ? { x, y } : null,
  };
}
