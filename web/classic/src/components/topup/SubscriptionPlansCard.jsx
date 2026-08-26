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

import React, { useMemo, useState } from 'react';
import {
  Badge,
  Button,
  Card,
  Divider,
  Modal,
  Select,
  Skeleton,
  Space,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import {
  API,
  showError,
  showSuccess,
  showInfo,
  renderQuota,
  openExternal,
} from '../../helpers';
import { getCurrencyConfig } from '../../helpers/render';
import { RefreshCw, Sparkles } from 'lucide-react';
import SubscriptionPurchaseModal from './modals/SubscriptionPurchaseModal';
import WalletConnectQrModal from './modals/WalletConnectQrModal';
import {
  formatSubscriptionDuration,
  formatSubscriptionResetPeriod,
  formatTiersSummary,
} from '../../helpers/subscriptionFormat';
import {
  executeEthereumOrderWithAutoWallet,
  isEthereumUserRejected,
} from '../../helpers/ethereumWallet';

const { Text } = Typography;

// 过滤易支付方式
function getEpayMethods(payMethods = []) {
  return (payMethods || []).filter(
    (m) =>
      m?.type &&
      m.type !== 'stripe' &&
      m.type !== 'creem' &&
      m.type !== 'ethereum',
  );
}

// 提交易支付表单
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

function formatGlobalPurchaseResetPeriod(plan, t) {
  const period = plan?.max_purchase_reset_period || 'never';
  if (period === 'active') return t('到期释放名额');
  if (period === 'daily') return t('每天');
  if (period === 'weekly') return t('每周');
  if (period === 'monthly') return t('每月');
  if (period === 'custom') {
    const seconds = Number(plan?.max_purchase_reset_custom_seconds || 0);
    if (seconds >= 86400) return `${Math.floor(seconds / 86400)} ${t('天')}`;
    if (seconds >= 3600) return `${Math.floor(seconds / 3600)} ${t('小时')}`;
    if (seconds >= 60) return `${Math.floor(seconds / 60)} ${t('分钟')}`;
    return `${seconds} ${t('秒')}`;
  }
  return t('不刷新');
}

function getWalletConnectConfig(ethereumInfo) {
  const walletConnect = ethereumInfo?.wallet_connect || {};
  return {
    projectId: walletConnect.project_id || '',
    appName: walletConnect.app_name || '',
    description: walletConnect.description || '',
    url: walletConnect.url || '',
    icon: walletConnect.icon || '',
    relayProxyEnabled: Boolean(walletConnect.relay_proxy_enabled),
    relayProxyUrl: walletConnect.relay_proxy_url || '',
    primaryRelayUrl: walletConnect.primary_relay_url || '',
    backupRelayUrl: walletConnect.backup_relay_url || '',
  };
}

const SubscriptionPlansCard = ({
  t,
  loading = false,
  plans = [],
  payMethods = [],
  enableOnlineTopUp = false,
  enableStripeTopUp = false,
  enableCreemTopUp = false,
  enableEthereumTopUp = false,
  ethereumInfo = null,
  billingPreference,
  onChangeBillingPreference,
  activeSubscriptions = [],
  allSubscriptions = [],
  reloadSubscriptionSelf,
  reloadUserQuota,
  withCard = true,
}) => {
  const [open, setOpen] = useState(false);
  const [selectedPlan, setSelectedPlan] = useState(null);
  const [paying, setPaying] = useState(false);
  const [selectedPayMethod, setSelectedPayMethod] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [switchingSubscriptionId, setSwitchingSubscriptionId] = useState(null);
  const [walletConnectUri, setWalletConnectUri] = useState('');
  const [walletConnectModalVisible, setWalletConnectModalVisible] =
    useState(false);

  const epayMethods = useMemo(() => getEpayMethods(payMethods), [payMethods]);

  const openBuy = (p) => {
    setSelectedPlan(p);
    setSelectedPayMethod(epayMethods?.[0]?.type || '');
    setOpen(true);
  };

  const closeBuy = () => {
    setOpen(false);
    setSelectedPlan(null);
    setPaying(false);
  };

  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      await reloadSubscriptionSelf?.();
    } finally {
      setRefreshing(false);
    }
  };

  const handleSwitchSubscription = async (subscriptionId) => {
    if (!subscriptionId) return;
    setSwitchingSubscriptionId(subscriptionId);
    try {
      const res = await API.post('/api/subscription/self/switch', {
        subscription_id: subscriptionId,
      });
      if (res.data?.success) {
        showSuccess(res.data?.data?.message || t('切换成功'));
        await reloadSubscriptionSelf?.();
      } else {
        showError(res.data?.message || t('切换失败'));
      }
    } catch (e) {
      showError(t('请求失败'));
    } finally {
      setSwitchingSubscriptionId(null);
    }
  };

  const payStripe = async () => {
    if (!selectedPlan?.plan?.stripe_price_id) {
      showError(t('该套餐未配置 Stripe'));
      return;
    }
    setPaying(true);
    try {
      const res = await API.post('/api/subscription/stripe/pay', {
        plan_id: selectedPlan.plan.id,
      });
      if (res.data?.message === 'success') {
        openExternal(res.data.data?.pay_link);
        showSuccess(t('已创建订单并打开支付页面，可在账单继续支付'));
        closeBuy();
      } else {
        const errorMsg =
          typeof res.data?.data === 'string'
            ? res.data.data
            : res.data?.message || t('支付失败');
        showError(errorMsg);
      }
    } catch (e) {
      showError(t('支付请求失败'));
    } finally {
      setPaying(false);
    }
  };

  const payCreem = async () => {
    if (!selectedPlan?.plan?.creem_product_id) {
      showError(t('该套餐未配置 Creem'));
      return;
    }
    setPaying(true);
    try {
      const res = await API.post('/api/subscription/creem/pay', {
        plan_id: selectedPlan.plan.id,
      });
      if (res.data?.message === 'success') {
        openExternal(res.data.data?.checkout_url);
        showSuccess(t('已创建订单并打开支付页面，可在账单继续支付'));
        closeBuy();
      } else {
        const errorMsg =
          typeof res.data?.data === 'string'
            ? res.data.data
            : res.data?.message || t('支付失败');
        showError(errorMsg);
      }
    } catch (e) {
      showError(t('支付请求失败'));
    } finally {
      setPaying(false);
    }
  };

  const payEpay = async (method) => {
    if (!method) {
      showError(t('请选择支付方式'));
      return;
    }
    setPaying(true);
    try {
      const res = await API.post('/api/subscription/epay/pay', {
        plan_id: selectedPlan.plan.id,
        payment_method: method,
      });
      if (res.data?.message === 'success') {
        submitEpayForm({ url: res.data.url, params: res.data.data });
        showSuccess(t('已创建订单并发起支付，可在账单继续支付'));
        closeBuy();
      } else {
        const errorMsg =
          typeof res.data?.data === 'string'
            ? res.data.data
            : res.data?.message || t('支付失败');
        showError(errorMsg);
      }
    } catch (e) {
      showError(t('支付请求失败'));
    } finally {
      setPaying(false);
    }
  };

  const payBalance = () => {
    const plan = selectedPlan?.plan;
    if (!plan?.id) return;
    const { symbol, rate } = getCurrencyConfig();
    const convertedPrice = Number(plan.price_amount || 0) * rate;
    const displayPrice = convertedPrice.toFixed(
      Number.isInteger(convertedPrice) ? 0 : 2,
    );
    Modal.confirm({
      title: t('确认使用余额购买'),
      content: `${t('将从钱包余额扣除')} ${symbol}${displayPrice}`,
      centered: true,
      onOk: async () => {
        setPaying(true);
        try {
          const res = await API.post('/api/subscription/balance/pay', {
            plan_id: plan.id,
          });
          if (res.data?.success) {
            showSuccess(t('购买成功'));
            closeBuy();
            reloadSubscriptionSelf?.();
            reloadUserQuota?.();
          } else {
            showError(res.data?.message || t('支付失败'));
          }
        } catch (e) {
          showError(t('支付请求失败'));
        } finally {
          setPaying(false);
        }
      },
    });
  };

  const payEthereum = async (tokenAddress) => {
    if (!selectedPlan?.plan?.id) return;
    setPaying(true);
    try {
      // 1. Create pending subscription order on backend
      const res = await API.post('/api/subscription/ethereum/pay', {
        plan_id: selectedPlan.plan.id,
        token_address: tokenAddress,
      });
      if (!res?.data || res.data.message !== 'success') {
        showError(res?.data?.data || t('创建订单失败'));
        return;
      }
      const {
        order_id,
        contract_address,
        chain_id,
        token_address: respTokenAddr,
        pay_amount,
      } = res.data.data;
      showInfo(
        t('正在连接钱包，如未检测到浏览器钱包将显示 WalletConnect 连接信息...'),
      );
      const receipt = await executeEthereumOrderWithAutoWallet(
        {
          order_id,
          contract_address,
          chain_id,
          token_address: respTokenAddr,
          pay_amount,
        },
        getWalletConnectConfig(ethereumInfo),
        {
          onWalletConnectPending: () => {
            setWalletConnectUri('');
            setWalletConnectModalVisible(true);
          },
          onWalletConnectUri: (uri) => {
            setWalletConnectUri(uri || '');
            setWalletConnectModalVisible(Boolean(uri));
          },
          onWalletConnectConnected: () => {
            showInfo(t('钱包已连接，正在准备交易请求...'));
          },
          onWalletConnectSessionEstablished: () => {
            setWalletConnectModalVisible(false);
            showInfo(t('连接已建立，正在同步钱包会话...'));
          },
          onWalletConnectSwitchNetworkPending: () => {
            showInfo(t('请在钱包中确认切换网络'));
          },
          onWalletConnectReadyToSign: () => {
            showInfo(t('钱包已就绪，正在发起交易请求...'));
          },
          onWalletConnectApprovePending: () => {
            showInfo(t('请在钱包中确认代币授权'));
          },
          onWalletConnectTransactionPending: () => {
            setWalletConnectModalVisible(false);
            showInfo(t('请在钱包中确认支付交易'));
          },
          onWalletConnectError: () => {
            setWalletConnectModalVisible(false);
          },
        },
      );
      showSuccess(
        receipt?.walletName
          ? t('交易确认！额度将在几秒内到账，钱包：') + receipt.walletName
          : t('交易确认！额度将在几秒内到账'),
      );
      closeBuy();
    } catch (e) {
      if (isEthereumUserRejected(e)) {
        showError(t('用户取消了交易'));
      } else {
        showError(e?.reason || e?.message || t('交易失败'));
      }
    } finally {
      setPaying(false);
    }
  };

  // 当前订阅信息 - 支持多个订阅
  const hasActiveSubscription = activeSubscriptions.length > 0;
  const hasAnySubscription = allSubscriptions.length > 0;
  const disableSubscriptionPreference = !hasActiveSubscription;
  const isSubscriptionPreference =
    billingPreference === 'subscription_first' ||
    billingPreference === 'subscription_only';
  const displayBillingPreference =
    disableSubscriptionPreference && isSubscriptionPreference
      ? 'wallet_first'
      : billingPreference;
  const subscriptionPreferenceLabel =
    billingPreference === 'subscription_only' ? t('仅用订阅') : t('优先订阅');

  const planPurchaseCountMap = useMemo(() => {
    const map = new Map();
    (allSubscriptions || []).forEach((sub) => {
      const planId = sub?.subscription?.plan_id;
      if (!planId) return;
      map.set(planId, (map.get(planId) || 0) + 1);
    });
    return map;
  }, [allSubscriptions]);

  const planTitleMap = useMemo(() => {
    const map = new Map();
    (plans || []).forEach((p) => {
      const plan = p?.plan;
      if (!plan?.id) return;
      map.set(plan.id, plan.title || '');
    });
    return map;
  }, [plans]);

  // 持有未过期订阅的套餐：无全局限购时再次购买为续期（顺延到期时间、不占名额），
  // 有全局限购时不提供续期也不允许重复购买
  const livePlanIdSet = useMemo(() => {
    const now = Date.now() / 1000;
    const set = new Set();
    (allSubscriptions || []).forEach((sub) => {
      const subscription = sub?.subscription;
      if (!subscription?.plan_id) return;
      const isLive =
        (subscription.status === 'active' ||
          subscription.status === 'inactive') &&
        (subscription.end_time || 0) > now;
      if (isLive) {
        set.add(subscription.plan_id);
      }
    });
    return set;
  }, [allSubscriptions]);

  const subscriptionStatusCounts = useMemo(() => {
    const now = Date.now() / 1000;
    let inactive = 0;
    let expired = 0;
    let cancelled = 0;
    (allSubscriptions || []).forEach((sub) => {
      const subscription = sub?.subscription;
      const isExpired =
        (subscription?.end_time || 0) > 0 &&
        (subscription?.end_time || 0) < now;
      if (subscription?.status === 'cancelled') {
        cancelled++;
        return;
      }
      if (subscription?.status === 'inactive' && !isExpired) {
        inactive++;
        return;
      }
      if (subscription?.status === 'expired' || isExpired) {
        expired++;
      }
    });
    return { inactive, expired, cancelled };
  }, [allSubscriptions]);

  const getPlanPurchaseCount = (planId) =>
    planPurchaseCountMap.get(planId) || 0;

  // 计算单个订阅的剩余天数
  const getRemainingDays = (sub) => {
    if (!sub?.subscription?.end_time) return 0;
    const now = Date.now() / 1000;
    const remaining = sub.subscription.end_time - now;
    return Math.max(0, Math.ceil(remaining / 86400));
  };

  // 计算单个订阅的使用进度
  const getUsagePercent = (sub) => {
    const total = Number(sub?.subscription?.amount_total || 0);
    const used = Number(sub?.subscription?.amount_used || 0);
    if (total <= 0) return 0;
    return Math.round((used / total) * 100);
  };

  const cardContent = (
    <>
      {/* 卡片头部 */}
      {loading ? (
        <div className='space-y-4'>
          {/* 我的订阅骨架屏 */}
          <Card className='!rounded-xl w-full' bodyStyle={{ padding: '12px' }}>
            <div className='flex items-center justify-between mb-3'>
              <Skeleton.Title active style={{ width: 100, height: 20 }} />
              <Skeleton.Button active style={{ width: 24, height: 24 }} />
            </div>
            <div className='space-y-2'>
              <Skeleton.Paragraph active rows={2} />
            </div>
          </Card>
          {/* 套餐列表骨架屏 */}
          <div className='grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-2 xl:grid-cols-3 gap-5 w-full px-1'>
            {[1, 2, 3].map((i) => (
              <Card
                key={i}
                className='!rounded-xl w-full h-full'
                bodyStyle={{ padding: 16 }}
              >
                <Skeleton.Title
                  active
                  style={{ width: '60%', height: 24, marginBottom: 8 }}
                />
                <Skeleton.Paragraph
                  active
                  rows={1}
                  style={{ marginBottom: 12 }}
                />
                <div className='text-center py-4'>
                  <Skeleton.Title
                    active
                    style={{ width: '40%', height: 32, margin: '0 auto' }}
                  />
                </div>
                <Skeleton.Paragraph active rows={3} style={{ marginTop: 12 }} />
                <Skeleton.Button
                  active
                  block
                  style={{ marginTop: 16, height: 32 }}
                />
              </Card>
            ))}
          </div>
        </div>
      ) : (
        <Space vertical style={{ width: '100%' }} spacing={8}>
          {/* 当前订阅状态 */}
          <Card className='!rounded-xl w-full' bodyStyle={{ padding: '12px' }}>
            <div className='flex items-center justify-between mb-2 gap-3'>
              <div className='flex items-center gap-2 flex-1 min-w-0'>
                <Text strong>{t('我的订阅')}</Text>
                {hasActiveSubscription ? (
                  <Tag
                    color='white'
                    size='small'
                    shape='circle'
                    prefixIcon={<Badge dot type='success' />}
                  >
                    {activeSubscriptions.length} {t('个生效中')}
                  </Tag>
                ) : (
                  <Tag color='white' size='small' shape='circle'>
                    {t('无生效')}
                  </Tag>
                )}
                {subscriptionStatusCounts.inactive > 0 && (
                  <Tag color='white' size='small' shape='circle'>
                    {subscriptionStatusCounts.inactive} {t('个未激活')}
                  </Tag>
                )}
                {subscriptionStatusCounts.expired > 0 && (
                  <Tag color='white' size='small' shape='circle'>
                    {subscriptionStatusCounts.expired} {t('个已过期')}
                  </Tag>
                )}
                {subscriptionStatusCounts.cancelled > 0 && (
                  <Tag color='white' size='small' shape='circle'>
                    {subscriptionStatusCounts.cancelled} {t('个已作废')}
                  </Tag>
                )}
              </div>
              <div className='flex items-center gap-2'>
                <Select
                  value={displayBillingPreference}
                  onChange={onChangeBillingPreference}
                  size='small'
                  optionList={[
                    {
                      value: 'subscription_first',
                      label: disableSubscriptionPreference
                        ? `${t('优先订阅')} (${t('无生效')})`
                        : t('优先订阅'),
                      disabled: disableSubscriptionPreference,
                    },
                    { value: 'wallet_first', label: t('优先钱包') },
                    {
                      value: 'subscription_only',
                      label: disableSubscriptionPreference
                        ? `${t('仅用订阅')} (${t('无生效')})`
                        : t('仅用订阅'),
                      disabled: disableSubscriptionPreference,
                    },
                    { value: 'wallet_only', label: t('仅用钱包') },
                  ]}
                />
                <Button
                  size='small'
                  theme='light'
                  type='tertiary'
                  icon={
                    <RefreshCw
                      size={12}
                      className={refreshing ? 'animate-spin' : ''}
                    />
                  }
                  onClick={handleRefresh}
                  loading={refreshing}
                />
              </div>
            </div>
            {disableSubscriptionPreference && isSubscriptionPreference && (
              <Text type='tertiary' size='small'>
                {t('已保存偏好为')}
                {subscriptionPreferenceLabel}
                {t('，当前无生效订阅，将自动使用钱包')}
              </Text>
            )}

            {hasAnySubscription ? (
              <>
                <Divider margin={8} />
                <div className='max-h-64 overflow-y-auto pr-1 semi-table-body'>
                  {allSubscriptions.map((sub, subIndex) => {
                    const isLast = subIndex === allSubscriptions.length - 1;
                    const subscription = sub.subscription;
                    const totalAmount = Number(subscription?.amount_total || 0);
                    const usedAmount = Number(subscription?.amount_used || 0);
                    const remainAmount =
                      totalAmount > 0
                        ? Math.max(0, totalAmount - usedAmount)
                        : 0;
                    const planTitle =
                      planTitleMap.get(subscription?.plan_id) || '';
                    const remainDays = getRemainingDays(sub);
                    const usagePercent = getUsagePercent(sub);
                    const now = Date.now() / 1000;
                    const isExpired = (subscription?.end_time || 0) < now;
                    const isCancelled = subscription?.status === 'cancelled';
                    const isInactive =
                      subscription?.status === 'inactive' && !isExpired;
                    const isActive =
                      subscription?.status === 'active' && !isExpired;

                    return (
                      <div key={subscription?.id || subIndex}>
                        {/* 订阅概要 */}
                        <div className='flex items-center justify-between text-xs mb-2'>
                          <div className='flex items-center gap-2'>
                            <span className='font-medium'>
                              {planTitle
                                ? `${planTitle} · ${t('订阅')} #${subscription?.id}`
                                : `${t('订阅')} #${subscription?.id}`}
                            </span>
                            {isActive ? (
                              <Tag
                                color='white'
                                size='small'
                                shape='circle'
                                prefixIcon={<Badge dot type='success' />}
                              >
                                {t('生效')}
                              </Tag>
                            ) : isInactive ? (
                              <Tag color='orange' size='small' shape='circle'>
                                {t('未激活')}
                              </Tag>
                            ) : isCancelled ? (
                              <Tag color='white' size='small' shape='circle'>
                                {t('已作废')}
                              </Tag>
                            ) : (
                              <Tag color='white' size='small' shape='circle'>
                                {t('已过期')}
                              </Tag>
                            )}
                          </div>
                          {isActive && (
                            <span className='text-gray-500'>
                              {t('剩余')} {remainDays} {t('天')}
                            </span>
                          )}
                        </div>
                        <div className='text-xs text-gray-500 mb-2'>
                          {isActive || isInactive
                            ? t('至')
                            : isCancelled
                              ? t('作废于')
                              : t('过期于')}{' '}
                          {new Date(
                            (subscription?.end_time || 0) * 1000,
                          ).toLocaleString()}
                        </div>
                        {(isActive || isInactive) &&
                          subscription?.next_reset_time > 0 && (
                            <div className='text-xs text-gray-500 mb-2'>
                              {t('下一次重置')}:{' '}
                              {new Date(
                                subscription.next_reset_time * 1000,
                              ).toLocaleString()}
                            </div>
                          )}
                        <div className='text-xs text-gray-500 mb-2'>
                          {t('总额度')}:{' '}
                          {totalAmount > 0 ? (
                            <Tooltip
                              content={`${t('原生额度')}：${usedAmount}/${totalAmount} · ${t('剩余')} ${remainAmount}`}
                            >
                              <span>
                                {renderQuota(usedAmount)}/
                                {renderQuota(totalAmount)} · {t('剩余')}{' '}
                                {renderQuota(remainAmount)}
                              </span>
                            </Tooltip>
                          ) : (
                            t('不限')
                          )}
                          {totalAmount > 0 && (
                            <span className='ml-2'>
                              {t('已用')} {usagePercent}%
                            </span>
                          )}
                        </div>
                        {isInactive && (
                          <div className='flex justify-end mb-2'>
                            <Button
                              size='small'
                              theme='light'
                              type='primary'
                              loading={
                                switchingSubscriptionId === subscription?.id
                              }
                              onClick={() =>
                                handleSwitchSubscription(subscription?.id)
                              }
                            >
                              {t('切换到此订阅')}
                            </Button>
                          </div>
                        )}
                        {!isLast && <Divider margin={12} />}
                      </div>
                    );
                  })}
                </div>
              </>
            ) : (
              <div className='text-xs text-gray-500'>
                {t('购买套餐后即可享受模型权益')}
              </div>
            )}
          </Card>

          {/* 可购买套餐 - 标准定价卡片 */}
          {plans.length > 0 ? (
            <div className='grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-2 xl:grid-cols-3 gap-5 w-full px-1'>
              {plans.map((p, index) => {
                const plan = p?.plan;
                const totalAmount = Number(plan?.total_amount || 0);
                const { symbol, rate } = getCurrencyConfig();
                const price = Number(plan?.price_amount || 0);
                const convertedPrice = price * rate;
                const displayPrice = convertedPrice.toFixed(
                  Number.isInteger(convertedPrice) ? 0 : 2,
                );
                const isPopular = index === 0 && plans.length > 1;
                const limit = Number(plan?.max_purchase_per_user || 0);
                const globalLimit = Number(plan?.max_purchase_total || 0);
                const globalPurchaseCount = Number(plan?.purchase_count || 0);
                const limitLabel = limit > 0 ? `${t('限购')} ${limit}` : null;
                const globalLimitLabel =
                  globalLimit > 0
                    ? `${t('全局限购')}: ${globalPurchaseCount}/${globalLimit}`
                    : null;
                const globalResetLabel =
                  globalLimit > 0 &&
                  plan?.max_purchase_reset_period &&
                  plan.max_purchase_reset_period !== 'never'
                    ? `${t('名额刷新')}: ${formatGlobalPurchaseResetPeriod(plan, t)}`
                    : null;
                const totalLabel =
                  totalAmount > 0
                    ? `${t('总额度')}: ${renderQuota(totalAmount)}`
                    : `${t('总额度')}: ${t('不限')}`;
                const upgradeLabel = plan?.upgrade_group
                  ? `${plan.group_mode === 'attach' ? t('附加分组') : t('升级分组')}: ${plan.upgrade_group}`
                  : null;
                const tierSummary = formatTiersSummary(plan?.quota_tiers, t);
                const resetLabel = tierSummary
                  ? `${t('额度限制')}: ${tierSummary}`
                  : formatSubscriptionResetPeriod(plan, t) === t('不重置')
                    ? null
                    : `${t('额度重置')}: ${formatSubscriptionResetPeriod(plan, t)}`;
                const disableBalanceLabel = plan?.disable_balance_deduction
                  ? t('已禁用余额扣费')
                  : null;
                const planBenefits = [
                  {
                    label: `${t('有效期')}: ${formatSubscriptionDuration(plan, t)}`,
                  },
                  resetLabel ? { label: resetLabel } : null,
                  !tierSummary && totalAmount > 0
                    ? {
                        label: totalLabel,
                        tooltip: `${t('原生额度')}：${totalAmount}`,
                      }
                    : !tierSummary
                      ? { label: totalLabel }
                      : null,
                  limitLabel ? { label: limitLabel } : null,
                  globalLimitLabel ? { label: globalLimitLabel } : null,
                  globalResetLabel ? { label: globalResetLabel } : null,
                  upgradeLabel ? { label: upgradeLabel } : null,
                  disableBalanceLabel ? { label: disableBalanceLabel } : null,
                ].filter(Boolean);

                return (
                  <Card
                    key={plan?.id}
                    className={`!rounded-xl transition-all hover:shadow-lg w-full h-full ${
                      isPopular ? 'ring-2 ring-purple-500' : ''
                    }`}
                    bodyStyle={{ padding: 0 }}
                  >
                    <div className='p-4 h-full flex flex-col'>
                      {/* 推荐标签 */}
                      {isPopular && (
                        <div className='mb-2'>
                          <Tag color='purple' shape='circle' size='small'>
                            <Sparkles size={10} className='mr-1' />
                            {t('推荐')}
                          </Tag>
                        </div>
                      )}
                      {/* 套餐名称 */}
                      <div className='mb-3'>
                        <Typography.Title
                          heading={5}
                          ellipsis={{ rows: 1, showTooltip: true }}
                          style={{ margin: 0 }}
                        >
                          {plan?.title || t('订阅套餐')}
                        </Typography.Title>
                        {plan?.subtitle && (
                          <Text
                            type='tertiary'
                            size='small'
                            ellipsis={{ rows: 1, showTooltip: true }}
                            style={{ display: 'block' }}
                          >
                            {plan.subtitle}
                          </Text>
                        )}
                      </div>

                      {/* 价格区域 */}
                      <div className='py-2'>
                        <div className='flex items-baseline justify-start'>
                          <span className='text-xl font-bold text-purple-600'>
                            {symbol}
                          </span>
                          <span className='text-3xl font-bold text-purple-600'>
                            {displayPrice}
                          </span>
                        </div>
                      </div>

                      {/* 套餐权益描述 */}
                      <div className='flex flex-col items-start gap-1 pb-2'>
                        {planBenefits.map((item) => {
                          const content = (
                            <div className='flex items-center gap-2 text-xs text-gray-500'>
                              <Badge dot type='tertiary' />
                              <span>{item.label}</span>
                            </div>
                          );
                          if (!item.tooltip) {
                            return (
                              <div
                                key={item.label}
                                className='w-full flex justify-start'
                              >
                                {content}
                              </div>
                            );
                          }
                          return (
                            <Tooltip key={item.label} content={item.tooltip}>
                              <div className='w-full flex justify-start'>
                                {content}
                              </div>
                            </Tooltip>
                          );
                        })}
                      </div>

                      <div className='mt-auto'>
                        <Divider margin={12} />

                        {/* 购买按钮 */}
                        {(() => {
                          const planId = plan?.id;
                          const count = getPlanPurchaseCount(planId);
                          const holdsLive = livePlanIdSet.has(planId);
                          // 全局限购套餐不提供持有期续期，名额到期释放后重新购买
                          const renewable = globalLimit <= 0 && holdsLive;
                          // 追加分组订阅并存生效、额度叠加，多份有意义
                          const stacksAttached =
                            plan?.group_mode === 'attach' &&
                            !!plan?.upgrade_group;
                          // 全局限购套餐既不续期也不重复购买：重复购买只会新建排队订阅，
                          // 不自动接棒却多占一个名额
                          const holdingSeat =
                            globalLimit > 0 && holdsLive && !stacksAttached;
                          const reachedPerUser = limit > 0 && count >= limit;
                          const soldOut =
                            globalLimit > 0 &&
                            globalPurchaseCount >= globalLimit;
                          // 持有者续期不新建订阅、不占名额，限购不禁用按钮
                          const reached =
                            !renewable &&
                            (holdingSeat || reachedPerUser || soldOut);
                          const tip = renewable
                            ? t('已持有该套餐，购买将顺延到期时间')
                            : holdingSeat
                              ? t('已持有该套餐，到期后可重新购买')
                              : soldOut
                                ? globalResetLabel
                                  ? `${t('该套餐已售罄')} · ${globalResetLabel}`
                                  : t('该套餐已售罄')
                                : reachedPerUser
                                  ? t('已达到购买上限') + ` (${count}/${limit})`
                                  : '';
                          const buttonEl = (
                            <Button
                              theme='outline'
                              type='primary'
                              block
                              disabled={reached}
                              onClick={() => {
                                if (!reached) openBuy(p);
                              }}
                            >
                              {renewable
                                ? t('续期')
                                : reached
                                  ? holdingSeat
                                    ? t('已持有')
                                    : soldOut
                                      ? t('已售罄')
                                      : t('已达上限')
                                  : t('立即订阅')}
                            </Button>
                          );
                          return tip ? (
                            <Tooltip content={tip} position='top'>
                              {buttonEl}
                            </Tooltip>
                          ) : (
                            buttonEl
                          );
                        })()}
                      </div>
                    </div>
                  </Card>
                );
              })}
            </div>
          ) : (
            <div className='text-center text-gray-400 text-sm py-4'>
              {t('暂无可购买套餐')}
            </div>
          )}
        </Space>
      )}
    </>
  );

  return (
    <>
      {withCard ? (
        <Card className='!rounded-2xl shadow-sm border-0'>{cardContent}</Card>
      ) : (
        <div className='space-y-3'>{cardContent}</div>
      )}

      {/* 购买确认弹窗 */}
      <SubscriptionPurchaseModal
        t={t}
        visible={open}
        onCancel={closeBuy}
        selectedPlan={selectedPlan}
        paying={paying}
        isRenewal={
          Number(selectedPlan?.plan?.max_purchase_total || 0) <= 0 &&
          livePlanIdSet.has(selectedPlan?.plan?.id)
        }
        selectedPayMethod={selectedPayMethod}
        setSelectedPayMethod={setSelectedPayMethod}
        epayMethods={epayMethods}
        payMethods={payMethods}
        enableOnlineTopUp={enableOnlineTopUp}
        enableStripeTopUp={enableStripeTopUp}
        enableCreemTopUp={enableCreemTopUp}
        enableEthereumTopUp={enableEthereumTopUp}
        ethereumInfo={ethereumInfo}
        purchaseLimitInfo={
          selectedPlan?.plan?.id
            ? {
                limit: Number(selectedPlan?.plan?.max_purchase_per_user || 0),
                count: getPlanPurchaseCount(selectedPlan?.plan?.id),
                global_limit: Number(
                  selectedPlan?.plan?.max_purchase_total || 0,
                ),
                global_count: Number(selectedPlan?.plan?.purchase_count || 0),
                global_reset_label:
                  Number(selectedPlan?.plan?.max_purchase_total || 0) > 0 &&
                  selectedPlan?.plan?.max_purchase_reset_period &&
                  selectedPlan.plan.max_purchase_reset_period !== 'never'
                    ? formatGlobalPurchaseResetPeriod(selectedPlan.plan, t)
                    : '',
              }
            : null
        }
        onPayStripe={payStripe}
        onPayCreem={payCreem}
        onPayEpay={payEpay}
        onPayEthereum={payEthereum}
        onPayBalance={payBalance}
      />
      <WalletConnectQrModal
        t={t}
        visible={walletConnectModalVisible}
        uri={walletConnectUri}
        onCancel={() => setWalletConnectModalVisible(false)}
      />
    </>
  );
};

export default SubscriptionPlansCard;
