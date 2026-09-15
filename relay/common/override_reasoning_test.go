package common

import (
	"testing"

	common2 "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// 参数覆盖改写或删除推理参数后，日志里的 reasoning_effort 必须跟随实际发往上游的请求体；
// 覆盖没有触及推理参数时保留处理器或转换器记录的原值。
func TestApplyParamOverrideWithRelayInfoSynchronizesReasoningEffort(t *testing.T) {
	originalDebugEnabled := common2.DebugEnabled
	common2.DebugEnabled = false
	t.Cleanup(func() { common2.DebugEnabled = originalDebugEnabled })

	tests := []struct {
		name          string
		relayFormat   types.RelayFormat
		initialEffort string
		input         string
		operation     map[string]interface{}
		expected      string
	}{
		{"Responses set", types.RelayFormatOpenAIResponses, "high", `{"reasoning":{"effort":"high"}}`, map[string]interface{}{"mode": "set", "path": "reasoning.effort", "value": "max"}, "max"},
		{"chat delete", types.RelayFormatOpenAI, "high", `{"reasoning_effort":"high"}`, map[string]interface{}{"mode": "delete", "path": "reasoning_effort"}, ""},
		{"chat set when the client sent none", types.RelayFormatOpenAI, "", `{"model":"gpt-5"}`, map[string]interface{}{"mode": "set", "path": "reasoning_effort", "value": "low"}, "low"},
		{"OpenRouter nested set", types.RelayFormatOpenAI, "medium", `{"reasoning":{"effort":"medium"}}`, map[string]interface{}{"mode": "set", "path": "reasoning.effort", "value": "xhigh"}, "xhigh"},
		{"Claude output config set", types.RelayFormatClaude, "high", `{"output_config":{"effort":"high"}}`, map[string]interface{}{"mode": "set", "path": "output_config.effort", "value": "max"}, "max"},
		{"Claude thinking budget set", types.RelayFormatClaude, "enabled (budget 1024)", `{"thinking":{"type":"enabled","budget_tokens":1024}}`, map[string]interface{}{"mode": "set", "path": "thinking.budget_tokens", "value": 2048}, "enabled (budget 2048)"},
		{"Claude thinking delete", types.RelayFormatClaude, "enabled (budget 1024)", `{"thinking":{"type":"enabled","budget_tokens":1024},"max_tokens":4096}`, map[string]interface{}{"mode": "delete", "path": "thinking"}, ""},
		{"Gemini thinking level set", types.RelayFormatGemini, "medium", `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"medium"}}}`, map[string]interface{}{"mode": "set", "path": "generationConfig.thinkingConfig.thinkingLevel", "value": "high"}, "high"},
		{"Gemini thinking budget disabled", types.RelayFormatGemini, "budget 2048", `{"generationConfig":{"thinkingConfig":{"thinkingBudget":2048}}}`, map[string]interface{}{"mode": "set", "path": "generationConfig.thinkingConfig.thinkingBudget", "value": 0}, "disabled"},
		{"non-string value clears effort", types.RelayFormatOpenAIResponses, "high", `{"reasoning":{"effort":"high"}}`, map[string]interface{}{"mode": "set", "path": "reasoning.effort", "value": 42}, ""},
		{"unrelated override preserves converter-derived effort", types.RelayFormatClaude, "high", `{"thinking":{"type":"adaptive"},"max_tokens":4096}`, map[string]interface{}{"mode": "set", "path": "max_tokens", "value": 8192}, "high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &RelayInfo{
				RelayFormat:     tt.relayFormat,
				ReasoningEffort: tt.initialEffort,
				ChannelMeta: &ChannelMeta{ParamOverride: map[string]interface{}{
					"operations": []interface{}{tt.operation},
				}},
			}
			_, err := ApplyParamOverrideWithRelayInfo([]byte(tt.input), info)
			require.NoError(t, err)
			require.Equal(t, tt.expected, info.ReasoningEffort)
		})
	}
}

// 推理参数属于影响计费与行为的敏感覆盖，改写它的操作必须进入日志审计。
func TestApplyParamOverrideWithRelayInfoAuditsReasoningOverrides(t *testing.T) {
	originalDebugEnabled := common2.DebugEnabled
	common2.DebugEnabled = false
	t.Cleanup(func() { common2.DebugEnabled = originalDebugEnabled })

	info := &RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		ChannelMeta: &ChannelMeta{ParamOverride: map[string]interface{}{
			"operations": []interface{}{
				map[string]interface{}{"mode": "set", "path": "reasoning.effort", "value": "max"},
			},
		}},
	}
	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"reasoning":{"effort":"high"}}`), info)
	require.NoError(t, err)
	require.Equal(t, []string{"set reasoning.effort = max"}, info.ParamOverrideAudit)
}
