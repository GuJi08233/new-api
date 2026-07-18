package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 归档渠道对自动路径冻结：自动启用/自动禁用不得改写归档状态，
// 归档与恢复只能通过管理端状态接口（UpdateChannelStatusManual）完成。
func TestUpdateChannelStatusArchivedFreeze(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)

	oldMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCache
	})

	channel := &Channel{
		Id:     1,
		Name:   "archived-channel",
		Key:    "sk-test",
		Status: common.ChannelStatusArchived,
		Models: "gpt-4o",
		Group:  "default",
	}
	require.NoError(t, DB.Create(channel).Error)

	// 自动启用（测试恢复路径，带 usingKey）被拒绝
	assert.False(t, UpdateChannelStatus(1, "sk-test", common.ChannelStatusEnabled, ""))
	// 自动禁用（在途请求错误路径）被拒绝
	assert.False(t, UpdateChannelStatus(1, "sk-test", common.ChannelStatusAutoDisabled, "upstream error"))
	// MJ 自动封禁路径（空 usingKey + 手动禁用状态值）同样被拒绝
	assert.False(t, UpdateChannelStatus(1, "", common.ChannelStatusManuallyDisabled, "No available account instance"))

	var got Channel
	require.NoError(t, DB.First(&got, 1).Error)
	assert.Equal(t, common.ChannelStatusArchived, got.Status)

	// 手动恢复为手动禁用成功
	assert.True(t, UpdateChannelStatusManual(1, common.ChannelStatusManuallyDisabled, "manual operation"))
	require.NoError(t, DB.First(&got, 1).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, got.Status)

	// 再次手动归档成功
	assert.True(t, UpdateChannelStatusManual(1, common.ChannelStatusArchived, "manual operation"))
	require.NoError(t, DB.First(&got, 1).Error)
	assert.Equal(t, common.ChannelStatusArchived, got.Status)

	// 手动恢复为启用成功，且 abilities 随之启用
	require.NoError(t, channel.AddAbilities(nil))
	assert.True(t, UpdateChannelStatusManual(1, common.ChannelStatusEnabled, "manual operation"))
	require.NoError(t, DB.First(&got, 1).Error)
	assert.Equal(t, common.ChannelStatusEnabled, got.Status)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", 1).First(&ability).Error)
	assert.True(t, ability.Enabled)
}
