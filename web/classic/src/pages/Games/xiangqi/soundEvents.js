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

// 仅比较服务端已落子的棋步；记谱文字补全或本地选择变化都不产生音效。
const moveKey = (entry) =>
  JSON.stringify([
    entry.ply,
    entry.color,
    entry.piece,
    entry.captured || '',
    entry.from,
    entry.to,
  ]);

export function xiangqiSoundTransition(previous, state, gameId) {
  if (!state || gameId == null || !Number.isInteger(state.plies)) {
    return { cursor: null, sounds: [], reset: true };
  }
  const history = Array.isArray(state.history) ? state.history : [];
  const cursor = {
    gameId,
    plies: state.plies,
    moves: history.map(moveKey),
  };
  const result = { cursor, sounds: [], reset: true };
  if (!previous || previous.gameId !== gameId) return result;
  // 悔棋、替换棋谱或补载早期记录都建立新基线，不能播放撤回的着法。
  if (
    cursor.plies < previous.plies ||
    cursor.moves.length < previous.moves.length ||
    previous.moves.some((key, i) => key !== cursor.moves[i])
  ) {
    return result;
  }
  const added = history.slice(previous.moves.length);
  if (
    added.length !== cursor.plies - previous.plies ||
    added.some((entry, i) => entry.ply !== previous.plies + i + 1)
  ) {
    return result;
  }
  return {
    cursor,
    reset: false,
    sounds: added.map((entry) => (entry.captured ? 'capture' : 'move')),
  };
}
