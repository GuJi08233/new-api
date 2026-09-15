package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 状态流只写自己拥有的列：并发改动的名称、密钥和计费计数不会被状态更新覆盖，
// abilities 与状态在同一事务内提交，重复的相同转换不产生第二次变更。
func TestUpdateChannelStatusPreservesConcurrentConfiguration(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	oldMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemoryCache })

	channel := &Channel{Name: "original", Key: "original-key", Status: common.ChannelStatusEnabled, Models: "gpt-4o", Group: "default"}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"name": "updated", "key": "rotated", "used_quota": 1234,
	}).Error)

	require.True(t, UpdateChannelStatus(channel.Id, "rotated", common.ChannelStatusAutoDisabled, "upstream error"))
	var saved Channel
	require.NoError(t, DB.First(&saved, channel.Id).Error)
	assert.Equal(t, "updated", saved.Name)
	assert.Equal(t, "rotated", saved.Key)
	assert.Equal(t, int64(1234), saved.UsedQuota)
	assert.Equal(t, common.ChannelStatusAutoDisabled, saved.Status)
	assert.Equal(t, "upstream error", saved.GetOtherInfo()["status_reason"])
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.False(t, ability.Enabled, "abilities are committed together with the status")

	assert.False(t, UpdateChannelStatus(channel.Id, "rotated", common.ChannelStatusAutoDisabled, "upstream error again"))
	require.NoError(t, DB.First(&saved, channel.Id).Error)
	assert.Equal(t, "upstream error", saved.GetOtherInfo()["status_reason"], "an identical transition is a no-op")
}
