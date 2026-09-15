package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCachedStatusChannel 创建一个已进入内存缓存与路由表的启用渠道。
func newCachedStatusChannel(t *testing.T, multiKey bool) *Channel {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	oldMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemoryCache })
	channel := &Channel{Name: "cached", Key: "k1", Status: common.ChannelStatusEnabled, Models: "gpt-4o", Group: "default"}
	if multiKey {
		channel.Key = "k1\nk2"
		channel.ChannelInfo = ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling}
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	InitChannelCache()
	require.NotNil(t, routedCachedChannel(t))
	return channel
}

func routedCachedChannel(t *testing.T) *Channel {
	t.Helper()
	routed, err := GetRandomSatisfiedChannel("default", "gpt-4o", 0, "")
	require.NoError(t, err)
	return routed
}

func abilityEnabled(t *testing.T, channelID int) bool {
	t.Helper()
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channelID).First(&ability).Error)
	return ability.Enabled
}

// 状态转换先连同 abilities 提交到数据库，再镜像到缓存；重新启用后立即回到路由表。
func TestUpdateChannelStatusMirrorsCommittedStateIntoCache(t *testing.T) {
	channel := newCachedStatusChannel(t, false)

	require.True(t, UpdateChannelStatus(channel.Id, "k1", common.ChannelStatusAutoDisabled, "429"))
	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, got.Status)
	assert.Equal(t, "429", got.GetOtherInfo()["status_reason"])
	assert.False(t, abilityEnabled(t, channel.Id))
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, cached.Status)
	assert.Equal(t, "429", cached.GetOtherInfo()["status_reason"])
	assert.Nil(t, routedCachedChannel(t), "a disabled channel leaves routing immediately")

	require.True(t, UpdateChannelStatus(channel.Id, "k1", common.ChannelStatusEnabled, ""))
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, got.Status)
	assert.True(t, abilityEnabled(t, channel.Id))
	assert.Equal(t, common.ChannelStatusEnabled, cached.Status)
	routed := routedCachedChannel(t)
	require.NotNil(t, routed, "an enabled channel rejoins routing without waiting for the next sync")
	assert.Equal(t, channel.Id, routed.Id)
}

// 单个 key 的状态变化持久化到 channel_info 并镜像到缓存，缓存独有的轮询游标不被覆盖。
func TestUpdateChannelStatusMultiKeyMirrorsKeyStateAndKeepsPollingCursor(t *testing.T) {
	channel := newCachedStatusChannel(t, true)
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	cached.ChannelInfo.MultiKeyPollingIndex = 1

	require.True(t, UpdateChannelStatus(channel.Id, "k1", common.ChannelStatusAutoDisabled, "429"))
	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, got.Status)
	assert.Equal(t, map[int]int{0: common.ChannelStatusAutoDisabled}, got.ChannelInfo.MultiKeyStatusList)
	assert.Equal(t, "429", got.ChannelInfo.MultiKeyDisabledReason[0])
	assert.Equal(t, got.ChannelInfo.MultiKeyStatusList, cached.ChannelInfo.MultiKeyStatusList)
	assert.Equal(t, 1, cached.ChannelInfo.MultiKeyPollingIndex)
	assert.True(t, abilityEnabled(t, channel.Id))
	require.NotNil(t, routedCachedChannel(t))

	assert.False(t, UpdateChannelStatus(channel.Id, "k1", common.ChannelStatusAutoDisabled, "429 again"), "re-disabling a disabled key is a no-op")
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, "429", got.ChannelInfo.MultiKeyDisabledReason[0])

	require.True(t, UpdateChannelStatus(channel.Id, "k2", common.ChannelStatusAutoDisabled, "401"))
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, got.Status)
	assert.Equal(t, "All keys are disabled", got.GetOtherInfo()["status_reason"])
	assert.False(t, abilityEnabled(t, channel.Id))
	assert.Equal(t, common.ChannelStatusAutoDisabled, cached.Status)
	assert.Nil(t, routedCachedChannel(t))

	require.True(t, UpdateChannelStatus(channel.Id, "k1", common.ChannelStatusEnabled, ""))
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, got.Status)
	assert.Equal(t, map[int]int{1: common.ChannelStatusAutoDisabled}, got.ChannelInfo.MultiKeyStatusList)
	assert.True(t, abilityEnabled(t, channel.Id))
	assert.Equal(t, got.ChannelInfo.MultiKeyStatusList, cached.ChannelInfo.MultiKeyStatusList)
	assert.Equal(t, 1, cached.ChannelInfo.MultiKeyPollingIndex)
	require.NotNil(t, routedCachedChannel(t))
}
