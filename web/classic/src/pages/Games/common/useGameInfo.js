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

import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { API } from '../../../helpers';

// 读取游戏页的规则与进度(GET /api/game/<key>)。info 里除了各游戏自己的字段，总有 balance、symbol、enabled 与
// play_turnstile。游戏关闭、IP 被封禁时 unavailable 是原因，页面只显示它。
// reload(true) 用于操作没有得到答复之后重新读取：这时读不到就保留页面上的内容。
export function useGameInfo(key) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState('');
  const [info, setInfo] = useState(null);

  const reload = useCallback(
    async (refresh = false) => {
      try {
        // 重新读取时页面上已经有内容，失败由调用方自己提示，不再弹全局的错误。
        const res = await API.get(`/api/game/${key}`, {
          skipErrorHandler: refresh,
        });
        if (!res.data.success) {
          if (!refresh) setUnavailable(res.data.message);
          return null;
        }
        setInfo(res.data.data);
        setUnavailable('');
        return res.data.data;
      } catch (error) {
        // 被风控封禁的 IP 收到 403，显示服务端给出的原因；其他错误已由全局拦截器提示。
        if (!refresh) {
          setUnavailable(
            error?.response?.data?.message || t('加载失败，请刷新重试'),
          );
        }
        return null;
      } finally {
        setLoading(false);
      }
    },
    [key, t],
  );

  useEffect(() => {
    reload();
  }, [reload]);

  // 把一次操作返回的新进度并进页面上的信息，例如余额与今日输赢。
  const patch = useCallback((fields) => {
    setInfo((previous) => (previous ? { ...previous, ...fields } : previous));
  }, []);

  return { loading, unavailable, info, setInfo, patch, reload };
}
