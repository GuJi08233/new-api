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

// 一行(或一列)里连续涂黑的段长，与服务端 pkg/nonogram 的提示同样计算；一格都没涂时是空数组。
export function lineRuns(line) {
  const runs = [];
  let count = 0;
  for (const on of line) {
    if (on) count++;
    else if (count > 0) {
      runs.push(count);
      count = 0;
    }
  }
  if (count > 0) runs.push(count);
  return runs;
}

// lineMatches 表示这一行现在的涂法正好符合提示(提示为空表示整行留白)。
export const lineMatches = (line, clue) => {
  const runs = lineRuns(line);
  const want = clue.filter((n) => n > 0);
  return runs.length === want.length && runs.every((n, i) => n === want[i]);
};

// boardLines 把 size×size 的盘面拆成行与列。
export function boardLines(filled, size) {
  const rows = [];
  const cols = [];
  for (let i = 0; i < size; i++) {
    rows.push(filled.slice(i * size, (i + 1) * size));
    cols.push(Array.from({ length: size }, (_, r) => filled[r * size + i]));
  }
  return { rows, cols };
}
