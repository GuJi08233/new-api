package geminichat

import (
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGeminiToolResultsMatchCallsAcrossContentBoundaries(t *testing.T) {
	request := &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{
		{Role: "model", Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{ID: "native-1", FunctionName: "first"}}, {FunctionCall: &dto.FunctionCall{FunctionName: "second"}}}},
		{Role: "user", Parts: []dto.GeminiPart{{FunctionResponse: &dto.GeminiFunctionResponse{Name: "second", Response: map[string]interface{}{"ok": true}}}, {FunctionResponse: &dto.GeminiFunctionResponse{Name: "first", Response: map[string]interface{}{"ok": true}}}}},
	}}
	got, err := GeminiGenerateContentRequestToOpenAIChat(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}})
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
	calls := got.Messages[0].ParseToolCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, "native-1", calls[0].ID)
	assert.Equal(t, calls[1].ID, got.Messages[1].ToolCallId)
	assert.Equal(t, calls[0].ID, got.Messages[2].ToolCallId)
}
