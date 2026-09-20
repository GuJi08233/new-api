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
import { useCallback, useEffect, useMemo, useState } from 'react';
import { API, showError, showSuccess, showWarning } from '../../../../helpers';
import {
  combineBillingExpr,
  splitBillingExprAndRequestRules,
} from '../components/requestRuleExpr';

export const PAGE_SIZE = 10;
export const PRICE_SUFFIX = '$/1M tokens';
const EMPTY_CANDIDATE_MODEL_NAMES = [];

// 分组级定价的全部 option，清空分组配置时需要逐个移除该分组的键
const GROUP_PRICING_OPTION_KEYS = [
  'GroupModelPrice',
  'GroupModelRatio',
  'GroupCompletionRatio',
  'GroupCacheRatio',
  'GroupCreateCacheRatio',
  'GroupImageRatio',
  'GroupAudioRatio',
  'GroupAudioCompletionRatio',
  'GroupBillingMode',
  'GroupBillingExpr',
];

const EMPTY_MODEL = {
  name: '',
  billingMode: 'per-token',
  fixedPrice: '',
  inputPrice: '',
  completionPrice: '',
  lockedCompletionRatio: '',
  completionRatioLocked: false,
  cachePrice: '',
  createCachePrice: '',
  imagePrice: '',
  audioInputPrice: '',
  audioOutputPrice: '',
  billingExpr: '',
  requestRuleExpr: '',
  rawRatios: {
    modelRatio: '',
    completionRatio: '',
    cacheRatio: '',
    createCacheRatio: '',
    imageRatio: '',
    audioRatio: '',
    audioCompletionRatio: '',
  },
  hasConflict: false,
};

const NUMERIC_INPUT_REGEX = /^(\d+(\.\d*)?|\.\d*)?$/;

export const hasValue = (value) =>
  value !== '' && value !== null && value !== undefined && value !== false;

const toNumericString = (value) => {
  if (!hasValue(value) && value !== 0) {
    return '';
  }
  const num = Number(value);
  return Number.isFinite(num) ? String(num) : '';
};

const toNumberOrNull = (value) => {
  if (!hasValue(value) && value !== 0) {
    return null;
  }
  const num = Number(value);
  return Number.isFinite(num) ? num : null;
};

const formatNumber = (value) => {
  const num = toNumberOrNull(value);
  if (num === null) {
    return '';
  }
  return parseFloat(num.toFixed(12)).toString();
};

const toNormalizedNumber = (value) => {
  const formatted = formatNumber(value);
  return formatted === '' ? null : Number(formatted);
};

export const parseOptionJSON = (rawValue) => {
  if (!rawValue || rawValue.trim() === '') {
    return {};
  }
  try {
    const parsed = JSON.parse(rawValue);
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch (error) {
    console.error('JSON解析错误:', error);
    return {};
  }
};

const ratioToBasePrice = (ratio) => {
  const num = toNumberOrNull(ratio);
  if (num === null) return '';
  return formatNumber(num * 2);
};

const normalizeCompletionRatioMeta = (rawMeta) => {
  if (!rawMeta || typeof rawMeta !== 'object' || Array.isArray(rawMeta)) {
    return {
      locked: false,
      ratio: '',
    };
  }

  return {
    locked: Boolean(rawMeta.locked),
    ratio: toNumericString(rawMeta.ratio),
  };
};

const buildModelState = (name, sourceMaps) => {
  const billingMode = sourceMaps.ModelBillingMode?.[name];
  if (billingMode === 'tiered_expr') {
    const fullBillingExpr = sourceMaps.ModelBillingExpr?.[name] || '';
    const { billingExpr, requestRuleExpr } =
      splitBillingExprAndRequestRules(fullBillingExpr);
    return {
      ...EMPTY_MODEL,
      name,
      billingMode: 'tiered_expr',
      billingExpr,
      requestRuleExpr,
      rawRatios: { ...EMPTY_MODEL.rawRatios },
      hasConflict: false,
    };
  }

  const modelRatio = toNumericString(sourceMaps.ModelRatio[name]);
  const completionRatio = toNumericString(sourceMaps.CompletionRatio[name]);
  const completionRatioMeta = normalizeCompletionRatioMeta(
    sourceMaps.CompletionRatioMeta?.[name],
  );
  const cacheRatio = toNumericString(sourceMaps.CacheRatio[name]);
  const createCacheRatio = toNumericString(sourceMaps.CreateCacheRatio[name]);
  const imageRatio = toNumericString(sourceMaps.ImageRatio[name]);
  const audioRatio = toNumericString(sourceMaps.AudioRatio[name]);
  const audioCompletionRatio = toNumericString(
    sourceMaps.AudioCompletionRatio[name],
  );
  const fixedPrice = toNumericString(sourceMaps.ModelPrice[name]);
  const inputPrice = ratioToBasePrice(modelRatio);
  const inputPriceNumber = toNumberOrNull(inputPrice);
  const audioInputPrice =
    inputPriceNumber !== null && hasValue(audioRatio)
      ? formatNumber(inputPriceNumber * Number(audioRatio))
      : '';

  return {
    ...EMPTY_MODEL,
    name,
    billingMode: hasValue(fixedPrice) ? 'per-request' : 'per-token',
    fixedPrice,
    inputPrice,
    completionRatioLocked: completionRatioMeta.locked,
    lockedCompletionRatio: completionRatioMeta.ratio,
    completionPrice:
      inputPriceNumber !== null &&
      hasValue(
        completionRatioMeta.locked
          ? completionRatioMeta.ratio
          : completionRatio,
      )
        ? formatNumber(
            inputPriceNumber *
              Number(
                completionRatioMeta.locked
                  ? completionRatioMeta.ratio
                  : completionRatio,
              ),
          )
        : '',
    cachePrice:
      inputPriceNumber !== null && hasValue(cacheRatio)
        ? formatNumber(inputPriceNumber * Number(cacheRatio))
        : '',
    createCachePrice:
      inputPriceNumber !== null && hasValue(createCacheRatio)
        ? formatNumber(inputPriceNumber * Number(createCacheRatio))
        : '',
    imagePrice:
      inputPriceNumber !== null && hasValue(imageRatio)
        ? formatNumber(inputPriceNumber * Number(imageRatio))
        : '',
    audioInputPrice,
    audioOutputPrice:
      toNumberOrNull(audioInputPrice) !== null && hasValue(audioCompletionRatio)
        ? formatNumber(Number(audioInputPrice) * Number(audioCompletionRatio))
        : '',
    requestRuleExpr: '',
    rawRatios: {
      modelRatio,
      completionRatio,
      cacheRatio,
      createCacheRatio,
      imageRatio,
      audioRatio,
      audioCompletionRatio,
    },
    hasConflict:
      hasValue(fixedPrice) &&
      [
        modelRatio,
        completionRatio,
        cacheRatio,
        createCacheRatio,
        imageRatio,
        audioRatio,
        audioCompletionRatio,
      ].some(hasValue),
  };
};

export const isBasePricingUnset = (model) =>
  model.billingMode !== 'tiered_expr' &&
  !hasValue(model.fixedPrice) &&
  !hasValue(model.inputPrice);

export const getModelWarnings = (model, t) => {
  if (!model) {
    return [];
  }
  if (model.billingMode === 'tiered_expr') {
    return [];
  }
  const warnings = [];
  const hasDerivedPricing = [
    model.inputPrice,
    model.completionPrice,
    model.cachePrice,
    model.createCachePrice,
    model.imagePrice,
    model.audioInputPrice,
    model.audioOutputPrice,
  ].some(hasValue);

  if (model.hasConflict) {
    warnings.push(
      t('当前模型同时存在按次价格和倍率配置，保存时会按当前计费方式覆盖。'),
    );
  }

  if (
    !hasValue(model.inputPrice) &&
    [
      model.rawRatios.completionRatio,
      model.rawRatios.cacheRatio,
      model.rawRatios.createCacheRatio,
      model.rawRatios.imageRatio,
      model.rawRatios.audioRatio,
      model.rawRatios.audioCompletionRatio,
    ].some(hasValue)
  ) {
    warnings.push(
      t(
        '当前模型存在未显式设置输入倍率的扩展倍率；填写输入价格后会自动换算为价格字段。',
      ),
    );
  }

  if (
    model.billingMode === 'per-token' &&
    hasDerivedPricing &&
    !hasValue(model.inputPrice)
  ) {
    warnings.push(t('按量计费下需要先填写输入价格，才能保存其它价格项。'));
  }

  if (
    model.billingMode === 'per-token' &&
    hasValue(model.audioOutputPrice) &&
    !hasValue(model.audioInputPrice)
  ) {
    warnings.push(t('填写音频补全价格前，需要先填写音频输入价格。'));
  }

  return warnings;
};

export const buildSummaryText = (model, t) => {
  const requestRuleSuffix =
    model.billingMode === 'tiered_expr' && model.requestRuleExpr
      ? `，${t('请求规则')}`
      : '';
  if (model.billingMode === 'tiered_expr') {
    const expr = model.billingExpr;
    if (!expr) return `${t('表达式计费')}${requestRuleSuffix}`;
    const tierCount = (expr.match(/tier\(/g) || []).length;
    if (tierCount === 0) {
      return `${t('表达式计费')}${requestRuleSuffix}`;
    }
    return `${t('阶梯计费')} (${tierCount} ${t('档')})${requestRuleSuffix}`;
  }

  if (model.billingMode === 'per-request' && hasValue(model.fixedPrice)) {
    return `${t('按次')} $${model.fixedPrice} / ${t('次')}${requestRuleSuffix}`;
  }

  if (hasValue(model.inputPrice)) {
    const extraCount = [
      model.completionPrice,
      model.cachePrice,
      model.createCachePrice,
      model.imagePrice,
      model.audioInputPrice,
      model.audioOutputPrice,
    ].filter(hasValue).length;
    const extraLabel =
      extraCount > 0 ? `，${t('额外价格项')} ${extraCount}` : '';
    return `${t('输入')} $${model.inputPrice}${extraLabel}${requestRuleSuffix}`;
  }

  return `${t('未设置价格')}${requestRuleSuffix}`;
};

export const buildOptionalFieldToggles = (model) => ({
  completionPrice:
    model.completionRatioLocked || hasValue(model.completionPrice),
  cachePrice: hasValue(model.cachePrice),
  createCachePrice: hasValue(model.createCachePrice),
  imagePrice: hasValue(model.imagePrice),
  audioInputPrice: hasValue(model.audioInputPrice),
  audioOutputPrice: hasValue(model.audioOutputPrice),
});

const serializeModel = (model, t) => {
  const result = {
    ModelPrice: null,
    ModelRatio: null,
    CompletionRatio: null,
    CacheRatio: null,
    CreateCacheRatio: null,
    ImageRatio: null,
    AudioRatio: null,
    AudioCompletionRatio: null,
  };

  if (model.billingMode === 'per-request') {
    if (hasValue(model.fixedPrice)) {
      result.ModelPrice = toNormalizedNumber(model.fixedPrice);
    }
    return result;
  }

  const inputPrice = toNumberOrNull(model.inputPrice);
  const completionPrice = toNumberOrNull(model.completionPrice);
  const cachePrice = toNumberOrNull(model.cachePrice);
  const createCachePrice = toNumberOrNull(model.createCachePrice);
  const imagePrice = toNumberOrNull(model.imagePrice);
  const audioInputPrice = toNumberOrNull(model.audioInputPrice);
  const audioOutputPrice = toNumberOrNull(model.audioOutputPrice);

  const hasDependentPrice = [
    completionPrice,
    cachePrice,
    createCachePrice,
    imagePrice,
    audioInputPrice,
    audioOutputPrice,
  ].some((value) => value !== null);

  if (inputPrice === null) {
    if (hasDependentPrice) {
      throw new Error(
        t(
          '模型 {{name}} 缺少输入价格，无法计算补全/缓存/图片/音频价格对应的倍率',
          {
            name: model.name,
          },
        ),
      );
    }

    if (hasValue(model.rawRatios.modelRatio)) {
      result.ModelRatio = toNormalizedNumber(model.rawRatios.modelRatio);
    }
    if (hasValue(model.rawRatios.completionRatio)) {
      result.CompletionRatio = toNormalizedNumber(
        model.rawRatios.completionRatio,
      );
    }
    if (hasValue(model.rawRatios.cacheRatio)) {
      result.CacheRatio = toNormalizedNumber(model.rawRatios.cacheRatio);
    }
    if (hasValue(model.rawRatios.createCacheRatio)) {
      result.CreateCacheRatio = toNormalizedNumber(
        model.rawRatios.createCacheRatio,
      );
    }
    if (hasValue(model.rawRatios.imageRatio)) {
      result.ImageRatio = toNormalizedNumber(model.rawRatios.imageRatio);
    }
    if (hasValue(model.rawRatios.audioRatio)) {
      result.AudioRatio = toNormalizedNumber(model.rawRatios.audioRatio);
    }
    if (hasValue(model.rawRatios.audioCompletionRatio)) {
      result.AudioCompletionRatio = toNormalizedNumber(
        model.rawRatios.audioCompletionRatio,
      );
    }
    return result;
  }

  result.ModelRatio = toNormalizedNumber(inputPrice / 2);

  if (!model.completionRatioLocked && completionPrice !== null) {
    result.CompletionRatio = toNormalizedNumber(completionPrice / inputPrice);
  } else if (
    model.completionRatioLocked &&
    hasValue(model.rawRatios.completionRatio)
  ) {
    result.CompletionRatio = toNormalizedNumber(
      model.rawRatios.completionRatio,
    );
  }
  if (cachePrice !== null) {
    result.CacheRatio = toNormalizedNumber(cachePrice / inputPrice);
  }
  if (createCachePrice !== null) {
    result.CreateCacheRatio = toNormalizedNumber(createCachePrice / inputPrice);
  }
  if (imagePrice !== null) {
    result.ImageRatio = toNormalizedNumber(imagePrice / inputPrice);
  }
  if (audioInputPrice !== null) {
    result.AudioRatio = toNormalizedNumber(audioInputPrice / inputPrice);
  }
  if (audioOutputPrice !== null) {
    if (audioInputPrice === null || audioInputPrice === 0) {
      throw new Error(
        t('模型 {{name}} 缺少音频输入价格，无法计算音频补全倍率', {
          name: model.name,
        }),
      );
    }
    result.AudioCompletionRatio = toNormalizedNumber(
      audioOutputPrice / audioInputPrice,
    );
  }

  return result;
};

export const buildPreviewRows = (model, t) => {
  if (!model) return [];
  const finalBillingExpr = combineBillingExpr(
    model.billingExpr,
    model.requestRuleExpr,
  );

  if (model.billingMode === 'tiered_expr') {
    const rows = [
      {
        key: 'BillingMode',
        label: 'ModelBillingMode',
        value: 'tiered_expr',
      },
    ];
    if (finalBillingExpr) {
      const tierCount = (model.billingExpr.match(/tier\(/g) || []).length;
      rows.push({
        key: 'BillingExpr',
        label: 'ModelBillingExpr',
        value:
          tierCount > 0
            ? `${tierCount} ${t('档')} — ${
                finalBillingExpr.length > 60
                  ? finalBillingExpr.slice(0, 60) + '...'
                  : finalBillingExpr
              }`
            : finalBillingExpr.length > 60
              ? finalBillingExpr.slice(0, 60) + '...'
              : finalBillingExpr,
      });
    }
    return rows;
  }

  if (model.billingMode === 'per-request') {
    const rows = [
      {
        key: 'ModelPrice',
        label: 'ModelPrice',
        value: hasValue(model.fixedPrice) ? model.fixedPrice : t('空'),
      },
    ];
    return rows;
  }

  const inputPrice = toNumberOrNull(model.inputPrice);
  if (inputPrice === null) {
    const rows = [
      {
        key: 'ModelRatio',
        label: 'ModelRatio',
        value: hasValue(model.rawRatios.modelRatio)
          ? model.rawRatios.modelRatio
          : t('空'),
      },
      {
        key: 'CompletionRatio',
        label: 'CompletionRatio',
        value: hasValue(model.rawRatios.completionRatio)
          ? model.rawRatios.completionRatio
          : t('空'),
      },
      {
        key: 'CacheRatio',
        label: 'CacheRatio',
        value: hasValue(model.rawRatios.cacheRatio)
          ? model.rawRatios.cacheRatio
          : t('空'),
      },
      {
        key: 'CreateCacheRatio',
        label: 'CreateCacheRatio',
        value: hasValue(model.rawRatios.createCacheRatio)
          ? model.rawRatios.createCacheRatio
          : t('空'),
      },
      {
        key: 'ImageRatio',
        label: 'ImageRatio',
        value: hasValue(model.rawRatios.imageRatio)
          ? model.rawRatios.imageRatio
          : t('空'),
      },
      {
        key: 'AudioRatio',
        label: 'AudioRatio',
        value: hasValue(model.rawRatios.audioRatio)
          ? model.rawRatios.audioRatio
          : t('空'),
      },
      {
        key: 'AudioCompletionRatio',
        label: 'AudioCompletionRatio',
        value: hasValue(model.rawRatios.audioCompletionRatio)
          ? model.rawRatios.audioCompletionRatio
          : t('空'),
      },
    ];
    return rows;
  }

  const completionPrice = toNumberOrNull(model.completionPrice);
  const cachePrice = toNumberOrNull(model.cachePrice);
  const createCachePrice = toNumberOrNull(model.createCachePrice);
  const imagePrice = toNumberOrNull(model.imagePrice);
  const audioInputPrice = toNumberOrNull(model.audioInputPrice);
  const audioOutputPrice = toNumberOrNull(model.audioOutputPrice);

  const rows = [
    {
      key: 'ModelRatio',
      label: 'ModelRatio',
      value: formatNumber(inputPrice / 2),
    },
    {
      key: 'CompletionRatio',
      label: 'CompletionRatio',
      value: model.completionRatioLocked
        ? `${model.lockedCompletionRatio || t('空')} (${t('后端固定')})`
        : completionPrice !== null
          ? formatNumber(completionPrice / inputPrice)
          : t('空'),
    },
    {
      key: 'CacheRatio',
      label: 'CacheRatio',
      value:
        cachePrice !== null ? formatNumber(cachePrice / inputPrice) : t('空'),
    },
    {
      key: 'CreateCacheRatio',
      label: 'CreateCacheRatio',
      value:
        createCachePrice !== null
          ? formatNumber(createCachePrice / inputPrice)
          : t('空'),
    },
    {
      key: 'ImageRatio',
      label: 'ImageRatio',
      value:
        imagePrice !== null ? formatNumber(imagePrice / inputPrice) : t('空'),
    },
    {
      key: 'AudioRatio',
      label: 'AudioRatio',
      value:
        audioInputPrice !== null
          ? formatNumber(audioInputPrice / inputPrice)
          : t('空'),
    },
    {
      key: 'AudioCompletionRatio',
      label: 'AudioCompletionRatio',
      value:
        audioOutputPrice !== null &&
        audioInputPrice !== null &&
        audioInputPrice !== 0
          ? formatNumber(audioOutputPrice / audioInputPrice)
          : t('空'),
    },
  ];
  return rows;
};

export function useModelPricingEditorState({
  options,
  refresh,
  t,
  candidateModelNames = EMPTY_CANDIDATE_MODEL_NAMES,
  filterMode = 'all',
  selectedGroup = 'global', // 新增：当前选中的分组
}) {
  const [models, setModels] = useState([]);
  const [initialVisibleModelNames, setInitialVisibleModelNames] = useState([]);
  const [selectedModelName, setSelectedModelName] = useState('');
  const [selectedModelNames, setSelectedModelNames] = useState([]);
  const [searchText, setSearchText] = useState('');
  const [currentPage, setCurrentPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [conflictOnly, setConflictOnly] = useState(false);
  const [optionalFieldToggles, setOptionalFieldToggles] = useState({});
  // 单个模型在上游的全部报价，按模型名缓存
  const [upstreamCandidates, setUpstreamCandidates] = useState({});
  const [upstreamLoading, setUpstreamLoading] = useState(false);
  // 价格来源：llm-metadata 官方精选，models.dev 全量
  const [pricingUpstream, setPricingUpstream] = useState('llm-metadata');

  // 根据选中的分组确定数据源
  const getSourceMaps = useCallback(() => {
    if (selectedGroup === 'global') {
      // 全局配置
      return {
        ModelPrice: parseOptionJSON(options.ModelPrice),
        ModelRatio: parseOptionJSON(options.ModelRatio),
        CompletionRatio: parseOptionJSON(options.CompletionRatio),
        CompletionRatioMeta: parseOptionJSON(options.CompletionRatioMeta),
        CacheRatio: parseOptionJSON(options.CacheRatio),
        CreateCacheRatio: parseOptionJSON(options.CreateCacheRatio),
        ImageRatio: parseOptionJSON(options.ImageRatio),
        AudioRatio: parseOptionJSON(options.AudioRatio),
        AudioCompletionRatio: parseOptionJSON(options.AudioCompletionRatio),
        ModelBillingMode: parseOptionJSON(
          options['billing_setting.billing_mode'],
        ),
        ModelBillingExpr: parseOptionJSON(
          options['billing_setting.billing_expr'],
        ),
      };
    } else {
      // 分组级别配置
      const groupModelPrice = parseOptionJSON(options.GroupModelPrice);
      const groupModelRatio = parseOptionJSON(options.GroupModelRatio);
      const groupCompletionRatio = parseOptionJSON(
        options.GroupCompletionRatio,
      );
      const groupCacheRatio = parseOptionJSON(options.GroupCacheRatio);
      const groupCreateCacheRatio = parseOptionJSON(
        options.GroupCreateCacheRatio,
      );
      const groupImageRatio = parseOptionJSON(options.GroupImageRatio);
      const groupAudioRatio = parseOptionJSON(options.GroupAudioRatio);
      const groupAudioCompletionRatio = parseOptionJSON(
        options.GroupAudioCompletionRatio,
      );
      const groupBillingMode = parseOptionJSON(options.GroupBillingMode);
      const groupBillingExpr = parseOptionJSON(options.GroupBillingExpr);

      return {
        ModelPrice: groupModelPrice[selectedGroup] || {},
        ModelRatio: groupModelRatio[selectedGroup] || {},
        CompletionRatio: groupCompletionRatio[selectedGroup] || {},
        CompletionRatioMeta: {}, // 分组级别没有 meta
        CacheRatio: groupCacheRatio[selectedGroup] || {},
        CreateCacheRatio: groupCreateCacheRatio[selectedGroup] || {},
        ImageRatio: groupImageRatio[selectedGroup] || {},
        AudioRatio: groupAudioRatio[selectedGroup] || {},
        AudioCompletionRatio: groupAudioCompletionRatio[selectedGroup] || {},
        ModelBillingMode: groupBillingMode[selectedGroup] || {},
        ModelBillingExpr: groupBillingExpr[selectedGroup] || {},
      };
    }
  }, [options, selectedGroup]);

  useEffect(() => {
    const sourceMaps = getSourceMaps();

    const names = new Set([
      ...candidateModelNames,
      ...Object.keys(sourceMaps.ModelPrice),
      ...Object.keys(sourceMaps.ModelRatio),
      ...Object.keys(sourceMaps.CompletionRatio),
      ...Object.keys(sourceMaps.CompletionRatioMeta),
      ...Object.keys(sourceMaps.CacheRatio),
      ...Object.keys(sourceMaps.CreateCacheRatio),
      ...Object.keys(sourceMaps.ImageRatio),
      ...Object.keys(sourceMaps.AudioRatio),
      ...Object.keys(sourceMaps.AudioCompletionRatio),
      ...Object.keys(sourceMaps.ModelBillingMode),
      ...Object.keys(sourceMaps.ModelBillingExpr),
    ]);

    const nextModels = Array.from(names)
      .map((name) => buildModelState(name, sourceMaps))
      .sort((a, b) => a.name.localeCompare(b.name));

    setModels(nextModels);
    setInitialVisibleModelNames(
      filterMode === 'unset'
        ? nextModels
            .filter((model) => isBasePricingUnset(model))
            .map((model) => model.name)
        : nextModels.map((model) => model.name),
    );
    setOptionalFieldToggles(
      nextModels.reduce((acc, model) => {
        acc[model.name] = buildOptionalFieldToggles(model);
        return acc;
      }, {}),
    );
    setSelectedModelName((previous) => {
      if (previous && nextModels.some((model) => model.name === previous)) {
        return previous;
      }
      const nextVisibleModels =
        filterMode === 'unset'
          ? nextModels.filter((model) => isBasePricingUnset(model))
          : nextModels;
      return nextVisibleModels[0]?.name || '';
    });
  }, [candidateModelNames, filterMode, options, selectedGroup, getSourceMaps]);

  const visibleModels = useMemo(() => {
    return filterMode === 'unset'
      ? models.filter((model) => initialVisibleModelNames.includes(model.name))
      : models;
  }, [filterMode, initialVisibleModelNames, models]);

  const filteredModels = useMemo(() => {
    return visibleModels.filter((model) => {
      const keyword = searchText.trim().toLowerCase();
      const keywordMatch = keyword
        ? model.name.toLowerCase().includes(keyword)
        : true;
      const conflictMatch = conflictOnly ? model.hasConflict : true;
      return keywordMatch && conflictMatch;
    });
  }, [conflictOnly, searchText, visibleModels]);

  const pagedData = useMemo(() => {
    const start = (currentPage - 1) * PAGE_SIZE;
    return filteredModels.slice(start, start + PAGE_SIZE);
  }, [currentPage, filteredModels]);

  const selectedModel = useMemo(
    () =>
      visibleModels.find((model) => model.name === selectedModelName) || null,
    [selectedModelName, visibleModels],
  );

  const selectedWarnings = useMemo(
    () => getModelWarnings(selectedModel, t),
    [selectedModel, t],
  );

  const previewRows = useMemo(
    () => buildPreviewRows(selectedModel, t),
    [selectedModel, t],
  );

  useEffect(() => {
    setCurrentPage(1);
  }, [searchText, conflictOnly, filterMode, candidateModelNames]);

  useEffect(() => {
    setSelectedModelNames((previous) =>
      previous.filter((name) =>
        visibleModels.some((model) => model.name === name),
      ),
    );
  }, [visibleModels]);

  useEffect(() => {
    if (visibleModels.length === 0) {
      setSelectedModelName('');
      return;
    }
    if (!visibleModels.some((model) => model.name === selectedModelName)) {
      setSelectedModelName(visibleModels[0].name);
    }
  }, [selectedModelName, visibleModels]);

  const upsertModel = (name, updater) => {
    setModels((previous) =>
      previous.map((model) => {
        if (model.name !== name) return model;
        return typeof updater === 'function' ? updater(model) : updater;
      }),
    );
  };

  const isOptionalFieldEnabled = (model, field) => {
    if (!model) return false;
    const modelToggles = optionalFieldToggles[model.name];
    if (modelToggles && typeof modelToggles[field] === 'boolean') {
      return modelToggles[field];
    }
    return buildOptionalFieldToggles(model)[field];
  };

  const updateOptionalFieldToggle = (modelName, field, checked) => {
    setOptionalFieldToggles((prev) => ({
      ...prev,
      [modelName]: {
        ...(prev[modelName] || {}),
        [field]: checked,
      },
    }));
  };

  const handleOptionalFieldToggle = (field, checked) => {
    if (!selectedModel) return;

    updateOptionalFieldToggle(selectedModel.name, field, checked);

    if (checked) {
      return;
    }

    upsertModel(selectedModel.name, (model) => {
      const nextModel = { ...model, [field]: '' };

      if (field === 'audioInputPrice') {
        nextModel.audioOutputPrice = '';
        setOptionalFieldToggles((prev) => ({
          ...prev,
          [selectedModel.name]: {
            ...(prev[selectedModel.name] || {}),
            audioInputPrice: false,
            audioOutputPrice: false,
          },
        }));
      }

      return nextModel;
    });
  };

  const fillDerivedPricesFromBase = (model, nextInputPrice) => {
    const baseNumber = toNumberOrNull(nextInputPrice);
    if (baseNumber === null) {
      return model;
    }

    return {
      ...model,
      completionPrice:
        model.completionRatioLocked && hasValue(model.lockedCompletionRatio)
          ? formatNumber(baseNumber * Number(model.lockedCompletionRatio))
          : !hasValue(model.completionPrice) &&
              hasValue(model.rawRatios.completionRatio)
            ? formatNumber(baseNumber * Number(model.rawRatios.completionRatio))
            : model.completionPrice,
      cachePrice:
        !hasValue(model.cachePrice) && hasValue(model.rawRatios.cacheRatio)
          ? formatNumber(baseNumber * Number(model.rawRatios.cacheRatio))
          : model.cachePrice,
      createCachePrice:
        !hasValue(model.createCachePrice) &&
        hasValue(model.rawRatios.createCacheRatio)
          ? formatNumber(baseNumber * Number(model.rawRatios.createCacheRatio))
          : model.createCachePrice,
      imagePrice:
        !hasValue(model.imagePrice) && hasValue(model.rawRatios.imageRatio)
          ? formatNumber(baseNumber * Number(model.rawRatios.imageRatio))
          : model.imagePrice,
      audioInputPrice:
        !hasValue(model.audioInputPrice) && hasValue(model.rawRatios.audioRatio)
          ? formatNumber(baseNumber * Number(model.rawRatios.audioRatio))
          : model.audioInputPrice,
      audioOutputPrice:
        !hasValue(model.audioOutputPrice) &&
        hasValue(model.rawRatios.audioRatio) &&
        hasValue(model.rawRatios.audioCompletionRatio)
          ? formatNumber(
              baseNumber *
                Number(model.rawRatios.audioRatio) *
                Number(model.rawRatios.audioCompletionRatio),
            )
          : model.audioOutputPrice,
    };
  };

  const handleNumericFieldChange = (field, value) => {
    if (!selectedModel || !NUMERIC_INPUT_REGEX.test(value)) {
      return;
    }

    upsertModel(selectedModel.name, (model) => {
      const updatedModel = { ...model, [field]: value };

      if (field === 'inputPrice') {
        return fillDerivedPricesFromBase(updatedModel, value);
      }

      return updatedModel;
    });
  };

  const handleBillingModeChange = (value) => {
    if (!selectedModel) return;
    upsertModel(selectedModel.name, (model) => {
      const next = { ...model, billingMode: value };
      if (value === 'tiered_expr' && !model.billingExpr) {
        next.billingExpr = 'tier("base", p * 0 + c * 0)';
      }
      return next;
    });
  };

  const handleBillingExprChange = (newExpr) => {
    if (!selectedModel) return;
    upsertModel(selectedModel.name, (model) => ({
      ...model,
      billingExpr: newExpr,
    }));
  };

  const handleRequestRuleExprChange = (newExpr) => {
    if (!selectedModel) return;
    upsertModel(selectedModel.name, (model) => ({
      ...model,
      requestRuleExpr: newExpr,
    }));
  };

  const addModel = (modelName) => {
    const trimmedName = modelName.trim();
    if (!trimmedName) {
      showError(t('请输入模型名称'));
      return false;
    }
    if (models.some((model) => model.name === trimmedName)) {
      showError(t('模型名称已存在'));
      return false;
    }

    const nextModel = {
      ...EMPTY_MODEL,
      name: trimmedName,
      rawRatios: { ...EMPTY_MODEL.rawRatios },
    };

    setModels((previous) => [nextModel, ...previous]);
    setOptionalFieldToggles((prev) => ({
      ...prev,
      [trimmedName]: buildOptionalFieldToggles(nextModel),
    }));
    setSelectedModelName(trimmedName);
    setCurrentPage(1);
    return true;
  };

  const deleteModel = (name) => {
    const nextModels = models.filter((model) => model.name !== name);
    setModels(nextModels);
    setOptionalFieldToggles((prev) => {
      const next = { ...prev };
      delete next[name];
      return next;
    });
    setSelectedModelNames((previous) =>
      previous.filter((item) => item !== name),
    );
    if (selectedModelName === name) {
      setSelectedModelName(nextModels[0]?.name || '');
    }
  };

  const applySelectedModelPricing = () => {
    if (!selectedModel) {
      showError(t('请先选择一个作为模板的模型'));
      return false;
    }
    if (selectedModelNames.length === 0) {
      showError(t('请先勾选需要批量设置的模型'));
      return false;
    }

    const sourceToggles = optionalFieldToggles[selectedModel.name] || {};

    setModels((previous) =>
      previous.map((model) => {
        if (!selectedModelNames.includes(model.name)) {
          return model;
        }

        const nextModel = {
          ...model,
          billingMode: selectedModel.billingMode,
          fixedPrice: selectedModel.fixedPrice,
          inputPrice: selectedModel.inputPrice,
          completionPrice: selectedModel.completionPrice,
          cachePrice: selectedModel.cachePrice,
          createCachePrice: selectedModel.createCachePrice,
          imagePrice: selectedModel.imagePrice,
          audioInputPrice: selectedModel.audioInputPrice,
          audioOutputPrice: selectedModel.audioOutputPrice,
          billingExpr: selectedModel.billingExpr || '',
          requestRuleExpr: selectedModel.requestRuleExpr || '',
        };

        if (
          nextModel.billingMode === 'per-token' &&
          nextModel.completionRatioLocked &&
          hasValue(nextModel.inputPrice) &&
          hasValue(nextModel.lockedCompletionRatio)
        ) {
          nextModel.completionPrice = formatNumber(
            Number(nextModel.inputPrice) *
              Number(nextModel.lockedCompletionRatio),
          );
        }

        return nextModel;
      }),
    );

    setOptionalFieldToggles((previous) => {
      const next = { ...previous };
      selectedModelNames.forEach((modelName) => {
        const targetModel = models.find((item) => item.name === modelName);
        next[modelName] = {
          completionPrice: targetModel?.completionRatioLocked
            ? true
            : Boolean(sourceToggles.completionPrice),
          cachePrice: Boolean(sourceToggles.cachePrice),
          createCachePrice: Boolean(sourceToggles.createCachePrice),
          imagePrice: Boolean(sourceToggles.imagePrice),
          audioInputPrice: Boolean(sourceToggles.audioInputPrice),
          audioOutputPrice:
            Boolean(sourceToggles.audioInputPrice) &&
            Boolean(sourceToggles.audioOutputPrice),
        };
      });
      return next;
    });

    showSuccess(
      t('已将模型 {{name}} 的价格配置批量应用到 {{count}} 个模型', {
        name: selectedModel.name,
        count: selectedModelNames.length,
      }),
    );
    return true;
  };

  const handleSubmit = async () => {
    setLoading(true);
    try {
      // 根据选中的分组确定输出的 key 前缀
      const isGroupMode = selectedGroup !== 'global';
      const groupPrefix = isGroupMode ? 'Group' : '';

      const output = {
        [`${groupPrefix}ModelPrice`]: {},
        [`${groupPrefix}ModelRatio`]: {},
        [`${groupPrefix}CompletionRatio`]: {},
        [`${groupPrefix}CacheRatio`]: {},
        [`${groupPrefix}CreateCacheRatio`]: {},
        [`${groupPrefix}ImageRatio`]: {},
        [`${groupPrefix}AudioRatio`]: {},
        [`${groupPrefix}AudioCompletionRatio`]: {},
      };

      const tieredOutput = {};
      if (isGroupMode) {
        tieredOutput['GroupBillingMode'] = {};
        tieredOutput['GroupBillingExpr'] = {};
      } else {
        tieredOutput['billing_setting.billing_mode'] = {};
        tieredOutput['billing_setting.billing_expr'] = {};
      }

      const groupLeafOutput = isGroupMode
        ? {
            GroupModelPrice: {},
            GroupModelRatio: {},
            GroupCompletionRatio: {},
            GroupCacheRatio: {},
            GroupCreateCacheRatio: {},
            GroupImageRatio: {},
            GroupAudioRatio: {},
            GroupAudioCompletionRatio: {},
          }
        : null;
      const groupBillingModeLeaf = isGroupMode ? {} : null;
      const groupBillingExprLeaf = isGroupMode ? {} : null;

      for (const model of models) {
        if (model.billingMode === 'tiered_expr') {
          const finalBillingExpr = combineBillingExpr(
            model.billingExpr,
            model.requestRuleExpr,
          );
          if (!finalBillingExpr) {
            throw new Error(`${model.name}: ${t('表达式错误')}`);
          }
          if (isGroupMode) {
            groupBillingModeLeaf[model.name] = 'tiered_expr';
            groupBillingExprLeaf[model.name] = finalBillingExpr;
          } else {
            tieredOutput['billing_setting.billing_mode'][model.name] =
              'tiered_expr';
            tieredOutput['billing_setting.billing_expr'][model.name] =
              finalBillingExpr;
          }
        }

        // 序列化价格/倍率值
        try {
          const serialized = serializeModel(model, t);
          const hasSerializedValue = Object.values(serialized).some(
            (value) => value !== null,
          );

          if (
            isGroupMode &&
            hasSerializedValue &&
            model.billingMode !== 'tiered_expr'
          ) {
            groupBillingModeLeaf[model.name] =
              model.billingMode === 'per-request' ? 'per-request' : 'per-token';
          }

          Object.entries(serialized).forEach(([key, value]) => {
            if (value !== null) {
              const outputKey = `${groupPrefix}${key}`;
              if (isGroupMode) {
                groupLeafOutput[outputKey][model.name] = value;
              } else {
                output[outputKey][model.name] = value;
              }
            }
          });
        } catch (e) {
          if (model.billingMode !== 'tiered_expr') {
            throw e;
          }
        }
      }

      if (isGroupMode) {
        Object.keys(groupLeafOutput).forEach((key) => {
          const existingData = parseOptionJSON(options[key]);
          if (Object.keys(groupLeafOutput[key]).length === 0) {
            delete existingData[selectedGroup];
          } else {
            existingData[selectedGroup] = groupLeafOutput[key];
          }
          output[key] = existingData;
        });

        const existingModes = parseOptionJSON(options.GroupBillingMode);
        if (Object.keys(groupBillingModeLeaf).length === 0) {
          delete existingModes[selectedGroup];
        } else {
          existingModes[selectedGroup] = groupBillingModeLeaf;
        }
        tieredOutput['GroupBillingMode'] = existingModes;

        const existingExprs = parseOptionJSON(options.GroupBillingExpr);
        if (Object.keys(groupBillingExprLeaf).length === 0) {
          delete existingExprs[selectedGroup];
        } else {
          existingExprs[selectedGroup] = groupBillingExprLeaf;
        }
        tieredOutput['GroupBillingExpr'] = existingExprs;
      }

      const res = await API.put('/api/option/pricing', {
        options: Object.fromEntries(
          Object.entries({ ...output, ...tieredOutput }).map(([key, value]) => [
            key,
            JSON.stringify(value),
          ]),
        ),
      });
      if (!res?.data?.success) {
        throw new Error(res?.data?.message || t('保存失败，请重试'));
      }

      showSuccess(t('保存成功'));
      await refresh();
    } catch (error) {
      console.error('保存失败:', error);
      showError(error.message || t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  };

  return {
    models,
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
  };

  // 切换上游后，已缓存的候选来自另一个数据源，不能继续用
  function changePricingUpstream(nextUpstream) {
    setPricingUpstream(nextUpstream);
    setUpstreamCandidates({});
  }

  // 拉取单个模型在上游的全部报价，供用户挑提供商
  async function loadUpstreamCandidates(modelName) {
    if (!modelName) return false;

    setUpstreamLoading(true);
    try {
      const res = await API.post('/api/ratio_sync/fetch', {
        timeout: 15,
        model_names: [modelName],
        upstream: pricingUpstream,
      });
      if (!res.data.success) {
        showError(
          res.data.message ||
            t('获取 {{upstream}} 价格失败', { upstream: pricingUpstream }),
        );
        return false;
      }

      const candidates = res.data.data?.candidates?.[modelName] || [];
      setUpstreamCandidates((previous) => ({
        ...previous,
        [modelName]: candidates,
      }));
      if (candidates.length === 0) {
        showWarning(
          t('{{upstream}} 上没有收录 {{model}}', {
            upstream: pricingUpstream,
            model: modelName,
          }),
        );
      }
      return true;
    } catch (error) {
      console.error('获取上游价格失败:', error);
      showError(
        error.message ||
          t('获取 {{upstream}} 价格失败', { upstream: pricingUpstream }),
      );
      return false;
    } finally {
      setUpstreamLoading(false);
    }
  }

  // 把选中提供商的报价填入 modelName 的表单。lookupName 是上游那边的模型名，
  // 与 modelName 不同时相当于"借用另一个模型的价格"，用于重定向过的模型。
  function applyUpstreamCandidate(modelName, provider, options = {}) {
    const { lookupName = modelName } = options;
    const candidate = (upstreamCandidates[lookupName] || []).find(
      (item) => item.provider === provider,
    );
    if (!candidate) return false;

    const model = models.find((item) => item.name === modelName);
    if (!model) return false;

    // 计费方式跟随上游：来源带阶梯/时段表达式就用表达式计费，否则按量计费。
    // 传入 useTiered 可显式覆盖这个判断。
    const useTiered = options.useTiered ?? Boolean(candidate.billing_expr);

    if (useTiered) {
      if (!candidate.billing_expr) return false;
      upsertModel(modelName, (previous) => ({
        ...previous,
        billingMode: 'tiered_expr',
        billingExpr: candidate.billing_expr,
        requestRuleExpr: '',
        fixedPrice: '',
      }));
      showSuccess(
        t('已填入 {{provider}} 的上下文阶梯价，确认后点击保存生效', {
          provider,
        }),
      );
      return true;
    }

    const inputPriceNumber = Number(candidate.model_ratio) * 2;
    if (!Number.isFinite(inputPriceNumber)) return false;

    const completionRatio =
      model.completionRatioLocked && hasValue(model.lockedCompletionRatio)
        ? Number(model.lockedCompletionRatio)
        : toNumberOrNull(candidate.completion_ratio);
    const cacheRatio = toNumberOrNull(candidate.cache_ratio);
    const createCacheRatio = toNumberOrNull(candidate.create_cache_ratio);
    const audioRatio = toNumberOrNull(candidate.audio_ratio);
    const audioCompletionRatio = toNumberOrNull(
      candidate.audio_completion_ratio,
    );
    const audioInputPriceNumber =
      audioRatio !== null ? inputPriceNumber * audioRatio : null;

    upsertModel(modelName, (previous) => ({
      ...previous,
      billingMode: 'per-token',
      fixedPrice: '',
      inputPrice: formatNumber(inputPriceNumber),
      completionPrice:
        completionRatio !== null
          ? formatNumber(inputPriceNumber * completionRatio)
          : previous.completionPrice,
      cachePrice:
        cacheRatio !== null
          ? formatNumber(inputPriceNumber * cacheRatio)
          : previous.cachePrice,
      createCachePrice:
        createCacheRatio !== null
          ? formatNumber(inputPriceNumber * createCacheRatio)
          : previous.createCachePrice,
      audioInputPrice:
        audioInputPriceNumber !== null
          ? formatNumber(audioInputPriceNumber)
          : previous.audioInputPrice,
      audioOutputPrice:
        audioInputPriceNumber !== null && audioCompletionRatio !== null
          ? formatNumber(audioInputPriceNumber * audioCompletionRatio)
          : previous.audioOutputPrice,
    }));

    setOptionalFieldToggles((previous) => {
      const current = previous[modelName] || {};
      return {
        ...previous,
        [modelName]: {
          ...current,
          completionPrice:
            completionRatio !== null || Boolean(current.completionPrice),
          cachePrice: cacheRatio !== null || Boolean(current.cachePrice),
          createCachePrice:
            createCacheRatio !== null || Boolean(current.createCachePrice),
          audioInputPrice:
            audioRatio !== null || Boolean(current.audioInputPrice),
          audioOutputPrice:
            (audioRatio !== null && audioCompletionRatio !== null) ||
            Boolean(current.audioOutputPrice),
        },
      };
    });

    showSuccess(
      candidate.official
        ? t('已填入 {{provider}} 的价格，确认后点击保存生效', { provider })
        : t('已填入第三方来源 {{provider}} 的价格，请自行核对后保存', {
            provider,
          }),
    );
    return true;
  }

  // 清空当前分组的全部定价配置，清空后该分组回落到全局定价
  async function clearGroupPricing() {
    if (selectedGroup === 'global') {
      showError(t('全局配置不支持清空'));
      return false;
    }

    setLoading(true);
    try {
      const res = await API.put('/api/option/pricing', {
        options: Object.fromEntries(
          GROUP_PRICING_OPTION_KEYS.map((key) => {
            const existing = parseOptionJSON(options[key]);
            delete existing[selectedGroup];
            return [key, JSON.stringify(existing)];
          }),
        ),
      });
      if (!res?.data?.success) {
        throw new Error(res?.data?.message || t('清空失败，请重试'));
      }

      setSelectedModelNames([]);
      showSuccess(t('已清空该分组的定价配置，该分组将回落到全局定价'));
      await refresh();
      return true;
    } catch (error) {
      console.error('清空分组配置失败:', error);
      showError(error.message || t('清空失败，请重试'));
      return false;
    } finally {
      setLoading(false);
    }
  }

  // 用上游价格填充编辑器，填充后仍需用户点击保存才会写入
  async function fillPricingFromUpstream(targetModelNames) {
    const names =
      targetModelNames && targetModelNames.length > 0
        ? targetModelNames
        : filteredModels.map((model) => model.name);
    if (names.length === 0) {
      showError(t('当前没有可同步的模型'));
      return false;
    }

    setLoading(true);
    try {
      const res = await API.post('/api/ratio_sync/fetch', {
        timeout: 15,
        model_names: names,
        upstream: pricingUpstream,
      });
      if (!res.data.success) {
        showError(
          res.data.message ||
            t('获取 {{upstream}} 价格失败', { upstream: pricingUpstream }),
        );
        return false;
      }

      const differences = res.data.data?.differences || {};
      const upstreamSources = res.data.data?.sources || {};
      const targetNameSet = new Set(names);
      const pricePatches = new Map();
      let skippedTieredCount = 0;
      let tieredCount = 0;

      for (const model of models) {
        if (!targetNameSet.has(model.name)) continue;
        const diff = differences[model.name];
        if (!diff) continue;

        // 上游给了上下文阶梯价时按表达式计费填充，倍率不再适用
        const upstreamExpr = diff.billing_expr?.upstream;
        if (typeof upstreamExpr === 'string' && upstreamExpr !== '') {
          tieredCount += 1;
          pricePatches.set(model.name, {
            billingMode: 'tiered_expr',
            billingExpr: upstreamExpr,
            requestRuleExpr: '',
            fixedPrice: '',
          });
          continue;
        }

        if (model.billingMode === 'tiered_expr') {
          skippedTieredCount += 1;
          continue;
        }

        const upstreamModelRatio = toNumberOrNull(diff.model_ratio?.upstream);
        const inputPriceNumber =
          upstreamModelRatio !== null
            ? upstreamModelRatio * 2
            : toNumberOrNull(model.inputPrice);
        if (inputPriceNumber === null) continue;

        const ratioOf = (field, localRatio) =>
          toNumberOrNull(diff[field]?.upstream) ?? toNumberOrNull(localRatio);
        const completionRatio = ratioOf(
          'completion_ratio',
          model.rawRatios.completionRatio,
        );
        const cacheRatio = ratioOf('cache_ratio', model.rawRatios.cacheRatio);
        const createCacheRatio = ratioOf(
          'create_cache_ratio',
          model.rawRatios.createCacheRatio,
        );
        const audioRatio = ratioOf('audio_ratio', model.rawRatios.audioRatio);
        const audioCompletionRatio = ratioOf(
          'audio_completion_ratio',
          model.rawRatios.audioCompletionRatio,
        );
        const audioInputPriceNumber =
          audioRatio !== null ? inputPriceNumber * audioRatio : null;

        pricePatches.set(model.name, {
          billingMode: 'per-token',
          fixedPrice: '',
          inputPrice: formatNumber(inputPriceNumber),
          completionPrice:
            model.completionRatioLocked && hasValue(model.lockedCompletionRatio)
              ? formatNumber(
                  inputPriceNumber * Number(model.lockedCompletionRatio),
                )
              : completionRatio !== null
                ? formatNumber(inputPriceNumber * completionRatio)
                : model.completionPrice,
          cachePrice:
            cacheRatio !== null
              ? formatNumber(inputPriceNumber * cacheRatio)
              : model.cachePrice,
          createCachePrice:
            createCacheRatio !== null
              ? formatNumber(inputPriceNumber * createCacheRatio)
              : model.createCachePrice,
          audioInputPrice:
            audioInputPriceNumber !== null
              ? formatNumber(audioInputPriceNumber)
              : model.audioInputPrice,
          audioOutputPrice:
            audioInputPriceNumber !== null && audioCompletionRatio !== null
              ? formatNumber(audioInputPriceNumber * audioCompletionRatio)
              : model.audioOutputPrice,
        });
      }

      const filledNames = [...pricePatches.keys()];

      if (filledNames.length > 0) {
        setModels((previous) =>
          previous.map((model) => {
            const patch = pricePatches.get(model.name);
            return patch ? { ...model, ...patch } : model;
          }),
        );
        setOptionalFieldToggles((previous) => {
          const next = { ...previous };
          filledNames.forEach((name) => {
            const diff = differences[name] || {};
            const current = next[name] || {};
            const filled = (field) => diff[field]?.upstream !== undefined;
            next[name] = {
              ...current,
              completionPrice:
                current.completionPrice || filled('completion_ratio'),
              cachePrice: current.cachePrice || filled('cache_ratio'),
              createCachePrice:
                current.createCachePrice || filled('create_cache_ratio'),
              audioInputPrice: current.audioInputPrice || filled('audio_ratio'),
              audioOutputPrice:
                current.audioOutputPrice ||
                (filled('audio_ratio') && filled('audio_completion_ratio')),
            };
          });
          return next;
        });
      }

      if (filledNames.length === 0) {
        showSuccess(
          skippedTieredCount > 0
            ? t(
                '{{upstream}} 没有可填充的新价格（已跳过 {{count}} 个表达式计费模型）',
                { upstream: pricingUpstream, count: skippedTieredCount },
              )
            : t('{{upstream}} 没有这些模型的新价格', {
                upstream: pricingUpstream,
              }),
        );
        return true;
      }

      showSuccess(
        skippedTieredCount > 0
          ? t(
              '已填充 {{count}} 个模型的价格（跳过 {{skipped}} 个表达式计费模型），确认后点击保存生效',
              { count: filledNames.length, skipped: skippedTieredCount },
            )
          : t('已填充 {{count}} 个模型的价格，确认后点击保存生效', {
              count: filledNames.length,
            }),
      );

      if (tieredCount > 0) {
        showWarning(
          t('其中 {{count}} 个模型上游有上下文阶梯价，已填充为表达式计费', {
            count: tieredCount,
          }),
        );
      }

      const thirdPartyCount = filledNames.filter(
        (name) => upstreamSources[name] && !upstreamSources[name].official,
      ).length;
      if (thirdPartyCount > 0) {
        showWarning(
          t(
            '其中 {{count}} 个模型 {{upstream}} 上没有官方条目，价格取自第三方转售商，请自行核对',
            { count: thirdPartyCount, upstream: pricingUpstream },
          ),
        );
      }
      return true;
    } catch (error) {
      console.error('同步上游价格失败:', error);
      showError(
        error.message ||
          t('获取 {{upstream}} 价格失败', { upstream: pricingUpstream }),
      );
      return false;
    } finally {
      setLoading(false);
    }
  }

  // 一键同步分组定价
  async function syncGroupPricing(
    targetGroups,
    modelNames = null,
    fromGlobal = false,
  ) {
    if (!fromGlobal && selectedGroup === 'global') {
      showError(t('全局配置无法同步，请先选择一个分组'));
      return false;
    }
    if (!targetGroups || targetGroups.length === 0) {
      showError(t('请选择目标分组'));
      return false;
    }

    setLoading(true);
    try {
      // 构建同步数据
      const syncData = {
        source_group: fromGlobal ? 'global' : selectedGroup,
        target_groups: targetGroups,
        from_global: fromGlobal,
      };

      // 如果指定了模型名称，添加到请求中
      if (modelNames && modelNames.length > 0) {
        syncData.model_names = modelNames;
      }

      const res = await API.post('/api/option/sync_group_pricing', syncData);
      if (res.data.success) {
        showSuccess(t('同步成功'));
        await refresh();
        return true;
      } else {
        showError(res.data.message || t('同步失败'));
        return false;
      }
    } catch (error) {
      console.error('同步失败:', error);
      showError(error.message || t('同步失败，请重试'));
      return false;
    } finally {
      setLoading(false);
    }
  }
}
