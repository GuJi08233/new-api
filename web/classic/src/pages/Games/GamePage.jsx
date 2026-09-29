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

import React from 'react';
import { Navigate, useParams } from 'react-router-dom';
import { GAME_PAGES } from './registry';

// /games/:game 的页面：按游戏标识找到对应的游戏页，未知的游戏回到游戏中心。只认 GAME_PAGES 自己的键：constructor、
// toString 这类从原型继承来的属性也能取到值，拿去渲染会报错，而不是回到游戏中心。
const GamePage = () => {
  const { game } = useParams();
  const Page = Object.prototype.hasOwnProperty.call(GAME_PAGES, game)
    ? GAME_PAGES[game]
    : null;
  if (!Page) {
    return <Navigate to='/games' replace />;
  }
  return <Page />;
};

export default GamePage;
