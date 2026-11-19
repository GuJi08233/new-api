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

func TestClaudeChatStreamFlushesFinalToolArguments(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := newOpenAIStreamWriteRelayInfo(relayconstant.RelayModeChatCompletions, types.RelayFormatClaude)
	info.ClaudeConvertInfo = nil
	frames := "data: " + `{"id":"test","model":"test","choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\n" +
		"data: " + `{"id":"test","model":"test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call12345","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\ndata: [DONE]\n\n"
	usage, apiErr := OaiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(frames))})
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 5, usage.CompletionTokens)
	body := recorder.Body.String()
	assert.Contains(t, body, `"name":"lookup"`)
	assert.Contains(t, body, `"partial_json":"{\"city\":\"Paris\"}"`)
	assert.Contains(t, body, `"stop_reason":"tool_use"`)
	assert.Equal(t, 1, strings.Count(body, "event: message_stop"))
	assert.Equal(t, 1, strings.Count(body, `"partial_json"`))
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

func TestResponsesTerminalUsagePreservesDetailsAndExplicitZero(t *testing.T) {
	tests := []struct {
		name       string
		event      string
		usage      string
		prompt     int
		completion int
		reasoning  int
		cached     int
	}{
		{name: "completed", event: "completed", usage: `{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":3}}`, prompt: 10, completion: 5, reasoning: 3, cached: 4},
		{name: "incomplete", event: "incomplete", usage: `{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":3}}`, prompt: 10, completion: 5, reasoning: 3, cached: 4},
		{name: "failed", event: "failed", usage: `{"input_tokens":10,"output_tokens":1,"total_tokens":11}`, prompt: 10, completion: 1},
		{name: "upstream zero is authoritative", event: "completed", usage: `{"input_tokens":0,"output_tokens":0,"total_tokens":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{DisablePing: true, RelayFormat: types.RelayFormatOpenAIResponses, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}}
			frames := "data: " + `{"type":"response.output_text.delta","delta":"some generated text"}` + "\n\n" +
				"data: " + `{"type":"response.` + tt.event + `","response":{"status":"` + tt.event + `","usage":` + tt.usage + `}}` + "\n\n"
			usage, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(frames))})
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, tt.prompt, usage.PromptTokens)
			assert.Equal(t, tt.completion, usage.CompletionTokens)
			assert.Equal(t, tt.prompt+tt.completion, usage.TotalTokens)
			assert.Equal(t, tt.reasoning, usage.CompletionTokenDetails.ReasoningTokens)
			assert.Equal(t, tt.cached, usage.PromptTokensDetails.CachedTokens)
			assert.Contains(t, recorder.Body.String(), `"type":"response.`+tt.event+`"`)
		})
	}
}
