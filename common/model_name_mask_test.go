package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMaskModelNameInText covers the substring hazard: when the requested model
// name contains the upstream one, a plain replace corrupts names that were
// already correct, so masking must be skipped instead.
func TestMaskModelNameInText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		upstream string
		request  string
		want     string
	}{
		{
			name:     "replaces upstream name",
			text:     "The model `gpt-4o-2024-08-06` does not exist",
			upstream: "gpt-4o-2024-08-06",
			request:  "gpt-4",
			want:     "The model `gpt-4` does not exist",
		},
		{
			name:     "skips when request name contains upstream name",
			text:     "模型 gpt-4-turbo 当前分组下无可用渠道",
			upstream: "gpt-4",
			request:  "gpt-4-turbo",
			want:     "模型 gpt-4-turbo 当前分组下无可用渠道",
		},
		{
			name:     "identical names are a no-op",
			text:     "model gpt-4 failed",
			upstream: "gpt-4",
			request:  "gpt-4",
			want:     "model gpt-4 failed",
		},
		{
			name:     "missing names are a no-op",
			text:     "model gpt-4 failed",
			upstream: "",
			request:  "gpt-4",
			want:     "model gpt-4 failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MaskModelNameInText(tt.text, tt.upstream, tt.request))
		})
	}
}
