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

// 局面分析的显示：引擎给出的分数是"分"，一个马约 270、一个车约 600，页面换成人能读的说法。
// 提示只在服务端算好之后随响应返回，这里只做展示，不再重复判断棋力。

// 分差超过这么多就算"明显占优"：约四分之一个车，业余棋手也能感觉出差别。
const CLEAR_EDGE = 150;

// 一步大约多少分：用来把分值换成"约几个子"。
const PIECE_VALUE = 270;

// formatEvaluation 把从走棋一方看的分数说成一句话：正数是自己占优。杀棋时按步数说，不谈子力。
export function formatEvaluation(score, mate, t) {
  if (mate > 0) return t('约 {{moves}} 步内将死对方', { moves: mate });
  if (mate < 0) return t('约 {{moves}} 步内被将死', { moves: -mate });
  if (!Number.isFinite(score)) return '';
  if (score >= CLEAR_EDGE) {
    return t('优势约 {{pieces}} 个子', {
      pieces: (score / PIECE_VALUE).toFixed(1),
    });
  }
  if (score <= -CLEAR_EDGE) {
    return t('落后约 {{pieces}} 个子', {
      pieces: (-score / PIECE_VALUE).toFixed(1),
    });
  }
  return t('大致均势');
}

// hintForPlies 返回仍适用于当前局面的提示：走了一步、悔棋之后局面变了，旧的提示就作废，不能继续透给玩家。
export function hintForPlies(hint, state) {
  if (!hint || !state) return null;
  if (!Number.isInteger(hint.plies) || hint.plies !== state.plies) return null;
  return hint;
}

// hintMarker 是提示在棋盘上要标出的格子：起点、落点各一个。没有提示时为 null。
export function hintMarker(hint) {
  const line = hint?.line;
  if (!line || !Array.isArray(line.from) || !Array.isArray(line.to)) {
    return null;
  }
  return { from: line.from, to: line.to };
}
