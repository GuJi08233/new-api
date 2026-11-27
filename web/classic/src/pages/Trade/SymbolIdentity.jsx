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

import React from 'react';
import { Tag, Typography } from '@douyinfe/semi-ui';
import {
  extractTicker,
  getSymbolName,
  getSymbolSector,
  resolveSymbolMeta,
} from './symbols';

const { Text } = Typography;

// 品种头像徽章：按品种品牌色渲染首字/代号，用于行情列表、交易页头部、持仓和组合篮子。
export const SymbolAvatar = ({ item, size = 30 }) => {
  const meta = resolveSymbolMeta(item);
  const ticker = meta?.ticker || extractTicker(item);
  const badge = meta?.badge || ticker.slice(0, 2) || '?';
  const accent = meta?.accent || '#3b82f6';
  const fontSize =
    badge.length >= 3
      ? Math.max(9, Math.round(size * 0.31))
      : badge.length === 2
        ? Math.max(10, Math.round(size * 0.38))
        : Math.max(12, Math.round(size * 0.48));
  return (
    <span
      className='trade-symbol-avatar'
      style={{
        width: size,
        height: size,
        fontSize,
        color: accent,
        background: `${accent}18`,
        borderColor: `${accent}3a`,
      }}
      aria-hidden='true'
    >
      {badge}
    </span>
  );
};

// 品种直观标识：主标题显示中文或品牌全名(如英伟达、Anthropic、OpenAI、谷歌)，副标题展示代码与交易类型。
export const SymbolIdentity = ({
  item,
  futures = false,
  showSector = false,
  compact = false,
  size = 30,
  t,
}) => {
  const meta = resolveSymbolMeta(item);
  const ticker = meta?.ticker || extractTicker(item);
  const name = getSymbolName(item, t);
  const sector = showSector ? getSymbolSector(item) : null;
  const suffix = futures ? `USDT ${t ? t('永续') : '永续'}` : '/USDT';

  if (compact) {
    return (
      <span className='inline-flex items-center gap-1.5 min-w-0'>
        <SymbolAvatar item={item} size={size} />
        <span className='inline-flex items-baseline gap-1 min-w-0'>
          <Text strong className='truncate'>
            {name}
          </Text>
          {name.toUpperCase() !== ticker.toUpperCase() && (
            <Text type='tertiary' size='small' className='shrink-0'>
              {ticker}
            </Text>
          )}
        </span>
      </span>
    );
  }

  return (
    <div className='flex items-center gap-2.5 min-w-0'>
      <SymbolAvatar item={item} size={size} />
      <div className='flex flex-col min-w-0 leading-tight'>
        <div className='flex items-center gap-1.5 min-w-0'>
          <Text strong className='truncate text-sm'>
            {name}
          </Text>
          {sector && (
            <Tag
              size='small'
              color={sector.color}
              type='light'
              className='shrink-0 !text-[10px] !px-1.5 !leading-4'
            >
              {t ? t(sector.label) : sector.label}
            </Tag>
          )}
        </div>
        <Text type='tertiary' size='small' className='!text-[11px] truncate'>
          {ticker}
          {suffix}
        </Text>
      </div>
    </div>
  );
};

export default SymbolIdentity;
