package common

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RelayInfo 创建时按各处理器写日志的口径记录客户端请求里的推理参数。
func TestGenRelayInfoCapturesRequestReasoningEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	budget := 0
	tests := []struct {
		name        string
		path        string
		relayFormat types.RelayFormat
		request     dto.Request
		expected    string
	}{
		{"OpenAI chat top-level effort", "/v1/chat/completions", types.RelayFormatOpenAI, &dto.GeneralOpenAIRequest{Model: "gpt-5", ReasoningEffort: " high "}, "high"},
		{"OpenRouter nested chat effort", "/v1/chat/completions", types.RelayFormatOpenAI, &dto.GeneralOpenAIRequest{Model: "anthropic/claude", Reasoning: json.RawMessage(`{"effort":"xhigh"}`)}, "xhigh"},
		{"non-string nested effort is ignored", "/v1/chat/completions", types.RelayFormatOpenAI, &dto.GeneralOpenAIRequest{Model: "anthropic/claude", Reasoning: json.RawMessage(`{"effort":42}`)}, ""},
		{"OpenAI Responses effort", "/v1/responses", types.RelayFormatOpenAIResponses, &dto.OpenAIResponsesRequest{Model: "gpt-5", Reasoning: &dto.Reasoning{Effort: "max"}}, "max"},
		{"Claude output config effort", "/v1/messages", types.RelayFormatClaude, &dto.ClaudeRequest{Model: "claude-opus-4-7", OutputConfig: json.RawMessage(`{"effort":"medium"}`)}, "medium"},
		{"Claude thinking budget", "/v1/messages", types.RelayFormatClaude, &dto.ClaudeRequest{Model: "claude-opus-4-7", Thinking: &dto.Thinking{Type: "enabled", BudgetTokens: common.GetPointer(1024)}}, "enabled (budget 1024)"},
		{"Gemini thinking level", "/v1beta/models/gemini-3-pro:generateContent", types.RelayFormatGemini, &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "low"}}}, "low"},
		{"Gemini disabled thinking budget", "/v1beta/models/gemini-3-pro:generateContent", types.RelayFormatGemini, &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingBudget: &budget}}}, "disabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", tt.path, nil)
			info, err := GenRelayInfo(ctx, tt.relayFormat, tt.request, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, info.ReasoningEffort)
		})
	}
}

// 换渠道重试时，上一次尝试留下的推理参数（模型后缀或参数覆盖的结果）不能带进下一次尝试的日志。
func TestInitChannelMetaRestoresRequestReasoningEffortForRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	info, err := GenRelayInfo(ctx, types.RelayFormatOpenAIResponses, &dto.OpenAIResponsesRequest{Model: "gpt-5", Reasoning: &dto.Reasoning{Effort: "max"}}, nil)
	require.NoError(t, err)

	info.ReasoningEffort = "high"
	info.InitChannelMeta(ctx)
	assert.Equal(t, "max", info.ReasoningEffort)

	info.ReasoningEffort = ""
	info.InitChannelMeta(ctx)
	assert.Equal(t, "max", info.ReasoningEffort)
}
