package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelTestUsesNativeEndpointRequest(t *testing.T) {
	claude, ok := buildTestRequest("test", string(constant.EndpointTypeAnthropic), &model.Channel{}, true).(*dto.ClaudeRequest)
	require.True(t, ok)
	assert.True(t, *claude.Stream)
	assert.Equal(t, "hi", claude.Messages[0].Content)
	gemini, ok := buildTestRequest("test", string(constant.EndpointTypeGemini), &model.Channel{}, true).(*dto.GeminiChatRequest)
	require.True(t, ok)
	assert.Equal(t, "hi", gemini.Contents[0].Parts[0].Text)
	assert.Equal(t, uint(3000), *gemini.GenerationConfig.MaxOutputTokens)
}
