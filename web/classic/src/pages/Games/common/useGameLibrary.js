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
import { GAME_ORDER } from '../../../constants/game.constants';
import { readGameStorage, writeGameStorage } from './gameStorage';

const knownGames = new Set(GAME_ORDER);

function readLibrary(key) {
  const { value } = readGameStorage(key);
  return {
    favorites: Array.isArray(value?.favorites)
      ? [...new Set(value.favorites.filter((game) => knownGames.has(game)))]
      : [],
    recent: Array.isArray(value?.recent)
      ? [...new Set(value.recent.filter((game) => knownGames.has(game)))].slice(
          0,
          8,
        )
      : [],
  };
}

// 只保存游戏标识，不保存账户或对局内容；浏览器拒绝存储时本次访问照常使用收藏。
export function useGameLibrary() {
  const [userState] = useContext(UserContext);
  const userId = userState?.user?.id;
  const key = `game-library:v1:${userId || 0}`;
  const [library, setLibrary] = useState(() => readLibrary(key));
  useEffect(() => {
    setLibrary(readLibrary(key));
    const onStorage = (event) => {
      if (event.key === key) setLibrary(readLibrary(key));
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, [key]);
  const toggleFavorite = useCallback(
    (game) => {
      if (!userId || !knownGames.has(game)) return;
      setLibrary((previous) => {
        const next = {
          ...previous,
          favorites: previous.favorites.includes(game)
            ? previous.favorites.filter((item) => item !== game)
            : [...previous.favorites, game],
        };
        writeGameStorage(key, next);
        return next;
      });
    },
    [key, userId],
  );
  const recordRecent = useCallback(
    (game) => {
      if (!userId || !knownGames.has(game)) return;
      const previous = readLibrary(key);
      if (previous.recent[0] === game) return;
      const next = {
        ...previous,
        recent: [
          game,
          ...previous.recent.filter((item) => item !== game),
        ].slice(0, 8),
      };
      writeGameStorage(key, next);
      setLibrary(next);
    },
    [key, userId],
  );
  return { ...library, toggleFavorite, recordRecent };
}
