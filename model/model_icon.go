package model

import (
	"strings"
	"sync"
	"time"
)

// ModelIconRule 描述模型管理中配置的图标及其名称匹配规则，
// 供前端在日志、模型列表等场景按模型名解析应展示的图标。
type ModelIconRule struct {
	ModelName string `json:"model_name"`
	Icon      string `json:"icon"`
	NameRule  int    `json:"name_rule"`
}

const modelIconRulesTTL = time.Minute

var (
	modelIconRules     []ModelIconRule
	modelIconRulesTime time.Time
	modelIconRulesLock sync.Mutex
)

// GetModelIconRules 返回所有已启用且配置了图标的模型规则。
// 模型自身未配置图标时回退到其供应商图标，与模型广场的展示优先级保持一致。
func GetModelIconRules() ([]ModelIconRule, error) {
	modelIconRulesLock.Lock()
	defer modelIconRulesLock.Unlock()

	if modelIconRules != nil && time.Since(modelIconRulesTime) < modelIconRulesTTL {
		return modelIconRules, nil
	}

	var metas []Model
	if err := ReadDB().Model(&Model{}).
		Select("model_name", "icon", "vendor_id", "name_rule").
		Where("status = ?", 1).
		Find(&metas).Error; err != nil {
		return nil, err
	}

	var vendors []Vendor
	if err := ReadDB().Model(&Vendor{}).Select("id", "icon").Find(&vendors).Error; err != nil {
		return nil, err
	}
	vendorIcons := make(map[int]string, len(vendors))
	for _, vendor := range vendors {
		vendorIcons[vendor.Id] = strings.TrimSpace(vendor.Icon)
	}

	rules := make([]ModelIconRule, 0, len(metas))
	for _, meta := range metas {
		modelName := strings.TrimSpace(meta.ModelName)
		if modelName == "" {
			continue
		}
		icon := strings.TrimSpace(meta.Icon)
		if icon == "" {
			icon = vendorIcons[meta.VendorID]
		}
		if icon == "" {
			continue
		}
		rules = append(rules, ModelIconRule{
			ModelName: modelName,
			Icon:      icon,
			NameRule:  meta.NameRule,
		})
	}

	modelIconRules = rules
	modelIconRulesTime = time.Now()
	return rules, nil
}
