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

import { lazy } from 'react';
import {
  Binary,
  Bird,
  Blocks,
  Calculator,
  CircleDot,
  Diamond,
  Bomb,
  Brain,
  ChartPie,
  CircleDollarSign,
  Club,
  Crown,
  Dices,
  Gem,
  Grid2x2,
  Grid3x3,
  Hand,
  Layers,
  LayoutGrid,
  Network,
  Rocket,
  Sparkles,
  Spade,
  Ticket,
  Trophy,
  Waypoints,
  Worm,
  Circle,
  Hash,
  Lightbulb,
} from 'lucide-react';

// 每个游戏的页面组件(按需加载)与在游戏中心里的图标。键与 constants/game.constants.js 的游戏标识一致，
// 页面目录名不一定等于标识(2048 放在 g2048 目录里，免得目录名以数字开头)。
export const GAME_PAGES = {
  tetris: lazy(() => import('./tetris')),
  2048: lazy(() => import('./g2048')),
  snake: lazy(() => import('./snake')),
  flappy: lazy(() => import('./flappy')),
  minesweeper: lazy(() => import('./minesweeper')),
  sudoku: lazy(() => import('./sudoku')),
  memory: lazy(() => import('./memory')),
  puzzle15: lazy(() => import('./puzzle15')),
  pairlink: lazy(() => import('./pairlink')),
  breakout: lazy(() => import('./breakout')),
  sokoban: lazy(() => import('./sokoban')),
  lightsout: lazy(() => import('./lightsout')),
  hanoi: lazy(() => import('./hanoi')),
  twentyfour: lazy(() => import('./twentyfour')),
  bulls: lazy(() => import('./bulls')),
  nonogram: lazy(() => import('./nonogram')),
  gomoku: lazy(() => import('./gomoku')),
  connect4: lazy(() => import('./connect4')),
  reversi: lazy(() => import('./reversi')),
  xiangqi: lazy(() => import('./xiangqi')),
  rps: lazy(() => import('./rps')),
  landlord: lazy(() => import('./landlord')),
  dots: lazy(() => import('./dots')),
  slots: lazy(() => import('./slots')),
  wheel: lazy(() => import('./wheel')),
  plinko: lazy(() => import('./plinko')),
  dice: lazy(() => import('./dice')),
  roulette: lazy(() => import('./roulette')),
  blackjack: lazy(() => import('./blackjack')),
  mines: lazy(() => import('./mines')),
  hilo: lazy(() => import('./hilo')),
  crash: lazy(() => import('./crash')),
  scratch: lazy(() => import('./scratch')),
  baccarat: lazy(() => import('./baccarat')),
  keno: lazy(() => import('./keno')),
  jackpot: lazy(() => import('./jackpot')),
};

export const GAME_ICONS = {
  tetris: Blocks,
  2048: Grid2x2,
  snake: Worm,
  flappy: Bird,
  minesweeper: Bomb,
  sudoku: Hash,
  memory: Brain,
  puzzle15: Grid3x3,
  pairlink: Gem,
  breakout: Blocks,
  sokoban: Grid2x2,
  lightsout: Lightbulb,
  hanoi: Layers,
  twentyfour: Calculator,
  bulls: Binary,
  nonogram: Grid3x3,
  gomoku: Circle,
  connect4: LayoutGrid,
  reversi: Layers,
  xiangqi: Crown,
  rps: Hand,
  landlord: Spade,
  dots: Waypoints,
  slots: Sparkles,
  wheel: ChartPie,
  plinko: Network,
  dice: Dices,
  roulette: CircleDollarSign,
  blackjack: Spade,
  mines: Gem,
  hilo: Club,
  crash: Rocket,
  scratch: Ticket,
  baccarat: Diamond,
  keno: CircleDot,
  jackpot: Trophy,
};
