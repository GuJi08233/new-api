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
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, isRoot } from '../../helpers';
import { riskActionLabel } from './riskLabels';
import RiskUserState from './RiskUserState';

export function RiskBatchToolbar({ targets, mode, onBatch }) {
  const { t } = useTranslation();
  if (!targets.length) return null;
  const actions =
    mode === 'user'
      ? [
          'disable_user',
          'enable_user',
          ...(isRoot() ? ['whitelist_global', 'whitelist_account'] : []),
        ]
      : mode === 'ip'
        ? ['ban_ip']
        : ['unban_ip'];
  return (
    <Space wrap className='my-3'>
      <Typography.Text>
        {t('已勾选 {{count}} 个目标', { count: targets.length })}
      </Typography.Text>
      {actions.map((action) => (
        <Button
          key={action}
          disabled={targets.length > 100}
          onClick={() => onBatch({ targets, action })}
        >
          {t('批量{{action}}', { action: riskActionLabel(action, t) })}
        </Button>
      ))}
      {targets.length > 100 && (
        <Typography.Text type='danger'>
          {t('每次最多处理 100 个目标')}
        </Typography.Text>
      )}
    </Space>
  );
}

export default function RiskBatchModal({ batch, onClose, onApplied }) {
  const { t } = useTranslation();
  const [preview, setPreview] = useState([]);
  const [results, setResults] = useState(null);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  const [reason, setReason] = useState('');
  const [durationMode, setDurationMode] = useState('temporary');
  const [minutes, setMinutes] = useState(60);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setPreview([]);
    setError('');
    API.post(
      '/api/risk/batch',
      { ...batch, preview: true },
      { signal: controller.signal, skipErrorHandler: true },
    )
      .then((res) => {
        if (controller.signal.aborted) return;
        if (!res.data.success) throw new Error(res.data.message);
        setPreview(res.data.data?.results || []);
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setError(e.response?.data?.message || e.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [batch, reload]);
  const allowed = preview.filter((item) => item.allowed);
  const submit = async () => {
    if (submitting || !allowed.length || !reason.trim()) return;
    if (
      batch.action === 'ban_ip' &&
      durationMode === 'temporary' &&
      (!Number.isInteger(minutes) || minutes < 1 || minutes > 43200)
    ) {
      setError(t('封禁时长无效'));
      return;
    }
    setSubmitting(true);
    setError('');
    try {
      const res = await API.post(
        '/api/risk/batch',
        {
          ...batch,
          targets: allowed.map((item) => item.target),
          reason: reason.trim(),
          expire_minutes:
            batch.action === 'ban_ip' && durationMode === 'temporary'
              ? minutes
              : 0,
        },
        { skipErrorHandler: true },
      );
      if (!res.data.success) throw new Error(res.data.message);
      const executed = res.data.data?.results || [];
      setResults([...preview.filter((item) => !item.allowed), ...executed]);
      onApplied();
    } catch (e) {
      setError(e.response?.data?.message || e.message);
    }
    setSubmitting(false);
  };
  const statusLabel = (status) =>
    ({
      ready: t('可以执行'),
      success: t('成功'),
      failed: t('失败'),
      skipped: t('已跳过'),
    })[status] || status;
  return (
    <Modal
      visible
      title={t('批量{{action}}', { action: riskActionLabel(batch.action, t) })}
      width={800}
      onCancel={() => {
        if (!submitting) onClose();
      }}
      onOk={results ? onClose : submit}
      okText={results ? t('完成') : t('确认执行')}
      okButtonProps={{
        disabled: !results && (loading || !allowed.length || !reason.trim()),
        loading: submitting,
        'aria-label': results ? t('完成') : t('确认执行'),
      }}
      cancelButtonProps={{ disabled: submitting, 'aria-label': t('取消') }}
      cancelText={t('取消')}
      maskClosable={!submitting}
      closable={!submitting}
    >
      {error && (
        <Banner type='danger' className='mb-3' description={t(error)} />
      )}
      <Banner
        type='info'
        className='mb-3'
        description={
          batch.action === 'disable_user'
            ? t(
                '仅处理明确勾选的账号，禁用为永久；临时禁用账号将转为永久禁用。',
              )
            : batch.action === 'ban_ip'
              ? t(
                  '封禁会影响所选地址或网段的请求。当前管理员来源 IP 会被服务端排除。',
                )
              : batch.action === 'unban_ip'
                ? t(
                    '仅解除勾选的封禁记录，其他覆盖封禁或静态黑名单仍可能拦截请求。',
                  )
                : batch.action === 'whitelist_global'
                  ? t(
                      '全局豁免会跳过自动处置与请求风险拦截；不会删除已有手动封禁记录。',
                    )
                  : batch.action === 'whitelist_account'
                    ? t(
                        '账号豁免只保护账号免于自动禁用，IP 封禁与请求拦截仍生效。',
                      )
                    : t('仅恢复明确勾选的账号，并清除禁用原因与到期时间。')
        }
      />
      {!results && (
        <Space vertical align='start' className='w-full mb-3'>
          <Typography.Text>
            {t('本次预计执行 {{count}} 个目标', { count: allowed.length })}
          </Typography.Text>
          {batch.action === 'ban_ip' && (
            <Space wrap>
              <Select
                value={durationMode}
                onChange={setDurationMode}
                style={{ width: 120 }}
                aria-label={t('封禁期限')}
              >
                <Select.Option value='temporary'>{t('临时封禁')}</Select.Option>
                <Select.Option value='permanent'>{t('永久封禁')}</Select.Option>
              </Select>
              {durationMode === 'temporary' && (
                <>
                  <InputNumber
                    value={minutes}
                    onChange={setMinutes}
                    min={1}
                    max={43200}
                    aria-label={t('封禁分钟数')}
                  />
                  <Typography.Text>{t('分钟')}</Typography.Text>
                </>
              )}
            </Space>
          )}
          <TextArea
            value={reason}
            onChange={setReason}
            maxCount={512}
            placeholder={t('填写处置原因（必填）')}
            style={{ width: '100%' }}
          />
          <Button
            onClick={() => setReload((n) => n + 1)}
            disabled={loading || submitting}
          >
            {t('重新核对影响')}
          </Button>
        </Space>
      )}
      <Table
        loading={loading}
        dataSource={results || preview}
        pagination={{ pageSize: 10 }}
        rowKey={(row) => `${row.target.type}:${row.target.value}`}
        columns={[
          {
            title: t('目标'),
            render: (_, row) =>
              row.target.type === 'ip_ban'
                ? row.state?.target || `#${row.target.value}`
                : row.target.value,
          },
          {
            title: t('当前状态'),
            render: (_, row) =>
              !row.state ? (
                t('状态未知')
              ) : row.target.type === 'user' ? (
                <RiskUserState state={row.state} />
              ) : row.target.type === 'ip_ban' ? (
                t('已封禁')
              ) : row.state?.length ? (
                t('存在覆盖或重叠封禁')
              ) : (
                t('暂无生效封禁')
              ),
          },
          {
            title: t('结果'),
            dataIndex: 'status',
            render: (value) => (
              <Tag
                color={
                  value === 'failed'
                    ? 'red'
                    : value === 'success'
                      ? 'green'
                      : 'grey'
                }
              >
                {statusLabel(value)}
              </Tag>
            ),
          },
          {
            title: t('说明'),
            dataIndex: 'message',
            render: (value) => t(value),
          },
        ]}
      />
    </Modal>
  );
}
