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
import { Tooltip, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

// 一笔提现的链上交易，label 是显示的文字。只有交易所在的链(chainId)就是当前配置的链(currentChainId)时才链到区块浏览器：
// 管理员换过链之后，旧交易在新链的浏览器上查不到，只显示可复制的交易哈希。
export const TxHashLink = ({
  hash,
  label,
  chainId,
  currentChainId,
  explorerUrl,
}) => {
  const { t } = useTranslation();
  if (explorerUrl && Number(chainId) === Number(currentChainId)) {
    return (
      <a
        href={`${explorerUrl}/tx/${hash}`}
        target='_blank'
        rel='noopener noreferrer'
      >
        {label}
      </a>
    );
  }
  const text = <Text copyable={{ content: hash }}>{label}</Text>;
  if (!explorerUrl || !chainId) return text;
  return (
    <Tooltip
      content={t('这笔交易在链 {{chain}} 上，当前配置的区块浏览器查不到', {
        chain: chainId,
      })}
    >
      {text}
    </Tooltip>
  );
};
