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

import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';
import RiskUserState from './RiskUserState';
import { RiskBatchToolbar } from './RiskBatchModal';
import useRiskFilters, { riskInteger } from './useRiskFilters';

const { Text } = Typography;

// 跨账号共享会话 ID / 缓存关联键排查视图：
// 同一客户端会话或提示缓存键跨多个不同账号出现，即使来源 IP 不同也能识别共号/号池风险。
export default function SharedSessionRisk({
  kind,
  onInspect,
  onDisposition,
  onBatch,
  refreshKey,
}) {
  const { t } = useTranslation();
  const [filters, setFilters] = useRiskFilters(kind === 'cache' ? 'mc' : 'ms', {
    hours: 24,
    min: 2,
    exclude: true,
    sort: 'user_count',
    order: 'descend',
    p: 1,
    keyword: '',
  });
  const hours = riskInteger(filters.hours, 24, 1, 168);
  const minUsers = riskInteger(filters.min, 2, 2, 1000);
  const excludeWhitelist = filters.exclude === 'true';
  const page = riskInteger(filters.p, 1, 1, 1000000);
  const sort = {
    by: filters.sort || 'user_count',
    order: filters.order === 'ascend' ? 'ascend' : 'descend',
  };
  const [keyword, setKeyword] = useState(filters.keyword);
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [userStates, setUserStates] = useState({});
  const [meta, setMeta] = useState({});
  const [activeItem, setActiveItem] = useState(null);
  const [selectedUserIds, setSelectedUserIds] = useState([]);
  const requestRef = useRef(null);

  const loadData = useCallback(async () => {
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    setLoading(true);
    try {
      const params = new URLSearchParams({
        kind,
        hours: String(hours),
        min_users: String(minUsers),
        p: String(page),
        page_size: '20',
        keyword: filters.keyword,
        sort_by: sort.by,
        sort_order: sort.order === 'ascend' ? 'asc' : 'desc',
      });
      if (excludeWhitelist) params.set('exclude_whitelist', 'true');
      const res = await API.get(
        `/api/risk/shared_sessions?${params.toString()}`,
        { signal: controller.signal, skipErrorHandler: true },
      );
      if (controller.signal.aborted) return;
      const { success, message, data } = res.data;
      if (success) {
        setItems(data?.items || []);
        setTotal(data?.total || 0);
        setUserStates(data?.user_states || {});
        setMeta(data?.meta || {});
      } else {
        showError(message);
      }
    } catch (e) {
      if (!controller.signal.aborted) showError(e.message);
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [
    kind,
    hours,
    minUsers,
    excludeWhitelist,
    page,
    filters.keyword,
    sort.by,
    sort.order,
  ]);

  useEffect(() => setKeyword(filters.keyword), [filters.keyword]);
  useEffect(() => {
    loadData();
    return () => requestRef.current?.abort();
  }, [loadData, refreshKey]);

  const idLabel = kind === 'cache' ? t('缓存关联标识') : t('会话 ID');

  const columns = [
    {
      title: idLabel,
      dataIndex: 'identifier',
      width: 280,
      fixed: 'left',
      render: (v, row) => (
        <Space vertical align='start' spacing={4}>
          <Text code className='break-all'>
            {v}
          </Text>
          <Space wrap spacing={4}>
            {(row.sources || []).map((src) => (
              <Tag key={src} size='small' color='grey'>
                {src}
              </Tag>
            ))}
          </Space>
        </Space>
      ),
    },
    {
      title: t('关联账号数'),
      dataIndex: 'user_count',
      width: 120,
      sorter: true,
      sortOrder: sort.by === 'user_count' ? sort.order : false,
      render: (v) => <Tag color={v >= 3 ? 'red' : 'orange'}>{v}</Tag>,
    },
    {
      title: t('关联账号'),
      dataIndex: 'users',
      width: 260,
      render: (users) => (
        <Space wrap spacing={4}>
          {(users || []).slice(0, 4).map((u) => (
            <Tag key={u.user_id} color='blue'>
              {`${u.username || '-'} (#${u.user_id}) ×${u.request_count}`}
            </Tag>
          ))}
          {(users || []).length > 4 && <Tag>+{users.length - 4}</Tag>}
        </Space>
      ),
    },
    {
      title: t('令牌数'),
      dataIndex: 'token_count',
      width: 100,
      sorter: true,
      sortOrder: sort.by === 'token_count' ? sort.order : false,
    },
    {
      title: t('请求数'),
      dataIndex: 'request_count',
      width: 100,
      sorter: true,
      sortOrder: sort.by === 'request_count' ? sort.order : false,
    },
    {
      title: t('错误数'),
      dataIndex: 'error_count',
      width: 100,
      sorter: true,
      sortOrder: sort.by === 'error_count' ? sort.order : false,
    },
    {
      title: t('来源 IP'),
      dataIndex: 'ips',
      width: 200,
      render: (ips) => (
        <Space wrap spacing={4}>
          {(ips || []).slice(0, 2).map((ip) => (
            <IpTag key={ip} ip={ip} />
          ))}
          {(ips || []).length > 2 && <Tag>+{ips.length - 2}</Tag>}
        </Space>
      ),
    },
    {
      title: t('最近活跃'),
      dataIndex: 'last_seen',
      width: 170,
      sorter: true,
      sortOrder: sort.by === 'last_seen' ? sort.order : false,
      render: (v) => (v ? timestamp2string(v) : '-'),
    },
    {
      title: t('操作'),
      width: 130,
      fixed: 'right',
      render: (_, row) => (
        <Button
          size='small'
          theme='light'
          onClick={() => {
            setActiveItem(row);
            setSelectedUserIds([]);
          }}
        >
          {t('查看账号')}
        </Button>
      ),
    },
  ];

  return (
    <>
      {meta.session_recording_enabled === false && (
        <Banner
          type='warning'
          className='mb-4'
          description={t(
            '会话与缓存标识记录当前未开启，仅显示历史已记录数据。请前往「风控设置」开启记录后生效。',
          )}
        />
      )}
      <Banner
        type='info'
        className='mb-4'
        description={t(
          '统计多个不同账号共用同一客户端会话 ID 或提示缓存键（如 Claude Code、OpenCode、prompt_cache_key 等）的记录，即使切换代理 IP 也能发现多号共用与号池行为。',
        )}
      />
      <Space className='mb-4' wrap>
        <Text>{t('时间范围')}</Text>
        <Select
          value={hours}
          onChange={(value) => setFilters({ hours: value })}
          style={{ width: 140 }}
        >
          <Select.Option value={24}>{t('近 1 天')}</Select.Option>
          <Select.Option value={72}>{t('近 3 天')}</Select.Option>
          <Select.Option value={168}>{t('近 7 天')}</Select.Option>
        </Select>
        <Text>{t('最少关联账号数')}</Text>
        <InputNumber
          value={minUsers}
          min={2}
          max={1000}
          style={{ width: 100 }}
          onChange={(v) => setFilters({ min: v >= 2 ? v : 2 })}
        />
        <Text>{t('过滤全局白名单')}</Text>
        <Switch
          checked={excludeWhitelist}
          onChange={(value) => setFilters({ exclude: value })}
          size='small'
        />
        <Input
          value={keyword}
          onChange={setKeyword}
          style={{ width: 240 }}
          placeholder={t('搜索会话标识、用户名、用户 ID 或 IP')}
          onEnterPress={() => setFilters({ keyword: keyword.trim() })}
        />
        <Button
          icon={<IconSearch />}
          onClick={() => setFilters({ keyword: keyword.trim() })}
        >
          {t('查询')}
        </Button>
        <Button icon={<IconRefresh />} onClick={loadData}>
          {t('刷新')}
        </Button>
      </Space>

      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey='identifier'
        scroll={{ x: 1500 }}
        pagination={{
          pageSize: 20,
          currentPage: page,
          total,
          onPageChange: (value) => setFilters({ p: value }),
        }}
        empty={t('暂无跨账号共享记录')}
        onChange={({ sorter }) => {
          if (!sorter?.dataIndex) return;
          const next = sorter.sortOrder
            ? { by: sorter.dataIndex, order: sorter.sortOrder }
            : { by: 'user_count', order: 'descend' };
          if (sort.by !== next.by || sort.order !== next.order)
            setFilters({ sort: next.by, order: next.order });
        }}
      />

      <Modal
        title={activeItem ? `${idLabel} — ${activeItem.identifier}` : ''}
        visible={!!activeItem}
        onCancel={() => setActiveItem(null)}
        footer={null}
        width={820}
      >
        {activeItem && (
          <Space vertical align='start' className='w-full'>
            <RiskBatchToolbar
              mode='user'
              targets={selectedUserIds.map((id) => ({
                type: 'user',
                value: String(id),
              }))}
              onBatch={onBatch}
            />
            <Table
              className='w-full'
              dataSource={activeItem.users || []}
              rowKey='user_id'
              pagination={false}
              rowSelection={{
                selectedRowKeys: selectedUserIds,
                onChange: setSelectedUserIds,
                getCheckboxProps: (row) => ({
                  disabled: !userStates[row.user_id]?.can_manage,
                }),
              }}
              columns={[
                {
                  title: t('用户'),
                  dataIndex: 'username',
                  render: (v, row) => (
                    <Space>
                      <Text>{`${v || '-'} (#${row.user_id})`}</Text>
                      <RiskUserState state={userStates[row.user_id]} />
                    </Space>
                  ),
                },
                {
                  title: t('请求数'),
                  dataIndex: 'request_count',
                  width: 100,
                },
                {
                  title: t('操作'),
                  width: 180,
                  render: (_, row) => (
                    <Space>
                      <Button
                        size='small'
                        onClick={() => {
                          setActiveItem(null);
                          onInspect({
                            type: 'user',
                            value: String(row.user_id),
                            hours,
                            excludeWhitelist,
                          });
                        }}
                      >
                        {t('查看详情')}
                      </Button>
                      {userStates[row.user_id]?.can_disable && (
                        <Button
                          size='small'
                          type='danger'
                          onClick={() =>
                            onDisposition({
                              type: 'user',
                              value: String(row.user_id),
                              label: row.username,
                              action: 'disable',
                            })
                          }
                        >
                          {t('禁用用户')}
                        </Button>
                      )}
                    </Space>
                  ),
                },
              ]}
            />
          </Space>
        )}
      </Modal>
    </>
  );
}
