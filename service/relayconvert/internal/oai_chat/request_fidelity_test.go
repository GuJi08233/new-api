package oaichat

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestChatToResponsesPreservesCacheKeyBudgetAndZeroPenalties(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "qwen-test", Messages: []dto.Message{{Role: "user", Content: "hi"}}, PromptCacheKey: "a\"b\n", EnableThinking: []byte(`false`), ThinkingBudget: []byte(`0`), FrequencyPenalty: common.GetPointer(0.0), PresencePenalty: common.GetPointer(0.5)}
	response, err := ChatCompletionsRequestToResponsesRequest(request)
	require.NoError(t, err)
	data, err := common.Marshal(response)
	require.NoError(t, err)
	assert.Equal(t, request.PromptCacheKey, gjson.GetBytes(data, "prompt_cache_key").String())
	assert.True(t, gjson.GetBytes(data, "frequency_penalty").Exists())
	assert.Equal(t, 0.5, gjson.GetBytes(data, "presence_penalty").Float())
	assert.Equal(t, "0", gjson.GetBytes(data, "thinking_budget").Raw)
}

func TestChatToClaudePreservesParameterlessToolAndOmitsEmptyTools(t *testing.T) {
	request := dto.GeneralOpenAIRequest{Model: "claude-test", MaxTokens: common.GetPointer(uint(128)), Messages: []dto.Message{{Role: "user", Content: "hi"}}}
	converted, err := OpenAIChatRequestToClaudeMessages(nil, request)
	require.NoError(t, err)
	data, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.False(t, gjson.GetBytes(data, "tools").Exists())
	request.Tools = []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "time"}}}
	converted, err = OpenAIChatRequestToClaudeMessages(nil, request)
	require.NoError(t, err)
	data, err = common.Marshal(converted)
	require.NoError(t, err)
	assert.Equal(t, "time", gjson.GetBytes(data, "tools.0.name").String())
	assert.Equal(t, "object", gjson.GetBytes(data, "tools.0.input_schema.type").String())
}
