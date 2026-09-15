package ollama

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaRequestPreservesToolContextAndReasoning(t *testing.T) {
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-oss","reasoning_effort":"high","messages":[{"role":"assistant","reasoning_content":"thinking","tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{\"days\":0}"}}]},{"role":"tool","tool_call_id":"call_weather","content":"sunny"}],"response_format":{"type":"json_schema","json_schema":{"name":"weather","schema":{"type":"object"},"strict":true}}}`), &request))
	got, err := openAIChatToOllamaChat(nil, &request)
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	assert.JSONEq(t, `"high"`, string(got.Think))
	assert.JSONEq(t, `"thinking"`, string(got.Messages[0].Thinking))
	require.Len(t, got.Messages[0].ToolCalls, 1)
	assert.Equal(t, "call_weather", got.Messages[0].ToolCalls[0].ID)
	assert.Equal(t, "call_weather", got.Messages[1].ToolCallID)
	assert.Equal(t, "weather", got.Messages[1].ToolName)
	assert.Equal(t, map[string]any{"type": "object"}, got.Format)
	data, err := common.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"stream":false`)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))
	assert.Equal(t, false, body["stream"])
}

func TestOllamaExplicitThinkAndJSONMode(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "model", Think: []byte("false"), ReasoningEffort: "high", ResponseFormat: &dto.ResponseFormat{Type: "json_object"}}
	got, err := openAIChatToOllamaChat(nil, request)
	require.NoError(t, err)
	assert.Equal(t, "false", string(got.Think))
	assert.Equal(t, "json", got.Format)
	gen, err := openAIToGenerate(nil, request)
	require.NoError(t, err)
	data, err := common.Marshal(gen)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"stream":false`)
}

func TestOllamaDoneFramePreservesPayloadAndToolID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/chat", nil)
	raw := `{"model":"gpt-oss","message":{"role":"assistant","content":"final text","thinking":"final thought","tool_calls":[{"id":"upstream_call","function":{"name":"weather","arguments":{"days":0}}}]},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":4}`
	usage, apiErr := ollamaStreamHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-oss"}}, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(raw)), Header: make(http.Header)})
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 7, usage.TotalTokens)
	var tools []dto.ToolCallResponse
	var content, reasoning, finish string
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") || strings.TrimPrefix(line, "data: ") == "[DONE]" {
			continue
		}
		var chunk dto.ChatCompletionsStreamResponse
		require.NoError(t, common.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk))
		for _, choice := range chunk.Choices {
			content += choice.Delta.GetContentString()
			reasoning += choice.Delta.GetReasoningContent()
			tools = append(tools, choice.Delta.ToolCalls...)
			if choice.FinishReason != nil {
				finish = *choice.FinishReason
			}
		}
	}
	assert.Equal(t, "final text", content)
	assert.Equal(t, "final thought", reasoning)
	require.Len(t, tools, 1)
	assert.Equal(t, "upstream_call", tools[0].ID)
	assert.JSONEq(t, `{"days":0}`, tools[0].Function.Arguments)
	assert.Equal(t, "tool_calls", finish)
	assert.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
}
