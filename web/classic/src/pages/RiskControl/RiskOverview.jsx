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
  Card,
  Descriptions,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';

export default function RiskOverview({ refreshKey, onViewEvents }) {
  const { t } = useTranslation();
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [reload, setReload] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError('');
    API.get('/api/risk/overview', {
      signal: controller.signal,
      skipErrorHandler: true,
    })
      .then((res) => {
        if (controller.signal.aborted) return;
        if (!res.data.success) throw new Error(res.data.message);
        setData(res.data.data);
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setError(e.response?.data?.message || e.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [refreshKey, reload]);
  const setting = data?.setting || {};
  const enabledRules = (setting.auto_ban_rules || []).filter(
    (rule) => rule.enabled,
  ).length;
  const scan = data?.scan;
  const scanMinutes = setting.scan_minutes || 10;
  const scanStale =
    scan &&
    Date.now() / 1000 - (scan.completed_at || scan.started_at) >
      Math.max(scanMinutes * 120, 180);
  const guardMode = (enabled, dryRun) =>
    !data
      ? '-'
      : !setting.enabled || !enabled
        ? t('未启用')
        : dryRun
          ? t('演练模式')
          : t('执行模式');
  const eventLabels = {
    alert: t('规则告警'),
    block_ip: t('IP 拦截'),
    block_ua: t('UA 拦截'),
    ban_auto: t('自动封禁'),
    ban_manual: t('手动封禁'),
    ban_ip: t('IP 封禁'),
    unban: t('解除封禁'),
    unban_auto: t('到期解封'),
    unban_ip: t('IP 解封'),
  };
  return (
    <Spin spinning={loading}>
      <Space className='mb-4' wrap>
        <Button
          icon={<IconRefresh />}
          aria-label={t('刷新')}
          onClick={() => setReload((n) => n + 1)}
        >
          {t('刷新')}
        </Button>
        <Typography.Text type='tertiary'>
          {t(
            '概览统计近 24 小时的已落库记录。告警聚合可能延迟，记录条数与命中次数不同。',
          )}
        </Typography.Text>
      </Space>
      {error && <Banner type='danger' className='mb-4' description={error} />}
      {Object.entries(data?.errors || {}).map(([key, message]) => (
        <Banner
          key={key}
          type='warning'
          className='mb-4'
          description={t('部分概览数据读取失败：{{message}}', { message })}
        />
      ))}
      <div className='grid grid-cols-1 md:grid-cols-3 gap-4 mb-4'>
        {[
          [t('已禁用账号（含手动禁用）'), data?.counts?.disabled_users],
          [t('生效 IP 封禁记录'), data?.counts?.active_ip_bans],
          [t('待处理告警'), data?.counts?.pending_alerts],
        ].map(([title, value]) => (
          <Card key={title} title={title}>
            <Typography.Title heading={2}>{value ?? '-'}</Typography.Title>
            {title === t('待处理告警') && (
              <Button
                theme='borderless'
                onClick={() =>
                  onViewEvents({
                    event_type: 'alert',
                    review_status: 'pending',
                  })
                }
              >
                {t('查看事件')}
              </Button>
            )}
          </Card>
        ))}
      </div>
      <div className='grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4'>
        <Card title={t('防护与日志状态')}>
          <Descriptions
            data={[
              {
                key: t('自动检测总开关'),
                value: !data ? (
                  '-'
                ) : (
                  <Tag color={setting.enabled ? 'green' : 'grey'}>
                    {setting.enabled ? t('已启用') : t('未启用')}
                  </Tag>
                ),
              },
              {
                key: t('有效扫描规则'),
                value: data ? (setting.enabled ? enabledRules : 0) : '-',
              },
              {
                key: t('已配置扫描规则'),
                value: data ? (setting.auto_ban_rules || []).length : '-',
              },
              {
                key: t('测活防护'),
                value: guardMode(
                  setting.probe_guard_enabled,
                  setting.probe_guard_dry_run,
                ),
              },
              {
                key: t('错误防护'),
                value: guardMode(
                  setting.error_guard_enabled,
                  setting.error_guard_dry_run,
                ),
              },
              {
                key: t('静态黑名单条目'),
                value: data
                  ? (setting.ip_blacklist?.length || 0) +
                    (setting.ua_blacklist?.length || 0)
                  : '-',
              },
              {
                key: t('全局 IP 记录'),
                value: data
                  ? data.ip_log_enabled
                    ? t('已启用')
                    : t('未启用')
                  : '-',
              },
              {
                key: t('全局 UA 记录'),
                value: data
                  ? data.ua_log_enabled
                    ? t('已启用')
                    : t('未启用')
                  : '-',
              },
              {
                key: t('消费日志记录'),
                value: data
                  ? data.consume_log_enabled
                    ? t('已启用')
                    : t('未启用')
                  : '-',
              },
              {
                key: t('近 24 小时 IP / UA 日志覆盖'),
                value: data?.coverage
                  ? `${data.coverage.with_ip} / ${data.coverage.with_ua} / ${data.coverage.requests}`
                  : '-',
              },
            ]}
          />
          <Typography.Text type='tertiary' className='block mt-3'>
            {t(
              '覆盖数依次为记录 IP、记录 UA、总请求日志数；开关开启也不能补回历史缺失字段。动态 IP 封禁独立于总开关生效。',
            )}
          </Typography.Text>
          {data &&
            (!data.ip_log_enabled ||
              !data.ua_log_enabled ||
              !data.consume_log_enabled) && (
              <Banner
                type='warning'
                className='mt-3'
                description={t(
                  '部分日志记录已关闭，基于日志的扫描与排行可能不完整。',
                )}
              />
            )}
        </Card>
        <Card title={t('最近一次主节点扫描')}>
          {scan ? (
            <>
              <Descriptions
                data={[
                  { key: t('执行节点'), value: scan.node || '-' },
                  {
                    key: t('扫描结果'),
                    value:
                      scan.status === 'success'
                        ? t('成功')
                        : scan.status === 'failed'
                          ? t('失败')
                          : t('执行中'),
                  },
                  {
                    key: t('开始时间'),
                    value: timestamp2string(scan.started_at),
                  },
                  {
                    key: t('完成时间'),
                    value: scan.completed_at
                      ? timestamp2string(scan.completed_at)
                      : '-',
                  },
                  { key: t('评估规则数'), value: scan.rules },
                  { key: t('命中目标数'), value: scan.hits },
                ]}
              />
              {scan.error && (
                <Banner
                  type='danger'
                  className='mt-3'
                  description={scan.error}
                />
              )}
              {setting.enabled && enabledRules > 0 && scanStale && (
                <Banner
                  type='warning'
                  className='mt-3'
                  description={t(
                    '最近扫描时间已超过预期周期，请检查主节点后台任务。',
                  )}
                />
              )}
            </>
          ) : (
            <Typography.Text type='tertiary'>
              {data?.errors?.scan ? t('扫描状态未知') : t('尚无扫描执行记录')}
            </Typography.Text>
          )}
          <Typography.Text type='tertiary' className='block mt-3'>
            {t(
              '命中目标数是本轮超过阈值的目标数，实际处置仍受权限、豁免及已有封禁影响。',
            )}
          </Typography.Text>
          <Descriptions
            className='mt-4'
            data={[
              { key: t('当前请求节点'), value: data?.node || '-' },
              {
                key: t('本节点后台启动时间'),
                value: data?.runtime?.started_at
                  ? timestamp2string(data.runtime.started_at)
                  : t('未知'),
              },
              {
                key: t('本节点封禁缓存同步'),
                value: data?.runtime?.cache_synced_at
                  ? timestamp2string(data.runtime.cache_synced_at)
                  : t('未知'),
              },
            ]}
          />
          {data?.runtime?.cache_error && (
            <Banner
              type='warning'
              className='mt-3'
              description={data.runtime.cache_error}
            />
          )}
          {data?.runtime?.event_error && (
            <Banner
              type='warning'
              className='mt-3'
              description={
                <Space vertical align='start'>
                  <Typography.Text>
                    {t('事件写入失败')}: {data.runtime.event_error}
                  </Typography.Text>
                  <Typography.Text>
                    {t('时间')}: {timestamp2string(data.runtime.event_error_at)}
                  </Typography.Text>
                </Space>
              }
            />
          )}
          <Typography.Text type='tertiary' className='block mt-3'>
            {t('实时守卫按节点统计；其他节点的封禁缓存通常在一分钟内同步。')}
          </Typography.Text>
        </Card>
      </div>
      <Card title={t('近 24 小时风控事件')}>
        <Table
          pagination={false}
          rowKey='event_type'
          dataSource={data?.events || []}
          empty={t('暂无数据')}
          columns={[
            {
              title: t('类型'),
              dataIndex: 'event_type',
              render: (value) => (
                <Button
                  theme='borderless'
                  onClick={() => onViewEvents({ event_type: value, hours: 24 })}
                >
                  {eventLabels[value] || value}
                </Button>
              ),
            },
            { title: t('记录条数'), dataIndex: 'records' },
            { title: t('累计命中次数'), dataIndex: 'hits' },
          ]}
        />
      </Card>
    </Spin>
  );
}
