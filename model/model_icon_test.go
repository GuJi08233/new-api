package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetModelIconRulesCache() {
	modelIconRulesLock.Lock()
	defer modelIconRulesLock.Unlock()
	modelIconRules = nil
	modelIconRulesTime = time.Time{}
}

// 图标只能来自模型管理的显式配置：模型自身图标优先，其次是供应商图标，
// 两者都为空时不返回规则，前端才不会退回按模型名猜测厂商。
func TestGetModelIconRulesOnlyReturnsConfiguredIcons(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Model{}, &Vendor{}))
	require.NoError(t, DB.Exec("DELETE FROM models").Error)
	require.NoError(t, DB.Exec("DELETE FROM vendors").Error)
	resetModelIconRulesCache()
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DELETE FROM models").Error)
		require.NoError(t, DB.Exec("DELETE FROM vendors").Error)
		resetModelIconRulesCache()
	})

	vendor := Vendor{Name: "Meta", Icon: "Meta.Color"}
	require.NoError(t, vendor.Insert())
	iconlessVendor := Vendor{Name: "Unbranded"}
	require.NoError(t, iconlessVendor.Insert())

	for _, m := range []Model{
		{ModelName: "muse-spark-1.3-contributor", Icon: "Meta.Color", Status: 1, NameRule: NameRuleExact},
		{ModelName: "muse-", VendorID: vendor.Id, Status: 1, NameRule: NameRulePrefix},
		{ModelName: "no-icon-anywhere", VendorID: iconlessVendor.Id, Status: 1},
		{ModelName: "disabled-model", Icon: "OpenAI.Color", Status: 0},
	} {
		meta := m
		require.NoError(t, meta.Insert())
	}

	rules, err := GetModelIconRules()
	require.NoError(t, err)
	assert.ElementsMatch(t, []ModelIconRule{
		{ModelName: "muse-spark-1.3-contributor", Icon: "Meta.Color", NameRule: NameRuleExact},
		{ModelName: "muse-", Icon: "Meta.Color", NameRule: NameRulePrefix},
	}, rules)
}
