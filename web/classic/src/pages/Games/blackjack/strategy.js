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

// 一张牌的点数：A 记 11，J、Q、K 记 10。
export const cardValue = (card) =>
  card.rank === 1 ? 11 : Math.min(card.rank, 10);

// 基本策略(6 副牌、庄家软 17 停牌、可以分牌后加倍、没有投降)给出的建议动作。actions 是这一手现在能做的动作，
// 想加倍或分牌但不能时退回要牌或停牌。只是参考，玩家自己决定怎么打。
export function basicStrategy(hand, upcard, actions) {
  const dealer = cardValue(upcard);
  const can = (action) => actions.includes(action);
  const among = (...values) => values.includes(dealer);
  const pair = cardValue(hand.cards[0]);
  if (
    hand.cards.length === 2 &&
    pair === cardValue(hand.cards[1]) &&
    can('split')
  ) {
    const split =
      pair === 11 ||
      pair === 8 ||
      (pair === 9 && !among(7, 10, 11)) ||
      ((pair === 7 || pair === 3 || pair === 2) && dealer <= 7) ||
      (pair === 6 && dealer <= 6) ||
      (pair === 4 && among(5, 6));
    if (split) return 'split';
  }
  const total = hand.total;
  const doubleOr = (fallback) => (can('double') ? 'double' : fallback);
  if (hand.soft) {
    if (total >= 20) return 'stand';
    if (total === 19) return dealer === 6 ? doubleOr('stand') : 'stand';
    if (total === 18) {
      if (dealer >= 3 && dealer <= 6) return doubleOr('stand');
      return among(2, 7, 8) ? 'stand' : 'hit';
    }
    if (total === 17)
      return dealer >= 3 && dealer <= 6 ? doubleOr('hit') : 'hit';
    if (total >= 15)
      return dealer >= 4 && dealer <= 6 ? doubleOr('hit') : 'hit';
    if (total >= 13)
      return dealer >= 5 && dealer <= 6 ? doubleOr('hit') : 'hit';
    // 软 12 只有不能再分的一对 A，要牌。
    return 'hit';
  }
  if (total >= 17) return 'stand';
  if (total >= 13) return dealer <= 6 ? 'stand' : 'hit';
  if (total === 12) return dealer >= 4 && dealer <= 6 ? 'stand' : 'hit';
  if (total === 11) return dealer <= 10 ? doubleOr('hit') : 'hit';
  if (total === 10) return dealer <= 9 ? doubleOr('hit') : 'hit';
  if (total === 9) return dealer >= 3 && dealer <= 6 ? doubleOr('hit') : 'hit';
  return 'hit';
}
