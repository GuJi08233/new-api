package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatStreamPreservesPenultimateUsageForTextModels(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := newOpenAIStreamWriteRelayInfo(relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI)
	frames := "data: {\"id\":\"id\",\"created\":1,\"model\":\"text-model\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120,\"prompt_tokens_details\":{\"cached_tokens\":75}}}\n\n" +
		"data: {\"id\":\"id\",\"created\":1,\"model\":\"text-model\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	usage, apiErr := OaiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(frames))})
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.PromptTokens)
	assert.Equal(t, 20, usage.CompletionTokens)
	assert.Equal(t, 75, usage.PromptTokensDetails.CachedTokens)
}

func TestResponsesBillingCountsActualToolCallsOnly(t *testing.T) {
	for _, stream := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}, ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{dto.BuildInToolWebSearchPreview: {}}}}
		body := `{"tools":[{"type":"web_search_preview"}],"output":[{"type":"file_search_call","id":"fs1"},{"type":"file_search_call","id":"fs2"},{"type":"image_generation_call","id":"im1","quality":"low","size":"1024x1024","status":"completed"},{"type":"image_generation_call","id":"im2","quality":"high","size":"1024x1024","status":"completed"}],"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}`
		if stream {
			body = "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"file_search_call\",\"id\":\"fs1\"}}\n\n" +
				"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"file_search_call\",\"id\":\"fs1\"}}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":" + body + "}\n\n"
			_, err := OaiResponsesStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, err)
		} else {
			_, err := OaiResponsesHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, err)
		}
		assert.Zero(t, info.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
		require.NotNil(t, info.BuiltInTools[dto.BuildInToolFileSearch])
		assert.Equal(t, 2, info.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
		assert.Equal(t, []relaycommon.ImageGenerationCall{{Quality: "low", Size: "1024x1024"}, {Quality: "high", Size: "1024x1024"}}, info.ImageGenerationCalls)
	}
}
