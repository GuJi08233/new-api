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

import React, { useCallback, useState } from 'react';
import Turnstile from 'react-turnstile';

// 领免费奖励的对局开始前的人机验证。config 是游戏信息里的 play_turnstile({ required, site_key })。
// 令牌只能用一次：take() 取出当前令牌并换一个新的验证组件重新取。widget 平时不可见，Cloudflare 需要玩家操作时才展开，
// 放在开局按钮下方。
export function usePlayTurnstile(config) {
  const required = !!config?.required;
  const [token, setToken] = useState('');
  const [key, setKey] = useState(0);
  const [failed, setFailed] = useState(false);

  const take = useCallback(() => {
    if (!required) return '';
    const value = token;
    setToken('');
    setKey((previous) => previous + 1);
    return value;
  }, [required, token]);

  const widget = required ? (
    <div className='game-turnstile'>
      <Turnstile
        key={key}
        sitekey={config.site_key}
        appearance='interaction-only'
        onVerify={(value) => {
          setToken(value);
          setFailed(false);
        }}
        onExpire={() => setToken('')}
        onError={() => {
          setToken('');
          setFailed(true);
        }}
      />
    </div>
  ) : null;

  return { required, ready: !required || !!token, failed, take, widget };
}
