package operation_setting

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var riskRuleIdPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// NormalizeRiskAutoBanRules 给旧规则补充确定的标识。首次保存后标识随规则持久化，
// 后续更改阈值或排序不会改变标识；版本另由规则内容决定。
func NormalizeRiskAutoBanRules(rules []RiskAutoBanRule) []RiskAutoBanRule {
	result := append([]RiskAutoBanRule{}, rules...)
	used := make(map[string]bool, len(result))
	for _, rule := range result {
		if rule.Id != "" {
			used[rule.Id] = true
		}
	}
	for index := range result {
		if result[index].Id == "" {
			id := fmt.Sprintf("legacy-%d", index+1)
			for used[id] {
				id += "_"
			}
			result[index].Id = id
			used[id] = true
		}
		result[index].Name = strings.TrimSpace(result[index].Name)
	}
	return result
}

func NormalizeRiskControlOption(key, value string) (string, error) {
	if key != RiskControlSettingPrefix+"auto_ban_rules" {
		return value, nil
	}
	var rules []RiskAutoBanRule
	if err := common.UnmarshalJsonStr(value, &rules); err != nil || rules == nil {
		return value, fmt.Errorf("自动封禁规则必须是规则数组")
	}
	data, err := common.Marshal(NormalizeRiskAutoBanRules(rules))
	return string(data), err
}

// RiskRuleVersion 是规则配置的内容指纹，用于区分同一规则的不同历史版本。
func RiskRuleVersion(configuration any) string {
	data, err := common.Marshal(configuration)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:8])
}
