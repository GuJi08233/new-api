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
  Descriptions,
  Modal,
  Table,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API } from '../../helpers';
import { riskActionLabel, riskMetricLabel } from './riskLabels';

export default function RiskRulePreviewModal({
  rule,
  tinyMaxTokens,
  onClose,
  onInspect,
}) {
  const { t } = useTranslation();
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    API.post(
      '/api/risk/rules/preview',
      { rule, tiny_max_tokens: tinyMaxTokens },
      { signal: controller.signal, skipErrorHandler: true },
    )
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
  }, [rule, tinyMaxTokens]);
  return (
    <Modal
      visible
      title={t('规则命中预览')}
      onCancel={onClose}
      footer={null}
      width={800}
    >
      <Banner
        type='info'
        className='mb-3'
        description={t(
          '预览使用当前表单规则和已保存的全局豁免名单，只读取日志，不执行封禁。',
        )}
      />
      {error && <Banner type='danger' className='mb-3' description={error} />}
      <Descriptions
        data={[
          {
            key: t('规则名称'),
            value: rule.name || riskMetricLabel(rule.metric, t),
          },
          { key: t('规则标识'), value: rule.id || '-' },
          { key: t('规则版本'), value: data?.version || '-' },
          {
            key: t('触发条件'),
            value: t(
              '{{hours}} 小时内，{{metric}} > {{threshold}} 时{{action}}',
              {
                hours: rule.window_hours,
                metric: riskMetricLabel(rule.metric, t),
                threshold: rule.threshold,
                action: riskActionLabel(rule.action, t),
              },
            ),
          },
          { key: t('命中目标数'), value: data?.hits?.length ?? '-' },
        ]}
      />
      {data?.limit_reached && (
        <Banner
          type='warning'
          className='my-3'
          description={t(
            '本轮与实际扫描一样，最多评估指标排名前 {{count}} 个目标。结果可能不含其他目标。',
            { count: data.limit },
          )}
        />
      )}
      <Typography.Text type='tertiary' className='block my-3'>
        {t(
          '命中不代表一定会执行处置；管理员、豁免名单和已有封禁会影响实际动作。',
        )}
      </Typography.Text>
      <Table
        loading={loading}
        dataSource={data?.hits || []}
        pagination={{ pageSize: 10 }}
        rowKey={(row) => `${row.type}:${row.value}`}
        empty={t('暂无命中')}
        columns={[
          {
            title: t('目标'),
            render: (_, row) => (
              <Button
                theme='borderless'
                onClick={() =>
                  onInspect?.({
                    type: row.type,
                    value: row.value,
                    hours: rule.window_hours,
                    excludeWhitelist: true,
                  })
                }
              >
                {row.username ? `${row.username} (#${row.value})` : row.value}
              </Button>
            ),
          },
          { title: t('观测值'), dataIndex: 'observed' },
          { title: t('原因'), dataIndex: 'reason' },
        ]}
      />
    </Modal>
  );
}
