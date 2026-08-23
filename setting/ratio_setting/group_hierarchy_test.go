package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGroupSpecialGrantedGroups(t *testing.T) {
	settings := GetGroupRatioSetting().GroupSpecialUsableGroup
	prev, had := settings.Get("svip")
	settings.Set("svip", map[string]string{
		"vip":       "vip分组",
		"+:default": "默认分组",
		"banned":    "被移除的分组",
		"-:banned":  "",
	})
	t.Cleanup(func() {
		if had {
			settings.Set("svip", prev)
		} else {
			settings.Set("svip", map[string]string{})
		}
	})

	assert.ElementsMatch(t, []string{"default", "vip"}, GroupSpecialGrantedGroups("svip"))
	assert.True(t, GroupSpecialGrantsGroup("svip", "vip"))
	assert.True(t, GroupSpecialGrantsGroup("svip", "default"))
	// "-:" 移除优先于同名添加
	assert.False(t, GroupSpecialGrantsGroup("svip", "banned"))
	// 未配置特殊可选分组的分组不具备任何层级覆盖
	assert.False(t, GroupSpecialGrantsGroup("default", "vip"))
	assert.Nil(t, GroupSpecialGrantedGroups(""))
	assert.False(t, GroupSpecialGrantsGroup("svip", ""))
}
