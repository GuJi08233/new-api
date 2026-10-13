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
import { API } from '../../helpers';

// 管理员名单在一次页面会话内只取一次:事件列表、处理弹窗与禁用账号列表都要把
// 负责人和操作人的 ID 显示成用户名。
let cached = null;
let pending = null;

const loadAssignees = () => {
  if (cached) return Promise.resolve(cached);
  if (!pending) {
    pending = API.get('/api/risk/assignees', { skipErrorHandler: true })
      .then((res) => {
        cached = res.data?.success ? res.data.data || [] : [];
        return cached;
      })
      .catch(() => [])
      .finally(() => {
        pending = null;
      });
  }
  return pending;
};

export default function useRiskAssignees() {
  const [list, setList] = useState(cached || []);
  useEffect(() => {
    let cancelled = false;
    loadAssignees().then((items) => {
      if (!cancelled) setList(items);
    });
    return () => {
      cancelled = true;
    };
  }, []);
  const byId = Object.fromEntries(list.map((item) => [item.id, item]));
  const nameOf = (id) => {
    if (!id) return '';
    const admin = byId[id];
    return admin ? admin.display_name || admin.username : `#${id}`;
  };
  return { list, byId, nameOf };
}
