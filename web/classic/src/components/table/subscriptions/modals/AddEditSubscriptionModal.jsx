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
  Button,
  Card,
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
  { value: 'daily', label: '每天' },
  { value: 'weekly', label: '每周' },
  { value: 'monthly', label: '每月' },
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
  { value: 'monthly', label: '每月' },
  { value: 'weekly', label: '每周' },
  { value: 'daily', label: '每天' },
  { value: 'hourly', label: '每小时' },
  { value: 'custom', label: '自定义(秒)' },
  { value: 'none', label: '不重置(总量)' },
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
  const [useMultiTier, setUseMultiTier] = useState(false);
  const [quotaTiers, setQuotaTiers] = useState([]);
  const [disableBalanceDeduction, setDisableBalanceDeduction] = useState(false);
  const isMobile = useIsMobile();
  const formApiRef = useRef(null);
  const isEdit = editingPlan?.plan?.id !== undefined;
  const formKey = isEdit ? `edit-${editingPlan?.plan?.id}` : 'create';

  const normalizeTierLimitForForm = useCallback((limit) => {
    return Number(quotaToDisplayAmount(limit || 0).toFixed(2));
  }, []);

  const normalizeTierLimitForPayload = useCallback((limit) => {
    return displayAmountToQuota(limit || 0);
  }, []);

  const mapQuotaTiersToForm = useCallback((tiers) => {
    if (!Array.isArray(tiers)) return [];
    return tiers.map((tier, index) => ({
      period: tier?.period || 'monthly',
      limit: normalizeTierLimitForForm(tier?.limit || 0),
      custom_seconds: Number(tier?.custom_seconds || 0),
      sort_priority: Number(tier?.sort_priority || (index + 1) * 10),
    }));
  }, [normalizeTierLimitForForm]);

  const mapQuotaTiersToPayload = useCallback((tiers) => {
    if (!Array.isArray(tiers)) return [];
    return tiers
      .map((tier, index) => ({
        period: tier?.period || 'monthly',
        limit: normalizeTierLimitForPayload(tier?.limit || 0),
        custom_seconds: Number(tier?.custom_seconds || 0),
        sort_priority: Number(tier?.sort_priority || (index + 1) * 10),
      }))
      .filter((tier) => tier.limit > 0);
  }, [normalizeTierLimitForPayload]);

  const addTier = useCallback(() => {
    setQuotaTiers(prev => [...prev, {
      period: 'monthly',
      limit: 0,
      custom_seconds: 0,
      sort_priority: (prev.length + 1) * 10,
    }]);
  }, []);

  const removeTier = useCallback((index) => {
    setQuotaTiers(prev => prev.filter((_, i) => i !== index));
  }, []);

  const updateTier = useCallback((index, field, value) => {
    setQuotaTiers(prev => prev.map((tier, i) =>
      i === index ? { ...tier, [field]: value } : tier
    ));
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
    // Initialize multi-tier state from plan
    const plan = editingPlan?.plan;
    if (plan?.quota_tiers && plan.quota_tiers !== '[]') {
      try {
        const tiers = JSON.parse(plan.quota_tiers);
        if (Array.isArray(tiers) && tiers.length > 0) {
          setUseMultiTier(true);
          setQuotaTiers(mapQuotaTiersToForm(tiers));
        } else {
          setUseMultiTier(false);
          setQuotaTiers([]);
        }
      } catch {
        setUseMultiTier(false);
        setQuotaTiers([]);
      }
    } else {
      setUseMultiTier(false);
      setQuotaTiers([]);
    }
    setDisableBalanceDeduction(plan?.disable_balance_deduction || false);
  }, [visible, editingPlan, mapQuotaTiersToForm]);

  const TIER_PERIOD_ORDER = { hourly: 1, daily: 2, weekly: 3, monthly: 4, none: 5, custom: 1 };

  const getCustomOrder = (seconds) => {
    if (seconds <= 0) return 1;
    if (seconds < 86400) return 1;
    if (seconds < 604800) return 2;
    if (seconds < 2592000) return 3;
    return 4;
  };

  const getTierOrder = (tier) => {
    if (tier.period === 'custom') return getCustomOrder(tier.custom_seconds);
    return TIER_PERIOD_ORDER[tier.period] || 1;
  };

  const validateTiers = (tiers) => {
    if (tiers.length <= 1) return null;
    const sorted = [...tiers].sort((a, b) => getTierOrder(a) - getTierOrder(b));
    for (let i = 1; i < sorted.length; i++) {
      const prev = sorted[i - 1];
      const curr = sorted[i];
      const prevOrder = getTierOrder(prev);
      const currOrder = getTierOrder(curr);
      if (currOrder < prevOrder) {
        return t('层级周期必须长于或等于上一层级');
      }
      if (curr.limit > 0 && prev.limit > 0 && curr.limit < prev.limit) {
        return t('层级限额必须大于或等于上一层级');
      }
    }
    return null;
  };

  const submit = useCallback(async (values) => {
    if (!values.title || values.title.trim() === '') {
      showError(t('套餐标题不能为空'));
      return;
    }
    // Validate tier ordering
    if (useMultiTier) {
      const activeTiers = mapQuotaTiersToPayload(quotaTiers);
      const error = validateTiers(activeTiers);
      if (error) {
        showError(error);
        return;
      }
    }
    setLoading(true);
    try {
      const effectiveTiers = useMultiTier ? mapQuotaTiersToPayload(quotaTiers) : [];
      const payload = {
        plan: {
          ...values,
          price_amount: Number(values.price_amount || 0),
          currency: 'USD',
          duration_value: Number(values.duration_value || 0),
          custom_seconds: Number(values.custom_seconds || 0),
          quota_reset_period: useMultiTier ? 'never' : (values.quota_reset_period || 'never'),
          quota_reset_custom_seconds:
            !useMultiTier && values.quota_reset_period === 'custom'
              ? Number(values.quota_reset_custom_seconds || 0)
              : 0,
          sort_order: Number(values.sort_order || 0),
          max_purchase_per_user: Number(values.max_purchase_per_user || 0),
          max_purchase_total: Number(values.max_purchase_total || 0),
          max_purchase_reset_period:
            Number(values.max_purchase_total || 0) > 0
              ? (values.max_purchase_reset_period || 'never')
              : 'never',
          max_purchase_reset_custom_seconds:
            Number(values.max_purchase_total || 0) > 0 &&
            values.max_purchase_reset_period === 'custom'
              ? Number(values.max_purchase_reset_custom_seconds || 0)
              : 0,
          total_amount: useMultiTier ? 0 : displayAmountToQuota(values.total_amount),
          upgrade_group: values.upgrade_group || '',
          group_mode: values.group_mode === 'attach' ? 'attach' : '',
          quota_tiers: effectiveTiers.length > 0 ? JSON.stringify(effectiveTiers) : '[]',
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
  }, [disableBalanceDeduction, editingPlan, handleClose, mapQuotaTiersToPayload, quotaTiers, refresh, t, useMultiTier]);

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
                      <Form.InputNumber
                        field='total_amount'
                        label={t('总额度')}
                        required
                        min={0}
                        precision={2}
                        rules={[{ required: true, message: t('请输入总额度') }]}
                        extraText={`${t('0 表示不限')} · ${t('原生额度')}：${displayAmountToQuota(
                          values.total_amount,
                        )}`}
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

                {/* 额度重置 */}
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
                        {t('额度重置')}
                      </Text>
                      <div className='text-xs text-gray-600'>
                        {t('支持周期性重置套餐权益额度')}
                      </div>
                    </div>
                  </div>

                  <div className='mb-3 flex items-center justify-between'>
                    <Text>{t('多周期限额')}</Text>
                    <Switch
                      checked={useMultiTier}
                      onChange={setUseMultiTier}
                    />
                  </div>

                  {useMultiTier ? (
                    <div className='space-y-3'>
                      {quotaTiers.map((tier, index) => (
                        <div key={index} className='rounded-lg border p-3'>
                          <div className='flex items-center justify-between mb-2'>
                            <Text strong>{t('层级')} #{index + 1}</Text>
                            <Button
                              type='danger'
                              theme='borderless'
                              size='small'
                              icon={<IconDelete />}
                              onClick={() => removeTier(index)}
                            />
                          </div>
                          <Row gutter={8}>
                            <Col span={8}>
                              <div className='mb-1 text-xs text-gray-500'>{t('周期')}</div>
                              <Select
                                value={tier.period}
                                onChange={(v) => updateTier(index, 'period', v)}
                                style={{ width: '100%' }}
                              >
                                {tierPeriodOptions.map(o => (
                                  <Select.Option key={o.value} value={o.value}>{o.label}</Select.Option>
                                ))}
                              </Select>
                            </Col>
                            <Col span={8}>
                              <div className='mb-1 text-xs text-gray-500'>{t('限额')}</div>
                              <InputNumber
                                value={tier.limit}
                                min={0}
                                precision={2}
                                onChange={(value) => updateTier(index, 'limit', Number(value || 0))}
                                style={{ width: '100%' }}
                              />
                              <div className='mt-1 text-xs text-gray-500'>
                                {t('0 表示不限')} · {t('原生额度')}：{normalizeTierLimitForPayload(
                                  tier.limit,
                                )}
                              </div>
                            </Col>
                            {tier.period === 'custom' && (
                              <Col span={8}>
                                <div className='mb-1 text-xs text-gray-500'>{t('秒数')}</div>
                                <input
                                  type='number'
                                  className='semi-input'
                                  value={tier.custom_seconds}
                                  min={1}
                                  onChange={(e) => updateTier(index, 'custom_seconds', parseInt(e.target.value, 10) || 0)}
                                  style={{ width: '100%' }}
                                />
                              </Col>
                            )}
                          </Row>
                        </div>
                      ))}
                      <Button
                        type='primary'
                        theme='light'
                        icon={<IconPlus />}
                        onClick={addTier}
                        block
                      >
                        {t('添加层级')}
                      </Button>
                    </div>
                  ) : (
                    <Row gutter={12}>
                      <Col span={12}>
                        <Form.Select
                          field='quota_reset_period'
                          label={t('重置周期')}
                        >
                          {resetPeriodOptions.map((o) => (
                            <Select.Option key={o.value} value={o.value}>
                              {o.label}
                            </Select.Option>
                          ))}
                        </Form.Select>
                      </Col>
                      <Col span={12}>
                        {values.quota_reset_period === 'custom' ? (
                          <Form.InputNumber
                            field='quota_reset_custom_seconds'
                            label={t('自定义秒数')}
                            required
                            min={60}
                            precision={0}
                            rules={[{ required: true, message: t('请输入秒数') }]}
                            style={{ width: '100%' }}
                          />
                        ) : (
                          <Form.InputNumber
                            field='quota_reset_custom_seconds'
                            label={t('自定义秒数')}
                            min={0}
                            precision={0}
                            style={{ width: '100%' }}
                            disabled
                          />
                        )}
                      </Col>
                    </Row>
                  )}

                  <div className='mt-3 flex items-center justify-between'>
                    <div>
                      <Text>{t('禁用余额扣费')}</Text>
                      <div className='text-xs text-gray-500'>{t('开启后只能使用订阅额度')}</div>
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
                        {t('第三方支付配置')}
                      </Text>
                      <div className='text-xs text-gray-600'>
                        {t('Stripe/Creem 商品ID（可选）')}
                      </div>
                    </div>
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
