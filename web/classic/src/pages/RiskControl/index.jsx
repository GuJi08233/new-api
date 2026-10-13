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
import { useSearchParams } from 'react-router-dom';
import { Tabs, TabPane } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { isRoot } from '../../helpers';
import RiskRankings from './RiskRankings';
import MultiAccount from './MultiAccount';
import RiskEvents from './RiskEvents';
import IpBans from './IpBans';
import DisabledUsers from './DisabledUsers';
import RiskSettings from './RiskSettings';
import RiskSubjectDrawer from './RiskSubjectDrawer';
import RiskDispositionModal from './RiskDispositionModal';
import RiskOverview from './RiskOverview';
import RiskBatchModal from './RiskBatchModal';

const SUBJECT_KEYS = [
  'subject_type',
  'subject_value',
  'detail_hours',
  'exclude_whitelist',
];

const RiskControl = () => {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const [disposition, setDisposition] = useState(null);
  const [batch, setBatch] = useState(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const tabs = [
    'overview',
    'events',
    'dispositions',
    'rankings',
    'multi_account',
    ...(isRoot() ? ['settings'] : []),
  ];
  // 旧地址里的 tab=ip_bans 并入「处置管理」的 IP 封禁视图。
  const legacyIpBans = params.get('tab') === 'ip_bans';
  const activeTab = legacyIpBans
    ? 'dispositions'
    : tabs.includes(params.get('tab'))
      ? params.get('tab')
      : 'overview';
  const dispositionView =
    legacyIpBans || params.get('d_view') === 'ip' ? 'ip' : 'users';
  const subject =
    ['user', 'ip', 'ua'].includes(params.get('subject_type')) &&
    params.get('subject_value')
      ? {
          type: params.get('subject_type'),
          value: params.get('subject_value'),
          hours: Number(params.get('detail_hours')) || 24,
          excludeWhitelist: params.get('exclude_whitelist') === 'true',
        }
      : null;
  const closeDetail = () => {
    const next = new URLSearchParams(params);
    for (const key of SUBJECT_KEYS) next.delete(key);
    setParams(next);
  };
  const inspect = (target) => {
    const next = new URLSearchParams(params);
    next.set('subject_type', target.type);
    next.set('subject_value', target.value);
    next.set('detail_hours', String(target.hours || 24));
    next.set('exclude_whitelist', String(!!target.excludeWhitelist));
    setParams(next);
  };
  // 切到某个标签并带上该页的筛选参数(参数名带各页的前缀,如 d_view、b_status、s_section)。
  const navigate = (tab, patch = {}) => {
    const next = new URLSearchParams(params);
    for (const key of SUBJECT_KEYS) next.delete(key);
    next.set('tab', tab);
    for (const [key, value] of Object.entries(patch)) {
      if (value === '' || value === null || value === undefined)
        next.delete(key);
      else next.set(key, String(value));
    }
    setParams(next);
  };
  const viewEvents = (filters) => {
    const next = new URLSearchParams(params);
    for (const key of [...SUBJECT_KEYS, 'user_id', 'ip', 'ua'])
      next.delete(key);
    next.set('tab', 'events');
    for (const key of [...next.keys()]) {
      if (key.startsWith('e_')) next.delete(key);
    }
    for (const [key, value] of Object.entries(filters))
      next.set(`e_${key}`, String(value));
    if (filters.hours)
      next.set(
        'e_start_timestamp',
        String(Math.floor(Date.now() / 1000) - filters.hours * 3600),
      );
    setParams(next);
  };
  const shared = {
    onInspect: inspect,
    onDisposition: setDisposition,
    onBatch: setBatch,
    refreshKey,
  };

  return (
    <div className='mt-[60px] px-2'>
      <Tabs
        type='line'
        activeKey={activeTab}
        keepDOM={false}
        onChange={(key) => navigate(key)}
      >
        <TabPane tab={t('风控概览')} itemKey='overview'>
          <div className='mt-4'>
            <RiskOverview
              refreshKey={refreshKey}
              onViewEvents={viewEvents}
              onNavigate={navigate}
              onInspect={inspect}
            />
          </div>
        </TabPane>
        <TabPane tab={t('风控事件')} itemKey='events'>
          <div className='mt-4'>
            <RiskEvents onInspect={inspect} refreshKey={refreshKey} />
          </div>
        </TabPane>
        <TabPane tab={t('处置管理')} itemKey='dispositions'>
          <div className='mt-4'>
            <Tabs
              type='button'
              className='mb-3'
              activeKey={dispositionView}
              onChange={(key) => navigate('dispositions', { d_view: key })}
              tabList={[
                { tab: t('已禁用账号'), itemKey: 'users' },
                { tab: t('IP 封禁'), itemKey: 'ip' },
              ]}
            />
            {dispositionView === 'ip' ? (
              <IpBans {...shared} />
            ) : (
              <DisabledUsers {...shared} onViewEvents={viewEvents} />
            )}
          </div>
        </TabPane>
        <TabPane tab={t('滥用排行榜')} itemKey='rankings'>
          <div className='mt-4'>
            <RiskRankings {...shared} />
          </div>
        </TabPane>
        <TabPane tab={t('多账号关联')} itemKey='multi_account'>
          <div className='mt-4'>
            <MultiAccount {...shared} />
          </div>
        </TabPane>
        {isRoot() && (
          <TabPane tab={t('风控设置')} itemKey='settings'>
            <div className='mt-4'>
              <RiskSettings
                onInspect={inspect}
                onViewEvents={viewEvents}
                onSaved={() => setRefreshKey((n) => n + 1)}
              />
            </div>
          </TabPane>
        )}
      </Tabs>
      {subject && (
        <RiskSubjectDrawer
          subject={subject}
          onClose={closeDetail}
          onInspect={inspect}
          onDisposition={setDisposition}
          onViewEvents={viewEvents}
          refreshKey={refreshKey}
        />
      )}
      {disposition && (
        <RiskDispositionModal
          target={disposition}
          onCancel={() => setDisposition(null)}
          onSuccess={() => {
            setDisposition(null);
            setRefreshKey((n) => n + 1);
          }}
        />
      )}
      {batch && (
        <RiskBatchModal
          batch={batch}
          onClose={() => setBatch(null)}
          onApplied={() => setRefreshKey((n) => n + 1)}
        />
      )}
    </div>
  );
};

export default RiskControl;
