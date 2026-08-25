package setting

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var autoGroups = []string{
	"default",
}

var DefaultUseAutoGroup = false

func ContainsAutoGroup(group string) bool {
	for _, autoGroup := range GetAutoGroups() {
		if autoGroup == group {
			return true
		}
	}
	return false
}

func UpdateAutoGroupsByJsonString(jsonString string) error {
	autoGroups = make([]string, 0)
	return common.Unmarshal([]byte(jsonString), &autoGroups)
}

func AutoGroups2JsonString() string {
	jsonBytes, err := common.Marshal(autoGroups)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

// GetAutoGroups 返回 auto 分组的候选顺序：始终覆盖分组倍率中的全部分组，
// 顺序取管理员配置的排序，未出现在配置里的分组按名称排序追加在后，
// 配置中已不存在的分组名自动失效。管理员因此只需要调整顺序，
// 新增分组会自动进入序列末尾，无需手动添加。
func GetAutoGroups() []string {
	allGroups := ratio_setting.GetGroupRatioCopy()
	ordered := make([]string, 0, len(allGroups))
	seen := make(map[string]bool)
	for _, group := range autoGroups {
		if group == "" || group == "auto" || seen[group] {
			continue
		}
		if _, ok := allGroups[group]; !ok {
			continue
		}
		seen[group] = true
		ordered = append(ordered, group)
	}
	missing := make([]string, 0)
	for group := range allGroups {
		if group == "" || group == "auto" || seen[group] {
			continue
		}
		missing = append(missing, group)
	}
	sort.Strings(missing)
	return append(ordered, missing...)
}
