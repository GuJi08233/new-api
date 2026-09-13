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

import React, { useEffect } from 'react';
import {
  Banner,
  Modal,
  Typography,
  Card,
  Button,
  Select,
  Divider,
  Tooltip,
} from '@douyinfe/semi-ui';
import { Crown, CalendarClock, Package } from 'lucide-react';
import { renderQuota } from '../../../helpers';
import { getCurrencyConfig } from '../../../helpers/render';
import {
  formatSubscriptionDuration,
  formatSubscriptionResetPeriod,
} from '../../../helpers/subscriptionFormat';
import {
  SUBSCRIPTION_BALANCE_PAY_METHOD,
  buildSubscriptionPayOptions,
  normalizeSubscriptionPayMethod,
  parseAllowedPaymentMethods,
} from '../../../helpers/subscriptionPayment';

const { Text } = Typography;

const SubscriptionPurchaseModal = ({
  t,
  visible,
  onCancel,
  selectedPlan,
  paying,
  isRenewal = false,
  selectedPayMethod,
  setSelectedPayMethod,
  payMethods = [],
  enableOnlineTopUp = false,
  enableStripeTopUp = false,
  enableCreemTopUp = false,
  enableEthereumTopUp = false,
  ethereumInfo = null,
  purchaseLimitInfo = null,
  onPayStripe,
  onPayCreem,
  onPayEpay,
  onPayEthereum,
  onPayBalance,
}) => {
  const plan = selectedPlan?.plan;
  const totalAmount = Number(plan?.total_amount || 0);
  const { symbol, rate } = getCurrencyConfig();
  const price = plan ? Number(plan.price_amount || 0) : 0;
  const convertedPrice = price * rate;
  const displayPrice = convertedPrice.toFixed(
    Number.isInteger(convertedPrice) ? 0 : 2,
  );
  // 统一支付方式下拉框：易支付通道 + Stripe/Creem + 加密货币 + 余额支付
  const allowedPayMethods = parseAllowedPaymentMethods(
    plan?.allowed_payment_methods,
  );
  const payOptions = buildSubscriptionPayOptions({
    payMethods,
    enableOnlineTopUp,
    enableStripeTopUp,
    enableCreemTopUp,
    enableEthereumTopUp,
    ethereumInfo,
    balanceLabel: t('余额支付'),
  }).filter((option) => {
    // 站点开了网关还不够，套餐得配了对应商品 ID 才买得成
    if (option.value === 'stripe' && !plan?.stripe_price_id) return false;
    if (option.value === 'creem' && !plan?.creem_product_id) return false;
    if (
      option.value === SUBSCRIPTION_BALANCE_PAY_METHOD &&
      plan?.allow_balance_pay === false
    ) {
      return false;
    }
    // 套餐自己的支付方式白名单，为空表示不限制
    return (
      allowedPayMethods.length === 0 ||
      allowedPayMethods.includes(normalizeSubscriptionPayMethod(option.value))
    );
  });
  const hasAnyPayment = payOptions.length > 0;

  useEffect(() => {
    if (!visible || payOptions.length === 0) return;
    if (!payOptions.some((option) => option.value === selectedPayMethod)) {
      setSelectedPayMethod(payOptions[0].value);
    }
  });

  const handleUnifiedPay = () => {
    if (!selectedPayMethod) return;
    if (selectedPayMethod === 'stripe') {
      onPayStripe?.();
      return;
    }
    if (selectedPayMethod === 'creem') {
      onPayCreem?.();
      return;
    }
    if (selectedPayMethod === 'balance') {
      onPayBalance?.();
      return;
    }
    if (selectedPayMethod.startsWith('ethereum:')) {
      onPayEthereum?.(selectedPayMethod.slice('ethereum:'.length));
      return;
    }
    onPayEpay?.(selectedPayMethod);
  };

  const purchaseLimit = Number(purchaseLimitInfo?.limit || 0);
  const purchaseCount = Number(purchaseLimitInfo?.count || 0);
  const globalPurchaseLimit = Number(purchaseLimitInfo?.global_limit || 0);
  const globalPurchaseCount = Number(purchaseLimitInfo?.global_count || 0);
  const globalResetLabel = purchaseLimitInfo?.global_reset_label || '';
  const purchaseLimitReached =
    purchaseLimit > 0 && purchaseCount >= purchaseLimit;
  const globalLimitReached =
    globalPurchaseLimit > 0 && globalPurchaseCount >= globalPurchaseLimit;
  // 续期不新建订阅、不占名额，限购不阻拦
  const anyLimitReached =
    !isRenewal && (purchaseLimitReached || globalLimitReached);

  return (
    <Modal
      title={
        <div className='flex items-center'>
          <Crown className='mr-2' size={18} />
          {t('购买订阅套餐')}
        </div>
      }
      visible={visible}
      onCancel={onCancel}
      footer={null}
      size='small'
      centered
    >
      {plan ? (
        <div className='space-y-4 pb-10'>
          {/* 套餐信息 */}
          <Card className='!rounded-xl !border-0 bg-slate-50 dark:bg-slate-800'>
            <div className='space-y-3'>
              <div className='flex justify-between items-center'>
                <Text strong className='text-slate-700 dark:text-slate-200'>
                  {t('套餐名称')}：
                </Text>
                <Typography.Text
                  ellipsis={{ rows: 1, showTooltip: true }}
                  className='text-slate-900 dark:text-slate-100'
                  style={{ maxWidth: 200 }}
                >
                  {plan.title}
                </Typography.Text>
              </div>
              <div className='flex justify-between items-center'>
                <Text strong className='text-slate-700 dark:text-slate-200'>
                  {t('有效期')}：
                </Text>
                <div className='flex items-center'>
                  <CalendarClock size={14} className='mr-1 text-slate-500' />
                  <Text className='text-slate-900 dark:text-slate-100'>
                    {formatSubscriptionDuration(plan, t)}
                  </Text>
                </div>
              </div>
              {formatSubscriptionResetPeriod(plan, t) !== t('不重置') && (
                <div className='flex justify-between items-center'>
                  <Text strong className='text-slate-700 dark:text-slate-200'>
                    {t('重置周期')}：
                  </Text>
                  <Text className='text-slate-900 dark:text-slate-100'>
                    {formatSubscriptionResetPeriod(plan, t)}
                  </Text>
                </div>
              )}
              <div className='flex justify-between items-center'>
                <Text strong className='text-slate-700 dark:text-slate-200'>
                  {t('总额度')}：
                </Text>
                <div className='flex items-center'>
                  <Package size={14} className='mr-1 text-slate-500' />
                  {totalAmount > 0 ? (
                    <Tooltip content={`${t('原生额度')}：${totalAmount}`}>
                      <Text className='text-slate-900 dark:text-slate-100'>
                        {renderQuota(totalAmount)}
                      </Text>
                    </Tooltip>
                  ) : (
                    <Text className='text-slate-900 dark:text-slate-100'>
                      {t('不限')}
                    </Text>
                  )}
                </div>
              </div>
              {plan?.upgrade_group ? (
                <div className='flex justify-between items-center'>
                  <Text strong className='text-slate-700 dark:text-slate-200'>
                    {plan.group_mode === 'attach'
                      ? t('附加分组')
                      : t('升级分组')}
                    ：
                  </Text>
                  <Text className='text-slate-900 dark:text-slate-100'>
                    {plan.upgrade_group}
                  </Text>
                </div>
              ) : null}
              <Divider margin={8} />
              <div className='flex justify-between items-center'>
                <Text strong className='text-slate-700 dark:text-slate-200'>
                  {t('应付金额')}：
                </Text>
                <Text strong className='text-xl text-purple-600'>
                  {symbol}
                  {displayPrice}
                </Text>
              </div>
            </div>
          </Card>

          {/* 续期提示 */}
          {isRenewal && (
            <Banner
              type='info'
              description={`${t('已持有该套餐，本次支付将为现有订阅续期')}，${t('到期时间顺延')} ${formatSubscriptionDuration(plan, t)}`}
              className='!rounded-xl'
              closeIcon={null}
            />
          )}

          {/* 支付方式 */}
          {anyLimitReached && (
            <Banner
              type='warning'
              description={
                globalLimitReached
                  ? globalResetLabel
                    ? `${t('该套餐已售罄')} · ${t('名额刷新')}: ${globalResetLabel}`
                    : t('该套餐已售罄')
                  : `${t('已达到购买上限')} (${purchaseCount}/${purchaseLimit})`
              }
              className='!rounded-xl'
              closeIcon={null}
            />
          )}

          {hasAnyPayment ? (
            <div className='space-y-3'>
              <Text size='small' type='tertiary'>
                {t('选择支付方式')}：
              </Text>

              <div className='flex gap-2'>
                <Select
                  value={selectedPayMethod}
                  onChange={setSelectedPayMethod}
                  style={{ flex: 1 }}
                  size='default'
                  placeholder={t('选择支付方式')}
                  optionList={payOptions}
                  disabled={anyLimitReached}
                />
                <Button
                  theme='solid'
                  type='primary'
                  onClick={handleUnifiedPay}
                  loading={paying}
                  disabled={!selectedPayMethod || anyLimitReached}
                >
                  {t('支付')}
                </Button>
              </div>
            </div>
          ) : (
            <Banner
              type='info'
              description={t('管理员未开启在线支付功能，请联系管理员配置。')}
              className='!rounded-xl'
              closeIcon={null}
            />
          )}
        </div>
      ) : null}
    </Modal>
  );
};

export default SubscriptionPurchaseModal;
