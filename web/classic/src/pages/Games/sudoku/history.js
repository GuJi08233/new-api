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

// 撤销同时保存数字和候选笔记，避免擦掉误填数字后丢失自动清除的候选。
export class SudokuHistory {
  constructor(limit = 200) {
    this.limit = limit;
    this.entries = [];
  }

  record(values, notes) {
    this.entries.push({ values: values.slice(), notes: notes.slice() });
    if (this.entries.length > this.limit) this.entries.shift();
  }

  undo() {
    return this.entries.pop() || null;
  }

  clear() {
    this.entries = [];
  }

  get size() {
    return this.entries.length;
  }
}
