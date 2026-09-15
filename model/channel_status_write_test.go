package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelStatusWritePreservesConcurrentConfiguration(t *testing.T) {
	channel := &Channel{Name: "original", Key: "original-key", Status: common.ChannelStatusEnabled}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() { require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error) })
	stale := *channel
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"name": "updated", "key": "rotated", "used_quota": 1234,
	}).Error)
	stale.Status = common.ChannelStatusAutoDisabled
	stale.SetOtherInfo(map[string]any{"status_reason": "upstream error"})
	require.NoError(t, stale.saveStatusState())
	var saved Channel
	require.NoError(t, DB.First(&saved, channel.Id).Error)
	assert.Equal(t, "updated", saved.Name)
	assert.Equal(t, "rotated", saved.Key)
	assert.Equal(t, int64(1234), saved.UsedQuota)
	assert.Equal(t, common.ChannelStatusAutoDisabled, saved.Status)
	assert.Equal(t, "upstream error", saved.GetOtherInfo()["status_reason"])
}
