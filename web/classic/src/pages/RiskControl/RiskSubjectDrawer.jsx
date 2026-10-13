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
import {
  Banner,
  Button,
  SideSheet,
  Space,
  Spin,
  Table,
  Tabs,
  TabPane,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';
import RiskUserState from './RiskUserState';
import RiskEvents from './RiskEvents';
import { riskSourceLabel } from './riskLabels';

const RiskSubjectDrawer = ({
  subject,
  onClose,
  onInspect,
  onDisposition,
  onViewEvents,
  refreshKey,
}) => {
  const { t } = useTranslation();
  const [state, setState] = useState(null);
  const [detail, setDetail] = useState({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [tab, setTab] = useState('items');
  const [retry, setRetry] = useState(0);
  const type = subject.type;
  const value = String(subject.value);
  const hours =
    Number.isInteger(subject.hours) && subject.hours > 0
      ? Math.min(subject.hours, 168)
      : 24;
  const isNetwork = type === 'ip' && value.includes('/');
  const excludeWhitelist = !!subject.excludeWhitelist;

  useEffect(() => {
    setTab(isNetwork ? 'events' : 'items');
  }, [type, value, isNetwork]);
  useEffect(() => {
    const controller = new AbortController();
    const params = new URLSearchParams({ type, value, hours: String(hours) });
    if (excludeWhitelist) params.set('exclude_whitelist', 'true');
    setLoading(true);
    setState(null);
    setDetail({});
    setError('');
    Promise.allSettled([
      API.get(`/api/risk/state?${params}`, {
        signal: controller.signal,
        skipErrorHandler: true,
      }),
      isNetwork
        ? Promise.resolve({ data: { success: true, data: {} } })
        : API.get(`/api/risk/detail?${params}`, {
            signal: controller.signal,
            skipErrorHandler: true,
          }),
    ])
      .then(([stateResult, detailResult]) => {
        if (controller.signal.aborted) return;
        if (detailResult.status === 'rejected') throw detailResult.reason;
        if (!detailResult.value.data.success)
          throw new Error(detailResult.value.data.message);
        setDetail(detailResult.value.data.data || {});
        if (
          stateResult.status === 'fulfilled' &&
          stateResult.value.data.success
        ) {
          setState(stateResult.value.data.data);
        } else {
          const failure =
            stateResult.status === 'rejected'
              ? stateResult.reason
              : new Error(stateResult.value.data.message);
          setError(failure.response?.data?.message || failure.message);
        }
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setError(e.response?.data?.message || e.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [type, value, hours, excludeWhitelist, refreshKey, retry, isNetwork]);

  const inspect = (subjectType, subjectValue) =>
    onInspect({
      type: subjectType,
      value: String(subjectValue),
      hours,
      excludeWhitelist,
    });
  const relatedColumns = [
    type === 'user'
      ? {
          title: 'IP',
          dataIndex: 'ip',
          render: (ip) => (
            <Button theme='borderless' onClick={() => inspect('ip', ip)}>
              {ip}
            </Button>
          ),
        }
      : {
          title: t('用户'),
          dataIndex: 'username',
          render: (name, row) => (
            <Button
              theme='borderless'
              onClick={() => inspect('user', row.user_id)}
            >
              {name || '-'} (#{row.user_id})
            </Button>
          ),
        },
    { title: t('请求数'), dataIndex: 'request_count' },
    {
      title: t('首次'),
      dataIndex: 'first_seen',
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
    {
      title: t('最近'),
      dataIndex: 'last_seen',
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
  ];
  const ipColumns = [
    {
      title: 'IP',
      dataIndex: 'ip',
      render: (ip) => (
        <Button theme='borderless' onClick={() => inspect('ip', ip)}>
          {ip}
        </Button>
      ),
    },
    { title: t('请求数'), dataIndex: 'request_count' },
  ];
  const uaColumns = [
    {
      title: 'User-Agent',
      dataIndex: 'ua',
      ellipsis: { showTitle: true },
      render: (ua) => (
        <Button theme='borderless' onClick={() => inspect('ua', ua)}>
          {ua}
        </Button>
      ),
    },
    { title: t('请求数'), dataIndex: 'request_count' },
  ];
  const banColumns = [
    { title: t('封禁目标'), dataIndex: 'target' },
    {
      title: t('到期时间'),
      dataIndex: 'expires_at',
      render: (v) => (v ? timestamp2string(v) : t('永久')),
    },
    {
      title: t('来源'),
      dataIndex: 'source',
      render: (v) => riskSourceLabel(v, t),
    },
    { title: t('原因'), dataIndex: 'reason', ellipsis: { showTitle: true } },
    {
      title: t('操作'),
      render: (_, ban) => (
        <Button
          onClick={() =>
            onDisposition({
              type: 'ip',
              value: ban.target,
              action: 'unban_ip',
              banId: ban.id,
            })
          }
        >
          {t('解除封禁')}
        </Button>
      ),
    },
  ];
  const eventFilters =
    type === 'user'
      ? { user_id: value }
      : type === 'ip'
        ? { ip: value }
        : { ua: value };

  return (
    <SideSheet
      title={t('风控详情')}
      visible
      width={960}
      style={{ maxWidth: '100vw' }}
      onCancel={onClose}
    >
      <Space vertical align='start' className='w-full mb-4'>
        <Typography.Text strong className='break-all'>
          {state?.user ? `${state.user.username} (#${state.user.id})` : value}
        </Typography.Text>
        <Space wrap>
          {state?.user && <RiskUserState state={state.user} />}
          {state?.user?.can_disable && (
            <Button
              type='danger'
              onClick={() =>
                onDisposition({
                  type: 'user',
                  value,
                  label: state.user.username,
                  action: 'disable',
                })
              }
            >
              {state.user.status === 2 ? t('转为永久禁用') : t('禁用用户')}
            </Button>
          )}
          {state?.user?.can_enable && (
            <Button
              onClick={() =>
                onDisposition({
                  type: 'user',
                  value,
                  label: state.user.username,
                  action: 'enable',
                })
              }
            >
              {t('启用用户')}
            </Button>
          )}
          {type === 'ip' && state && (
            <Button
              type='danger'
              disabled={state.is_current_ip}
              onClick={() =>
                onDisposition({ type: 'ip', value, action: 'ban_ip' })
              }
            >
              {t('封禁 IP')}
            </Button>
          )}
          <Button onClick={() => onViewEvents(eventFilters)}>
            {t('查看全部事件')}
          </Button>
        </Space>
        {state?.user?.disable_reason && (
          <Typography.Text>
            {t('禁用原因')}: {state.user.disable_reason}
          </Typography.Text>
        )}
        {state?.blacklist_blocked && (
          <Banner
            type='warning'
            description={
              <>
                {t('静态黑名单仍在生效，解除动态封禁后仍会被拦截。')}{' '}
                <Typography.Text code>{state.blacklist_rule}</Typography.Text>
              </>
            }
          />
        )}
      </Space>
      {error && (
        <Banner
          type='danger'
          className='mb-4'
          description={
            <Space>
              {error}
              <Button onClick={() => setRetry((n) => n + 1)}>
                {t('重试')}
              </Button>
            </Space>
          }
        />
      )}
      <Spin spinning={loading}>
        {type === 'ip' && state && (
          <>
            <Typography.Title heading={6}>{t('当前生效封禁')}</Typography.Title>
            <Table
              columns={banColumns}
              dataSource={state.ip_bans || []}
              rowKey='id'
              pagination={false}
              empty={t('暂无数据')}
              className='mb-4'
            />
          </>
        )}
        <Tabs activeKey={tab} onChange={setTab} keepDOM={false}>
          {!isNetwork && (
            <TabPane
              tab={type === 'user' ? t('IP 明细') : t('关联用户')}
              itemKey='items'
            >
              <Table
                columns={relatedColumns}
                dataSource={detail.items || []}
                rowKey={(row) => row.user_id || row.ip}
                pagination={{ pageSize: 10 }}
                empty={t('暂无数据')}
              />
            </TabPane>
          )}
          {!isNetwork && (
            <TabPane
              tab={type === 'ua' ? t('IP 明细') : t('UA 明细')}
              itemKey='extra'
            >
              <Table
                columns={type === 'ua' ? ipColumns : uaColumns}
                dataSource={type === 'ua' ? detail.ips || [] : detail.uas || []}
                rowKey={(row) => row.ip || row.ua}
                pagination={{ pageSize: 10 }}
                empty={t('暂无数据')}
              />
            </TabPane>
          )}
          {!isNetwork && (
            <TabPane tab={t('错误状态码')} itemKey='errors'>
              {detail.errors_sampled && (
                <Banner
                  type='info'
                  description={t(
                    '错误数量较多,分布基于最近的错误样本统计,不代表窗口内全量。',
                  )}
                />
              )}
              <Table
                columns={[
                  { title: t('状态码'), dataIndex: 'status_code' },
                  { title: t('错误代码'), dataIndex: 'error_code' },
                  { title: t('次数'), dataIndex: 'count' },
                ]}
                dataSource={detail.errors || []}
                rowKey={(row) => `${row.status_code}:${row.error_code}`}
                pagination={{ pageSize: 10 }}
                empty={t('暂无数据')}
              />
            </TabPane>
          )}
          <TabPane tab={t('风控事件')} itemKey='events'>
            <RiskEvents
              filters={eventFilters}
              hideFilters
              onInspect={onInspect}
              refreshKey={refreshKey}
            />
          </TabPane>
        </Tabs>
        <Typography.Text type='tertiary' className='block mt-3'>
          {isNetwork
            ? t('网段详情展示封禁记录；调用证据请查看具体 IP。')
            : t('调用证据统计近 {{hours}} 小时，事件记录可查看全部历史。', {
                hours,
              })}
        </Typography.Text>
      </Spin>
    </SideSheet>
  );
};

export default RiskSubjectDrawer;
