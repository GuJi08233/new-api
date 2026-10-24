package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeRequestMergesAllAssistantToolCalls(t *testing.T) {
	for _, tc := range []struct{ name, first, wantText string }{
		{"empty tool message", "", "next"},
		{"literal ellipsis", "...", "... next"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := dto.Message{Role: "assistant", Content: tc.first}
			first.SetToolCalls([]dto.ToolCallRequest{{ID: "call_1", Type: "function", Function: dto.FunctionRequest{Name: "one", Arguments: "{}"}}})
			second := dto.Message{Role: "assistant", Content: "next"}
			second.SetToolCalls([]dto.ToolCallRequest{{ID: "call_2", Type: "function", Function: dto.FunctionRequest{Name: "two", Arguments: "{}"}}})
			converted, err := OpenAIChatRequestToClaudeMessages(nil, dto.GeneralOpenAIRequest{
				Model: "claude-test", MaxTokens: common.GetPointer(uint(16)),
				Messages: []dto.Message{{Role: "user", Content: "run tools"}, first, second,
					{Role: "tool", ToolCallId: "call_1", Content: "ok"}, {Role: "tool", ToolCallId: "call_2", Content: "ok"}},
			})
			require.NoError(t, err)
			require.Len(t, converted.Messages, 3)
			blocks, ok := converted.Messages[1].Content.([]dto.ClaudeMediaMessage)
			require.True(t, ok)
			var calls []string
			var text string
			for _, block := range blocks {
				if block.Type == "tool_use" {
					calls = append(calls, block.Id)
				}
				if block.Type == "text" && block.Text != nil {
					text += *block.Text
				}
			}
			assert.Equal(t, []string{"call_1", "call_2"}, calls)
			assert.Equal(t, tc.wantText, text)
		})
	}
}

func TestClaudeStreamPreservesReasoningAndTextInOneChunk(t *testing.T) {
	delta := dto.ChatCompletionsStreamResponseChoiceDelta{}
	delta.SetReasoningContent("think")
	delta.SetContentString("answer")
	info := &relaycommon.RelayInfo{SendResponseCount: 1}
	events := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: delta}},
	}, info)
	var eventTypes, deltaTypes, values []string
	for _, event := range events {
		eventTypes = append(eventTypes, event.Type)
		if event.Type == "content_block_delta" {
			deltaTypes = append(deltaTypes, event.Delta.Type)
			if event.Delta.Thinking != nil {
				values = append(values, *event.Delta.Thinking)
			}
			if event.Delta.Text != nil {
				values = append(values, *event.Delta.Text)
			}
		}
	}
	assert.Equal(t, []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "content_block_start", "content_block_delta"}, eventTypes)
	assert.Equal(t, []string{"thinking_delta", "text_delta"}, deltaTypes)
	assert.Equal(t, []string{"think", "answer"}, values)
	require.NotNil(t, events[len(events)-1].Index)
	assert.Equal(t, 1, *events[len(events)-1].Index)
}
