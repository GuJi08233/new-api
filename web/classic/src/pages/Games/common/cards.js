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

import { Club, Diamond, Heart, Spade } from 'lucide-react';

// 扑克牌(21 点、高低牌)的点数与花色，编号与服务端一致：点数 1 到 13 依次是 A、2 到 10、J、Q、K；
// 花色 0 黑桃、1 红心、2 方块、3 梅花。
export const RANK_LABELS = [
  'A',
  '2',
  '3',
  '4',
  '5',
  '6',
  '7',
  '8',
  '9',
  '10',
  'J',
  'Q',
  'K',
];
export const SUIT_ICONS = [Spade, Heart, Diamond, Club];

// 红心与方块是红色的花色。
export const isRedSuit = (suit) => suit === 1 || suit === 2;
