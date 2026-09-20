package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Both System One paths must resolve to the same relay mode: the mode drives
// token estimation and channel re-routing, and a path that falls through to
// RelayModeUnknown silently skips both.
func TestPath2RelayModeSystemOne(t *testing.T) {
	tests := []struct {
		name string
		path string
		want int
	}{
		{name: "canonical path", path: "/v1/systemone", want: RelayModeTypeSafe},
		{name: "typesafe prefix alias", path: "/typesafe/v1/systemone", want: RelayModeTypeSafe},
		{name: "chat completions unaffected", path: "/v1/chat/completions", want: RelayModeChatCompletions},
		{name: "rerank unaffected", path: "/v1/rerank", want: RelayModeRerank},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Path2RelayMode(tt.path))
		})
	}
}
