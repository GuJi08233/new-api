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
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import IpTag from '../../components/common/ui/IpTag';

const { Text } = Typography;

const PAGE_SIZE = 20;

// 封禁来源 → 展示标签。
const SOURCE_META = {
  manual: { labelKey: '手动添加', color: 'purple' },
  auto_rule: { labelKey: '自动规则', color: 'orange' },
  probe_guard: { labelKey: '测活防护', color: 'red' },
};

const IpBans = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState('');

  // 手动添加弹窗
  const [addVisible, setAddVisible] = useState(false);
  const [addTarget, setAddTarget] = useState('');
  const [addReason, setAddReason] = useState('');
  const [addMinutes, setAddMinutes] = useState(0);
  const [adding, setAdding] = useState(false);

  const loadBans = useCallback(
    async (targetPage) => {
      setLoading(true);
      try {
        const params = new URLSearchParams({
          p: String(targetPage),
          page_size: String(PAGE_SIZE),
        });
        if (keyword.trim()) params.set('keyword', keyword.trim());
        const res = await API.get(`/api/risk/ip_bans?${params.toString()}`);
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
    [keyword],
  );

  useEffect(() => {
    loadBans(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const confirmAdd = async () => {
    if (!addTarget.trim()) {
      showError(t('请输入要封禁的 IP 或 CIDR'));
      return;
    }
    setAdding(true);
    try {
      const res = await API.post('/api/risk/ip_bans', {
        target: addTarget.trim(),
        reason: addReason.trim(),
        expire_minutes: addMinutes > 0 ? addMinutes : 0,
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('IP 已封禁'));
        setAddVisible(false);
        setAddTarget('');
        setAddReason('');
        setAddMinutes(0);
        loadBans(1);
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
    setAdding(false);
  };

  const removeBan = async (id) => {
    try {
      const res = await API.delete(`/api/risk/ip_bans/${id}`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('已解除封禁'));
        loadBans(page);
      } else {
        showError(message);
      }
    } catch (e) {
      showError(e.message);
    }
  };

  const renderExpiry = (expiresAt) => {
    if (!expiresAt) {
      return <Tag color='red'>{t('永久')}</Tag>;
    }
    const expired = expiresAt * 1000 <= Date.now();
    return (
      <Tag color={expired ? 'grey' : 'orange'}>
        {expired ? t('已过期') : timestamp2string(expiresAt)}
      </Tag>
    );
  };

  const columns = [
    {
      title: t('封禁目标'),
      dataIndex: 'target',
      render: (v) =>
        v.includes('/') ? <Text code>{v}</Text> : <IpTag ip={v} />,
    },
    {
      title: t('到期时间'),
      dataIndex: 'expires_at',
      width: 170,
      render: renderExpiry,
    },
    {
      title: t('来源'),
      dataIndex: 'source',
      width: 110,
      render: (v) => {
        const meta = SOURCE_META[v];
        if (!meta) return v || '-';
        return <Tag color={meta.color}>{t(meta.labelKey)}</Tag>;
      },
    },
    {
      title: t('原因'),
      dataIndex: 'reason',
      ellipsis: { showTitle: true },
      render: (v) => v || '-',
    },
    {
      title: t('添加时间'),
      dataIndex: 'created_at',
      width: 160,
      render: (v) => timestamp2string(v),
    },
    {
      title: t('操作人'),
      dataIndex: 'created_by',
      width: 90,
      render: (v) => (v > 0 ? `#${v}` : '-'),
    },
    {
      title: t('操作'),
      width: 110,
      render: (_, record) => (
        <Popconfirm
          title={t('确定解除该封禁？')}
          content={record.target}
          onConfirm={() => removeBan(record.id)}
        >
          <Button theme='light' type='danger' size='small'>
            {t('解除封禁')}
          </Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <>
      <Space className='mb-4' wrap>
        <Input
          value={keyword}
          onChange={setKeyword}
          placeholder={t('搜索目标或原因')}
          style={{ width: 220 }}
          onEnterPress={() => loadBans(1)}
        />
        <Button icon={<IconSearch />} onClick={() => loadBans(1)}>
          {t('查询')}
        </Button>
        <Button icon={<IconRefresh />} onClick={() => loadBans(page)}>
          {t('刷新')}
        </Button>
        <Button
          theme='solid'
          type='primary'
          icon={<IconPlus />}
          onClick={() => setAddVisible(true)}
        >
          {t('封禁 IP')}
        </Button>
      </Space>

      <Text type='tertiary' size='small' className='block mb-2'>
        {t(
          '动态 IP 封禁不受风控总开关控制,添加后立即生效。临时封禁到期自动失效,过期记录保留 3 天后清理;封禁与解封历史可在「风控事件」中追溯。',
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
          onPageChange: (p) => loadBans(p),
        }}
        empty={t('暂无数据')}
      />

      <Modal
        title={t('封禁 IP')}
        visible={addVisible}
        onOk={confirmAdd}
        onCancel={() => setAddVisible(false)}
        okType='danger'
        okText={t('确认封禁')}
        cancelText={t('取消')}
        confirmLoading={adding}
      >
        <Space vertical align='start' style={{ width: '100%' }}>
          <Text>{t('IP 或 CIDR 网段')}</Text>
          <Input
            value={addTarget}
            onChange={setAddTarget}
            placeholder='1.2.3.4 / 10.0.0.0/8 / 2001:db8::/32'
          />
          <Text>{t('封禁时长(分钟,0 或留空为永久)')}</Text>
          <InputNumber
            value={addMinutes}
            min={0}
            max={43200}
            style={{ width: 160 }}
            onChange={(v) => setAddMinutes(v > 0 ? v : 0)}
          />
          <Text>{t('原因')}</Text>
          <TextArea
            value={addReason}
            onChange={setAddReason}
            placeholder={t('封禁原因(可选)')}
            rows={3}
          />
        </Space>
      </Modal>
    </>
  );
};

export default IpBans;
