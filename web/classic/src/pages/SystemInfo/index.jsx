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
import { TabPane, Tabs, Tag, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import SystemInstances from './SystemInstances';
import SystemTasks from './SystemTasks';

const SystemInfo = () => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const defaultTab = searchParams.get('task_id') ? 'tasks' : 'instances';

  return (
    <div className='mt-[60px] px-2'>
      <div className='mb-3 flex items-center gap-2'>
        <Typography.Title heading={4} style={{ margin: 0 }}>
          {t('系统信息')}
        </Typography.Title>
        <Tag color='blue' shape='circle'>
          Root
        </Tag>
      </div>
      <Tabs type='line' defaultActiveKey={defaultTab} keepDOM={false}>
        <TabPane tab={t('系统实例')} itemKey='instances'>
          <div className='mt-4'>
            <SystemInstances />
          </div>
        </TabPane>
        <TabPane tab={t('系统任务')} itemKey='tasks'>
          <div className='mt-4'>
            <SystemTasks />
          </div>
        </TabPane>
      </Tabs>
    </div>
  );
};

export default SystemInfo;
