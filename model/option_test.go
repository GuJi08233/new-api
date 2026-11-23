package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestUpdateOptionMapAppliesBooleanOptionsWithoutEnabledSuffix guards the
// dispatch gate in updateOptionMap: boolean options are only applied when the
// key ends with "Enabled" or is listed explicitly. A key that satisfies
// neither is silently persisted to the database while the in-memory flag stays
// at its default, so the feature it controls never turns on.
func TestUpdateOptionMapAppliesBooleanOptionsWithoutEnabledSuffix(t *testing.T) {
	original := common.IsHideModelMappingForUserEnabled()
	t.Cleanup(func() { common.SetHideModelMappingForUser(original) })
	common.SetHideModelMappingForUser(false)

	// updateOptionMap writes into the shared option map, which only exists
	// after InitOptionMap in a running server.
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
		t.Cleanup(func() { common.OptionMap = nil })
	}

	require.NoError(t, updateOptionMap("HideModelMappingForUser", "true"))
	assert.True(t, common.IsHideModelMappingForUserEnabled())
	assert.Equal(t, "true", common.OptionMap["HideModelMappingForUser"])

	require.NoError(t, updateOptionMap("HideModelMappingForUser", "false"))
	assert.False(t, common.IsHideModelMappingForUserEnabled())
}

func TestLoadOptionsUpgradesLegacyModelRequestRateLimitGroup(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&Option{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		DB = previousDB
		_ = sqlDB.Close()
	})

	prevGroups := setting.ModelRequestRateLimitGroup
	prevTokens := setting.ModelRequestRateLimitGroupTokens
	prevConc := setting.ModelRequestRateLimitGroupConcurrency
	common.OptionMapRWMutex.RLock()
	prevOptionMap := common.OptionMap
	common.OptionMapRWMutex.RUnlock()
	t.Cleanup(func() {
		setting.ModelRequestRateLimitMutex.Lock()
		setting.ModelRequestRateLimitGroup = prevGroups
		setting.ModelRequestRateLimitGroupTokens = prevTokens
		setting.ModelRequestRateLimitGroupConcurrency = prevConc
		setting.ModelRequestRateLimitMutex.Unlock()
		common.OptionMapRWMutex.Lock()
		common.OptionMap = prevOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	require.NoError(t, DB.Create(&Option{
		Key:   "ModelRequestRateLimitGroup",
		Value: `{"default":[200,100],"vip":[0,1000]}`,
	}).Error)

	InitOptionMap()

	var saved Option
	require.NoError(t, DB.Where(&Option{Key: "ModelRequestRateLimitGroup"}).First(&saved).Error)
	_, changed, err := setting.NormalizeModelRequestRateLimitGroupJSON(saved.Value)
	require.NoError(t, err)
	assert.False(t, changed, "legacy 2-element JSON in DB must be upgraded to 4-element format")

	total, success, tokens, conc, found := setting.GetGroupRateLimitConfig("default")
	require.True(t, found)
	assert.Equal(t, 200, total)
	assert.Equal(t, 100, success)
	assert.EqualValues(t, 0, tokens)
	assert.Equal(t, 0, conc)

	total, success, tokens, conc, found = setting.GetGroupRateLimitConfig("vip")
	require.True(t, found)
	assert.Equal(t, 0, total)
	assert.Equal(t, 1000, success)
	assert.EqualValues(t, 0, tokens)
	assert.Equal(t, 0, conc)
}
