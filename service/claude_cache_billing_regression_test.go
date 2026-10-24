package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCacheOnlySettlementDebitsQuotaAndRecordsUsage(t *testing.T) {
	truncate(t)
	seedUser(t, 858, 1000)
	seedToken(t, 858, 858, "claude-cache-only", 1000)
	seedChannel(t, 858)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := &relaycommon.RelayInfo{
		UserId: 858, TokenId: 858, TokenKey: "claude-cache-only", ForcePreConsume: true,
		UserGroup: "default", UsingGroup: "default", OriginModelName: "claude-cache-test",
		RelayFormat: types.RelayFormatClaude, StartTime: time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 858},
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
		PriceData:   types.PriceData{ModelRatio: 1, CacheRatio: 0.1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	require.Nil(t, PreConsumeBilling(ctx, 100, info))
	PostTextConsumeQuota(ctx, info, &dto.Usage{BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{CacheReadInputTokens: 100})}, nil)
	assert.Equal(t, 990, getUserQuota(t, 858))
	assert.Equal(t, 990, getTokenRemainQuota(t, 858))
	var user model.User
	require.NoError(t, model.DB.First(&user, 858).Error)
	assert.Equal(t, 10, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var log model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 858, model.LogTypeConsume).First(&log).Error)
	assert.Equal(t, 10, log.Quota)
}

func TestClaudeCacheOnlyUsageChargesLegacyPricing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		usage     dto.ClaudeUsage
		wantQuota int
	}{
		{name: "cache read", usage: dto.ClaudeUsage{CacheReadInputTokens: 100}, wantQuota: 10},
		{name: "unsplit cache write", usage: dto.ClaudeUsage{CacheCreationInputTokens: 100}, wantQuota: 125},
		{name: "one hour cache write", usage: dto.ClaudeUsage{CacheCreationInputTokens: 100, CacheCreation: &dto.ClaudeCacheCreationUsage{Ephemeral1hInputTokens: 100}}, wantQuota: 200},
		{name: "split without total", usage: dto.ClaudeUsage{CacheCreation: &dto.ClaudeCacheCreationUsage{Ephemeral5mInputTokens: 40, Ephemeral1hInputTokens: 60}}, wantQuota: 170},
		{name: "no usage", wantQuota: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatClaude,
				PriceData: types.PriceData{
					ModelRatio: 1, CompletionRatio: 2, CacheRatio: 0.1,
					CacheCreationRatio: 1.25, CacheCreation5mRatio: 1.25, CacheCreation1hRatio: 2,
					GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
				},
			}
			usage := effectiveBillingUsage(&dto.Usage{UsageSemantic: "anthropic", BillingUsage: dto.NewClaudeMessagesBillingUsage(&tc.usage)})
			summary := calculateTextQuotaSummary(ctx, info, usage)
			assert.Zero(t, summary.TotalTokens)
			assert.Equal(t, tc.wantQuota, summary.Quota)
			assert.Equal(t, tc.wantQuota > 0, summary.hasBillableUsage())
		})
	}
}

func TestClaudeUnsplitCacheWritesPreserveTierAndCharge(t *testing.T) {
	const expression = `len > 5000 ? tier("long", p * 6 + c * 30 + cc * 7.5 + cc1h * 12) : tier("short", p * 3 + c * 15 + cc * 3.75 + cc1h * 6)`
	for _, tc := range []struct {
		name             string
		cacheCreation    *dto.ClaudeCacheCreationUsage
		wantCC, wantCC1h float64
		wantQuota        int
	}{
		{name: "total without split", wantCC: 10000, wantQuota: 38550},
		{name: "partial split", cacheCreation: &dto.ClaudeCacheCreationUsage{Ephemeral1hInputTokens: 4000}, wantCC: 6000, wantCC1h: 4000, wantQuota: 47550},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage := effectiveBillingUsage(&dto.Usage{BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{
				InputTokens: 100, OutputTokens: 50, CacheCreationInputTokens: 10000, CacheCreation: tc.cacheCreation,
			})})
			params := BuildTieredTokenParams(usage, true, billingexpr.UsedVars(expression))
			assert.Equal(t, float64(10100), params.Len)
			assert.Equal(t, tc.wantCC, params.CC)
			assert.Equal(t, tc.wantCC1h, params.CC1h)
			info := &relaycommon.RelayInfo{TieredBillingSnapshot: makeSnapshot(expression, 1, 100, 50)}
			ok, quota, result := TryTieredSettle(info, params)
			require.True(t, ok)
			require.NotNil(t, result)
			assert.Equal(t, "long", result.MatchedTier)
			assert.Equal(t, tc.wantQuota, quota)
		})
	}
}
