package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealtimeCumulativeUsageIsChargedOnce(t *testing.T) {
	for _, source := range []string{BillingSourceWallet, BillingSourceSubscription} {
		t.Run(source, func(t *testing.T) {
			truncate(t)
			wallet := 1000
			if source == BillingSourceSubscription {
				wallet = 0
				plan := seedSubscriptionPlan(t, 9801, "realtime subscription", false)
				seedSubscriptionWithPlan(t, 9801, 9801, plan.Id, "", 1000, 0)
			}
			seedUser(t, 9801, wallet)
			seedToken(t, 9801, 9801, "realtime-billing", 1000)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 9801).Update("expired_time", -1).Error)
			seedChannel(t, 9801)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
			info := &relaycommon.RelayInfo{
				UserId: 9801, TokenId: 9801, TokenKey: "realtime-billing", RequestId: "realtime-billing",
				OriginModelName: "gpt-4o-realtime-preview", UserGroup: "default", UsingGroup: "default",
				StartTime: time.Now(), ForcePreConsume: true, RelayFormat: types.RelayFormatOpenAIRealtime,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9801, UpstreamModelName: "gpt-4o-realtime-preview"},
				UserSetting: dto.UserSetting{BillingPreference: source + "_only"},
				PriceData:   types.PriceData{ModelRatio: 2.5, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
			}
			require.Nil(t, PreConsumeBilling(ctx, 10, info))
			usage := &dto.RealtimeUsage{TotalTokens: 40, InputTokens: 40}
			usage.InputTokenDetails.TextTokens = 40
			require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
			assert.Equal(t, 900, getTokenRemainQuota(t, 9801))
			usage.TotalTokens, usage.InputTokens, usage.InputTokenDetails.TextTokens = 100, 100, 100
			require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
			assert.Equal(t, 750, getTokenRemainQuota(t, 9801))
			PostWssConsumeQuota(ctx, info, info.UpstreamModelName, usage, "")
			assert.Equal(t, 750, getTokenRemainQuota(t, 9801))
			assert.Equal(t, 250, getTokenUsedQuota(t, 9801))
			if source == BillingSourceSubscription {
				assert.EqualValues(t, 250, getSubscriptionUsed(t, 9801))
				assert.Zero(t, getUserQuota(t, 9801))
			} else {
				assert.Equal(t, 750, getUserQuota(t, 9801))
			}
			var log model.Log
			require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 9801, model.LogTypeConsume).First(&log).Error)
			assert.Equal(t, 250, log.Quota)
			var user model.User
			require.NoError(t, model.DB.First(&user, 9801).Error)
			assert.Equal(t, 250, user.UsedQuota)
		})
	}
}

func TestRealtimeReserveFailureStillSettlesDeliveredUsage(t *testing.T) {
	truncate(t)
	seedUser(t, 9802, 150)
	seedToken(t, 9802, 9802, "realtime-insufficient", 1000)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 9802).Update("expired_time", -1).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 9802, TokenId: 9802, TokenKey: "realtime-insufficient", OriginModelName: "gpt-4o-realtime-preview",
		ForcePreConsume: true, RelayFormat: types.RelayFormatOpenAIRealtime,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o-realtime-preview"},
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
		PriceData:   types.PriceData{ModelRatio: 2.5, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	require.Nil(t, PreConsumeBilling(ctx, 10, info))
	// 即使初始预扣走过信任旁路，Realtime 补预扣仍必须严格检查剩余额度。
	info.Billing.(*BillingSession).trusted = true
	info.ForcePreConsume = false
	usage := &dto.RealtimeUsage{TotalTokens: 100, InputTokens: 100}
	usage.InputTokenDetails.TextTokens = 100
	require.Error(t, PreWssConsumeQuota(ctx, info, usage))
	assert.Equal(t, 140, getUserQuota(t, 9802))
	assert.Equal(t, 990, getTokenRemainQuota(t, 9802))
	require.NoError(t, SettleBilling(ctx, info, 250))
	assert.Equal(t, -100, getUserQuota(t, 9802))
	assert.Equal(t, 750, getTokenRemainQuota(t, 9802))
}

func TestSubscriptionTextSettlementChargesDeliveredOverage(t *testing.T) {
	truncate(t)
	seedUser(t, 9803, 0)
	seedToken(t, 9803, 9803, "text-subscription-overage", 1000)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 9803).Update("expired_time", -1).Error)
	seedChannel(t, 9803)
	plan := seedSubscriptionPlan(t, 9803, "text subscription", false)
	seedSubscriptionWithPlan(t, 9803, 9803, plan.Id, "", 100, 0)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		UserId: 9803, TokenId: 9803, TokenKey: "text-subscription-overage", RequestId: "text-subscription-overage",
		OriginModelName: "test", UserGroup: "default", UsingGroup: "default", StartTime: time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9803},
		UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
		PriceData:   types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	require.Nil(t, PreConsumeBilling(ctx, 60, info))
	PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 50, CompletionTokens: 100, TotalTokens: 150}, nil)
	assert.EqualValues(t, 150, getSubscriptionUsed(t, 9803))
	assert.Equal(t, 850, getTokenRemainQuota(t, 9803))
	var log model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 9803, model.LogTypeConsume).First(&log).Error)
	assert.Equal(t, 150, log.Quota)
	assert.Zero(t, getUserQuota(t, 9803))
	var user model.User
	require.NoError(t, model.DB.First(&user, 9803).Error)
	assert.Equal(t, 150, user.UsedQuota)
}

func TestSubscriptionReserveRollsBackWhenTokenQuotaIsInsufficient(t *testing.T) {
	truncate(t)
	seedUser(t, 9804, 0)
	seedToken(t, 9804, 9804, "subscription-token-boundary", 30)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 9804).Update("expired_time", -1).Error)
	plan := seedSubscriptionPlan(t, 9804, "reserve rollback", false)
	seedSubscriptionWithPlan(t, 9804, 9804, plan.Id, "", 100, 0)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 9804, TokenId: 9804, TokenKey: "subscription-token-boundary", RequestId: "subscription-token-boundary",
		UserGroup: "default", UsingGroup: "default", ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
	}
	require.Nil(t, PreConsumeBilling(ctx, 20, info))
	require.Error(t, info.Billing.Reserve(40))
	assert.EqualValues(t, 20, getSubscriptionUsed(t, 9804))
	assert.Equal(t, 10, getTokenRemainQuota(t, 9804))
	var record model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&record).Error)
	assert.EqualValues(t, 20, record.PreConsumed)
	require.NoError(t, SettleBilling(ctx, info, 10))
	assert.EqualValues(t, 10, getSubscriptionUsed(t, 9804))
	assert.Equal(t, 20, getTokenRemainQuota(t, 9804))
}
