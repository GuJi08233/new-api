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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Checkbox,
  Empty,
  Input,
  Modal,
  Radio,
  RadioGroup,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IconDelete,
  IconPlus,
  IconSave,
  IconSearch,
} from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import {
  PAGE_SIZE,
  PRICE_SUFFIX,
  buildSummaryText,
  hasValue,
  parseOptionJSON,
  useModelPricingEditorState,
} from '../hooks/useModelPricingEditorState';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';
import TieredPricingEditor from './TieredPricingEditor';

const { Text } = Typography;
const EMPTY_CANDIDATE_MODEL_NAMES = [];

const formatUpstreamPrice = (value) => {
  const number = Number(value);
  if (!Number.isFinite(number)) return '-';
  return String(parseFloat(number.toFixed(6)));
};

const PriceInput = ({
  label,
  value,
  placeholder,
  onChange,
  suffix = PRICE_SUFFIX,
  disabled = false,
  extraText = '',
  headerAction = null,
  hidden = false,
}) => (
  <div style={{ marginBottom: 16 }}>
    <div className='mb-1 font-medium text-gray-700 flex items-center justify-between gap-3'>
      <span>{label}</span>
      {headerAction}
    </div>
    {!hidden ? (
      <Input
        value={value}
        placeholder={placeholder}
        onChange={onChange}
        suffix={suffix}
        disabled={disabled}
      />
    ) : null}
    {extraText ? (
      <div className='mt-1 text-xs text-gray-500'>{extraText}</div>
    ) : null}
  </div>
);

export default function ModelPricingEditor({
  options,
  refresh,
  candidateModelNames = EMPTY_CANDIDATE_MODEL_NAMES,
  filterMode = 'all',
  allowAddModel = true,
  allowDeleteModel = true,
  showConflictFilter = true,
  listDescription = '',
  emptyTitle = '',
  emptyDescription = '',
  onSelectedGroupChange,
}) {
  const { t } = useTranslation();
  const isMobile = useIsMobile();
  const [addVisible, setAddVisible] = useState(false);
  const [batchVisible, setBatchVisible] = useState(false);
  const [syncVisible, setSyncVisible] = useState(false);
  const [newModelName, setNewModelName] = useState('');
  const [pickedUpstreamProvider, setPickedUpstreamProvider] = useState('');
  const [selectedGroup, setSelectedGroupState] = useState('global');
  const setSelectedGroup = useCallback(
    (next) => {
      setSelectedGroupState(next);
      onSelectedGroupChange?.(next);
    },
    [onSelectedGroupChange],
  );
  const [syncTargetGroups, setSyncTargetGroups] = useState([]);
  const [syncMode, setSyncMode] = useState('selected'); // 'selected' | 'all'

  // 从 options 中获取可用的分组列表
  const availableGroups = useMemo(() => {
    const groupRatio = parseOptionJSON(options.GroupRatio);
    return ['global', ...Object.keys(groupRatio)];
  }, [options.GroupRatio]);

  const {
    selectedModel,
    selectedModelName,
    selectedModelNames,
    setSelectedModelName,
    setSelectedModelNames,
    searchText,
    setSearchText,
    currentPage,
    setCurrentPage,
    loading,
    conflictOnly,
    setConflictOnly,
    filteredModels,
    pagedData,
    selectedWarnings,
    previewRows,
    isOptionalFieldEnabled,
    handleOptionalFieldToggle,
    handleNumericFieldChange,
    handleBillingModeChange,
    handleBillingExprChange,
    handleRequestRuleExprChange,
    handleSubmit,
    addModel,
    deleteModel,
    applySelectedModelPricing,
    syncGroupPricing,
    clearGroupPricing,
    fillPricingFromUpstream,
    upstreamCandidates,
    upstreamLoading,
    loadUpstreamCandidates,
    applyUpstreamCandidate,
    pricingUpstream,
    changePricingUpstream,
  } = useModelPricingEditorState({
    options,
    refresh,
    t,
    candidateModelNames,
    filterMode,
    selectedGroup,
  });

  // 去上游查价用的模型名，默认跟当前模型一致；改成别的名字就是借用那个模型的价格
  const [upstreamLookupName, setUpstreamLookupName] = useState('');

  // 切换模型时清掉上一个模型选中的上游提供商和查询名
  useEffect(() => {
    setPickedUpstreamProvider('');
    setUpstreamLookupName(selectedModelName || '');
  }, [selectedModelName]);

  const lookupName = upstreamLookupName.trim() || selectedModelName;
  const lookupCandidates = upstreamCandidates[lookupName] || [];
  const isBorrowedPricing = Boolean(
    lookupName && selectedModelName && lookupName !== selectedModelName,
  );

  const pickedUpstreamCandidate = useMemo(() => {
    if (!lookupName || !pickedUpstreamProvider) return null;
    return (
      (upstreamCandidates[lookupName] || []).find(
        (candidate) => candidate.provider === pickedUpstreamProvider,
      ) || null
    );
  }, [lookupName, pickedUpstreamProvider, upstreamCandidates]);

  const getExprModeLabel = useCallback((model) => {
    if (model?.billingMode !== 'tiered_expr') {
      return '';
    }
    return (model.billingExpr || '').includes('tier(')
      ? t('阶梯计费')
      : t('表达式计费');
  }, [t]);

  const columns = useMemo(
    () => [
      {
        title: t('模型名称'),
        dataIndex: 'name',
        key: 'name',
        render: (text, record) => (
          <Space>
            <Button
              theme='borderless'
              type='tertiary'
              onClick={() => setSelectedModelName(record.name)}
              style={{
                padding: 0,
                color:
                  record.name === selectedModelName
                    ? 'var(--semi-color-primary)'
                    : undefined,
              }}
            >
              {text}
            </Button>
            {selectedModelNames.includes(record.name) ? (
              <Tag color='green' shape='circle'>
                {t('已勾选')}
              </Tag>
            ) : null}
            {record.hasConflict ? (
              <Tag color='red' shape='circle'>
                {t('矛盾')}
              </Tag>
            ) : null}
          </Space>
        ),
      },
      {
        title: t('计费方式'),
        dataIndex: 'billingMode',
        key: 'billingMode',
        render: (_, record) => (
          <Tag
            color={
              record.billingMode === 'per-request'
                ? 'teal'
                : record.billingMode === 'tiered_expr'
                  ? 'amber'
                  : 'violet'
            }
          >
            {record.billingMode === 'per-request'
              ? t('按次计费')
              : record.billingMode === 'tiered_expr'
                ? getExprModeLabel(record)
                : t('按量计费')}
          </Tag>
        ),
      },
      {
        title: t('价格摘要'),
        dataIndex: 'summary',
        key: 'summary',
        render: (_, record) => buildSummaryText(record, t),
      },
      {
        title: t('操作'),
        key: 'action',
        render: (_, record) => (
          <Space>
            {allowDeleteModel ? (
              <Button
                size='small'
                type='danger'
                icon={<IconDelete />}
                onClick={() => deleteModel(record.name)}
              />
            ) : null}
          </Space>
        ),
      },
    ],
    [
      allowDeleteModel,
      deleteModel,
      getExprModeLabel,
      selectedModelName,
      selectedModelNames,
      setSelectedModelName,
      t,
    ],
  );

  const handleAddModel = () => {
    if (addModel(newModelName)) {
      setNewModelName('');
      setAddVisible(false);
    }
  };

  const rowSelection = {
    selectedRowKeys: selectedModelNames,
    onChange: (selectedRowKeys) => setSelectedModelNames(selectedRowKeys),
  };

  return (
    <>
      <Space vertical align='start' style={{ width: '100%' }}>
        {/* 分组选择器 */}
        <Space wrap className='mt-2'>
          <Select
            value={selectedGroup}
            onChange={setSelectedGroup}
            style={{ width: 200 }}
            placeholder={t('选择分组')}
          >
            <Select.Option value='global'>{t('全局配置')}</Select.Option>
            {availableGroups.filter(g => g !== 'global').map(group => (
              <Select.Option key={group} value={group}>
                {group} {t('分组')}
              </Select.Option>
            ))}
          </Select>
          {/* 同步按钮 */}
          {selectedGroup === 'global' ? (
            <Button
              disabled={selectedModelNames.length === 0}
              onClick={() => {
                setSyncMode('to_groups');
                setSyncVisible(true);
              }}
              style={isMobile ? { width: '100%' } : undefined}
            >
              {t('同步所选模型到分组')}
              {selectedModelNames.length > 0 ? ` (${selectedModelNames.length})` : ''}
            </Button>
          ) : (
            <>
              <Button
                onClick={() => {
                  setSyncMode('from_global');
                  setSyncVisible(true);
                }}
                style={isMobile ? { width: '100%' } : undefined}
              >
                {t('从全局同步到当前分组')}
              </Button>
              <Button
                disabled={selectedModelNames.length === 0}
                onClick={() => {
                  setSyncMode('to_groups');
                  setSyncVisible(true);
                }}
                style={isMobile ? { width: '100%' } : undefined}
              >
                {t('同步所选模型到分组')}
                {selectedModelNames.length > 0 ? ` (${selectedModelNames.length})` : ''}
              </Button>
              <Button
                type='danger'
                onClick={() =>
                  Modal.confirm({
                    title: t('清空分组配置'),
                    content: t(
                      '将删除分组 {{group}} 的全部模型定价配置，之后该分组按全局定价计费。此操作不可撤销。',
                      { group: selectedGroup },
                    ),
                    okText: t('清空'),
                    cancelText: t('取消'),
                    okButtonProps: { type: 'danger' },
                    onOk: clearGroupPricing,
                  })
                }
                style={isMobile ? { width: '100%' } : undefined}
              >
                {t('清空当前分组配置')}
              </Button>
            </>
          )}
        </Space>

        <Space wrap className='mt-2'>
          {allowAddModel ? (
            <Button
              icon={<IconPlus />}
              onClick={() => setAddVisible(true)}
              style={isMobile ? { width: '100%' } : undefined}
            >
              {t('添加模型')}
            </Button>
          ) : null}
          <Button
            type='primary'
            icon={<IconSave />}
            loading={loading}
            onClick={handleSubmit}
            style={isMobile ? { width: '100%' } : undefined}
          >
            {selectedGroup === 'global' ? t('应用更改') : t('保存分组配置')}
          </Button>
          <Button
            disabled={!selectedModel || selectedModelNames.length === 0}
            onClick={() => setBatchVisible(true)}
            style={isMobile ? { width: '100%' } : undefined}
          >
            {t('批量应用当前模型价格')}
            {selectedModelNames.length > 0 ? ` (${selectedModelNames.length})` : ''}
          </Button>
          <Tooltip
            content={t(
              '官方精选只收录厂商自营入口，并支持 1 小时缓存写、思考模式、错峰计费；全量覆盖长尾模型，但可能只有转售商报价',
            )}
          >
            <Select
              value={pricingUpstream}
              onChange={changePricingUpstream}
              style={isMobile ? { width: '100%' } : { width: 200 }}
              optionList={[
                {
                  value: 'llm-metadata',
                  label: t('官方精选（llm-metadata）'),
                },
                { value: 'models.dev', label: t('全量（models.dev）') },
              ]}
            />
          </Tooltip>
          <Tooltip
            content={t(
              '勾选模型后只同步所选模型，未勾选时同步当前列表的全部模型；价格填入表单后仍需保存才会生效',
            )}
          >
            <Button
              loading={loading}
              onClick={() => fillPricingFromUpstream(selectedModelNames)}
              style={isMobile ? { width: '100%' } : undefined}
            >
              {t('从 {{upstream}} 同步价格', { upstream: pricingUpstream })}
              {selectedModelNames.length > 0 ? ` (${selectedModelNames.length})` : ''}
            </Button>
          </Tooltip>
          <Input
            prefix={<IconSearch />}
            placeholder={t('搜索模型名称')}
            value={searchText}
            onChange={(value) => setSearchText(value)}
            style={{ width: isMobile ? '100%' : 220 }}
            showClear
          />
          {showConflictFilter ? (
            <Checkbox
              checked={conflictOnly}
              onChange={(event) => setConflictOnly(event.target.checked)}
            >
              {t('仅显示矛盾倍率')}
            </Checkbox>
          ) : null}
        </Space>

        {listDescription ? (
          <div className='text-sm text-gray-500'>{listDescription}</div>
        ) : null}
        {selectedModelNames.length > 0 ? (
          <div
            style={{
              width: '100%',
              padding: '10px 12px',
              borderRadius: 8,
              background: 'var(--semi-color-primary-light-default)',
              border: '1px solid var(--semi-color-primary)',
              color: 'var(--semi-color-primary)',
              fontWeight: 600,
            }}
          >
            {t('已勾选 {{count}} 个模型', { count: selectedModelNames.length })}
          </div>
        ) : null}

        <div
          style={{
            width: '100%',
            display: 'grid',
            gap: 16,
            gridTemplateColumns: isMobile
              ? 'minmax(0, 1fr)'
              : 'minmax(300px, 0.8fr) minmax(480px, 1.2fr)',
          }}
        >
          <Card
            bodyStyle={{ padding: 0 }}
            style={isMobile ? { order: 2 } : undefined}
          >
            <div style={{ overflowX: 'auto' }}>
              <Table
                columns={columns}
                dataSource={pagedData}
                rowKey='name'
                rowSelection={rowSelection}
                pagination={{
                  currentPage,
                  pageSize: PAGE_SIZE,
                  total: filteredModels.length,
                  onPageChange: (page) => setCurrentPage(page),
                  showTotal: true,
                  showSizeChanger: false,
                }}
                empty={
                  <div style={{ textAlign: 'center', padding: '20px' }}>
                    {emptyTitle || t('暂无模型')}
                  </div>
                }
                onRow={(record) => ({
                  style: {
                    background: selectedModelNames.includes(record.name)
                      ? 'var(--semi-color-success-light-default)'
                      : record.name === selectedModelName
                        ? 'var(--semi-color-primary-light-default)'
                        : undefined,
                    boxShadow: selectedModelNames.includes(record.name)
                      ? 'inset 4px 0 0 var(--semi-color-success)'
                      : record.name === selectedModelName
                        ? 'inset 4px 0 0 var(--semi-color-primary)'
                        : undefined,
                    transition: 'background 0.2s ease, box-shadow 0.2s ease',
                  },
                  onClick: () => setSelectedModelName(record.name),
                })}
                scroll={isMobile ? { x: 720 } : undefined}
              />
            </div>
          </Card>

          <Card
            style={isMobile ? { order: 1 } : undefined}
            title={selectedModel ? selectedModel.name : t('模型计费编辑器')}
            headerExtraContent={
              selectedModel ? (
                <Tag
                  color={
                    selectedModel.billingMode === 'per-request'
                      ? 'teal'
                      : selectedModel.billingMode === 'tiered_expr'
                        ? 'amber'
                        : 'blue'
                  }
                >
                  {selectedModel.billingMode === 'per-request'
                    ? t('按次计费')
                    : selectedModel.billingMode === 'tiered_expr'
                      ? getExprModeLabel(selectedModel)
                      : t('按量计费')}
                </Tag>
              ) : null
            }
          >
            {!selectedModel ? (
              <Empty
                title={emptyTitle || t('暂无模型')}
                description={
                  emptyDescription || t('请先新增模型或从左侧列表选择一个模型')
                }
              />
            ) : (
              <div>
                <div className='mb-4'>
                  <div className='mb-2 font-medium text-gray-700'>
                    {t('计费方式')}
                  </div>
                  <RadioGroup
                    type='button'
                    value={selectedModel.billingMode}
                    onChange={(event) => handleBillingModeChange(event.target.value)}
                  >
                    <Radio value='per-token'>{t('按量计费')}</Radio>
                    <Radio value='per-request'>{t('按次计费')}</Radio>
                    <Radio value='tiered_expr'>{t('表达式/阶梯计费')}</Radio>
                  </RadioGroup>
                  <div className='mt-2 text-xs text-gray-500'>
                    {t(
                      '普通按量/按次直接填价格就行；如果价格要跟请求参数或请求头联动，请切到表达式/阶梯计费。',
                    )}
                  </div>
                </div>

                {/* 选中来源会自动把该模型切到对应的计费模式，所以任何模式下都能用 */}
                <Card
                  bodyStyle={{ padding: 12 }}
                  style={{
                    marginBottom: 16,
                    background: 'var(--semi-color-fill-0)',
                  }}
                >
                  <div className='flex items-center justify-between mb-2'>
                    <span className='font-medium'>
                      {t('{{upstream}} 参考价格', {
                        upstream: pricingUpstream,
                      })}
                    </span>
                  </div>
                  <div className='flex items-center gap-2 mb-2'>
                    <Input
                      size='small'
                      value={upstreamLookupName}
                      onChange={(value) => {
                        setUpstreamLookupName(value);
                        setPickedUpstreamProvider('');
                      }}
                      placeholder={selectedModel.name}
                      style={{ flex: 1 }}
                    />
                    <Button
                      size='small'
                      loading={upstreamLoading}
                      disabled={!lookupName}
                      onClick={() => loadUpstreamCandidates(lookupName)}
                    >
                      {upstreamCandidates[lookupName]
                        ? t('重新获取')
                        : t('获取')}
                    </Button>
                  </div>
                  <div className='mb-2 text-xs text-gray-500'>
                    {isBorrowedPricing
                      ? t('将把 {{source}} 的价格填给 {{target}}', {
                          source: lookupName,
                          target: selectedModel.name,
                        })
                      : t('可改成别的模型名，借用那个模型的价格（适合重定向的模型）')}
                  </div>
                  {lookupCandidates.length > 0 ? (
                    <>
                      <Select
                        key={lookupName}
                        style={{ width: '100%' }}
                        value={pickedUpstreamProvider || undefined}
                        placeholder={t('选择提供商，价格将填入下方表单')}
                        onChange={(value) => {
                          setPickedUpstreamProvider(value);
                          applyUpstreamCandidate(selectedModel.name, value, {
                            lookupName,
                          });
                        }}
                        optionList={lookupCandidates.map((candidate) => ({
                          value: candidate.provider,
                          label: `${candidate.provider} · ${
                            candidate.official ? t('官方') : t('第三方')
                          } · ${t('输入')} $${formatUpstreamPrice(
                            candidate.model_ratio * 2,
                          )}${
                            candidate.completion_ratio !== undefined &&
                            candidate.completion_ratio !== null
                              ? ` · ${t('输出')} $${formatUpstreamPrice(
                                  candidate.model_ratio *
                                    2 *
                                    candidate.completion_ratio,
                                )}`
                              : ''
                          } /1M${candidate.billing_expr ? ` · ${t('有阶梯价')}` : ''}`,
                        }))}
                      />
                      <div className='mt-1 text-xs text-gray-500'>
                        {t(
                          '选择后会把该模型切到按量计费并填入价格，仍需点击保存才会生效；第三方来源的报价请自行核对',
                        )}
                      </div>
                      {pickedUpstreamCandidate?.billing_expr ? (
                        <div className='mt-2'>
                          <Button
                            size='small'
                            onClick={() =>
                              applyUpstreamCandidate(
                                selectedModel.name,
                                pickedUpstreamProvider,
                                { lookupName, useTiered: true },
                              )
                            }
                          >
                            {t('改用该来源的上下文阶梯价')}
                          </Button>
                          <div className='mt-1 text-xs text-gray-500'>
                            {t(
                              '该来源按上下文长度分档计价，套用后模型会切换为表达式计费',
                            )}
                          </div>
                        </div>
                      ) : null}
                    </>
                  ) : (
                    <div className='text-xs text-gray-500'>
                      {t(
                        '点击获取，查看上面这个模型在 {{upstream}} 上各家提供商的报价并直接填入',
                        { upstream: pricingUpstream },
                      )}
                    </div>
                  )}
                </Card>

                {selectedWarnings.length > 0 ? (
                  <Card
                    bodyStyle={{ padding: 12 }}
                    style={{
                      marginBottom: 16,
                      background: 'var(--semi-color-warning-light-default)',
                    }}
                  >
                    <div className='font-medium mb-2'>{t('当前提示')}</div>
                    {selectedWarnings.map((warning) => (
                      <div key={warning} className='text-sm text-gray-700 mb-1'>
                        {warning}
                      </div>
                    ))}
                  </Card>
                ) : null}

                {selectedModel.billingMode === 'per-request' ? (
                  <PriceInput
                    label={t('固定价格')}
                    value={selectedModel.fixedPrice}
                    placeholder={t('输入每次调用价格')}
                    suffix={t('$/次')}
                    onChange={(value) => handleNumericFieldChange('fixedPrice', value)}
                    extraText={t('适合 MJ / 任务类等按次收费模型。')}
                  />
                ) : selectedModel.billingMode === 'tiered_expr' ? (
                  <TieredPricingEditor
                    model={selectedModel}
                    onExprChange={handleBillingExprChange}
                    requestRuleExpr={selectedModel.requestRuleExpr}
                    onRequestRuleExprChange={handleRequestRuleExprChange}
                    t={t}
                  />
                ) : (
                  <>
                    <Card
                      bodyStyle={{ padding: 16 }}
                      style={{
                        marginBottom: 16,
                        background: 'var(--semi-color-fill-0)',
                      }}
                    >
                      <div className='font-medium mb-3'>{t('基础价格')}</div>
                      <PriceInput
                        label={t('输入价格')}
                        value={selectedModel.inputPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) => handleNumericFieldChange('inputPrice', value)}
                      />
                      {selectedModel.completionRatioLocked ? (
                        <Banner
                          type='warning'
                          bordered
                          fullMode={false}
                          closeIcon={null}
                          style={{ marginBottom: 12 }}
                          title={t('补全价格已锁定')}
                          description={t(
                            '该模型补全倍率由后端固定为 {{ratio}}。补全价格不能在这里修改。',
                            {
                              ratio: selectedModel.lockedCompletionRatio || '-',
                            },
                          )}
                        />
                      ) : null}
                      <PriceInput
                        label={t('补全价格')}
                        value={selectedModel.completionPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('completionPrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'completionPrice',
                            )}
                            disabled={selectedModel.completionRatioLocked}
                            onChange={(checked) =>
                              handleOptionalFieldToggle('completionPrice', checked)
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(selectedModel, 'completionPrice')
                        }
                        disabled={
                          !hasValue(selectedModel.inputPrice) ||
                          selectedModel.completionRatioLocked
                        }
                        extraText={
                          selectedModel.completionRatioLocked
                            ? t(
                                '后端固定倍率：{{ratio}}。该字段仅展示换算后的价格。',
                                {
                                  ratio: selectedModel.lockedCompletionRatio || '-',
                                },
                              )
                            : !isOptionalFieldEnabled(
                                  selectedModel,
                                  'completionPrice',
                                )
                              ? t('当前未启用，需要时再打开即可。')
                              : ''
                        }
                      />
                      <PriceInput
                        label={t('缓存读取价格')}
                        value={selectedModel.cachePrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) => handleNumericFieldChange('cachePrice', value)}
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(selectedModel, 'cachePrice')}
                            onChange={(checked) =>
                              handleOptionalFieldToggle('cachePrice', checked)
                            }
                          />
                        }
                        hidden={!isOptionalFieldEnabled(selectedModel, 'cachePrice')}
                        disabled={!hasValue(selectedModel.inputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(selectedModel, 'cachePrice')
                            ? t('当前未启用，需要时再打开即可。')
                            : ''
                        }
                      />
                      <PriceInput
                        label={t('缓存创建价格')}
                        value={selectedModel.createCachePrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('createCachePrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'createCachePrice',
                            )}
                            onChange={(checked) =>
                              handleOptionalFieldToggle('createCachePrice', checked)
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(selectedModel, 'createCachePrice')
                        }
                        disabled={!hasValue(selectedModel.inputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'createCachePrice',
                          )
                            ? t('当前未启用，需要时再打开即可。')
                            : ''
                        }
                      />
                    </Card>

                    <Card
                      bodyStyle={{ padding: 16 }}
                      style={{
                        marginBottom: 16,
                        background: 'var(--semi-color-fill-0)',
                      }}
                    >
                      <div className='mb-3'>
                        <div className='font-medium'>{t('扩展价格')}</div>
                        <div className='text-xs text-gray-500 mt-1'>
                          {t('这些价格都是可选项，不填也可以。')}
                        </div>
                      </div>
                      <PriceInput
                        label={t('图片输入价格')}
                        value={selectedModel.imagePrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) => handleNumericFieldChange('imagePrice', value)}
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(selectedModel, 'imagePrice')}
                            onChange={(checked) =>
                              handleOptionalFieldToggle('imagePrice', checked)
                            }
                          />
                        }
                        hidden={!isOptionalFieldEnabled(selectedModel, 'imagePrice')}
                        disabled={!hasValue(selectedModel.inputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(selectedModel, 'imagePrice')
                            ? t('当前未启用，需要时再打开即可。')
                            : ''
                        }
                      />
                      <PriceInput
                        label={t('音频输入价格')}
                        value={selectedModel.audioInputPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('audioInputPrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'audioInputPrice',
                            )}
                            onChange={(checked) =>
                              handleOptionalFieldToggle('audioInputPrice', checked)
                            }
                          />
                        }
                        hidden={!isOptionalFieldEnabled(selectedModel, 'audioInputPrice')}
                        disabled={!hasValue(selectedModel.inputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'audioInputPrice',
                          )
                            ? t('当前未启用，需要时再打开即可。')
                            : ''
                        }
                      />
                      <PriceInput
                        label={t('音频补全价格')}
                        value={selectedModel.audioOutputPrice}
                        placeholder={t('输入 $/1M tokens')}
                        onChange={(value) =>
                          handleNumericFieldChange('audioOutputPrice', value)
                        }
                        headerAction={
                          <Switch
                            size='small'
                            checked={isOptionalFieldEnabled(
                              selectedModel,
                              'audioOutputPrice',
                            )}
                            disabled={!isOptionalFieldEnabled(
                              selectedModel,
                              'audioInputPrice',
                            )}
                            onChange={(checked) =>
                              handleOptionalFieldToggle('audioOutputPrice', checked)
                            }
                          />
                        }
                        hidden={
                          !isOptionalFieldEnabled(selectedModel, 'audioOutputPrice')
                        }
                        disabled={!hasValue(selectedModel.audioInputPrice)}
                        extraText={
                          !isOptionalFieldEnabled(
                            selectedModel,
                            'audioInputPrice',
                          )
                            ? t('请先开启并填写音频输入价格。')
                            : !isOptionalFieldEnabled(
                                  selectedModel,
                                  'audioOutputPrice',
                                )
                              ? t('当前未启用，需要时再打开即可。')
                              : ''
                        }
                      />
                    </Card>
                  </>
                )}

                <Card
                  bodyStyle={{ padding: 16 }}
                  style={{ background: 'var(--semi-color-fill-0)' }}
                >
                  <div className='font-medium mb-3'>{t('保存预览')}</div>
                  <div className='text-xs text-gray-500 mb-3'>
                    {t(
                      '下面展示这个模型保存后会写入哪些后端字段，便于和原始 JSON 编辑框保持一致。',
                    )}
                  </div>
                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'minmax(140px, 180px) 1fr',
                      gap: 8,
                    }}
                  >
                    {previewRows.map((row) => (
                      <React.Fragment key={row.key}>
                        <Text strong>{row.label}</Text>
                        <Text>{row.value}</Text>
                      </React.Fragment>
                    ))}
                  </div>
                </Card>
              </div>
            )}
          </Card>
        </div>
      </Space>

      {allowAddModel ? (
        <Modal
          title={t('添加模型')}
          visible={addVisible}
          onCancel={() => {
            setAddVisible(false);
            setNewModelName('');
          }}
          onOk={handleAddModel}
        >
          <Input
            value={newModelName}
            placeholder={t('输入模型名称，例如 gpt-4.1')}
            onChange={(value) => setNewModelName(value)}
          />
        </Modal>
      ) : null}

      <Modal
        title={t('批量应用当前模型价格')}
        visible={batchVisible}
        onCancel={() => setBatchVisible(false)}
        onOk={() => {
          if (applySelectedModelPricing()) {
            setBatchVisible(false);
          }
        }}
      >
        <div className='text-sm text-gray-600'>
          {selectedModel
            ? t(
                '将把当前编辑中的模型 {{name}} 的价格配置，批量应用到已勾选的 {{count}} 个模型。',
                {
                  name: selectedModel.name,
                  count: selectedModelNames.length,
                },
              )
            : t('请先选择一个作为模板的模型')}
        </div>
        {selectedModel ? (
          <div className='text-xs text-gray-500 mt-3'>
            {t(
              '适合同系列模型一起定价，例如把 gpt-5.1 的价格批量同步到 gpt-5.1-high、gpt-5.1-low 等模型。',
            )}
          </div>
        ) : null}
      </Modal>

      {/* 同步分组配置 Modal */}
      <Modal
        title={
          syncMode === 'from_global' ? t('从全局同步到当前分组') :
          t('同步所选模型到分组')
        }
        visible={syncVisible}
        onCancel={() => {
          setSyncVisible(false);
          setSyncTargetGroups([]);
        }}
        onOk={async () => {
          let success;
          if (syncMode === 'from_global') {
            // 从全局同步到当前分组
            success = await syncGroupPricing([selectedGroup], null, true);
          } else {
            // 从当前分组同步到其他分组
            success = await syncGroupPricing(syncTargetGroups, selectedModelNames, false);
          }
          if (success) {
            setSyncVisible(false);
            setSyncTargetGroups([]);
          }
        }}
        okButtonProps={{
          disabled: syncMode !== 'from_global' && syncTargetGroups.length === 0
        }}
      >
        {syncMode === 'from_global' ? (
          <>
            <div className='text-sm text-gray-600 mb-3'>
              {t('将全局配置同步到当前分组 {{group}}', { group: selectedGroup })}
            </div>
            <div className='text-xs text-gray-500 mb-3'>
              {t('同步后，当前分组将继承全局的所有配置，之后可以修改特定模型的价格')}
            </div>
            <div className='text-xs text-orange-500 mb-3'>
              {t('注意：这将覆盖当前分组的现有配置')}
            </div>
          </>
        ) : (
          <>
            <div className='text-sm text-gray-600 mb-3'>
              {selectedGroup === 'global'
                ? t('将全局配置中已勾选的 {{count}} 个模型同步到分组', {
                    count: selectedModelNames.length,
                  })
                : t('将 {{group}} 中已勾选的 {{count}} 个模型同步到其他分组', {
                    group: selectedGroup,
                    count: selectedModelNames.length,
                  })
              }
            </div>
            <div className='text-xs text-gray-500 mb-3'>
              {t('已选模型')}: {selectedModelNames.join(', ')}
            </div>
            <Select
              multiple
              placeholder={t('选择目标分组')}
              value={syncTargetGroups}
              onChange={setSyncTargetGroups}
              style={{ width: '100%' }}
            >
              {availableGroups
                .filter(g => g !== 'global' && g !== selectedGroup)
                .map(group => (
                  <Select.Option key={group} value={group}>
                    {group} {t('分组')}
                  </Select.Option>
                ))}
            </Select>
          </>
        )}
      </Modal>
    </>
  );
}
