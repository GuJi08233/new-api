package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRemovedPaymentComplianceOptionsAreIgnored(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	const legacyKey = "payment_setting.compliance_confirmed"
	require.NoError(t, updateOptionMap(legacyKey, "true"))
	require.NoError(t, UpdateOption(legacyKey, "true"))
	require.NoError(t, UpdateOptionsBulk(map[string]string{legacyKey: "true"}))

	common.OptionMapRWMutex.RLock()
	_, exists := common.OptionMap[legacyKey]
	common.OptionMapRWMutex.RUnlock()
	require.False(t, exists)
}
