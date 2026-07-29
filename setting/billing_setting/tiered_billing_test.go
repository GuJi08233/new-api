package billing_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateExprMapJSONString(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantError string
	}{
		{
			name:  "valid expressions",
			value: `{"model-a":"tier(\"base\", p * 2 + c * 10)","model-b":"len > 1000 ? tier(\"large\", p * 3) : tier(\"base\", p)"}`,
		},
		{
			name:      "invalid json",
			value:     `{"model-a":`,
			wantError: "invalid tiered billing expression map",
		},
		{
			name:      "empty expression",
			value:     `{"model-a":" "}`,
			wantError: "empty tiered billing expression",
		},
		{
			name:      "invalid syntax",
			value:     `{"model-a":"p + ("}`,
			wantError: "invalid tiered billing expression",
		},
		{
			name:      "negative sampled charge",
			value:     `{"model-a":"p == 1000 ? -1 : p"}`,
			wantError: "cannot be negative",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateExprMapJSONString(tc.value)
			if tc.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantError)
		})
	}
}
