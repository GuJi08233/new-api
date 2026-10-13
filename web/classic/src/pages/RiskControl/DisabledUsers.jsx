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
  Input,
  Radio,
  RadioGroup,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';
import {
  riskEventTypeMeta,
  riskRelativeTime,
  riskRemainingTime,
  riskRuleLabel,
} from './riskLabels';
import useRiskFilters, { riskInteger } from './useRiskFilters';
import useRiskAssignees from './useRiskAssignees';
import { RiskBatchToolbar } from './RiskBatchModal';

const { Text } = Typography;

// 当前被禁用的账号。临时禁用按到期先后排列,方便看哪些账号即将自动恢复;
// 每行带最近一次禁用的来源,可直接启用、转为永久或勾选后批量处理。
const DisabledUsers = ({
  onInspect,
  onDisposition,
  onBatch,
  onViewEvents,
  refreshKey,
}) => {
  const { t } = useTranslation();
  const { nameOf } = useRiskAssignees();
  const [filters, setFilters] = useRiskFilters('u', {
    keyword: '',
    kind: '',
    p: 1,
  });
  const kind = ['temporary', 'permanent'].includes(filters.kind)
    ? filters.kind
    : '';
  const page = riskInteger(filters.p, 1, 1, 1000000);
  const [keyword, setKeyword] = useState(filters.keyword);
  const [items, setItems] = useState([]);
  const [states, setStates] = useState({});
  const [total, setTotal] = useState(0);
  const [selected, setSelected] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [reload, setReload] = useState(0);

  useEffect(() => setKeyword(filters.keyword), [filters.keyword]);
  useEffect(() => {
    const controller = new AbortController();
    const params = new URLSearchParams({
      p: String(page),
      page_size: '20',
      keyword: filters.keyword,
    });
    if (kind) params.set('kind', kind);
    setLoading(true);
    setSelected([]);
    setError('');
    API.get(`/api/risk/disabled_users?${params}`, {
      signal: controller.signal,
      skipErrorHandler: true,
    })
      .then((res) => {
        if (controller.signal.aborted) return;
        if (!res.data.success) throw new Error(res.data.message);
        setItems(res.data.data?.page?.items || []);
        setTotal(res.data.data?.page?.total || 0);
        setStates(res.data.data?.user_states || {});
      })
      .catch((e) => {
        if (!controller.signal.aborted) {
          setItems([]);
          setTotal(0);
          setError(e.response?.data?.message || e.message);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [page, filters.keyword, kind, reload, refreshKey]);

  const search = () => setFilters({ keyword: keyword.trim(), p: 1 });
  const columns = [
    {
      title: t('账号'),
      width: 220,
      render: (_, user) => (
        <div className='flex flex-col items-start'>
          <Button
            theme='borderless'
            size='small'
            style={{ padding: 0, height: 'auto' }}
            onClick={() => onInspect({ type: 'user', value: String(user.id) })}
          >
            {user.username} #{user.id}
          </Button>
          {user.display_name && user.display_name !== user.username && (
            <Text type='tertiary' size='small'>
              {user.display_name}
            </Text>
          )}
        </div>
      ),
    },
    {
      title: t('期限'),
      width: 230,
      render: (_, user) => {
        if (!user.disable_expires_at)
          return <Tag color='red'>{t('永久禁用')}</Tag>;
        const remaining = riskRemainingTime(user.disable_expires_at);
        return (
          <div className='flex flex-col items-start gap-1'>
            <Tag color='orange'>
              {remaining
                ? t('临时禁用，剩余 {{time}}', { time: remaining })
                : t('临时禁用，即将恢复')}
            </Tag>
            <Text type='tertiary' size='small'>
              {timestamp2string(user.disable_expires_at)}
            </Text>
          </div>
        );
      },
    },
    {
      title: t('禁用原因'),
      dataIndex: 'disable_reason',
      ellipsis: { showTitle: true },
      render: (value) => value || '-',
    },
    {
      title: t('最近一次禁用'),
      width: 230,
      render: (_, user) => {
        if (!user.last_ban_at)
          return <Text type='tertiary'>{t('无处置记录')}</Text>;
        const meta = riskEventTypeMeta(user.last_ban_type, t);
        return (
          <div className='flex flex-col items-start gap-1'>
            <Space spacing={4}>
              <Tag color={meta.color}>{meta.label}</Tag>
              <Text type='tertiary' size='small'>
                {t('{{time}} 前', { time: riskRelativeTime(user.last_ban_at) })}
              </Text>
            </Space>
            <Text type='tertiary' size='small'>
              {user.last_ban_type === 'ban_auto'
                ? riskRuleLabel(
                    {
                      event_type: user.last_ban_type,
                      rule: user.last_ban_rule,
                    },
                    [],
                    t,
                  )
                : user.last_ban_operator_id > 0
                  ? `${t('操作人')}: ${nameOf(user.last_ban_operator_id)}`
                  : ''}
            </Text>
          </div>
        );
      },
    },
    {
      title: t('操作'),
      width: 230,
      fixed: 'right',
      render: (_, user) => {
        const state = states[user.id];
        return (
          <Space spacing={4} wrap>
            {state?.can_enable && (
              <Button
                size='small'
                theme='solid'
                onClick={() =>
                  onDisposition({
                    type: 'user',
                    value: String(user.id),
                    label: user.username,
                    action: 'enable',
                  })
                }
              >
                {t('启用')}
              </Button>
            )}
            {user.disable_expires_at > 0 && state?.can_disable && (
              <Button
                size='small'
                type='danger'
                onClick={() =>
                  onDisposition({
                    type: 'user',
                    value: String(user.id),
                    label: user.username,
                    action: 'disable',
                  })
                }
              >
                {t('转为永久')}
              </Button>
            )}
            <Button
              size='small'
              theme='borderless'
              onClick={() => onViewEvents({ user_id: user.id })}
            >
              {t('查看事件')}
            </Button>
          </Space>
        );
      },
    },
  ];

  return (
    <>
      <Space wrap className='mb-3'>
        <RadioGroup
          type='button'
          value={kind}
          onChange={(event) => setFilters({ kind: event.target.value, p: 1 })}
          aria-label={t('禁用期限')}
        >
          <Radio value=''>{t('全部')}</Radio>
          <Radio value='temporary'>{t('临时禁用')}</Radio>
          <Radio value='permanent'>{t('永久禁用')}</Radio>
        </RadioGroup>
        <Input
          value={keyword}
          onChange={setKeyword}
          onEnterPress={search}
          placeholder={t('用户 ID、用户名或显示名')}
          style={{ width: 260 }}
          showClear
          onClear={() => setFilters({ keyword: '', p: 1 })}
          aria-label={t('搜索账号')}
        />
        <Button icon={<IconSearch />} onClick={search} aria-label={t('查询')}>
          {t('查询')}
        </Button>
        <Button
          icon={<IconRefresh />}
          onClick={() => setReload((n) => n + 1)}
          aria-label={t('刷新')}
        >
          {t('刷新')}
        </Button>
      </Space>
      {error && <Banner type='danger' className='mb-3' description={error} />}
      <Text type='tertiary' size='small' className='block mb-2'>
        {t(
          '临时禁用来自自动规则，到期后系统自动恢复；永久禁用（包括管理员手动禁用）需要管理员启用。',
        )}
      </Text>
      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey='id'
        scroll={{ x: 1100 }}
        pagination={{
          currentPage: page,
          pageSize: 20,
          total,
          onPageChange: (value) => setFilters({ p: value }),
        }}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: setSelected,
          getCheckboxProps: (user) => ({
            disabled: !states[user.id]?.can_manage,
          }),
        }}
        empty={t('没有被禁用的账号')}
      />
      <RiskBatchToolbar
        mode='user'
        targets={selected.map((id) => ({ type: 'user', value: String(id) }))}
        onBatch={onBatch}
      />
    </>
  );
};

export default DisabledUsers;
