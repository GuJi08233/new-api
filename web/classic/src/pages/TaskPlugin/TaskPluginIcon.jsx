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

import React, { useEffect, useState } from 'react';
import { Avatar } from '@douyinfe/semi-ui';
import { API, getLobeHubIcon } from '../../helpers';

// 插件图标：优先使用已知的图片（市场拉取的 data URI 或已安装插件的附带图标），
// 否则按 meta.icon 渲染 LobeHub 图标或 text 方案的文字头像。
// 已安装图标需要带 New-Api-User 请求头，<img> 直接引用会被鉴权拒绝，因此经 API 取回后转为 blob 地址。
const TaskPluginIcon = ({
  pluginKey,
  name,
  icon,
  iconSrc,
  hasIcon,
  size = 32,
}) => {
  const [blobUrl, setBlobUrl] = useState('');

  useEffect(() => {
    if (iconSrc || !hasIcon || !pluginKey) return undefined;
    let objectUrl = '';
    let cancelled = false;
    API.get(`/api/plugin/task/${encodeURIComponent(pluginKey)}/icon`, {
      responseType: 'blob',
      skipErrorHandler: true,
    })
      .then((res) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(res.data);
        setBlobUrl(objectUrl);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [pluginKey, iconSrc, hasIcon]);

  const imageSrc = iconSrc || blobUrl;
  if (imageSrc) {
    return (
      <img
        src={imageSrc}
        alt={name || pluginKey}
        style={{ width: size, height: size, objectFit: 'contain' }}
        className='rounded-md shrink-0'
      />
    );
  }

  const trimmed = typeof icon === 'string' ? icon.trim() : '';
  if (trimmed && trimmed !== 'text' && !trimmed.startsWith('text:')) {
    return (
      <span
        className='inline-flex items-center justify-center shrink-0'
        style={{ width: size, height: size }}
      >
        {getLobeHubIcon(trimmed, size)}
      </span>
    );
  }
  const label = trimmed.startsWith('text:')
    ? trimmed.slice(5).slice(0, 3)
    : String(name || pluginKey || '?')
        .slice(0, 2)
        .toUpperCase();
  return (
    <Avatar
      shape='square'
      style={{ width: size, height: size, fontSize: size * 0.4 }}
    >
      {label}
    </Avatar>
  );
};

export default TaskPluginIcon;
