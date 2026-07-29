package helper

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func modelPriceNotConfiguredError(modelName string, userId int) error {
	if model.IsAdmin(userId) {
		return fmt.Errorf(
			"模型 %s 的价格未配置。请前往「系统设置 → 运营设置」开启自用模式，或在「系统设置 → 分组与模型定价设置」中为该模型配置价格；"+
				"Model %s price not configured. Go to System Settings → Operation Settings to enable self-use mode, or configure the model price in System Settings → Group & Model Pricing.",
			modelName, modelName,
		)
	}
	return fmt.Errorf(
		"模型 %s 的价格尚未由管理员配置，暂时无法使用，请联系站点管理员开启该模型；"+
			"Model %s has not been priced by the administrator yet. Please contact the site administrator to enable this model.",
		modelName, modelName,
	)
}

// https://docs.claude.com/en/docs/build-with-claude/prompt-caching#1-hour-cache-duration
const claudeCacheCreation1hMultiplier = 6 / 3.75

// defaultTieredPreConsumeMaxTokens is the fallback completion-token estimate
// used for tiered expression pre-consume when the client omits max_tokens, so
// the pre-consumed quota still reflects a plausible output cost in paid groups.
const defaultTieredPreConsumeMaxTokens = 8192

// HandleGroupRatio checks for "auto_group" in the context and updates the group ratio and relayInfo.UsingGroup if present
func HandleGroupRatio(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) types.GroupRatioInfo {
	groupRatioInfo := types.GroupRatioInfo{
		GroupRatio:        1.0, // default ratio
		GroupSpecialRatio: -1,
	}

	// check auto group
	autoGroup, exists := ctx.Get("auto_group")
	if exists {
		logger.LogDebug(ctx, "final group: %s", autoGroup)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	// check user group special ratio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		// user group special ratio
		groupRatioInfo.GroupSpecialRatio = userGroupRatio
		groupRatioInfo.GroupRatio = userGroupRatio
		groupRatioInfo.HasSpecialRatio = true
	} else {
		// normal group ratio
		groupRatioInfo.GroupRatio = ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	}

	return groupRatioInfo
}

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (types.PriceData, error) {
	// 获取用户分组，优先使用分组级别配置
	userGroup := info.UserGroup
	if autoGroup, exists := c.Get("auto_group"); exists {
		userGroup = autoGroup.(string)
	}

	// 先尝试获取分组级别的计费模式和价格
	groupBillingMode := ratio_setting.GetGroupBillingMode(userGroup, info.OriginModelName)
	groupModelPrice, hasGroupPrice := ratio_setting.GetGroupModelPrice(userGroup, info.OriginModelName)
	groupModelRatio, hasGroupRatio := ratio_setting.GetGroupModelRatio(userGroup, info.OriginModelName)

	// 确定使用的价格和计费方式
	var modelPrice float64
	var usePrice bool
	var billingMode string

	if groupBillingMode != "" {
		// 分组有独立的计费方式配置
		billingMode = groupBillingMode
		switch billingMode {
		case "per-request":
			if hasGroupPrice {
				modelPrice = groupModelPrice
				usePrice = true
			} else {
				// 回退到全局
				modelPrice, usePrice = ratio_setting.GetModelPrice(info.OriginModelName, false)
			}
		case "tiered_expr":
			// 表达式计费，不需要价格
			usePrice = false
		default: // per-token
			// 按量计费：不应触碰 modelPrice 字段。
			// 倍率会在下方 if !usePrice 分支统一从 groupModelRatio 或全局 GetModelRatio 取，
			// 这里保持 modelPrice=-1 以遵守"非按次时 ModelPrice <= 0"的全局约定，
			// 避免污染 PriceData.ModelPrice 导致日志被误判为按次计费。
			modelPrice = -1
			usePrice = false
		}
	} else {
		// 分组没有独立配置，使用全局配置
		modelPrice, usePrice = ratio_setting.GetModelPrice(info.OriginModelName, false)
		billingMode = billing_setting.GetBillingMode(info.OriginModelName)
	}

	groupRatioInfo := HandleGroupRatio(c, info)

	// Check if this model uses tiered_expr billing
	if billingMode == billing_setting.BillingModeTieredExpr || billingMode == "tiered_expr" {
		// 尝试获取分组级别的表达式
		groupExpr := ratio_setting.GetGroupBillingExpr(userGroup, info.OriginModelName)
		if groupExpr != "" {
			return modelPriceHelperTieredWithExpr(c, info, promptTokens, meta, groupRatioInfo, groupExpr)
		}
		return modelPriceHelperTiered(c, info, promptTokens, meta, groupRatioInfo)
	}

	var preConsumedQuota int
	var modelRatio float64
	var completionRatio float64
	var cacheRatio float64
	var imageRatio float64
	var cacheCreationRatio float64
	var cacheCreationRatio5m float64
	var cacheCreationRatio1h float64
	var audioRatio float64
	var audioCompletionRatio float64
	var freeModel bool
	if !usePrice {
		preConsumedTokens := common.Max(promptTokens, common.PreConsumedQuota)
		if meta.MaxTokens != 0 {
			preConsumedTokens += meta.MaxTokens
		}
		var success bool
		var matchName string

		// 优先使用分组级别的倍率
		if hasGroupRatio {
			modelRatio = groupModelRatio
			success = true
			matchName = info.OriginModelName
		} else {
			modelRatio, success, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
		}

		if !success {
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !acceptUnsetRatio {
				return types.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
			}
		}

		// 获取分组级别的其他倍率，如果没有则使用全局配置
		if r, ok := ratio_setting.GetGroupCompletionRatio(userGroup, info.OriginModelName); ok {
			completionRatio = r
		} else {
			completionRatio = ratio_setting.GetCompletionRatio(info.OriginModelName)
		}

		if r, ok := ratio_setting.GetGroupCacheRatio(userGroup, info.OriginModelName); ok {
			cacheRatio = r
		} else {
			cacheRatio, _ = ratio_setting.GetCacheRatio(info.OriginModelName)
		}

		if r, ok := ratio_setting.GetGroupCreateCacheRatio(userGroup, info.OriginModelName); ok {
			cacheCreationRatio = r
		} else {
			cacheCreationRatio, _ = ratio_setting.GetCreateCacheRatio(info.OriginModelName)
		}

		cacheCreationRatio5m = cacheCreationRatio
		cacheCreationRatio1h = cacheCreationRatio * claudeCacheCreation1hMultiplier

		if r, ok := ratio_setting.GetGroupImageRatio(userGroup, info.OriginModelName); ok {
			imageRatio = r
		} else {
			imageRatio, _ = ratio_setting.GetImageRatio(info.OriginModelName)
		}

		if r, ok := ratio_setting.GetGroupAudioRatio(userGroup, info.OriginModelName); ok {
			audioRatio = r
		} else {
			audioRatio = ratio_setting.GetAudioRatio(info.OriginModelName)
		}

		if r, ok := ratio_setting.GetGroupAudioCompletionRatio(userGroup, info.OriginModelName); ok {
			audioCompletionRatio = r
		} else {
			audioCompletionRatio = ratio_setting.GetAudioCompletionRatio(info.OriginModelName)
		}

		ratio := modelRatio * groupRatioInfo.GroupRatio
		quota, err := common.QuotaFromFloatStrict(float64(preConsumedTokens) * ratio)
		if err != nil {
			return types.PriceData{}, err
		}
		preConsumedQuota = quota
	} else {
		if meta.ImagePriceRatio != 0 {
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
	}

	// check if free model pre-consume is disabled
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		// if model price or ratio is 0, do not pre-consume quota
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		} else if usePrice {
			if modelPrice == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		} else {
			if modelRatio == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		}
	}

	priceData := types.PriceData{
		FreeModel:            freeModel,
		ModelPrice:           modelPrice,
		ModelRatio:           modelRatio,
		CompletionRatio:      completionRatio,
		GroupRatioInfo:       groupRatioInfo,
		UsePrice:             usePrice,
		CacheRatio:           cacheRatio,
		ImageRatio:           imageRatio,
		AudioRatio:           audioRatio,
		AudioCompletionRatio: audioCompletionRatio,
		CacheCreationRatio:   cacheCreationRatio,
		CacheCreation5mRatio: cacheCreationRatio5m,
		CacheCreation1hRatio: cacheCreationRatio1h,
		QuotaToPreConsume:    preConsumedQuota,
	}
	if usePrice {
		for name, ratio := range meta.BillingRatios {
			priceData.AddOtherRatio(name, ratio)
		}
		quotaToPreConsume := priceData.ApplyOtherRatiosToFloat(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		quota, err := common.QuotaFromFloatStrict(quotaToPreConsume)
		if err != nil {
			return types.PriceData{}, err
		}
		priceData.QuotaToPreConsume = quota
	}

	if common.DebugEnabled {
		logger.LogDebug(c, "model_price_helper result: %s", priceData.ToSetting())
	}
	info.PriceData = priceData
	return priceData, nil
}

// ModelPriceHelperPerCall 按次/按量计费的 PriceHelper (MJ、Task)
func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (types.PriceData, error) {
	groupRatioInfo := HandleGroupRatio(c, info)

	// 获取用户分组
	userGroup := info.UserGroup
	if autoGroup, exists := c.Get("auto_group"); exists {
		userGroup = autoGroup.(string)
	}

	// 先尝试获取分组级别的配置
	groupBillingMode := ratio_setting.GetGroupBillingMode(userGroup, info.OriginModelName)
	groupModelPrice, hasGroupPrice := ratio_setting.GetGroupModelPrice(userGroup, info.OriginModelName)
	groupModelRatio, hasGroupRatio := ratio_setting.GetGroupModelRatio(userGroup, info.OriginModelName)

	var modelPrice float64
	var usePrice bool
	var modelRatio float64

	// 确定计费方式和价格
	if groupBillingMode == "per-request" && hasGroupPrice {
		// 分组配置为按次计费且有价格
		modelPrice = groupModelPrice
		usePrice = true
	} else if groupBillingMode == "per-token" && hasGroupRatio {
		// 分组配置为按量计费且有倍率
		modelRatio = groupModelRatio
		usePrice = false
	} else {
		// 回退到全局配置
		var success bool
		modelPrice, success = ratio_setting.GetModelPrice(info.OriginModelName, true)
		usePrice = success

		if !success {
			defaultPrice, ok := ratio_setting.GetDefaultModelPriceMap()[info.OriginModelName]
			if ok {
				modelPrice = defaultPrice
				usePrice = true
			} else {
				var ratioSuccess bool
				var matchName string

				// 检查分组级别倍率
				if hasGroupRatio {
					modelRatio = groupModelRatio
					ratioSuccess = true
				} else {
					modelRatio, ratioSuccess, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
				}

				acceptUnsetRatio := false
				if info.UserSetting.AcceptUnsetRatioModel {
					acceptUnsetRatio = true
				}
				if !ratioSuccess && !acceptUnsetRatio {
					return types.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
				}
			}
		}
	}

	var quota int
	freeModel := false

	if usePrice {
		var err error
		quota, err = common.QuotaFromFloatStrict(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return types.PriceData{}, err
		}
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelPrice == 0 {
				quota = 0
				freeModel = true
			}
		}
	} else {
		// 按量计费：以模型倍率的一半作为预扣额度
		var err error
		quota, err = common.QuotaFromFloatStrict(modelRatio / 2 * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return types.PriceData{}, err
		}
		modelPrice = -1
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelRatio == 0 {
				quota = 0
				freeModel = true
			}
		}
	}

	priceData := types.PriceData{
		FreeModel:      freeModel,
		ModelPrice:     modelPrice,
		ModelRatio:     modelRatio,
		UsePrice:       usePrice,
		Quota:          quota,
		GroupRatioInfo: groupRatioInfo,
	}
	return priceData, nil
}

func HasModelBillingConfig(modelName string) bool {
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	if _, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
		return true
	}
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr {
		return false
	}
	expr, ok := billing_setting.GetBillingExpr(modelName)
	return ok && strings.TrimSpace(expr) != ""
}

func modelPriceHelperTiered(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta, groupRatioInfo types.GroupRatioInfo) (types.PriceData, error) {
	exprStr, ok := billing_setting.GetBillingExpr(info.OriginModelName)
	if !ok {
		return types.PriceData{}, fmt.Errorf("model %s is configured as tiered_expr but has no billing expression", info.OriginModelName)
	}
	return modelPriceHelperTieredWithExpr(c, info, promptTokens, meta, groupRatioInfo, exprStr)
}

// modelPriceHelperTieredWithExpr 使用指定的表达式进行阶梯计费（用于分组级别配置）
func modelPriceHelperTieredWithExpr(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta, groupRatioInfo types.GroupRatioInfo, exprStr string) (types.PriceData, error) {
	estimatedCompletionTokens := meta.MaxTokens
	if estimatedCompletionTokens == 0 && groupRatioInfo.GroupRatio != 0 {
		estimatedCompletionTokens = defaultTieredPreConsumeMaxTokens
	}

	requestInput, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return types.PriceData{}, err
	}

	rawCost, trace, err := billingexpr.RunExprWithRequest(exprStr, billingexpr.TokenParams{
		P:   float64(promptTokens),
		C:   float64(estimatedCompletionTokens),
		Len: float64(promptTokens),
	}, requestInput)
	if err != nil {
		return types.PriceData{}, fmt.Errorf("model %s tiered expr run failed: %w", info.OriginModelName, err)
	}
	if err := billingexpr.ValidateCost(rawCost); err != nil {
		return types.PriceData{}, fmt.Errorf("model %s tiered expr produced an invalid charge: %w", info.OriginModelName, err)
	}

	// Expression coefficients are $/1M tokens prices; convert to quota the same way per-call billing does.
	quotaBeforeGroup := rawCost / 1_000_000 * common.QuotaPerUnit
	preConsumedQuota, err := billingexpr.QuotaRoundStrict(quotaBeforeGroup * groupRatioInfo.GroupRatio)
	if err != nil {
		return types.PriceData{}, err
	}

	freeModel := false
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		}
	}

	exprHash := billingexpr.ExprHashString(exprStr)
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode:               billing_setting.BillingModeTieredExpr,
		ModelName:                 info.OriginModelName,
		ExprString:                exprStr,
		ExprHash:                  exprHash,
		GroupRatio:                groupRatioInfo.GroupRatio,
		EstimatedPromptTokens:     promptTokens,
		EstimatedCompletionTokens: estimatedCompletionTokens,
		EstimatedQuotaBeforeGroup: quotaBeforeGroup,
		EstimatedQuotaAfterGroup:  preConsumedQuota,
		EstimatedTier:             trace.MatchedTier,
		QuotaPerUnit:              common.QuotaPerUnit,
		ExprVersion:               billingexpr.ExprVersion(exprStr),
	}
	info.TieredBillingSnapshot = snapshot
	info.BillingRequestInput = &requestInput

	priceData := types.PriceData{
		FreeModel:         freeModel,
		GroupRatioInfo:    groupRatioInfo,
		QuotaToPreConsume: preConsumedQuota,
	}

	logger.LogDebug(c, "model_price_helper_tiered result: model=%s preConsume=%d quotaBeforeGroup=%.2f groupRatio=%.2f tier=%s", info.OriginModelName, preConsumedQuota, quotaBeforeGroup, groupRatioInfo.GroupRatio, trace.MatchedTier)

	info.PriceData = priceData
	return priceData, nil
}
