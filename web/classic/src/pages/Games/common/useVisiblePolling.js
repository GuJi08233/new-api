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

import { useEffect } from 'react';

// 页面可见时每隔 intervalMs 调一次 poll；切到后台时停下，不再白白请求服务端，切回来时立即调一次再接着定时。
// enabled 为假时不轮询。poll 要用 useCallback 固定下来，它变了会重新开始计时。
export function useVisiblePolling(poll, intervalMs, enabled) {
  useEffect(() => {
    if (!enabled) return undefined;
    let timer = null;
    const start = () => {
      if (timer === null) timer = setInterval(poll, intervalMs);
    };
    const stop = () => {
      clearInterval(timer);
      timer = null;
    };
    const onVisibilityChange = () => {
      if (document.hidden) {
        stop();
        return;
      }
      poll();
      start();
    };
    if (!document.hidden) start();
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => {
      stop();
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [poll, intervalMs, enabled]);
}
