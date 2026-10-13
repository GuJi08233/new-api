package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 上游失败后的重试不能换到对当前客户端受限的渠道；结束重试时保留上一次的上游错误。
func TestClientUaRuleRetryErrorKeepsUpstreamError(t *testing.T) {
	t.Cleanup(operation_setting.SetClientUaRuleSettingForTest([]operation_setting.ClientUaRule{
		{Name: "channel-5-cli-only", ChannelIDs: []int{5}, UaAllow: []string{"(external, cli)"}},
	}))
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	ctx.Request.Header.Set("User-Agent", "claude-cli/2.1.296 (external, sdk-cli)")
	upstreamErr := types.NewErrorWithStatusCode(errors.New("upstream overloaded"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable)
	info := &relaycommon.RelayInfo{OriginModelName: "claude-opus-4-1", LastError: upstreamErr}

	assert.Same(t, upstreamErr, clientUaRuleRetryError(ctx, info, 5))
	assert.Nil(t, clientUaRuleRetryError(ctx, info, 6))
}
