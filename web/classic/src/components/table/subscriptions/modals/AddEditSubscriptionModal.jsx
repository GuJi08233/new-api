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

import React, { useEffect, useState, useRef, useCallback } from 'react';
import {
  Avatar,
  Banner,
  Button,
  Card,
  Checkbox,
  CheckboxGroup,
  Col,
  Form,
  InputNumber,
  Row,
  Select,
  SideSheet,
  Space,
  Spin,
  Switch,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IconCalendarClock,
  IconClose,
  IconCreditCard,
  IconDelete,
  IconPlus,
  IconSave,
} from '@douyinfe/semi-icons';
import { Clock, RefreshCw } from 'lucide-react';
import { API, showError, showSuccess } from '../../../../helpers';
import {
  quotaToDisplayAmount,
  displayAmountToQuota,
} from '../../../../helpers/quota';
import {
  formatSubscriptionResetPeriod,
  formatSubscriptionQuotaResetHint,
} from '../../../../helpers/subscriptionFormat';
import {
  buildSubscriptionQuotaConfig,
  buildSubscriptionQuotaTiers,
  getSubscriptionQuotaMode,
} from '../../../../helpers/subscriptionQuota';
import {
  formatSubscriptionPeriodDuration,
  parseSubscriptionPeriodDuration,
} from '../../../../helpers/subscriptionDuration';
import SubscriptionPeriodDurationInput from '../../../common/ui/SubscriptionPeriodDurationInput';
import { getCurrencyConfig, renderQuota } from '../../../../helpers/render';
import {
  SUBSCRIPTION_BALANCE_PAY_METHOD,
  buildSubscriptionPayOptions,
  parseAllowedPaymentMethods,
} from '../../../../helpers/subscriptionPayment';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';

const { Text, Title } = Typography;

const durationUnitOptions = [
  { value: 'year', label: '年' },
  { value: 'month', label: '月' },
  { value: 'day', label: '日' },
  { value: 'hour', label: '小时' },
  { value: 'custom', label: '自定义(秒)' },
];

const resetPeriodOptions = [
  { value: 'never', label: '不重置' },
  { value: 'daily', label: '每自然日' },
  { value: 'weekly', label: '每自然周' },
  { value: 'monthly', label: '每自然月' },
  { value: 'custom', label: '自定义(秒)' },
];

const purchaseResetPeriodOptions = [
  { value: 'never', label: '不重置' },
  { value: 'daily', label: '每天' },
  { value: 'weekly', label: '每周' },
  { value: 'monthly', label: '每月' },
  { value: 'custom', label: '自定义(秒)' },
  { value: 'active', label: '到期释放名额' },
];

const tierPeriodOptions = [
  { value: 'monthly', label: '每自然月' },
  { value: 'weekly', label: '每自然周' },
  { value: 'daily', label: '每自然日' },
  { value: 'hourly', label: '每小时' },
  { value: 'custom', label: '自定义时长' },
];

const AddEditSubscriptionModal = ({
  visible,
  handleClose,
  editingPlan,
  placement = 'left',
  refresh,
  t,
}) => {
  const [loading, setLoading] = useState(false);
  const [groupOptions, setGroupOptions] = useState([]);
  const [groupLoading, setGroupLoading] = useState(false);
  const [quotaMode, setQuotaMode] = useState('multi');
  const [legacyResetDuration, setLegacyResetDuration] = useState('');
  const [preserveLegacyQuota, setPreserveLegacyQuota] = useState(false);
  const [quotaTiers, setQuotaTiers] = useState([
    { period: 'monthly', limit: 0, custom_seconds: 0, sort_priority: 10 },
  ]);
  const useMultiTier = quotaMode === 'multi';
  const quotaCurrency = getCurrencyConfig();
  const [disableBalanceDeduction, setDisableBalanceDeduction] = useState(false);
  // 站点当前开放的支付方式；为空表示还没拉到，提交时不去动套餐的支付配置
  const [payMethodOptions, setPayMethodOptions] = useState([]);
  const [allowedPayMethods, setAllowedPayMethods] = useState([]);
  const isMobile = useIsMobile();
  const formApiRef = useRef(null);
  const isEdit = editingPlan?.plan?.id !== undefined;
  const originalQuotaMode = isEdit
    ? getSubscriptionQuotaMode(editingPlan.plan)
    : null;
  const formKey = isEdit ? `edit-${editingPlan?.plan?.id}` : 'create';

  const normalizeTierLimitForForm = useCallback((limit) => {
    return Number(quotaToDisplayAmount(limit || 0).toFixed(2));
  }, []);

  const normalizeTierLimitForPayload = useCallback((limit) => {
    return displayAmountToQuota(limit || 0);
  }, []);

  const mapQuotaTiersToForm = useCallback(
    (tiers) => {
      if (!Array.isArray(tiers)) return [];
      return tiers.map((tier, index) => ({
        period: tier?.period || 'monthly',
        limit: normalizeTierLimitForForm(tier?.limit || 0),
        custom_seconds: Number(tier?.custom_seconds || 0),
        duration_text: formatSubscriptionPeriodDuration(tier?.custom_seconds),
        sort_priority: Number(tier?.sort_priority || (index + 1) * 10),
      }));
    },
    [normalizeTierLimitForForm],
  );

  const mapQuotaTiersToPayload = useCallback(
    (tiers) => {
      return buildSubscriptionQuotaTiers(tiers, normalizeTierLimitForPayload);
    },
    [normalizeTierLimitForPayload],
  );

  const addTier = useCallback(() => {
    setQuotaTiers((prev) => [
      ...prev,
      {
        period: 'monthly',
        limit: 0,
        custom_seconds: 0,
        duration_text: '',
        sort_priority: (prev.length + 1) * 10,
      },
    ]);
  }, []);

  const removeTier = useCallback((index) => {
    setQuotaTiers((prev) => prev.filter((_, i) => i !== index));
  }, []);

  const updateTier = useCallback((index, field, value) => {
    setQuotaTiers((prev) =>
      prev.map((tier, i) => (i === index ? { ...tier, [field]: value } : tier)),
    );
  }, []);

  const getInitValues = () => ({
    title: '',
    subtitle: '',
    price_amount: 0,
    currency: 'USD',
    duration_unit: 'month',
    duration_value: 1,
    custom_seconds: 0,
    quota_reset_period: 'never',
    quota_reset_custom_seconds: 0,
    enabled: true,
    sort_order: 0,
    max_purchase_per_user: 0,
    max_purchase_total: 0,
    max_purchase_reset_period: 'never',
    max_purchase_reset_custom_seconds: 0,
    total_amount: 0,
    upgrade_group: '',
    group_mode: 'upgrade',
    stripe_price_id: '',
    creem_product_id: '',
  });

  const buildFormValues = () => {
    const base = getInitValues();
    if (editingPlan?.plan?.id === undefined) return base;
    const p = editingPlan.plan || {};
    return {
      ...base,
      title: p.title || '',
      subtitle: p.subtitle || '',
      price_amount: Number(p.price_amount || 0),
      currency: 'USD',
      duration_unit: p.duration_unit || 'month',
      duration_value: Number(p.duration_value || 1),
      custom_seconds: Number(p.custom_seconds || 0),
      quota_reset_period: p.quota_reset_period || 'never',
      quota_reset_custom_seconds: Number(p.quota_reset_custom_seconds || 0),
      enabled: p.enabled !== false,
      sort_order: Number(p.sort_order || 0),
      max_purchase_per_user: Number(p.max_purchase_per_user || 0),
      max_purchase_total: Number(p.max_purchase_total || 0),
      max_purchase_reset_period: p.max_purchase_reset_period || 'never',
      max_purchase_reset_custom_seconds: Number(
        p.max_purchase_reset_custom_seconds || 0,
      ),
      total_amount: Number(
        quotaToDisplayAmount(p.total_amount || 0).toFixed(2),
      ),
      upgrade_group: p.upgrade_group || '',
      group_mode: p.group_mode === 'attach' ? 'attach' : 'upgrade',
      stripe_price_id: p.stripe_price_id || '',
      creem_product_id: p.creem_product_id || '',
    };
  };

  useEffect(() => {
    if (!visible) return;
    setGroupLoading(true);
    API.get('/api/group')
      .then((res) => {
        if (res.data?.success) {
          setGroupOptions(res.data?.data || []);
        } else {
          setGroupOptions([]);
        }
      })
      .catch(() => setGroupOptions([]))
      .finally(() => setGroupLoading(false));
    const plan = editingPlan?.plan;
    let tiers = [];
    try {
      const parsed = JSON.parse(plan?.quota_tiers || '[]');
      if (Array.isArray(parsed)) tiers = parsed;
    } catch {
      // Invalid historical tier data falls back to the legacy quota fields.
    }
    const mode = getSubscriptionQuotaMode(plan);
    setQuotaMode(mode);
    setLegacyResetDuration(
      formatSubscriptionPeriodDuration(plan?.quota_reset_custom_seconds),
    );
    setPreserveLegacyQuota(
      tiers.length > 0 &&
        (Number(plan.total_amount || 0) > 0 ||
          (plan.quota_reset_period || 'never') !== 'never'),
    );
    setQuotaTiers(
      tiers.length > 0
        ? mapQuotaTiersToForm(tiers)
        : [
            {
              period: 'monthly',
              limit: 0,
              custom_seconds: 0,
              duration_text: '',
              sort_priority: 10,
            },
          ],
    );
    setDisableBalanceDeduction(plan?.disable_balance_deduction || false);

    // 勾选项来自“额度充值”里已配置的支付方式，再加上余额支付
    setPayMethodOptions([]);
    setAllowedPayMethods([]);
    API.get('/api/user/topup/info')
      .then((res) => {
        if (!res.data?.success) return;
        const info = res.data.data || {};
        const options = buildSubscriptionPayOptions({
          payMethods: info.pay_methods || [],
          enableOnlineTopUp: info.enable_online_topup,
          enableStripeTopUp: info.enable_stripe_topup,
          enableCreemTopUp: info.enable_creem_topup,
          enableEthereumTopUp: info.enable_ethereum_topup,
          ethereumInfo: info.ethereum_info,
          balanceLabel: t('余额支付'),
        });
        setPayMethodOptions(options);

        // 白名单为空 = 不限制，回显成全选；老套餐可能只关了 allow_balance_pay
        const stored = parseAllowedPaymentMethods(
          plan?.allowed_payment_methods,
        );
        setAllowedPayMethods(
          stored.length > 0
            ? stored
            : options
                .map((option) => option.value)
                .filter(
                  (value) =>
                    value !== SUBSCRIPTION_BALANCE_PAY_METHOD ||
                    plan?.allow_balance_pay !== false,
                ),
        );
      })
      .catch(() => setPayMethodOptions([]));
  }, [visible, editingPlan, mapQuotaTiersToForm, t]);

  const TIER_PERIOD_ORDER = {
    hourly: 3600,
    daily: 86400,
    weekly: 604800,
    monthly: 2592000,
    none: Infinity,
  };

  const getTierOrder = (tier) => {
    if (tier.period === 'custom') return tier.custom_seconds;
    return TIER_PERIOD_ORDER[tier.period];
  };

  const validateTiers = (tiers) => {
    if (tiers.length === 0) return t('请至少添加一项额度限制');
    const periods = new Set();
    for (const tier of tiers) {
      if (!Number.isSafeInteger(tier.limit) || tier.limit <= 0) {
        return t('每项限额必须大于0且不超过安全额度上限');
      }
      if (
        tier.period === 'custom' &&
        (!Number.isSafeInteger(tier.custom_seconds) || tier.custom_seconds <= 0)
      ) {
        return t('请输入有效时长，例如 5h、1d 或 1h30m');
      }
      const periodKey =
        tier.period === 'custom' || tier.period === 'hourly'
          ? `window:${tier.period === 'hourly' ? 3600 : tier.custom_seconds}`
          : tier.period;
      if (periods.has(periodKey)) return t('同一周期只能配置一项限额');
      periods.add(periodKey);
    }
    if (tiers.length <= 1) return null;
    const sorted = [...tiers].sort((a, b) => getTierOrder(a) - getTierOrder(b));
    for (let i = 1; i < sorted.length; i++) {
      const prev = sorted[i - 1];
      const curr = sorted[i];
      if (curr.limit > 0 && prev.limit > 0 && curr.limit < prev.limit) {
        return t('层级限额必须大于或等于上一层级');
      }
    }
    return null;
  };

  const submit = useCallback(
    async (values) => {
      if (!values.title || values.title.trim() === '') {
        showError(t('套餐标题不能为空'));
        return;
      }
      // Validate tier ordering
      if (useMultiTier && !preserveLegacyQuota) {
        const activeTiers = mapQuotaTiersToPayload(quotaTiers);
        const error = validateTiers(activeTiers);
        if (error) {
          showError(error);
          return;
        }
      }
      if (
        !preserveLegacyQuota &&
        (quotaMode === 'total' || quotaMode === 'periodic')
      ) {
        const amount = displayAmountToQuota(values.total_amount);
        if (!Number.isSafeInteger(amount) || amount <= 0) {
          showError(t('额度必须大于0且不超过安全额度上限'));
          return;
        }
        if (
          quotaMode === 'periodic' &&
          (!values.quota_reset_period || values.quota_reset_period === 'never')
        ) {
          showError(t('请选择额度重置周期'));
          return;
        }
        if (
          quotaMode === 'periodic' &&
          values.quota_reset_period === 'custom' &&
          parseSubscriptionPeriodDuration(legacyResetDuration) === null
        ) {
          showError(t('请输入有效时长，例如 5h、1d 或 1h30m'));
          return;
        }
      }
      // 支付方式：全选时存空串表示不限制，以后新接的支付渠道自动可用；
      // 选项没拉到时整块跳过，避免把套餐现有的支付配置清成不可买
      const allPayMethods = payMethodOptions.map((option) => option.value);
      const selectedPayMethods = allowedPayMethods.filter((value) =>
        allPayMethods.includes(value),
      );
      if (allPayMethods.length > 0 && selectedPayMethods.length === 0) {
        showError(t('请至少选择一种支付方式'));
        return;
      }
      const payMethodPatch =
        allPayMethods.length > 0
          ? {
              allowed_payment_methods:
                selectedPayMethods.length === allPayMethods.length
                  ? ''
                  : selectedPayMethods.join(','),
              allow_balance_pay: selectedPayMethods.includes(
                SUBSCRIPTION_BALANCE_PAY_METHOD,
              ),
            }
          : {};
      setLoading(true);
      try {
        const effectiveTiers = useMultiTier
          ? mapQuotaTiersToPayload(quotaTiers)
          : [];
        const payload = {
          plan: {
            ...editingPlan?.plan,
            ...values,
            ...payMethodPatch,
            ...buildSubscriptionQuotaConfig({
              mode: quotaMode,
              totalAmount: displayAmountToQuota(values.total_amount),
              resetPeriod: values.quota_reset_period,
              customSeconds:
                parseSubscriptionPeriodDuration(legacyResetDuration),
              tiers: effectiveTiers,
              legacyPlan: preserveLegacyQuota ? editingPlan.plan : null,
            }),
            price_amount: Number(values.price_amount || 0),
            currency: 'USD',
            duration_value: Number(values.duration_value || 0),
            custom_seconds: Number(values.custom_seconds || 0),
            sort_order: Number(values.sort_order || 0),
            max_purchase_per_user: Number(values.max_purchase_per_user || 0),
            max_purchase_total: Number(values.max_purchase_total || 0),
            max_purchase_reset_period:
              Number(values.max_purchase_total || 0) > 0
                ? values.max_purchase_reset_period || 'never'
                : 'never',
            max_purchase_reset_custom_seconds:
              Number(values.max_purchase_total || 0) > 0 &&
              values.max_purchase_reset_period === 'custom'
                ? Number(values.max_purchase_reset_custom_seconds || 0)
                : 0,
            upgrade_group: values.upgrade_group || '',
            group_mode: values.group_mode === 'attach' ? 'attach' : '',
            downgrade_group:
              values.group_mode === 'attach'
                ? ''
                : editingPlan?.plan?.downgrade_group || '',
            disable_balance_deduction: disableBalanceDeduction,
          },
        };
        if (editingPlan?.plan?.id) {
          const res = await API.put(
            `/api/subscription/admin/plans/${editingPlan.plan.id}`,
            payload,
          );
          if (res.data?.success) {
            showSuccess(t('更新成功'));
            handleClose();
            refresh?.();
          } else {
            showError(res.data?.message || t('更新失败'));
          }
        } else {
          const res = await API.post('/api/subscription/admin/plans', payload);
          if (res.data?.success) {
            showSuccess(t('创建成功'));
            handleClose();
            refresh?.();
          } else {
            showError(res.data?.message || t('创建失败'));
          }
        }
      } catch (e) {
        showError(t('请求失败'));
      } finally {
        setLoading(false);
      }
    },
    [
      allowedPayMethods,
      disableBalanceDeduction,
      editingPlan,
      handleClose,
      mapQuotaTiersToPayload,
      legacyResetDuration,
      payMethodOptions,
      quotaMode,
      preserveLegacyQuota,
      quotaTiers,
      refresh,
      t,
      useMultiTier,
    ],
  );

  return (
    <>
      <SideSheet
        placement={placement}
        title={
          <Space>
            {isEdit ? (
              <Tag color='blue' shape='circle'>
                {t('更新')}
              </Tag>
            ) : (
              <Tag color='green' shape='circle'>
                {t('新建')}
              </Tag>
            )}
            <Title heading={4} className='m-0'>
              {isEdit ? t('更新套餐信息') : t('创建新的订阅套餐')}
            </Title>
          </Space>
        }
        bodyStyle={{ padding: '0' }}
        visible={visible}
        width={isMobile ? '100%' : 600}
        footer={
          <div className='flex justify-end bg-white'>
            <Space>
              <Button
                theme='solid'
                onClick={() => formApiRef.current?.submitForm()}
                icon={<IconSave />}
                loading={loading}
              >
                {t('提交')}
              </Button>
              <Button
                theme='light'
                type='primary'
                onClick={handleClose}
                icon={<IconClose />}
              >
                {t('取消')}
              </Button>
            </Space>
          </div>
        }
        closeIcon={null}
        onCancel={handleClose}
      >
        <Spin spinning={loading}>
          <Form
            key={formKey}
            initValues={buildFormValues()}
            getFormApi={(api) => (formApiRef.current = api)}
            onSubmit={submit}
          >
            {({ values }) => (
              <div className='p-2'>
                {/* 基本信息 */}
                <Card className='!rounded-2xl shadow-sm border-0 mb-4'>
                  <div className='flex items-center mb-2'>
                    <Avatar
                      size='small'
                      color='blue'
                      className='mr-2 shadow-md'
                    >
                      <IconCalendarClock size={16} />
                    </Avatar>
                    <div>
                      <Text className='text-lg font-medium'>
                        {t('基本信息')}
                      </Text>
                      <div className='text-xs text-gray-600'>
                        {t('套餐的基本信息和定价')}
                      </div>
                    </div>
                  </div>

                  <Row gutter={12}>
                    <Col span={24}>
                      <Form.Input
                        field='title'
                        label={t('套餐标题')}
                        placeholder={t('例如：基础套餐')}
                        required
                        rules={[
                          { required: true, message: t('请输入套餐标题') },
                        ]}
                        showClear
                      />
                    </Col>

                    <Col span={24}>
                      <Form.Input
                        field='subtitle'
                        label={t('套餐副标题')}
                        placeholder={t('例如：适合轻度使用')}
                        showClear
                      />
                    </Col>

                    <Col span={12}>
                      <Form.InputNumber
                        field='price_amount'
                        label={t('实付金额')}
                        required
                        min={0}
                        precision={2}
                        rules={[{ required: true, message: t('请输入金额') }]}
                        style={{ width: '100%' }}
                      />
                    </Col>

                    <Col span={12}>
                      <Form.Select
                        field='group_mode'
                        label={t('分组模式')}
                        extraText={t(
                          '提升：购买后替换用户分组；追加：仅授予该分组使用权，不改变用户分组',
                        )}
                      >
                        <Select.Option value='upgrade'>
                          {t('提升分组')}
                        </Select.Option>
                        <Select.Option value='attach'>
                          {t('追加分组')}
                        </Select.Option>
                      </Form.Select>
                    </Col>

                    <Col span={12}>
                      <Form.Select
                        field='upgrade_group'
                        label={
                          values.group_mode === 'attach'
                            ? t('附加分组')
                            : t('升级分组')
                        }
                        showClear
                        loading={groupLoading}
                        placeholder={
                          values.group_mode === 'attach'
                            ? t('请选择分组')
                            : t('不升级')
                        }
                        rules={
                          values.group_mode === 'attach'
                            ? [
                                {
                                  required: true,
                                  message: t('请选择分组'),
                                },
                              ]
                            : []
                        }
                        extraText={
                          values.group_mode === 'attach'
                            ? t(
                                '订阅生效期间为用户追加该分组的使用权，不改变用户分组，到期自动回收',
                              )
                            : t(
                                '购买或手动新增订阅会升级到该分组；当套餐失效/过期或手动作废/删除后，将回退到升级前分组。回退不会立即生效，通常会有几分钟延迟。',
                              )
                        }
                      >
                        <Select.Option value=''>{t('不升级')}</Select.Option>
                        {(groupOptions || []).map((g) => (
                          <Select.Option key={g} value={g}>
                            {g}
                          </Select.Option>
                        ))}
                      </Form.Select>
                    </Col>

                    <Col span={12}>
                      <Form.Input
                        field='currency'
                        label={t('币种')}
                        disabled
                        extraText={t('由全站货币展示设置统一控制')}
                      />
                    </Col>

                    <Col span={12}>
                      <Form.InputNumber
                        field='sort_order'
                        label={t('排序')}
                        precision={0}
                        style={{ width: '100%' }}
                      />
                    </Col>

                    <Col span={12}>
                      <Form.InputNumber
                        field='max_purchase_per_user'
                        label={t('购买上限')}
                        min={0}
                        precision={0}
                        extraText={t('0 表示不限')}
                        style={{ width: '100%' }}
                      />
                    </Col>

                    <Col span={12}>
                      <Form.InputNumber
                        field='max_purchase_total'
                        label={t('全局购买上限')}
                        min={0}
                        precision={0}
                        extraText={t('0 表示不限；可用于控制总名额')}
                        style={{ width: '100%' }}
                      />
                    </Col>

                    <Col span={12}>
                      <Form.Select
                        field='max_purchase_reset_period'
                        label={t('全局限购刷新周期')}
                        disabled={Number(values.max_purchase_total || 0) <= 0}
                        extraText={t(
                          '可设置按固定周期补货，或按订阅到期释放名额。例如全局购买上限=1 且选择到期释放名额，即同时只允许 1 个有效订阅',
                        )}
                      >
                        {purchaseResetPeriodOptions.map((o) => (
                          <Select.Option key={o.value} value={o.value}>
                            {o.label}
                          </Select.Option>
                        ))}
                      </Form.Select>
                    </Col>

                    <Col span={12}>
                      <Form.InputNumber
                        field='max_purchase_reset_custom_seconds'
                        label={t('全局限购自定义秒数')}
                        min={0}
                        precision={0}
                        disabled={
                          Number(values.max_purchase_total || 0) <= 0 ||
                          values.max_purchase_reset_period !== 'custom'
                        }
                        extraText={t('仅在全局限购刷新周期为自定义时生效')}
                        style={{ width: '100%' }}
                      />
                    </Col>

                    <Col span={12}>
                      <Form.Switch
                        field='enabled'
                        label={t('启用状态')}
                        size='large'
                      />
                    </Col>
                  </Row>
                </Card>

                {/* 有效期设置 */}
                <Card className='!rounded-2xl shadow-sm border-0 mb-4'>
                  <div className='flex items-center mb-2'>
                    <Avatar
                      size='small'
                      color='green'
                      className='mr-2 shadow-md'
                    >
                      <Clock size={16} />
                    </Avatar>
                    <div>
                      <Text className='text-lg font-medium'>
                        {t('有效期设置')}
                      </Text>
                      <div className='text-xs text-gray-600'>
                        {t('配置套餐的有效时长')}
                      </div>
                    </div>
                  </div>

                  <Row gutter={12}>
                    <Col span={12}>
                      <Form.Select
                        field='duration_unit'
                        label={t('有效期单位')}
                        required
                        rules={[{ required: true }]}
                      >
                        {durationUnitOptions.map((o) => (
                          <Select.Option key={o.value} value={o.value}>
                            {o.label}
                          </Select.Option>
                        ))}
                      </Form.Select>
                    </Col>

                    <Col span={12}>
                      {values.duration_unit === 'custom' ? (
                        <Form.InputNumber
                          field='custom_seconds'
                          label={t('自定义秒数')}
                          required
                          min={1}
                          precision={0}
                          rules={[{ required: true, message: t('请输入秒数') }]}
                          style={{ width: '100%' }}
                        />
                      ) : (
                        <Form.InputNumber
                          field='duration_value'
                          label={t('有效期数值')}
                          required
                          min={1}
                          precision={0}
                          rules={[{ required: true, message: t('请输入数值') }]}
                          style={{ width: '100%' }}
                        />
                      )}
                    </Col>
                  </Row>
                </Card>

                {/* 额度配置 */}
                <Card className='!rounded-2xl shadow-sm border-0 mb-4'>
                  <div className='flex items-center mb-2'>
                    <Avatar
                      size='small'
                      color='orange'
                      className='mr-2 shadow-md'
                    >
                      <RefreshCw size={16} />
                    </Avatar>
                    <div>
                      <Text className='text-lg font-medium'>
                        {t('额度配置')}
                      </Text>
                      <div className='text-xs text-gray-600'>
                        {t('按周期设置使用限额，可叠加多个周期限制')}
                      </div>
                    </div>
                  </div>

                  <div className='mb-3'>
                    <div className='mb-2'>
                      <Text>{t('额度模式')}</Text>
                    </div>
                    <Select
                      value={quotaMode}
                      disabled={preserveLegacyQuota}
                      onChange={(mode) => {
                        setQuotaMode(mode);
                        if (
                          mode === 'periodic' &&
                          (!values.quota_reset_period ||
                            values.quota_reset_period === 'never')
                        ) {
                          formApiRef.current?.setValue(
                            'quota_reset_period',
                            'daily',
                          );
                        }
                      }}
                      style={{ width: '100%' }}
                      optionList={[
                        { value: 'multi', label: t('按周期限额') },
                        { value: 'unlimited', label: t('不限额度') },
                        ...(originalQuotaMode === 'total'
                          ? [
                              {
                                value: 'total',
                                label: t('历史额度包（续订叠加）'),
                              },
                            ]
                          : []),
                        ...(originalQuotaMode === 'periodic'
                          ? [{ value: 'periodic', label: t('历史单周期额度') }]
                          : []),
                      ]}
                    />
                  </div>

                  {preserveLegacyQuota && (
                    <Banner
                      type='warning'
                      closeIcon={null}
                      className='mb-3'
                      description={
                        <div className='space-y-2'>
                          <div>
                            {t(
                              '此套餐包含历史混合额度配置，所有限制同时生效。直接保存将保留原配置；重新配置将更新套餐额度规则，已有订阅仍可能保留原额度限制，请在用户订阅中核对。',
                            )}
                          </div>
                          <div>
                            {t('历史额度限制')}：
                            {editingPlan.plan.quota_reset_period === 'never' ||
                            !editingPlan.plan.quota_reset_period
                              ? t('额度包（续订叠加）')
                              : t('每周期额度')}
                            {' · '}
                            {renderQuota(editingPlan.plan.total_amount)}
                            {' · '}
                            {formatSubscriptionResetPeriod(editingPlan.plan, t)}
                          </div>
                          <Button
                            size='small'
                            onClick={() => setPreserveLegacyQuota(false)}
                          >
                            {t('重新配置额度')}
                          </Button>
                        </div>
                      }
                    />
                  )}

                  <div className='mb-3 text-xs text-gray-500'>
                    {quotaMode === 'total' &&
                      t(
                        '历史额度包：使用后不自动恢复，续订会追加一份套餐额度。',
                      )}
                    {quotaMode === 'periodic' &&
                      t(
                        '每个周期使用独立额度，周期结束后恢复，不累计为整个订阅的总额度。',
                      )}
                    {quotaMode === 'multi' &&
                      t(
                        '每行配置一个周期和限额；仅配置一行即可，多行限制同时生效，不能相加。',
                      )}
                    {quotaMode === 'unlimited' &&
                      t(
                        '订阅有效期内不限制额度，仍受套餐有效期和可用分组约束。',
                      )}
                    {quotaMode !== 'unlimited' && (
                      <div className='mt-1'>
                        {t('额度单位')}：
                        {quotaCurrency.type === 'TOKENS'
                          ? t('原生额度')
                          : quotaCurrency.symbol}
                      </div>
                    )}
                    {quotaMode === 'multi' && (
                      <div className='mt-1'>
                        {t(
                          '自然月不等于固定 30 天；固定 30 天请设置自定义周期为 2592000 秒。',
                        )}
                      </div>
                    )}
                    {quotaMode === 'periodic' && (
                      <div className='mt-1'>
                        {formatSubscriptionQuotaResetHint(
                          { key: 'base', period: values.quota_reset_period },
                          t,
                        )}
                      </div>
                    )}
                  </div>

                  {(quotaMode === 'total' || quotaMode === 'periodic') && (
                    <Form.InputNumber
                      field='total_amount'
                      keepState
                      label={
                        quotaMode === 'total'
                          ? t('历史额度包（续订叠加）')
                          : t('每周期额度')
                      }
                      required
                      min={0}
                      precision={2}
                      rules={[{ required: true, message: t('请输入额度') }]}
                      extraText={`${t('额度需大于0；不限制额度请选择“不限额度”')} · ${t('原生额度')}：${displayAmountToQuota(values.total_amount)}`}
                      style={{ width: '100%' }}
                    />
                  )}

                  {useMultiTier ? (
                    <div className='space-y-3'>
                      {quotaTiers.map((tier, index) => (
                        <div key={index} className='rounded-lg border p-3'>
                          <div className='flex items-center justify-between mb-2'>
                            <Text strong>
                              {t('额度限制')} #{index + 1}
                            </Text>
                            <Button
                              type='danger'
                              theme='borderless'
                              size='small'
                              icon={<IconDelete />}
                              disabled={preserveLegacyQuota}
                              onClick={() => removeTier(index)}
                            />
                          </div>
                          <Row gutter={8}>
                            <Col span={12}>
                              <div className='mb-1 text-xs text-gray-500'>
                                {t('周期')}
                              </div>
                              <Select
                                value={tier.period}
                                disabled={preserveLegacyQuota}
                                onChange={(v) => updateTier(index, 'period', v)}
                                style={{ width: '100%' }}
                              >
                                {tierPeriodOptions.map((o) => (
                                  <Select.Option key={o.value} value={o.value}>
                                    {t(o.label)}
                                  </Select.Option>
                                ))}
                                {tier.period === 'none' && (
                                  <Select.Option value='none'>
                                    {t('历史累计用量上限（续订不增加）')}
                                  </Select.Option>
                                )}
                              </Select>
                            </Col>
                            <Col span={12}>
                              <div className='mb-1 text-xs text-gray-500'>
                                {t('限额')}
                              </div>
                              <InputNumber
                                value={tier.limit}
                                disabled={preserveLegacyQuota}
                                min={0}
                                precision={2}
                                onChange={(value) =>
                                  updateTier(index, 'limit', Number(value || 0))
                                }
                                style={{ width: '100%' }}
                              />
                              <div className='mt-1 text-xs text-gray-500'>
                                {t('限额需大于0，不限制的周期无需添加')} ·{' '}
                                {t('原生额度')}：
                                {normalizeTierLimitForPayload(tier.limit)}
                              </div>
                            </Col>
                            {tier.period === 'custom' && (
                              <Col span={24}>
                                <div className='mb-1 text-xs text-gray-500'>
                                  {t('自定义时长')}
                                </div>
                                <SubscriptionPeriodDurationInput
                                  value={tier.duration_text || ''}
                                  disabled={preserveLegacyQuota}
                                  onChange={(value) =>
                                    updateTier(index, 'duration_text', value)
                                  }
                                />
                              </Col>
                            )}
                          </Row>
                          <div className='mt-2 text-xs text-gray-500'>
                            {formatSubscriptionQuotaResetHint(tier, t)}
                          </div>
                        </div>
                      ))}
                      <Button
                        type='primary'
                        theme='light'
                        icon={<IconPlus />}
                        onClick={addTier}
                        disabled={preserveLegacyQuota}
                        block
                      >
                        {t('添加额度限制')}
                      </Button>
                    </div>
                  ) : quotaMode === 'periodic' ? (
                    <Row gutter={12}>
                      <Col span={12}>
                        <Form.Select
                          field='quota_reset_period'
                          keepState
                          label={t('重置周期')}
                          extraText={t(
                            '重置仅恢复当前周期额度，不延长订阅有效期',
                          )}
                        >
                          {resetPeriodOptions
                            .filter((o) => o.value !== 'never')
                            .map((o) => (
                              <Select.Option key={o.value} value={o.value}>
                                {t(o.label)}
                              </Select.Option>
                            ))}
                        </Form.Select>
                      </Col>
                      <Col span={12}>
                        {values.quota_reset_period === 'custom' ? (
                          <div className='pt-3'>
                            <div className='mb-1'>{t('自定义时长')}</div>
                            <SubscriptionPeriodDurationInput
                              value={legacyResetDuration}
                              onChange={setLegacyResetDuration}
                            />
                          </div>
                        ) : null}
                      </Col>
                    </Row>
                  ) : null}

                  <div className='mt-3 flex items-center justify-between'>
                    <div>
                      <Text>{t('禁用余额扣费')}</Text>
                      <div className='text-xs text-gray-500'>
                        {t('开启后只能使用订阅额度')}
                      </div>
                    </div>
                    <Switch
                      checked={disableBalanceDeduction}
                      onChange={setDisableBalanceDeduction}
                    />
                  </div>
                </Card>

                {/* 第三方支付配置 */}
                <Card className='!rounded-2xl shadow-sm border-0 mb-4'>
                  <div className='flex items-center mb-2'>
                    <Avatar
                      size='small'
                      color='purple'
                      className='mr-2 shadow-md'
                    >
                      <IconCreditCard size={16} />
                    </Avatar>
                    <div>
                      <Text className='text-lg font-medium'>
                        {t('支付配置')}
                      </Text>
                      <div className='text-xs text-gray-600'>
                        {t('可用支付方式与 Stripe/Creem 商品ID（可选）')}
                      </div>
                    </div>
                  </div>

                  <div className='mb-4'>
                    <Text>{t('可用支付方式')}</Text>
                    <div className='text-xs text-gray-500 mb-2'>
                      {t('全选表示不限制，以后新增的支付方式也会自动开放')}
                    </div>
                    {payMethodOptions.length > 0 ? (
                      <CheckboxGroup
                        value={allowedPayMethods}
                        onChange={setAllowedPayMethods}
                      >
                        {payMethodOptions.map((option) => (
                          <Checkbox key={option.value} value={option.value}>
                            {option.label}
                          </Checkbox>
                        ))}
                      </CheckboxGroup>
                    ) : (
                      <div className='text-xs text-gray-500'>
                        {t('尚未配置任何支付方式')}
                      </div>
                    )}
                  </div>

                  <Row gutter={12}>
                    <Col span={24}>
                      <Form.Input
                        field='stripe_price_id'
                        label='Stripe PriceId'
                        placeholder='price_...'
                        showClear
                      />
                    </Col>

                    <Col span={24}>
                      <Form.Input
                        field='creem_product_id'
                        label='Creem ProductId'
                        placeholder='prod_...'
                        showClear
                      />
                    </Col>
                  </Row>
                </Card>
              </div>
            )}
          </Form>
        </Spin>
      </SideSheet>
    </>
  );
};

export default AddEditSubscriptionModal;
