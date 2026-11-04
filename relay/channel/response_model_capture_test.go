package channel_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayPreservesResponseModelBeforeConversionAndRewrite(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousRewrite := settings.RewriteResponseModelEnabled
	settings.RewriteResponseModelEnabled = true
	t.Cleanup(func() { settings.RewriteResponseModelEnabled = previousRewrite })

	tests := []struct {
		name    string
		body    string
		model   string
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{
			name:    "OpenAI chat response",
			body:    `{"id":"chatcmpl-1","model":"actual-chat-version","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`,
			model:   "actual-chat-version",
			handler: openai.OpenaiHandler,
		},
		{
			name:  "Claude converted to OpenAI",
			body:  `{"id":"msg-1","type":"message","role":"assistant","model":"actual-claude-version","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":3}}`,
			model: "actual-claude-version",
			handler: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
				return claude.ClaudeHandler(c, resp, info)
			},
		},
		{
			name:    "Gemini modelVersion survives DTO conversion",
			body:    `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5},"modelVersion":"actual-gemini-version"}`,
			model:   "actual-gemini-version",
			handler: gemini.GeminiChatHandler,
		},
		{
			name:    "Gemini does not report synthesized model as actual",
			body:    `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}`,
			handler: gemini.GeminiChatHandler,
		},
		{
			name:    "buffered Responses stream converted to OpenAI",
			body:    "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"model\":\"actual-responses-version\",\"status\":\"completed\",\"output\":[{\"id\":\"msg-1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}],\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n",
			model:   "actual-responses-version",
			handler: openai.OaiResponsesToChatBufferedStreamHandler,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				OriginModelName: "client-model",
				RelayFormat:     types.RelayFormatOpenAI,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "mapped-model"},
			}
			service.SetModelRewrite(c, info, "mapped-model", "client-model")
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}

			usage, apiErr := tt.handler(c, info, resp)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 5, usage.TotalTokens)
			assert.Equal(t, tt.model, common.GetContextKeyString(c, constant.ContextKeyResponseModelName))
			assert.Equal(t, "client-model", info.OriginModelName)
			assert.Equal(t, "mapped-model", info.UpstreamModelName)
			var response struct {
				Model string `json:"model"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, "client-model", response.Model)
		})
	}
}
