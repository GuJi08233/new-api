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

import React, { useEffect } from 'react';
import { useTokenKeys } from '../../hooks/chat/useTokenKeys';

const chat2page = () => {
  const { tokenKey, serverAddress, isLoading } = useTokenKeys();

  // 跳转是副作用，必须放进 useEffect：
  // 直接写在渲染函数体里会在 React 严格模式的重复渲染中被执行多次。
  useEffect(() => {
    if (isLoading || !tokenKey || !serverAddress) return;

    let chatLink = '';
    try {
      const status = JSON.parse(localStorage.getItem('status') || '{}');
      chatLink = status?.chat_link || '';
    } catch (e) {
      console.error('Failed to parse status from localStorage:', e);
    }
    if (!chatLink) return;

    window.location.href = `${chatLink}/#/?settings={"key":"sk-${tokenKey}","url":"${encodeURIComponent(serverAddress)}"}`;
  }, [isLoading, tokenKey, serverAddress]);

  return (
    <div className='mt-[60px] px-2'>
      <h3>正在加载，请稍候...</h3>
    </div>
  );
};

export default chat2page;
