package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDecisionsTestUsesNativeNonStreamingRequests(t *testing.T) {
	gemini, ok := buildTestRequest("gemini-decision-1", "gemini-decisions", "test-nonce", true).(*dto.GeminiDecisionsRequest)
	require.True(t, ok)
	require.NoError(t, gemini.Validate())
	assert.False(t, gemini.IsStream(nil))
	assert.Contains(t, string(gemini.Input), "test-nonce")
	assert.Equal(t, "noul", gemini.Questions["is_greeting"].Type)
	openAI, ok := buildTestRequest("gpt-6-luna-decisions", "openai-decisions", "test-nonce", true).(*dto.OpenAIDecisionsRequest)
	require.True(t, ok)
	require.NoError(t, openAI.Validate())
	assert.False(t, openAI.IsStream(nil))
	assert.Contains(t, string(openAI.Input), "test-nonce")
	require.Len(t, openAI.Questions, 1)
	assert.Equal(t, "predicate", openAI.Questions[0].Type)
	assert.Equal(t, "gemini-decisions", normalizeChannelTestEndpoint(&model.Channel{Type: constant.ChannelTypeGemini}, "gemini-decision-1", ""))
	assert.Equal(t, "openai-decisions", normalizeChannelTestEndpoint(&model.Channel{Type: constant.ChannelTypeOpenAI}, "gpt-6-luna-decisions", ""))
	assert.Equal(t, "openai-response", normalizeChannelTestEndpoint(&model.Channel{Type: constant.ChannelTypeOpenAI}, "gpt-6-luna-decisions", "openai-response"))
}
