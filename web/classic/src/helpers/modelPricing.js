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

export const calculateModelPrice = ({
  record,
  selectedGroup,
  groupRatio,
  tokenUnit,
  displayPrice,
  currency,
  quotaDisplayType = 'USD',
  precision = 4,
  groupPricing = null, // 新增：分组定价数据
}) => {
  const modelKey = record?.model_name || record?.model || record?.name || '';
  const availableGroups = Array.isArray(record?.enable_groups)
    ? record.enable_groups
    : [];
  const hasRatioValue = (value) =>
    value !== undefined &&
    value !== null &&
    value !== '' &&
    Number.isFinite(Number(value));

  // 1. 获取分组级别的定价配置（如果有）
  const getGroupModelData = (groupName) => {
    if (!groupPricing || !groupName || groupName === 'all' || !modelKey) {
      return null;
    }

    const groupBillingMode =
      groupPricing.group_billing_mode?.[groupName]?.[modelKey] || null;
    const groupModelPrice =
      groupPricing.group_model_price?.[groupName]?.[modelKey] ?? null;
    const groupModelRatio =
      groupPricing.group_model_ratio?.[groupName]?.[modelKey] ?? null;
    const groupCompletionRatio =
      groupPricing.group_completion_ratio?.[groupName]?.[modelKey] ?? null;
    const groupCacheRatio =
      groupPricing.group_cache_ratio?.[groupName]?.[modelKey] ?? null;
    const groupCreateCacheRatio =
      groupPricing.group_create_cache_ratio?.[groupName]?.[modelKey] ?? null;
    const groupImageRatio =
      groupPricing.group_image_ratio?.[groupName]?.[modelKey] ?? null;
    const groupAudioRatio =
      groupPricing.group_audio_ratio?.[groupName]?.[modelKey] ?? null;
    const groupAudioCompletionRatio =
      groupPricing.group_audio_completion_ratio?.[groupName]?.[modelKey] ??
      null;
    const groupBillingExpr =
      groupPricing.group_billing_expr?.[groupName]?.[modelKey] || null;

    // 如果分组没有任何配置，返回 null
    if (
      !groupBillingMode &&
      !groupBillingExpr &&
      [
        groupModelPrice,
        groupModelRatio,
        groupCompletionRatio,
        groupCacheRatio,
        groupCreateCacheRatio,
        groupImageRatio,
        groupAudioRatio,
        groupAudioCompletionRatio,
      ].every((value) => value === null)
    ) {
      return null;
    }

    return {
      billingMode: groupBillingMode,
      modelPrice: groupModelPrice,
      modelRatio: groupModelRatio,
      completionRatio: groupCompletionRatio,
      cacheRatio: groupCacheRatio,
      createCacheRatio: groupCreateCacheRatio,
      imageRatio: groupImageRatio,
      audioRatio: groupAudioRatio,
      audioCompletionRatio: groupAudioCompletionRatio,
      billingExpr: groupBillingExpr,
    };
  };

  // 2. 选择实际使用的分组
  let usedGroup = selectedGroup;
  let usedGroupRatio = groupRatio[selectedGroup];
  let groupModelData =
    selectedGroup && selectedGroup !== 'all'
      ? getGroupModelData(selectedGroup)
      : null;

  if (selectedGroup === 'all' || usedGroupRatio === undefined) {
    let minScore = Number.POSITIVE_INFINITY;
    let fallbackGroup = availableGroups[0];
    let fallbackRatio = groupRatio[fallbackGroup];
    let fallbackGroupModelData = getGroupModelData(fallbackGroup);

    availableGroups.forEach((g) => {
      const currentRatio =
        groupRatio[g] !== undefined ? Number(groupRatio[g]) : 1;
      const currentGroupModelData = getGroupModelData(g);
      const currentBillingMode =
        currentGroupModelData?.billingMode || record.billing_mode;

      let score = currentRatio;

      if (
        currentBillingMode === 'tiered_expr' &&
        (currentGroupModelData?.billingExpr || record.billing_expr)
      ) {
        score = currentRatio;
      } else {
        const currentModelPrice =
          currentGroupModelData?.modelPrice ?? record.model_price;
        const currentModelRatio =
          currentGroupModelData?.modelRatio ?? record.model_ratio;
        const currentCompletionRatio =
          currentGroupModelData?.completionRatio ?? record.completion_ratio;
        const currentBillingTypeIsPerRequest =
          currentGroupModelData?.billingMode === 'per-request' ||
          (currentGroupModelData?.billingMode == null &&
            record.quota_type === 1) ||
          hasRatioValue(currentGroupModelData?.modelPrice);

        if (
          currentBillingTypeIsPerRequest &&
          hasRatioValue(currentModelPrice)
        ) {
          score = Number(currentModelPrice) * currentRatio;
        } else if (hasRatioValue(currentModelRatio)) {
          const completionMultiplier = hasRatioValue(currentCompletionRatio)
            ? Math.max(Number(currentCompletionRatio), 1)
            : 1;
          score =
            Number(currentModelRatio) * 2 * completionMultiplier * currentRatio;
        }
      }

      if (score < minScore) {
        minScore = score;
        usedGroup = g;
        usedGroupRatio = currentRatio;
        groupModelData = currentGroupModelData;
      }

      if (fallbackRatio === undefined) {
        fallbackGroup = g;
        fallbackRatio = currentRatio;
        fallbackGroupModelData = currentGroupModelData;
      }
    });

    if (!usedGroup && fallbackGroup) {
      usedGroup = fallbackGroup;
      usedGroupRatio = fallbackRatio;
      groupModelData = fallbackGroupModelData;
    }

    if (usedGroupRatio === undefined) {
      usedGroupRatio = 1;
    }
  }

  // 3. 动态计费（tiered_expr）
  const effectiveBillingExpr =
    groupModelData?.billingExpr || record.billing_expr;
  const effectiveBillingMode =
    groupModelData?.billingMode || record.billing_mode;

  if (effectiveBillingMode === 'tiered_expr' && effectiveBillingExpr) {
    return {
      isDynamicPricing: true,
      billingMode: effectiveBillingMode,
      billingExpr: effectiveBillingExpr,
      usedGroup,
      usedGroupRatio,
    };
  }

  // 4. 确定使用的价格/倍率（分组优先，否则使用全局）
  const effectiveModelPrice = groupModelData?.modelPrice ?? record.model_price;
  const effectiveModelRatio = groupModelData?.modelRatio ?? record.model_ratio;
  const effectiveCompletionRatio =
    groupModelData?.completionRatio ?? record.completion_ratio;
  const effectiveCacheRatio = groupModelData?.cacheRatio ?? record.cache_ratio;
  const effectiveCreateCacheRatio =
    groupModelData?.createCacheRatio ?? record.create_cache_ratio;
  const effectiveImageRatio = groupModelData?.imageRatio ?? record.image_ratio;
  const effectiveAudioRatio = groupModelData?.audioRatio ?? record.audio_ratio;
  const effectiveAudioCompletionRatio =
    groupModelData?.audioCompletionRatio ?? record.audio_completion_ratio;

  // 5. 确定计费类型
  // 如果分组配置了 per-request 模式或者有 model_price，则按次计费
  const isPerRequest =
    groupModelData?.billingMode === 'per-request' ||
    (groupModelData?.billingMode == null && record.quota_type === 1) ||
    (groupModelData?.modelPrice !== undefined &&
      groupModelData?.modelPrice !== null);

  // 6. 根据计费类型计算价格
  if (!isPerRequest) {
    // 按量计费
    const isTokensDisplay = quotaDisplayType === 'TOKENS';
    const inputRatioPriceUSD = (effectiveModelRatio || 0) * 2 * usedGroupRatio;
    const unitDivisor = tokenUnit === 'K' ? 1000 : 1;
    const unitLabel = tokenUnit === 'K' ? 'K' : 'M';

    const formatRatio = (value) =>
      hasRatioValue(value) ? Number(Number(value).toFixed(6)) : null;

    if (isTokensDisplay) {
      return {
        inputRatio: formatRatio(effectiveModelRatio),
        completionRatio: formatRatio(effectiveCompletionRatio),
        cacheRatio: formatRatio(effectiveCacheRatio),
        createCacheRatio: formatRatio(effectiveCreateCacheRatio),
        imageRatio: formatRatio(effectiveImageRatio),
        audioInputRatio: formatRatio(effectiveAudioRatio),
        audioOutputRatio: formatRatio(effectiveAudioCompletionRatio),
        billingMode: effectiveBillingMode || 'per-token',
        isPerToken: true,
        isTokensDisplay: true,
        usedGroup,
        usedGroupRatio,
      };
    }

    let symbol = '$';
    if (currency === 'CNY') {
      symbol = '¥';
    } else if (currency === 'CUSTOM') {
      try {
        const statusStr = localStorage.getItem('status');
        if (statusStr) {
          const s = JSON.parse(statusStr);
          symbol = s?.custom_currency_symbol || '¤';
        } else {
          symbol = '¤';
        }
      } catch (e) {
        symbol = '¤';
      }
    }

    const formatTokenPrice = (priceUSD) => {
      const rawDisplayPrice = displayPrice(priceUSD);
      const numericPrice =
        parseFloat(rawDisplayPrice.replace(/[^0-9.]/g, '')) / unitDivisor;
      return `${symbol}${numericPrice.toFixed(precision)}`;
    };

    const inputPrice = formatTokenPrice(inputRatioPriceUSD);
    const audioInputPrice = hasRatioValue(effectiveAudioRatio)
      ? formatTokenPrice(inputRatioPriceUSD * Number(effectiveAudioRatio))
      : null;

    return {
      inputPrice,
      completionPrice: formatTokenPrice(
        inputRatioPriceUSD * Number(effectiveCompletionRatio || 0),
      ),
      cachePrice: hasRatioValue(effectiveCacheRatio)
        ? formatTokenPrice(inputRatioPriceUSD * Number(effectiveCacheRatio))
        : null,
      createCachePrice: hasRatioValue(effectiveCreateCacheRatio)
        ? formatTokenPrice(
            inputRatioPriceUSD * Number(effectiveCreateCacheRatio),
          )
        : null,
      imagePrice: hasRatioValue(effectiveImageRatio)
        ? formatTokenPrice(inputRatioPriceUSD * Number(effectiveImageRatio))
        : null,
      audioInputPrice,
      audioOutputPrice:
        audioInputPrice && hasRatioValue(effectiveAudioCompletionRatio)
          ? formatTokenPrice(
              inputRatioPriceUSD *
                Number(effectiveAudioRatio) *
                Number(effectiveAudioCompletionRatio),
            )
          : null,
      unitLabel,
      billingMode: effectiveBillingMode || 'per-token',
      isPerToken: true,
      isTokensDisplay: false,
      usedGroup,
      usedGroupRatio,
    };
  }

  // 按次计费
  const priceUSD = parseFloat(effectiveModelPrice || 0) * usedGroupRatio;
  const displayVal = displayPrice(priceUSD);

  return {
    price: displayVal,
    billingMode: effectiveBillingMode || 'per-request',
    isPerToken: false,
    isTokensDisplay: false,
    usedGroup,
    usedGroupRatio,
  };
};

const finiteNumber = (value) => {
  if (
    (typeof value !== 'number' && typeof value !== 'string') ||
    (typeof value === 'string' && value.trim() === '')
  ) {
    return null;
  }
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
};

export const resolvePricingGroup = (
  record,
  selectedGroup = 'all',
  groupRatio = {},
) => {
  const selectedRatio = finiteNumber(groupRatio[selectedGroup]);
  if (selectedGroup !== 'all' && selectedRatio !== null) {
    return { usedGroup: selectedGroup, usedGroupRatio: selectedRatio };
  }

  let usedGroup = selectedGroup;
  let usedGroupRatio = null;
  for (const group of record.enable_groups || []) {
    const ratio = finiteNumber(groupRatio[group]);
    if (ratio !== null && (usedGroupRatio === null || ratio < usedGroupRatio)) {
      usedGroup = group;
      usedGroupRatio = ratio;
    }
  }
  return { usedGroup, usedGroupRatio: usedGroupRatio ?? 1 };
};

const inputPrice = (model) => {
  const ratio = finiteNumber(model.model_ratio);
  return ratio === null ? null : ratio * 2;
};

const PRICING_SORTS = {
  default: { label: (t) => t('默认排序') },
  input_price: {
    label: (t) => t('输入价格'),
    quotaType: 0,
    price: inputPrice,
  },
  output_price: {
    label: (t) => t('输出价格'),
    quotaType: 0,
    price: (model) => {
      const input = inputPrice(model);
      const completion = finiteNumber(model.completion_ratio);
      return input === null || completion === null ? null : input * completion;
    },
  },
  per_call_price: {
    label: (t) => t('单次价格'),
    quotaType: 1,
    price: (model) => finiteNumber(model.model_price),
  },
  model_name: { label: (t) => t('模型名称') },
};

export const getPricingSortOptions = (t) =>
  Object.entries(PRICING_SORTS).map(([value, config]) => ({
    value,
    label: config.label(t),
  }));

export const getModelPriceSortValue = (
  model,
  sortBy,
  selectedGroup,
  groupRatio,
) => {
  const config = PRICING_SORTS[sortBy];
  if (
    !config?.price ||
    model.quota_type !== config.quotaType ||
    model.billing_mode === 'tiered_expr'
  ) {
    return null;
  }
  const price = config.price(model);
  if (price === null) return null;
  const { usedGroupRatio } = resolvePricingGroup(
    model,
    selectedGroup,
    groupRatio,
  );
  return finiteNumber(price * usedGroupRatio);
};

const nameCollator = new Intl.Collator('en', {
  numeric: true,
  sensitivity: 'base',
});

export const sortPricingModels = (
  models,
  {
    sortBy = 'default',
    sortDirection = 'asc',
    selectedGroup = 'all',
    groupRatio = {},
  } = {},
) => {
  if (sortBy === 'default' || !Object.hasOwn(PRICING_SORTS, sortBy)) {
    return models;
  }
  const direction = sortDirection === 'desc' ? -1 : 1;

  return models
    .map((model, index) => ({
      model,
      index,
      price: getModelPriceSortValue(model, sortBy, selectedGroup, groupRatio),
    }))
    .sort((a, b) => {
      const byName = nameCollator.compare(
        a.model.model_name || '',
        b.model.model_name || '',
      );
      if (sortBy === 'model_name') {
        return byName * direction || a.index - b.index;
      }
      if (a.price === null && b.price !== null) return 1;
      if (a.price !== null && b.price === null) return -1;
      if (a.price !== null && b.price !== null && a.price !== b.price) {
        return (a.price < b.price ? -1 : 1) * direction;
      }
      return byName || a.index - b.index;
    })
    .map(({ model }) => model);
};

