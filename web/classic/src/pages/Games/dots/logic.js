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

// 点格棋棋盘：SIZE × SIZE 个格子。边的编号与服务端 pkg/dots 一致：先是横边，第 r 行第 c 条是 r*SIZE + c；
// 再是竖边，第 r 行第 c 条是 HORIZONTAL + r*(SIZE+1) + c。格子按行编号 r*SIZE + c。
export const SIZE = 4;
export const HORIZONTAL = (SIZE + 1) * SIZE;
export const EDGES = HORIZONTAL + SIZE * (SIZE + 1);

// 边的两个端点(点的行列)。
export function edgeEnds(edge) {
  if (edge < HORIZONTAL) {
    const r = Math.floor(edge / SIZE);
    const c = edge % SIZE;
    return [
      [r, c],
      [r, c + 1],
    ];
  }
  const v = edge - HORIZONTAL;
  const r = Math.floor(v / (SIZE + 1));
  const c = v % (SIZE + 1);
  return [
    [r, c],
    [r + 1, c],
  ];
}

// 格子的四条边：上、下、左、右。
export function boxEdges(box) {
  const r = Math.floor(box / SIZE);
  const c = box % SIZE;
  return [
    r * SIZE + c,
    (r + 1) * SIZE + c,
    HORIZONTAL + r * (SIZE + 1) + c,
    HORIZONTAL + r * (SIZE + 1) + c + 1,
  ];
}

// 边两侧的格子(棋盘边上的边只有一个)。
export function edgeBoxes(edge) {
  const boxes = [];
  for (let box = 0; box < SIZE * SIZE; box++) {
    if (boxEdges(box).includes(edge)) boxes.push(box);
  }
  return boxes;
}

// edgeEffect 说明在 edges('0'/'1' 串)上连 edge 的后果：capture 是能围下几个格子，gift 是会让几个格子只差一条边(送给对方)。
export function edgeEffect(edges, edge) {
  let capture = 0;
  let gift = 0;
  for (const box of edgeBoxes(edge)) {
    const drawn = boxEdges(box).filter((e) => edges[e] === '1').length;
    if (drawn === 3) capture++;
    else if (drawn === 2) gift++;
  }
  return { capture, gift };
}
