package service

import (
	"bytes"
	"context"
	"github.com/QuantumNous/new-api/constant"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReserveBillingForPaidGroupCreatesSessionAndRejectsInsufficientQuota(t *testing.T) {
	for _, quota := range []int{100, 40} {
		t.Run(strconv.Itoa(quota), func(t *testing.T) {
			truncate(t)
			seedUser(t, 851, quota)
			seedToken(t, 851, 851, "reserve-free-to-paid", 100)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{UserId: 851, TokenId: 851, TokenKey: "reserve-free-to-paid", UsingGroup: "default", UserGroup: "default", ForcePreConsume: true, PriceData: types.PriceData{FreeModel: true}, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
			err := ReserveBillingForRequest(c, info, 60)
			if quota < 60 {
				require.NotNil(t, err)
				assert.Equal(t, 403, err.StatusCode)
				assert.Nil(t, info.Billing)
				assert.Equal(t, quota, getUserQuota(t, 851))
				assert.Equal(t, 100, getTokenRemainQuota(t, 851))
				return
			}
			require.Nil(t, err)
			require.NotNil(t, info.Billing)
			assert.False(t, info.PriceData.FreeModel)
			assert.Equal(t, 40, getUserQuota(t, 851))
			assert.Equal(t, 40, getTokenRemainQuota(t, 851))
			require.Nil(t, ReserveBillingForRequest(c, info, 60))
			assert.Equal(t, 40, getUserQuota(t, 851))
		})
	}
}

func TestTaskRefundRestoresUsageAndClearsPersistentQuota(t *testing.T) {
	truncate(t)
	seedUser(t, 852, 40)
	seedToken(t, 852, 852, "refund-stat", 40)
	seedChannel(t, 852)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 852).Updates(map[string]interface{}{"used_quota": 60, "request_count": 1}).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 852).Update("used_quota", 60).Error)
	task := makeTask(852, 852, 60, 852, BillingSourceWallet, 0)
	task.Status = model.TaskStatusFailure
	require.NoError(t, model.DB.Create(task).Error)
	require.True(t, RefundTaskQuota(context.Background(), task, "failure"))
	var user model.User
	require.NoError(t, model.DB.First(&user, 852).Error)
	assert.Equal(t, 100, user.Quota)
	assert.Equal(t, 0, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, 852).Error)
	assert.EqualValues(t, 0, channel.UsedQuota)
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Zero(t, persisted.Quota)
	require.True(t, RefundTaskQuota(context.Background(), &persisted, "duplicate"))
	assert.Equal(t, 100, getUserQuota(t, 852))
}

func TestMidjourneyUnbilledTaskHasNoRefund(t *testing.T) {
	truncate(t)
	seedUser(t, 853, 100)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}))
	task := &model.Midjourney{UserId: 853, MjId: "unbilled-mj"}
	require.NoError(t, model.DB.Create(task).Error)
	t.Cleanup(func() { model.DB.Delete(&model.Midjourney{}, task.Id) })
	billed, err := SettleMidjourneyTaskBilling(&relaycommon.RelayInfo{UserId: 853}, task, 50, false)
	require.NoError(t, err)
	assert.False(t, billed)
	assert.Zero(t, task.Quota)
	require.True(t, RefundMidjourneyQuota(context.Background(), task, "failure"))
	assert.Equal(t, 100, getUserQuota(t, 853))
}

func TestStrictReserveRollsBackWalletWhenTokenInsufficient(t *testing.T) {
	truncate(t)
	seedUser(t, 854, 100)
	seedToken(t, 854, 854, "strict-reserve", 5)
	info := &relaycommon.RelayInfo{UserId: 854, TokenId: 854, TokenKey: "strict-reserve", ForcePreConsume: true}
	funding := &WalletFunding{userId: 854, consumed: 20}
	session := &BillingSession{relayInfo: info, funding: funding, preConsumedQuota: 20, tokenConsumed: 20}
	require.Error(t, session.Reserve(40))
	assert.Equal(t, 100, getUserQuota(t, 854))
	assert.Equal(t, 5, getTokenRemainQuota(t, 854))
	assert.Equal(t, 20, funding.consumed)
	assert.Equal(t, 20, session.GetPreConsumedQuota())
}

func TestMidjourneyChargeAndRefundIncludeTokenAndUsage(t *testing.T) {
	truncate(t)
	seedUser(t, 855, 100)
	seedToken(t, 855, 855, "mj-billed", 100)
	seedChannel(t, 855)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}))
	task := &model.Midjourney{UserId: 855, ChannelId: 855, MjId: "billed-mj"}
	require.NoError(t, model.DB.Create(task).Error)
	t.Cleanup(func() { model.DB.Delete(&model.Midjourney{}, task.Id) })
	info := &relaycommon.RelayInfo{UserId: 855, TokenId: 855, TokenKey: "mj-billed", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 855}}
	billed, err := SettleMidjourneyTaskBilling(info, task, 50, true)
	require.NoError(t, err)
	require.True(t, billed)
	model.UpdateUserUsedQuotaAndRequestCount(855, 50)
	model.UpdateChannelUsedQuota(855, 50)
	assert.Equal(t, 50, getUserQuota(t, 855))
	assert.Equal(t, 50, getTokenRemainQuota(t, 855))
	require.True(t, RefundMidjourneyQuota(context.Background(), task, "failed"))
	assert.Equal(t, 100, getUserQuota(t, 855))
	assert.Equal(t, 100, getTokenRemainQuota(t, 855))
	var user model.User
	require.NoError(t, model.DB.First(&user, 855).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
}

type sunoFailureBillingAdaptor struct{}

func (sunoFailureBillingAdaptor) Init(*relaycommon.RelayInfo) {}
func (sunoFailureBillingAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(`{"code":"success","data":[{"task_id":"suno-refund","status":"FAILURE","fail_reason":"failed"}]}`))}, nil
}
func (sunoFailureBillingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}
func (sunoFailureBillingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func TestSunoStalePollingSnapshotRefundsOnlyOnce(t *testing.T) {
	truncate(t)
	seedUser(t, 856, 40)
	seedToken(t, 856, 856, "suno-refund", 40)
	base := "http://localhost"
	require.NoError(t, model.DB.Create(&model.Channel{Id: 856, BaseURL: &base, Key: "key", Status: 1}).Error)
	task := makeTask(856, 856, 60, 856, BillingSourceWallet, 0)
	task.TaskID = "suno-refund"
	task.Status = model.TaskStatusInProgress
	require.NoError(t, model.DB.Create(task).Error)
	stale := *task
	old := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return sunoFailureBillingAdaptor{} }
	t.Cleanup(func() { GetTaskAdaptorFunc = old })
	require.NoError(t, updateSunoTasks(context.Background(), 856, []string{"suno-refund"}, map[string]*model.Task{"suno-refund": task}))
	require.NoError(t, updateSunoTasks(context.Background(), 856, []string{"suno-refund"}, map[string]*model.Task{"suno-refund": &stale}))
	assert.Equal(t, 100, getUserQuota(t, 856))
	assert.Equal(t, 100, getTokenRemainQuota(t, 856))
	var saved model.Task
	require.NoError(t, model.DB.First(&saved, task.ID).Error)
	assert.Zero(t, saved.Quota)
	assert.EqualValues(t, model.TaskStatusFailure, saved.Status)
}
