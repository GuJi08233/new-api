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

// 棋子和棋谱中的汉字属于棋具本身，各种界面语言使用相同的字形。
export const PIECE_GLYPHS = {
  K: '帅',
  A: '仕',
  B: '相',
  N: '马',
  R: '车',
  C: '炮',
  P: '兵',
  k: '将',
  a: '士',
  b: '象',
  n: '马',
  r: '车',
  c: '炮',
  p: '卒',
};

// 仅在旧接口缺少标准记谱时使用固定坐标，不根据当前棋盘猜测前后子。
export const squareLabel = ([x, y]) => `${String.fromCharCode(65 + x)}${y + 1}`;

export const xiangqiMoveLabel = (entry) =>
  entry.notation ||
  `${PIECE_GLYPHS[entry.piece]} ${squareLabel(entry.from)} → ${squareLabel(entry.to)}`;

// 依据真实步数配对，旧局从黑方应着开始记录时仍保留空缺的红方一着。
export function groupXiangqiHistory(history) {
  const rounds = new Map();
  for (const entry of history) {
    const number = Math.ceil(entry.ply / 2);
    if (!rounds.has(number)) rounds.set(number, { number });
    rounds.get(number)[entry.color] = entry;
  }
  return [...rounds.values()].sort((a, b) => a.number - b.number);
}

// 汇总以已记录的实际吃子为准，悔棋删掉的记录不会继续计入，旧局缺失的棋步也不补猜。
export function getXiangqiCaptures(history) {
  const captures = { red: [], black: [] };
  for (const entry of history) {
    if (entry.captured) captures[entry.color].push(entry);
  }
  return captures;
}

export function formatXiangqiRecord(
  history,
  captureText = (piece) => `吃${PIECE_GLYPHS[piece]}`,
) {
  return groupXiangqiHistory(history)
    .map(
      ({ number, red, black }) =>
        `${number}. ${[red, black]
          .map((entry) =>
            entry
              ? `${xiangqiMoveLabel(entry)}${entry.captured ? `（${captureText(entry.captured)}）` : ''}`
              : '…',
          )
          .join('  ')}`,
    )
    .join('\n');
}
