package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

// OA2 多合一渠道自动测试端点契约：显式端点原样保留；自动检测时按已配置的格式挑选，
// OpenAI 优先，只配置了 Gemini/Claude/Codex 的渠道分别落到对应原生端点，避免打到默认 OpenAI 地址。
func TestNormalizeChannelTestEndpointOA2(t *testing.T) {
	oa2 := func(otherSettings string) *model.Channel {
		return &model.Channel{Id: 1, Type: constant.ChannelTypeOA2, OtherSettings: otherSettings}
	}

	tests := []struct {
		name     string
		channel  *model.Channel
		endpoint string
		want     string
	}{
		{name: "explicit endpoint is preserved", channel: oa2(`{"oa2_gemini_enabled":true,"oa2_base_url_gemini":"https://generativelanguage.googleapis.com"}`), endpoint: "openai", want: "openai"},
		{name: "openai format wins when configured", channel: oa2(`{"oa2_openai_enabled":true,"oa2_base_url_openai":"https://generativelanguage.googleapis.com/v1beta/openai","oa2_gemini_enabled":true,"oa2_base_url_gemini":"https://generativelanguage.googleapis.com"}`), want: ""},
		{name: "gemini only channel tests through gemini endpoint", channel: oa2(`{"oa2_gemini_enabled":true,"oa2_base_url_gemini":"https://generativelanguage.googleapis.com"}`), want: string(constant.EndpointTypeGemini)},
		{name: "gemini enabled without url falls back to openai", channel: oa2(`{"oa2_gemini_enabled":true}`), want: ""},
		{name: "claude only channel tests through anthropic endpoint", channel: oa2(`{"oa2_claude_enabled":true,"oa2_base_url_claude":"https://api.anthropic.com"}`), want: string(constant.EndpointTypeAnthropic)},
		{name: "codex only channel tests through responses endpoint", channel: oa2(`{"oa2_codex_enabled":true,"oa2_base_url_codex":"https://chatgpt.com/backend-api"}`), want: string(constant.EndpointTypeOpenAIResponse)},
		{name: "legacy oa2 channel without other settings keeps auto detection", channel: oa2(""), want: ""},
		{name: "non oa2 channel is untouched", channel: &model.Channel{Id: 2, Type: constant.ChannelTypeGemini}, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeChannelTestEndpoint(tc.channel, "gemini-2.5-flash", tc.endpoint))
		})
	}
}
