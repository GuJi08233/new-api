package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChannelTestUsesNativeEndpointRequest(t *testing.T) {
	claude, ok := buildTestRequest("test", string(constant.EndpointTypeAnthropic), "", true).(*dto.ClaudeRequest)
	require.True(t, ok)
	assert.True(t, *claude.Stream)
	assert.Equal(t, "hi", claude.Messages[0].Content)
	gemini, ok := buildTestRequest("test", string(constant.EndpointTypeGemini), "", true).(*dto.GeminiChatRequest)
	require.True(t, ok)
	assert.Equal(t, "hi", gemini.Contents[0].Parts[0].Text)
	assert.Equal(t, uint(3000), *gemini.GenerationConfig.MaxOutputTokens)
}

// 缓存绕过标记必须在参数覆盖之后才落进请求体：覆盖能写任意 JSON 路径，把测试文本
// 改回固定内容，上游网关就会用缓存响应把已失效的密钥伪装成可用。
func TestInjectTestCacheBustReachesConvertedRequestText(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "openai chat string content",
			body: `{"model":"m","messages":[{"role":"user","content":"fixed"}]}`,
			want: `messages.0.content`,
		},
		{
			name: "claude structured content",
			body: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"fixed"}]}]}`,
			want: `messages.0.content.0.text`,
		},
		{
			name: "gemini parts",
			body: `{"contents":[{"role":"user","parts":[{"text":"fixed"}]}]}`,
			want: `contents.0.parts.0.text`,
		},
		{
			name: "responses input item",
			body: `{"model":"m","input":[{"role":"user","content":"fixed"}]}`,
			want: `input.0.content`,
		},
		{
			name: "embedding input array",
			body: `{"model":"m","input":["fixed"]}`,
			want: `input.0`,
		},
		{
			name: "image prompt",
			body: `{"model":"m","prompt":"fixed"}`,
			want: `prompt`,
		},
		{
			name: "rerank query",
			body: `{"model":"m","query":"fixed","documents":["a"]}`,
			want: `query`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			patched, ok := injectTestCacheBust([]byte(tc.body), "nonce123")
			require.True(t, ok)
			assert.Equal(t, "fixed (nonce123)", gjson.GetBytes(patched, tc.want).String())
		})
	}
}

func TestInjectTestCacheBustReportsMissingTextField(t *testing.T) {
	body := []byte(`{"model":"m","messages":[]}`)
	patched, ok := injectTestCacheBust(body, "nonce123")
	assert.False(t, ok)
	assert.Equal(t, body, patched)
}
