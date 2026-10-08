package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTradeSettingOptionsTest(t *testing.T) {
	t.Helper()
	previousDB := DB
	previousSnapshot := operation_setting.GetTradeSetting()
	previousStaging, err := config.ConfigToMap(previousSnapshot)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		DB = previousDB
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("trade_setting"), previousStaging))
		operation_setting.SetTradeSettingForTest(previousSnapshot)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
	defaults, err := config.ConfigToMap(operation_setting.DefaultTradeSetting())
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("trade_setting"), defaults))
	operation_setting.SyncTradeSetting()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "trade-options.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	DB = db
	require.NoError(t, DB.AutoMigrate(&Option{}))
}

// 管理员保存的模拟盘配置先校验再落库，保存成功后请求立刻读到新值；非法值什么都不写。
func TestTradeSettingOptionsAreValidatedAndPublished(t *testing.T) {
	setupTradeSettingOptionsTest(t)

	require.Error(t, UpdateOption(operation_setting.TradeSettingPrefix+"fee_bps", "1000"))
	require.Error(t, UpdateOptionsBulk(map[string]string{
		operation_setting.TradeSettingPrefix + "enabled": "true",
		operation_setting.TradeSettingPrefix + "symbols": `["BTCUSDT","FOOUSDT"]`,
	}))
	var rows int64
	require.NoError(t, DB.Model(&Option{}).Count(&rows).Error)
	assert.Zero(t, rows)
	assert.Equal(t, operation_setting.DefaultTradeSetting(), operation_setting.GetTradeSetting())

	require.NoError(t, UpdateOption(operation_setting.TradeSettingPrefix+"fee_bps", "20"))
	require.NoError(t, UpdateOptionsBulk(map[string]string{
		operation_setting.TradeSettingPrefix + "enabled": "true",
		operation_setting.TradeSettingPrefix + "symbols": `["ETHUSDT","NVDABUSDT"]`,
	}))
	published := operation_setting.GetTradeSetting()
	assert.Equal(t, 20, published.FeeBps)
	assert.True(t, published.Enabled)
	assert.Equal(t, []string{"ETHUSDT", "NVDABUSDT"}, published.Symbols)
}
