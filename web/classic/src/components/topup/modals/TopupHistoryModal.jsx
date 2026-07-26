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
import React, { useState, useEffect, useMemo } from 'react';
import {
  Modal,
  Table,
  Badge,
  Typography,
  Toast,
  Empty,
  Button,
  Input,
  Tag,
  Tabs,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { Coins } from 'lucide-react';
import { IconSearch } from '@douyinfe/semi-icons';
import { API, openExternal, timestamp2string } from '../../../helpers';
import { isAdmin } from '../../../helpers/utils';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
const { Text } = Typography;
const { TabPane } = Tabs;

// 状态映射配置
const STATUS_CONFIG = {
  success: { type: 'success', key: '成功' },
  pending: { type: 'warning', key: '待支付' },
  failed: { type: 'danger', key: '失败' },
  expired: { type: 'danger', key: '已过期' },
};

// 支付方式映射
const PAYMENT_METHOD_MAP = {
  stripe: 'Stripe',
  creem: 'Creem',
  waffo: 'Waffo',
  ethereum: 'Ethereum',
  alipay: '支付宝',
  wxpay: '微信',
};

function submitEpayForm({ url, params }) {
  const form = document.createElement('form');
  form.action = url;
  form.method = 'POST';
  const isSafari =
    navigator.userAgent.indexOf('Safari') > -1 &&
    navigator.userAgent.indexOf('Chrome') < 1;
  if (!isSafari) form.target = '_blank';
  Object.keys(params || {}).forEach((key) => {
    const input = document.createElement('input');
    input.type = 'hidden';
    input.name = key;
    input.value = params[key];
    form.appendChild(input);
  });
  document.body.appendChild(form);
  form.submit();
  document.body.removeChild(form);
}

function formatCountdown(seconds, t) {
  const value = Math.max(0, Number(seconds || 0));
  const hours = Math.floor(value / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  const secs = value % 60;
  const mm = String(minutes).padStart(2, '0');
  const ss = String(secs).padStart(2, '0');
  if (hours > 0) {
    return `${String(hours).padStart(2, '0')}:${mm}:${ss}`;
  }
  if (value === 0) {
    return t('已过期');
  }
  return `${mm}:${ss}`;
}

const TopupHistoryModal = ({ visible, onCancel, t }) => {
  const userIsAdmin = useMemo(() => isAdmin(), []);
  const [activeTab, setActiveTab] = useState('topups');
  const [loading, setLoading] = useState(false);
  const [topups, setTopups] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [subscriptionOrders, setSubscriptionOrders] = useState([]);
  const [subscriptionTotal, setSubscriptionTotal] = useState(0);
  const [subscriptionPage, setSubscriptionPage] = useState(1);
  const [subscriptionPageSize, setSubscriptionPageSize] = useState(10);
  const [keyword, setKeyword] = useState('');
  const [nowTs, setNowTs] = useState(() => Date.now());
  const isMobile = useIsMobile();

  const loadTopups = async (currentPage, currentPageSize) => {
    setLoading(true);
    try {
      const base = isAdmin() ? '/api/user/topup' : '/api/user/topup/self';
      const qs =
        `p=${currentPage}&page_size=${currentPageSize}` +
        (keyword ? `&keyword=${encodeURIComponent(keyword)}` : '');
      const endpoint = `${base}?${qs}`;
      const res = await API.get(endpoint);
      const { success, message, data } = res.data;
      if (success) {
        setTopups(data.items || []);
        setTotal(data.total || 0);
      } else {
        Toast.error({ content: message || t('加载失败') });
      }
    } catch (error) {
      Toast.error({ content: t('加载账单失败') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!visible) {
      return;
    }
    if (userIsAdmin || activeTab === 'topups') {
      loadTopups(page, pageSize);
      return;
    }
    loadSubscriptionOrders(subscriptionPage, subscriptionPageSize);
  }, [
    visible,
    activeTab,
    page,
    pageSize,
    subscriptionPage,
    subscriptionPageSize,
    keyword,
    userIsAdmin,
  ]);

  useEffect(() => {
    if (!visible) {
      return undefined;
    }
    setNowTs(Date.now());
    const timer = window.setInterval(() => {
      setNowTs(Date.now());
    }, 1000);
    return () => window.clearInterval(timer);
  }, [visible]);

  const handlePageChange = (currentPage) => {
    if (activeTab === 'subscriptions' && !userIsAdmin) {
      setSubscriptionPage(currentPage);
      return;
    }
    setPage(currentPage);
  };

  const handlePageSizeChange = (currentPageSize) => {
    if (activeTab === 'subscriptions' && !userIsAdmin) {
      setSubscriptionPageSize(currentPageSize);
      setSubscriptionPage(1);
      return;
    }
    setPageSize(currentPageSize);
    setPage(1);
  };

  const handleKeywordChange = (value) => {
    setKeyword(value);
    setPage(1);
    setSubscriptionPage(1);
  };

  // 管理员补单
  const handleAdminComplete = async (tradeNo) => {
    try {
      const res = await API.post('/api/user/topup/complete', {
        trade_no: tradeNo,
      });
      const { success, message } = res.data;
      if (success) {
        Toast.success({ content: t('补单成功') });
        await loadTopups(page, pageSize);
      } else {
        Toast.error({ content: message || t('补单失败') });
      }
    } catch (e) {
      Toast.error({ content: t('补单失败') });
    }
  };

  const confirmAdminComplete = (tradeNo) => {
    Modal.confirm({
      title: t('确认补单'),
      content: t('是否将该订单标记为成功并为用户入账？'),
      onOk: () => handleAdminComplete(tradeNo),
    });
  };

  // 渲染状态徽章
  const renderStatusBadge = (status) => {
    const config = STATUS_CONFIG[status] || { type: 'primary', key: status };
    return (
      <span className='flex items-center gap-2'>
        <Badge dot type={config.type} />
        <span>{t(config.key)}</span>
      </span>
    );
  };

  // 渲染支付方式
  const renderPaymentMethod = (pm) => {
    const displayName = PAYMENT_METHOD_MAP[pm];
    return <Text>{displayName ? t(displayName) : pm || '-'}</Text>;
  };

  const isSubscriptionTopup = (record) => {
    const tradeNo = (record?.trade_no || '').toLowerCase();
    return Number(record?.amount || 0) === 0 && tradeNo.startsWith('sub');
  };

  const loadSubscriptionOrders = async (
    currentPage,
    currentPageSize,
  ) => {
    setLoading(true);
    try {
      const qs =
        `p=${currentPage}&page_size=${currentPageSize}` +
        (keyword ? `&keyword=${encodeURIComponent(keyword)}` : '');
      const res = await API.get(`/api/subscription/orders/self?${qs}`);
      const { success, message, data } = res.data;
      if (success) {
        setSubscriptionOrders(data.items || []);
        setSubscriptionTotal(data.total || 0);
      } else {
        Toast.error({ content: message || t('加载失败') });
      }
    } catch (error) {
      Toast.error({ content: t('加载账单失败') });
    } finally {
      setLoading(false);
    }
  };

  const handleContinueSubscriptionOrder = (record) => {
    if (!record) return;
    if (record.resume_type === 'url' && record.resume_url) {
      openExternal(record.resume_url);
      return;
    }
    if (record.resume_type === 'form' && record.resume_url) {
      submitEpayForm({
        url: record.resume_url,
        params: record.resume_params || {},
      });
      return;
    }
    Toast.info({
      content:
        record.resume_message || t('当前支付方式需要重新发起支付'),
    });
  };

  const handleCancelSubscriptionOrder = async (tradeNo) => {
    try {
      const res = await API.post(`/api/subscription/orders/${tradeNo}/cancel`);
      const { success, message } = res.data;
      if (success) {
        Toast.success({ content: t('订单已取消') });
        await loadSubscriptionOrders(subscriptionPage, subscriptionPageSize);
      } else {
        Toast.error({ content: message || t('取消失败') });
      }
    } catch (error) {
      Toast.error({ content: t('取消失败') });
    }
  };

  const confirmCancelSubscriptionOrder = (tradeNo) => {
    Modal.confirm({
      title: t('确认取消订单'),
      content: t('取消后该待支付订单将失效，如需继续请重新创建订单。'),
      onOk: () => handleCancelSubscriptionOrder(tradeNo),
    });
  };

  const columns = useMemo(() => {
    const baseColumns = [
      ...(userIsAdmin
        ? [
            {
              title: t('用户ID'),
              dataIndex: 'user_id',
              key: 'user_id',
              render: (userId) => <Text>{userId ?? '-'}</Text>,
            },
          ]
        : []),
      {
        title: t('订单号'),
        dataIndex: 'trade_no',
        key: 'trade_no',
        render: (text) => <Text copyable>{text}</Text>,
      },
      {
        title: t('支付方式'),
        dataIndex: 'payment_method',
        key: 'payment_method',
        render: renderPaymentMethod,
      },
      {
        title: t('充值额度'),
        dataIndex: 'amount',
        key: 'amount',
        render: (amount, record) => {
          if (isSubscriptionTopup(record)) {
            return (
              <Tag color='purple' shape='circle' size='small'>
                {t('订阅套餐')}
              </Tag>
            );
          }
          return (
            <span className='flex items-center gap-1'>
              <Coins size={16} />
              <Text>{amount}</Text>
            </span>
          );
        },
      },
      {
        title: t('支付金额'),
        dataIndex: 'money',
        key: 'money',
        render: (money) => <Text type='danger'>¥{money.toFixed(2)}</Text>,
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        key: 'status',
        render: renderStatusBadge,
      },
    ];

    // 管理员才显示操作列
    if (userIsAdmin) {
      baseColumns.push({
        title: t('操作'),
        key: 'action',
        render: (_, record) => {
          const actions = [];
          if (record.status === 'pending') {
            actions.push(
              <Button
                key="complete"
                size='small'
                type='primary'
                theme='outline'
                onClick={() => confirmAdminComplete(record.trade_no)}
              >
                {t('补单')}
              </Button>
            );
          }
          return actions.length > 0 ? <>{actions}</> : null;
        },
      });
    }

    baseColumns.push({
      title: t('创建时间'),
      dataIndex: 'create_time',
      key: 'create_time',
      render: (time) => timestamp2string(time),
    });

    return baseColumns;
  }, [t, userIsAdmin]);

  const subscriptionColumns = useMemo(
    () => [
      {
        title: t('订单号'),
        dataIndex: 'trade_no',
        key: 'trade_no',
        render: (text) => <Text copyable>{text}</Text>,
      },
      {
        title: t('套餐'),
        dataIndex: 'plan_title',
        key: 'plan_title',
        render: (text) => <Text>{text || '-'}</Text>,
      },
      {
        title: t('支付方式'),
        dataIndex: 'payment_method',
        key: 'payment_method',
        render: renderPaymentMethod,
      },
      {
        title: t('支付金额'),
        dataIndex: 'money',
        key: 'money',
        render: (money) => <Text type='danger'>¥{Number(money || 0).toFixed(2)}</Text>,
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        key: 'status',
        render: renderStatusBadge,
      },
      {
        title: t('剩余支付时间'),
        key: 'remaining_seconds',
        render: (_, record) => {
          if (record.status !== 'pending') {
            return <Text type='tertiary'>-</Text>;
          }
          const remain = Math.max(
            0,
            Number(record.expires_at || 0) - Math.floor(nowTs / 1000),
          );
          return (
            <Text type={remain > 0 ? 'warning' : 'danger'}>
              {formatCountdown(remain, t)}
            </Text>
          );
        },
      },
      {
        title: t('操作'),
        key: 'action',
        render: (_, record) => {
          if (record.status !== 'pending') {
            return record.resume_message ? (
              <Text type='tertiary'>{record.resume_message}</Text>
            ) : null;
          }
          const remain = Math.max(
            0,
            Number(record.expires_at || 0) - Math.floor(nowTs / 1000),
          );
          if (remain <= 0) {
            return <Text type='danger'>{t('已过期')}</Text>;
          }
          return (
            <div className='flex items-center gap-2 flex-wrap'>
              {record.resume_type && record.resume_type !== 'recreate' ? (
                <Button
                  size='small'
                  theme='outline'
                  type='primary'
                  onClick={() => handleContinueSubscriptionOrder(record)}
                >
                  {t('继续支付')}
                </Button>
              ) : null}
              <Button
                size='small'
                type='danger'
                theme='borderless'
                onClick={() =>
                  confirmCancelSubscriptionOrder(record.trade_no)
                }
              >
                {t('取消订单')}
              </Button>
              {record.resume_type === 'recreate' && record.resume_message ? (
                <Text type='tertiary'>{record.resume_message}</Text>
              ) : null}
            </div>
          );
        },
      },
      {
        title: t('创建时间'),
        dataIndex: 'create_time',
        key: 'create_time',
        render: (time) => timestamp2string(time),
      },
    ],
    [nowTs, t],
  );

  const topupEmpty = (
    <Empty
      image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
      darkModeImage={
        <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
      }
      description={t('暂无充值记录')}
      style={{ padding: 30 }}
    />
  );

  const subscriptionEmpty = (
    <Empty
      image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
      darkModeImage={
        <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
      }
      description={t('暂无订阅订单')}
      style={{ padding: 30 }}
    />
  );

  return (
    <Modal
      title={t('充值账单')}
      visible={visible}
      onCancel={onCancel}
      footer={null}
      size={isMobile ? 'full-width' : 'large'}
    >
      <div className='mb-3'>
        <Input
          prefix={<IconSearch />}
          placeholder={t('订单号')}
          value={keyword}
          onChange={handleKeywordChange}
          showClear
        />
      </div>
      {userIsAdmin ? (
        <Table
          columns={columns}
          dataSource={topups}
          loading={loading}
          rowKey='id'
          pagination={{
            currentPage: page,
            pageSize: pageSize,
            total: total,
            showSizeChanger: true,
            pageSizeOpts: [10, 20, 50, 100],
            onPageChange: handlePageChange,
            onPageSizeChange: handlePageSizeChange,
          }}
          size='small'
          empty={topupEmpty}
        />
      ) : (
        <Tabs activeKey={activeTab} onChange={setActiveTab} type='line'>
          <TabPane tab={t('充值账单')} itemKey='topups'>
            <Table
              columns={columns}
              dataSource={topups}
              loading={loading && activeTab === 'topups'}
              rowKey='id'
              pagination={{
                currentPage: page,
                pageSize: pageSize,
                total: total,
                showSizeChanger: true,
                pageSizeOpts: [10, 20, 50, 100],
                onPageChange: handlePageChange,
                onPageSizeChange: handlePageSizeChange,
              }}
              size='small'
              empty={topupEmpty}
            />
          </TabPane>
          <TabPane tab={t('订阅订单')} itemKey='subscriptions'>
            <Table
              columns={subscriptionColumns}
              dataSource={subscriptionOrders}
              loading={loading && activeTab === 'subscriptions'}
              rowKey='id'
              pagination={{
                currentPage: subscriptionPage,
                pageSize: subscriptionPageSize,
                total: subscriptionTotal,
                showSizeChanger: true,
                pageSizeOpts: [10, 20, 50, 100],
                onPageChange: handlePageChange,
                onPageSizeChange: handlePageSizeChange,
              }}
              size='small'
              empty={subscriptionEmpty}
            />
          </TabPane>
        </Tabs>
      )}
    </Modal>
  );
};

export default TopupHistoryModal;
