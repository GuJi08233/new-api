package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateGroupBillingExprJSONString(t *testing.T) {
	require.NoError(t, ValidateGroupBillingExprJSONString(
		`{"default":{"model-a":"tier(\"base\", p * 2 + c * 10)"}}`,
	))

	err := ValidateGroupBillingExprJSONString(
		`{"vip":{"model-a":"p == 1000 ? -1 : p"}}`,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group vip model model-a")
	assert.Contains(t, err.Error(), "cannot be negative")
}
