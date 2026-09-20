package ratio_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"
)

// 分组级别模型定价（覆盖全局配置）
// 格式: group -> (model -> value)
var groupModelPriceMap = types.NewRWMap[string, map[string]float64]()
var groupModelRatioMap = types.NewRWMap[string, map[string]float64]()
var groupCompletionRatioMap = types.NewRWMap[string, map[string]float64]()
var groupCacheRatioMap = types.NewRWMap[string, map[string]float64]()
var groupCreateCacheRatioMap = types.NewRWMap[string, map[string]float64]()
var groupImageRatioMap = types.NewRWMap[string, map[string]float64]()
var groupAudioRatioMap = types.NewRWMap[string, map[string]float64]()
var groupAudioCompletionRatioMap = types.NewRWMap[string, map[string]float64]()
var groupBillingModeMap = types.NewRWMap[string, map[string]string]()
var groupBillingExprMap = types.NewRWMap[string, map[string]string]()

// ===== Model Price =====

func GetGroupModelPrice(group, model string) (float64, bool) {
	groupMap, ok := groupModelPriceMap.Get(group)
	if !ok {
		return 0, false
	}
	price, ok := groupMap[model]
	return price, ok
}

func GetGroupModelPriceCopy() map[string]map[string]float64 {
	return groupModelPriceMap.ReadAll()
}

func GroupModelPrice2JSONString() string {
	return groupModelPriceMap.MarshalJSONString()
}

func UpdateGroupModelPriceByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(groupModelPriceMap, jsonStr, InvalidateExposedDataCache)
}

// ===== Model Ratio =====

func GetGroupModelRatio(group, model string) (float64, bool) {
	groupMap, ok := groupModelRatioMap.Get(group)
	if !ok {
		return 0, false
	}
	ratio, ok := groupMap[model]
	return ratio, ok
}

func GetGroupModelRatioCopy() map[string]map[string]float64 {
	return groupModelRatioMap.ReadAll()
}

func GroupModelRatio2JSONString() string {
	return groupModelRatioMap.MarshalJSONString()
}

func UpdateGroupModelRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(groupModelRatioMap, jsonStr, InvalidateExposedDataCache)
}

// ===== Completion Ratio =====

func GetGroupCompletionRatio(group, model string) (float64, bool) {
	groupMap, ok := groupCompletionRatioMap.Get(group)
	if !ok {
		return 0, false
	}
	ratio, ok := groupMap[model]
	return ratio, ok
}

func GetGroupCompletionRatioCopy() map[string]map[string]float64 {
	return groupCompletionRatioMap.ReadAll()
}

func GroupCompletionRatio2JSONString() string {
	return groupCompletionRatioMap.MarshalJSONString()
}

func UpdateGroupCompletionRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(groupCompletionRatioMap, jsonStr, InvalidateExposedDataCache)
}

// ===== Cache Ratio =====

func GetGroupCacheRatio(group, model string) (float64, bool) {
	groupMap, ok := groupCacheRatioMap.Get(group)
	if !ok {
		return 0, false
	}
	ratio, ok := groupMap[model]
	return ratio, ok
}

func GetGroupCacheRatioCopy() map[string]map[string]float64 {
	return groupCacheRatioMap.ReadAll()
}

func GroupCacheRatio2JSONString() string {
	return groupCacheRatioMap.MarshalJSONString()
}

func UpdateGroupCacheRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(groupCacheRatioMap, jsonStr, InvalidateExposedDataCache)
}

// ===== Create Cache Ratio =====

func GetGroupCreateCacheRatio(group, model string) (float64, bool) {
	groupMap, ok := groupCreateCacheRatioMap.Get(group)
	if !ok {
		return 0, false
	}
	ratio, ok := groupMap[model]
	return ratio, ok
}

func GetGroupCreateCacheRatioCopy() map[string]map[string]float64 {
	return groupCreateCacheRatioMap.ReadAll()
}

func GroupCreateCacheRatio2JSONString() string {
	return groupCreateCacheRatioMap.MarshalJSONString()
}

func UpdateGroupCreateCacheRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(groupCreateCacheRatioMap, jsonStr, InvalidateExposedDataCache)
}

// ===== Image Ratio =====

func GetGroupImageRatio(group, model string) (float64, bool) {
	groupMap, ok := groupImageRatioMap.Get(group)
	if !ok {
		return 0, false
	}
	ratio, ok := groupMap[model]
	return ratio, ok
}

func GetGroupImageRatioCopy() map[string]map[string]float64 {
	return groupImageRatioMap.ReadAll()
}

func GroupImageRatio2JSONString() string {
	return groupImageRatioMap.MarshalJSONString()
}

func UpdateGroupImageRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(groupImageRatioMap, jsonStr, InvalidateExposedDataCache)
}

// ===== Audio Ratio =====

func GetGroupAudioRatio(group, model string) (float64, bool) {
	groupMap, ok := groupAudioRatioMap.Get(group)
	if !ok {
		return 0, false
	}
	ratio, ok := groupMap[model]
	return ratio, ok
}

func GetGroupAudioRatioCopy() map[string]map[string]float64 {
	return groupAudioRatioMap.ReadAll()
}

func GroupAudioRatio2JSONString() string {
	return groupAudioRatioMap.MarshalJSONString()
}

func UpdateGroupAudioRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(groupAudioRatioMap, jsonStr, InvalidateExposedDataCache)
}

// ===== Audio Completion Ratio =====

func GetGroupAudioCompletionRatio(group, model string) (float64, bool) {
	groupMap, ok := groupAudioCompletionRatioMap.Get(group)
	if !ok {
		return 0, false
	}
	ratio, ok := groupMap[model]
	return ratio, ok
}

func GetGroupAudioCompletionRatioCopy() map[string]map[string]float64 {
	return groupAudioCompletionRatioMap.ReadAll()
}

func GroupAudioCompletionRatio2JSONString() string {
	return groupAudioCompletionRatioMap.MarshalJSONString()
}

func UpdateGroupAudioCompletionRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(groupAudioCompletionRatioMap, jsonStr, InvalidateExposedDataCache)
}

// ===== Billing Mode =====

func GetGroupBillingMode(group, model string) string {
	groupMap, ok := groupBillingModeMap.Get(group)
	if !ok {
		return ""
	}
	mode, ok := groupMap[model]
	if !ok {
		return ""
	}
	return mode
}

func GetGroupBillingModeCopy() map[string]map[string]string {
	return groupBillingModeMap.ReadAll()
}

func GroupBillingMode2JSONString() string {
	return groupBillingModeMap.MarshalJSONString()
}

func UpdateGroupBillingModeByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(groupBillingModeMap, jsonStr, InvalidateExposedDataCache)
}

// ===== Billing Expr =====

func GetGroupBillingExpr(group, model string) string {
	groupMap, ok := groupBillingExprMap.Get(group)
	if !ok {
		return ""
	}
	expr, ok := groupMap[model]
	if !ok {
		return ""
	}
	return expr
}

func GetGroupBillingExprCopy() map[string]map[string]string {
	return groupBillingExprMap.ReadAll()
}

func GroupBillingExpr2JSONString() string {
	return groupBillingExprMap.MarshalJSONString()
}

func UpdateGroupBillingExprByJSONString(jsonStr string) error {
	if err := ValidateGroupBillingExprJSONString(jsonStr); err != nil {
		return err
	}
	return types.LoadFromJsonStringWithCallback(groupBillingExprMap, jsonStr, InvalidateExposedDataCache)
}

func ValidateGroupBillingExprJSONString(jsonStr string) error {
	expressions := make(map[string]map[string]string)
	if err := common.UnmarshalJsonStr(jsonStr, &expressions); err != nil {
		return fmt.Errorf("invalid group tiered billing expression map: %w", err)
	}
	for groupName, groupExpressions := range expressions {
		for modelName, exprStr := range groupExpressions {
			if strings.TrimSpace(exprStr) == "" {
				return fmt.Errorf("group %s model %s has an empty tiered billing expression", groupName, modelName)
			}
			if err := billing_setting.SmokeTestExpr(exprStr); err != nil {
				return fmt.Errorf("group %s model %s has an invalid tiered billing expression: %w", groupName, modelName, err)
			}
		}
	}
	return nil
}

// ===== Helper Functions =====

// GetGroupPricingForModel 获取指定分组的模型定价，如果分组没有配置则返回全局配置
func GetGroupPricingForModel(group, model string) (price float64, usePrice bool, mode string) {
	// 1. 先查分组级别的计费方式
	mode = GetGroupBillingMode(group, model)
	if mode == "" {
		// 回退到全局
		mode = GetGlobalBillingMode(model)
	}

	// 2. 根据计费方式查询对应的价格/倍率
	switch mode {
	case "per-request":
		if p, ok := GetGroupModelPrice(group, model); ok {
			return p, true, mode
		}
		// 回退到全局
		if p, ok := GetModelPrice(model, false); ok {
			return p, true, mode
		}
	case "tiered_expr":
		// 表达式计费，不需要价格
		return 0, false, mode
	default: // per-token
		if r, ok := GetGroupModelRatio(group, model); ok {
			return r, false, mode
		}
		// 回退到全局
		if r, ok, _ := GetModelRatio(model); ok {
			return r, false, mode
		}
	}

	return 0, false, mode
}

// GetGlobalBillingMode 获取全局计费模式
func GetGlobalBillingMode(model string) string {
	// 检查是否设置了按次计费的价格
	if _, ok := GetModelPrice(model, false); ok {
		return "per-request"
	}
	// 检查是否有表达式计费
	// 这里需要导入 billing_setting，但为了避免循环依赖，使用简单判断
	return "per-token"
}

// GetGroupModelRatiosForModel 获取指定分组的所有倍率配置
func GetGroupModelRatiosForModel(group, model string) map[string]float64 {
	result := make(map[string]float64)

	if r, ok := GetGroupModelRatio(group, model); ok {
		result["model_ratio"] = r
	} else if r, ok, _ := GetModelRatio(model); ok {
		result["model_ratio"] = r
	}

	if r, ok := GetGroupCompletionRatio(group, model); ok {
		result["completion_ratio"] = r
	} else {
		result["completion_ratio"] = GetCompletionRatio(model)
	}

	if r, ok := GetGroupCacheRatio(group, model); ok {
		result["cache_ratio"] = r
	} else if r, ok := GetCacheRatio(model); ok {
		result["cache_ratio"] = r
	}

	if r, ok := GetGroupCreateCacheRatio(group, model); ok {
		result["create_cache_ratio"] = r
	} else if r, ok := GetCreateCacheRatio(model); ok {
		result["create_cache_ratio"] = r
	}

	if r, ok := GetGroupImageRatio(group, model); ok {
		result["image_ratio"] = r
	} else if r, ok := GetImageRatio(model); ok {
		result["image_ratio"] = r
	}

	if r, ok := GetGroupAudioRatio(group, model); ok {
		result["audio_ratio"] = r
	} else {
		result["audio_ratio"] = GetAudioRatio(model)
	}

	if r, ok := GetGroupAudioCompletionRatio(group, model); ok {
		result["audio_completion_ratio"] = r
	} else {
		result["audio_completion_ratio"] = GetAudioCompletionRatio(model)
	}

	return result
}

// InvalidateGroupPricingCache 清除分组定价缓存
func InvalidateGroupPricingCache() {
	InvalidateExposedDataCache()
}

// groupPricingDraft 是分组定价十张表的可修改副本。同步在副本上计算，
// 结果交由 model 层在写入锁内校验、落库并发布，运行时不会出现半同步状态。
type groupPricingDraft struct {
	groupModelPriceMap           *types.RWMap[string, map[string]float64]
	groupModelRatioMap           *types.RWMap[string, map[string]float64]
	groupCompletionRatioMap      *types.RWMap[string, map[string]float64]
	groupCacheRatioMap           *types.RWMap[string, map[string]float64]
	groupCreateCacheRatioMap     *types.RWMap[string, map[string]float64]
	groupImageRatioMap           *types.RWMap[string, map[string]float64]
	groupAudioRatioMap           *types.RWMap[string, map[string]float64]
	groupAudioCompletionRatioMap *types.RWMap[string, map[string]float64]
	groupBillingModeMap          *types.RWMap[string, map[string]string]
	groupBillingExprMap          *types.RWMap[string, map[string]string]
}

func cloneFloat64GroupMap(src *types.RWMap[string, map[string]float64]) *types.RWMap[string, map[string]float64] {
	dst := types.NewRWMap[string, map[string]float64]()
	for group, values := range src.ReadAll() {
		dst.Set(group, copyFloat64Map(values))
	}
	return dst
}

func cloneStringGroupMap(src *types.RWMap[string, map[string]string]) *types.RWMap[string, map[string]string] {
	dst := types.NewRWMap[string, map[string]string]()
	for group, values := range src.ReadAll() {
		dst.Set(group, copyStringMap(values))
	}
	return dst
}

// HasGroupPricingConfig 判断分组是否配置过任何定价项。
func HasGroupPricingConfig(group string) bool {
	if _, ok := groupModelPriceMap.Get(group); ok {
		return true
	}
	if _, ok := groupModelRatioMap.Get(group); ok {
		return true
	}
	_, ok := groupBillingModeMap.Get(group)
	return ok
}

// BuildGroupPricingSync 计算分组同步后的十项分组定价配置，不修改运行时。
// 调用方必须在配置写入锁内调用并提交结果，避免读取源配置与落库之间插入其他写入。
func BuildGroupPricingSync(sourceGroup string, targetGroups []string, modelNames []string, fromGlobal bool) map[string]string {
	d := &groupPricingDraft{
		groupModelPriceMap:           cloneFloat64GroupMap(groupModelPriceMap),
		groupModelRatioMap:           cloneFloat64GroupMap(groupModelRatioMap),
		groupCompletionRatioMap:      cloneFloat64GroupMap(groupCompletionRatioMap),
		groupCacheRatioMap:           cloneFloat64GroupMap(groupCacheRatioMap),
		groupCreateCacheRatioMap:     cloneFloat64GroupMap(groupCreateCacheRatioMap),
		groupImageRatioMap:           cloneFloat64GroupMap(groupImageRatioMap),
		groupAudioRatioMap:           cloneFloat64GroupMap(groupAudioRatioMap),
		groupAudioCompletionRatioMap: cloneFloat64GroupMap(groupAudioCompletionRatioMap),
		groupBillingModeMap:          cloneStringGroupMap(groupBillingModeMap),
		groupBillingExprMap:          cloneStringGroupMap(groupBillingExprMap),
	}
	switch {
	case fromGlobal || sourceGroup == "global":
		d.syncFromGlobal(targetGroups, modelNames)
	case len(modelNames) > 0 && !HasGroupPricingConfig(sourceGroup):
		// 源分组没有配置时退回全局配置
		d.syncFromGlobal(targetGroups, modelNames)
	case len(modelNames) > 0:
		d.syncModels(sourceGroup, targetGroups, modelNames)
	default:
		d.syncAll(sourceGroup, targetGroups)
	}
	return map[string]string{
		"GroupModelPrice":           d.groupModelPriceMap.MarshalJSONString(),
		"GroupModelRatio":           d.groupModelRatioMap.MarshalJSONString(),
		"GroupCompletionRatio":      d.groupCompletionRatioMap.MarshalJSONString(),
		"GroupCacheRatio":           d.groupCacheRatioMap.MarshalJSONString(),
		"GroupCreateCacheRatio":     d.groupCreateCacheRatioMap.MarshalJSONString(),
		"GroupImageRatio":           d.groupImageRatioMap.MarshalJSONString(),
		"GroupAudioRatio":           d.groupAudioRatioMap.MarshalJSONString(),
		"GroupAudioCompletionRatio": d.groupAudioCompletionRatioMap.MarshalJSONString(),
		"GroupBillingMode":          d.groupBillingModeMap.MarshalJSONString(),
		"GroupBillingExpr":          d.groupBillingExprMap.MarshalJSONString(),
	}
}

// syncAll 将源分组的全部配置复制到目标分组。
func (d *groupPricingDraft) syncAll(sourceGroup string, targetGroups []string) {
	// 获取源分组的所有配置
	sourcePrice := d.groupModelPriceMap.ReadAll()
	sourceRatio := d.groupModelRatioMap.ReadAll()
	sourceCompletion := d.groupCompletionRatioMap.ReadAll()
	sourceCache := d.groupCacheRatioMap.ReadAll()
	sourceCreateCache := d.groupCreateCacheRatioMap.ReadAll()
	sourceImage := d.groupImageRatioMap.ReadAll()
	sourceAudio := d.groupAudioRatioMap.ReadAll()
	sourceAudioCompletion := d.groupAudioCompletionRatioMap.ReadAll()
	sourceBillingMode := d.groupBillingModeMap.ReadAll()
	sourceBillingExpr := d.groupBillingExprMap.ReadAll()

	// 复制到目标分组
	for _, target := range targetGroups {
		if sourceData, ok := sourcePrice[sourceGroup]; ok {
			d.groupModelPriceMap.Set(target, copyFloat64Map(sourceData))
		}
		if sourceData, ok := sourceRatio[sourceGroup]; ok {
			d.groupModelRatioMap.Set(target, copyFloat64Map(sourceData))
		}
		if sourceData, ok := sourceCompletion[sourceGroup]; ok {
			d.groupCompletionRatioMap.Set(target, copyFloat64Map(sourceData))
		}
		if sourceData, ok := sourceCache[sourceGroup]; ok {
			d.groupCacheRatioMap.Set(target, copyFloat64Map(sourceData))
		}
		if sourceData, ok := sourceCreateCache[sourceGroup]; ok {
			d.groupCreateCacheRatioMap.Set(target, copyFloat64Map(sourceData))
		}
		if sourceData, ok := sourceImage[sourceGroup]; ok {
			d.groupImageRatioMap.Set(target, copyFloat64Map(sourceData))
		}
		if sourceData, ok := sourceAudio[sourceGroup]; ok {
			d.groupAudioRatioMap.Set(target, copyFloat64Map(sourceData))
		}
		if sourceData, ok := sourceAudioCompletion[sourceGroup]; ok {
			d.groupAudioCompletionRatioMap.Set(target, copyFloat64Map(sourceData))
		}
		if sourceData, ok := sourceBillingMode[sourceGroup]; ok {
			d.groupBillingModeMap.Set(target, copyStringMap(sourceData))
		}
		if sourceData, ok := sourceBillingExpr[sourceGroup]; ok {
			d.groupBillingExprMap.Set(target, copyStringMap(sourceData))
		}
	}

}

// syncModels 只把源分组中指定模型的配置复制到目标分组。
func (d *groupPricingDraft) syncModels(sourceGroup string, targetGroups []string, modelNames []string) {
	if len(modelNames) == 0 {
		return
	}

	// 构建模型名称集合，方便查找
	modelSet := make(map[string]struct{}, len(modelNames))
	for _, name := range modelNames {
		modelSet[name] = struct{}{}
	}

	// 获取源分组的所有配置
	sourcePrice := d.groupModelPriceMap.ReadAll()
	sourceRatio := d.groupModelRatioMap.ReadAll()
	sourceCompletion := d.groupCompletionRatioMap.ReadAll()
	sourceCache := d.groupCacheRatioMap.ReadAll()
	sourceCreateCache := d.groupCreateCacheRatioMap.ReadAll()
	sourceImage := d.groupImageRatioMap.ReadAll()
	sourceAudio := d.groupAudioRatioMap.ReadAll()
	sourceAudioCompletion := d.groupAudioCompletionRatioMap.ReadAll()
	sourceBillingMode := d.groupBillingModeMap.ReadAll()
	sourceBillingExpr := d.groupBillingExprMap.ReadAll()

	// 只复制指定模型的配置到目标分组
	for _, target := range targetGroups {
		// 获取目标分组的现有配置
		targetPrice := copyFloat64Map(d.groupModelPriceMap.ReadAll()[target])
		if targetPrice == nil {
			targetPrice = make(map[string]float64)
		}
		targetRatio := copyFloat64Map(d.groupModelRatioMap.ReadAll()[target])
		if targetRatio == nil {
			targetRatio = make(map[string]float64)
		}
		targetCompletion := copyFloat64Map(d.groupCompletionRatioMap.ReadAll()[target])
		if targetCompletion == nil {
			targetCompletion = make(map[string]float64)
		}
		targetCache := copyFloat64Map(d.groupCacheRatioMap.ReadAll()[target])
		if targetCache == nil {
			targetCache = make(map[string]float64)
		}
		targetCreateCache := copyFloat64Map(d.groupCreateCacheRatioMap.ReadAll()[target])
		if targetCreateCache == nil {
			targetCreateCache = make(map[string]float64)
		}
		targetImage := copyFloat64Map(d.groupImageRatioMap.ReadAll()[target])
		if targetImage == nil {
			targetImage = make(map[string]float64)
		}
		targetAudio := copyFloat64Map(d.groupAudioRatioMap.ReadAll()[target])
		if targetAudio == nil {
			targetAudio = make(map[string]float64)
		}
		targetAudioCompletion := copyFloat64Map(d.groupAudioCompletionRatioMap.ReadAll()[target])
		if targetAudioCompletion == nil {
			targetAudioCompletion = make(map[string]float64)
		}
		targetBillingMode := copyStringMap(d.groupBillingModeMap.ReadAll()[target])
		if targetBillingMode == nil {
			targetBillingMode = make(map[string]string)
		}
		targetBillingExpr := copyStringMap(d.groupBillingExprMap.ReadAll()[target])
		if targetBillingExpr == nil {
			targetBillingExpr = make(map[string]string)
		}

		// 只复制指定模型的配置
		for modelName := range modelSet {
			if sourceData, ok := sourcePrice[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetPrice[modelName] = val
				}
			}
			if sourceData, ok := sourceRatio[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetRatio[modelName] = val
				}
			}
			if sourceData, ok := sourceCompletion[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetCompletion[modelName] = val
				}
			}
			if sourceData, ok := sourceCache[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetCache[modelName] = val
				}
			}
			if sourceData, ok := sourceCreateCache[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetCreateCache[modelName] = val
				}
			}
			if sourceData, ok := sourceImage[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetImage[modelName] = val
				}
			}
			if sourceData, ok := sourceAudio[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetAudio[modelName] = val
				}
			}
			if sourceData, ok := sourceAudioCompletion[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetAudioCompletion[modelName] = val
				}
			}
			if sourceData, ok := sourceBillingMode[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetBillingMode[modelName] = val
				}
			}
			if sourceData, ok := sourceBillingExpr[sourceGroup]; ok {
				if val, exists := sourceData[modelName]; exists {
					targetBillingExpr[modelName] = val
				}
			}
		}

		// 更新目标分组的配置
		d.groupModelPriceMap.Set(target, targetPrice)
		d.groupModelRatioMap.Set(target, targetRatio)
		d.groupCompletionRatioMap.Set(target, targetCompletion)
		d.groupCacheRatioMap.Set(target, targetCache)
		d.groupCreateCacheRatioMap.Set(target, targetCreateCache)
		d.groupImageRatioMap.Set(target, targetImage)
		d.groupAudioRatioMap.Set(target, targetAudio)
		d.groupAudioCompletionRatioMap.Set(target, targetAudioCompletion)
		d.groupBillingModeMap.Set(target, targetBillingMode)
		d.groupBillingExprMap.Set(target, targetBillingExpr)
	}

}

// syncFromGlobal 把全局配置复制到目标分组。
// modelNames 为空时同步全部全局配置，否则只同步指定的模型。
func (d *groupPricingDraft) syncFromGlobal(targetGroups []string, modelNames []string) {
	// 获取全局配置
	globalModelPrice := GetModelPriceCopy()
	globalModelRatio := GetModelRatioCopy()
	globalCompletionRatio := GetCompletionRatioCopy()
	globalCacheRatio := GetCacheRatioCopy()
	globalCreateCacheRatio := GetCreateCacheRatioCopy()
	globalImageRatio := GetImageRatioCopy()
	globalAudioRatio := GetAudioRatioCopy()
	globalAudioCompletionRatio := GetAudioCompletionRatioCopy()
	// 获取全局计费模式和表达式（包含 tiered_expr）
	globalBillingMode := billing_setting.GetBillingModeCopy()
	globalBillingExpr := billing_setting.GetBillingExprCopy()

	// 构建模型名称集合
	modelSet := make(map[string]struct{})
	if len(modelNames) > 0 {
		for _, name := range modelNames {
			modelSet[name] = struct{}{}
		}
	}

	for _, target := range targetGroups {
		// 获取目标分组的现有配置
		targetPrice := copyFloat64Map(d.groupModelPriceMap.ReadAll()[target])
		if targetPrice == nil {
			targetPrice = make(map[string]float64)
		}
		targetRatio := copyFloat64Map(d.groupModelRatioMap.ReadAll()[target])
		if targetRatio == nil {
			targetRatio = make(map[string]float64)
		}
		targetCompletion := copyFloat64Map(d.groupCompletionRatioMap.ReadAll()[target])
		if targetCompletion == nil {
			targetCompletion = make(map[string]float64)
		}
		targetCache := copyFloat64Map(d.groupCacheRatioMap.ReadAll()[target])
		if targetCache == nil {
			targetCache = make(map[string]float64)
		}
		targetCreateCache := copyFloat64Map(d.groupCreateCacheRatioMap.ReadAll()[target])
		if targetCreateCache == nil {
			targetCreateCache = make(map[string]float64)
		}
		targetImage := copyFloat64Map(d.groupImageRatioMap.ReadAll()[target])
		if targetImage == nil {
			targetImage = make(map[string]float64)
		}
		targetAudio := copyFloat64Map(d.groupAudioRatioMap.ReadAll()[target])
		if targetAudio == nil {
			targetAudio = make(map[string]float64)
		}
		targetAudioCompletion := copyFloat64Map(d.groupAudioCompletionRatioMap.ReadAll()[target])
		if targetAudioCompletion == nil {
			targetAudioCompletion = make(map[string]float64)
		}
		targetBillingMode := copyStringMap(d.groupBillingModeMap.ReadAll()[target])
		if targetBillingMode == nil {
			targetBillingMode = make(map[string]string)
		}
		targetBillingExpr := copyStringMap(d.groupBillingExprMap.ReadAll()[target])
		if targetBillingExpr == nil {
			targetBillingExpr = make(map[string]string)
		}

		// 同步全局配置到分组
		if len(modelNames) > 0 {
			// 只同步指定的模型
			for modelName := range modelSet {
				if val, ok := globalModelPrice[modelName]; ok {
					targetPrice[modelName] = val
				}
				if val, ok := globalModelRatio[modelName]; ok {
					targetRatio[modelName] = val
				}
				if val, ok := globalCompletionRatio[modelName]; ok {
					targetCompletion[modelName] = val
				}
				if val, ok := globalCacheRatio[modelName]; ok {
					targetCache[modelName] = val
				}
				if val, ok := globalCreateCacheRatio[modelName]; ok {
					targetCreateCache[modelName] = val
				}
				if val, ok := globalImageRatio[modelName]; ok {
					targetImage[modelName] = val
				}
				if val, ok := globalAudioRatio[modelName]; ok {
					targetAudio[modelName] = val
				}
				if val, ok := globalAudioCompletionRatio[modelName]; ok {
					targetAudioCompletion[modelName] = val
				}
				// 同步计费模式和表达式（支持 tiered_expr）
				if mode, ok := globalBillingMode[modelName]; ok && mode != "" {
					targetBillingMode[modelName] = mode
				} else if _, ok := globalModelPrice[modelName]; ok {
					targetBillingMode[modelName] = "per-request"
				} else {
					targetBillingMode[modelName] = "per-token"
				}
				if expr, ok := globalBillingExpr[modelName]; ok && expr != "" {
					targetBillingExpr[modelName] = expr
				}
			}
		} else {
			// 同步所有全局配置
			for modelName, val := range globalModelPrice {
				targetPrice[modelName] = val
			}
			for modelName, val := range globalModelRatio {
				targetRatio[modelName] = val
			}
			for modelName, val := range globalCompletionRatio {
				targetCompletion[modelName] = val
			}
			for modelName, val := range globalCacheRatio {
				targetCache[modelName] = val
			}
			for modelName, val := range globalCreateCacheRatio {
				targetCreateCache[modelName] = val
			}
			for modelName, val := range globalImageRatio {
				targetImage[modelName] = val
			}
			for modelName, val := range globalAudioRatio {
				targetAudio[modelName] = val
			}
			for modelName, val := range globalAudioCompletionRatio {
				targetAudioCompletion[modelName] = val
			}
			// 同步计费模式和表达式
			for modelName, mode := range globalBillingMode {
				if mode != "" {
					targetBillingMode[modelName] = mode
				}
			}
			for modelName, expr := range globalBillingExpr {
				if expr != "" {
					targetBillingExpr[modelName] = expr
				}
			}
			// 对于没有显式计费模式的模型，根据是否有 ModelPrice 推断
			for modelName := range globalModelPrice {
				if _, exists := targetBillingMode[modelName]; !exists {
					targetBillingMode[modelName] = "per-request"
				}
			}
			for modelName := range globalModelRatio {
				if _, exists := targetBillingMode[modelName]; !exists {
					targetBillingMode[modelName] = "per-token"
				}
			}
		}

		// 更新目标分组的配置
		d.groupModelPriceMap.Set(target, targetPrice)
		d.groupModelRatioMap.Set(target, targetRatio)
		d.groupCompletionRatioMap.Set(target, targetCompletion)
		d.groupCacheRatioMap.Set(target, targetCache)
		d.groupCreateCacheRatioMap.Set(target, targetCreateCache)
		d.groupImageRatioMap.Set(target, targetImage)
		d.groupAudioRatioMap.Set(target, targetAudio)
		d.groupAudioCompletionRatioMap.Set(target, targetAudioCompletion)
		d.groupBillingModeMap.Set(target, targetBillingMode)
		d.groupBillingExprMap.Set(target, targetBillingExpr)
	}

}

func copyFloat64Map(m map[string]float64) map[string]float64 {
	if m == nil {
		return nil
	}
	result := make(map[string]float64, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func init() {
	common.SysLog("group model pricing module initialized")
}
