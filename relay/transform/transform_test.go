package transform

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseOpenAIToClaudePreservesReasoningAndTermination(t *testing.T) {
	for _, tt := range []struct {
		finish string
		stop   string
	}{
		{finish: "stop", stop: "end_turn"},
		{finish: "length", stop: "max_tokens"},
		{finish: "content_filter", stop: "refusal"},
	} {
		t.Run(tt.finish, func(t *testing.T) {
			body := `{"id":"test","choices":[{"message":{"role":"assistant","content":"answer","reasoning_content":"think"},"finish_reason":"` + tt.finish + `"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`
			result, err := ResponseOpenAIToClaude([]byte(body))
			require.NoError(t, err)
			var response struct {
				StopReason string           `json:"stop_reason"`
				Content    []map[string]any `json:"content"`
			}
			require.NoError(t, common.Unmarshal(result, &response))
			assert.Equal(t, tt.stop, response.StopReason)
			assert.Equal(t, []map[string]any{
				{"type": "thinking", "thinking": "think"},
				{"type": "text", "text": "answer"},
			}, response.Content)
		})
	}
}

func TestResponseOpenAIToClaudeRejectsInvalidToolArguments(t *testing.T) {
	for _, arguments := range []any{"{", "null", "[]", nil, 3} {
		body, err := common.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"tool_calls": []any{map[string]any{"id": "test", "function": map[string]any{"name": "lookup", "arguments": arguments}}},
		}}}})
		require.NoError(t, err)
		_, err = ResponseOpenAIToClaude(body)
		require.ErrorContains(t, err, "arguments")
	}
}

func TestRequestClaudeToOpenAIReasoningEffort(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		effort   string
	}{
		{name: "disabled", settings: `"thinking":{"type":"disabled"}`, effort: "none"},
		{name: "enabled", settings: `"thinking":{"type":"enabled","budget_tokens":2048}`, effort: "medium"},
		{name: "adaptive with explicit effort", settings: `"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}`, effort: "low"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converted, err := RequestClaudeToOpenAI([]byte(`{"model":"test","messages":[],`+tt.settings+`}`), TransformFlags{})
			require.NoError(t, err)
			var request map[string]any
			require.NoError(t, common.Unmarshal(converted, &request))
			assert.Equal(t, tt.effort, request["reasoning_effort"])
		})
	}
}

func TestClaudeContentOrderAndToolErrorsArePreserved(t *testing.T) {
	result, err := RequestClaudeToOpenAI([]byte(`{
		"model":"test","top_k":0,"messages":[{"role":"user","content":[
			{"type":"text","text":"before"},
			{"type":"tool_result","tool_use_id":"tool_1","is_error":true,"content":[{"type":"text","text":"failed"}]},
			{"type":"text","text":"look"},{"type":"image","source":{"type":"url","url":"https://example.test/a.png"}},
			{"type":"text","text":"then"},{"type":"document","source":{"type":"text","data":"document body"}}
		]}]}`), TransformFlags{})
	require.NoError(t, err)
	var request struct {
		TopK     *int `json:"top_k"`
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, common.Unmarshal(result, &request))
	require.NotNil(t, request.TopK)
	assert.Zero(t, *request.TopK)
	require.Len(t, request.Messages, 3)
	assert.Equal(t, "before", request.Messages[0].Content)
	assert.Equal(t, "tool", request.Messages[1].Role)
	assert.Equal(t, "Tool execution failed (is_error=true): failed", request.Messages[1].Content)
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "look"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.test/a.png"}},
		map[string]any{"type": "text", "text": "then"},
		map[string]any{"type": "text", "text": "document body"},
	}, request.Messages[2].Content)
}

func TestRequestOpenAIToClaude_Basic(t *testing.T) {
	openAIReq := `{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "user", "content": "Hello, world!"}
		],
		"temperature": 0.7,
		"max_tokens": 1000
	}`

	result, err := RequestOpenAIToClaude([]byte(openAIReq), TransformFlags{})
	assert.NoError(t, err)

	var claude map[string]any
	err = json.Unmarshal(result, &claude)
	assert.NoError(t, err)

	assert.Equal(t, "claude-3-5-sonnet-20241022", claude["model"])
	assert.Equal(t, float64(0.7), claude["temperature"])
	assert.Equal(t, float64(1000), claude["max_tokens"].(float64))

	messages, ok := claude["messages"].([]any)
	assert.True(t, ok)
	assert.Len(t, messages, 1)
}

func TestRequestOpenAIToClaude_SystemMessage(t *testing.T) {
	openAIReq := `{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant"},
			{"role": "user", "content": "Hello!"}
		]
	}`

	result, err := RequestOpenAIToClaude([]byte(openAIReq), TransformFlags{})
	assert.NoError(t, err)

	var claude map[string]any
	err = json.Unmarshal(result, &claude)
	assert.NoError(t, err)

	system, ok := claude["system"].([]any)
	assert.True(t, ok)
	assert.Len(t, system, 1)
}

func TestRequestOpenAIToClaude_ToolCalls(t *testing.T) {
	openAIReq := `{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "user", "content": "What's the weather?"}
		],
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "get_weather",
					"description": "Get weather information",
					"parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
				}
			}
		],
		"tool_choice": "auto"
	}`

	result, err := RequestOpenAIToClaude([]byte(openAIReq), TransformFlags{})
	assert.NoError(t, err)

	var claude map[string]any
	err = json.Unmarshal(result, &claude)
	assert.NoError(t, err)

	tools, ok := claude["tools"].([]any)
	assert.True(t, ok)
	assert.Len(t, tools, 1)

	toolChoice, ok := claude["tool_choice"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "auto", toolChoice["type"])
}

func TestRequestOpenAIToClaude_ReasoningEffort(t *testing.T) {
	tests := []struct {
		name     string
		req      string
		wantType string
	}{
		{
			name:     "low effort",
			req:      `{"model": "test", "messages": [], "reasoning_effort": "low"}`,
			wantType: "enabled",
		},
		{
			name:     "medium effort",
			req:      `{"model": "test", "messages": [], "reasoning_effort": "medium"}`,
			wantType: "enabled",
		},
		{
			name:     "high effort",
			req:      `{"model": "test", "messages": [], "reasoning_effort": "high"}`,
			wantType: "enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RequestOpenAIToClaude([]byte(tt.req), TransformFlags{})
			assert.NoError(t, err)

			var claude map[string]any
			err = json.Unmarshal(result, &claude)
			assert.NoError(t, err)

			thinking, ok := claude["thinking"].(map[string]any)
			assert.True(t, ok, "thinking should be present")
			assert.Equal(t, tt.wantType, thinking["type"])
		})
	}
}

func TestRequestOpenAIToClaude_CodexOAuth(t *testing.T) {
	openAIReq := `{
		"model": "gpt-4.5",
		"messages": [{"role": "user", "content": "Hello"}],
		"temperature": 0.7,
		"max_tokens": 1000
	}`

	flags := TransformFlags{
		IsCodexOAuth:  true,
		CodexFastMode: true,
	}

	result, err := RequestOpenAIToClaude([]byte(openAIReq), flags)
	assert.NoError(t, err)

	var claude map[string]any
	err = json.Unmarshal(result, &claude)
	assert.NoError(t, err)

	// Max tokens should be removed for Codex OAuth
	_, hasMaxTokens := claude["max_tokens"]
	assert.False(t, hasMaxTokens, "max_tokens should be removed for Codex OAuth")

	// Temperature should be removed
	_, hasTemp := claude["temperature"]
	assert.False(t, hasTemp, "temperature should be removed for Codex OAuth")

	// Store should be false
	assert.Equal(t, false, claude["store"])

	// Include should have reasoning
	include, ok := claude["include"].([]any)
	assert.True(t, ok)
	assert.Contains(t, include, "reasoning.encrypted_content")

	// Service tier should be priority
	assert.Equal(t, "priority", claude["service_tier"])
}

func TestRequestOpenAIToClaude_OSeries(t *testing.T) {
	openAIReq := `{
		"model": "o3-mini",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1000
	}`

	flags := TransformFlags{
		OSeriesMode: true,
	}

	result, err := RequestOpenAIToClaude([]byte(openAIReq), flags)
	assert.NoError(t, err)

	var claude map[string]any
	err = json.Unmarshal(result, &claude)
	assert.NoError(t, err)

	// O-series should use max_completion_tokens
	_, hasMaxTokens := claude["max_tokens"]
	assert.False(t, hasMaxTokens, "max_tokens should not be present for o-series")

	_, hasMaxCompletion := claude["max_completion_tokens"]
	assert.True(t, hasMaxCompletion, "max_completion_tokens should be present for o-series")
}

func TestResponseClaudeToOpenAI_Basic(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-5-sonnet-20241022",
		"content": [
			{"type": "text", "text": "Hello! How can I help you?"}
		],
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 10,
			"output_tokens": 20
		}
	}`

	result, err := ResponseClaudeToOpenAI([]byte(claudeResp))
	assert.NoError(t, err)

	var openAI map[string]any
	err = json.Unmarshal(result, &openAI)
	assert.NoError(t, err)

	assert.Equal(t, "msg_123", openAI["id"])
	assert.Equal(t, "chat.completion", openAI["object"])
	assert.Equal(t, "claude-3-5-sonnet-20241022", openAI["model"])

	choices, ok := openAI["choices"].([]any)
	assert.True(t, ok)
	assert.Len(t, choices, 1)

	usage, ok := openAI["usage"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, float64(10), usage["prompt_tokens"])
	assert.Equal(t, float64(20), usage["completion_tokens"])
	assert.Equal(t, float64(30), usage["total_tokens"])
}

func TestResponseClaudeToOpenAI_ToolCalls(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-5-sonnet-20241022",
		"content": [
			{"type": "tool_use", "id": "toolu_123", "name": "get_weather", "input": {"city": "London"}}
		],
		"stop_reason": "tool_use"
	}`

	result, err := ResponseClaudeToOpenAI([]byte(claudeResp))
	assert.NoError(t, err)

	var openAI map[string]any
	err = json.Unmarshal(result, &openAI)
	assert.NoError(t, err)

	choices, ok := openAI["choices"].([]any)
	assert.True(t, ok)

	choice := choices[0].(map[string]any)
	message := choice["message"].(map[string]any)
	toolCalls := message["tool_calls"].([]any)
	assert.Len(t, toolCalls, 1)

	tc := toolCalls[0].(map[string]any)
	assert.Equal(t, "toolu_123", tc["id"])
}

func TestResolveReasoningEffort(t *testing.T) {
	tests := []struct {
		name     string
		req      map[string]any
		expected string
	}{
		{
			name:     "output_config effort high",
			req:      map[string]any{"output_config": map[string]any{"effort": "high"}},
			expected: "high",
		},
		{
			name:     "thinking adaptive",
			req:      map[string]any{"thinking": map[string]any{"type": "adaptive"}},
			expected: "xhigh",
		},
		{
			name:     "thinking enabled low budget",
			req:      map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": float64(1000)}},
			expected: "low",
		},
		{
			name:     "thinking enabled medium budget",
			req:      map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": float64(8000)}},
			expected: "medium",
		},
		{
			name:     "thinking enabled high budget",
			req:      map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": float64(20000)}},
			expected: "high",
		},
		{
			name:     "reasoning_effort medium",
			req:      map[string]any{"reasoning_effort": "medium"},
			expected: "medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveReasoningEffort(tt.req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapClaudeStopReason(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"stop_sequence", "stop"},
		{"tool_use", "tool_calls"},
		{"", "stop"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapClaudeStopReason(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapClaudeUsageToOpenAI(t *testing.T) {
	usage := map[string]any{
		"input_tokens":  100,
		"output_tokens": 50,
	}

	result := mapClaudeUsageToOpenAI(usage)
	assert.Equal(t, 100, result["prompt_tokens"].(int))
	assert.Equal(t, 50, result["completion_tokens"].(int))
	assert.Equal(t, 150, result["total_tokens"].(int))
}

// ============================================================================
// Reverse Direction Tests: Claude -> OpenAI
// ============================================================================

func TestRequestClaudeToOpenAI_Basic(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-sonnet-20240229",
		"messages": [
			{"role": "user", "content": "Hello, world!"}
		],
		"temperature": 0.7,
		"max_tokens": 1000
	}`

	result, err := RequestClaudeToOpenAI([]byte(claudeReq), TransformFlags{})
	assert.NoError(t, err)

	var openAI map[string]any
	err = json.Unmarshal(result, &openAI)
	assert.NoError(t, err)

	assert.Equal(t, "claude-3-sonnet-20240229", openAI["model"])
	assert.Equal(t, float64(0.7), openAI["temperature"])
	assert.Equal(t, float64(1000), openAI["max_tokens"])

	messages, ok := openAI["messages"].([]any)
	assert.True(t, ok)
	assert.Len(t, messages, 1)
}

func TestRequestClaudeToOpenAI_WithSystem(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-sonnet-20240229",
		"system": "You are a helpful assistant",
		"messages": [
			{"role": "user", "content": "Hello!"}
		]
	}`

	result, err := RequestClaudeToOpenAI([]byte(claudeReq), TransformFlags{})
	assert.NoError(t, err)

	var openAI map[string]any
	err = json.Unmarshal(result, &openAI)
	assert.NoError(t, err)

	messages := openAI["messages"].([]any)
	assert.Len(t, messages, 2)

	firstMsg := messages[0].(map[string]any)
	assert.Equal(t, "system", firstMsg["role"])
	assert.Equal(t, "You are a helpful assistant", firstMsg["content"])
}

func TestRequestClaudeToOpenAI_Tools(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-sonnet-20240229",
		"messages": [{"role": "user", "content": "What's the weather?"}],
		"tools": [
			{
				"name": "get_weather",
				"description": "Get weather info",
				"input_schema": {"type": "object", "properties": {"city": {"type": "string"}}}
			}
		],
		"tool_choice": {"type": "auto"}
	}`

	result, err := RequestClaudeToOpenAI([]byte(claudeReq), TransformFlags{})
	assert.NoError(t, err)

	var openAI map[string]any
	err = json.Unmarshal(result, &openAI)
	assert.NoError(t, err)

	tools, ok := openAI["tools"].([]any)
	assert.True(t, ok)
	assert.Len(t, tools, 1)

	assert.Equal(t, "auto", openAI["tool_choice"])
}

func TestRequestClaudeToOpenAI_ArraySystem(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-sonnet-20240229",
		"system": [{"type": "text", "text": "You are a helpful assistant"}],
		"messages": [
			{"role": "user", "content": "Hello!"}
		]
	}`

	result, err := RequestClaudeToOpenAI([]byte(claudeReq), TransformFlags{})
	assert.NoError(t, err)

	var openAI map[string]any
	err = json.Unmarshal(result, &openAI)
	assert.NoError(t, err)

	messages := openAI["messages"].([]any)
	assert.Len(t, messages, 2)
	assert.Equal(t, "system", messages[0].(map[string]any)["role"])
}

func TestResponseOpenAIToClaude_Basic(t *testing.T) {
	openAIResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1712345678,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello there!"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`

	result, err := ResponseOpenAIToClaude([]byte(openAIResp))
	assert.NoError(t, err)

	var claude map[string]any
	err = json.Unmarshal(result, &claude)
	assert.NoError(t, err)

	assert.Equal(t, "chatcmpl-123", claude["id"])
	assert.Equal(t, "message", claude["type"])
	assert.Equal(t, "assistant", claude["role"])
	assert.Equal(t, "end_turn", claude["stop_reason"])

	content, ok := claude["content"].([]any)
	assert.True(t, ok)
	assert.Len(t, content, 1)
	assert.Equal(t, "text", content[0].(map[string]any)["type"])
	assert.Equal(t, "Hello there!", content[0].(map[string]any)["text"])
}

func TestResponseOpenAIToClaude_ToolCalls(t *testing.T) {
	openAIResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"tool_calls": [
					{
						"id": "call_123",
						"type": "function",
						"function": {
							"name": "get_weather",
							"arguments": "{\"city\":\"London\"}"
						}
					}
				]
			},
			"finish_reason": "tool_calls"
		}]
	}`

	result, err := ResponseOpenAIToClaude([]byte(openAIResp))
	assert.NoError(t, err)

	var claude map[string]any
	err = json.Unmarshal(result, &claude)
	assert.NoError(t, err)

	assert.Equal(t, "tool_use", claude["stop_reason"])

	content := claude["content"].([]any)
	assert.Len(t, content, 1)

	toolUse := content[0].(map[string]any)
	assert.Equal(t, "tool_use", toolUse["type"])
	assert.Equal(t, "call_123", toolUse["id"])
	assert.Equal(t, "get_weather", toolUse["name"])
}
