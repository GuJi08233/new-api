package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiUsageMergePreservesEarlyCacheAndCombinesModalities(t *testing.T) {
	first := &GeminiUsageMetadata{
		PromptTokenCount: 100, CachedContentTokenCount: 40,
		PromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "audio", TokenCount: 7}, {Modality: " AUDIO ", TokenCount: 3}},
	}
	merged := MergeGeminiUsageMetadataNonZero(nil, first)
	merged = MergeGeminiUsageMetadataNonZero(merged, &GeminiUsageMetadata{
		CandidatesTokenCount: 15, TotalTokenCount: 115,
		PromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 0}},
	})
	require.NotNil(t, merged)
	assert.Equal(t, 40, merged.CachedContentTokenCount)
	assert.Equal(t, 100, merged.PromptTokenCount)
	assert.Equal(t, 15, merged.CandidatesTokenCount)
	assert.Equal(t, []GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 10}}, merged.PromptTokensDetails)
	assert.Equal(t, "audio", first.PromptTokensDetails[0].Modality)
}
