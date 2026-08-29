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
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Tabs,
  TabPane,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';

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

  // 带原因的封禁弹窗
  const [banTarget, setBanTarget] = useState(null); // {user_id, username}
  const [banReason, setBanReason] = useState('');
  const [banning, setBanning] = useState(false);

  // 封禁 IP 弹窗
  const [banIpTarget, setBanIpTarget] = useState(''); // 要封禁的 IP
  const [banIpReason, setBanIpReason] = useState('');
  const [banIpMinutes, setBanIpMinutes] = useState(0);
  const [banningIp, setBanningIp] = useState(false);

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

  const openBanModal = (userId, username) => {
    setBanTarget({ user_id: userId, username });
    setBanReason('');
  };

  const confirmBan = async () => {
    if (!banTarget) return;
    setBanning(true);
    try {
      const res = await API.post('/api/user/manage', {
        id: banTarget.user_id,
        action: 'disable',
        reason: banReason.trim(),
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('用户已禁用'));
        setBanTarget(null);
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
    setBanning(false);
  };

  const confirmBanIp = async () => {
    if (!banIpTarget) return;
    setBanningIp(true);
    try {
      const res = await API.post('/api/risk/ip_bans', {
        target: banIpTarget,
        reason: banIpReason.trim(),
        expire_minutes: banIpMinutes > 0 ? banIpMinutes : 0,
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('IP 已封禁'));
        setBanIpTarget('');
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
    setBanningIp(false);
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

  const banButton = (userId, username) => (
    <Button
      theme='light'
      type='danger'
      size='small'
      onClick={() => openBanModal(userId, username)}
    >
      {t('禁用用户')}
    </Button>
  );

  const userActions = (record) => (
    <Space>
      <Button
        theme='light'
        size='small'
        onClick={() => openUserDetail(record.user_id, record.username)}
      >
        {t('查看 IP')}
      </Button>
      {banButton(record.user_id, record.username)}
    </Space>
  );

  const ipActions = (ip) => (
    <Space>
      <Button theme='light' size='small' onClick={() => openIpDetail(ip)}>
        {t('查看用户')}
      </Button>
      <Button
        theme='light'
        type='danger'
        size='small'
        onClick={() => {
          setBanIpTarget(ip);
          setBanIpReason('');
          setBanIpMinutes(0);
        }}
      >
        {t('封禁 IP')}
      </Button>
    </Space>
  );

  const ipColumns = [
    {
      title: 'IP',
      dataIndex: 'ip',
      render: (v) => <IpTag ip={v} />,
    },
    {
      title: t('关联用户数'),
      dataIndex: 'user_count',
      render: (v) => (
        <Tag color={v > 5 ? 'red' : v > 1 ? 'orange' : 'blue'}>{v}</Tag>
      ),
      sorter: (a, b) => a.user_count - b.user_count,
    },
    { title: t('请求数'), dataIndex: 'request_count' },
    {
      title: t('操作'),
      render: (_, record) => ipActions(record.ip),
    },
  ];

  const ipTokenColumns = [
    {
      title: 'IP',
      dataIndex: 'ip',
      render: (v) => <IpTag ip={v} />,
    },
    {
      title: t('使用令牌数'),
      dataIndex: 'token_count',
      render: (v) => (
        <Tag color={v > 5 ? 'red' : v > 1 ? 'orange' : 'blue'}>{v}</Tag>
      ),
      sorter: (a, b) => a.token_count - b.token_count,
    },
    { title: t('关联用户数'), dataIndex: 'user_count' },
    { title: t('请求数'), dataIndex: 'request_count' },
    {
      title: t('操作'),
      render: (_, record) => ipActions(record.ip),
    },
  ];

  const tokenIpColumns = [
    {
      title: t('令牌'),
      dataIndex: 'token_name',
      render: (v, record) => `${v || '-'} (#${record.token_id})`,
    },
    {
      title: t('所属用户'),
      dataIndex: 'username',
      render: (v, record) => `${v || '-'} (#${record.user_id})`,
    },
    {
      title: t('使用 IP 数'),
      dataIndex: 'ip_count',
      render: (v) => (
        <Tag color={v > 10 ? 'red' : v > 3 ? 'orange' : 'blue'}>{v}</Tag>
      ),
      sorter: (a, b) => a.ip_count - b.ip_count,
    },
    { title: t('请求数'), dataIndex: 'request_count' },
    {
      title: t('操作'),
      render: (_, record) => userActions(record),
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
      render: (_, record) => userActions(record),
    },
  ];

  const tinyRequestColumns = [
    { title: t('用户 ID'), dataIndex: 'user_id' },
    { title: t('用户名'), dataIndex: 'username' },
    {
      title: t('微量请求数'),
      dataIndex: 'request_count',
      render: (v) => (
        <Tag color={v > 100 ? 'red' : v > 20 ? 'orange' : 'blue'}>{v}</Tag>
      ),
      sorter: (a, b) => a.request_count - b.request_count,
    },
    { title: t('使用令牌数'), dataIndex: 'token_count' },
    {
      title: t('操作'),
      render: (_, record) => userActions(record),
    },
  ];

  const errorBurstColumns = [
    { title: t('用户 ID'), dataIndex: 'user_id' },
    { title: t('用户名'), dataIndex: 'username' },
    {
      title: t('错误请求数'),
      dataIndex: 'request_count',
      render: (v) => (
        <Tag color={v > 100 ? 'red' : v > 20 ? 'orange' : 'blue'}>{v}</Tag>
      ),
      sorter: (a, b) => a.request_count - b.request_count,
    },
    {
      title: t('操作'),
      render: (_, record) => userActions(record),
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
    ip_multi_token: ipTokenColumns,
    user_multi_ip: userColumns,
    user_tiny_request: tinyRequestColumns,
    user_error_burst: errorBurstColumns,
    token_multi_ip: tokenIpColumns,
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
            render: (_, record) => banButton(record.user_id, record.username),
          },
        ]
      : [
          {
            title: 'IP',
            dataIndex: 'ip',
            render: (v) => <IpTag ip={v} />,
          },
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

      <Tabs type='button' activeKey={metric} onChange={(k) => setMetric(k)}>
        <TabPane tab={t('IP 排行')} itemKey='ip_multi_user' />
        <TabPane tab={t('单用户多 IP')} itemKey='user_multi_ip' />
        <TabPane tab={t('单 IP 多令牌')} itemKey='ip_multi_token' />
        <TabPane tab={t('令牌多 IP(泄露)')} itemKey='token_multi_ip' />
        <TabPane tab={t('微量请求(测活)')} itemKey='user_tiny_request' />
        <TabPane tab={t('错误爆发')} itemKey='user_error_burst' />
        <TabPane tab={t('UA 排行')} itemKey='ua' />
      </Tabs>

      {metric === 'token_multi_ip' && (
        <Text type='tertiary' size='small' className='block mb-2'>
          {t(
            '单个令牌短时间被大量不同 IP 使用,通常意味着密钥已泄露或被倒卖,建议联系所属用户或直接禁用。',
          )}
        </Text>
      )}

      {metric === 'user_tiny_request' && (
        <Text type='tertiary' size='small' className='block mb-2'>
          {t(
            '微量请求指输入与输出 tokens 均不超过判定阈值的成功请求,是脚本自动测活的典型特征。当前阈值:{{count}} tokens,可在风控设置中调整。',
            { count: meta.tiny_request_max_tokens ?? 16 },
          )}
        </Text>
      )}
      {metric === 'ip_multi_token' && (
        <Text type='tertiary' size='small' className='block mb-2'>
          {t('单个 IP 短时间使用大量不同令牌,是批量测活/号商倒卖的典型特征。')}
        </Text>
      )}

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

      <Modal
        title={t('禁用用户')}
        visible={!!banTarget}
        onOk={confirmBan}
        onCancel={() => setBanTarget(null)}
        okType='danger'
        okText={t('确认禁用')}
        cancelText={t('取消')}
        confirmLoading={banning}
      >
        <Space vertical align='start' style={{ width: '100%' }}>
          <Text>
            {banTarget
              ? `${banTarget.username || ''} (#${banTarget.user_id})`
              : ''}
          </Text>
          <Text type='tertiary'>
            {t('封禁原因将写入风控事件记录,便于日后审计与解封时参考。')}
          </Text>
          <TextArea
            value={banReason}
            onChange={setBanReason}
            placeholder={t('封禁原因(可选)')}
            rows={3}
          />
        </Space>
      </Modal>

      <Modal
        title={t('封禁 IP')}
        visible={!!banIpTarget}
        onOk={confirmBanIp}
        onCancel={() => setBanIpTarget('')}
        okType='danger'
        okText={t('确认封禁')}
        cancelText={t('取消')}
        confirmLoading={banningIp}
      >
        <Space vertical align='start' style={{ width: '100%' }}>
          <Text>{banIpTarget}</Text>
          <Text>{t('封禁时长(分钟,0 或留空为永久)')}</Text>
          <InputNumber
            value={banIpMinutes}
            min={0}
            max={43200}
            style={{ width: 160 }}
            onChange={(v) => setBanIpMinutes(v > 0 ? v : 0)}
          />
          <TextArea
            value={banIpReason}
            onChange={setBanIpReason}
            placeholder={t('封禁原因(可选)')}
            rows={3}
          />
          <Text type='tertiary' size='small'>
            {t('封禁后该 IP 的所有请求将被拒绝,可在「IP 封禁」页解除。')}
          </Text>
        </Space>
      </Modal>
    </>
  );
};

export default RiskRankings;
