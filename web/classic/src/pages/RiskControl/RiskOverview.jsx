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
  Empty,
  Progress,
  Radio,
  RadioGroup,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, isRoot, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';
import RiskTrendChart from './RiskTrendChart';
import RiskEventReviewModal from './RiskEventReviewModal';
import useRiskFilters from './useRiskFilters';
import {
  riskEventTypeMeta,
  riskRelativeTime,
  riskRuleEventFilters,
  riskRuleLabel,
} from './riskLabels';

const { Text, Title } = Typography;

const BLOCK_TYPES = ['block_ip', 'block_ua', 'block_client_ua'];
const BAN_TYPES = ['ban_ip', 'ban_auto', 'ban_manual'];

// 一层防护的状态卡片:名称、状态标签与一句说明;root 可直接跳到对应的设置分区。
function GuardTile({ title, status, color, detail, onConfigure }) {
  const { t } = useTranslation();
  return (
    <div className='rounded-lg border border-[var(--semi-color-border)] p-3 flex flex-col gap-1 min-w-0'>
      <div className='flex items-center justify-between gap-2'>
        <Text strong ellipsis={{ showTooltip: true }}>
          {title}
        </Text>
        <Tag color={color} size='small'>
          {status}
        </Tag>
      </div>
      <Text type='tertiary' size='small' ellipsis={{ showTooltip: true }}>
        {detail}
      </Text>
      {onConfigure && (
        <div>
          <Button size='small' theme='borderless' onClick={onConfigure}>
            {t('去设置')}
          </Button>
        </div>
      )}
    </div>
  );
}

// 关键指标卡片:数值、说明与一个跳转。
function StatCard({ title, value, hint, actionLabel, onAction, danger }) {
  return (
    <Card bordered className='h-full'>
      <Text type='tertiary' size='small'>
        {title}
      </Text>
      <Title
        heading={2}
        style={{
          margin: '4px 0',
          color: danger && value > 0 ? 'var(--semi-color-danger)' : undefined,
        }}
      >
        {value ?? '-'}
      </Title>
      <div className='flex items-center justify-between gap-2'>
        <Text type='tertiary' size='small'>
          {hint}
        </Text>
        {onAction && (
          <Button size='small' theme='borderless' onClick={onAction}>
            {actionLabel}
          </Button>
        )}
      </div>
    </Card>
  );
}

export default function RiskOverview({
  refreshKey,
  onViewEvents,
  onNavigate,
  onInspect,
}) {
  const { t } = useTranslation();
  const [filters, setFilters] = useRiskFilters('o', { range: '24h' });
  const range = filters.range === '7d' ? '7d' : '24h';
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [reload, setReload] = useState(0);
  const [reviewEvent, setReviewEvent] = useState(null);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError('');
    API.get(`/api/risk/overview?range=${range}`, {
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
  }, [range, refreshKey, reload]);

  const setting = data?.setting || {};
  const rules = setting.auto_ban_rules || [];
  const enabledRules = rules.filter((rule) => rule.enabled).length;
  const scan = data?.scan;
  const scanMinutes = setting.scan_minutes || 10;
  const scanStale =
    scan &&
    Date.now() / 1000 - (scan.completed_at || scan.started_at) >
      Math.max(scanMinutes * 120, 180);
  const clientUa = data?.client_ua || {};
  const coverage = data?.coverage;
  const totals = data?.events || [];
  const sumOf = (types, field) =>
    totals
      .filter((item) => types.includes(item.event_type))
      .reduce((sum, item) => sum + (item[field] || 0), 0);
  const rangeHours = range === '7d' ? 168 : 24;
  const rangeLabel = range === '7d' ? t('近 7 天') : t('近 24 小时');
  const configure = isRoot()
    ? (section) => () => onNavigate('settings', { s_section: section })
    : () => undefined;

  const guardState = (enabledKey, dryRunKey, windowKey, thresholdKey, unit) => {
    if (!data) return { status: '-', color: 'grey', detail: '-' };
    if (!setting.enabled)
      return {
        status: t('未启用'),
        color: 'grey',
        detail: t('风控总开关关闭'),
      };
    if (!setting[enabledKey])
      return { status: t('未启用'), color: 'grey', detail: t('未开启') };
    return {
      status: setting[dryRunKey] ? t('演练模式') : t('执行模式'),
      color: setting[dryRunKey] ? 'orange' : 'red',
      detail: t('{{window}} 秒内 ≥ {{threshold}} {{unit}}', {
        window: setting[windowKey] || 60,
        threshold: setting[thresholdKey] || 5,
        unit,
      }),
    };
  };
  const probe = guardState(
    'probe_guard_enabled',
    'probe_guard_dry_run',
    'probe_guard_window_seconds',
    'probe_guard_model_count',
    t('个不同模型'),
  );
  const errorGuard = guardState(
    'error_guard_enabled',
    'error_guard_dry_run',
    'error_guard_window_seconds',
    'error_guard_threshold',
    t('次错误响应'),
  );
  const scanDetail = !data
    ? '-'
    : scan
      ? t('最近扫描 {{time}} 前 · {{status}}', {
          time: riskRelativeTime(scan.completed_at || scan.started_at),
          status:
            scan.status === 'success'
              ? t('成功')
              : scan.status === 'failed'
                ? t('失败')
                : t('执行中'),
        })
      : t('尚无扫描执行记录');

  return (
    <Spin spinning={loading}>
      <div className='flex flex-wrap items-center gap-3 mb-4'>
        <RadioGroup
          type='button'
          value={range}
          onChange={(event) => setFilters({ range: event.target.value })}
          aria-label={t('统计范围')}
        >
          <Radio value='24h'>{t('近 24 小时')}</Radio>
          <Radio value='7d'>{t('近 7 天')}</Radio>
        </RadioGroup>
        <Button
          icon={<IconRefresh />}
          aria-label={t('刷新')}
          onClick={() => setReload((n) => n + 1)}
        >
          {t('刷新')}
        </Button>
        <Text type='tertiary' size='small'>
          {t('拦截按来源聚合落库，命中次数是聚合后的累计值。')}
        </Text>
      </div>
      {error && <Banner type='danger' className='mb-4' description={error} />}
      {Object.entries(data?.errors || {}).map(([key, message]) => (
        <Banner
          key={key}
          type='warning'
          className='mb-4'
          description={t('部分概览数据读取失败：{{message}}', { message })}
        />
      ))}
      {data && setting.enabled && enabledRules > 0 && scanStale && (
        <Banner
          type='warning'
          className='mb-4'
          description={t('最近扫描时间已超过预期周期，请检查主节点后台任务。')}
        />
      )}
      {scan?.error && (
        <Banner
          type='danger'
          className='mb-4'
          description={`${t('最近一次扫描失败')}: ${scan.error}`}
        />
      )}

      <Card bordered className='mb-4' title={t('防护状态')}>
        <div className='grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-6 gap-3'>
          <GuardTile
            title={t('风控总开关')}
            status={data ? (setting.enabled ? t('已启用') : t('未启用')) : '-'}
            color={setting.enabled ? 'green' : 'grey'}
            detail={t('控制黑名单、扫描规则与实时防护；动态封禁始终生效')}
            onConfigure={configure('general')}
          />
          <GuardTile
            title={t('测活防护')}
            status={probe.status}
            color={probe.color}
            detail={probe.detail}
            onConfigure={configure('realtime')}
          />
          <GuardTile
            title={t('错误防护')}
            status={errorGuard.status}
            color={errorGuard.color}
            detail={errorGuard.detail}
            onConfigure={configure('realtime')}
          />
          <GuardTile
            title={t('扫描规则')}
            status={
              data
                ? t('{{enabled}} / {{total}} 条启用', {
                    enabled: setting.enabled ? enabledRules : 0,
                    total: rules.length,
                  })
                : '-'
            }
            color={
              scan?.status === 'failed'
                ? 'red'
                : setting.enabled && enabledRules > 0
                  ? 'green'
                  : 'grey'
            }
            detail={scanDetail}
            onConfigure={configure('scan')}
          />
          <GuardTile
            title={t('客户端 UA 准入')}
            status={data ? (clientUa.enabled ? t('已启用') : t('未启用')) : '-'}
            color={clientUa.enabled && clientUa.active > 0 ? 'green' : 'grey'}
            detail={t('生效 {{active}} / {{configured}} 条规则', {
              active: clientUa.active ?? 0,
              configured: clientUa.configured ?? 0,
            })}
          />
          <GuardTile
            title={t('静态黑名单')}
            status={
              data
                ? t('{{count}} 条', {
                    count:
                      (setting.ip_blacklist?.length || 0) +
                      (setting.ua_blacklist?.length || 0),
                  })
                : '-'
            }
            color={
              setting.enabled &&
              (setting.ip_blacklist?.length || setting.ua_blacklist?.length)
                ? 'green'
                : 'grey'
            }
            detail={t('IP {{ip}} 条，UA {{ua}} 条', {
              ip: setting.ip_blacklist?.length || 0,
              ua: setting.ua_blacklist?.length || 0,
            })}
            onConfigure={configure('blacklist')}
          />
        </div>
      </Card>

      <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4 mb-4'>
        <StatCard
          title={t('待处理告警')}
          value={data?.counts?.pending_alerts}
          hint={t('等待确认或标记误报')}
          actionLabel={t('去处理')}
          onAction={() =>
            onViewEvents({ category: 'alert', review_status: 'pending' })
          }
          danger
        />
        <StatCard
          title={t('已禁用账号')}
          value={data?.counts?.disabled_users}
          hint={t('其中临时禁用 {{count}} 个', {
            count: data?.counts?.temporary_disabled_users ?? 0,
          })}
          actionLabel={t('查看')}
          onAction={() => onNavigate('dispositions', { d_view: 'users' })}
        />
        <StatCard
          title={t('生效 IP 封禁')}
          value={data?.counts?.active_ip_bans}
          hint={t('其中永久 {{count}} 条', {
            count: data?.counts?.permanent_ip_bans ?? 0,
          })}
          actionLabel={t('查看')}
          onAction={() =>
            onNavigate('dispositions', { d_view: 'ip', b_status: 'active' })
          }
        />
        <StatCard
          title={t('{{range}}拦截次数', { range: rangeLabel })}
          value={data ? sumOf(BLOCK_TYPES, 'hits') : undefined}
          hint={t('新增封禁与禁用 {{count}} 次', {
            count: sumOf(BAN_TYPES, 'records'),
          })}
          actionLabel={t('查看')}
          onAction={() =>
            onViewEvents({ category: 'block', hours: rangeHours })
          }
        />
      </div>

      <Card
        bordered
        className='mb-4'
        title={t('风控活动趋势')}
        headerExtraContent={
          <Text type='tertiary' size='small'>
            {rangeLabel}
          </Text>
        }
      >
        <RiskTrendChart
          buckets={data?.activity || []}
          since={data?.since}
          hours={rangeHours}
        />
      </Card>

      <div className='grid grid-cols-1 xl:grid-cols-2 gap-4 mb-4'>
        <Card
          bordered
          title={t('拦截最多的来源')}
          headerExtraContent={
            <Button
              size='small'
              theme='borderless'
              onClick={() =>
                onViewEvents({ category: 'block', hours: rangeHours })
              }
            >
              {t('全部拦截记录')}
            </Button>
          }
        >
          <Table
            size='small'
            pagination={false}
            rowKey='ip'
            dataSource={data?.top_blocked_ips || []}
            empty={<Empty description={t('窗口内没有拦截记录')} />}
            columns={[
              {
                title: t('来源 IP'),
                dataIndex: 'ip',
                render: (ip) => <IpTag ip={ip} />,
              },
              {
                title: t('命中次数'),
                dataIndex: 'hits',
                width: 110,
                render: (value) => <Tag color='red'>{value}</Tag>,
              },
              {
                title: t('最近'),
                dataIndex: 'last_at',
                width: 90,
                render: (value) => (
                  <Text type='tertiary' size='small'>
                    {t('{{time}} 前', { time: riskRelativeTime(value) })}
                  </Text>
                ),
              },
              {
                title: t('操作'),
                width: 150,
                render: (_, row) => (
                  <Space spacing={4}>
                    <Button
                      size='small'
                      onClick={() =>
                        onInspect({ type: 'ip', value: row.ip, hours: 24 })
                      }
                    >
                      {t('调查')}
                    </Button>
                    <Button
                      size='small'
                      theme='borderless'
                      onClick={() =>
                        onViewEvents({ ip: row.ip, hours: rangeHours })
                      }
                    >
                      {t('记录')}
                    </Button>
                  </Space>
                ),
              },
            ]}
          />
        </Card>

        <Card
          bordered
          title={t('最活跃的规则与防护')}
          headerExtraContent={
            <Text type='tertiary' size='small'>
              {t('按命中次数排序')}
            </Text>
          }
        >
          <Table
            size='small'
            pagination={false}
            rowKey={(row) => `${row.event_type}:${row.rule_id}:${row.rule}`}
            dataSource={data?.top_rules || []}
            empty={<Empty description={t('窗口内没有规则命中')} />}
            columns={[
              {
                title: t('规则 / 来源'),
                render: (_, row) => (
                  <Text
                    ellipsis={{ showTooltip: true }}
                    style={{ maxWidth: 260 }}
                  >
                    {riskRuleLabel(row, rules, t)}
                  </Text>
                ),
              },
              {
                title: t('结果'),
                dataIndex: 'event_type',
                width: 120,
                render: (_, row) => {
                  const meta = riskEventTypeMeta(row.event_type, t);
                  return (
                    <Tag size='small' color={meta.color}>
                      {meta.label}
                    </Tag>
                  );
                },
              },
              {
                title: t('命中次数'),
                dataIndex: 'hits',
                width: 100,
                render: (value) => <Tag color='blue'>{value}</Tag>,
              },
              {
                title: t('操作'),
                width: 90,
                render: (_, row) => (
                  <Button
                    size='small'
                    theme='borderless'
                    onClick={() =>
                      onViewEvents({
                        ...riskRuleEventFilters(row),
                        hours: rangeHours,
                      })
                    }
                  >
                    {t('记录')}
                  </Button>
                ),
              },
            ]}
          />
        </Card>
      </div>

      <div className='grid grid-cols-1 xl:grid-cols-2 gap-4'>
        <Card
          bordered
          title={t('最新待处理告警')}
          headerExtraContent={
            <Button
              size='small'
              theme='borderless'
              onClick={() =>
                onViewEvents({ category: 'alert', review_status: 'pending' })
              }
            >
              {t('全部待处理')}
            </Button>
          }
        >
          <Table
            size='small'
            pagination={false}
            rowKey='id'
            dataSource={data?.pending || []}
            empty={<Empty description={t('没有待处理的告警')} />}
            columns={[
              {
                title: t('时间'),
                dataIndex: 'created_at',
                width: 90,
                render: (value) => (
                  <Text type='tertiary' size='small'>
                    {t('{{time}} 前', { time: riskRelativeTime(value) })}
                  </Text>
                ),
              },
              {
                title: t('规则'),
                width: 160,
                render: (_, row) => (
                  <Text
                    ellipsis={{ showTooltip: true }}
                    style={{ maxWidth: 150 }}
                  >
                    {riskRuleLabel(row, rules, t)}
                  </Text>
                ),
              },
              {
                title: t('原因'),
                dataIndex: 'reason',
                ellipsis: { showTitle: true },
              },
              {
                title: t('操作'),
                width: 80,
                render: (_, row) => (
                  <Button size='small' onClick={() => setReviewEvent(row)}>
                    {t('处理')}
                  </Button>
                ),
              },
            ]}
          />
        </Card>

        <Card bordered title={t('日志与运行状态')}>
          <div className='mb-3'>
            <div className='flex items-center justify-between'>
              <Text size='small'>{t('近 24 小时日志带 IP 的比例')}</Text>
              <Text size='small' type='tertiary'>
                {coverage ? `${coverage.with_ip} / ${coverage.requests}` : '-'}
              </Text>
            </div>
            <Progress
              percent={
                coverage?.requests
                  ? Math.round((coverage.with_ip / coverage.requests) * 100)
                  : 0
              }
              showInfo
              aria-label={t('近 24 小时日志带 IP 的比例')}
            />
            <div className='flex items-center justify-between mt-2'>
              <Text size='small'>{t('近 24 小时日志带 UA 的比例')}</Text>
              <Text size='small' type='tertiary'>
                {coverage ? `${coverage.with_ua} / ${coverage.requests}` : '-'}
              </Text>
            </div>
            <Progress
              percent={
                coverage?.requests
                  ? Math.round((coverage.with_ua / coverage.requests) * 100)
                  : 0
              }
              showInfo
              aria-label={t('近 24 小时日志带 UA 的比例')}
            />
          </div>
          {data &&
            (!data.ip_log_enabled ||
              !data.ua_log_enabled ||
              !data.consume_log_enabled) && (
              <Banner
                type='warning'
                className='mb-3'
                description={t(
                  '部分日志记录已关闭，基于日志的扫描与排行可能不完整。开启后也不能补回历史缺失字段。',
                )}
              />
            )}
          <Descriptions
            size='small'
            data={[
              {
                key: t('当前请求节点'),
                value: data
                  ? `${data.node || '-'}${data.is_master ? ` (${t('主节点')})` : ''}`
                  : '-',
              },
              {
                key: t('最近一次主节点扫描'),
                value: scan
                  ? `${timestamp2string(scan.started_at)} · ${t('评估 {{rules}} 条规则，命中 {{hits}} 个目标', { rules: scan.rules, hits: scan.hits })}`
                  : t('尚无扫描执行记录'),
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
              className='mt-2'
              description={data.runtime.cache_error}
            />
          )}
          {data?.runtime?.event_error && (
            <Banner
              type='warning'
              className='mt-2'
              description={`${t('事件写入失败')}: ${data.runtime.event_error} (${timestamp2string(data.runtime.event_error_at)})`}
            />
          )}
          <Text type='tertiary' size='small' className='block mt-2'>
            {t('实时防护按节点统计；其他节点的封禁缓存通常在一分钟内同步。')}
          </Text>
        </Card>
      </div>
      {reviewEvent && (
        <RiskEventReviewModal
          event={reviewEvent}
          rules={rules}
          onClose={() => setReviewEvent(null)}
          onSaved={() => {
            setReviewEvent(null);
            setReload((n) => n + 1);
          }}
        />
      )}
    </Spin>
  );
}
