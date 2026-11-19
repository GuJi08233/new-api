package mistral

import (
	"encoding/json"
	"fmt"
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
		"seed":0,"min_tokens":0,"top_k":0,"safe_prompt":false,"prompt_logprobs":false,
		"top_prompt_logprobs":0,"repetition_penalty":0,"prompt_mode":"reasoning","service_tier":"standard_only",
		"guardrails":[{"name":"test"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"},"strict":false}}],
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
	for _, field := range []string{"random_seed", "min_tokens", "top_k", "top_prompt_logprobs", "repetition_penalty"} {
		assert.Equal(t, "0", gjson.GetBytes(body, field).Raw, field)
	}
	for _, field := range []string{"safe_prompt", "prompt_logprobs", "tools.0.function.strict"} {
		assert.Equal(t, "false", gjson.GetBytes(body, field).Raw, field)
	}
	assert.False(t, gjson.GetBytes(body, "seed").Exists())
	assert.Equal(t, "standard_only", gjson.GetBytes(body, "service_tier").String())
	assert.Equal(t, "reasoning", gjson.GetBytes(body, "prompt_mode").String())
	assert.JSONEq(t, `[{"name":"test"}]`, gjson.GetBytes(body, "guardrails").Raw)
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
				for _, index := range []string{"0", "2", "5"} {
					assert.Equal(t, "message", gjson.GetBytes(upstream.Input, index+".type").String())
				}
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
		Store:           json.RawMessage(`false`),
		Truncation:      json.RawMessage(`"disabled"`),
		Background:      common.GetPointer(false),
		TopLogProbs:     common.GetPointer(0),
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

func TestConvertOpenAIResponsesRequestNormalizesMessageTypes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "string prompt",
			input: `"hello"`,
			want:  `"hello"`,
		},
		{
			name:  "easy input messages",
			input: `[{"role":"system","content":"rules"},{"role":"developer","content":"brief"},{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"https://example.test/a.png"}]},{"role":"assistant","content":"hi"}]`,
			want:  `[{"type":"message","role":"system","content":"rules"},{"type":"message","role":"developer","content":"brief"},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"https://example.test/a.png"}]},{"type":"message","role":"assistant","content":"hi"}]`,
		},
		{
			name:  "assistant output_text replay",
			input: `[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"output_text","text":"ok"}]},{"role":"user","content":"again"}]`,
			want:  `[{"type":"message","role":"user","content":"hi"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]},{"type":"message","role":"user","content":"again"}]`,
		},
		{
			name:  "null message type",
			input: `[{"type":null,"role":"user","content":"hello"}]`,
			want:  `[{"type":"message","role":"user","content":"hello"}]`,
		},
		{
			name:  "history preserves typed items and extra fields",
			input: `[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"checking","annotations":[]}]},{"type":"reasoning","id":"rs_1","summary":[]},{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":"result"},{"role":"user","content":"continue","extra":{"sequence":9007199254740993}}]`,
			want:  `[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"checking","annotations":[]}]},{"type":"reasoning","id":"rs_1","summary":[]},{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":"result"},{"type":"message","role":"user","content":"continue","extra":{"sequence":9007199254740993}}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := dto.OpenAIResponsesRequest{Model: "mistral-large-latest", Input: json.RawMessage(tt.input)}
			converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, request)
			require.NoError(t, err)
			got, ok := converted.(*dto.OpenAIResponsesRequest)
			require.True(t, ok)
			assert.JSONEq(t, tt.want, string(got.Input))
			assert.Equal(t, tt.input, string(request.Input))
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

func TestConvertChatNativeSeedAndStrictFunctions(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "native seed precision", body: `"random_seed":9223372036854775807`, want: "9223372036854775807"},
		{name: "OpenAI seed", body: `"seed":42`, want: "42"},
		{name: "matching aliases", body: `"seed":0,"random_seed":0`, want: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"test","messages":[{"role":"user","content":"hi"}],`+tt.body+`,"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"},"strict":true}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`, &request))
			converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, &request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.Equal(t, tt.want, gjson.GetBytes(body, "random_seed").Raw)
			assert.Equal(t, "lookup", gjson.GetBytes(body, "tools.0.function.name").String())
			assert.True(t, gjson.GetBytes(body, "tools.0.function.strict").Bool())
			assert.JSONEq(t, `{"type":"function","function":{"name":"lookup"}}`, gjson.GetBytes(body, "tool_choice").Raw)
			assert.False(t, gjson.GetBytes(body, "functions").Exists())
			assert.False(t, gjson.GetBytes(body, "function_call").Exists())
		})
	}
}

func TestMistralRejectsUnsupportedRequestSemanticsWith400(t *testing.T) {
	for _, field := range []string{
		`"store":true`, `"truncation":"auto"`, `"background":true`, `"top_logprobs":1`,
		`"moderation":{}`, `"frequency_penalty":0.5`, `"presence_penalty":0.5`,
	} {
		t.Run(field, func(t *testing.T) {
			var request dto.OpenAIResponsesRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"test","input":"hi",`+field+`}`, &request))
			_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, request)
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
		})
	}
	for _, input := range []string{
		`[{"type":"item_reference","id":"msg_1"}]`, `[{"id":"unknown"}]`, `[null]`,
		`[{"role":null,"content":"hi"}]`, `[{"type":3}]`, `[{"type":"custom_tool_call"}]`,
		`{}`, `null`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(input)})
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Contains(t, apiErr.Error(), "input")
		})
	}
	for _, field := range []string{
		`"seed":1.5`, `"seed":9223372036854775808`, `"seed":-9223372036854777856`,
		`"seed":1,"random_seed":2`, `"min_tokens":2,"max_tokens":1`,
		`"tools":[{"type":"web_search"}]`,
		`"functions":[{"name":"lookup"}]`, `"function_call":{"name":"lookup"}`,
	} {
		t.Run(field, func(t *testing.T) {
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"test","messages":[{"role":"user","content":"hi"}],`+field+`}`, &request))
			_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, &request)
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		})
	}
}

func TestClaudeMultimodalAndStructuredOutputViaResponses(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{
		"model":"test","max_tokens":256,"metadata":{"user_id":"test-user"},
		"tools":[{"name":"lookup","strict":false,"input_schema":{"type":"object","properties":{}}}],
		"output_config":{"format":{"type":"json_schema","schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}},
		"messages":[
			{"role":"user","content":[{"type":"text","text":"before"},{"type":"image","source":{"type":"url","url":"https://example.test/one.png"}},{"type":"text","text":"after"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_1","is_error":true,"content":[
					{"type":"text","text":"result"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}},
					{"type":"document","title":"report.pdf","source":{"type":"url","url":"https://example.test/report.pdf"}},
					{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"cGRm"}}
				]},
				{"type":"text","text":"continue"}
			]}
		]}`, &request))
	before, err := common.Marshal(request)
	require.NoError(t, err)
	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, nil, &request)
	require.NoError(t, err)
	responses, ok := converted.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.Equal(t, "before", gjson.GetBytes(responses.Input, "0.content.0.text").String())
	assert.Equal(t, "https://example.test/one.png", gjson.GetBytes(responses.Input, "0.content.1.image_url").String())
	assert.Equal(t, "after", gjson.GetBytes(responses.Input, "0.content.2.text").String())
	assert.Equal(t, "function_call_output", gjson.GetBytes(responses.Input, "3.type").String())
	assert.Contains(t, gjson.GetBytes(responses.Input, "3.output.0.text").String(), "is_error=true")
	assert.Equal(t, "result", gjson.GetBytes(responses.Input, "3.output.1.text").String())
	assert.Equal(t, "data:image/png;base64,aGVsbG8=", gjson.GetBytes(responses.Input, "3.output.2.image_url").String())
	assert.Equal(t, "input_file", gjson.GetBytes(responses.Input, "3.output.3.type").String())
	assert.Equal(t, "https://example.test/report.pdf", gjson.GetBytes(responses.Input, "3.output.3.file_url").String())
	assert.Equal(t, "report.pdf", gjson.GetBytes(responses.Input, "3.output.3.filename").String())
	assert.Equal(t, "data:application/pdf;base64,cGRm", gjson.GetBytes(responses.Input, "3.output.4.file_data").String())
	assert.Equal(t, "continue", gjson.GetBytes(responses.Input, "4.content").String())
	assert.Equal(t, "false", gjson.GetBytes(responses.Tools, "0.strict").Raw)
	assert.Equal(t, "json_schema", gjson.GetBytes(responses.Text, "format.type").String())
	assert.Equal(t, "string", gjson.GetBytes(responses.Text, "format.schema.properties.answer.type").String())
	assert.Equal(t, "true", gjson.GetBytes(responses.Text, "format.strict").Raw)
	assert.Equal(t, "test-user", gjson.GetBytes(responses.Metadata, "user_id").String())
	after, err := common.Marshal(request)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
}

func TestClaudeSamplingControlsUseMistralChat(t *testing.T) {
	service.InitHttpClient()
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/chat/completions", r.URL.Path)
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Equal(t, "0", gjson.GetBytes(body, "top_k").Raw)
				assert.JSONEq(t, `["STOP"]`, gjson.GetBytes(body, "stop").Raw)
				assert.Equal(t, "json_schema", gjson.GetBytes(body, "response_format.type").String())
				assert.Equal(t, "true", gjson.GetBytes(body, "tools.0.function.strict").Raw)
				assert.Equal(t, "before", gjson.GetBytes(body, "messages.0.content.0.text").String())
				assert.Equal(t, "https://example.test/a.png", gjson.GetBytes(body, "messages.0.content.1.image_url").String())
				assert.Equal(t, "after", gjson.GetBytes(body, "messages.0.content.2.text").String())
				assert.Equal(t, "https://example.test/a.pdf", gjson.GetBytes(body, "messages.0.content.3.document_url").String())
				if !stream {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"id":"chat_1","object":"chat.completion","created":1,"model":"test","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"text","text":"ok"}]},"finish_reason":"model_length"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: "+`{"id":"chat_1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`+"\n\n"+
					"data: "+`{"id":"chat_1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":[{"type":"text","text":"ok"}]},"finish_reason":"model_length"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`+"\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			var request dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"test","max_tokens":256,"stop_sequences":["STOP"],"top_k":0,
				"output_format":{"type":"json_schema","schema":{"type":"object"}},
				"tools":[{"name":"lookup","strict":true,"input_schema":{"type":"object"}}],
				"messages":[{"role":"user","content":[
					{"type":"text","text":"before"},{"type":"image","source":{"type":"url","url":"https://example.test/a.png"}},
					{"type":"text","text":"after"},{"type":"document","source":{"type":"url","url":"https://example.test/a.pdf"}}
				]}]}`, &request))
			request.Stream = &stream
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral, ChannelBaseUrl: server.URL, UpstreamModelName: "test", ApiKey: "test-key"},
				RelayFormat: types.RelayFormatClaude, RequestURLPath: "/v1/messages", Request: &request, IsStream: stream, DisablePing: true,
			}
			adaptor := &Adaptor{}
			converted, err := adaptor.ConvertClaudeRequest(c, info, &request)
			require.NoError(t, err)
			require.IsType(t, &dto.GeneralOpenAIRequest{}, converted)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			resp, err := adaptor.DoRequest(c, info, strings.NewReader(string(body)))
			require.NoError(t, err)
			usage, apiErr := adaptor.DoResponse(c, resp.(*http.Response), info)
			require.Nil(t, apiErr)
			require.IsType(t, &dto.Usage{}, usage)
			assert.Equal(t, 10, usage.(*dto.Usage).PromptTokens)
			assert.Equal(t, 2, usage.(*dto.Usage).CompletionTokens)
			if stream {
				assert.Contains(t, recorder.Body.String(), `"text":"ok"`)
				assert.Contains(t, recorder.Body.String(), `"stop_reason":"max_tokens"`)
				assert.Equal(t, 1, strings.Count(recorder.Body.String(), "event: message_stop"))
			} else {
				assert.Equal(t, "ok", gjson.GetBytes(recorder.Body.Bytes(), "content.0.text").String())
				assert.Equal(t, "max_tokens", gjson.GetBytes(recorder.Body.Bytes(), "stop_reason").String())
			}
		})
	}
}

func TestClaudeRejectsUntranslatableFeatures(t *testing.T) {
	for _, fields := range []string{
		`"container":"container_1"`, `"context_management":{"edits":[]}`, `"mcp_servers":[{"type":"url","url":"https://example.test"}]`,
		`"tools":[{"type":"web_search_20250305","name":"web_search"}]`,
		`"messages":[{"role":"user","content":[{"type":"image","source":{"type":"unknown"}}]}]`,
		`"messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"opaque"}]}]`,
		`"output_config":{"format":"json"}`, `"output_format":{"type":"json_object"}`, `"output_config":[]`,
	} {
		t.Run(fields, func(t *testing.T) {
			var request dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"test","max_tokens":256,`+fields+`}`, &request))
			_, err := (&Adaptor{}).ConvertClaudeRequest(nil, nil, &request)
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		})
	}
}

func TestForcedResponsesDoesNotDiscardChatControls(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Stop: []string{"STOP"}}
	info := &relaycommon.RelayInfo{Request: request}
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(`"hi"`)})
	require.ErrorContains(t, err, "stop")
	info.Request = &dto.ClaudeRequest{TopK: common.GetPointer(0)}
	_, err = (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(`"hi"`)})
	require.ErrorContains(t, err, "top_k")
}
