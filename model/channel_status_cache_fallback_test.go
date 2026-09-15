package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 数据库拒绝转换（行已归档、行已不存在）时缓存保持原样，不会出现缓存与数据库分叉。
func TestUpdateChannelStatusLeavesCacheUntouchedWhenDatabaseRejects(t *testing.T) {
	channel := newCachedStatusChannel(t, false)
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)

	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("status", common.ChannelStatusArchived).Error)
	assert.False(t, UpdateChannelStatus(channel.Id, "k1", common.ChannelStatusAutoDisabled, "429"))
	assert.Equal(t, common.ChannelStatusEnabled, cached.Status)
	require.NotNil(t, routedCachedChannel(t))

	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	assert.False(t, UpdateChannelStatus(channel.Id, "k1", common.ChannelStatusAutoDisabled, "429"))
	assert.Equal(t, common.ChannelStatusEnabled, cached.Status)
}

// 缓存中尚无该渠道时仍以数据库为准完成转换，并重建缓存让结果可见。
func TestUpdateChannelStatusCacheMissFallsBackToDatabase(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	oldMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemoryCache })
	InitChannelCache()
	channel := &Channel{Name: "uncached", Key: "k1", Status: common.ChannelStatusEnabled, Models: "gpt-4o", Group: "default"}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	_, err := CacheGetChannel(channel.Id)
	require.Error(t, err)

	require.True(t, UpdateChannelStatus(channel.Id, "k1", common.ChannelStatusAutoDisabled, "429"))
	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, got.Status)
	assert.False(t, abilityEnabled(t, channel.Id))
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, cached.Status)
}
