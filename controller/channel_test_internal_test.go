package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
		{Id: 4, Status: common.ChannelStatusArchived},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
		{Id: 4, Status: common.ChannelStatusArchived},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

// 开启缓存绕过后，每个测试端点的提示词都必须带上本次测试的随机串，否则上游网关对相同
// 请求体的缓存响应会把已失效的密钥测成可用；关闭时请求体必须保持原样。
func TestBuildTestRequestCacheBust(t *testing.T) {
	const nonce = "abc123xyz789"

	tests := []struct {
		name      string
		modelName string
		endpoint  string
		baseText  string
	}{
		{name: "openai", modelName: "gpt-4o-mini", endpoint: string(constant.EndpointTypeOpenAI), baseText: `"content":"hi"`},
		{name: "anthropic", modelName: "claude-sonnet-4", endpoint: string(constant.EndpointTypeAnthropic), baseText: `"content":"hi"`},
		{name: "gemini", modelName: "gemini-2.5-flash", endpoint: string(constant.EndpointTypeGemini), baseText: `"text":"hi"`},
		{name: "responses", modelName: "gpt-5", endpoint: string(constant.EndpointTypeOpenAIResponse), baseText: `"content":"hi"`},
		{name: "embeddings", modelName: "text-embedding-3-small", endpoint: string(constant.EndpointTypeEmbeddings), baseText: `"hello world"`},
		{name: "rerank", modelName: "jina-reranker-v2", endpoint: string(constant.EndpointTypeJinaRerank), baseText: `"What is Deep Learning?"`},
		{name: "image", modelName: "dall-e-3", endpoint: string(constant.EndpointTypeImageGeneration), baseText: `"a cute cat"`},
		{name: "auto detected chat", modelName: "gpt-4o-mini", baseText: `"content":"hi"`},
		{name: "auto detected embedding", modelName: "text-embedding-3-small", baseText: `"hello world"`},
		{name: "auto detected rerank", modelName: "bge-reranker-v2", baseText: `"What is Deep Learning?"`},
		{name: "auto detected codex", modelName: "gpt-5-codex", baseText: `"content":"hi"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			disabled, err := common.Marshal(buildTestRequest(tc.modelName, tc.endpoint, "", false))
			require.NoError(t, err)
			assert.Contains(t, string(disabled), tc.baseText)
			assert.NotContains(t, string(disabled), nonce)

			enabled, err := common.Marshal(buildTestRequest(tc.modelName, tc.endpoint, nonce, false))
			require.NoError(t, err)
			assert.NotContains(t, string(enabled), tc.baseText)
			assert.Contains(t, string(enabled), nonce)
		})
	}
}

// 全局缓存绕过开关对没有单独开启的渠道同样生效：手动测试、定时测试与自动恢复共用
// 同一个判定，两个开关任一开启就必须注入标记。
func TestChannelTestCacheBustEnabledHonoursBothSwitches(t *testing.T) {
	monitor := operation_setting.GetMonitorSetting()
	original := monitor.TestCacheBustEnabled
	t.Cleanup(func() { monitor.TestCacheBustEnabled = original })

	channelWithCacheBust := func(enabled bool) *model.Channel {
		channel := &model.Channel{Id: 1}
		channel.SetSetting(dto.ChannelSettings{TestCacheBustEnabled: enabled})
		return channel
	}

	monitor.TestCacheBustEnabled = false
	assert.False(t, channelTestCacheBustEnabled(channelWithCacheBust(false)))
	assert.True(t, channelTestCacheBustEnabled(channelWithCacheBust(true)))

	monitor.TestCacheBustEnabled = true
	assert.True(t, channelTestCacheBustEnabled(channelWithCacheBust(false)))
	assert.True(t, channelTestCacheBustEnabled(channelWithCacheBust(true)))
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}
