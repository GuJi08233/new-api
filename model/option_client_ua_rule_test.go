package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 库里有一条无法编译的客户端 UA 规则(手改数据库或校验收紧)时，回放只跳过这一条，
// 其余规则照常生效，不能让整组规则在启动时静默失效。
func TestLoadOptionsSkipsOnlyInvalidClientUaRule(t *testing.T) {
	previousDB := DB
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		DB = previousDB
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
	t.Cleanup(operation_setting.SetClientUaRuleSettingForTest(nil))

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "client-ua-options.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	DB = db
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, DB.Create(&[]Option{
		{Key: operation_setting.ClientUaRuleSettingPrefix + "enabled", Value: "true"},
		{Key: operation_setting.ClientUaRuleSettingPrefix + "rules", Value: `[{"name":"broken","ua_allow":["re:("]},{"name":"opus-cli-only","model_regex":["^claude-opus"],"ua_allow":["(external, cli)"]}]`},
	}).Error)

	loadOptionsFromDatabase()

	rule := operation_setting.FindViolatedClientUaRule(operation_setting.ClientUaRequest{Model: "claude-opus-4-1", UserAgent: "Claude-Desktop/0.14.10"})
	require.NotNil(t, rule)
	assert.Equal(t, "opus-cli-only", rule.Name)
	assert.Nil(t, operation_setting.FindViolatedClientUaRule(operation_setting.ClientUaRequest{Model: "claude-opus-4-1", UserAgent: "claude-cli/2.1.266 (external, cli)"}))
}
