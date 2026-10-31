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

// 问道的水墨素材共用的小工具。颜色都取自 wendao.css 里 .wd-page 上的变量(--wd-ink、--wd-paper……)，深色主题自动换成
// 夜色墨纸，所以 SVG 里不写死颜色，用 ink('ink') 这样的 style。

export const ink = (name, opacity) =>
  opacity === undefined
    ? { fill: `var(--wd-${name})` }
    : { fill: `var(--wd-${name})`, fillOpacity: opacity };

export const stroke = (name, width = 2, opacity) => ({
  fill: 'none',
  stroke: `var(--wd-${name})`,
  strokeWidth: width,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  ...(opacity === undefined ? {} : { strokeOpacity: opacity }),
});

// 一道山脊：从左到右按几组正弦叠出起伏，seed 不同山形就不同，同一个 seed 永远画出同一座山。
export function ridge({
  y,
  amp,
  seed,
  width = 800,
  height = 520,
  step = 16,
  sharp = 0,
}) {
  let d = `M0 ${height} L0 ${y}`;
  for (let x = 0; x <= width; x += step) {
    const a = Math.abs(Math.sin((x + seed) * 0.009));
    const b = Math.sin((x + seed * 3) * 0.031) * 0.35;
    const c = Math.sin((x + seed * 7) * 0.083) * 0.12;
    const peak = sharp ? Math.pow(a, 1 + sharp) : a;
    d += ` L${x} ${(y - amp * (peak + b + c)).toFixed(1)}`;
  }
  return `${d} L${width} ${height} Z`;
}

// 一棵水墨松树：树干与三层松针。
export function pinePath(x, y, s) {
  return [
    `M${x - 1.2 * s} ${y} L${x - 0.6 * s} ${y - 30 * s} L${x + 0.6 * s} ${y - 30 * s} L${x + 1.2 * s} ${y} Z`,
    `M${x} ${y - 46 * s} L${x - 9 * s} ${y - 32 * s} L${x + 9 * s} ${y - 32 * s} Z`,
    `M${x} ${y - 38 * s} L${x - 13 * s} ${y - 20 * s} L${x + 13 * s} ${y - 20 * s} Z`,
    `M${x} ${y - 28 * s} L${x - 16 * s} ${y - 8 * s} L${x + 16 * s} ${y - 8 * s} Z`,
  ].join(' ');
}

// 伪随机：同一个 seed 得到同一串数，用来撒星点、雪花、落叶。
export function scatter(count, seed, width, height) {
  let s = seed;
  const next = () => {
    s = (s * 9301 + 49297) % 233280;
    return s / 233280;
  };
  return Array.from({ length: count }, () => ({
    x: next() * width,
    y: next() * height,
    r: next(),
  }));
}
