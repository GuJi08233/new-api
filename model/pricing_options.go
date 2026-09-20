package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// UpdateModelPricingOptions 将一次定价编辑的全部配置校验后一起提交。
// 任意表达式或倍率无效时，数据库及运行中的定价都保持原值。
func UpdateModelPricingOptions(values map[string]string) error {
	if err := validateModelPricingOptions(values); err != nil {
		return err
	}
	return UpdateOptionsBulk(values)
}

func validateModelPricingOptions(values map[string]string) error {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	_, groupMode := values["GroupBillingMode"]
	prefix := ""
	modeKey, exprKey := "billing_setting.billing_mode", billing_setting.BillingExprOptionKey
	if groupMode {
		prefix = "Group"
		modeKey, exprKey = "GroupBillingMode", "GroupBillingExpr"
	}
	priceKeys := []string{"ModelPrice", "ModelRatio", "CompletionRatio", "CacheRatio", "CreateCacheRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio"}
	if len(values) != len(priceKeys)+2 {
		return fmt.Errorf("a pricing update must contain all prices, billing modes and expressions for one scope")
	}
	for _, key := range append([]string{modeKey, exprKey}, priceKeys...) {
		if key != modeKey && key != exprKey {
			key = prefix + key
		}
		if _, ok := values[key]; !ok {
			return fmt.Errorf("missing pricing option %s", key)
		}
	}
	for _, key := range priceKeys {
		key = prefix + key
		var prices map[string]map[string]*float64
		if groupMode {
			if err := common.UnmarshalJsonStr(values[key], &prices); err != nil || prices == nil {
				return fmt.Errorf("invalid pricing map %s", key)
			}
		} else {
			var modelPrices map[string]*float64
			if err := common.UnmarshalJsonStr(values[key], &modelPrices); err != nil || modelPrices == nil {
				return fmt.Errorf("invalid pricing map %s", key)
			}
			prices = map[string]map[string]*float64{"": modelPrices}
		}
		for group, modelPrices := range prices {
			if modelPrices == nil {
				return fmt.Errorf("invalid pricing map %s for group %s", key, group)
			}
			for name, price := range modelPrices {
				if price == nil || *price < 0 {
					return fmt.Errorf("%s for model %s must be a non-negative number", key, name)
				}
			}
		}
	}

	var modes, expressions map[string]map[string]string
	if groupMode {
		if err := common.UnmarshalJsonStr(values[modeKey], &modes); err != nil || modes == nil {
			return fmt.Errorf("invalid billing modes")
		}
		if err := common.UnmarshalJsonStr(values[exprKey], &expressions); err != nil || expressions == nil {
			return fmt.Errorf("invalid billing expressions")
		}
	} else {
		var modelModes, modelExpressions map[string]string
		if err := common.UnmarshalJsonStr(values[modeKey], &modelModes); err != nil || modelModes == nil {
			return fmt.Errorf("invalid billing modes")
		}
		if err := common.UnmarshalJsonStr(values[exprKey], &modelExpressions); err != nil || modelExpressions == nil {
			return fmt.Errorf("invalid billing expressions")
		}
		modes = map[string]map[string]string{"": modelModes}
		expressions = map[string]map[string]string{"": modelExpressions}
		// 分组可以沿用全局表达式，删除全局配置前必须检查这些依赖。
		for group, groupModes := range ratio_setting.GetGroupBillingModeCopy() {
			for name, mode := range groupModes {
				if mode == billing_setting.BillingModeTieredExpr &&
					strings.TrimSpace(ratio_setting.GetGroupBillingExpr(group, name)) == "" &&
					strings.TrimSpace(modelExpressions[name]) == "" {
					return fmt.Errorf("group %s model %s still requires a global billing expression", group, name)
				}
			}
		}
	}
	for group, modelModes := range modes {
		for name, mode := range modelModes {
			switch mode {
			case billing_setting.BillingModeTieredExpr:
				expr := expressions[group][name]
				if groupMode && strings.TrimSpace(expr) == "" {
					expr, _ = billing_setting.GetBillingExpr(name)
				}
				if strings.TrimSpace(expr) == "" {
					return fmt.Errorf("model %s is configured as tiered_expr but has no billing expression", name)
				}
			case billing_setting.BillingModeRatio, "per-token", "per-request":
			default:
				return fmt.Errorf("invalid billing mode %q for model %s", mode, name)
			}
		}
	}
	return nil
}
