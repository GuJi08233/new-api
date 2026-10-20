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

import { cardRank } from './cards.js';

// 与 pkg/landlord.Classify 使用同一牌型约定，仅给选牌提供即时反馈；最终出牌仍由服务端校验。
export function classifyCombination(cards) {
  if (cards.length === 0 || cards.length > 20) return null;
  const seen = new Set();
  const counts = Array(18).fill(0);
  for (const card of cards) {
    if (!Number.isInteger(card) || card < 0 || card >= 54 || seen.has(card))
      return null;
    seen.add(card);
    counts[cardRank(card)] += 1;
  }
  if (cards.length === 2 && counts[16] === 1 && counts[17] === 1)
    return { type: 'rocket', rank: 17, chain: 1 };

  const ranks = [];
  const groups = [[], [], [], [], []];
  for (let rank = 3; rank <= 17; rank += 1) {
    if (counts[rank] === 0) continue;
    ranks.push(rank);
    groups[counts[rank]].push(rank);
  }
  if (ranks.length === 1) {
    return {
      type: ['', 'single', 'pair', 'triple', 'bomb'][cards.length],
      rank: ranks[0],
      chain: 1,
    };
  }
  if (groups[3].length === 1) {
    if (cards.length === 4 && groups[1].length === 1)
      return { type: 'triple_single', rank: groups[3][0], chain: 1 };
    if (cards.length === 5 && groups[2].length === 1)
      return { type: 'triple_pair', rank: groups[3][0], chain: 1 };
  }
  if (groups[4].length === 1) {
    if (
      cards.length === 6 &&
      (groups[1].length === 2 || groups[2].length === 1)
    )
      return { type: 'four_two', rank: groups[4][0], chain: 1 };
    if (cards.length === 8 && groups[2].length === 2)
      return { type: 'four_two_pairs', rank: groups[4][0], chain: 1 };
  }
  const highest = ranks[ranks.length - 1];
  if (highest <= 14 && highest - ranks[0] + 1 === ranks.length) {
    if (cards.length >= 5 && groups[1].length === ranks.length)
      return { type: 'straight', rank: highest, chain: ranks.length };
    if (ranks.length >= 3 && groups[2].length === ranks.length)
      return { type: 'pair_straight', rank: highest, chain: ranks.length };
    if (ranks.length >= 2 && groups[3].length === ranks.length)
      return { type: 'airplane', rank: highest, chain: ranks.length };
  }
  for (const [size, type] of [
    [4, 'airplane_single'],
    [5, 'airplane_pair'],
  ]) {
    const chain = cards.length / size;
    if (!Number.isInteger(chain) || chain < 2) continue;
    for (let first = 3; first + chain - 1 <= 14; first += 1) {
      let valid = true;
      let pairs = 0;
      for (let rank = 3; rank <= 17; rank += 1) {
        if (rank >= first && rank < first + chain) {
          if (counts[rank] !== 3) valid = false;
        } else {
          if (counts[rank] > 2 || (size === 5 && counts[rank] === 1))
            valid = false;
          if (counts[rank] === 2) pairs += 1;
        }
      }
      if (valid && (size === 4 || pairs === chain))
        return { type, rank: first + chain - 1, chain };
    }
  }
  return null;
}

// 不把“尚未选完”误报成能出牌，也不把提示候选当作全部合法选牌的白名单。
export function selectionFeedback(cards, previous) {
  if (cards.length === 0) return { reason: 'empty', combination: null };
  const combination = classifyCombination(cards);
  if (!combination) return { reason: 'invalid', combination: null };
  if (!previous) return { reason: null, combination };
  if (previous.type === 'rocket') return { reason: 'rank', combination };
  if (
    combination.type === 'rocket' ||
    (combination.type === 'bomb' && previous.type !== 'bomb')
  )
    return { reason: null, combination };
  if (
    combination.type !== previous.type ||
    combination.chain !== previous.chain
  )
    return { reason: 'shape', combination };
  return {
    reason: combination.rank > previous.rank ? null : 'rank',
    combination,
  };
}

// 提示只在牌面确实仍是该候选时显示序号；手动改牌或清空后不会残留旧的“第 N 组”。
export function selectedHintIndex(selected, hints, choice, key) {
  if (choice.key !== key) return -1;
  const candidate = hints[choice.index];
  if (
    !candidate ||
    candidate.length !== selected.length ||
    !candidate.every((card) => selected.includes(card))
  )
    return -1;
  return choice.index;
}
