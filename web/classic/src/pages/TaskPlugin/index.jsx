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

import React, { useState } from 'react';
import { Tabs, TabPane } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import TaskPluginInstalled from './TaskPluginInstalled';
import TaskPluginMarketplace from './TaskPluginMarketplace';

const TaskPlugin = () => {
  const { t } = useTranslation();
  // 任一标签页安装/停用插件后递增，让另一个标签页刷新已安装列表
  const [revision, setRevision] = useState(0);
  const bump = () => setRevision((value) => value + 1);

  return (
    <div className='mt-[60px] px-2'>
      <Tabs type='line' defaultActiveKey='installed'>
        <TabPane tab={t('已安装插件')} itemKey='installed'>
          <div className='mt-4'>
            <TaskPluginInstalled refreshKey={revision} onChanged={bump} />
          </div>
        </TabPane>
        <TabPane tab={t('插件市场')} itemKey='marketplace'>
          <div className='mt-4'>
            <TaskPluginMarketplace refreshKey={revision} onChanged={bump} />
          </div>
        </TabPane>
      </Tabs>
    </div>
  );
};

export default TaskPlugin;
