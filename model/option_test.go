package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateOptionMapAppliesBooleanOptionsWithoutEnabledSuffix guards the
// dispatch gate in updateOptionMap: boolean options are only applied when the
// key ends with "Enabled" or is listed explicitly. A key that satisfies
// neither is silently persisted to the database while the in-memory flag stays
// at its default, so the feature it controls never turns on.
func TestUpdateOptionMapAppliesBooleanOptionsWithoutEnabledSuffix(t *testing.T) {
	original := common.IsHideModelMappingForUserEnabled()
	t.Cleanup(func() { common.SetHideModelMappingForUser(original) })
	common.SetHideModelMappingForUser(false)

	// updateOptionMap writes into the shared option map, which only exists
	// after InitOptionMap in a running server.
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
		t.Cleanup(func() { common.OptionMap = nil })
	}

	require.NoError(t, updateOptionMap("HideModelMappingForUser", "true"))
	assert.True(t, common.IsHideModelMappingForUserEnabled())
	assert.Equal(t, "true", common.OptionMap["HideModelMappingForUser"])

	require.NoError(t, updateOptionMap("HideModelMappingForUser", "false"))
	assert.False(t, common.IsHideModelMappingForUserEnabled())
}
