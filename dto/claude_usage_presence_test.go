package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeUsageMergeDistinguishesMissingAndExplicitZero(t *testing.T) {
	initial := &ClaudeUsage{InputTokens: 10, OutputTokens: 2, CacheReadInputTokens: 30}
	var delta ClaudeUsage
	require.NoError(t, common.UnmarshalJsonStr(`{"output_tokens":0}`, &delta))
	merged := MergeClaudeUsage(initial, &delta)
	assert.Equal(t, 10, merged.InputTokens)
	assert.Equal(t, 30, merged.CacheReadInputTokens)
	assert.Zero(t, merged.OutputTokens)
	assert.True(t, merged.HasOutputTokens())
	assert.True(t, merged.HasInputTokens(), "programmatic nonzero counters remain authoritative")
	assert.Equal(t, 2, initial.OutputTokens)
	assert.Nil(t, NewClaudeMessagesBillingUsage(&ClaudeUsage{}), "an empty programmatic placeholder must not override real usage")
	var zero ClaudeUsage
	require.NoError(t, common.UnmarshalJsonStr(`{"input_tokens":0,"output_tokens":0}`, &zero))
	assert.NotNil(t, NewClaudeMessagesBillingUsage(&zero), "upstream explicit zeros remain authoritative")
}
