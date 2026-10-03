package model

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGameSettingOptionsTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousSnapshot := operation_setting.GetGameSetting()
	previousStaging, err := config.ConfigToMap(operation_setting.DefaultGameSettingForTest())
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		DB = previousDB
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("game_setting"), previousStaging))
		operation_setting.SetGameSettingForTest(previousSnapshot)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
	defaults, err := config.ConfigToMap(operation_setting.DefaultGameSetting())
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("game_setting"), defaults))
	operation_setting.SyncGameSetting()

	path := filepath.Join(t.TempDir(), "game-options.db")
	connections := make([]*gorm.DB, 0, 2)
	for range 2 {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
		// 另一连接在回调中同步尝试写入，锁冲突立即返回，不依赖等待或调度时机。
		require.NoError(t, db.Exec("PRAGMA busy_timeout = 0").Error)
		connections = append(connections, db)
	}
	DB = connections[0]
	require.NoError(t, DB.AutoMigrate(&Option{}))
	return connections[1]
}

func readGameSettingOptionsTest(t *testing.T) *operation_setting.GameSetting {
	t.Helper()
	var rows []Option
	require.NoError(t, DB.Find(&rows).Error)
	values := make(map[string]string)
	for _, row := range rows {
		if key, ok := strings.CutPrefix(row.Key, operation_setting.GameSettingPrefix); ok {
			values[key] = row.Value
		}
	}
	setting := operation_setting.DefaultGameSetting()
	require.NoError(t, config.UpdateConfigFromMap(setting, values))
	return setting
}

func TestGameSettingPatchSerializesRuleEditsAcrossConnections(t *testing.T) {
	for _, persisted := range []bool{true, false} {
		name := "defaults only"
		if persisted {
			name = "existing rule"
		}
		t.Run(name, func(t *testing.T) {
			other := setupGameSettingOptionsTest(t)
			key := operation_setting.GameSettingPrefix + "slots"
			initial := operation_setting.DefaultGameSetting().Slots
			if persisted {
				initial.Enabled = true
				encoded, err := common.Marshal(initial)
				require.NoError(t, err)
				require.NoError(t, DB.Create(&Option{Key: key, Value: string(encoded)}).Error)
			}
			stale := initial
			stale.MaxBet = 20
			staleJSON, err := common.Marshal(stale)
			require.NoError(t, err)
			var competingWriteErr error
			err = UpdateGameSettingFields(func(setting *operation_setting.GameSetting) (map[string]string, error) {
				// 此时首个节点已读取底稿，另一节点提交基于旧规则的上限更改。
				competingWriteErr = other.Save(&Option{Key: key, Value: string(staleJSON)}).Error
				setting.Slots.Enabled = !initial.Enabled
				encoded, err := common.Marshal(setting.Slots)
				return map[string]string{key: string(encoded)}, err
			})
			require.NoError(t, err)
			assert.Error(t, competingWriteErr, "另一连接不能在读取底稿和保存之间提交旧规则")

			// 首个事务完成后重试第二项改动，节点自己的运行时快照仍可落后。
			staleSnapshot := operation_setting.DefaultGameSetting()
			staleSnapshot.Slots = initial
			operation_setting.SetGameSettingForTest(staleSnapshot)
			require.NoError(t, UpdateGameSettingFields(func(setting *operation_setting.GameSetting) (map[string]string, error) {
				setting.Slots.MaxBet = 20
				encoded, err := common.Marshal(setting.Slots)
				return map[string]string{key: string(encoded)}, err
			}))
			stored := readGameSettingOptionsTest(t)
			assert.Equal(t, !initial.Enabled, stored.Slots.Enabled)
			assert.Equal(t, 20, stored.Slots.MaxBet)
			assert.Equal(t, stored.Slots, operation_setting.GetGameSetting().Slots)
		})
	}
}

func TestGameSettingPatchSerializesWithdrawalBounds(t *testing.T) {
	other := setupGameSettingOptionsTest(t)
	minKey := operation_setting.GameSettingPrefix + "withdraw_min_amount"
	maxKey := operation_setting.GameSettingPrefix + "withdraw_max_amount"
	var competingWriteErr error
	require.NoError(t, UpdateGameSettingFields(func(setting *operation_setting.GameSetting) (map[string]string, error) {
		competingWriteErr = other.Save(&Option{Key: minKey, Value: "20"}).Error
		setting.WithdrawMaxAmount = 10
		return map[string]string{maxKey: "10"}, nil
	}))
	assert.Error(t, competingWriteErr, "另一节点不能同时用旧上限通过提现下限校验")
	// 另一节点重新提交时必须按已经降低的上限拒绝，不能生成 min > max 的配置。
	require.Error(t, UpdateOption(minKey, "20"))
	stored := readGameSettingOptionsTest(t)
	assert.Equal(t, 1, stored.WithdrawMinAmount)
	assert.Equal(t, 10, stored.WithdrawMaxAmount)
}

func TestGameSettingWritesValidateDatabaseBoundsAndPublishCompleteSnapshot(t *testing.T) {
	minKey := operation_setting.GameSettingPrefix + "withdraw_min_amount"
	maxKey := operation_setting.GameSettingPrefix + "withdraw_max_amount"
	for _, entry := range []string{"single option", "bulk options", "game patch"} {
		t.Run(entry, func(t *testing.T) {
			setupGameSettingOptionsTest(t)
			// 数据库已经被另一节点更新，但本节点运行时仍是默认上限 100。
			require.NoError(t, DB.Create(&Option{Key: maxKey, Value: "10"}).Error)
			write := func(value string) error {
				switch entry {
				case "single option":
					return UpdateOption(minKey, value)
				case "bulk options":
					return UpdateOptionsBulk(map[string]string{minKey: value, "game_test_unrelated_option": "saved"})
				default:
					return UpdateGameSettingFields(func(*operation_setting.GameSetting) (map[string]string, error) {
						return map[string]string{minKey: value}, nil
					})
				}
			}
			require.Error(t, write("20"))
			var rows int64
			require.NoError(t, DB.Model(&Option{}).Where("key IN ?", []string{minKey, "game_test_unrelated_option"}).Count(&rows).Error)
			assert.Zero(t, rows, "校验失败不能提交部分配置")
			assert.Equal(t, 1, operation_setting.GetGameSetting().WithdrawMinAmount)
			assert.Equal(t, 100, operation_setting.GetGameSetting().WithdrawMaxAmount)

			require.NoError(t, write("5"))
			stored := readGameSettingOptionsTest(t)
			assert.Equal(t, 5, stored.WithdrawMinAmount)
			assert.Equal(t, 10, stored.WithdrawMaxAmount)
			assert.Equal(t, stored.WithdrawMinAmount, operation_setting.GetGameSetting().WithdrawMinAmount)
			assert.Equal(t, stored.WithdrawMaxAmount, operation_setting.GetGameSetting().WithdrawMaxAmount,
				"成功发布必须包含数据库中已有的新上限，不能拼接陈旧运行时配置")
		})
	}
}

func TestGameSettingBulkAcceptsValidWithdrawalBoundsTogether(t *testing.T) {
	setupGameSettingOptionsTest(t)
	require.NoError(t, UpdateOptionsBulk(map[string]string{
		operation_setting.GameSettingPrefix + "withdraw_min_amount": "120",
		operation_setting.GameSettingPrefix + "withdraw_max_amount": "200",
	}))
	stored := readGameSettingOptionsTest(t)
	assert.Equal(t, 120, stored.WithdrawMinAmount)
	assert.Equal(t, 200, stored.WithdrawMaxAmount)
	assert.Equal(t, stored.WithdrawMinAmount, operation_setting.GetGameSetting().WithdrawMinAmount)
	assert.Equal(t, stored.WithdrawMaxAmount, operation_setting.GetGameSetting().WithdrawMaxAmount)
}

func TestGameSettingFirstInvalidPatchLeavesDefaultsUnchanged(t *testing.T) {
	setupGameSettingOptionsTest(t)
	require.Error(t, UpdateGameSettingFields(func(*operation_setting.GameSetting) (map[string]string, error) {
		return map[string]string{operation_setting.GameSettingPrefix + "withdraw_min_amount": "101"}, nil
	}))
	var rows int64
	require.NoError(t, DB.Model(&Option{}).Count(&rows).Error)
	assert.Zero(t, rows, "首次无效保存不能留下配置项或协调写入时创建的行")
	assert.Equal(t, operation_setting.DefaultGameSetting(), operation_setting.GetGameSetting())
}
