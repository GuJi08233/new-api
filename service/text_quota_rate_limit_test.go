package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordRateLimitTokensUpdatesUserAndChannelTPM(t *testing.T) {
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	subject := "svc-tpm-user"
	common.SetContextKey(c, constant.ContextKeyModelRateLimitTokenTarget, common.ModelRateLimitTokenTarget{
		Subject:  subject,
		Duration: 60,
	})

	channelID := 960001
	channelSetting := dto.ChannelSettings{
		RateLimitPeriodMinutes: 1,
		RateLimitMaxTokens:     1000,
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:      channelID,
			ChannelSetting: channelSetting,
		},
	}

	recordRateLimitTokens(c, info, 600)
	allowed, err := common.CheckModelTokenRateLimit(subject, 1000, 60)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.EqualValues(t, 600, model.GetChannelPeriodTokenCount(channelID, model.NewChannelRequestLimitConfig(channelSetting)))

	recordRateLimitTokens(c, info, 400)
	allowed, err = common.CheckModelTokenRateLimit(subject, 1000, 60)
	require.NoError(t, err)
	assert.False(t, allowed, "user TPM must block once 1000 tokens are recorded")
	assert.True(t, model.IsChannelRequestLimitReached(channelID, model.NewChannelRequestLimitConfig(channelSetting)), "channel TPM must block once 1000 tokens are recorded")
}

func TestBeginChannelAttemptPrechecksEstimatedTokensAndSettlesActual(t *testing.T) {
	require.NoError(t, i18n.Init())
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })

	channelID := 960002
	channelSetting := dto.ChannelSettings{
		RateLimitPeriodMinutes: 1,
		RateLimitMaxTokens:     800_000,
	}

	// Request 1: estimated 500k -> admitted, settled at 510k.
	c1, _ := gin.CreateTestContext(httptest.NewRecorder())
	c1.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c1, constant.ContextKeyEstimatedTokens, 500_000)

	finish1, apiErr := BeginChannelAttempt(c1, channelID, channelSetting)
	require.Nil(t, apiErr)

	// Concurrent Request 2: estimated 500k while Request 1 holds 500k -> rejected before upstream.
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c2, constant.ContextKeyEstimatedTokens, 500_000)
	_, apiErr2 := BeginChannelAttempt(c2, channelID, channelSetting)
	require.NotNil(t, apiErr2)
	assert.Equal(t, http.StatusTooManyRequests, apiErr2.StatusCode)

	info1 := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:      channelID,
			ChannelSetting: channelSetting,
		},
	}
	recordRateLimitTokens(c1, info1, 510_000)
	finish1(true)

	// Request 3: estimated 515k after 510k settled -> 510k + 515k > 800k -> rejected.
	c3, _ := gin.CreateTestContext(httptest.NewRecorder())
	c3.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c3, constant.ContextKeyEstimatedTokens, 515_000)
	_, apiErr3 := BeginChannelAttempt(c3, channelID, channelSetting)
	require.NotNil(t, apiErr3)
	assert.Equal(t, http.StatusTooManyRequests, apiErr3.StatusCode)
}
