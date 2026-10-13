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
import {
  Banner,
  Descriptions,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';
import {
  riskActionLabel,
  riskEventTypeMeta,
  riskRuleLabel,
} from './riskLabels';

const { Text } = Typography;

const windowLabel = (seconds, t) => {
  if (!seconds) return '-';
  if (seconds % 3600 === 0)
    return t('{{count}} 小时', { count: seconds / 3600 });
  return t('{{count}} 秒', { count: seconds });
};

// 一条风控事件的完整证据:命中了什么、观测值对阈值、当时的配置与实际处置。
// 规则快照记录的是命中时的配置,之后改动规则不会改变这里显示的内容。
export default function RiskEventEvidence({ event, rules, compact = false }) {
  const { t } = useTranslation();
  const meta = riskEventTypeMeta(event.event_type, t);
  const evidence = event.evidence;
  // 命中事件的观测值总是达到了阈值,显示超出多少比画一根总是满格的进度条更有用。
  const ratio =
    evidence?.threshold > 0
      ? Math.round((evidence.observed / evidence.threshold) * 100)
      : 0;

  const summary = [
    {
      key: t('事件'),
      value: (
        <Space spacing={4} wrap>
          <Tag color={meta.color}>{meta.label}</Tag>
          <Text>{timestamp2string(event.created_at)}</Text>
          {event.count > 1 && (
            <Tag size='small'>
              {t('聚合 {{count}} 次', { count: event.count })}
            </Tag>
          )}
        </Space>
      ),
    },
    { key: t('规则 / 来源'), value: riskRuleLabel(event, rules, t) },
    ...(event.user_id
      ? [
          {
            key: t('用户'),
            value: `${event.username || '-'} (#${event.user_id})`,
          },
        ]
      : []),
    ...(event.ip ? [{ key: 'IP', value: <IpTag ip={event.ip} /> }] : []),
    ...(event.ua
      ? [
          {
            key: 'User-Agent',
            value: (
              <Text size='small' className='break-all'>
                {event.ua}
              </Text>
            ),
          },
        ]
      : []),
    {
      key: t('原因'),
      value: (
        <Text size='small' className='break-all'>
          {event.reason || '-'}
        </Text>
      ),
    },
  ];

  const details = evidence
    ? [
        {
          key: t('观测值 / 阈值'),
          value: (
            <Space spacing={8}>
              <Text strong>
                {evidence.observed} {evidence.comparison} {evidence.threshold}
              </Text>
              {ratio > 0 && (
                <Tag size='small' color={ratio >= 200 ? 'red' : 'orange'}>
                  {t('阈值的 {{ratio}}%', { ratio })}
                </Tag>
              )}
            </Space>
          ),
        },
        { key: t('统计窗口'), value: windowLabel(evidence.window_seconds, t) },
        {
          key: t('当时配置动作'),
          value: (
            <Space spacing={4}>
              <Text>{riskActionLabel(evidence.action, t)}</Text>
              {evidence.dry_run && (
                <Tag color='orange' size='small'>
                  {t('演练模式')}
                </Tag>
              )}
            </Space>
          ),
        },
        ...(evidence.offense
          ? [
              {
                key: t('实际处置'),
                value: t('第 {{offense}} 次违规，{{expires}}', {
                  offense: evidence.offense,
                  expires: evidence.expires_at
                    ? t('到 {{time}} 解除', {
                        time: timestamp2string(evidence.expires_at),
                      })
                    : t('永久'),
                }),
              },
            ]
          : []),
        ...(evidence.tiny_max_tokens
          ? [
              {
                key: t('微量请求判定阈值'),
                value: evidence.tiny_max_tokens,
              },
            ]
          : []),
        ...(evidence.status_codes?.length
          ? [
              {
                key: t('统计的状态码'),
                value: evidence.status_codes.join(', '),
              },
            ]
          : []),
        {
          key: t('规则标识 / 版本'),
          value: (
            <Text size='small' type='tertiary'>
              {evidence.rule_id || '-'} / {evidence.version || '-'}
            </Text>
          ),
        },
      ]
    : [];

  // 自动处置与告警才会保存规则快照;拦截与管理员手动处置本来就没有,不提示缺失。
  const expectsEvidence =
    ['alert', 'ban_auto', 'ban_ip'].includes(event.event_type) &&
    event.rule !== 'manual' &&
    !event.operator_id;

  return (
    <div className={compact ? '' : 'grid grid-cols-1 md:grid-cols-2 gap-4'}>
      <Descriptions size='small' data={summary} />
      {evidence ? (
        <Descriptions size='small' data={details} />
      ) : expectsEvidence ? (
        <Banner
          type='info'
          description={t(
            '此事件未保存结构化规则快照，请依据事件原因和当时日志判断。',
          )}
        />
      ) : null}
    </div>
  );
}
