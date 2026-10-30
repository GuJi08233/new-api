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

// 播棋的页面逻辑。坑的编号与服务端 pkg/mancala 一致：0..5 先手小坑、6 先手大仓、7..12 后手小坑、13 后手大仓。
// 规则以服务端为准，这里只用来在点下去的那一刻把自己这一步一颗颗播出来。
export const PITS = 6;
export const HOLES = 14;
export const FIRST_STORE = 6;
export const SECOND_STORE = 13;

// side 0 是先手、1 是后手。
export const storeOf = (side) => (side === 0 ? FIRST_STORE : SECOND_STORE);
export const holeOf = (side, pit) => (side === 0 ? pit : FIRST_STORE + 1 + pit);
export const ownsHole = (side, hole) =>
  side === 0
    ? hole >= 0 && hole < PITS
    : hole > FIRST_STORE && hole < SECOND_STORE;

// 播 side 一方第 pit 个坑：返回每落一颗之后的盘面(frames)、落子顺序(path)、吃掉的对面坑(captured，没有为 -1)
// 与是否再走一次(extra)。坑是空的时返回 null。不处理一方坑空之后的收尾。
export function sow(board, side, pit) {
  let hole = holeOf(side, pit);
  let seeds = board[hole];
  if (!seeds) return null;
  const next = board.slice();
  next[hole] = 0;
  const frames = [next.slice()];
  const path = [];
  const skip = storeOf(1 - side);
  while (seeds > 0) {
    hole = (hole + 1) % HOLES;
    if (hole === skip) continue;
    next[hole]++;
    path.push(hole);
    frames.push(next.slice());
    seeds--;
  }
  let captured = -1;
  if (ownsHole(side, hole) && next[hole] === 1) {
    const opposite = 2 * PITS - hole;
    if (next[opposite] > 0) {
      next[storeOf(side)] += next[opposite] + 1;
      next[opposite] = 0;
      next[hole] = 0;
      captured = opposite;
      frames.push(next.slice());
    }
  }
  return { frames, path, captured, extra: hole === storeOf(side) };
}
