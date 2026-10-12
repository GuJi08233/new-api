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

import React, { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Input,
  InputNumber,
  Modal,
  Space,
  Table,
  Typography,
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import { formatCoins, signed } from '../Games/common/api';

const { Text } = Typography;

const PAGE_SIZE = 20;

const GameAdminAccounts = () => {
  const { t } = useTranslation();
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [userId, setUserId] = useState();

  const [adjustVisible, setAdjustVisible] = useState(false);
  const [adjustUserId, setAdjustUserId] = useState();
  const [adjustAmount, setAdjustAmount] = useState();
  const [adjustRemark, setAdjustRemark] = useState('');
  const [adjusting, setAdjusting] = useState(false);

  const load = useCallback(
    async (targetPage) => {
      setLoading(true);
      try {
        const params = new URLSearchParams({
          p: String(targetPage),
          page_size: String(PAGE_SIZE),
        });
        if (userId) params.set('user_id', String(userId));
        const res = await API.get(`/api/game/admin/accounts?${params}`);
        if (res.data.success) {
          setItems(res.data.data.items || []);
          setTotal(res.data.data.total || 0);
          setPage(targetPage);
        } else {
          showError(res.data.message);
        }
      } finally {
        setLoading(false);
      }
    },
    [userId],
  );

  useEffect(() => {
    load(1);
  }, [load]);

  const openAdjust = (targetUserId) => {
    setAdjustUserId(targetUserId);
    setAdjustAmount(undefined);
    setAdjustRemark('');
    setAdjustVisible(true);
  };

  const submitAdjust = async () => {
    // 游戏币可以有小数，按额度单位记账，最多 6 位小数。
    if (
      !adjustUserId ||
      typeof adjustAmount !== 'number' ||
      adjustAmount === 0 ||
      Number(adjustAmount.toFixed(6)) !== adjustAmount
    ) {
      showError(t('请填写用户 ID 和非零的数量，最多 6 位小数'));
      return;
    }
    setAdjusting(true);
    try {
      const res = await API.post('/api/game/admin/adjust', {
        user_id: adjustUserId,
        amount: adjustAmount,
        remark: adjustRemark,
      });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess(
        t('调整成功，当前余额 {{balance}}', {
          balance: formatCoins(res.data.data.balance),
        }),
      );
      setAdjustVisible(false);
      load(page);
    } finally {
      setAdjusting(false);
    }
  };

  const columns = [
    {
      title: t('用户'),
      dataIndex: 'user_id',
      render: (value, record) => `${record.username || '-'} (#${value})`,
    },
    {
      title: t('余额'),
      dataIndex: 'balance',
      render: (value) => <Text strong>{formatCoins(value)}</Text>,
    },
    {
      title: t('累计获得'),
      dataIndex: 'total_earned',
      render: (value) => formatCoins(value),
    },
    {
      title: t('已兑换'),
      dataIndex: 'total_converted',
      render: (value) => formatCoins(value),
    },
    {
      title: t('已提现'),
      dataIndex: 'total_withdrawn',
      render: (value) => formatCoins(value),
    },
    {
      title: t('游戏输赢'),
      dataIndex: 'total_wager_net',
      render: (value) => signed(value),
    },
    {
      title: t('更新时间'),
      dataIndex: 'updated_at',
      render: (value) => timestamp2string(value),
    },
    {
      title: t('操作'),
      dataIndex: 'operate',
      render: (_, record) => (
        <Button size='small' onClick={() => openAdjust(record.user_id)}>
          {t('调整')}
        </Button>
      ),
    },
  ];

  return (
    <div className='flex flex-col gap-3'>
      <Space wrap>
        <InputNumber
          value={userId}
          min={1}
          precision={0}
          placeholder={t('用户 ID')}
          onChange={(value) =>
            setUserId(typeof value === 'number' ? value : undefined)
          }
        />
        <Button icon={<IconRefresh />} onClick={() => load(page)}>
          {t('刷新')}
        </Button>
        <Button
          theme='solid'
          icon={<IconPlus />}
          onClick={() => openAdjust(undefined)}
        >
          {t('调整游戏币')}
        </Button>
      </Space>
      <Table
        rowKey='user_id'
        columns={columns}
        dataSource={items}
        loading={loading}
        pagination={{
          currentPage: page,
          pageSize: PAGE_SIZE,
          total,
          onPageChange: load,
        }}
      />

      <Modal
        title={t('调整游戏币')}
        visible={adjustVisible}
        confirmLoading={adjusting}
        onOk={submitAdjust}
        onCancel={() => setAdjustVisible(false)}
      >
        <div className='flex flex-col gap-3'>
          <InputNumber
            value={adjustUserId}
            min={1}
            precision={0}
            placeholder={t('用户 ID')}
            style={{ width: '100%' }}
            onChange={(value) =>
              setAdjustUserId(typeof value === 'number' ? value : undefined)
            }
          />
          <InputNumber
            value={adjustAmount}
            placeholder={t('数量，正数加币、负数扣币，可以有小数')}
            style={{ width: '100%' }}
            onChange={(value) =>
              setAdjustAmount(typeof value === 'number' ? value : undefined)
            }
          />
          <Input
            value={adjustRemark}
            maxLength={255}
            placeholder={t('备注（选填），会显示在用户的游戏币明细里')}
            onChange={setAdjustRemark}
          />
        </div>
      </Modal>
    </div>
  );
};

export default GameAdminAccounts;
