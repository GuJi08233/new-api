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

export const cardRank = (card) =>
  card >= 52 ? card - 52 + 16 : Math.floor(card / 4) + 3;

// 带牌的组合先摆三张/四张/飞机机身，再摆附件；各部分内部仍从大到小摆。
export function groupPlayedCards(cards, type) {
  const ordered = [...cards].sort((left, right) => right - left);
  let bodySize = 0;
  if (
    [
      'triple_single',
      'triple_pair',
      'airplane_single',
      'airplane_pair',
    ].includes(type)
  ) {
    bodySize = 3;
  } else if (['four_two', 'four_two_pairs'].includes(type)) {
    bodySize = 4;
  }
  if (!bodySize) return [ordered];
  const counts = new Map();
  for (const card of ordered) {
    const rank = cardRank(card);
    counts.set(rank, (counts.get(rank) || 0) + 1);
  }
  const body = ordered.filter(
    (card) => counts.get(cardRank(card)) === bodySize,
  );
  const wings = ordered.filter(
    (card) => counts.get(cardRank(card)) !== bodySize,
  );
  return [body, wings].filter((group) => group.length > 0);
}
