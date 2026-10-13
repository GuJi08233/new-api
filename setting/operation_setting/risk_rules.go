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

// NormalizeRiskControlOption 在保存入口规范化并校验需要整体处理的配置项。
// 数据库回放不经过这里：回放时 UA 黑名单里一条坏正则只让那一条不生效，不拖垮整份名单。
func NormalizeRiskControlOption(key, value string) (string, error) {
	switch key {
	case RiskControlSettingPrefix + "auto_ban_rules":
		var rules []RiskAutoBanRule
		if err := common.UnmarshalJsonStr(value, &rules); err != nil || rules == nil {
			return value, fmt.Errorf("自动封禁规则必须是规则数组")
		}
		data, err := common.Marshal(NormalizeRiskAutoBanRules(rules))
		return string(data), err
	case RiskControlSettingPrefix + "ua_blacklist":
		var entries []string
		if err := common.UnmarshalJsonStr(value, &entries); err != nil || entries == nil {
			return value, fmt.Errorf("UA 黑名单必须是字符串数组")
		}
		for index, entry := range entries {
			if _, err := CompileUaPattern(entry); err != nil {
				return value, fmt.Errorf("UA 黑名单第 %d 项%s", index+1, err.Error())
			}
			entries[index] = strings.TrimSpace(entry)
		}
		data, err := common.Marshal(entries)
		return string(data), err
	}
	return value, nil
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
