package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type completedVideoBillingAdaptor struct {
	taskPollingFetchAdaptor
}

func (*completedVideoBillingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 60
}

func TestVideoTaskResultSettlesOnceAcrossRealtimeAndPollingSnapshots(t *testing.T) {
	truncate(t)
	seedUser(t, 859, 100)
	seedToken(t, 859, 859, "video-completion", 100)
	seedChannel(t, 859)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 859).Update("used_quota", 100).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 859).Update("used_quota", 100).Error)
	task := makeTask(859, 859, 100, 859, BillingSourceWallet, 0)
	task.Status = model.TaskStatusInProgress
	task.Progress = "50%"
	require.NoError(t, model.DB.Create(task).Error)
	stale := *task
	result := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Url: "https://example.test/video.mp4"}
	adaptor := &completedVideoBillingAdaptor{}
	require.NoError(t, ApplyVideoTaskResult(context.Background(), adaptor, task, result, []byte(`{"done":true}`)))
	require.NoError(t, ApplyVideoTaskResult(context.Background(), adaptor, &stale, result, []byte(`{"done":true}`)))
	var saved model.Task
	require.NoError(t, model.DB.First(&saved, task.ID).Error)
	require.NoError(t, ApplyVideoTaskResult(context.Background(), adaptor, &saved, result, []byte(`{"done":true}`)))
	assert.EqualValues(t, model.TaskStatusSuccess, saved.Status)
	assert.Equal(t, 60, saved.Quota)
	assert.Equal(t, 140, getUserQuota(t, 859))
	assert.Equal(t, 140, getTokenRemainQuota(t, 859))
	var user model.User
	require.NoError(t, model.DB.First(&user, 859).Error)
	assert.Equal(t, 60, user.UsedQuota)
}
