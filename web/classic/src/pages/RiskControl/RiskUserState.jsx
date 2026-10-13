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
import { Space, Tag, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { timestamp2string } from '../../helpers';

const RiskUserState = ({ state }) => {
  const { t } = useTranslation();
  if (!state)
    return <Typography.Text type='tertiary'>{t('状态未知')}</Typography.Text>;
  return (
    <Space wrap spacing={4}>
      <Tag color={state.status === 2 ? 'red' : 'green'}>
        {state.status === 2 ? t('已禁用') : t('已启用')}
      </Tag>
      {state.status === 2 && (
        <Tag>
          {state.disable_expires_at
            ? timestamp2string(state.disable_expires_at)
            : t('永久')}
        </Tag>
      )}
      {state.whitelisted && <Tag color='blue'>{t('全局白名单')}</Tag>}
      {!state.whitelisted && state.account_exempt && (
        <Tag color='blue'>{t('仅保护账号')}</Tag>
      )}
    </Space>
  );
};

export default RiskUserState;
