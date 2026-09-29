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

import { useCallback, useEffect, useRef, useState } from 'react';
import { gameGet } from '../api';

// 出错之后隔多久再轮询，免得服务端出问题时连续请求。
const RETRY_MS = 3000;

export const isRoomOpen = (room) =>
  room?.status === 'waiting' || room?.status === 'playing';

// 对战房间的长轮询：房间还在等待或对局中时，一直带着当前版本请求 GET /api/game/rooms/:id?after=version，
// 服务端在房间有变化(对手走了一步、加入、超时)时立即返回，否则最多等 25 秒。自己走的一步由 setRoom 直接换上新状态。
// clockOffset() 是服务器时钟减本地时钟(秒)，倒计时按服务器时间算。
export function usePvpRoom(t) {
  const [room, setRoomState] = useState(null);
  const roomRef = useRef(null);
  const offsetRef = useRef(0);
  const versionRef = useRef(-1);

  const setRoom = useCallback((next) => {
    // 落子与长轮询的响应可能先后错乱：同一个房间只接受不比现在旧的状态，晚到的旧状态不能盖住新的，否则这时挂着的长轮询
    // 带的是新版本，要等到下一次变化或 25 秒后才会把页面纠正过来。
    const current = roomRef.current;
    if (
      next &&
      current &&
      next.id === current.id &&
      next.version < current.version
    ) {
      return;
    }
    roomRef.current = next;
    if (next?.server_time) {
      offsetRef.current = next.server_time - Date.now() / 1000;
    }
    versionRef.current = next?.version ?? -1;
    setRoomState(next);
  }, []);

  const roomId = room?.id;
  const open = isRoomOpen(room);
  useEffect(() => {
    if (!roomId || !open) return undefined;
    let alive = true;
    const controller = new AbortController();
    const loop = async () => {
      while (alive) {
        const response = await gameGet(
          `/api/game/rooms/${roomId}?after=${versionRef.current}`,
          t,
          { signal: controller.signal },
        );
        if (!alive) return;
        if (response.data) {
          if (response.data.version !== versionRef.current) {
            setRoom(response.data);
          }
          if (!isRoomOpen(response.data)) return;
        } else {
          await new Promise((resolve) => setTimeout(resolve, RETRY_MS));
        }
      }
    };
    loop();
    return () => {
      alive = false;
      controller.abort();
    };
  }, [roomId, open, t, setRoom]);

  const clockOffset = useCallback(() => offsetRef.current, []);
  return { room, setRoom, clockOffset };
}
