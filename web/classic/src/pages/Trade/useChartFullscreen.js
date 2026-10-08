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

import { useEffect, useState } from 'react';

// 原生全屏不可用或被浏览器拒绝时，用同一个图表节点铺满视口，避免重建图表丢失视窗。
export function useChartFullscreen(ref) {
  const [native, setNative] = useState(false);
  const [css, setCss] = useState(false);

  useEffect(() => {
    const sync = () => {
      const element =
        document.fullscreenElement || document.webkitFullscreenElement;
      setNative(!!ref.current && element === ref.current);
    };
    document.addEventListener('fullscreenchange', sync);
    document.addEventListener('webkitfullscreenchange', sync);
    return () => {
      document.removeEventListener('fullscreenchange', sync);
      document.removeEventListener('webkitfullscreenchange', sync);
    };
  }, [ref]);

  useEffect(() => {
    if (!css) return;
    const onKey = (event) => {
      if (event.key === 'Escape' && !event.defaultPrevented) setCss(false);
    };
    const overflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    window.addEventListener('keydown', onKey);
    return () => {
      document.body.style.overflow = overflow;
      window.removeEventListener('keydown', onKey);
    };
  }, [css]);

  const toggle = async () => {
    if (document.fullscreenElement || document.webkitFullscreenElement) {
      const exit = document.exitFullscreen || document.webkitExitFullscreen;
      try {
        await exit?.call(document);
      } catch {
        /* 浏览器自行管理原生全屏。 */
      }
      return;
    }
    if (css) {
      setCss(false);
      return;
    }
    const element = ref.current;
    if (!element) return;
    const request =
      element.requestFullscreen || element.webkitRequestFullscreen;
    if (!request) {
      setCss(true);
      return;
    }
    try {
      await request.call(element);
    } catch {
      // 异步拒绝时宿主可能已经卸载。
      if (ref.current === element) setCss(true);
    }
  };

  return { active: native || css, css, toggle };
}
