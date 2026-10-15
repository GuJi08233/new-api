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

import React, { useCallback, useRef, useState } from 'react';
import Turnstile from 'react-turnstile';
import { useTranslation } from 'react-i18next';

// 领免费奖励的对局开始前的人机验证。config 是游戏信息里的 play_turnstile({ required, site_key })。
// 令牌只能用一次：take() 取出当前令牌并换一个新的验证组件重新取。widget 平时不可见，Cloudflare 需要玩家操作时才展开，
// 放在开局按钮下方。
export function usePlayTurnstile(config) {
  const required = !!config?.required;
  const scope = `${required ? 'required' : 'optional'}:${config?.site_key || ''}`;
  const [verification, setVerification] = useState({
    scope,
    generation: 0,
    token: '',
    failed: false,
  });
  let current = verification;
  if (verification.scope !== scope) {
    // 配置变了就换一份验证状态，豁免后不留下错误，重新要求验证或更换 site key 时也不能沿用旧令牌。
    current = {
      scope,
      generation: verification.generation + 1,
      token: '',
      failed: false,
    };
    setVerification(current);
  }
  const verificationRef = useRef(current);
  verificationRef.current = current;
  const generation = current.generation;
  const updateVerification = useCallback(
    (changes) => {
      setVerification((previous) =>
        previous.scope === scope && previous.generation === generation
          ? { ...previous, ...changes }
          : previous,
      );
    },
    [scope, generation],
  );

  const take = useCallback(() => {
    if (!required) return '';
    const previous = verificationRef.current;
    const next = {
      scope,
      generation: previous.generation + 1,
      token: '',
      failed: false,
    };
    verificationRef.current = next;
    setVerification(next);
    return previous.token;
  }, [required, scope]);

  const widget = required ? (
    <div className='game-turnstile'>
      <Turnstile
        key={`${scope}:${generation}`}
        sitekey={config.site_key}
        appearance='interaction-only'
        onVerify={(value) => {
          updateVerification({ token: value, failed: false });
        }}
        onExpire={() => updateVerification({ token: '' })}
        onError={() => {
          updateVerification({ token: '', failed: true });
        }}
      />
    </div>
  ) : null;

  return {
    required,
    ready: !required || !!current.token,
    failed: required && current.failed,
    take,
    widget,
  };
}

// PlayTurnstile 放在开局按钮下方：usePlayTurnstile 的验证组件，加载失败时提示刷新页面。
export const PlayTurnstile = ({ turnstile }) => {
  const { t } = useTranslation();
  return (
    <>
      {turnstile.widget}
      {turnstile.failed && (
        <div className='games-stage-hint'>
          {t('人机验证加载失败，请刷新页面重试')}
        </div>
      )}
    </>
  );
};
