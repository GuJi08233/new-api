package mistral

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestAdaptsMessagesAndReplaysReasoning(t *testing.T) {
	raw := `{
		"model":"mistral-large-4","max_tokens":512,"max_completion_tokens":0,"temperature":0,"top_p":0,
		"parallel_tool_calls":false,"reasoning_effort":"high",
		"messages":[
			{"role":"developer","content":"rules"},
			{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.test/a.png","detail":"low"}}]},
			{"role":"assistant","content":"checking","reasoning_content":"think first","tool_calls":[{"id":"call_openai_long","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_openai_long","content":"result"},
			{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"native thinking"}],"closed":true},{"type":"text","text":"native answer"}]},
			{"role":"assistant","content":null,"reasoning":"reasoning alias"}
		]
	}`
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(raw, &request))
	original, err := common.Marshal(request)
	require.NoError(t, err)
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, &request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)

	assert.Equal(t, "system", gjson.GetBytes(body, "messages.0.role").String())
	assert.Equal(t, "rules", gjson.GetBytes(body, "messages.0.content").String())
	assert.Equal(t, "https://example.test/a.png", gjson.GetBytes(body, "messages.1.content.1.image_url").String())
	assert.Equal(t, "think first", gjson.GetBytes(body, "messages.2.content.0.thinking.0.text").String())
	assert.Equal(t, "checking", gjson.GetBytes(body, "messages.2.content.1.text").String())
	callID := gjson.GetBytes(body, "messages.2.tool_calls.0.id").String()
	assert.Regexp(t, `^[a-zA-Z0-9]{9}$`, callID)
	assert.Equal(t, callID, gjson.GetBytes(body, "messages.3.tool_call_id").String())
	assert.Equal(t, "result", gjson.GetBytes(body, "messages.3.content").String())
	assert.Equal(t, "native thinking", gjson.GetBytes(body, "messages.4.content.0.thinking.0.text").String())
	assert.True(t, gjson.GetBytes(body, "messages.4.content.0.closed").Bool())
	assert.Equal(t, "reasoning alias", gjson.GetBytes(body, "messages.5.content.0.thinking.0.text").String())
	assert.False(t, gjson.GetBytes(body, "messages.2.reasoning_content").Exists())
	assert.False(t, gjson.GetBytes(body, "max_completion_tokens").Exists())
	for _, field := range []string{"max_tokens", "temperature", "top_p"} {
		assert.True(t, gjson.GetBytes(body, field).Exists(), field)
		assert.Zero(t, gjson.GetBytes(body, field).Float(), field)
	}
	assert.Equal(t, "false", gjson.GetBytes(body, "parallel_tool_calls").Raw)
	after, err := common.Marshal(request)
	require.NoError(t, err)
	assert.JSONEq(t, string(original), string(after))
}

func TestClaudeMessagesViaMistralResponses(t *testing.T) {
	service.InitHttpClient()
	const response = `{"id":"resp_1","object":"response","created_at":1795071161.5,"model":"mistral-large-4","status":"completed","output":[{"type":"reasoning","id":"r_1","summary":[],"content":[{"type":"reasoning_text","text":"check result"}]},{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"checking now"}]},{"type":"function_call","id":"fc_1","call_id":"call_next","name":"lookup","arguments":"{\"q\":\"next\"}"}],"usage":{"input_tokens":12,"output_tokens":4,"total_tokens":16,"output_tokens_details":{"reasoning_tokens":2}}}`
	for _, stream := range []bool{false, true} {
		name := "json"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/responses", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				var upstream dto.OpenAIResponsesRequest
				assert.NoError(t, common.DecodeJson(r.Body, &upstream))
				assert.Equal(t, stream, upstream.IsStream(nil))
				assert.Equal(t, `"rules"`, string(upstream.Instructions))
				if assert.NotNil(t, upstream.MaxOutputTokens) {
					assert.Equal(t, uint(512), *upstream.MaxOutputTokens)
				}
				if assert.NotNil(t, upstream.Reasoning) {
					assert.Equal(t, "high", upstream.Reasoning.Effort)
					assert.Empty(t, upstream.Reasoning.Summary)
				}
				assert.Equal(t, `"function"`, gjson.GetBytes(upstream.ToolChoice, "type").Raw)
				assert.Equal(t, "lookup", gjson.GetBytes(upstream.ToolChoice, "name").String())
				assert.Equal(t, "false", string(upstream.ParallelToolCalls))
				assert.Equal(t, "reasoning", gjson.GetBytes(upstream.Input, "1.type").String())
				assert.NotEmpty(t, gjson.GetBytes(upstream.Input, "1.id").String())
				assert.Equal(t, "[]", gjson.GetBytes(upstream.Input, "1.summary").Raw)
				assert.Equal(t, "prior thinking", gjson.GetBytes(upstream.Input, "1.content.0.text").String())
				assert.Equal(t, "function_call", gjson.GetBytes(upstream.Input, "3.type").String())
				assert.Equal(t, "toolu_1", gjson.GetBytes(upstream.Input, "4.call_id").String())
				assert.Equal(t, "first second", gjson.GetBytes(upstream.Input, "4.output").String())
				assert.Equal(t, "continue", gjson.GetBytes(upstream.Input, "5.content").String())
				if !stream {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, response)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				events := []string{
					`{"type":"response.created","response":{"id":"resp_1","model":"mistral-large-4","created_at":1795071161.5}}`,
					`{"type":"response.reasoning_text.delta","output_index":0,"delta":"check result"}`,
					`{"type":"response.reasoning_text.done","output_index":0,"text":"check result"}`,
					`{"type":"response.output_text.delta","output_index":1,"delta":"checking now"}`,
					`{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_next","name":"lookup"}}`,
					`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"q\":\"next\"}"}`,
					`{"type":"response.completed","response":` + response + `}`,
				}
				for _, event := range events {
					_, _ = io.WriteString(w, "data: "+event+"\n\n")
				}
			}))
			defer server.Close()

			var request dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(`{
				"model":"mistral-large-4","system":"rules","max_tokens":512,"temperature":0,
				"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},
				"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}],
				"tool_choice":{"type":"tool","name":"lookup","disable_parallel_tool_use":true},
				"messages":[
					{"role":"user","content":"start"},
					{"role":"assistant","content":[{"type":"thinking","thinking":"prior thinking"},{"type":"text","text":"looking"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"first"}}]},
					{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"first "},{"type":"text","text":"second"}]},{"type":"text","text":"continue"}]}
				]}`, &request))
			request.Stream = &stream
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeMistral, ChannelBaseUrl: server.URL,
					ApiKey: "test-key", UpstreamModelName: request.Model,
				},
				Request: &request, RelayFormat: types.RelayFormatClaude,
				RequestURLPath: "/v1/messages", IsStream: stream, DisablePing: true,
			}
			adaptor := &Adaptor{}
			converted, err := adaptor.ConvertClaudeRequest(c, info, &request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			resp, err := adaptor.DoRequest(c, info, strings.NewReader(string(body)))
			require.NoError(t, err)
			usage, apiErr := adaptor.DoResponse(c, resp.(*http.Response), info)
			require.Nil(t, apiErr)
			require.IsType(t, &dto.Usage{}, usage)
			assert.Equal(t, 12, usage.(*dto.Usage).PromptTokens)
			assert.Equal(t, 4, usage.(*dto.Usage).CompletionTokens)
			assert.Equal(t, 2, usage.(*dto.Usage).CompletionTokenDetails.ReasoningTokens)
			if stream {
				got := recorder.Body.String()
				assert.Contains(t, got, "event: message_start")
				assert.Contains(t, got, `"thinking":"check result"`)
				assert.Contains(t, got, `"text":"checking now"`)
				assert.Contains(t, got, `"id":"call_next"`)
				assert.Contains(t, got, `"partial_json":"{\"q\":\"next\"}"`)
				assert.Contains(t, got, `"stop_reason":"tool_use"`)
				assert.Contains(t, got, "event: message_stop")
				assert.Equal(t, 1, strings.Count(got, `"thinking":"check result"`))
				for _, line := range strings.Split(got, "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var event dto.ClaudeResponse
					require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &event))
					if event.Type == "message_delta" {
						require.NotNil(t, event.Usage)
						assert.Equal(t, 12, event.Usage.InputTokens)
						assert.Equal(t, 4, event.Usage.OutputTokens)
					}
				}
				return
			}
			var got dto.ClaudeResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &got))
			require.Len(t, got.Content, 3)
			assert.Equal(t, "thinking", got.Content[0].Type)
			require.NotNil(t, got.Content[0].Thinking)
			assert.Equal(t, "check result", *got.Content[0].Thinking)
			assert.Equal(t, "checking now", got.Content[1].GetText())
			assert.Equal(t, "call_next", got.Content[2].Id)
			assert.Equal(t, "tool_use", got.StopReason)
			assert.Equal(t, 12, got.Usage.InputTokens)
			assert.Equal(t, 4, got.Usage.OutputTokens)
		})
	}
}

func TestConvertClaudeRequestUsesMistralReasoningEffort(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		settings string
		effort   string
	}{
		{name: "enabled defaults to supported effort", model: "mistral-small-2603", settings: `"thinking":{"type":"enabled","budget_tokens":2048}`, effort: "high"},
		{name: "explicit effort", model: "mistral-large-4", settings: `"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}`, effort: "low"},
		{name: "disabled", model: "mistral-small-2603", settings: `"thinking":{"type":"disabled"}`, effort: "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"mistral-small-2603","max_tokens":4096,"messages":[{"role":"user","content":"hi"}],`+tt.settings+`}`, &request))
			request.Model = tt.model
			converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, nil, &request)
			require.NoError(t, err)
			responses, ok := converted.(*dto.OpenAIResponsesRequest)
			require.True(t, ok)
			require.NotNil(t, responses.Reasoning)
			assert.Equal(t, tt.effort, responses.Reasoning.Effort)
		})
	}
}

func TestConvertOpenAIResponsesRequestKeepsOnlyMistralFields(t *testing.T) {
	stream := true
	maxOutput := uint(512)
	request := dto.OpenAIResponsesRequest{
		Model:           "mistral-large-latest",
		Input:           json.RawMessage(`"hi"`),
		Instructions:    json.RawMessage(`"be brief"`),
		MaxOutputTokens: &maxOutput,
		Stream:          &stream,
		Reasoning:       &dto.Reasoning{Effort: "high", Summary: "auto"},
		Store:           json.RawMessage(`true`),
		Truncation:      json.RawMessage(`"auto"`),
		Include:         json.RawMessage(`["reasoning.encrypted_content"]`),
		ServiceTier:     "priority",
		Conversation:    json.RawMessage(`null`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}, request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(body, &got))
	assert.Equal(t, map[string]any{
		"model":             "mistral-large-latest",
		"input":             "hi",
		"instructions":      "be brief",
		"max_output_tokens": float64(512),
		"stream":            true,
		"reasoning":         map[string]any{"effort": "high"},
	}, got)
}

func TestConvertOpenAIResponsesRequestRejectsStatefulFields(t *testing.T) {
	tests := []struct {
		name    string
		request dto.OpenAIResponsesRequest
	}{
		{name: "previous_response_id", request: dto.OpenAIResponsesRequest{PreviousResponseID: "resp_1"}},
		{name: "conversation", request: dto.OpenAIResponsesRequest{Conversation: json.RawMessage(`"conv_1"`)}},
		{name: "prompt", request: dto.OpenAIResponsesRequest{Prompt: json.RawMessage(`{"id":"pmpt_1"}`)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.request.Model = "mistral-large-latest"
			_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}, tt.request)
			assert.ErrorContains(t, err, tt.name)
		})
	}
}

func TestNormalizeMistralChatPayloadFlattensContentChunks(t *testing.T) {
	tests := []struct {
		name          string
		payload       string
		stream        bool
		wantContent   any
		wantReasoning any
	}{
		{
			name:          "non-stream thinking and text",
			payload:       `{"id":"x","created":1795071161,"choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"think "},{"type":"text","text":"more"}],"closed":true},{"type":"text","text":"Hello"}]}}]}`,
			wantContent:   "Hello",
			wantReasoning: "think more",
		},
		{
			name:          "stream thinking-only delta omits content",
			payload:       `{"id":"x","choices":[{"index":0,"delta":{"index":0,"content":[{"type":"thinking","thinking":[{"type":"text","text":"The"}],"closed":true}]}}]}`,
			stream:        true,
			wantReasoning: "The",
		},
		{
			name:        "string content is untouched",
			payload:     `{"id":"x","choices":[{"index":0,"delta":{"content":"Hey"}}]}`,
			stream:      true,
			wantContent: "Hey",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Created int64 `json:"created"`
				Choices []struct {
					Message map[string]any `json:"message"`
					Delta   map[string]any `json:"delta"`
				} `json:"choices"`
			}
			require.NoError(t, common.Unmarshal(normalizeMistralChatPayload([]byte(tt.payload), tt.stream), &got))
			require.Len(t, got.Choices, 1)
			message := got.Choices[0].Message
			if tt.stream {
				message = got.Choices[0].Delta
			}
			assert.Equal(t, tt.wantContent, message["content"])
			assert.Equal(t, tt.wantReasoning, message["reasoning_content"])
			if !tt.stream {
				assert.Equal(t, int64(1795071161), got.Created)
			}
		})
	}
}

func TestNormalizeMistralChatBodyRewritesStreamLines(t *testing.T) {
	upstream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":[{\"type\":\"thinking\",\"thinking\":[{\"type\":\"text\",\"text\":\"hm\"}]}]}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	body, err := normalizeMistralChatBody(io.NopCloser(strings.NewReader(upstream)), true)
	require.NoError(t, err)
	defer body.Close()
	out, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hm\"},\"index\":0}]}\n\n"+
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n", string(out))
}
