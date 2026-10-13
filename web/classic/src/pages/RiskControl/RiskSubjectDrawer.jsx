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
  Radio,
  RadioGroup,
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
import { RiskTimelineChart } from './RiskTrendChart';
import { riskRemainingTime, riskSourceLabel } from './riskLabels';

const { Text } = Typography;

const HOUR_OPTIONS = [1, 24, 72, 168];

// 明细列表最多返回 500 条,达到上限时数量显示为 500+。
const DETAIL_LIMIT = 500;

const hoursLabel = (hours, t) => {
  if (hours === 1) return t('近 1 小时');
  if (hours === 24) return t('近 24 小时');
  if (hours % 24 === 0) return t('近 {{days}} 天', { days: hours / 24 });
  return t('近 {{hours}} 小时', { hours });
};

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

  const inspect = (subjectType, subjectValue, windowHours = hours) =>
    onInspect({
      type: subjectType,
      value: String(subjectValue),
      hours: windowHours,
      excludeWhitelist,
    });
  const timeline = detail.timeline || [];
  const requests = timeline.reduce((sum, bucket) => sum + bucket.requests, 0);
  const errors = timeline.reduce((sum, bucket) => sum + bucket.errors, 0);
  const related = detail.items || [];
  const extra = (type === 'ua' ? detail.ips : detail.uas) || [];
  const countText = (list) =>
    list.length >= DETAIL_LIMIT ? `${DETAIL_LIMIT}+` : String(list.length);
  const stats = [
    { label: t('调用次数'), value: requests.toLocaleString() },
    {
      label: t('错误调用'),
      value: errors.toLocaleString(),
      hint: requests
        ? t('错误率 {{rate}}%', {
            rate: ((errors / requests) * 100).toFixed(1),
          })
        : '',
      danger: requests > 0 && errors / requests >= 0.5,
    },
    {
      label: type === 'user' ? t('使用 IP 数') : t('关联用户数'),
      value: countText(related),
    },
    {
      label: type === 'ua' ? t('使用 IP 数') : t('UA 数'),
      value: countText(extra),
    },
  ];
  const hourOptions = HOUR_OPTIONS.includes(hours)
    ? HOUR_OPTIONS
    : [...HOUR_OPTIONS, hours].sort((a, b) => a - b);
  const activeBans = state?.ip_bans || [];
  // 同一目标的封禁只能延长:已是永久时不再提供封禁按钮。
  const exactBan = activeBans.find(
    (ban) => ban.target === (state?.value || value),
  );
  const typeLabel = isNetwork
    ? t('网段')
    : { user: t('用户'), ip: 'IP', ua: 'User-Agent' }[type];

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
    { title: t('请求数'), dataIndex: 'request_count', width: 100 },
    {
      title: t('首次'),
      dataIndex: 'first_seen',
      width: 170,
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
    {
      title: t('最近'),
      dataIndex: 'last_seen',
      width: 170,
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
  ];
  const extraColumns = [
    type === 'ua'
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
          title: 'User-Agent',
          dataIndex: 'ua',
          ellipsis: { showTitle: true },
          render: (ua) => (
            <Button theme='borderless' onClick={() => inspect('ua', ua)}>
              {ua}
            </Button>
          ),
        },
    { title: t('请求数'), dataIndex: 'request_count', width: 100 },
    {
      title: t('最近'),
      dataIndex: 'last_seen',
      width: 170,
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
  ];
  const banColumns = [
    { title: t('封禁目标'), dataIndex: 'target', width: 200 },
    {
      title: t('期限'),
      dataIndex: 'expires_at',
      width: 200,
      render: (v) =>
        v ? (
          <div className='flex flex-col items-start gap-1'>
            <Tag color='orange'>
              {t('剩余 {{time}}', { time: riskRemainingTime(v) })}
            </Tag>
            <Text type='tertiary' size='small'>
              {timestamp2string(v)}
            </Text>
          </div>
        ) : (
          <Tag color='red'>{t('永久')}</Tag>
        ),
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
      <div className='flex flex-col gap-3 mb-4'>
        <div className='flex flex-wrap items-center gap-2'>
          <Tag color='blue'>{typeLabel}</Tag>
          <Text strong className='break-all' copyable={{ content: value }}>
            {state?.user ? `${state.user.username} (#${state.user.id})` : value}
          </Text>
        </div>
        <Space wrap spacing={4}>
          {state?.user && <RiskUserState state={state.user} />}
          {type === 'ip' && state && activeBans.length > 0 && (
            <Tag color='red'>{t('封禁中')}</Tag>
          )}
          {type === 'ip' && state && !activeBans.length && (
            <Tag color='green'>{t('未封禁')}</Tag>
          )}
          {state?.blacklist_blocked && (
            <Tag color='red'>{t('静态黑名单命中')}</Tag>
          )}
          {state?.is_current_ip && <Tag color='blue'>{t('当前登录 IP')}</Tag>}
        </Space>
        <Space wrap>
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
          {type === 'ip' && state && exactBan?.expires_at !== 0 && (
            <Button
              type='danger'
              disabled={state.is_current_ip}
              onClick={() =>
                onDisposition({ type: 'ip', value, action: 'ban_ip' })
              }
            >
              {exactBan ? t('延长封禁') : t('封禁 IP')}
            </Button>
          )}
          <Button onClick={() => onViewEvents(eventFilters)}>
            {t('在事件页查看')}
          </Button>
        </Space>
        {state?.user?.disable_reason && (
          <Text>
            {t('禁用原因')}: {state.user.disable_reason}
          </Text>
        )}
        {state?.blacklist_blocked && (
          <Banner
            type='warning'
            description={
              <>
                {type === 'ip'
                  ? t('静态黑名单仍在生效，解除动态封禁后仍会被拦截。')
                  : t('此 UA 命中静态黑名单，请求会被直接拦截。')}{' '}
                <Text code>{state.blacklist_rule}</Text>
              </>
            }
          />
        )}
      </div>
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
        {!isNetwork && (
          <div className='mb-4'>
            <div className='flex flex-wrap items-center justify-between gap-2 mb-3'>
              <RadioGroup
                type='button'
                value={hours}
                onChange={(event) => inspect(type, value, event.target.value)}
                aria-label={t('统计窗口')}
              >
                {hourOptions.map((option) => (
                  <Radio key={option} value={option}>
                    {hoursLabel(option, t)}
                  </Radio>
                ))}
              </RadioGroup>
              {excludeWhitelist && (
                <Tag color='grey'>{t('已排除全局白名单用户')}</Tag>
              )}
            </div>
            <div className='grid grid-cols-2 md:grid-cols-4 gap-3 mb-3'>
              {stats.map((stat) => (
                <div
                  key={stat.label}
                  className='rounded-lg p-3 bg-[var(--semi-color-fill-0)]'
                >
                  <Text type='tertiary' size='small' className='block'>
                    {stat.label}
                  </Text>
                  <Text
                    strong
                    type={stat.danger ? 'danger' : undefined}
                    style={{ fontSize: 20 }}
                  >
                    {stat.value}
                  </Text>
                  {stat.hint && (
                    <Text type='tertiary' size='small' className='block'>
                      {stat.hint}
                    </Text>
                  )}
                </div>
              ))}
            </div>
            <RiskTimelineChart timeline={timeline} hours={hours} />
          </div>
        )}
        {type === 'ip' && state && (
          <>
            <Typography.Title heading={6}>{t('当前生效封禁')}</Typography.Title>
            <Table
              columns={banColumns}
              dataSource={activeBans}
              rowKey='id'
              size='small'
              pagination={false}
              empty={t('暂无数据')}
              className='mb-4'
            />
          </>
        )}
        <Tabs activeKey={tab} onChange={setTab} keepDOM={false}>
          {!isNetwork && (
            <TabPane
              tab={`${type === 'user' ? t('IP 明细') : t('关联用户')} (${countText(related)})`}
              itemKey='items'
            >
              <Table
                columns={relatedColumns}
                dataSource={related}
                rowKey={(row) => row.user_id || row.ip}
                size='small'
                pagination={{ pageSize: 10 }}
                empty={t('暂无数据')}
              />
            </TabPane>
          )}
          {!isNetwork && (
            <TabPane
              tab={`${type === 'ua' ? t('IP 明细') : t('UA 明细')} (${countText(extra)})`}
              itemKey='extra'
            >
              <Table
                columns={extraColumns}
                dataSource={extra}
                rowKey={(row) => row.ip || row.ua}
                size='small'
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
                  className='mb-2'
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
                size='small'
                pagination={{ pageSize: 10 }}
                empty={t('暂无数据')}
              />
            </TabPane>
          )}
          <TabPane tab={t('风控事件')} itemKey='events'>
            <RiskEvents
              key={`${type}:${value}`}
              embedded
              fixedFilters={eventFilters}
              onInspect={onInspect}
              refreshKey={refreshKey}
            />
          </TabPane>
        </Tabs>
        <Text type='tertiary' className='block mt-3'>
          {isNetwork
            ? t('网段详情展示封禁记录；调用证据请查看具体 IP。')
            : t('调用证据统计近 {{hours}} 小时，事件记录可查看全部历史。', {
                hours,
              })}
        </Text>
      </Spin>
    </SideSheet>
  );
};

export default RiskSubjectDrawer;
