package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMaskModelNameInText pins the boundary rules of user-facing masking: only a
// standalone occurrence of the upstream name is replaced, so names that merely
// share a prefix (gpt-4 vs gpt-4o, gpt-4-turbo) and versioned ids stay intact,
// while path prefixes, CJK text and sentence punctuation still count as edges.
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
			name:     "masks even when the request name contains the upstream name",
			text:     "The model `gpt-4` does not exist",
			upstream: "gpt-4",
			request:  "gpt-4-turbo",
			want:     "The model `gpt-4-turbo` does not exist",
		},
		{
			name:     "leaves the request name alone when it only starts with the upstream name",
			text:     "模型 gpt-4-turbo 当前分组下无可用渠道",
			upstream: "gpt-4",
			request:  "gpt-4-turbo",
			want:     "模型 gpt-4-turbo 当前分组下无可用渠道",
		},
		{
			name:     "leaves longer names that share the prefix",
			text:     "gpt-4 unavailable, try gpt-4o or gpt-40",
			upstream: "gpt-4",
			request:  "my-gpt",
			want:     "my-gpt unavailable, try gpt-4o or gpt-40",
		},
		{
			name:     "sentence punctuation is not part of the name",
			text:     "unsupported model gpt-4. Use another model: gpt-4:",
			upstream: "gpt-4",
			request:  "my-gpt",
			want:     "unsupported model my-gpt. Use another model: my-gpt:",
		},
		{
			name:     "versioned ids are different names",
			text:     "claude-3-5-sonnet@20240620 and llama3:8b are unavailable",
			upstream: "claude-3-5-sonnet",
			request:  "my-sonnet",
			want:     "claude-3-5-sonnet@20240620 and llama3:8b are unavailable",
		},
		{
			name:     "path prefix is a boundary",
			text:     "models/gemini-2.5-pro is not found for API version v1beta",
			upstream: "gemini-2.5-pro",
			request:  "my-pro",
			want:     "models/my-pro is not found for API version v1beta",
		},
		{
			name:     "cjk text is a boundary",
			text:     "模型gpt-4不存在，请检查gpt-4o是否可用",
			upstream: "gpt-4",
			request:  "my-gpt",
			want:     "模型my-gpt不存在，请检查gpt-4o是否可用",
		},
		{
			name:     "replaces every standalone occurrence",
			text:     "gpt-4 failed; retry gpt-4 later",
			upstream: "gpt-4",
			request:  "my-gpt",
			want:     "my-gpt failed; retry my-gpt later",
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
