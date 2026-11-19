package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatCompletionsRequestToResponsesRequestReplaysReasoningBeforeToolCalls(t *testing.T) {
	reasoning := "check before calling"
	message := assistantMessageWithTool("", "call_1", "lookup", "{}")
	message.Content = nil
	message.ReasoningContent = &reasoning
	got, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "mistral-large-4", Messages: []dto.Message{message},
	})
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, common.Unmarshal(got.Input, &items))
	require.Len(t, items, 3)
	assert.NotEmpty(t, items[0]["id"])
	assert.Equal(t, "reasoning", items[0]["type"])
	assert.Equal(t, reasoning, gjson.GetBytes(got.Input, "0.content.0.text").String())
	assert.Equal(t, "assistant", items[1]["role"])
	assert.Equal(t, "function_call", items[2]["type"])
	assert.Equal(t, "call_1", items[2]["call_id"])
}

func TestChatCompletionsRequestToResponsesRequestInstructionsAndTools(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		N:     lo.ToPtr(1),
		Messages: []dto.Message{
			{Role: "system", Content: "system rules"},
			{Role: "developer", Content: "developer rules"},
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "look"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.test/a.png"}},
			}},
			assistantMessageWithTool("partial text", "call_1", "lookup", `{"q":"x"}`),
			{Role: "tool", ToolCallId: "call_1", Content: "tool result"},
		},
	}

	got, err := ChatCompletionsRequestToResponsesRequest(req)
	require.NoError(t, err)

	assert.Equal(t, "gpt-test", got.Model)
	assert.Equal(t, `"system rules\n\ndeveloper rules"`, string(got.Instructions))
	assert.Equal(t, "input_image", gjson.GetBytes(got.Input, "0.content.1.type").String())
	assert.Equal(t, "function_call", gjson.GetBytes(got.Input, "2.type").String())
	assert.Equal(t, "call_1", gjson.GetBytes(got.Input, "2.call_id").String())
	assert.Equal(t, "function_call_output", gjson.GetBytes(got.Input, "3.type").String())
}

func TestChatCompletionsRequestToResponsesRequestRejectsMultipleChoices(t *testing.T) {
	_, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		N:     lo.ToPtr(2),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "n>1")
}

func TestChatResponsesOutputLimitPreservesAliasPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name string
		old  *uint
		new  *uint
		want *uint
	}{
		{name: "absent"},
		{name: "legacy", old: lo.ToPtr(uint(128)), want: lo.ToPtr(uint(128))},
		{name: "new limit takes precedence", old: lo.ToPtr(uint(128)), new: lo.ToPtr(uint(64)), want: lo.ToPtr(uint(64))},
		{name: "explicit zero takes precedence", old: lo.ToPtr(uint(128)), new: lo.ToPtr(uint(0)), want: lo.ToPtr(uint(0))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{Model: "test", MaxTokens: tt.old, MaxCompletionTokens: tt.new})
			require.NoError(t, err)
			assert.Equal(t, tt.want, response.MaxOutputTokens)
		})
	}
}

func assistantMessageWithTool(content string, id string, name string, args string) dto.Message {
	msg := dto.Message{Role: "assistant", Content: content}
	msg.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   id,
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      name,
				Arguments: args,
			},
		},
	})
	return msg
}

func TestChatResponsesPreservesStructuredToolOutputsAndImageDetail(t *testing.T) {
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{
		"model":"test",
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"},"strict":false}}],
		"messages":[
			{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/a.png","detail":"low"}},{"type":"file","file":{"file_data":"data:application/pdf;base64,cGRm"}}]},
			{"role":"tool","tool_call_id":"call_1","content":[{"type":"text","text":"result"},{"type":"image_url","image_url":"https://example.test/result.png"},{"type":"file","file":{"file_url":"https://example.test/report.pdf","filename":"report.pdf"}}]}
		]}`, &request))
	response, err := ChatCompletionsRequestToResponsesRequest(&request)
	require.NoError(t, err)
	assert.Equal(t, "low", gjson.GetBytes(response.Input, "0.content.0.detail").String())
	assert.Equal(t, "data:application/pdf;base64,cGRm", gjson.GetBytes(response.Input, "0.content.1.file_data").String())
	assert.False(t, gjson.GetBytes(response.Input, "0.content.1.file").Exists())
	assert.Equal(t, "function_call_output", gjson.GetBytes(response.Input, "1.type").String())
	assert.True(t, gjson.GetBytes(response.Input, "1.output").IsArray())
	assert.Equal(t, "input_text", gjson.GetBytes(response.Input, "1.output.0.type").String())
	assert.Equal(t, "https://example.test/result.png", gjson.GetBytes(response.Input, "1.output.1.image_url").String())
	assert.False(t, gjson.GetBytes(response.Input, "1.output.1.detail").Exists())
	assert.Equal(t, "https://example.test/report.pdf", gjson.GetBytes(response.Input, "1.output.2.file_url").String())
	assert.Equal(t, "report.pdf", gjson.GetBytes(response.Input, "1.output.2.filename").String())
	assert.Equal(t, "false", gjson.GetBytes(response.Tools, "0.strict").Raw)
}
