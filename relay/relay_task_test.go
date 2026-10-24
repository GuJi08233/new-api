package relay

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskRetryChecksReservationBeforeCallingUpstream(t *testing.T) {
	savedPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"task-retry-model":0.1}`))
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"task-retry-model","prompt":"p","duration":1}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeGemini)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "task-retry-model")
	session := &imagePreflightBilling{}
	info := &relaycommon.RelayInfo{OriginModelName: "task-retry-model", UserGroup: "default", UsingGroup: "default", Billing: session, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	result, taskErr := RelayTaskSubmit(ctx, info)
	require.NotNil(t, taskErr)
	assert.Nil(t, result)
	assert.Equal(t, http.StatusForbidden, taskErr.StatusCode)
	assert.Positive(t, session.target)
	assert.True(t, info.ForcePreConsume)
	assert.Zero(t, calls.Load())
}

func TestRealtimeFetchRefundsFailedGeminiTaskOnlyOnce(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "realtime-task.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	previousDB, previousReadDB, previousLogDB := model.DB, model.RO_DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch := common.RedisEnabled, common.BatchUpdateEnabled
	model.DB, model.RO_DB, model.LOG_DB = database, database, database
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.RO_DB, model.LOG_DB = previousDB, previousReadDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RedisEnabled, common.BatchUpdateEnabled = previousRedis, previousBatch
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, database.AutoMigrate(&model.Task{}, &model.User{}, &model.Channel{}, &model.Log{}))
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"operations/task-1","done":true,"error":{"code":3,"message":"blocked by safety"}}`))
	}))
	t.Cleanup(upstream.Close)
	channel := &model.Channel{Name: "realtime-refund", Type: constant.ChannelTypeGemini, Key: "channel-key", BaseURL: &upstream.URL}
	require.NoError(t, database.Create(channel).Error)
	require.NoError(t, database.Create(&model.User{Id: 7, Username: "realtime-owner", Quota: 100, UsedQuota: 1000}).Error)
	task := &model.Task{
		TaskID: "public-task", Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeGemini)), UserId: 7, ChannelId: channel.Id,
		Quota: 1000, Status: model.TaskStatusInProgress, Progress: "50%",
		PrivateData: model.TaskPrivateData{UpstreamTaskID: base64.RawURLEncoding.EncodeToString([]byte("operations/task-1"))},
	}
	require.NoError(t, database.Create(task).Error)
	stale := *task
	tryRealtimeFetch(task, true)
	tryRealtimeFetch(&stale, true)
	var saved model.Task
	require.NoError(t, database.First(&saved, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, saved.Status)
	assert.Equal(t, "blocked by safety", saved.FailReason)
	assert.Zero(t, saved.Quota)
	assert.False(t, saved.RefundPending)
	tryRealtimeFetch(&saved, true)
	var user model.User
	require.NoError(t, database.First(&user, 7).Error)
	assert.Equal(t, 1100, user.Quota)
	assert.Zero(t, user.UsedQuota)
}
