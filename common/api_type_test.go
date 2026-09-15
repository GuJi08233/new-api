package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
)

// /v1/responses/compact 只能由原样转发 Responses 协议的渠道承接；
// 高级自定义渠道通过路由配置声明支持，不能再被硬白名单拒绝。
func TestIsResponsesCompactAPIType(t *testing.T) {
	cases := []struct {
		name    string
		apiType int
		want    bool
	}{
		{"OpenAI", constant.APITypeOpenAI, true},
		{"Codex", constant.APITypeCodex, true},
		{"Advanced Custom", constant.APITypeAdvancedCustom, true},
		{"Anthropic", constant.APITypeAnthropic, false},
		{"Gemini", constant.APITypeGemini, false},
		{"OpenRouter", constant.APITypeOpenRouter, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsResponsesCompactAPIType(tc.apiType))
		})
	}
}
