package setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAutoGroupsAppendsUnlistedGroupsInOrder(t *testing.T) {
	savedAutoGroups := AutoGroups2JsonString()
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateAutoGroupsByJsonString(savedAutoGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(
		`{"default":1,"vip":0.8,"svip":0.5,"embedding":0.2,"auto":1}`))

	// 配置顺序在前，未配置的分组按名称追加，"auto" 自身与重复项被剔除
	require.NoError(t, UpdateAutoGroupsByJsonString(`["vip","default","vip","auto"]`))
	assert.Equal(t, []string{"vip", "default", "embedding", "svip"}, GetAutoGroups())

	// 空配置时退化为全部分组按名称排序，管理员无需手动添加
	require.NoError(t, UpdateAutoGroupsByJsonString(`[]`))
	assert.Equal(t, []string{"default", "embedding", "svip", "vip"}, GetAutoGroups())

	// 配置里已不存在于分组倍率的名字自动失效，序列始终等于现存全部分组
	require.NoError(t, UpdateAutoGroupsByJsonString(`["legacy","svip"]`))
	assert.Equal(t, []string{"svip", "default", "embedding", "vip"}, GetAutoGroups())
}
