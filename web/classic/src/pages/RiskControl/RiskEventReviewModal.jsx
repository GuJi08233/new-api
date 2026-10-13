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
  Modal,
  Radio,
  RadioGroup,
  Select,
  Space,
  Table,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';
import RiskEventEvidence from './RiskEventEvidence';
import useRiskAssignees from './useRiskAssignees';
import { riskCurrentUserId, riskReviewLabel } from './riskLabels';

const { Text } = Typography;

export default function RiskEventReviewModal({
  event,
  rules,
  onClose,
  onSaved,
}) {
  const { t } = useTranslation();
  const { list: assignees, nameOf } = useRiskAssignees();
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
  const me = riskCurrentUserId();

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
        if (!latest) throw new Error(t('当前事件读取失败'));
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
  }, [event.id, reload, t]);

  const save = async () => {
    if (saving || !loaded) return;
    setSaving(true);
    setError('');
    try {
      const res = await API.put(
        `/api/risk/events/${event.id}/review`,
        {
          status,
          note,
          assignee_id: assignee || 0,
          version: current.review_version || 0,
        },
        { skipErrorHandler: true },
      );
      if (!res.data.success) throw new Error(res.data.message);
      onSaved(res.data.data);
    } catch (e) {
      setError(e.response?.data?.message || e.message);
    }
    setSaving(false);
  };

  const assigneeOptions = [
    { value: 0, label: t('未分配') },
    ...assignees.map((admin) => ({
      value: admin.id,
      label: `${admin.display_name || admin.username} (#${admin.id})`,
    })),
    ...(assignee && !assignees.some((admin) => admin.id === assignee)
      ? [{ value: assignee, label: `#${assignee}` }]
      : []),
  ];

  return (
    <Modal
      visible
      title={t('处理风控事件 #{{id}}', { id: event.id })}
      width={860}
      onCancel={() => {
        if (!saving) onClose();
      }}
      footer={
        <Space>
          <Button
            disabled={loading || saving}
            onClick={() => setReload((n) => n + 1)}
          >
            {t('重新读取')}
          </Button>
          <Button disabled={saving} onClick={onClose}>
            {t('取消')}
          </Button>
          <Button
            theme='solid'
            loading={saving}
            disabled={!loaded || loading}
            onClick={save}
          >
            {t('保存处理记录')}
          </Button>
        </Space>
      }
      closable={!saving}
      maskClosable={!saving}
    >
      {error && (
        <Banner type='danger' className='mb-3' description={t(error)} />
      )}
      <RiskEventEvidence event={current} rules={rules} />
      <div className='grid grid-cols-1 md:grid-cols-2 gap-4 my-4'>
        <div>
          <Text strong className='block mb-2'>
            {t('处理结论')}
          </Text>
          <RadioGroup
            type='button'
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            disabled={loading || saving}
            aria-label={t('处理状态')}
          >
            {['pending', 'confirmed', 'false_positive'].map((value) => (
              <Radio value={value} key={value}>
                {riskReviewLabel(value, t)}
              </Radio>
            ))}
          </RadioGroup>
        </div>
        <div>
          <Text strong className='block mb-2'>
            {t('负责人')}
          </Text>
          <Space>
            <Select
              value={assignee}
              onChange={setAssignee}
              optionList={assigneeOptions}
              filter
              disabled={loading || saving}
              style={{ width: 240 }}
              aria-label={t('负责人')}
            />
            {me > 0 && assignee !== me && (
              <Button
                disabled={loading || saving}
                onClick={() => setAssignee(me)}
              >
                {t('分配给我')}
              </Button>
            )}
          </Space>
        </div>
      </div>
      <TextArea
        value={note}
        onChange={setNote}
        maxCount={2000}
        rows={3}
        disabled={loading || saving}
        placeholder={t('处理备注，例如核查结论或后续动作')}
      />
      <Text type='tertiary' size='small' className='block my-3'>
        {t(
          '保存时检查事件版本，避免覆盖其他管理员刚保存的内容；重新读取会丢弃尚未保存的修改。',
        )}
      </Text>
      <Table
        size='small'
        loading={loading}
        dataSource={history}
        rowKey='id'
        pagination={{ pageSize: 5 }}
        empty={t('暂无处理记录')}
        columns={[
          {
            title: t('时间'),
            dataIndex: 'created_at',
            width: 170,
            render: timestamp2string,
          },
          {
            title: t('处理状态'),
            dataIndex: 'status',
            width: 100,
            render: (value) => riskReviewLabel(value, t),
          },
          {
            title: t('负责人'),
            dataIndex: 'assignee_id',
            width: 120,
            render: (value) => nameOf(value) || '-',
          },
          {
            title: t('操作人'),
            dataIndex: 'operator_id',
            width: 120,
            render: (value) => nameOf(value) || '-',
          },
          { title: t('处理备注'), dataIndex: 'note' },
        ]}
      />
    </Modal>
  );
}
