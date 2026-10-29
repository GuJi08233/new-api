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

import { useCallback, useContext, useEffect, useState } from 'react';
import { UserContext } from '../../../context/User';
import { readGameStorage, writeGameStorage } from './gameStorage';

// 解谜类游戏在本机记下的个人最佳(最短用时、最少步数)，按账号、游戏与难度分开。服务端只记"赢下过的最高难度"，
// 这里补上同一难度里的快慢，给玩家一个挑战自己的目标；只是浏览器里的记录，不参与奖励，换设备或清空浏览器数据就没了。
// 记录格式：{ [难度]: { time: 秒, moves: 步数 } }，每一项越小越好，各自独立刷新。
export function useLocalBest(game) {
  const [userState] = useContext(UserContext);
  const userId = userState?.user?.id;
  const key = `game-best:v1:${userId || 0}:${game}`;
  const [bests, setBests] = useState({});

  useEffect(() => {
    const saved = readGameStorage(key).value;
    setBests(saved && typeof saved === 'object' ? saved : {});
  }, [key]);

  // 记下一次过关的成绩，返回哪几项刷新了纪录({ time: true, moves: false })；第一次过关也算刷新。
  const record = useCallback(
    (level, values) => {
      const previous = readGameStorage(key).value || {};
      const current = previous[level] || {};
      const next = { ...current };
      const improved = {};
      for (const [name, value] of Object.entries(values)) {
        if (!Number.isFinite(value) || value < 0) continue;
        if (current[name] === undefined || value < current[name]) {
          next[name] = value;
          improved[name] = true;
        }
      }
      const saved = { ...previous, [level]: next };
      writeGameStorage(key, saved);
      setBests(saved);
      return improved;
    },
    [key],
  );

  return { bests, record };
}
