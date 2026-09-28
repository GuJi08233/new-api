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

// 玩家走法与 pkg/sokoban 一致，最终成绩由服务端从开局地图重放。
export function movePuzzle(puzzle, direction) {
  const delta = { U: -puzzle.width, D: puzzle.width, L: -1, R: 1 }[direction];
  if (!delta) return null;
  const next = puzzle.player + delta;
  const floor = (position) =>
    position >= 0 &&
    position < puzzle.width * puzzle.height &&
    !puzzle.walls.includes(position);
  const horizontal = delta === 1 || delta === -1;
  if (
    !floor(next) ||
    (horizontal &&
      Math.floor(next / puzzle.width) !==
        Math.floor(puzzle.player / puzzle.width))
  )
    return null;
  const boxes = [...puzzle.boxes];
  const index = boxes.indexOf(next);
  if (index >= 0) {
    const after = next + delta;
    if (
      !floor(after) ||
      boxes.includes(after) ||
      (horizontal &&
        Math.floor(after / puzzle.width) !== Math.floor(next / puzzle.width))
    )
      return null;
    boxes[index] = after;
  }
  return {
    ...puzzle,
    boxes,
    player: next,
    pushes: (puzzle.pushes || 0) + (index >= 0 ? 1 : 0),
  };
}

export function replayPuzzle(puzzle, moves) {
  let current = puzzle;
  let applied = '';
  for (const direction of moves) {
    const next = movePuzzle(current, direction);
    if (!next) break;
    current = next;
    applied += direction;
  }
  return { puzzle: current, moves: applied };
}

export const solved = (puzzle) =>
  puzzle.boxes.length > 0 &&
  puzzle.boxes.every((box) => puzzle.goals.includes(box));
