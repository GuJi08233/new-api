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
  Switch,
  Table,
  Tag,
  TextArea,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';

const { Text } = Typography;

// 账号事件口径每个账号只留寥寥数行，可以回溯很久；并入调用日志后行数涨几个数量级，
// 后端会把窗口钳制到 7 天，这里的选项也随之收窄。
const ACCOUNT_HOURS_OPTIONS = [
  { value: 168, labelKey: '近 7 天' },
  { value: 720, labelKey: '近 30 天' },
  { value: 2160, labelKey: '近 90 天' },
];
const REQUEST_HOURS_OPTIONS = [
  { value: 24, labelKey: '近 1 天' },
  { value: 72, labelKey: '近 3 天' },
  { value: 168, labelKey: '近 7 天' },
];

const RANKING_LIMIT = 200;

// 关联账号数染色：2 个可能是家人同网，5 个以上基本是批量注册。
const userCountCell = (value) => {
  if (!value) return <Text type='tertiary'>0</Text>;
  const color = value >= 5 ? 'red' : value >= 3 ? 'orange' : 'blue';
  return <Tag color={color}>{value}</Tag>;
};

const countCell = (value, highlight) => {
  if (!value) return <Text type='tertiary'>0</Text>;
  return <Tag color={highlight ? 'red' : 'blue'}>{value}</Tag>;
};

// 注册来源最能说明账号是谁开的，其余绑定可能是事后补绑的；没有标记时退回第一条。
const primaryBinding = (bindings) =>
  bindings.find((item) => item.is_registration) || bindings[0];

// OAuth 的用户标识可以很长，列表里截断展示，完整值放在悬浮提示里。
const shortIdentifier = (value) => {
  if (!value) return '';
  return value.length > 22 ? `${value.slice(0, 22)}…` : value;
};

const MultiAccount = () => {
  const { t } = useTranslation();
  const [hours, setHours] = useState(168);
  const [minUsers, setMinUsers] = useState(2);
  const [includeRequests, setIncludeRequests] = useState(false);
  const [excludeWhitelist, setExcludeWhitelist] = useState(true);
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [meta, setMeta] = useState({});

  const [detailIp, setDetailIp] = useState('');
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailItems, setDetailItems] = useState([]);
  const [bindingUser, setBindingUser] = useState(null); // 查看某个账号的全部绑定

  const [banTarget, setBanTarget] = useState(null); // {user_id, username}
  const [banReason, setBanReason] = useState('');
  const [banning, setBanning] = useState(false);

  const [banIpTarget, setBanIpTarget] = useState('');
  const [banIpReason, setBanIpReason] = useState('');
  const [banIpMinutes, setBanIpMinutes] = useState(0);
  const [banningIp, setBanningIp] = useState(false);

  const hoursOptions = includeRequests
    ? REQUEST_HOURS_OPTIONS
    : ACCOUNT_HOURS_OPTIONS;

  const buildParams = useCallback(() => {
    const params = new URLSearchParams({
      hours: String(hours),
      min_users: String(minUsers),
    });
    if (includeRequests) params.set('include_requests', 'true');
    if (excludeWhitelist) params.set('exclude_whitelist', 'true');
    return params;
  }, [hours, minUsers, includeRequests, excludeWhitelist]);

  const loadRanking = useCallback(async () => {
    setLoading(true);
    try {
      const params = buildParams();
      params.set('limit', String(RANKING_LIMIT));
      const res = await API.get(`/api/risk/multi_account?${params.toString()}`);
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
  }, [buildParams]);

  useEffect(() => {
    loadRanking();
  }, [loadRanking]);

  // 切换证据口径时窗口上限会变，把超出新上限的选择拉回最大可选值
  const switchIncludeRequests = (checked) => {
    setIncludeRequests(checked);
    if (checked && hours > 168) setHours(168);
  };

  const openDetail = async (ip) => {
    setDetailIp(ip);
    setDetailItems([]);
    setDetailLoading(true);
    try {
      const params = buildParams();
      params.set('ip', ip);
      const res = await API.get(
        `/api/risk/multi_account/detail?${params.toString()}`,
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
        if (detailIp) openDetail(detailIp);
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

  // 内置绑定的展示名。专有名词不进翻译表，其余显式调用 t 以便提取。
  const bindingTypeNames = {
    email: t('邮箱'),
    github: 'GitHub',
    discord: 'Discord',
    oidc: 'OIDC',
    wechat: t('微信'),
    telegram: 'Telegram',
    linuxdo: 'LinuxDO',
    steam: 'Steam',
  };

  const bindingName = (binding) =>
    binding.type === 'custom'
      ? binding.name || t('自定义 OAuth')
      : bindingTypeNames[binding.type] || binding.type;

  // 注册渠道取自注册日志：密码注册不对应任何绑定，其余复用绑定名。
  const registerMethodName = (method) => {
    if (!method) return '';
    if (method === 'password') return t('密码注册');
    return bindingTypeNames[method] || method;
  };

  const renderBindings = (bindings, record) => {
    if (record.can_manage === false) {
      return <Text type='tertiary'>{t('无权查看')}</Text>;
    }
    const list = bindings || [];
    if (list.length === 0) {
      const method = registerMethodName(record.register_method);
      return (
        <Text type='tertiary'>
          {method ? `${t('无绑定')}（${method}）` : t('无绑定')}
        </Text>
      );
    }
    const primary = primaryBinding(list);
    const label = bindingName(primary);
    return (
      <Space spacing={4} wrap>
        <Tooltip
          content={`${label}: ${primary.identifier || '-'}${
            primary.is_registration ? `（${t('注册来源')}）` : ''
          }`}
        >
          <Tag color={primary.is_registration ? 'red' : 'blue'}>
            {`${label} · ${shortIdentifier(primary.identifier) || '-'}`}
          </Tag>
        </Tooltip>
        {list.length > 1 && (
          <Button
            theme='borderless'
            size='small'
            onClick={() => setBindingUser(record)}
          >
            {t('+{{count}} 查看全部', { count: list.length - 1 })}
          </Button>
        )}
      </Space>
    );
  };

  const bindingColumns = [
    {
      title: t('绑定方式'),
      dataIndex: 'type',
      width: 160,
      render: (_, record) => (
        <Space spacing={4}>
          <Text>{bindingName(record)}</Text>
          {record.is_registration && <Tag color='red'>{t('注册来源')}</Tag>}
        </Space>
      ),
    },
    {
      title: t('标识'),
      dataIndex: 'identifier',
      ellipsis: { showTitle: true },
      render: (v) => (v ? <Text code>{v}</Text> : '-'),
    },
  ];

  const columns = [
    {
      title: 'IP',
      dataIndex: 'ip',
      width: 200,
      fixed: 'left',
      render: (v) => <IpTag ip={v} />,
    },
    {
      title: t('关联账号数'),
      dataIndex: 'user_count',
      width: 130,
      render: userCountCell,
    },
    {
      title: t('注册数'),
      dataIndex: 'register_count',
      width: 110,
      render: (v) => countCell(v, v >= 2),
    },
    { title: t('登录数'), dataIndex: 'login_count', width: 110 },
    { title: t('证据行数'), dataIndex: 'event_count', width: 110 },
    {
      title: t('首次'),
      dataIndex: 'first_seen',
      width: 170,
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
    {
      title: t('最近'),
      dataIndex: 'last_seen',
      width: 170,
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
    {
      title: t('操作'),
      width: 190,
      fixed: 'right',
      render: (_, record) => (
        <Space>
          <Button
            theme='light'
            size='small'
            onClick={() => openDetail(record.ip)}
          >
            {t('查看账号')}
          </Button>
          <Button
            theme='light'
            type='danger'
            size='small'
            onClick={() => {
              setBanIpTarget(record.ip);
              setBanIpReason('');
              setBanIpMinutes(0);
            }}
          >
            {t('封禁 IP')}
          </Button>
        </Space>
      ),
    },
  ];

  const detailColumns = [
    {
      title: t('用户'),
      dataIndex: 'username',
      render: (v, record) => (
        <Space>
          <Text>{`${v || '-'} (#${record.user_id})`}</Text>
          {record.status === 2 && <Tag color='red'>{t('已禁用')}</Tag>}
        </Space>
      ),
    },
    {
      title: t('账号绑定'),
      dataIndex: 'bindings',
      width: 260,
      render: renderBindings,
    },
    {
      title: t('在此注册'),
      dataIndex: 'register_count',
      width: 110,
      render: (v) =>
        v > 0 ? (
          <Tag color='red'>{t('是')}</Tag>
        ) : (
          <Text type='tertiary'>-</Text>
        ),
    },
    { title: t('登录数'), dataIndex: 'login_count', width: 100 },
    { title: t('请求数'), dataIndex: 'request_count', width: 100 },
    {
      title: t('首次'),
      dataIndex: 'first_seen',
      width: 170,
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
    {
      title: t('最近'),
      dataIndex: 'last_seen',
      width: 170,
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
    {
      title: t('操作'),
      width: 120,
      render: (_, record) =>
        record.status === 2 ? (
          <Text type='tertiary'>{t('已禁用')}</Text>
        ) : (
          <Button
            theme='light'
            type='danger'
            size='small'
            onClick={() => {
              setBanTarget({
                user_id: record.user_id,
                username: record.username,
              });
              setBanReason('');
            }}
          >
            {t('禁用用户')}
          </Button>
        ),
    },
  ];

  return (
    <>
      {meta.ip_log_enabled === false && includeRequests && (
        <Banner
          type='warning'
          className='mb-4'
          description={t(
            '全局 IP 日志未开启，调用记录不会留下 IP，含调用证据的统计会偏少。注册、登录、签到的来源始终记录，不受该开关影响。',
          )}
        />
      )}
      <Banner
        type='info'
        className='mb-4'
        description={t(
          '本页只做统计，不会自动封禁。同一出口地址下出现多个账号并不一定是同一人：家庭宽带、公司网络、校园网、机房出口都会造成多个正常账号共用地址。请结合「在此注册」与注册时间集中度判断后再决定处置。',
        )}
      />

      <Space className='mb-4' wrap>
        <Text>{t('时间范围')}</Text>
        <Select value={hours} onChange={setHours} style={{ width: 140 }}>
          {hoursOptions.map((o) => (
            <Select.Option key={o.value} value={o.value}>
              {t(o.labelKey)}
            </Select.Option>
          ))}
        </Select>
        <Text>{t('最少关联账号数')}</Text>
        <InputNumber
          value={minUsers}
          min={2}
          max={1000}
          style={{ width: 100 }}
          onChange={(v) => setMinUsers(v >= 2 ? v : 2)}
        />
        <Text>{t('计入调用记录')}</Text>
        <Switch
          checked={includeRequests}
          onChange={switchIncludeRequests}
          size='small'
          aria-label={t('计入调用记录')}
        />
        <Text>{t('过滤全局白名单')}</Text>
        <Switch
          checked={excludeWhitelist}
          onChange={setExcludeWhitelist}
          size='small'
          aria-label={t('过滤全局白名单')}
        />
        <Button icon={<IconRefresh />} onClick={loadRanking}>
          {t('刷新')}
        </Button>
      </Space>

      <Text type='tertiary' size='small' className='block mb-2'>
        {includeRequests
          ? t(
              '当前口径：注册、登录、签到、安全操作 + 调用与错误记录。调用日志量大，窗口最长 7 天。',
            )
          : t(
              '当前口径：注册、登录、签到、安全操作。这类记录每个账号只有寥寥数行，可回溯 90 天；打开「计入调用记录」可加入调用证据，但窗口会收窄到 7 天。',
            )}
      </Text>

      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        scroll={{ x: 1180 }}
        rowKey='ip'
        pagination={{ pageSize: 20 }}
        empty={t('暂无数据')}
      />

      <Modal
        title={`${t('关联账号')}: ${detailIp}`}
        visible={!!detailIp}
        onCancel={() => {
          setDetailIp('');
          setBindingUser(null);
        }}
        footer={null}
        width={900}
      >
        <Text type='tertiary' size='small' className='block mb-2'>
          {t(
            '「在此注册」为「是」表示该账号是在这个地址上注册的，是同一人多号最强的证据。',
          )}
        </Text>
        <Text type='tertiary' size='small' className='block mb-2'>
          {t(
            '「账号绑定」展示该账号的第三方身份，标红的是注册来源；绑定多条时只显示注册来源，点「查看全部」看其余绑定。同一身份来源（同邮箱域名、同一提供商下相邻的账号 ID）比共用出口地址更能说明是同一人。',
          )}
        </Text>
        <Table
          columns={detailColumns}
          dataSource={detailItems}
          loading={detailLoading}
          rowKey='user_id'
          scroll={{ x: 1080 }}
          pagination={{ pageSize: 10 }}
          empty={t('暂无数据')}
        />
      </Modal>

      <Modal
        title={
          bindingUser
            ? `${t('账号绑定')}: ${bindingUser.username || '-'} (#${bindingUser.user_id})`
            : t('账号绑定')
        }
        visible={!!bindingUser}
        onCancel={() => setBindingUser(null)}
        footer={null}
        width={640}
      >
        <Text type='tertiary' size='small' className='block mb-2'>
          {t('注册方式')}:{' '}
          {registerMethodName(bindingUser?.register_method) || t('未知')}
        </Text>
        <Table
          columns={bindingColumns}
          dataSource={bindingUser?.bindings || []}
          rowKey={(record) => `${record.type}:${record.identifier}`}
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

export default MultiAccount;
