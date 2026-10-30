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

// 两格上下左右相邻。
export function adjacent(size, a, b) {
  if (a === undefined || b === undefined) return false;
  const [ra, ca] = [Math.floor(a / size), a % size];
  const [rb, cb] = [Math.floor(b / size), b % size];
  return Math.abs(ra - rb) + Math.abs(ca - cb) === 1;
}

// 把画好的路线换成要提交的操作：先撤回多少步(两条路线分叉之后服务端多出来的格子)，再连走哪几格。起点不能撤回。
export function planStroke(committed, next) {
  let shared = 0;
  while (
    shared < committed.length &&
    shared < next.length &&
    committed[shared] === next[shared]
  )
    shared++;
  shared = Math.max(shared, 1);
  return {
    undo: Math.max(0, committed.length - shared),
    cells: next.slice(shared),
  };
}
