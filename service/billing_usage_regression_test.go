package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingUsagePreservesResponsesCacheDetails(t *testing.T) {
	source := &dto.Usage{
		InputTokens: 100, OutputTokens: 10, TotalTokens: 110,
		InputTokensDetails:  &dto.InputTokenDetails{CachedTokens: 70, CacheWriteTokens: 5, ImageTokens: 20, AudioTokens: 3},
		PromptTokensDetails: dto.InputTokenDetails{ImageTokens: 12},
	}
	got := effectiveBillingUsage(&dto.Usage{BillingUsage: dto.NewOpenAIResponsesBillingUsage(source)})
	require.NotNil(t, got)
	assert.Equal(t, 100, got.PromptTokens)
	assert.Equal(t, 70, got.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 5, got.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, 12, got.PromptTokensDetails.ImageTokens)
	assert.Equal(t, 3, got.PromptTokensDetails.AudioTokens)
	assert.Zero(t, source.PromptTokensDetails.CachedTokens)
}

func TestBillingUsageExplicitClaudeCacheZeroWinsOverLegacySnapshot(t *testing.T) {
	for _, withObject := range []bool{false, true} {
		usage := &dto.ClaudeUsage{InputTokens: 100, ClaudeCacheCreation1hTokens: 90}
		if withObject {
			usage.CacheCreation = &dto.ClaudeCacheCreationUsage{}
		}
		got := effectiveBillingUsage(&dto.Usage{BillingUsage: dto.NewClaudeMessagesBillingUsage(usage)})
		want := 90
		if withObject {
			want = 0
		}
		assert.Equal(t, want, got.ClaudeCacheCreation1hTokens)
	}
}

func TestBillingUsageNormalizesGeminiModalityAndNegativeCompletion(t *testing.T) {
	got := effectiveBillingUsage(&dto.Usage{BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{
		PromptTokenCount: 100, TotalTokenCount: 90,
		PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: " audio ", TokenCount: 7}, {Modality: "AUDIO", TokenCount: 3}},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "image", TokenCount: 2}},
	})})
	require.NotNil(t, got)
	assert.Zero(t, got.CompletionTokens)
	assert.Equal(t, 10, got.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 2, got.CompletionTokenDetails.ImageTokens)
}
