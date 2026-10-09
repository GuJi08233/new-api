package openai

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelSpecificChatCapabilitiesKeepSupportedSampling(t *testing.T) {
	for _, tc := range []struct {
		model, effort           string
		sampling, maxCompletion bool
	}{
		{"gpt-5.2", "none", true, true}, {"gpt-5.2", "high", false, true}, {"gpt-6-astra", "", false, true}, {"gpt-4o", "", true, false}, {"gpt-7-custom", "", true, false},
	} {
		req := &dto.GeneralOpenAIRequest{Model: tc.model, ReasoningEffort: tc.effort, MaxTokens: common.GetPointer(uint(20)), Temperature: common.GetPointer(0.3), TopP: common.GetPointer(0.8), LogProbs: common.GetPointer(true), TopLogProbs: common.GetPointer(2)}
		info := &relaycommon.RelayInfo{OriginModelName: tc.model, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.model, ChannelType: constant.ChannelTypeOpenAI}}
		_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, req)
		require.NoError(t, err)
		assert.Equal(t, tc.sampling, req.Temperature != nil, tc.model)
		assert.Equal(t, tc.sampling, req.TopP != nil, tc.model)
		assert.Equal(t, tc.sampling, req.TopLogProbs != nil, tc.model)
		assert.Equal(t, tc.maxCompletion, req.MaxCompletionTokens != nil, tc.model)
	}
}

func TestRealtimeGAOmitsLegacyBetaHeader(t *testing.T) {
	for _, model := range []string{"gpt-realtime", "gpt-4o-realtime-preview"} {
		for _, protocol := range []string{"", "realtime"} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
			c.Request.Header.Set("Sec-WebSocket-Protocol", protocol)
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeRealtime, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: model, ChannelType: constant.ChannelTypeOpenAI}}
			headers := http.Header{}
			require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
			if model == "gpt-realtime" {
				assert.Empty(t, headers.Get("openai-beta"))
				assert.NotContains(t, headers.Get("Sec-WebSocket-Protocol"), "openai-beta")
			} else if protocol == "" {
				assert.Equal(t, "realtime=v1", headers.Get("openai-beta"))
			} else {
				assert.Contains(t, headers.Get("Sec-WebSocket-Protocol"), "openai-beta.realtime-v1")
			}
		}
	}
}

// OA2 空密钥对接无认证上游：各认证位置都不能发出不带令牌的畸形凭据，有密钥时照常发送。
func TestSetupRequestHeaderSendsCredentialsOnlyForNonEmptyKey(t *testing.T) {
	for _, tc := range []struct {
		name, apiKey, protocol string
		relayMode              int
		wantAuthorization      []string
		wantProtocol           string
	}{
		{name: "chat empty key", relayMode: relayconstant.RelayModeChatCompletions},
		{name: "chat with key", apiKey: "sk-test", relayMode: relayconstant.RelayModeChatCompletions, wantAuthorization: []string{"Bearer sk-test"}},
		{name: "realtime header empty key", relayMode: relayconstant.RelayModeRealtime},
		{name: "realtime header with key", apiKey: "sk-test", relayMode: relayconstant.RelayModeRealtime, wantAuthorization: []string{"Bearer sk-test"}},
		{name: "realtime subprotocol empty key", protocol: "realtime", relayMode: relayconstant.RelayModeRealtime, wantProtocol: "realtime"},
		{name: "realtime subprotocol with key", apiKey: "sk-test", protocol: "realtime", relayMode: relayconstant.RelayModeRealtime, wantProtocol: "realtime,openai-insecure-api-key.sk-test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
			c.Request.Header.Set("Sec-WebSocket-Protocol", tc.protocol)
			info := &relaycommon.RelayInfo{RelayMode: tc.relayMode, ChannelMeta: &relaycommon.ChannelMeta{ApiKey: tc.apiKey, UpstreamModelName: "gpt-realtime", ChannelType: constant.ChannelTypeOA2}}
			headers := http.Header{}
			require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
			assert.Equal(t, tc.wantAuthorization, headers.Values("Authorization"))
			assert.Equal(t, tc.wantProtocol, headers.Get("Sec-WebSocket-Protocol"))
		})
	}
}
