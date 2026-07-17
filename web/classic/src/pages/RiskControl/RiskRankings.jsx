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

import React, { useEffect, useState, useCallback } from 'react';
import {
  Banner,
  Button,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tabs,
  TabPane,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import {
  API,
  showError,
  showSuccess,
  timestamp2string,
} from '../../helpers';

const { Text } = Typography;

const HOURS_OPTIONS = [
  { value: 24, labelKey: '近 1 天' },
  { value: 72, labelKey: '近 3 天' },
  { value: 168, labelKey: '近 7 天' },
];

const RiskRankings = () => {
  const { t } = useTranslation();
  const [metric, setMetric] = useState('ip_multi_user');
  const [hours, setHours] = useState(24);
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [meta, setMeta] = useState({});

  // 下钻明细弹窗
  const [detailVisible, setDetailVisible] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailItems, setDetailItems] = useState([]);
  const [detailTitle, setDetailTitle] = useState('');
  const [detailType, setDetailType] = useState('ip');

  const loadRankings = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get(
        `/api/risk/rankings?metric=${metric}&hours=${hours}&limit=100`,
      );
      const { success, message, data } = res.data;
      if (success) {
        setItems(data?.items || []);
        setMeta(data?.meta || {});
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
    setLoading(false);
  }, [metric, hours]);

  useEffect(() => {
    loadRankings();
  }, [loadRankings]);

  const disableUser = async (userId) => {
    try {
      const res = await API.post('/api/user/manage', {
        id: userId,
        action: 'disable',
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('用户已禁用'));
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
  };

  const openIpDetail = async (ip) => {
    setDetailType('ip');
    setDetailTitle(t('IP 关联用户明细') + `: ${ip}`);
    setDetailVisible(true);
    setDetailLoading(true);
    try {
      const res = await API.get(
        `/api/risk/detail?type=ip&value=${encodeURIComponent(ip)}&hours=${hours}`,
      );
      const { success, message, data } = res.data;
      if (success) {
        setDetailItems(data?.items || []);
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
    setDetailLoading(false);
  };

  const openUserDetail = async (userId, username) => {
    setDetailType('user');
    setDetailTitle(t('用户使用 IP 明细') + `: ${username || userId}`);
    setDetailVisible(true);
    setDetailLoading(true);
    try {
      const res = await API.get(
        `/api/risk/detail?type=user&value=${userId}&hours=${hours}`,
      );
      const { success, message, data } = res.data;
      if (success) {
        setDetailItems(data?.items || []);
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
    setDetailLoading(false);
  };

  const ipColumns = [
    { title: 'IP', dataIndex: 'ip' },
    {
      title: t('关联用户数'),
      dataIndex: 'user_count',
      render: (v) => <Tag color={v > 5 ? 'red' : 'blue'}>{v}</Tag>,
      sorter: (a, b) => a.user_count - b.user_count,
    },
    { title: t('请求数'), dataIndex: 'request_count' },
    {
      title: t('操作'),
      render: (_, record) => (
        <Button theme='light' size='small' onClick={() => openIpDetail(record.ip)}>
          {t('查看用户')}
        </Button>
      ),
    },
  ];

  const userColumns = [
    { title: t('用户 ID'), dataIndex: 'user_id' },
    { title: t('用户名'), dataIndex: 'username' },
    {
      title: t('使用 IP 数'),
      dataIndex: 'ip_count',
      render: (v) => <Tag color={v > 5 ? 'red' : 'blue'}>{v}</Tag>,
      sorter: (a, b) => a.ip_count - b.ip_count,
    },
    { title: t('请求数'), dataIndex: 'request_count' },
    {
      title: t('操作'),
      render: (_, record) => (
        <Space>
          <Button
            theme='light'
            size='small'
            onClick={() => openUserDetail(record.user_id, record.username)}
          >
            {t('查看 IP')}
          </Button>
          <Popconfirm
            title={t('确定禁用该用户？')}
            content={`${record.username} (#${record.user_id})`}
            onConfirm={() => disableUser(record.user_id)}
          >
            <Button theme='light' type='danger' size='small'>
              {t('禁用用户')}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const uaColumns = [
    { title: 'User-Agent', dataIndex: 'ua', ellipsis: true },
    {
      title: t('用户数'),
      dataIndex: 'user_count',
      sorter: (a, b) => a.user_count - b.user_count,
    },
    { title: t('请求数'), dataIndex: 'request_count' },
  ];

  const columnsMap = {
    ip_multi_user: ipColumns,
    user_multi_ip: userColumns,
    ua: uaColumns,
  };

  const detailColumns =
    detailType === 'ip'
      ? [
          { title: t('用户 ID'), dataIndex: 'user_id' },
          { title: t('用户名'), dataIndex: 'username' },
          { title: t('请求数'), dataIndex: 'request_count' },
          {
            title: t('首次'),
            dataIndex: 'first_seen',
            render: (v) => timestamp2string(v),
          },
          {
            title: t('最近'),
            dataIndex: 'last_seen',
            render: (v) => timestamp2string(v),
          },
          {
            title: t('操作'),
            render: (_, record) => (
              <Popconfirm
                title={t('确定禁用该用户？')}
                content={`${record.username} (#${record.user_id})`}
                onConfirm={() => disableUser(record.user_id)}
              >
                <Button theme='light' type='danger' size='small'>
                  {t('禁用用户')}
                </Button>
              </Popconfirm>
            ),
          },
        ]
      : [
          { title: 'IP', dataIndex: 'ip' },
          { title: t('请求数'), dataIndex: 'request_count' },
          {
            title: t('首次'),
            dataIndex: 'first_seen',
            render: (v) => timestamp2string(v),
          },
          {
            title: t('最近'),
            dataIndex: 'last_seen',
            render: (v) => timestamp2string(v),
          },
        ];

  const showLogWarning =
    meta && (meta.ip_log_enabled === false || meta.ua_log_enabled === false);

  return (
    <>
      {showLogWarning && (
        <Banner
          type='warning'
          className='mb-4'
          description={t(
            '风控依赖 IP / User-Agent 日志记录。当前部分记录开关未开启，排行榜数据可能不完整。请前往「运营设置」开启全局 IP / UA 记录。',
          )}
        />
      )}
      <Space className='mb-4'>
        <Text>{t('时间范围')}</Text>
        <Select value={hours} onChange={setHours} style={{ width: 140 }}>
          {HOURS_OPTIONS.map((o) => (
            <Select.Option key={o.value} value={o.value}>
              {t(o.labelKey)}
            </Select.Option>
          ))}
        </Select>
        <Button icon={<IconRefresh />} onClick={loadRankings}>
          {t('刷新')}
        </Button>
      </Space>

      <Tabs
        type='button'
        activeKey={metric}
        onChange={(k) => setMetric(k)}
      >
        <TabPane tab={t('单 IP 多用户')} itemKey='ip_multi_user' />
        <TabPane tab={t('单用户多 IP')} itemKey='user_multi_ip' />
        <TabPane tab={t('UA 排行')} itemKey='ua' />
      </Tabs>

      <Table
        columns={columnsMap[metric]}
        dataSource={items}
        loading={loading}
        rowKey={(record) =>
          record.ip || record.user_id || record.ua || JSON.stringify(record)
        }
        pagination={{ pageSize: 20 }}
        empty={t('暂无数据')}
      />

      <Modal
        title={detailTitle}
        visible={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={null}
        width={720}
      >
        <Table
          columns={detailColumns}
          dataSource={detailItems}
          loading={detailLoading}
          rowKey={(record) =>
            record.ip || record.user_id || JSON.stringify(record)
          }
          pagination={false}
          empty={t('暂无数据')}
        />
      </Modal>
    </>
  );
};

export default RiskRankings;
