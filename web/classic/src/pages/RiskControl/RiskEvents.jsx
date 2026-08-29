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
  Button,
  Input,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';

const { Text } = Typography;

const PAGE_SIZE = 20;

// 事件类型 → 展示标签与颜色。labelKey 为 i18n 键。
const EVENT_TYPE_META = {
  block_ua: { labelKey: 'UA 拦截', color: 'orange' },
  block_ip: { labelKey: 'IP 拦截', color: 'red' },
  ban_auto: { labelKey: '自动封禁', color: 'red' },
  ban_manual: { labelKey: '手动封禁', color: 'purple' },
  unban: { labelKey: '解除封禁', color: 'green' },
  ban_ip: { labelKey: 'IP 封禁', color: 'red' },
  unban_ip: { labelKey: 'IP 解封', color: 'green' },
  alert: { labelKey: '规则告警', color: 'amber' },
};

const RiskEvents = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);

  // 筛选条件
  const [eventType, setEventType] = useState('');
  const [userIdText, setUserIdText] = useState('');
  const [ipText, setIpText] = useState('');

  const loadEvents = useCallback(
    async (targetPage) => {
      setLoading(true);
      try {
        const params = new URLSearchParams({
          p: String(targetPage),
          page_size: String(PAGE_SIZE),
        });
        if (eventType) params.set('event_type', eventType);
        const userId = parseInt(userIdText, 10);
        if (Number.isFinite(userId) && userId > 0) {
          params.set('user_id', String(userId));
        }
        if (ipText.trim()) params.set('ip', ipText.trim());

        const res = await API.get(`/api/risk/events?${params.toString()}`);
        const { success, message, data } = res.data;
        if (success) {
          setItems(data?.items || []);
          setTotal(data?.total || 0);
          setPage(targetPage);
        } else {
          showError(message);
        }
      } catch (e) {
        showError(e.message);
      }
      setLoading(false);
    },
    [eventType, userIdText, ipText],
  );

  useEffect(() => {
    loadEvents(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eventType]);

  const columns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      width: 160,
      render: (v) => timestamp2string(v),
    },
    {
      title: t('类型'),
      dataIndex: 'event_type',
      width: 110,
      render: (v) => {
        const meta = EVENT_TYPE_META[v];
        if (!meta) return <Tag>{v}</Tag>;
        return <Tag color={meta.color}>{t(meta.labelKey)}</Tag>;
      },
    },
    {
      title: t('用户'),
      dataIndex: 'user_id',
      width: 150,
      render: (userId, record) => {
        if (!userId) return '-';
        return record.username
          ? `${record.username} (#${userId})`
          : `#${userId}`;
      },
    },
    {
      title: 'IP',
      dataIndex: 'ip',
      width: 160,
      render: (v) => (v ? <IpTag ip={v} /> : '-'),
    },
    {
      title: 'User-Agent',
      dataIndex: 'ua',
      width: 200,
      ellipsis: { showTitle: true },
      render: (v) => v || '-',
    },
    {
      title: t('命中规则'),
      dataIndex: 'rule',
      width: 160,
      ellipsis: { showTitle: true },
      render: (v) => (v ? <Text code>{v}</Text> : '-'),
    },
    {
      title: t('原因'),
      dataIndex: 'reason',
      ellipsis: { showTitle: true },
      render: (v) => v || '-',
    },
    {
      title: t('次数'),
      dataIndex: 'count',
      width: 80,
      render: (v) => (v > 1 ? <Tag color='red'>{v}</Tag> : v),
    },
    {
      title: t('操作人'),
      dataIndex: 'operator_id',
      width: 90,
      render: (v) => (v > 0 ? `#${v}` : '-'),
    },
  ];

  return (
    <>
      <Space className='mb-4' wrap>
        <Text>{t('类型')}</Text>
        <Select
          value={eventType}
          onChange={(v) => setEventType(v)}
          style={{ width: 150 }}
        >
          <Select.Option value=''>{t('全部类型')}</Select.Option>
          {Object.entries(EVENT_TYPE_META).map(([value, meta]) => (
            <Select.Option key={value} value={value}>
              {t(meta.labelKey)}
            </Select.Option>
          ))}
        </Select>
        <Input
          value={userIdText}
          onChange={setUserIdText}
          placeholder={t('用户 ID')}
          style={{ width: 120 }}
        />
        <Input
          value={ipText}
          onChange={setIpText}
          placeholder='IP'
          style={{ width: 160 }}
        />
        <Button icon={<IconSearch />} onClick={() => loadEvents(1)}>
          {t('查询')}
        </Button>
        <Button icon={<IconRefresh />} onClick={() => loadEvents(page)}>
          {t('刷新')}
        </Button>
      </Space>

      <Text type='tertiary' size='small' className='block mb-2'>
        {t(
          '拦截事件按来源聚合记录,「次数」为聚合窗口内的累计命中数;封禁与解禁记录永久保留,拦截与告警记录按设置的保留天数自动清理。',
        )}
      </Text>

      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey='id'
        pagination={{
          currentPage: page,
          pageSize: PAGE_SIZE,
          total,
          onPageChange: (p) => loadEvents(p),
        }}
        empty={t('暂无数据')}
      />
    </>
  );
};

export default RiskEvents;
