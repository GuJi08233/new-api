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
import { Button, Input, Space } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { parseSubscriptionPeriodDuration } from '../../../helpers/subscriptionDuration';

const SubscriptionPeriodDurationInput = ({ value, onChange, disabled }) => {
  const { t } = useTranslation();
  const seconds = parseSubscriptionPeriodDuration(value);
  const invalid = Boolean(value?.trim()) && seconds === null;

  return (
    <div className='space-y-2'>
      <Input
        value={value}
        onChange={onChange}
        disabled={disabled}
        placeholder={t('例如：5h、1d、1h30m')}
        validateStatus={invalid ? 'error' : 'default'}
      />
      <Space spacing={4} wrap>
        {['1h', '5h', '1d', '7d'].map((duration) => (
          <Button
            key={duration}
            size='small'
            theme='light'
            disabled={disabled}
            onClick={() => onChange(duration)}
          >
            {duration}
          </Button>
        ))}
      </Space>
      <div className='text-xs text-gray-500'>
        {seconds !== null ? (
          t('已解析时长：{{seconds}} 秒', { seconds })
        ) : invalid ? (
          <span className='text-red-500'>
            {t('请输入有效时长，例如 5h、1d 或 1h30m')}
          </span>
        ) : (
          t('支持 s、m、h、d、w，可组合，例如 1h30m。')
        )}
      </div>
      <div className='text-xs text-gray-500'>
        {t('1d 表示固定 24 小时，与按自然日重置不同。')}
      </div>
    </div>
  );
};

export default SubscriptionPeriodDurationInput;
