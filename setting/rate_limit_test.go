package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRequestRateLimitGroupLegacyUpgradeAndValidation(t *testing.T) {
	prevGroups := ModelRequestRateLimitGroup
	prevTokens := ModelRequestRateLimitGroupTokens
	prevConc := ModelRequestRateLimitGroupConcurrency
	t.Cleanup(func() {
		ModelRequestRateLimitMutex.Lock()
		ModelRequestRateLimitGroup = prevGroups
		ModelRequestRateLimitGroupTokens = prevTokens
		ModelRequestRateLimitGroupConcurrency = prevConc
		ModelRequestRateLimitMutex.Unlock()
	})

	legacy := `{"default":[200,100],"pro":[10,5,50000],"vip":[0,1000,200000,15]}`
	require.NoError(t, CheckModelRequestRateLimitGroup(legacy))

	normalized, changed, err := NormalizeModelRequestRateLimitGroupJSON(legacy)
	require.NoError(t, err)
	assert.True(t, changed)

	normalizedAgain, changedAgain, err := NormalizeModelRequestRateLimitGroupJSON(normalized)
	require.NoError(t, err)
	assert.False(t, changedAgain)
	assert.Equal(t, normalized, normalizedAgain)

	require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(legacy))

	total, success, tokens, conc, found := GetGroupRateLimitConfig("default")
	require.True(t, found)
	assert.Equal(t, 200, total)
	assert.Equal(t, 100, success)
	assert.EqualValues(t, 0, tokens)
	assert.Equal(t, 0, conc)

	total, success, tokens, conc, found = GetGroupRateLimitConfig("pro")
	require.True(t, found)
	assert.Equal(t, 10, total)
	assert.Equal(t, 5, success)
	assert.EqualValues(t, 50000, tokens)
	assert.Equal(t, 0, conc)

	total, success, tokens, conc, found = GetGroupRateLimitConfig("vip")
	require.True(t, found)
	assert.Equal(t, 0, total)
	assert.Equal(t, 1000, success)
	assert.EqualValues(t, 200000, tokens)
	assert.Equal(t, 15, conc)

	// Re-serialization always emits full 4-element arrays.
	roundtrip := ModelRequestRateLimitGroup2JSONString()
	_, changedAfterRoundtrip, err := NormalizeModelRequestRateLimitGroupJSON(roundtrip)
	require.NoError(t, err)
	assert.False(t, changedAfterRoundtrip)

	assert.Error(t, CheckModelRequestRateLimitGroup(`{"bad":[1]}`))
	assert.Error(t, CheckModelRequestRateLimitGroup(`{"bad":[1,2,3,4,5]}`))
	assert.Error(t, CheckModelRequestRateLimitGroup(`{"bad":[1,1,-1,0]}`))
	assert.Error(t, CheckModelRequestRateLimitGroup(`{"bad":[1,1,0,-1]}`))
	assert.NoError(t, CheckModelRequestRateLimitTokenCount("0"))
	assert.NoError(t, CheckModelRequestRateLimitTokenCount("100000"))
	assert.Error(t, CheckModelRequestRateLimitTokenCount("-1"))
	assert.NoError(t, CheckModelRequestRateLimitConcurrency("0"))
	assert.NoError(t, CheckModelRequestRateLimitConcurrency("20"))
	assert.Error(t, CheckModelRequestRateLimitConcurrency("-1"))
}
