package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
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
