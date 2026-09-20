package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// UpdateModelPricingOptions 将一次定价编辑中提交的配置校验后一起提交。
// 只写入调用方提交的键，未提交的键保持现状；任意表达式或倍率无效时，
// 数据库及运行中的定价都保持原值。
func UpdateModelPricingOptions(values map[string]string) error {
	if err := validateModelPricingOptions(values); err != nil {
		return err
	}
	return UpdateOptionsBulk(values)
}

var pricingOptionKeys = []string{"ModelPrice", "ModelRatio", "CompletionRatio", "CacheRatio", "CreateCacheRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio"}

func validateModelPricingOptions(values map[string]string) error {
	if len(values) == 0 {
		return fmt.Errorf("a pricing update must contain at least one pricing option")
	}
	globalKeys := map[string]bool{"billing_setting.billing_mode": true, billing_setting.BillingExprOptionKey: true}
	groupKeys := map[string]bool{"GroupBillingMode": true, "GroupBillingExpr": true}
	for _, key := range pricingOptionKeys {
		globalKeys[key] = true
		groupKeys["Group"+key] = true
	}
	scope := ""
	for key := range values {
		keyScope := ""
		if globalKeys[key] {
			keyScope = "global"
		} else if groupKeys[key] {
			keyScope = "group"
		} else {
			return fmt.Errorf("unknown pricing option %s", key)
		}
		if scope != "" && scope != keyScope {
			return fmt.Errorf("a pricing update must target either global or group pricing, not both")
		}
		scope = keyScope
	}
	groupMode := scope == "group"
	for _, key := range pricingOptionKeys {
		if groupMode {
			key = "Group" + key
		}
		raw, ok := values[key]
		if !ok {
			continue
		}
		var prices map[string]map[string]*float64
		if groupMode {
			if err := common.UnmarshalJsonStr(raw, &prices); err != nil || prices == nil {
				return fmt.Errorf("invalid pricing map %s", key)
			}
		} else {
			var modelPrices map[string]*float64
			if err := common.UnmarshalJsonStr(raw, &modelPrices); err != nil || modelPrices == nil {
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
	// 模式与表达式的一致性由 UpdateOptionsBulk 在写入锁内统一校验。
	return nil
}

var billingModeOptionKeys = []string{"billing_setting.billing_mode", billing_setting.BillingExprOptionKey, "GroupBillingMode", "GroupBillingExpr"}

// validateBillingModeConsistency 把即将写入的计费模式和表达式与当前运行配置合并，
// 保证每个 tiered_expr 模型都能找到表达式。无论通过单项、批量还是定价编辑接口写入，
// 都不能出现"模式已切换、表达式缺失"导致模型不可用的中间状态。
func validateBillingModeConsistency(values map[string]string) error {
	touched := false
	for _, key := range billingModeOptionKeys {
		if _, ok := values[key]; ok {
			touched = true
			break
		}
	}
	if !touched {
		return nil
	}

	common.OptionMapRWMutex.RLock()
	globalModes := billing_setting.GetBillingModeCopy()
	globalExprs := billing_setting.GetBillingExprCopy()
	groupModes := ratio_setting.GetGroupBillingModeCopy()
	groupExprs := ratio_setting.GetGroupBillingExprCopy()
	common.OptionMapRWMutex.RUnlock()

	if raw, ok := values["billing_setting.billing_mode"]; ok {
		globalModes = nil
		if err := common.UnmarshalJsonStr(raw, &globalModes); err != nil || globalModes == nil {
			return fmt.Errorf("invalid billing modes")
		}
	}
	if raw, ok := values[billing_setting.BillingExprOptionKey]; ok {
		globalExprs = nil
		if err := common.UnmarshalJsonStr(raw, &globalExprs); err != nil || globalExprs == nil {
			return fmt.Errorf("invalid billing expressions")
		}
	}
	if raw, ok := values["GroupBillingMode"]; ok {
		groupModes = nil
		if err := common.UnmarshalJsonStr(raw, &groupModes); err != nil || groupModes == nil {
			return fmt.Errorf("invalid billing modes")
		}
	}
	if raw, ok := values["GroupBillingExpr"]; ok {
		groupExprs = nil
		if err := common.UnmarshalJsonStr(raw, &groupExprs); err != nil || groupExprs == nil {
			return fmt.Errorf("invalid billing expressions")
		}
	}

	for name, mode := range globalModes {
		switch mode {
		case billing_setting.BillingModeTieredExpr:
			if strings.TrimSpace(globalExprs[name]) == "" {
				return fmt.Errorf("model %s is configured as tiered_expr but has no billing expression", name)
			}
		case billing_setting.BillingModeRatio, "per-token", "per-request":
		default:
			return fmt.Errorf("invalid billing mode %q for model %s", mode, name)
		}
	}
	for group, modes := range groupModes {
		for name, mode := range modes {
			switch mode {
			case billing_setting.BillingModeTieredExpr:
				if strings.TrimSpace(groupExprs[group][name]) == "" && strings.TrimSpace(globalExprs[name]) == "" {
					return fmt.Errorf("group %s model %s is configured as tiered_expr and still requires a global billing expression", group, name)
				}
			case billing_setting.BillingModeRatio, "per-token", "per-request":
			default:
				return fmt.Errorf("invalid billing mode %q for group %s model %s", mode, group, name)
			}
		}
	}
	return nil
}
