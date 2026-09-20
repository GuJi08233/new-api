package model

import (
	"errors"
	"maps"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPricingOptionsTest(t *testing.T, group bool) map[string]string {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&Option{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)

	previous := map[string]string{
		"ModelPrice":                ratio_setting.ModelPrice2JSONString(),
		"ModelRatio":                ratio_setting.ModelRatio2JSONString(),
		"CompletionRatio":           ratio_setting.CompletionRatio2JSONString(),
		"CacheRatio":                ratio_setting.CacheRatio2JSONString(),
		"CreateCacheRatio":          ratio_setting.CreateCacheRatio2JSONString(),
		"ImageRatio":                ratio_setting.ImageRatio2JSONString(),
		"AudioRatio":                ratio_setting.AudioRatio2JSONString(),
		"AudioCompletionRatio":      ratio_setting.AudioCompletionRatio2JSONString(),
		"GroupModelPrice":           ratio_setting.GroupModelPrice2JSONString(),
		"GroupModelRatio":           ratio_setting.GroupModelRatio2JSONString(),
		"GroupCompletionRatio":      ratio_setting.GroupCompletionRatio2JSONString(),
		"GroupCacheRatio":           ratio_setting.GroupCacheRatio2JSONString(),
		"GroupCreateCacheRatio":     ratio_setting.GroupCreateCacheRatio2JSONString(),
		"GroupImageRatio":           ratio_setting.GroupImageRatio2JSONString(),
		"GroupAudioRatio":           ratio_setting.GroupAudioRatio2JSONString(),
		"GroupAudioCompletionRatio": ratio_setting.GroupAudioCompletionRatio2JSONString(),
		"GroupBillingMode":          ratio_setting.GroupBillingMode2JSONString(),
		"GroupBillingExpr":          ratio_setting.GroupBillingExpr2JSONString(),
	}
	modes, err := common.Marshal(billing_setting.GetBillingModeCopy())
	require.NoError(t, err)
	exprs, err := common.Marshal(billing_setting.GetBillingExprCopy())
	require.NoError(t, err)
	previous["billing_setting.billing_mode"] = string(modes)
	previous[billing_setting.BillingExprOptionKey] = string(exprs)
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		for key, value := range previous {
			require.NoError(t, updateOptionMap(key, value))
		}
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		DB = previousDB
		require.NoError(t, sqlDB.Close())
	})
	for key := range previous {
		require.NoError(t, updateOptionMap(key, "{}"))
	}
	common.OptionMapRWMutex.Lock()
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()

	prefix := ""
	modeKey, exprKey := "billing_setting.billing_mode", billing_setting.BillingExprOptionKey
	if group {
		prefix = "Group"
		modeKey, exprKey = "GroupBillingMode", "GroupBillingExpr"
	}
	values := map[string]string{modeKey: "{}", exprKey: "{}"}
	for _, key := range []string{"ModelPrice", "ModelRatio", "CompletionRatio", "CacheRatio", "CreateCacheRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio"} {
		values[prefix+key] = "{}"
	}
	require.NoError(t, UpdateModelPricingOptions(values))
	return values
}

func TestModelPricingUpdateRejectsInvalidBatchWithoutChangingPrices(t *testing.T) {
	for _, tc := range []struct {
		name  string
		group bool
		key   string
		value string
	}{
		{"invalid global expression", false, "billing_setting.billing_expr", `{"paid":"tier(\"base\", p * )"}`},
		{"invalid group expression", true, "GroupBillingExpr", `{"vip":{"paid":"tier(\"base\", p * )"}}`},
		{"missing expression", false, "billing_setting.billing_mode", `{"paid":"tiered_expr"}`},
		{"invalid numeric value", false, "ModelRatio", `{"paid":"expensive"}`},
		{"null numeric value", true, "GroupModelRatio", `{"vip":{"paid":null}}`},
		{"negative numeric value", false, "ModelRatio", `{"paid":-1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := setupPricingOptionsTest(t, tc.group)
			next := maps.Clone(before)
			next[tc.key] = tc.value
			if tc.key == billing_setting.BillingExprOptionKey {
				next["billing_setting.billing_mode"] = `{"paid":"tiered_expr"}`
			} else if tc.key == "GroupBillingExpr" {
				next["GroupBillingMode"] = `{"vip":{"paid":"tiered_expr"}}`
			}
			require.Error(t, UpdateModelPricingOptions(next))
			var stored []Option
			require.NoError(t, DB.Find(&stored).Error)
			actual := make(map[string]string)
			for _, option := range stored {
				actual[option.Key] = option.Value
			}
			assert.Equal(t, before, actual)
			common.OptionMapRWMutex.RLock()
			assert.Equal(t, before, common.OptionMap)
			common.OptionMapRWMutex.RUnlock()
		})
	}
}

func TestModelPricingUpdateRollsBackDatabaseFailure(t *testing.T) {
	before := setupPricingOptionsTest(t, false)
	next := maps.Clone(before)
	next["ModelRatio"] = `{"paid":2}`
	next["billing_setting.billing_mode"] = `{"paid":"tiered_expr"}`
	next[billing_setting.BillingExprOptionKey] = `{"paid":"tier(\"base\", p * 2)"}`
	writes := 0
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("pricing_failure", func(tx *gorm.DB) {
		writes++
		if writes == 2 {
			tx.AddError(errors.New("pricing write failed"))
		}
	}))
	require.ErrorContains(t, UpdateModelPricingOptions(next), "pricing write failed")
	var stored []Option
	require.NoError(t, DB.Find(&stored).Error)
	for _, option := range stored {
		assert.Equal(t, before[option.Key], option.Value)
	}
	assert.Equal(t, billing_setting.BillingModeRatio, billing_setting.GetBillingMode("paid"))
	_, exists := billing_setting.GetBillingExpr("paid")
	assert.False(t, exists)
	assert.NotContains(t, ratio_setting.GetModelRatioCopy(), "paid")
}

func TestModelPricingUpdateCommitsExpressionAndZeroPrices(t *testing.T) {
	for _, group := range []bool{false, true} {
		t.Run(map[bool]string{false: "global", true: "group"}[group], func(t *testing.T) {
			values := setupPricingOptionsTest(t, group)
			if group {
				values["GroupBillingMode"] = `{"vip":{"paid":"tiered_expr"}}`
				values["GroupBillingExpr"] = `{"vip":{"paid":"tier(\"base\", p * 2)"}}`
				values["GroupModelPrice"] = `{"vip":{"free":0}}`
			} else {
				values["billing_setting.billing_mode"] = `{"paid":"tiered_expr"}`
				values[billing_setting.BillingExprOptionKey] = `{"paid":"tier(\"base\", p * 2)"}`
				values["ModelPrice"] = `{"free":0}`
			}
			require.NoError(t, UpdateModelPricingOptions(values))
			if group {
				assert.Equal(t, "tiered_expr", ratio_setting.GetGroupBillingMode("vip", "paid"))
				assert.Equal(t, `tier("base", p * 2)`, ratio_setting.GetGroupBillingExpr("vip", "paid"))
				price, ok := ratio_setting.GetGroupModelPrice("vip", "free")
				assert.True(t, ok)
				assert.Zero(t, price)
			} else {
				assert.Equal(t, "tiered_expr", billing_setting.GetBillingMode("paid"))
				expr, ok := billing_setting.GetBillingExpr("paid")
				assert.True(t, ok)
				assert.Equal(t, `tier("base", p * 2)`, expr)
				price, ok := ratio_setting.GetModelPrice("free", false)
				assert.True(t, ok)
				assert.Zero(t, price)
			}
		})
	}
}

func TestModelPricingUpdatePreservesInheritedGroupExpression(t *testing.T) {
	values := setupPricingOptionsTest(t, false)
	values["billing_setting.billing_mode"] = `{"paid":"tiered_expr"}`
	values[billing_setting.BillingExprOptionKey] = `{"paid":"tier(\"base\", p * 2)"}`
	require.NoError(t, UpdateModelPricingOptions(values))
	require.NoError(t, ratio_setting.UpdateGroupBillingModeByJSONString(`{"vip":{"paid":"tiered_expr"}}`))

	values["billing_setting.billing_mode"] = "{}"
	values[billing_setting.BillingExprOptionKey] = "{}"
	require.ErrorContains(t, UpdateModelPricingOptions(values), "still requires a global billing expression")
	expr, ok := billing_setting.GetBillingExpr("paid")
	assert.True(t, ok)
	assert.Equal(t, `tier("base", p * 2)`, expr)

	// 分组改用独立表达式后，可以移除不再被引用的全局表达式。
	require.NoError(t, ratio_setting.UpdateGroupBillingExprByJSONString(`{"vip":{"paid":"tier(\"group\", p * 3)"}}`))
	require.NoError(t, UpdateModelPricingOptions(values))
	_, ok = billing_setting.GetBillingExpr("paid")
	assert.False(t, ok)
	assert.Equal(t, `tier("group", p * 3)`, ratio_setting.GetGroupBillingExpr("vip", "paid"))
}
