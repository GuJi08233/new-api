package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openChannelRecoveryStateTestDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&ChannelRecoveryState{}))
	return db
}

func setupChannelRecoveryStateTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "channel-recovery.db")
	originalDB, originalRODB := DB, RO_DB
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		DB, RO_DB = originalDB, originalRODB
		common.RedisEnabled = originalRedisEnabled
	})
	DB = openChannelRecoveryStateTestDB(t, path)
	RO_DB = nil
	common.RedisEnabled = false
	return path
}

func TestChannelRecoveryStatePersistsAcrossDatabaseConnections(t *testing.T) {
	path := setupChannelRecoveryStateTestDB(t)
	const channelID = 81
	const testedAt int64 = 1_800_000_000
	target := ChannelRecoveryTargetID("recovery-key", false)
	require.NoError(t, RecordChannelRecoveryTest(channelID, target, testedAt))

	// 关闭原连接再重新打开，模拟下一主节点或进程重启，不能依赖进程内缓存。
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	DB = openChannelRecoveryStateTestDB(t, path)
	// 只读副本故意为空；共享调度状态必须从主库读取。
	RO_DB = openChannelRecoveryStateTestDB(t, ":memory:")

	lastTests, err := GetChannelRecoveryLastTests()
	require.NoError(t, err)
	assert.Equal(t, map[int]map[string]int64{channelID: {target: testedAt}}, lastTests)
}

func TestRecordChannelRecoveryTestPreservesNewestTime(t *testing.T) {
	setupChannelRecoveryStateTestDB(t)
	target := ChannelRecoveryTargetID("recovery-key", false)
	const channelID = 82
	const start int64 = 1_800_000_000
	tests := []struct {
		name     string
		testedAt int64
		want     int64
	}{
		{name: "first probe is recorded", testedAt: start, want: start},
		{name: "newer probe advances the time", testedAt: start + 60, want: start + 60},
		{name: "duplicate report is idempotent", testedAt: start + 60, want: start + 60},
		{name: "late older report does not move time backward", testedAt: start + 30, want: start + 60},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, RecordChannelRecoveryTest(channelID, target, tc.testedAt))
			lastTests, err := GetChannelRecoveryLastTests()
			require.NoError(t, err)
			assert.Equal(t, tc.want, lastTests[channelID][target])
		})
	}
	var records int64
	require.NoError(t, DB.Model(&ChannelRecoveryState{}).Count(&records).Error)
	assert.EqualValues(t, 1, records)
}

func TestChannelRecoveryStateFollowsKeysAcrossReordering(t *testing.T) {
	setupChannelRecoveryStateTestDB(t)
	channel := Channel{Id: 83, Key: "first-secret\nsecond-secret"}
	const start int64 = 1_800_000_000
	for index, key := range channel.GetKeys() {
		require.NoError(t, RecordChannelRecoveryTest(channel.Id, ChannelRecoveryTargetID(key, false), start+int64(index)))
	}
	channelTarget := ChannelRecoveryTargetID("first-secret", true)
	require.NoError(t, RecordChannelRecoveryTest(channel.Id, channelTarget, start+2))
	require.NoError(t, RecordChannelRecoveryTest(channel.Id+1, ChannelRecoveryTargetID("first-secret", false), start+3))

	channel.Key = "second-secret\nfirst-secret\nreplacement-secret"
	lastTests, err := GetChannelRecoveryLastTests()
	require.NoError(t, err)
	keys := channel.GetKeys()
	assert.Equal(t, start+1, lastTests[channel.Id][ChannelRecoveryTargetID(keys[0], false)])
	assert.Equal(t, start, lastTests[channel.Id][ChannelRecoveryTargetID(keys[1], false)])
	assert.NotContains(t, lastTests[channel.Id], ChannelRecoveryTargetID(keys[2], false))
	assert.Equal(t, start+2, lastTests[channel.Id][channelTarget], "渠道目标与同一密钥的单独恢复目标互不影响")
	assert.Equal(t, start+3, lastTests[channel.Id+1][ChannelRecoveryTargetID("first-secret", false)], "不同渠道使用相同密钥时仍独立调度")

	var states []ChannelRecoveryState
	require.NoError(t, DB.Find(&states).Error)
	require.Len(t, states, 4)
	for _, state := range states {
		assert.Len(t, state.TargetKey, 64)
		assert.NotContains(t, state.TargetKey, "first-secret")
		assert.NotContains(t, state.TargetKey, "second-secret")
	}
}

func TestDeleteExpiredChannelRecoveryStatesPreservesRecentProbes(t *testing.T) {
	setupChannelRecoveryStateTestDB(t)
	const channelID = 84
	const before int64 = 1_800_000_000
	expiredTarget := ChannelRecoveryTargetID("expired-secret", false)
	boundaryTarget := ChannelRecoveryTargetID("boundary-secret", false)
	recentTarget := ChannelRecoveryTargetID("recent-secret", false)
	require.NoError(t, RecordChannelRecoveryTest(channelID, expiredTarget, before-1))
	require.NoError(t, RecordChannelRecoveryTest(channelID, boundaryTarget, before))
	require.NoError(t, RecordChannelRecoveryTest(channelID, recentTarget, before+1))

	require.NoError(t, DeleteExpiredChannelRecoveryStates(before))
	lastTests, err := GetChannelRecoveryLastTests()
	require.NoError(t, err)
	assert.Equal(t, map[int]map[string]int64{
		channelID: {boundaryTarget: before, recentTarget: before + 1},
	}, lastTests)
}
