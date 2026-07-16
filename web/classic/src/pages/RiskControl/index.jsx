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
import { Tabs, TabPane } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { isRoot } from '../../helpers';
import RiskRankings from './RiskRankings';
import RiskSettings from './RiskSettings';

const RiskControl = () => {
  const { t } = useTranslation();

  return (
    <div className='mt-[60px] px-2'>
      <Tabs type='line' defaultActiveKey='rankings'>
        <TabPane tab={t('滥用排行榜')} itemKey='rankings'>
          <div className='mt-4'>
            <RiskRankings />
          </div>
        </TabPane>
        {isRoot() && (
          <TabPane tab={t('风控设置')} itemKey='settings'>
            <div className='mt-4'>
              <RiskSettings />
            </div>
          </TabPane>
        )}
      </Tabs>
    </div>
  );
};

export default RiskControl;
