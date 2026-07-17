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
  Popconfirm,
  Progress,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';

const INSTANCE_POLL_INTERVAL_MS = 30000;
const { Text } = Typography;

const formatPercent = (value) => {
  const number = Number(value);
  if (!Number.isFinite(number)) return '-';
  return `${number.toFixed(1)}%`;
};

const formatBytes = (value) => {
  const bytes = Number(value);
  if (!Number.isFinite(bytes) || bytes < 0) return '-';
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  );
  const amount = bytes / 1024 ** index;
  return `${amount.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
};

const resourceProgress = (value) => {
  const number = Number(value);
  if (!Number.isFinite(number)) return 0;
  return Math.min(100, Math.max(0, number));
};

const SystemInstances = () => {
  const { t } = useTranslation();
  const [instances, setInstances] = useState([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [deletingNode, setDeletingNode] = useState('');

  const loadInstances = useCallback(
    async (silent = false) => {
      silent ? setRefreshing(true) : setLoading(true);
      try {
        const res = await API.get('/api/system-info/instances');
        const { success, message, data } = res.data || {};
        if (!success || !Array.isArray(data)) {
          showError(message || t('加载系统实例失败'));
          return;
        }
        setInstances(data);
      } catch (error) {
        showError(error?.message || t('加载系统实例失败'));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [t],
  );

  useEffect(() => {
    loadInstances();
    const timer = window.setInterval(
      () => loadInstances(true),
      INSTANCE_POLL_INTERVAL_MS,
    );
    return () => window.clearInterval(timer);
  }, [loadInstances]);

  const staleInstances = useMemo(
    () => instances.filter((instance) => instance.status === 'stale'),
    [instances],
  );

  const deleteInstance = async (nodeName) => {
    setDeletingNode(nodeName);
    try {
      const res = await API.delete(
        `/api/system-info/instances/${encodeURIComponent(nodeName)}`,
      );
      const { success, message } = res.data || {};
      if (!success) {
        showError(message || t('删除实例失败'));
        return;
      }
      showSuccess(t('已删除过期实例'));
      await loadInstances(true);
    } catch (error) {
      showError(error?.message || t('删除实例失败'));
    } finally {
      setDeletingNode('');
    }
  };

  const deleteAllStale = () => {
    Modal.confirm({
      title: t('清理全部过期实例'),
      content: t('确定删除全部过期实例记录吗？在线实例不会受到影响。'),
      okText: t('确认清理'),
      cancelText: t('取消'),
      okType: 'danger',
      centered: true,
      onOk: async () => {
        try {
          const res = await API.delete('/api/system-info/stale-instances');
          const { success, message, data } = res.data || {};
          if (!success) {
            showError(message || t('清理过期实例失败'));
            return;
          }
          showSuccess(
            t('已清理 {{count}} 个过期实例', {
              count: data?.deleted_count || 0,
            }),
          );
          await loadInstances(true);
        } catch (error) {
          showError(error?.message || t('清理过期实例失败'));
        }
      },
    });
  };

  const columns = useMemo(
    () => [
      {
        title: t('实例'),
        dataIndex: 'node_name',
        width: 220,
        render: (_, record) => (
          <div className='min-w-0'>
            <Text strong ellipsis={{ showTooltip: true }}>
              {record.info?.node?.name || record.node_name}
            </Text>
            <Text
              type='tertiary'
              size='small'
              ellipsis={{ showTooltip: true }}
              style={{ display: 'block' }}
            >
              {record.info?.host?.hostname || record.node_name}
            </Text>
          </div>
        ),
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        width: 90,
        render: (status) => (
          <Tag color={status === 'online' ? 'green' : 'orange'} shape='circle'>
            {status === 'online' ? t('在线') : t('已过期')}
          </Tag>
        ),
      },
      {
        title: t('角色'),
        width: 90,
        render: (_, record) =>
          record.info?.role?.is_master ? (
            <Tag color='blue'>Master</Tag>
          ) : (
            <Tag>Worker</Tag>
          ),
      },
      {
        title: 'CPU',
        width: 125,
        render: (_, record) => {
          const percent = record.info?.resources?.cpu?.usage_percent;
          return (
            <Space spacing={6}>
              <Progress
                percent={resourceProgress(percent)}
                showInfo={false}
                style={{ width: 62 }}
              />
              <Text size='small'>{formatPercent(percent)}</Text>
            </Space>
          );
        },
      },
      {
        title: t('内存'),
        width: 125,
        render: (_, record) => {
          const percent = record.info?.resources?.memory?.usage_percent;
          return (
            <Space spacing={6}>
              <Progress
                percent={resourceProgress(percent)}
                showInfo={false}
                style={{ width: 62 }}
              />
              <Text size='small'>{formatPercent(percent)}</Text>
            </Space>
          );
        },
      },
      {
        title: t('存储'),
        width: 145,
        render: (_, record) => {
          const storage = record.info?.resources?.storage;
          return (
            <div>
              <Text size='small'>{formatPercent(storage?.used_percent)}</Text>
              <Text type='tertiary' size='small' style={{ display: 'block' }}>
                {formatBytes(storage?.used_bytes)} /{' '}
                {formatBytes(storage?.total_bytes)}
              </Text>
            </div>
          );
        },
      },
      {
        title: t('运行时'),
        width: 145,
        render: (_, record) => {
          const runtime = record.info?.runtime || {};
          return (
            <div>
              <Text size='small'>{runtime.version || '-'}</Text>
              <Text type='tertiary' size='small' style={{ display: 'block' }}>
                {[runtime.goos, runtime.goarch].filter(Boolean).join('/') ||
                  '-'}
              </Text>
            </div>
          );
        },
      },
      {
        title: t('启动时间'),
        dataIndex: 'started_at',
        width: 170,
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
      {
        title: t('最后心跳'),
        dataIndex: 'last_seen_at',
        width: 170,
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
      {
        title: t('操作'),
        width: 90,
        fixed: 'right',
        render: (_, record) =>
          record.status === 'stale' ? (
            <Popconfirm
              title={t('删除过期实例')}
              content={t('仅删除当前过期实例记录，确定继续吗？')}
              onConfirm={() => deleteInstance(record.node_name)}
            >
              <Button
                type='danger'
                theme='light'
                size='small'
                icon={<IconDelete />}
                loading={deletingNode === record.node_name}
              />
            </Popconfirm>
          ) : (
            <Text type='tertiary'>-</Text>
          ),
      },
    ],
    [deletingNode, t],
  );

  return (
    <Card
      title={t('系统实例')}
      headerExtraContent={
        <Space>
          <Button
            theme='light'
            icon={<IconRefresh />}
            loading={refreshing}
            onClick={() => loadInstances(true)}
          >
            {t('刷新')}
          </Button>
          <Button
            type='danger'
            theme='light'
            icon={<IconDelete />}
            disabled={staleInstances.length === 0}
            onClick={deleteAllStale}
          >
            {t('清理过期实例')}
          </Button>
        </Space>
      }
    >
      <Banner
        type='info'
        className='mb-4'
        description={t(
          '实例每 30 秒自动刷新；超过心跳阈值的实例会标记为过期，可安全清理其状态记录。',
        )}
      />
      <Table
        columns={columns}
        dataSource={instances}
        rowKey='node_name'
        loading={loading}
        pagination={false}
        scroll={{ x: 'max-content' }}
        empty={t('暂无系统实例')}
      />
    </Card>
  );
};

export default SystemInstances;
