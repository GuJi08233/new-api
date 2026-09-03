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

// 三个排行维度共用同一组数值口径,差别只在分组对象。
const METRIC_USER = 'user_overview';
const METRIC_IP = 'ip_overview';
const METRIC_UA = 'ua';

// 排行榜默认排序:请求数降序。
const DEFAULT_SORT = { by: 'request_count', order: 'descend' };

// 一次取回的行数。服务端按当前排序字段截断,前端再本地分页,
// 因此换列排序会重新取数,保证 top N 始终取自被排序的那个维度。
const RANKING_LIMIT = 200;

// 数值单元格:达到警示/危险阈值时染色,便于横向扫视同一行的多个指标。
const countCell = (value, warn, danger) => {
  if (!value) {
    return <Text type='tertiary'>0</Text>;
  }
  const color = value >= danger ? 'red' : value >= warn ? 'orange' : 'blue';
  return <Tag color={color}>{value}</Tag>;
};

// 详情各分区的行都能从这些字段里取到稳定的键
const detailRowKey = (record) =>
  record.ip ||
  record.ua ||
  record.user_id ||
  `${record.status_code}:${record.error_code}`;

const RiskRankings = () => {
  const { t } = useTranslation();
  const [metric, setMetric] = useState(METRIC_USER);
  const [hours, setHours] = useState(24);
  const [excludeWhitelist, setExcludeWhitelist] = useState(false);
  const [sort, setSort] = useState(DEFAULT_SORT);
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [meta, setMeta] = useState({});

  // 下钻详情弹窗:主关联列表 + 该维度的补充明细 + 错误状态码分布
  const [detailVisible, setDetailVisible] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailTitle, setDetailTitle] = useState('');
  const [detailType, setDetailType] = useState('user');
  const [detailTab, setDetailTab] = useState('items');
  const [detailData, setDetailData] = useState({});

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
      const params = new URLSearchParams({
        metric,
        hours: String(hours),
        limit: String(RANKING_LIMIT),
        sort_by: sort.by,
        sort_order: sort.order === 'ascend' ? 'asc' : 'desc',
      });
      if (excludeWhitelist) params.set('exclude_whitelist', 'true');

      const res = await API.get(`/api/risk/rankings?${params.toString()}`);
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
  }, [metric, hours, excludeWhitelist, sort]);

  useEffect(() => {
    loadRankings();
  }, [loadRankings]);

  const switchMetric = (key) => {
    // 切换维度时清空数据并重置排序:各维度的可排序字段并不相同
    setItems([]);
    setSort(DEFAULT_SORT);
    setMetric(key);
  };

  const handleTableChange = ({ sorter }) => {
    if (!sorter || !sorter.dataIndex) return;
    const { dataIndex, sortOrder } = sorter;
    // 分页等其它变更也会触发 onChange,排序未变时不重复请求
    if (!sortOrder) {
      if (sort.by !== DEFAULT_SORT.by || sort.order !== DEFAULT_SORT.order) {
        setSort(DEFAULT_SORT);
      }
      return;
    }
    if (sort.by === dataIndex && sort.order === sortOrder) return;
    setSort({ by: dataIndex, order: sortOrder });
  };

  const openDetail = async (type, value, title) => {
    setDetailType(type);
    setDetailTitle(title);
    setDetailTab('items');
    setDetailData({});
    setDetailVisible(true);
    setDetailLoading(true);
    try {
      const params = new URLSearchParams({
        type,
        value: String(value),
        hours: String(hours),
      });
      if (excludeWhitelist) params.set('exclude_whitelist', 'true');

      const res = await API.get(`/api/risk/detail?${params.toString()}`);
      const { success, message, data } = res.data;
      if (success) {
        setDetailData(data || {});
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

  const detailButton = (onClick) => (
    <Button theme='light' size='small' onClick={onClick}>
      {t('查看详情')}
    </Button>
  );

  const banButton = (userId, username) => (
    <Button
      theme='light'
      type='danger'
      size='small'
      onClick={() => {
        setBanTarget({ user_id: userId, username });
        setBanReason('');
      }}
    >
      {t('禁用用户')}
    </Button>
  );

  // 服务端排序列:sortOrder 受控,点击列头由 handleTableChange 转成新的查询参数。
  const sortableCount = (dataIndex, title, width, warn, danger) => ({
    title,
    dataIndex,
    width,
    sorter: true,
    sortOrder: sort.by === dataIndex ? sort.order : false,
    render: (v) => countCell(v, warn, danger),
  });

  const requestCountColumn = {
    title: t('请求数'),
    dataIndex: 'request_count',
    width: 110,
    sorter: true,
    sortOrder: sort.by === 'request_count' ? sort.order : false,
  };

  const lastSeenColumn = {
    title: t('最近活跃'),
    dataIndex: 'last_seen',
    width: 170,
    sorter: true,
    sortOrder: sort.by === 'last_seen' ? sort.order : false,
    render: (v) => (v ? timestamp2string(v) : '-'),
  };

  const userColumns = [
    {
      title: t('用户'),
      dataIndex: 'username',
      width: 180,
      fixed: 'left',
      render: (v, record) => `${v || '-'} (#${record.user_id})`,
    },
    requestCountColumn,
    sortableCount('ip_count', t('IP 数'), 110, 5, 15),
    sortableCount('token_count', t('令牌数'), 110, 5, 20),
    sortableCount('tiny_request_count', t('微量请求'), 120, 20, 100),
    sortableCount('error_count', t('错误数'), 110, 20, 100),
    lastSeenColumn,
    {
      title: t('操作'),
      width: 190,
      fixed: 'right',
      render: (_, record) => (
        <Space>
          {detailButton(() =>
            openDetail(
              'user',
              record.user_id,
              `${t('用户明细')}: ${record.username || record.user_id}`,
            ),
          )}
          {banButton(record.user_id, record.username)}
        </Space>
      ),
    },
  ];

  const ipColumns = [
    {
      title: 'IP',
      dataIndex: 'ip',
      width: 200,
      fixed: 'left',
      render: (v) => <IpTag ip={v} />,
    },
    requestCountColumn,
    sortableCount('user_count', t('用户数'), 110, 2, 5),
    sortableCount('token_count', t('令牌数'), 110, 5, 20),
    sortableCount('tiny_request_count', t('微量请求'), 120, 20, 100),
    sortableCount('error_count', t('错误数'), 110, 20, 100),
    lastSeenColumn,
    {
      title: t('操作'),
      width: 190,
      fixed: 'right',
      render: (_, record) => (
        <Space>
          {detailButton(() =>
            openDetail('ip', record.ip, `${t('IP 明细')}: ${record.ip}`),
          )}
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

  const uaColumns = [
    {
      title: 'User-Agent',
      dataIndex: 'ua',
      width: 260,
      fixed: 'left',
      ellipsis: { showTitle: true },
    },
    requestCountColumn,
    sortableCount('user_count', t('用户数'), 110, 3, 10),
    sortableCount('ip_count', t('IP 数'), 110, 5, 20),
    sortableCount('token_count', t('令牌数'), 110, 5, 20),
    sortableCount('tiny_request_count', t('微量请求'), 120, 20, 100),
    sortableCount('error_count', t('错误数'), 110, 20, 100),
    lastSeenColumn,
    {
      title: t('操作'),
      width: 120,
      fixed: 'right',
      render: (_, record) =>
        detailButton(() =>
          openDetail('ua', record.ua, `${t('UA 明细')}: ${record.ua}`),
        ),
    },
  ];

  const columnsMap = {
    [METRIC_USER]: userColumns,
    [METRIC_IP]: ipColumns,
    [METRIC_UA]: uaColumns,
  };

  const seenColumns = [
    {
      title: t('首次'),
      dataIndex: 'first_seen',
      width: 170,
      render: (v) => timestamp2string(v),
    },
    {
      title: t('最近'),
      dataIndex: 'last_seen',
      width: 170,
      render: (v) => timestamp2string(v),
    },
  ];

  const detailUserColumns = [
    {
      title: t('用户'),
      dataIndex: 'username',
      render: (v, record) => `${v || '-'} (#${record.user_id})`,
    },
    { title: t('请求数'), dataIndex: 'request_count', width: 110 },
    ...seenColumns,
    {
      title: t('操作'),
      width: 120,
      render: (_, record) => banButton(record.user_id, record.username),
    },
  ];

  const detailIpColumns = [
    { title: 'IP', dataIndex: 'ip', render: (v) => <IpTag ip={v} /> },
    { title: t('请求数'), dataIndex: 'request_count', width: 110 },
    ...seenColumns,
  ];

  const detailUaColumns = [
    { title: 'User-Agent', dataIndex: 'ua', ellipsis: { showTitle: true } },
    { title: t('请求数'), dataIndex: 'request_count', width: 110 },
    ...seenColumns,
  ];

  const detailErrorColumns = [
    {
      title: t('状态码'),
      dataIndex: 'status_code',
      width: 120,
      render: (v) => (
        <Tag color={v >= 500 ? 'red' : v >= 400 ? 'orange' : 'blue'}>
          {v || '-'}
        </Tag>
      ),
    },
    { title: t('错误码'), dataIndex: 'error_code', ellipsis: true },
    { title: t('次数'), dataIndex: 'count', width: 110 },
  ];

  // 各维度的详情分区:主关联列表在前,补充明细居中,错误分布收尾
  const primaryDetailTab = {
    user: { label: t('IP 明细'), columns: detailIpColumns },
    ip: { label: t('关联用户'), columns: detailUserColumns },
    ua: { label: t('关联用户'), columns: detailUserColumns },
  }[detailType];

  const detailTabs = [
    { key: 'items', ...primaryDetailTab, rows: detailData.items },
    detailType === 'ua'
      ? {
          key: 'ips',
          label: t('IP 明细'),
          columns: detailIpColumns,
          rows: detailData.ips,
        }
      : {
          key: 'uas',
          label: t('UA 明细'),
          columns: detailUaColumns,
          rows: detailData.uas,
        },
    {
      key: 'errors',
      label: t('错误状态码'),
      columns: detailErrorColumns,
      rows: detailData.errors,
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
      <Space className='mb-4' wrap>
        <Text>{t('时间范围')}</Text>
        <Select value={hours} onChange={setHours} style={{ width: 140 }}>
          {HOURS_OPTIONS.map((o) => (
            <Select.Option key={o.value} value={o.value}>
              {t(o.labelKey)}
            </Select.Option>
          ))}
        </Select>
        <Text>{t('过滤全局白名单')}</Text>
        <Switch
          checked={excludeWhitelist}
          onChange={setExcludeWhitelist}
          size='small'
          aria-label={t('过滤全局白名单')}
        />
        <Text type='tertiary' size='small'>
          {meta.whitelist_count
            ? t('已配置 {{count}} 个全局白名单账号', {
                count: meta.whitelist_count,
              })
            : t('风控设置中尚未配置全局白名单账号')}
        </Text>
        <Button icon={<IconRefresh />} onClick={loadRankings}>
          {t('刷新')}
        </Button>
      </Space>

      <Tabs type='button' activeKey={metric} onChange={switchMetric}>
        <TabPane tab={t('用户排行')} itemKey={METRIC_USER} />
        <TabPane tab={t('IP 排行')} itemKey={METRIC_IP} />
        <TabPane tab={t('UA 排行')} itemKey={METRIC_UA} />
      </Tabs>

      {metric === METRIC_USER && (
        <Text type='tertiary' size='small' className='block mb-2'>
          {t(
            'IP 数明显高于令牌数,说明单个令牌被大量 IP 使用,通常意味着密钥已泄露或被倒卖;微量请求与错误数偏高则是脚本测活的典型特征。微量请求指输入与输出 tokens 均不超过 {{count}} 的成功请求,该阈值可在风控设置中调整。',
            { count: meta.tiny_request_max_tokens ?? 16 },
          )}
        </Text>
      )}
      {metric === METRIC_IP && (
        <Text type='tertiary' size='small' className='block mb-2'>
          {t(
            '单个 IP 短时间关联大量用户或令牌,是批量测活、号商倒卖的典型特征;错误数偏高说明大量密钥已失效。',
          )}
        </Text>
      )}
      {metric === METRIC_UA && (
        <Text type='tertiary' size='small' className='block mb-2'>
          {t(
            '同一客户端标识覆盖大量用户与 IP,通常是脚本或代理工具。确认可疑后可在风控设置中把它加入 UA 黑名单。',
          )}
        </Text>
      )}
      <Text type='tertiary' size='small' className='block mb-2'>
        {t('点击列头切换排序,榜单会按该指标重新从服务端取前 {{count}} 名。', {
          count: RANKING_LIMIT,
        })}
      </Text>

      <Table
        columns={columnsMap[metric]}
        dataSource={items}
        loading={loading}
        onChange={handleTableChange}
        scroll={{ x: 1240 }}
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
        width={900}
      >
        <Tabs type='line' activeKey={detailTab} onChange={setDetailTab}>
          {detailTabs.map((tab) => (
            <TabPane tab={tab.label} itemKey={tab.key} key={tab.key}>
              {tab.key === 'errors' && detailData.errors_sampled && (
                <Banner
                  type='info'
                  className='mb-2'
                  description={t(
                    '错误数量较多,分布基于最近的错误样本统计,不代表窗口内全量。',
                  )}
                />
              )}
              <Table
                columns={tab.columns}
                dataSource={tab.rows || []}
                loading={detailLoading}
                rowKey={detailRowKey}
                pagination={{ pageSize: 10 }}
                empty={t('暂无数据')}
              />
            </TabPane>
          ))}
        </Tabs>
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
