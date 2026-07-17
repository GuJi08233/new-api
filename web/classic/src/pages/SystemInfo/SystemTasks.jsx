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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Modal,
  Progress,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconEyeOpened, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { API, showError, timestamp2string } from '../../helpers';

const TASK_POLL_INTERVAL_MS = 8000;
const TASK_LIMIT = 50;
const { Text } = Typography;

const TASK_TYPE_KEYS = {
  log_cleanup: '日志清理',
  channel_test: '批量渠道测试',
  model_update: '批量上游模型更新',
  midjourney_poll: '绘图任务轮询',
  async_task_poll: '异步任务轮询',
};

const STATUS_META = {
  pending: { color: 'orange', key: '等待中' },
  running: { color: 'blue', key: '运行中' },
  succeeded: { color: 'green', key: '已成功' },
  failed: { color: 'red', key: '已失败' },
};

const isActiveTask = (task) =>
  task?.status === 'pending' || task?.status === 'running';

const taskProgress = (task) => {
  const progress = Number(task?.state?.progress);
  if (!Number.isFinite(progress)) return null;
  return Math.min(100, Math.max(0, progress));
};

const prettyJson = (value) => {
  if (value === undefined || value === null || value === '') return '-';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch (error) {
    return String(value);
  }
};

const SystemTasks = () => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const focusedTaskId = searchParams.get('task_id') || '';
  const [tasks, setTasks] = useState([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [detailTask, setDetailTask] = useState(null);

  const loadTasks = useCallback(
    async (silent = false) => {
      silent ? setRefreshing(true) : setLoading(true);
      try {
        const listRequest = API.get(
          `/api/system-task/list?limit=${TASK_LIMIT}`,
        );
        const focusedRequest = focusedTaskId
          ? API.get(`/api/system-task/${encodeURIComponent(focusedTaskId)}`)
          : Promise.resolve(null);
        const [listRes, focusedRes] = await Promise.all([
          listRequest,
          focusedRequest,
        ]);
        const { success, message, data } = listRes.data || {};
        if (!success || !Array.isArray(data)) {
          showError(message || t('加载系统任务失败'));
          return;
        }
        const focused = focusedRes?.data?.success ? focusedRes.data.data : null;
        if (focused && !data.some((task) => task.task_id === focused.task_id)) {
          setTasks([focused, ...data]);
        } else {
          setTasks(data);
        }
      } catch (error) {
        showError(error?.message || t('加载系统任务失败'));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [focusedTaskId, t],
  );

  useEffect(() => {
    loadTasks();
  }, [loadTasks]);

  const hasActiveTasks = tasks.some(isActiveTask);

  useEffect(() => {
    if (!hasActiveTasks) return undefined;
    const timer = window.setInterval(
      () => loadTasks(true),
      TASK_POLL_INTERVAL_MS,
    );
    return () => window.clearInterval(timer);
  }, [hasActiveTasks, loadTasks]);

  const columns = useMemo(
    () => [
      {
        title: t('类型'),
        dataIndex: 'type',
        width: 190,
        render: (type) => (
          <div>
            <Text strong>{t(TASK_TYPE_KEYS[type] || type)}</Text>
            <Text
              type='tertiary'
              size='small'
              style={{ display: 'block', fontFamily: 'monospace' }}
            >
              {type}
            </Text>
          </div>
        ),
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        width: 100,
        render: (status) => {
          const meta = STATUS_META[status] || { color: 'grey', key: status };
          return (
            <Tag color={meta.color} shape='circle'>
              {t(meta.key)}
            </Tag>
          );
        },
      },
      {
        title: t('进度'),
        width: 180,
        render: (_, record) => {
          const progress = taskProgress(record);
          return progress === null ? (
            <Text type='tertiary'>-</Text>
          ) : (
            <Progress percent={progress} size='small' style={{ width: 145 }} />
          );
        },
      },
      {
        title: t('执行实例'),
        dataIndex: 'locked_by',
        width: 220,
        render: (value) => (
          <Text ellipsis={{ showTooltip: true }}>{value || '-'}</Text>
        ),
      },
      {
        title: t('更新时间'),
        dataIndex: 'updated_at',
        width: 170,
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
      {
        title: t('错误信息'),
        dataIndex: 'error',
        width: 220,
        render: (value) => (
          <Text
            type={value ? 'danger' : 'tertiary'}
            ellipsis={{ showTooltip: true }}
          >
            {value || '-'}
          </Text>
        ),
      },
      {
        title: t('操作'),
        width: 90,
        fixed: 'right',
        render: (_, record) => (
          <Button
            size='small'
            theme='light'
            icon={<IconEyeOpened />}
            onClick={() => setDetailTask(record)}
          >
            {t('详情')}
          </Button>
        ),
      },
    ],
    [t],
  );

  return (
    <>
      <Card
        title={t('系统任务')}
        headerExtraContent={
          <Space>
            {hasActiveTasks ? (
              <Tag color='blue' shape='circle'>
                {t('自动刷新中')}
              </Tag>
            ) : null}
            <Button
              theme='light'
              icon={<IconRefresh />}
              loading={refreshing}
              onClick={() => loadTasks(true)}
            >
              {t('刷新')}
            </Button>
          </Space>
        }
      >
        <Banner
          type='info'
          className='mb-4'
          description={t(
            '这里展示跨实例执行的后台任务；存在等待中或运行中的任务时，每 8 秒自动刷新。',
          )}
        />
        <Table
          columns={columns}
          dataSource={tasks}
          rowKey='task_id'
          loading={loading}
          pagination={{ pageSize: 20 }}
          scroll={{ x: 'max-content' }}
          rowClassName={(record) =>
            record.task_id === focusedTaskId ? 'bg-blue-50' : ''
          }
          empty={t('暂无系统任务')}
        />
      </Card>

      <Modal
        title={t('系统任务详情')}
        visible={!!detailTask}
        onCancel={() => setDetailTask(null)}
        footer={null}
        width={760}
      >
        {detailTask ? (
          <Space vertical align='start' style={{ width: '100%' }} spacing={12}>
            <Text copyable={{ content: detailTask.task_id }}>
              <Text strong>{t('任务 ID')}：</Text> {detailTask.task_id}
            </Text>
            <Text>
              <Text strong>{t('创建时间')}：</Text>{' '}
              {timestamp2string(detailTask.created_at)}
            </Text>
            <div style={{ width: '100%' }}>
              <Text strong>{t('任务参数')}</Text>
              <pre className='mt-2 max-h-48 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-gray-800'>
                {prettyJson(detailTask.payload)}
              </pre>
            </div>
            <div style={{ width: '100%' }}>
              <Text strong>{t('任务状态数据')}</Text>
              <pre className='mt-2 max-h-48 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-gray-800'>
                {prettyJson(detailTask.state)}
              </pre>
            </div>
            <div style={{ width: '100%' }}>
              <Text strong>{t('任务结果')}</Text>
              <pre className='mt-2 max-h-48 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-gray-800'>
                {prettyJson(detailTask.result)}
              </pre>
            </div>
          </Space>
        ) : null}
      </Modal>
    </>
  );
};

export default SystemTasks;
