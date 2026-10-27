package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// completedVideoBillingAdaptor reports the polled task as finished and asks
// for a final quota different from the pre-charge.
type completedVideoBillingAdaptor struct{}

func (completedVideoBillingAdaptor) Init(*relaycommon.RelayInfo) {}
func (completedVideoBillingAdaptor) FetchTask(string, string, *model.Task, string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{"done":true}`))}, nil
}
func (completedVideoBillingAdaptor) ParseTaskResult(*model.Task, *http.Response, []byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Url: "https://example.test/video.mp4"}, nil
}
func (completedVideoBillingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 60
}

// A realtime query and the background poller may both observe completion from
// snapshots taken before either wrote it; the status CAS lets only one settle.
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
	task.PrivateData.UpstreamTaskID = "video-completion-upstream"
	require.NoError(t, model.DB.Create(task).Error)
	stale := *task
	channel, err := model.GetChannelById(859, true)
	require.NoError(t, err)
	adaptor := completedVideoBillingAdaptor{}
	upstreamID := task.GetUpstreamTaskID()

	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, channel, upstreamID, map[string]*model.Task{upstreamID: task}))
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, channel, upstreamID, map[string]*model.Task{upstreamID: &stale}))

	var saved model.Task
	require.NoError(t, model.DB.First(&saved, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, saved.Status)
	assert.Equal(t, 60, saved.Quota)
	assert.Equal(t, 140, getUserQuota(t, 859))
	assert.Equal(t, 140, getTokenRemainQuota(t, 859))
	var user model.User
	require.NoError(t, model.DB.First(&user, 859).Error)
	assert.Equal(t, 60, user.UsedQuota)
}
