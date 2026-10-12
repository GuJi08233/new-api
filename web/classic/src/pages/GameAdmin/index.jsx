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
import GameAdminSettings from './GameAdminSettings';
import GameAdminRules from './GameAdminRules';
import GameAdminWithdrawals from './GameAdminWithdrawals';
import GameAdminAccounts from './GameAdminAccounts';

const GameAdmin = () => {
  const { t } = useTranslation();

  return (
    <div className='mt-[60px] px-2'>
      <Tabs type='line' defaultActiveKey='settings'>
        <TabPane tab={t('基础设置')} itemKey='settings'>
          <div className='mt-4'>
            <GameAdminSettings />
          </div>
        </TabPane>
        <TabPane tab={t('游戏规则')} itemKey='rules'>
          <div className='mt-4'>
            <GameAdminRules />
          </div>
        </TabPane>
        <TabPane tab={t('提现记录')} itemKey='withdrawals'>
          <div className='mt-4'>
            <GameAdminWithdrawals />
          </div>
        </TabPane>
        <TabPane tab={t('游戏币账户')} itemKey='accounts'>
          <div className='mt-4'>
            <GameAdminAccounts />
          </div>
        </TabPane>
      </Tabs>
    </div>
  );
};

export default GameAdmin;
