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

import { useEffect, useSyncExternalStore } from 'react';
import {
  ensureModelIconsLoaded,
  getModelIconsVersion,
  subscribeModelIcons,
} from '../../helpers';

/**
 * 自定义 Hook：加载并订阅模型管理中配置的图标规则。
 * 返回的版本号仅用于驱动重渲染（例如作为列定义 useMemo 的依赖），
 * 使异步加载完成后 renderModelTag 能补上此前缺失的图标。
 */
export function useModelIcons() {
  useEffect(() => {
    ensureModelIconsLoaded();
  }, []);

  return useSyncExternalStore(subscribeModelIcons, getModelIconsVersion);
}
