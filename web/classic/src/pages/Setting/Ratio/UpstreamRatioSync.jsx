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

import React, { useState, useCallback, useMemo, useEffect } from 'react';
import {
  Button,
  Table,
  Tag,
  Empty,
  Checkbox,
  Form,
  Input,
  Tooltip,
  Select,
  Modal,
  Spin,
} from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import { RefreshCcw, CheckSquare, AlertTriangle } from 'lucide-react';
import {
  API,
  showError,
  showInfo,
  showSuccess,
  stringToColor,
  convertUSDToCurrency,
} from '../../../helpers';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
import { useTranslation } from 'react-i18next';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';

// 两个上游价格来源：
// llm-metadata 只收录厂商自营入口，是官方定价，并补齐了 1 小时缓存写、
// 思考模式、错峰这几类本地从成本字段推导不出来的计费；
// models.dev 是全量原始数据，覆盖长尾模型和转售商报价。
const UPSTREAM_LLM_METADATA = 'llm-metadata';
const UPSTREAM_MODELS_DEV = 'models.dev';

const PRICE_OPTION_KEY_BY_FIELD = {
  model_ratio: 'ModelRatio',
  completion_ratio: 'CompletionRatio',
  cache_ratio: 'CacheRatio',
  create_cache_ratio: 'CreateCacheRatio',
  image_ratio: 'ImageRatio',
  audio_ratio: 'AudioRatio',
  audio_completion_ratio: 'AudioCompletionRatio',
  model_price: 'ModelPrice',
};

const PRICE_LABEL_BY_FIELD = {
  model_ratio: '输入价格',
  completion_ratio: '补全价格',
  cache_ratio: '缓存读取价格',
  create_cache_ratio: '缓存创建价格',
  image_ratio: '图片输入价格',
  audio_ratio: '音频输入价格',
  audio_completion_ratio: '音频补全价格',
  model_price: '固定价格',
};

function parseRatioOption(raw) {
  try {
    return JSON.parse(raw || '{}');
  } catch {
    return {};
  }
}

function parsePricingOptions(options = {}) {
  return {
    ModelRatio: parseRatioOption(options.ModelRatio),
    CompletionRatio: parseRatioOption(options.CompletionRatio),
    CacheRatio: parseRatioOption(options.CacheRatio),
    CreateCacheRatio: parseRatioOption(options.CreateCacheRatio),
    ImageRatio: parseRatioOption(options.ImageRatio),
    AudioRatio: parseRatioOption(options.AudioRatio),
    AudioCompletionRatio: parseRatioOption(options.AudioCompletionRatio),
    ModelPrice: parseRatioOption(options.ModelPrice),
    'billing_setting.billing_mode': parseRatioOption(
      options['billing_setting.billing_mode'],
    ),
    'billing_setting.billing_expr': parseRatioOption(
      options['billing_setting.billing_expr'],
    ),
  };
}

function toFiniteNumber(value) {
  if (value === null || value === undefined || value === '') {
    return null;
  }
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function getPricingValue(model, field, ratioTypes, localPricing, useUpstream) {
  if (useUpstream) {
    const upstreamNumber = toFiniteNumber(ratioTypes[field]?.upstream);
    if (upstreamNumber !== null) return upstreamNumber;
  }

  const currentNumber = toFiniteNumber(ratioTypes[field]?.current);
  if (currentNumber !== null) return currentNumber;

  const optionKey = PRICE_OPTION_KEY_BY_FIELD[field];
  return toFiniteNumber(localPricing[optionKey]?.[model]);
}

function getSyncPricePreview(
  model,
  ratioType,
  ratioTypes,
  localPricing,
  useUpstream,
) {
  if (!PRICE_LABEL_BY_FIELD[ratioType]) return null;

  const fieldValue = getPricingValue(
    model,
    ratioType,
    ratioTypes,
    localPricing,
    useUpstream,
  );
  if (fieldValue === null) return null;

  if (ratioType === 'model_price') {
    return {
      labelKey: PRICE_LABEL_BY_FIELD[ratioType],
      amountUSD: fieldValue,
      unit: 'request',
    };
  }

  const modelRatio = getPricingValue(
    model,
    'model_ratio',
    ratioTypes,
    localPricing,
    useUpstream,
  );
  if (modelRatio === null) return null;

  const inputPrice = modelRatio * 2;
  let amountUSD = inputPrice;

  if (
    ratioType === 'completion_ratio' ||
    ratioType === 'cache_ratio' ||
    ratioType === 'create_cache_ratio' ||
    ratioType === 'image_ratio' ||
    ratioType === 'audio_ratio'
  ) {
    amountUSD = inputPrice * fieldValue;
  } else if (ratioType === 'audio_completion_ratio') {
    const audioRatio = getPricingValue(
      model,
      'audio_ratio',
      ratioTypes,
      localPricing,
      useUpstream,
    );
    if (audioRatio === null) return null;
    amountUSD = inputPrice * audioRatio * fieldValue;
  }

  if (!Number.isFinite(amountUSD)) return null;
  return {
    labelKey: PRICE_LABEL_BY_FIELD[ratioType],
    amountUSD,
    unit: 'million_tokens',
  };
}

function SyncPricePreview({ preview, t }) {
  if (!preview) return null;

  const price = convertUSDToCurrency(preview.amountUSD, 4);
  const unit = preview.unit === 'request' ? t('次') : `1M Tokens`;

  return (
    <span
      className='truncate text-xs tabular-nums'
      style={{ color: 'var(--semi-color-text-2)' }}
    >
      {t(preview.labelKey)} {price} / {unit}
    </span>
  );
}

function ConflictConfirmModal({ t, visible, items, loading, onOk, onCancel }) {
  const isMobile = useIsMobile();
  const columns = [
    { title: t('模型'), dataIndex: 'model' },
    {
      title: t('当前计费'),
      dataIndex: 'current',
      render: (text) => <div style={{ whiteSpace: 'pre-wrap' }}>{text}</div>,
    },
    {
      title: t('修改为'),
      dataIndex: 'newVal',
      render: (text) => <div style={{ whiteSpace: 'pre-wrap' }}>{text}</div>,
    },
  ];

  return (
    <Modal
      title={t('确认冲突项修改')}
      visible={visible}
      confirmLoading={loading}
      cancelButtonProps={{ disabled: loading }}
      maskClosable={!loading}
      onCancel={loading ? undefined : onCancel}
      onOk={onOk}
      size={isMobile ? 'full-width' : 'large'}
    >
      <Table
        columns={columns}
        dataSource={items}
        pagination={false}
        size='small'
      />
    </Modal>
  );
}

export default function UpstreamRatioSync(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [syncLoading, setSyncLoading] = useState(false);
  const [confirmLoading, setConfirmLoading] = useState(false);

  // 差异数据和用户选择
  const [differences, setDifferences] = useState({});
  const [resolutions, setResolutions] = useState({});
  // 每个模型的价格来自上游的哪个提供商，以及可切换的其它来源
  const [sources, setSources] = useState({});
  const [candidates, setCandidates] = useState({});
  const [providers, setProviders] = useState([]);

  // 上游来源，默认用官方精选
  const [upstream, setUpstream] = useState(UPSTREAM_LLM_METADATA);

  // 来源偏好：auto 官方优先 / official_only 仅官方 / prefer 指定提供商优先
  const [sourceMode, setSourceMode] = useState('auto');
  const [preferredProviders, setPreferredProviders] = useState([]);

  // 是否已经执行过拉取
  const [hasSynced, setHasSynced] = useState(false);

  // 只显示已启用模型
  const [onlyEnabledModels, setOnlyEnabledModels] = useState(true);

  // 分页相关状态
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);

  // 搜索相关状态
  const [searchKeyword, setSearchKeyword] = useState('');

  // 倍率类型过滤
  const [ratioTypeFilter, setRatioTypeFilter] = useState('');

  // 冲突确认弹窗相关
  const [confirmVisible, setConfirmVisible] = useState(false);
  const [conflictItems, setConflictItems] = useState([]); // {model, current, newVal}

  // 定价选项串在生产环境常有数百 KB，搜索/翻页每次 render 都重解析代价过高。
  const parsedRatios = useMemo(
    () => parsePricingOptions(props.options),
    [props.options],
  );

  useEffect(() => {
    setCurrentPage(1);
  }, [ratioTypeFilter, searchKeyword]);

  const resetFetchedData = () => {
    setDifferences({});
    setSources({});
    setCandidates({});
    setResolutions({});
    setHasSynced(false);
    setCurrentPage(1);
  };

  const fetchUpstreamRatios = async () => {
    setSyncLoading(true);

    try {
      const payload = {
        timeout: 10,
        only_enabled_models: onlyEnabledModels,
        upstream,
        source_mode: sourceMode,
      };
      if (sourceMode === 'prefer') {
        payload.preferred_providers = preferredProviders;
      }
      const res = await API.post('/api/ratio_sync/fetch', payload);

      if (!res.data.success) {
        showError(res.data.message || t('后端请求失败'));
        return;
      }

      const {
        differences = {},
        sources = {},
        candidates = {},
        providers = [],
      } = res.data.data;

      setDifferences(differences);
      setSources(sources);
      setCandidates(candidates);
      setProviders(providers);
      setResolutions({});
      setHasSynced(true);

      if (Object.keys(differences).length === 0) {
        showSuccess(t('未找到差异化价格，无需同步'));
      }
    } catch (e) {
      showError(t('请求后端接口失败：') + e.message);
    } finally {
      setSyncLoading(false);
    }
  };

  // 把某个模型的上游价格切换到另一个提供商的报价
  const applyCandidateSource = (model, provider) => {
    const candidate = (candidates[model] || []).find(
      (item) => item.provider === provider,
    );
    if (!candidate) return;

    const localValueOf = (field) => {
      const optionKey = PRICE_OPTION_KEY_BY_FIELD[field];
      return toFiniteNumber(parsedRatios[optionKey]?.[model]);
    };

    const upstreamByField = {
      model_ratio: candidate.model_ratio,
      completion_ratio: candidate.completion_ratio,
      cache_ratio: candidate.cache_ratio,
      create_cache_ratio: candidate.create_cache_ratio,
      audio_ratio: candidate.audio_ratio,
      audio_completion_ratio: candidate.audio_completion_ratio,
    };

    const nextRow = {};
    Object.entries(upstreamByField).forEach(([field, value]) => {
      if (value === null || value === undefined) return;
      const current = localValueOf(field);
      if (current !== null && Math.abs(current - value) < 1e-9) return;
      nextRow[field] = { current, upstream: value };
    });

    // 该来源带上下文阶梯价时，额外给出改用表达式计费的选项
    if (candidate.billing_expr) {
      const currentExpr =
        parsedRatios['billing_setting.billing_expr']?.[model] ?? null;
      if (currentExpr !== candidate.billing_expr) {
        nextRow.billing_expr = {
          current: currentExpr,
          upstream: candidate.billing_expr,
        };
        const currentMode =
          parsedRatios['billing_setting.billing_mode']?.[model] ?? null;
        if (currentMode !== 'tiered_expr') {
          nextRow.billing_mode = {
            current: currentMode,
            upstream: 'tiered_expr',
          };
        }
      }
    }

    setDifferences((prev) => {
      const next = { ...prev };
      if (Object.keys(nextRow).length === 0) {
        delete next[model];
      } else {
        next[model] = nextRow;
      }
      return next;
    });
    setSources((prev) => ({
      ...prev,
      [model]: { provider: candidate.provider, official: candidate.official },
    }));
    // 旧来源上的勾选不能带到新来源
    setResolutions((prev) => {
      if (!prev[model]) return prev;
      const next = { ...prev };
      delete next[model];
      return next;
    });

    if (Object.keys(nextRow).length === 0) {
      showInfo(t('该来源与本地价格一致，已从列表中移除'));
    }
  };

  const ratioSyncFields = [
    'model_ratio',
    'completion_ratio',
    'cache_ratio',
    'create_cache_ratio',
    'image_ratio',
    'audio_ratio',
    'audio_completion_ratio',
  ];

  const numericSyncFields = new Set([...ratioSyncFields, 'model_price']);
  const syncFieldOrder = [
    ...ratioSyncFields,
    'model_price',
    'billing_mode',
    'billing_expr',
  ];

  function getSyncFieldLabel(ratioType) {
    const typeMap = {
      model_ratio: t('模型倍率'),
      completion_ratio: t('补全倍率'),
      cache_ratio: t('缓存倍率'),
      create_cache_ratio: t('缓存创建倍率'),
      image_ratio: t('图片倍率'),
      audio_ratio: t('音频倍率'),
      audio_completion_ratio: t('音频补全倍率'),
      model_price: t('固定价格'),
      billing_mode: t('计费模式'),
      billing_expr: t('表达式计费'),
    };
    return typeMap[ratioType] || ratioType;
  }

  function getOrderedRatioTypes(ratioTypes) {
    const keys = Object.keys(ratioTypes || {});
    const ordered = [
      ...syncFieldOrder.filter((field) => keys.includes(field)),
      ...keys.filter((field) => !syncFieldOrder.includes(field)),
    ];
    return ratioTypeFilter
      ? ordered.filter((field) => field === ratioTypeFilter)
      : ordered;
  }

  function deleteResolutionField(newRes, model, ratioType) {
    if (!newRes[model]) return;
    delete newRes[model][ratioType];
    if (ratioType === 'billing_expr') {
      delete newRes[model].billing_mode;
    }
    if (ratioType === 'billing_mode') {
      delete newRes[model].billing_expr;
    }
    if (Object.keys(newRes[model]).length === 0) {
      delete newRes[model];
    }
  }

  function getBillingCategory(ratioType) {
    if (ratioType === 'model_price') return 'price';
    if (ratioType === 'billing_mode' || ratioType === 'billing_expr') {
      return 'tiered';
    }
    return 'ratio';
  }

  function optionKeyBySyncField(ratioType) {
    const explicit = {
      billing_mode: 'billing_setting.billing_mode',
      billing_expr: 'billing_setting.billing_expr',
    };
    if (explicit[ratioType]) return explicit[ratioType];
    return ratioType
      .split('_')
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join('');
  }

  const selectValue = useCallback((model, ratioType, value) => {
    const category = getBillingCategory(ratioType);

    setResolutions((prev) => {
      const newModelRes = { ...(prev[model] || {}) };

      // 固定价格与倍率互斥，同一模型只能同步其中一类
      Object.keys(newModelRes).forEach((rt) => {
        if (
          category !== 'tiered' &&
          getBillingCategory(rt) !== 'tiered' &&
          getBillingCategory(rt) !== category
        ) {
          delete newModelRes[rt];
        }
      });

      newModelRes[ratioType] = value;

      if (ratioType === 'billing_expr' && !newModelRes.billing_mode) {
        newModelRes.billing_mode = 'tiered_expr';
      }

      return {
        ...prev,
        [model]: newModelRes,
      };
    });
  }, []);

  const applySync = async () => {
    const currentRatios = parsePricingOptions(props.options);

    const conflicts = [];

    const getLocalBillingCategory = (model) => {
      if (currentRatios.ModelPrice[model] !== undefined) return 'price';
      if (
        currentRatios.ModelRatio[model] !== undefined ||
        currentRatios.CompletionRatio[model] !== undefined ||
        currentRatios.CacheRatio[model] !== undefined ||
        currentRatios.CreateCacheRatio[model] !== undefined ||
        currentRatios.ImageRatio[model] !== undefined ||
        currentRatios.AudioRatio[model] !== undefined ||
        currentRatios.AudioCompletionRatio[model] !== undefined
      )
        return 'ratio';
      return null;
    };

    Object.entries(resolutions).forEach(([model, ratios]) => {
      const localCat = getLocalBillingCategory(model);
      const newCat =
        'model_price' in ratios
          ? 'price'
          : ratioSyncFields.some((rt) => rt in ratios)
            ? 'ratio'
            : 'tiered';

      if (localCat && newCat !== 'tiered' && localCat !== newCat) {
        const currentDesc =
          localCat === 'price'
            ? `${t('固定价格')} : ${currentRatios.ModelPrice[model]}`
            : `${t('模型倍率')} : ${currentRatios.ModelRatio[model] ?? '-'}\n${t('补全倍率')} : ${currentRatios.CompletionRatio[model] ?? '-'}`;

        let newDesc = '';
        if (newCat === 'price') {
          newDesc = `${t('固定价格')} : ${ratios['model_price']}`;
        } else {
          const newModelRatio = ratios['model_ratio'] ?? '-';
          const newCompRatio = ratios['completion_ratio'] ?? '-';
          newDesc = `${t('模型倍率')} : ${newModelRatio}\n${t('补全倍率')} : ${newCompRatio}`;
        }

        conflicts.push({
          key: model,
          model,
          current: currentDesc,
          newVal: newDesc,
        });
      }
    });

    if (conflicts.length > 0) {
      setConflictItems(conflicts);
      setConfirmVisible(true);
      return;
    }

    await performSync(currentRatios);
  };

  const performSync = useCallback(
    async (currentRatios) => {
      const finalRatios = {
        ModelRatio: { ...currentRatios.ModelRatio },
        CompletionRatio: { ...currentRatios.CompletionRatio },
        CacheRatio: { ...currentRatios.CacheRatio },
        CreateCacheRatio: { ...currentRatios.CreateCacheRatio },
        ImageRatio: { ...currentRatios.ImageRatio },
        AudioRatio: { ...currentRatios.AudioRatio },
        AudioCompletionRatio: { ...currentRatios.AudioCompletionRatio },
        ModelPrice: { ...currentRatios.ModelPrice },
        'billing_setting.billing_mode': {
          ...currentRatios['billing_setting.billing_mode'],
        },
        'billing_setting.billing_expr': {
          ...currentRatios['billing_setting.billing_expr'],
        },
      };

      Object.entries(resolutions).forEach(([model, ratios]) => {
        const selectedTypes = Object.keys(ratios);
        const hasPrice = selectedTypes.includes('model_price');
        const hasRatio = selectedTypes.some((rt) =>
          ratioSyncFields.includes(rt),
        );

        if (hasPrice) {
          delete finalRatios.ModelRatio[model];
          delete finalRatios.CompletionRatio[model];
          delete finalRatios.CacheRatio[model];
          delete finalRatios.CreateCacheRatio[model];
          delete finalRatios.ImageRatio[model];
          delete finalRatios.AudioRatio[model];
          delete finalRatios.AudioCompletionRatio[model];
        }
        if (hasRatio) {
          delete finalRatios.ModelPrice[model];
        }

        Object.entries(ratios).forEach(([ratioType, value]) => {
          const optionKey = optionKeyBySyncField(ratioType);
          finalRatios[optionKey][model] = numericSyncFields.has(ratioType)
            ? parseFloat(value)
            : value;
        });
      });

      setLoading(true);
      showInfo(t('正在同步价格，请稍候'));
      let success = false;
      try {
        const updates = Object.entries(finalRatios).map(([key, value]) =>
          API.put('/api/option/', {
            key,
            value: JSON.stringify(value, null, 2),
          }),
        );

        const results = await Promise.all(updates);

        if (results.every((res) => res.data.success)) {
          showSuccess(t('同步成功'));
          props.refresh();

          setDifferences((prevDifferences) => {
            const newDifferences = { ...prevDifferences };

            Object.entries(resolutions).forEach(([model, ratios]) => {
              Object.keys(ratios).forEach((ratioType) => {
                if (newDifferences[model] && newDifferences[model][ratioType]) {
                  delete newDifferences[model][ratioType];

                  if (Object.keys(newDifferences[model]).length === 0) {
                    delete newDifferences[model];
                  }
                }
              });
            });

            return newDifferences;
          });

          setResolutions({});
          success = true;
        } else {
          showError(t('部分保存失败'));
        }
      } catch (error) {
        showError(t('保存失败'));
      } finally {
        setLoading(false);
      }
      return success;
    },
    [resolutions, props.options, props.refresh],
  );

  const getCurrentPageData = (dataSource) => {
    const startIndex = (currentPage - 1) * pageSize;
    const endIndex = startIndex + pageSize;
    return dataSource.slice(startIndex, endIndex);
  };

  const renderHeader = () => (
    <div className='flex flex-col w-full'>
      <div className='flex flex-col md:flex-row justify-between items-center gap-4 w-full'>
        <div className='flex flex-col md:flex-row gap-2 w-full md:w-auto order-2 md:order-1'>
          <Button
            icon={<RefreshCcw size={14} />}
            className='w-full md:w-auto mt-2'
            loading={syncLoading}
            disabled={loading || syncLoading || confirmLoading}
            onClick={fetchUpstreamRatios}
          >
            {t('从 {{upstream}} 获取价格', { upstream })}
          </Button>

          {(() => {
            const hasSelections = Object.keys(resolutions).length > 0;

            return (
              <Button
                icon={<CheckSquare size={14} />}
                type='secondary'
                onClick={applySync}
                loading={loading || confirmLoading}
                disabled={
                  !hasSelections || loading || syncLoading || confirmLoading
                }
                className='w-full md:w-auto mt-2'
              >
                {t('应用同步')}
              </Button>
            );
          })()}

          <div className='flex flex-col sm:flex-row gap-2 w-full md:w-auto mt-2'>
            <Tooltip
              content={t(
                '官方精选只收录厂商自营入口，并支持 1 小时缓存写、思考模式、错峰计费；全量覆盖长尾模型，但可能只有转售商报价',
              )}
            >
              <Select
                value={upstream}
                onChange={(value) => {
                  setUpstream(value);
                  // 两个上游的提供商清单不同，之前选的首选提供商带不过去
                  setProviders([]);
                  setPreferredProviders([]);
                  resetFetchedData();
                }}
                className='w-full sm:w-56'
                disabled={loading || syncLoading || confirmLoading}
              >
                <Select.Option value={UPSTREAM_LLM_METADATA}>
                  {t('官方精选（llm-metadata）')}
                </Select.Option>
                <Select.Option value={UPSTREAM_MODELS_DEV}>
                  {t('全量（models.dev）')}
                </Select.Option>
              </Select>
            </Tooltip>

            <Select
              value={sourceMode}
              onChange={(value) => {
                setSourceMode(value);
                resetFetchedData();
              }}
              className='w-full sm:w-48'
              disabled={loading || syncLoading || confirmLoading}
            >
              <Select.Option value='auto'>
                {t('自动（官方优先）')}
              </Select.Option>
              <Select.Option value='official_only'>
                {t('仅官方来源')}
              </Select.Option>
              <Select.Option value='prefer'>
                {t('优先指定提供商')}
              </Select.Option>
            </Select>

            {sourceMode === 'prefer' ? (
              <Select
                multiple
                filter
                maxTagCount={2}
                placeholder={
                  providers.length === 0
                    ? t('先获取一次价格后可选择提供商')
                    : t('选择优先使用的提供商')
                }
                value={preferredProviders}
                onChange={(value) => {
                  setPreferredProviders(value);
                  resetFetchedData();
                }}
                className='w-full sm:w-64'
                disabled={loading || syncLoading || confirmLoading}
              >
                {providers.map((provider) => (
                  <Select.Option
                    key={provider.provider}
                    value={provider.provider}
                  >
                    {provider.provider} ·{' '}
                    {provider.official ? t('官方') : t('第三方')} ·{' '}
                    {provider.model_count}
                  </Select.Option>
                ))}
              </Select>
            ) : null}

            <Input
              prefix={<IconSearch size={14} />}
              placeholder={t('搜索模型名称')}
              value={searchKeyword}
              onChange={setSearchKeyword}
              className='w-full sm:w-64'
              disabled={loading || syncLoading || confirmLoading}
              showClear
            />

            <Select
              placeholder={t('按价格字段筛选')}
              value={ratioTypeFilter}
              onChange={setRatioTypeFilter}
              className='w-full sm:w-48'
              disabled={loading || syncLoading || confirmLoading}
              showClear
              onClear={() => setRatioTypeFilter('')}
            >
              <Select.Option value='model_ratio'>{t('模型倍率')}</Select.Option>
              <Select.Option value='completion_ratio'>
                {t('补全倍率')}
              </Select.Option>
              <Select.Option value='cache_ratio'>{t('缓存倍率')}</Select.Option>
              <Select.Option value='create_cache_ratio'>
                {t('缓存创建倍率')}
              </Select.Option>
              <Select.Option value='image_ratio'>{t('图片倍率')}</Select.Option>
              <Select.Option value='audio_ratio'>{t('音频倍率')}</Select.Option>
              <Select.Option value='audio_completion_ratio'>
                {t('音频补全倍率')}
              </Select.Option>
              <Select.Option value='model_price'>{t('固定价格')}</Select.Option>
            </Select>

            <Checkbox
              checked={onlyEnabledModels}
              onChange={(e) => {
                setOnlyEnabledModels(e.target.checked);
                resetFetchedData();
              }}
              disabled={loading || syncLoading || confirmLoading}
              style={{ marginLeft: 4, whiteSpace: 'nowrap' }}
            >
              {t('只显示我的模型')}
            </Checkbox>
          </div>
        </div>
      </div>
    </div>
  );

  const renderDifferenceTable = () => {
    const dataSource = useMemo(() => {
      return Object.entries(differences).map(([model, ratioTypes]) => {
        const hasPrice = 'model_price' in ratioTypes;
        const hasOtherRatio = ratioSyncFields.some((rt) => rt in ratioTypes);

        return {
          key: model,
          model,
          ratioTypes,
          billingConflict: hasPrice && hasOtherRatio,
        };
      });
    }, [differences]);

    const filteredDataSource = useMemo(() => {
      if (!searchKeyword.trim() && !ratioTypeFilter) {
        return dataSource;
      }

      return dataSource.filter((item) => {
        const matchesKeyword =
          !searchKeyword.trim() ||
          item.model.toLowerCase().includes(searchKeyword.toLowerCase().trim());

        const matchesRatioType =
          !ratioTypeFilter || ratioTypeFilter in item.ratioTypes;

        return matchesKeyword && matchesRatioType;
      });
    }, [dataSource, searchKeyword, ratioTypeFilter]);

    const renderValueTag = (value, color = 'default') => {
      if (value === null || value === undefined) {
        return (
          <Tag color='default' shape='circle'>
            {t('未设置')}
          </Tag>
        );
      }

      const text = String(value);
      return (
        <Tooltip content={text}>
          <Tag color={color} shape='circle'>
            <span className='inline-block max-w-[360px] truncate align-bottom'>
              {text}
            </span>
          </Tag>
        </Tooltip>
      );
    };

    const renderCurrentFields = (record) => {
      const fields = getOrderedRatioTypes(record.ratioTypes);
      return (
        <div className='flex min-w-[260px] flex-col gap-2'>
          {fields.map((ratioType) => {
            const preview = getSyncPricePreview(
              record.model,
              ratioType,
              record.ratioTypes,
              parsedRatios,
              false,
            );
            return (
              <div
                key={ratioType}
                className='flex min-w-0 flex-wrap items-start gap-2'
              >
                <Tag color={stringToColor(ratioType)} shape='circle'>
                  {getSyncFieldLabel(ratioType)}
                </Tag>
                <div className='flex min-w-0 flex-col gap-1'>
                  {renderValueTag(
                    record.ratioTypes[ratioType]?.current,
                    'blue',
                  )}
                  <SyncPricePreview preview={preview} t={t} />
                </div>
              </div>
            );
          })}
        </div>
      );
    };

    const renderUpstreamField = (record, ratioType) => {
      const upstreamVal = record.ratioTypes[ratioType]?.upstream;
      const preview = getSyncPricePreview(
        record.model,
        ratioType,
        record.ratioTypes,
        parsedRatios,
        true,
      );

      if (upstreamVal === null || upstreamVal === undefined) {
        return renderValueTag(undefined);
      }

      const text = String(upstreamVal);
      const isSelected = resolutions[record.model]?.[ratioType] === upstreamVal;

      return (
        <Checkbox
          checked={isSelected}
          disabled={loading || syncLoading || confirmLoading}
          onChange={(e) => {
            if (e.target.checked) {
              selectValue(record.model, ratioType, upstreamVal);
            } else {
              setResolutions((prev) => {
                const newRes = { ...prev };
                deleteResolutionField(newRes, record.model, ratioType);
                return newRes;
              });
            }
          }}
        >
          <div className='flex min-w-0 flex-col gap-1'>
            <Tooltip content={text}>
              <span className='inline-block max-w-[360px] truncate align-bottom'>
                {text}
              </span>
            </Tooltip>
            <SyncPricePreview preview={preview} t={t} />
          </div>
        </Checkbox>
      );
    };

    const renderUpstreamFields = (record) => {
      const fields = getOrderedRatioTypes(record.ratioTypes);
      return (
        <div className='flex min-w-[340px] flex-col gap-2'>
          {fields.map((ratioType) => (
            <div key={ratioType} className='flex min-w-0 items-start gap-2'>
              <Tag
                color={stringToColor(ratioType)}
                shape='circle'
                className='shrink-0'
              >
                {getSyncFieldLabel(ratioType)}
              </Tag>
              <div className='min-w-0 flex-1'>
                {renderUpstreamField(record, ratioType)}
              </div>
            </div>
          ))}
        </div>
      );
    };

    const renderSourcePicker = (model, source) => {
      const modelCandidates = candidates[model] || [];

      if (modelCandidates.length <= 1) {
        if (!source) return null;
        return (
          <Tooltip
            content={
              source.official
                ? t('价格来自模型厂商或其官方云托管入口')
                : t(
                    '{{upstream}} 上没有该模型的官方条目，价格来自第三方转售/聚合商，仅供参考',
                    { upstream },
                  )
            }
          >
            <Tag
              size='small'
              shape='circle'
              color={source.official ? 'green' : 'orange'}
            >
              {source.provider} · {source.official ? t('官方') : t('第三方')}
            </Tag>
          </Tooltip>
        );
      }

      return (
        <Select
          size='small'
          value={source?.provider}
          onChange={(value) => applyCandidateSource(model, value)}
          disabled={loading || syncLoading || confirmLoading}
          style={{ width: 240 }}
          optionList={modelCandidates.map((candidate) => ({
            value: candidate.provider,
            label: `${candidate.provider} · ${
              candidate.official ? t('官方') : t('第三方')
            } · ${convertUSDToCurrency(candidate.model_ratio * 2, 2)}/1M`,
          }))}
        />
      );
    };

    if (filteredDataSource.length === 0) {
      if (syncLoading) {
        return (
          <div className='flex min-h-[260px] flex-col items-center justify-center gap-3'>
            <Spin size='large' />
            <div className='text-sm text-gray-500'>
              {t('正在同步上游价格，请稍候')}
            </div>
          </div>
        );
      }

      return (
        <Empty
          image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
          darkModeImage={
            <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
          }
          description={
            searchKeyword.trim()
              ? t('未找到匹配的模型')
              : hasSynced
                ? t('暂无差异化价格显示')
                : t('请先从 {{upstream}} 获取价格', { upstream })
          }
          style={{ padding: 30 }}
        />
      );
    }

    const upstreamStats = (() => {
      let selectableCount = 0;
      let selectedCount = 0;

      filteredDataSource.forEach((row) => {
        getOrderedRatioTypes(row.ratioTypes).forEach((ratioType) => {
          const upstreamVal = row.ratioTypes[ratioType]?.upstream;
          if (upstreamVal === null || upstreamVal === undefined) return;
          selectableCount++;
          if (resolutions[row.model]?.[ratioType] === upstreamVal) {
            selectedCount++;
          }
        });
      });

      return {
        allSelected: selectableCount > 0 && selectedCount === selectableCount,
        partiallySelected: selectedCount > 0 && selectedCount < selectableCount,
        hasSelectableItems: selectableCount > 0,
      };
    })();

    const handleBulkSelect = (checked) => {
      if (checked) {
        filteredDataSource.forEach((row) => {
          getOrderedRatioTypes(row.ratioTypes).forEach((ratioType) => {
            const upstreamVal = row.ratioTypes[ratioType]?.upstream;
            if (upstreamVal === null || upstreamVal === undefined) return;
            selectValue(row.model, ratioType, upstreamVal);
          });
        });
      } else {
        setResolutions((prev) => {
          const newRes = { ...prev };
          filteredDataSource.forEach((row) => {
            getOrderedRatioTypes(row.ratioTypes).forEach((ratioType) => {
              deleteResolutionField(newRes, row.model, ratioType);
            });
          });
          return newRes;
        });
      }
    };

    const columns = [
      {
        title: t('模型'),
        dataIndex: 'model',
        fixed: 'left',
        render: (text, record) => {
          const source = sources[record.model];
          return (
            <div className='flex min-w-[180px] flex-col gap-1'>
              <div className='flex items-center gap-2'>
                <span className='font-medium'>{text}</span>
                {record.billingConflict && (
                  <Tooltip
                    position='top'
                    content={t(
                      '该模型存在固定价格与倍率计费方式冲突，请确认选择',
                    )}
                  >
                    <AlertTriangle
                      size={14}
                      className='shrink-0 text-yellow-500'
                    />
                  </Tooltip>
                )}
              </div>
              {renderSourcePicker(record.model, source)}
            </div>
          );
        },
      },
      {
        title: t('当前价格'),
        dataIndex: 'current',
        render: (_, record) => renderCurrentFields(record),
      },
      {
        title: upstreamStats.hasSelectableItems ? (
          <Checkbox
            checked={upstreamStats.allSelected}
            indeterminate={upstreamStats.partiallySelected}
            disabled={loading || syncLoading || confirmLoading}
            onChange={(e) => handleBulkSelect(e.target.checked)}
          >
            {upstream}
          </Checkbox>
        ) : (
          <span>{upstream}</span>
        ),
        dataIndex: 'upstream',
        render: (_, record) => renderUpstreamFields(record),
      },
    ];

    return (
      <Table
        columns={columns}
        dataSource={getCurrentPageData(filteredDataSource)}
        pagination={{
          currentPage: currentPage,
          pageSize: pageSize,
          total: filteredDataSource.length,
          showSizeChanger: true,
          showQuickJumper: true,
          pageSizeOptions: ['5', '10', '20', '50'],
          onChange: (page, size) => {
            setCurrentPage(page);
            setPageSize(size);
          },
          onShowSizeChange: (current, size) => {
            setCurrentPage(1);
            setPageSize(size);
          },
        }}
        scroll={{ x: 'max-content' }}
        size='middle'
        loading={loading || syncLoading}
      />
    );
  };

  return (
    <>
      <Form.Section text={renderHeader()}>
        {renderDifferenceTable()}
      </Form.Section>

      <ConflictConfirmModal
        t={t}
        visible={confirmVisible}
        items={conflictItems}
        loading={confirmLoading}
        onOk={async () => {
          setConfirmLoading(true);
          try {
            const success = await performSync(
              parsePricingOptions(props.options),
            );
            if (success) {
              setConfirmVisible(false);
            }
          } finally {
            setConfirmLoading(false);
          }
        }}
        onCancel={() => setConfirmVisible(false)}
      />
    </>
  );
}
