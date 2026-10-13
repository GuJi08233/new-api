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
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';
import {
  riskActionLabel,
  riskMetricLabel,
  riskReviewLabel,
} from './riskLabels';

export default function RiskEventReviewModal({ event, onClose, onSaved }) {
  const { t } = useTranslation();
  const [current, setCurrent] = useState(event);
  const [status, setStatus] = useState(event.review_status || 'pending');
  const [note, setNote] = useState(event.review_note || '');
  const [assignee, setAssignee] = useState(event.assignee_id || 0);
  const [history, setHistory] = useState([]);
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [reload, setReload] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setLoaded(false);
    setError('');
    API.get(`/api/risk/events/${event.id}/reviews`, {
      signal: controller.signal,
      skipErrorHandler: true,
    })
      .then((res) => {
        if (controller.signal.aborted) return;
        if (!res.data.success) throw new Error(res.data.message);
        const latest = res.data.data?.event;
        if (!latest) throw new Error('当前事件读取失败');
        setCurrent(latest);
        setStatus(latest.review_status || 'pending');
        setNote(latest.review_note || '');
        setAssignee(latest.assignee_id || 0);
        setHistory(res.data.data?.items || []);
        setLoaded(true);
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setError(e.response?.data?.message || e.message);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [event.id, reload]);
  const save = async () => {
    if (saving || !loaded) return;
    if (!Number.isSafeInteger(assignee) || assignee < 0) {
      setError('负责人 ID 必须为非负整数');
      return;
    }
    setSaving(true);
    setError('');
    try {
      const res = await API.put(
        `/api/risk/events/${event.id}/review`,
        {
          status,
          note,
          assignee_id: assignee,
          version: current.review_version || 0,
        },
        { skipErrorHandler: true },
      );
      if (!res.data.success) throw new Error(res.data.message);
      onSaved();
    } catch (e) {
      setError(e.response?.data?.message || e.message);
    }
    setSaving(false);
  };
  const evidence = current.evidence;
  const operator = (() => {
    try {
      return JSON.parse(localStorage.getItem('user') || '{}').id;
    } catch {
      return null;
    }
  })();
  return (
    <Modal
      visible
      title={t('处理风控事件 #{{id}}', { id: event.id })}
      width={860}
      onCancel={() => {
        if (!saving) onClose();
      }}
      onOk={save}
      okText={t('保存处理记录')}
      cancelText={t('取消')}
      okButtonProps={{
        disabled: !loaded || loading,
        loading: saving,
        'aria-label': t('保存处理记录'),
      }}
      cancelButtonProps={{ disabled: saving, 'aria-label': t('取消') }}
      closable={!saving}
      maskClosable={!saving}
    >
      {error && (
        <Banner type='danger' className='mb-3' description={t(error)} />
      )}
      <Typography.Text className='block mb-3'>
        {current.reason || '-'}
      </Typography.Text>
      {evidence ? (
        <Descriptions
          data={[
            {
              key: t('规则名称'),
              value:
                evidence.name && evidence.name !== evidence.metric
                  ? evidence.name
                  : riskMetricLabel(evidence.metric, t),
            },
            { key: t('规则标识'), value: evidence.rule_id },
            { key: t('规则版本'), value: evidence.version },
            {
              key: t('观测值 / 阈值'),
              value: `${evidence.observed} ${evidence.comparison} ${evidence.threshold}`,
            },
            { key: t('统计窗口（秒）'), value: evidence.window_seconds },
            {
              key: t('当时配置动作'),
              value: riskActionLabel(evidence.action, t),
            },
            {
              key: t('当时运行模式'),
              value: evidence.dry_run ? t('演练模式') : t('执行模式'),
            },
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
            ...(evidence.offense
              ? [
                  {
                    key: t('实际处置期限'),
                    value: evidence.expires_at
                      ? timestamp2string(evidence.expires_at)
                      : t('永久'),
                  },
                ]
              : []),
          ]}
        />
      ) : (
        <Banner
          type='info'
          description={t(
            '此事件未保存结构化规则快照，请依据事件原因和当时日志判断。',
          )}
        />
      )}
      <Space wrap className='my-4'>
        <Typography.Text>{t('处理状态')}</Typography.Text>
        <Select
          value={status}
          onChange={setStatus}
          disabled={loading || saving}
          aria-label={t('处理状态')}
          style={{ width: 160 }}
        >
          {['pending', 'confirmed', 'false_positive'].map((value) => (
            <Select.Option value={value} key={value}>
              {riskReviewLabel(value, t)}
            </Select.Option>
          ))}
        </Select>
        <Typography.Text>{t('负责人 ID')}</Typography.Text>
        <InputNumber
          value={assignee}
          onChange={setAssignee}
          min={0}
          disabled={loading || saving}
          aria-label={t('负责人 ID')}
        />
        {operator && (
          <Button
            disabled={loading || saving}
            onClick={() => setAssignee(operator)}
          >
            {t('分配给我')}
          </Button>
        )}
        <Button
          disabled={loading || saving}
          onClick={() => setReload((n) => n + 1)}
        >
          {t('重新读取事件')}
        </Button>
      </Space>
      <TextArea
        value={note}
        onChange={setNote}
        maxCount={2000}
        disabled={loading || saving}
        placeholder={t('处理备注')}
      />
      <Typography.Text type='tertiary' className='block my-3'>
        {t(
          '负责人填 0 表示未分配；处理记录随对应事件保留。重新读取会覆盖尚未保存的表单内容。',
        )}
      </Typography.Text>
      <Table
        loading={loading}
        dataSource={history}
        rowKey='id'
        pagination={{ pageSize: 5 }}
        empty={t('暂无处理记录')}
        columns={[
          {
            title: t('时间'),
            dataIndex: 'created_at',
            render: timestamp2string,
          },
          {
            title: t('处理状态'),
            dataIndex: 'status',
            render: (value) => riskReviewLabel(value, t),
          },
          {
            title: t('负责人'),
            dataIndex: 'assignee_id',
            render: (value) => (value ? `#${value}` : '-'),
          },
          {
            title: t('操作人'),
            dataIndex: 'operator_id',
            render: (value) => `#${value}`,
          },
          { title: t('处理备注'), dataIndex: 'note' },
        ]}
      />
    </Modal>
  );
}
