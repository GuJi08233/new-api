package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetReasoningEffortFromRequest(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"chat low", `{"reasoning_effort":"low"}`, "low"},
		{"chat high", `{"reasoning_effort":"high"}`, "high"},
		{"responses xhigh", `{"reasoning":{"effort":"xhigh"}}`, "xhigh"},
		{"explicit none", `{"reasoning_effort":"none"}`, "none"},
		{"claude", `{"output_config":{"effort":"max"}}`, "max"},
		{"gemini camel", `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"HIGH"}}}`, "HIGH"},
		{"gemini snake", `{"generation_config":{"thinking_config":{"thinking_level":"low"}}}`, "low"},
		{"gemini mixed", `{"generationConfig":{"thinking_config":{"thinkingLevel":"medium"}}}`, "medium"},
		{"absent", `{"model":"gpt-5-high"}`, ""},
		{"empty", `{"reasoning_effort":""}`, ""},
		{"whitespace", `{"reasoning_effort":"   "}`, ""},
		{"null", `{"reasoning_effort":null}`, ""},
		{"thinking budget", `{"thinking":{"type":"enabled","budget_tokens":4096}}`, ""},
		{"malformed", `{"reasoning_effort":"high",`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &RelayInfo{ReasoningEffort: "stale"}
			body := []byte(tc.body)
			SetReasoningEffortFromRequest(info, body)
			assert.Equal(t, tc.want, info.ReasoningEffort)
			require.Equal(t, tc.body, string(body))
		})
	}
	SetReasoningEffortFromRequest(nil, []byte(`{}`))
}

func TestReasoningEffortOpenRouterPrefersNestedEffort(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}}
	SetReasoningEffortFromRequest(info, []byte(`{"reasoning_effort":"low","reasoning":{"effort":"high"}}`))
	assert.Equal(t, "high", info.ReasoningEffort)
}
