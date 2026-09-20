package model

import (
	"maps"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPricingCacheBuildKeepsOneConfigurationSnapshot(t *testing.T) {
	values := setupPricingOptionsTest(t, false)
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousReadDB, previousMemory := RO_DB, common.MemoryCacheEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()
	RO_DB, common.MemoryCacheEnabled = nil, false
	t.Cleanup(func() {
		RO_DB, common.MemoryCacheEnabled = previousReadDB, previousMemory
		common.SetDatabaseTypes(previousMain, previousLog)
		initCol()
		InvalidatePricingCache()
	})
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}, &Model{}, &Vendor{}))
	vendor := Vendor{Name: "Snapshot Vendor", Status: 1}
	require.NoError(t, DB.Create(&vendor).Error)
	require.NoError(t, DB.Create(&Channel{Id: 1, Name: "pricing-snapshot", Type: 1, Key: "unused", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "snapshot-model", ChannelId: 1, Enabled: true}).Error)
	values["ModelRatio"] = `{"snapshot-model":1}`
	values["billing_setting.billing_mode"] = `{"snapshot-model":"tiered_expr"}`
	values["billing_setting.billing_expr"] = `{"snapshot-model":"tier(\"old\", p * 2)"}`
	require.NoError(t, UpdateModelPricingOptions(values))

	entered, releaseQuery := make(chan struct{}), make(chan struct{})
	var queryOnce, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseQuery) }) }
	defer release()
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register("pricing_snapshot_query", func(tx *gorm.DB) {
		if tx.Statement.Table == "models" {
			queryOnce.Do(func() {
				close(entered)
				<-releaseQuery
			})
		}
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Query().Remove("pricing_snapshot_query")) })
	pricingResult := make(chan []Pricing, 1)
	go func() { pricingResult <- GetPricing() }()
	select {
	case <-entered:
	case <-pricingResult:
		require.FailNow(t, "pricing cache did not rebuild through the fixture query")
	}

	// 重建缓存时，模式、表达式和单价必须来自同一套配置。
	canPublish := common.OptionMapRWMutex.TryLock()
	if canPublish {
		common.OptionMapRWMutex.Unlock()
	}
	assert.False(t, canPublish, "缓存构建期间不能插入另一套运行时定价")
	next := maps.Clone(values)
	next["ModelRatio"] = `{"snapshot-model":3}`
	next["billing_setting.billing_expr"] = `{"snapshot-model":"tier(\"new\", p * 4)"}`
	updated := make(chan error, 1)
	go func() { updated <- UpdateModelPricingOptions(next) }()
	release()
	previous := <-pricingResult
	require.Len(t, previous, 1)
	assert.Equal(t, float64(1), previous[0].ModelRatio)
	assert.Equal(t, "tiered_expr", previous[0].BillingMode)
	assert.Equal(t, `tier("old", p * 2)`, previous[0].BillingExpr)
	require.NoError(t, <-updated)

	current := GetPricing()
	require.Len(t, current, 1)
	assert.Equal(t, float64(3), current[0].ModelRatio)
	assert.Equal(t, `tier("new", p * 4)`, current[0].BillingExpr)
	assert.Equal(t, []PricingVendor{{ID: vendor.Id, Name: vendor.Name}}, GetVendors())
}
