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
import { Button, Typography, Tag } from '@douyinfe/semi-ui';
import { IconChevronUp, IconChevronDown } from '@douyinfe/semi-icons';
import { GripVertical } from 'lucide-react';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

function parseConfiguredOrder(str) {
  if (!str || !str.trim()) return [];
  try {
    const parsed = JSON.parse(str);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((item) => typeof item === 'string');
  } catch {
    return [];
  }
}

// 与后端 GetAutoGroups 一致：序列始终覆盖全部现存分组，配置只决定顺序——
// 配置顺序在前，未配置的分组按名称追加，配置里已不存在的分组名剔除，auto 自身除外
function buildOrderedGroups(configured, groupNames) {
  const available = groupNames.filter((n) => n && n !== 'auto');
  const availableSet = new Set(available);
  const ordered = [];
  const seen = new Set();
  for (const name of configured) {
    if (seen.has(name) || !availableSet.has(name)) continue;
    seen.add(name);
    ordered.push(name);
  }
  const missing = available.filter((n) => !seen.has(n)).sort();
  return [...ordered, ...missing];
}

export default function AutoGroupList({ value, groupNames = [], onChange }) {
  const { t } = useTranslation();

  const [items, setItems] = useState(() =>
    buildOrderedGroups(parseConfiguredOrder(value), groupNames),
  );
  const [dragIndex, setDragIndex] = useState(null);
  const [overIndex, setOverIndex] = useState(null);

  // GroupRatio 中新增/删除分组时同步列表，保留已调整的顺序
  useEffect(() => {
    setItems((prev) =>
      buildOrderedGroups(
        prev.length > 0 ? prev : parseConfiguredOrder(value),
        groupNames,
      ),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groupNames]);

  const commit = (next) => {
    setItems(next);
    onChange?.(next.length === 0 ? '' : JSON.stringify(next));
  };

  const move = (index, offset) => {
    const target = index + offset;
    if (target < 0 || target >= items.length) return;
    const next = [...items];
    [next[index], next[target]] = [next[target], next[index]];
    commit(next);
  };

  const resetDragState = () => {
    setDragIndex(null);
    setOverIndex(null);
  };

  const handleDrop = (targetIndex) => {
    if (dragIndex === null || dragIndex === targetIndex) {
      resetDragState();
      return;
    }
    const next = [...items];
    const [moved] = next.splice(dragIndex, 1);
    next.splice(targetIndex, 0, moved);
    commit(next);
    resetDragState();
  };

  if (items.length === 0) {
    return (
      <Text type='tertiary' className='block text-center py-4'>
        {t('暂无分组，请先在分组倍率中配置分组')}
      </Text>
    );
  }

  return (
    <div className='space-y-1'>
      {items.map((name, index) => {
        const isDragging = dragIndex === index;
        const isDropTarget =
          dragIndex !== null && dragIndex !== index && overIndex === index;
        // 指示线跟随实际插入位置：向下拖时元素落在目标行之后，向上拖时落在之前
        const indicator = isDropTarget
          ? dragIndex < index
            ? { borderBottom: '2px solid var(--semi-color-primary)' }
            : { borderTop: '2px solid var(--semi-color-primary)' }
          : {};
        return (
          <div
            key={name}
            draggable
            onDragStart={(e) => {
              setDragIndex(index);
              e.dataTransfer.effectAllowed = 'move';
            }}
            onDragOver={(e) => {
              e.preventDefault();
              e.dataTransfer.dropEffect = 'move';
              if (overIndex !== index) setOverIndex(index);
            }}
            onDrop={(e) => {
              e.preventDefault();
              handleDrop(index);
            }}
            onDragEnd={resetDragState}
            className='flex items-center gap-2 rounded-md px-1 py-0.5'
            style={{
              cursor: 'grab',
              opacity: isDragging ? 0.4 : 1,
              borderTop: '2px solid transparent',
              borderBottom: '2px solid transparent',
              ...indicator,
            }}
          >
            <GripVertical
              size={14}
              className='shrink-0'
              style={{ color: 'var(--semi-color-text-2)' }}
            />
            <Tag size='small' color='blue' className='shrink-0'>
              {index + 1}
            </Tag>
            <Text className='flex-1 select-none'>{name}</Text>
            <Button
              icon={<IconChevronUp />}
              theme='borderless'
              size='small'
              disabled={index === 0}
              onClick={() => move(index, -1)}
            />
            <Button
              icon={<IconChevronDown />}
              theme='borderless'
              size='small'
              disabled={index === items.length - 1}
              onClick={() => move(index, 1)}
            />
          </div>
        );
      })}
      <Text type='tertiary' size='small' className='block mt-2'>
        {t('拖拽行即可调整优先级顺序')}
      </Text>
    </div>
  );
}
