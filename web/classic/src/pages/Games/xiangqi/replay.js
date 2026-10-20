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

import { PIECE_GLYPHS } from './records';

const validPoint = (point) =>
  Array.isArray(point) &&
  point.length === 2 &&
  Number.isInteger(point[0]) &&
  Number.isInteger(point[1]) &&
  point[0] >= 0 &&
  point[0] < 9 &&
  point[1] >= 0 &&
  point[1] < 10;

// 从实盘逆推，不假定旧局从初始棋盘开始记录。遇到断档或不匹配的棋步就停，只开放可验证的连续区间。
export function buildXiangqiReplay(state) {
  const lastPly = Number.isInteger(state?.plies) ? state.plies : 0;
  const replay = { firstPly: lastPly, lastPly, positions: new Map() };
  if (
    !Array.isArray(state?.board) ||
    state.board.length !== 10 ||
    state.board.some(
      (row) => typeof row !== 'string' || !/^[KABNRCPkabnrcp.]{9}$/.test(row),
    )
  ) {
    return replay;
  }

  const board = state.board.map((row) => [...row]);
  replay.positions.set(lastPly, state.board.join(''));
  const history = state.history || [];
  for (let i = history.length - 1; i >= 0; i--) {
    const entry = history[i];
    if (
      entry.ply !== replay.firstPly ||
      entry.ply < 1 ||
      !validPoint(entry.from) ||
      !validPoint(entry.to) ||
      !Object.hasOwn(PIECE_GLYPHS, entry.piece) ||
      entry.color !==
        (entry.piece === entry.piece.toUpperCase() ? 'red' : 'black') ||
      (entry.captured &&
        (!Object.hasOwn(PIECE_GLYPHS, entry.captured) ||
          (entry.captured === entry.captured.toUpperCase()) ===
            (entry.color === 'red'))) ||
      board[entry.from[1]][entry.from[0]] !== '.' ||
      board[entry.to[1]][entry.to[0]] !== entry.piece
    ) {
      break;
    }
    board[entry.from[1]][entry.from[0]] = entry.piece;
    board[entry.to[1]][entry.to[0]] = entry.captured || '.';
    replay.firstPly = entry.ply - 1;
    replay.positions.set(
      replay.firstPly,
      board.map((row) => row.join('')).join(''),
    );
  }
  return replay;
}

// 同一步数可能因悔棋而变成另一局面，除步数外还核对棋盘。新增棋步保留回看位置，撤掉该局面或换局则回到实盘。
export function resolveXiangqiReview(replay, review, gameId) {
  if (
    !review ||
    review.gameId !== gameId ||
    review.ply >= replay.lastPly ||
    replay.positions.get(review.ply) !== review.board
  ) {
    return null;
  }
  return review.ply;
}

// 历史接口没有将军、重复或未吃子计数，回看时只派生已知的棋盘、轮次与行棋标记，不沿用实盘状态。
export function xiangqiReplayPosition(state, replay, ply) {
  const board = replay.positions.get(ply);
  if (!board || ply === state.plies) return state;
  const history = (state.history || []).filter(
    (entry) => entry.ply > replay.firstPly && entry.ply <= ply,
  );
  const lastMoves = {};
  for (const entry of history) lastMoves[entry.color] = entry;
  const last = history.at(-1) || null;
  return {
    ...state,
    board: Array.from({ length: 10 }, (_, y) => board.slice(y * 9, y * 9 + 9)),
    plies: ply,
    turn: ply % 2 === 0 ? 'red' : 'black',
    history,
    last,
    last_moves: lastMoves,
    check: false,
    quiet: 0,
    repeat: 0,
    over: false,
    result: '',
    legal: [],
  };
}
