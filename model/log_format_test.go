package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

// TestFormatUserLogsHidesModelMapping locks the user-facing contract of the
// "hide model mapping" option: with it on, neither the redirect markers in
// other nor the upstream model name inside the log content may reach a
// non-admin viewer; with it off, both stay as they are today.
func TestFormatUserLogsHidesModelMapping(t *testing.T) {
	original := common.IsHideModelMappingForUserEnabled()
	t.Cleanup(func() { common.SetHideModelMappingForUser(original) })

	newLog := func() *Log {
		return &Log{
			ModelName: "gpt-4",
			Content:   "上游报错：model gpt-4o-2024-08-06 does not exist",
			Other: common.MapToJsonStr(map[string]interface{}{
				"model_ratio":         2.5,
				"is_model_mapped":     true,
				"upstream_model_name": "gpt-4o-2024-08-06",
			}),
		}
	}

	t.Run("hidden", func(t *testing.T) {
		common.SetHideModelMappingForUser(true)
		logs := []*Log{newLog()}

		formatUserLogs(logs, 0)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.NotContains(t, parsed, "is_model_mapped")
		assert.NotContains(t, parsed, "upstream_model_name")
		assert.Contains(t, parsed, "model_ratio")
		assert.Equal(t, "上游报错：model gpt-4 does not exist", logs[0].Content)
	})

	t.Run("visible", func(t *testing.T) {
		common.SetHideModelMappingForUser(false)
		logs := []*Log{newLog()}

		formatUserLogs(logs, 0)

		parsed, err := common.StrToMap(logs[0].Other)
		require.NoError(t, err)
		assert.Equal(t, true, parsed["is_model_mapped"])
		assert.Equal(t, "gpt-4o-2024-08-06", parsed["upstream_model_name"])
		assert.Equal(t, "上游报错：model gpt-4o-2024-08-06 does not exist", logs[0].Content)
	})
}
