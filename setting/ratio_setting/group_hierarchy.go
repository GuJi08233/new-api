package ratio_setting

import (
	"sort"
	"strings"
)

// GroupSpecialGrantedGroups returns the groups that userGroup's special
// usable-group configuration explicitly grants (plain "x" or "+:x" entries,
// minus "-:x" removals). An entry here is the admin-declared group hierarchy:
// it means userGroup already covers that group. The global user-usable-group
// list is intentionally ignored — it marks groups as publicly selectable for
// everyone and says nothing about one group outranking another.
func GroupSpecialGrantedGroups(userGroup string) []string {
	userGroup = strings.TrimSpace(userGroup)
	if userGroup == "" {
		return nil
	}
	special, ok := GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
	if !ok || len(special) == 0 {
		return nil
	}
	granted := make(map[string]struct{}, len(special))
	for key := range special {
		if strings.HasPrefix(key, "-:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(key, "+:"))
		if name != "" {
			granted[name] = struct{}{}
		}
	}
	for key := range special {
		if strings.HasPrefix(key, "-:") {
			delete(granted, strings.TrimSpace(strings.TrimPrefix(key, "-:")))
		}
	}
	if len(granted) == 0 {
		return nil
	}
	result := make([]string, 0, len(granted))
	for name := range granted {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// GroupSpecialGrantsGroup reports whether userGroup's special usable-group
// configuration already covers targetGroup.
func GroupSpecialGrantsGroup(userGroup, targetGroup string) bool {
	targetGroup = strings.TrimSpace(targetGroup)
	if targetGroup == "" {
		return false
	}
	for _, name := range GroupSpecialGrantedGroups(userGroup) {
		if name == targetGroup {
			return true
		}
	}
	return false
}
