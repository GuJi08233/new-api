package service

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelRouteMatchAnyRegex(t *testing.T) {
	if !channelRouteMatchAnyRegex([]string{"^/v1/messages$"}, "/v1/messages") {
		t.Fatalf("expected path regex to match")
	}
	if channelRouteMatchAnyRegex([]string{"^/v1/messages$"}, "/v1/chat/completions") {
		t.Fatalf("expected path regex not to match")
	}
	if !channelRouteMatchAnyRegex([]string{"^Qwen3\\.5-35B-A3B$"}, "Qwen3.5-35B-A3B") {
		t.Fatalf("expected model regex to match")
	}
}

func TestChannelRouteMatchPathRegexAcceptsTrailingSlash(t *testing.T) {
	assert.True(t, channelRouteMatchPathRegex([]string{"^/v1/rerank$"}, "/v1/rerank/"))
	assert.True(t, channelRouteMatchPathRegex([]string{"^/v1/rerank/$"}, "/v1/rerank"))
}

func TestCollectRouteCandidatesForGroupDeduplicates(t *testing.T) {
	candidates := collectRouteCandidatesForGroup("", "model", []int{1, 1, 2})
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates for empty group")
	}
}

func TestCollectTierChannelIDsSkipsRejectTiers(t *testing.T) {
	tiers := []operation_setting.RouteTier{
		{
			Conditions: []operation_setting.RouteTierCondition{{Var: "len", Op: "<", Value: 1000}},
			ChannelIDs: []int{1, 2},
		},
		{ChannelIDs: []int{2, 3}},
		{Reject: true, ChannelIDs: []int{4}},
	}

	assert.Equal(t, []int{1, 2, 3}, collectTierChannelIDs(tiers, false))
	assert.Equal(t, []int{2, 3}, collectTierChannelIDs(tiers, true))
}

func TestGetChannelRouteMatchGroupPrefersUsingGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "vip")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")

	group := getChannelRouteMatchGroup(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "test-model",
		Retry:      common.GetPointer(0),
	})
	if group != "vip" {
		t.Fatalf("expected using group, got %q", group)
	}
}

func TestGetChannelRouteMatchGroupFallsBackToUserGroupWhenAuto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "auto")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "vip")

	group := getChannelRouteMatchGroup(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "auto",
		ModelName:  "test-model",
		Retry:      common.GetPointer(0),
	})
	if group != "vip" {
		t.Fatalf("expected user group fallback, got %q", group)
	}
}

func TestEvaluateRouteCondition(t *testing.T) {
	tests := []struct {
		name     string
		cond     operation_setting.RouteTierCondition
		tokens   int
		expected bool
	}{
		{"len < 1000 true", operation_setting.RouteTierCondition{Var: "len", Op: "<", Value: 1000}, 500, true},
		{"len < 1000 false", operation_setting.RouteTierCondition{Var: "len", Op: "<", Value: 1000}, 1000, false},
		{"len <= 1000 true", operation_setting.RouteTierCondition{Var: "len", Op: "<=", Value: 1000}, 1000, true},
		{"p > 500 true", operation_setting.RouteTierCondition{Var: "p", Op: ">", Value: 500}, 600, true},
		{"p >= 500 true", operation_setting.RouteTierCondition{Var: "p", Op: ">=", Value: 500}, 500, true},
		{"c always 0", operation_setting.RouteTierCondition{Var: "c", Op: "<", Value: 100}, 500, true},
		{"unknown var", operation_setting.RouteTierCondition{Var: "x", Op: "<", Value: 100}, 50, false},
		{"unknown op", operation_setting.RouteTierCondition{Var: "len", Op: "~", Value: 100}, 50, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateRouteCondition(tt.cond, tt.tokens, 0)
			if got != tt.expected {
				t.Fatalf("evaluateRouteCondition(%v, %d) = %v, want %v", tt.cond, tt.tokens, got, tt.expected)
			}
		})
	}
}

func TestEvaluateRouteTier_EmptyConditions(t *testing.T) {
	if !evaluateRouteTier(nil, 500, 0) {
		t.Fatalf("empty conditions should match")
	}
	if !evaluateRouteTier([]operation_setting.RouteTierCondition{}, 500, 0) {
		t.Fatalf("empty conditions should match")
	}
}

func TestEvaluateRouteTier_ANDLogic(t *testing.T) {
	conditions := []operation_setting.RouteTierCondition{
		{Var: "len", Op: ">=", Value: 100},
		{Var: "len", Op: "<", Value: 1000},
	}
	if !evaluateRouteTier(conditions, 500, 0) {
		t.Fatalf("expected 500 to match 100 <= len < 1000")
	}
	if evaluateRouteTier(conditions, 50, 0) {
		t.Fatalf("expected 50 to not match 100 <= len < 1000")
	}
	if evaluateRouteTier(conditions, 1000, 0) {
		t.Fatalf("expected 1000 to not match 100 <= len < 1000")
	}
}

func TestEvaluateRouteConditionSupportsDocumentCounts(t *testing.T) {
	tests := []struct {
		name      string
		condition operation_setting.RouteTierCondition
		docs      int
		want      bool
	}{
		{name: "small boundary", condition: operation_setting.RouteTierCondition{Var: "docs", Op: "<=", Value: 25}, docs: 25, want: true},
		{name: "small overflow", condition: operation_setting.RouteTierCondition{Var: "docs", Op: "<=", Value: 25}, docs: 26, want: false},
		{name: "large lower boundary", condition: operation_setting.RouteTierCondition{Var: "docs", Op: ">", Value: 25}, docs: 26, want: true},
		{name: "large upper boundary", condition: operation_setting.RouteTierCondition{Var: "docs", Op: "<=", Value: 200}, docs: 200, want: true},
		{name: "over limit", condition: operation_setting.RouteTierCondition{Var: "docs", Op: ">", Value: 200}, docs: 201, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, evaluateRouteCondition(tt.condition, 0, tt.docs))
		})
	}
}

func TestRouteTierConditionsKnownPreservesUnknownAndExplicitZero(t *testing.T) {
	docsCondition := []operation_setting.RouteTierCondition{{Var: "docs", Op: "<=", Value: 0}}
	assert.False(t, routeTierConditionsKnown(docsCondition, true, false))
	assert.True(t, routeTierConditionsKnown(docsCondition, false, true))
	assert.False(t, routeTierConditionsKnown([]operation_setting.RouteTierCondition{{Var: "len", Op: "<", Value: 10}}, false, true))
}

func TestGetChannelByRoute_TieredRouting_MatchesFirstTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyEstimatedTokens, 500)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	cfg := operation_setting.GetChannelRouteSetting()
	origEnabled := cfg.Enabled
	origRules := cfg.Rules
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{
		{
			Name:       "test-tiered",
			ModelRegex: []string{"^gpt-4o$"},
			ChannelIDs: []int{1, 2, 3, 4},
			RouteTiers: []operation_setting.RouteTier{
				{
					Label:      "short",
					Conditions: []operation_setting.RouteTierCondition{{Var: "len", Op: "<", Value: 1000}},
					ChannelIDs: []int{1, 2},
				},
				{
					Label:      "long",
					ChannelIDs: []int{3, 4},
				},
			},
		},
	}
	defer func() {
		cfg.Enabled = origEnabled
		cfg.Rules = origRules
	}()

	result, err := GetChannelByRoute(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-4o",
		Retry:      common.GetPointer(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Matched {
		t.Fatalf("expected rule to match")
	}
	logInfo, ok := ctx.Get(ginKeyChannelRouteLogInfo)
	if !ok {
		t.Fatalf("expected log info to be set")
	}
	info := logInfo.(gin.H)
	if info["matched_tier"] != "short" {
		t.Fatalf("expected matched_tier=short, got %v", info["matched_tier"])
	}
}

func TestGetChannelByRoute_TieredRouting_MatchesSecondTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyEstimatedTokens, 5000)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	cfg := operation_setting.GetChannelRouteSetting()
	origEnabled := cfg.Enabled
	origRules := cfg.Rules
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{
		{
			Name:       "test-tiered",
			ModelRegex: []string{"^gpt-4o$"},
			ChannelIDs: []int{1, 2, 3, 4},
			RouteTiers: []operation_setting.RouteTier{
				{
					Label:      "short",
					Conditions: []operation_setting.RouteTierCondition{{Var: "len", Op: "<", Value: 1000}},
					ChannelIDs: []int{1, 2},
				},
				{
					Label:      "long",
					ChannelIDs: []int{3, 4},
				},
			},
		},
	}
	defer func() {
		cfg.Enabled = origEnabled
		cfg.Rules = origRules
	}()

	result, err := GetChannelByRoute(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-4o",
		Retry:      common.GetPointer(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Matched {
		t.Fatalf("expected rule to match")
	}
	logInfo, ok := ctx.Get(ginKeyChannelRouteLogInfo)
	if !ok {
		t.Fatalf("expected log info to be set")
	}
	info := logInfo.(gin.H)
	if info["matched_tier"] != "long" {
		t.Fatalf("expected matched_tier=long, got %v", info["matched_tier"])
	}
}

func TestGetChannelByRoute_TieredRouting_FallbackToDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyEstimatedTokens, 500)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	cfg := operation_setting.GetChannelRouteSetting()
	origEnabled := cfg.Enabled
	origRules := cfg.Rules
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{
		{
			Name:       "test-no-tiers",
			ModelRegex: []string{"^gpt-4o$"},
			ChannelIDs: []int{1, 2},
		},
	}
	defer func() {
		cfg.Enabled = origEnabled
		cfg.Rules = origRules
	}()

	result, err := GetChannelByRoute(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-4o",
		Retry:      common.GetPointer(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Matched {
		t.Fatalf("expected rule to match")
	}
	logInfo, ok := ctx.Get(ginKeyChannelRouteLogInfo)
	if !ok {
		t.Fatalf("expected log info to be set")
	}
	info := logInfo.(gin.H)
	if _, exists := info["matched_tier"]; exists {
		t.Fatalf("expected no matched_tier when no tiers configured")
	}
}

func TestGetChannelByRoute_TieredRouting_MultiCondition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyEstimatedTokens, 5000)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	cfg := operation_setting.GetChannelRouteSetting()
	origEnabled := cfg.Enabled
	origRules := cfg.Rules
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{
		{
			Name:       "test-multi-cond",
			ModelRegex: []string{"^gpt-4o$"},
			ChannelIDs: []int{99},
			RouteTiers: []operation_setting.RouteTier{
				{
					Label: "mid",
					Conditions: []operation_setting.RouteTierCondition{
						{Var: "len", Op: ">=", Value: 1000},
						{Var: "len", Op: "<", Value: 10000},
					},
					ChannelIDs: []int{5, 6},
				},
				{
					Label:      "large",
					ChannelIDs: []int{7, 8},
				},
			},
		},
	}
	defer func() {
		cfg.Enabled = origEnabled
		cfg.Rules = origRules
	}()

	result, err := GetChannelByRoute(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-4o",
		Retry:      common.GetPointer(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Matched {
		t.Fatalf("expected rule to match")
	}
	logInfo, ok := ctx.Get(ginKeyChannelRouteLogInfo)
	if !ok {
		t.Fatalf("expected log info to be set")
	}
	info := logInfo.(gin.H)
	// 5000 matches 1000 <= len < 10000
	if info["matched_tier"] != "mid" {
		t.Fatalf("expected matched_tier=mid, got %v", info["matched_tier"])
	}
}

func TestGetChannelByRoute_TieredRouting_SkipsEmptyPoolTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyEstimatedTokens, 500)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	cfg := operation_setting.GetChannelRouteSetting()
	origEnabled := cfg.Enabled
	origRules := cfg.Rules
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{
		{
			Name:       "test-skip-empty",
			ModelRegex: []string{"^gpt-4o$"},
			ChannelIDs: []int{99},
			RouteTiers: []operation_setting.RouteTier{
				{
					Label:      "empty-pool",
					Conditions: []operation_setting.RouteTierCondition{{Var: "len", Op: "<", Value: 1000}},
					ChannelIDs: []int{}, // empty pool
				},
				{
					Label:      "has-pool",
					Conditions: []operation_setting.RouteTierCondition{{Var: "len", Op: "<", Value: 1000}},
					ChannelIDs: []int{3, 4},
				},
			},
		},
	}
	defer func() {
		cfg.Enabled = origEnabled
		cfg.Rules = origRules
	}()

	result, err := GetChannelByRoute(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-4o",
		Retry:      common.GetPointer(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Matched {
		t.Fatalf("expected rule to match")
	}
	logInfo, ok := ctx.Get(ginKeyChannelRouteLogInfo)
	if !ok {
		t.Fatalf("expected log info to be set")
	}
	info := logInfo.(gin.H)
	// First tier matches but has empty pool, should skip to second tier
	if info["matched_tier"] != "has-pool" {
		t.Fatalf("expected matched_tier=has-pool, got %v", info["matched_tier"])
	}
}

func TestGetChannelByRouteDocumentBatchBoundariesAndReject(t *testing.T) {
	cfg := operation_setting.GetChannelRouteSetting()
	originalEnabled := cfg.Enabled
	originalRules := cfg.Rules
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.Rules = originalRules
	})
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{{
		Name:       "rerank-docs",
		ModelRegex: []string{"^rerank-model$"},
		PathRegex:  []string{"^/v1/rerank$"},
		Strict:     true,
		RouteTiers: []operation_setting.RouteTier{
			{
				Label:      "small",
				Conditions: []operation_setting.RouteTierCondition{{Var: "docs", Op: "<=", Value: 25}},
				ChannelIDs: []int{1, 2},
			},
			{
				Label: "large",
				Conditions: []operation_setting.RouteTierCondition{
					{Var: "docs", Op: ">", Value: 25},
					{Var: "docs", Op: "<=", Value: 200},
				},
				ChannelIDs: []int{2},
			},
			{
				Label:         "over-limit",
				Conditions:    []operation_setting.RouteTierCondition{{Var: "docs", Op: ">", Value: 200}},
				Reject:        true,
				RejectMessage: "Candidate documents cannot exceed 200",
			},
		},
	}}

	tests := []struct {
		name       string
		docs       int
		wantTier   string
		wantPool   []int
		wantReject bool
	}{
		{name: "explicit zero is small", docs: 0, wantTier: "small", wantPool: []int{1, 2}},
		{name: "25 is small", docs: 25, wantTier: "small", wantPool: []int{1, 2}},
		{name: "26 is large", docs: 26, wantTier: "large", wantPool: []int{2}},
		{name: "200 is large", docs: 200, wantTier: "large", wantPool: []int{2}},
		{name: "201 is rejected", docs: 201, wantTier: "over-limit", wantReject: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/rerank/", nil)
			common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyEstimatedDocs, tt.docs)

			result, err := GetChannelByRoute(&RetryParam{
				Ctx:        ctx,
				TokenGroup: "default",
				ModelName:  "rerank-model",
				Retry:      common.GetPointer(0),
			})
			require.NoError(t, err)
			require.True(t, result.Matched)
			assert.Equal(t, tt.wantReject, result.Rejected)
			assert.False(t, result.Deferred)
			assert.False(t, result.NeedsReroute)
			if tt.wantReject {
				assert.Equal(t, "Candidate documents cannot exceed 200", result.RejectMessage)
			}

			logValue, found := ctx.Get(ginKeyChannelRouteLogInfo)
			require.True(t, found)
			logInfo, ok := logValue.(gin.H)
			require.True(t, ok)
			assert.Equal(t, tt.wantTier, logInfo["matched_tier"])
			assert.Equal(t, tt.docs, logInfo["estimated_docs"])
			assert.Equal(t, tt.wantPool, logInfo["channel_ids"])
		})
	}
}

func TestGetChannelByRouteEmbeddingBatchUsesDocumentTiers(t *testing.T) {
	cfg := operation_setting.GetChannelRouteSetting()
	originalEnabled := cfg.Enabled
	originalRules := cfg.Rules
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.Rules = originalRules
	})
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{{
		Name:       "embedding-docs",
		ModelRegex: []string{"^embedding-model$"},
		PathRegex:  []string{"^/v1/embeddings$"},
		Strict:     true,
		RouteTiers: []operation_setting.RouteTier{
			{
				Label:      "small",
				Conditions: []operation_setting.RouteTierCondition{{Var: "docs", Op: "<=", Value: 25}},
				ChannelIDs: []int{11, 12},
			},
			{
				Label:      "large",
				Conditions: []operation_setting.RouteTierCondition{{Var: "docs", Op: ">", Value: 25}},
				ChannelIDs: []int{12},
			},
		},
	}}

	tests := []struct {
		name     string
		docs     int
		wantTier string
		wantPool []int
	}{
		{name: "25 inputs use both eligible channels", docs: 25, wantTier: "small", wantPool: []int{11, 12}},
		{name: "26 inputs use only full batch channel", docs: 26, wantTier: "large", wantPool: []int{12}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/embeddings/", nil)
			common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyEstimatedDocs, tt.docs)

			result, err := GetChannelByRoute(&RetryParam{
				Ctx:        ctx,
				TokenGroup: "default",
				ModelName:  "embedding-model",
				Retry:      common.GetPointer(0),
			})
			require.NoError(t, err)
			require.True(t, result.Matched)
			assert.True(t, result.Strict)
			assert.False(t, result.Rejected)

			logValue, found := ctx.Get(ginKeyChannelRouteLogInfo)
			require.True(t, found)
			logInfo, ok := logValue.(gin.H)
			require.True(t, ok)
			assert.Equal(t, tt.wantTier, logInfo["matched_tier"])
			assert.Equal(t, tt.wantPool, logInfo["channel_ids"])
		})
	}
}

func seedDocumentRouteChannels(t *testing.T, modelNames []string, channelIDs []int) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
	require.NoError(t, model.DB.Where("channel_id IN ?", channelIDs).Delete(&model.Ability{}).Error)
	require.NoError(t, model.DB.Where("id IN ?", channelIDs).Delete(&model.Channel{}).Error)

	priorities := []int64{100, 0}
	for index, channelID := range channelIDs {
		channel := &model.Channel{
			Id:       channelID,
			Type:     constant.ChannelTypeOpenAI,
			Key:      "route-test-key",
			Status:   common.ChannelStatusEnabled,
			Name:     "document-route-test",
			Group:    "default",
			Models:   strings.Join(modelNames, ","),
			Priority: common.GetPointer(priorities[index]),
			Weight:   common.GetPointer(uint(100)),
		}
		require.NoError(t, model.DB.Create(channel).Error)
		for _, modelName := range modelNames {
			require.NoError(t, model.DB.Create(&model.Ability{
				Group:     "default",
				Model:     modelName,
				ChannelId: channelID,
				Enabled:   true,
			}).Error)
		}
	}

	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id IN ?", channelIDs).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Where("id IN ?", channelIDs).Delete(&model.Channel{}).Error)
	})
}

func TestGetChannelByRouteDocumentTiersSelectOnlyEligibleChannelsAcrossRetries(t *testing.T) {
	const (
		smallOnlyChannel = 910001
		fullBatchChannel = 910002
	)
	modelNames := []string{"rerank-route-test", "embedding-route-test"}
	channelIDs := []int{smallOnlyChannel, fullBatchChannel}

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		model.InitChannelCache()
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})
	seedDocumentRouteChannels(t, modelNames, channelIDs)
	model.InitChannelCache()

	cfg := operation_setting.GetChannelRouteSetting()
	originalEnabled := cfg.Enabled
	originalRules := cfg.Rules
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.Rules = originalRules
	})
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{
		{
			Name:       "rerank-document-capacity",
			ModelRegex: []string{"^rerank-route-test$"},
			PathRegex:  []string{"^/v1/rerank$"},
			Strict:     true,
			RouteTiers: documentCapacityRouteTiers(channelIDs),
		},
		{
			Name:       "embedding-document-capacity",
			ModelRegex: []string{"^embedding-route-test$"},
			PathRegex:  []string{"^/v1/embeddings$"},
			Strict:     true,
			RouteTiers: documentCapacityRouteTiers(channelIDs),
		},
	}

	tests := []struct {
		name          string
		path          string
		modelName     string
		docs          int
		retry         int
		wantChannelID int
		wantRejected  bool
	}{
		{name: "rerank small batch first priority", path: "/v1/rerank", modelName: "rerank-route-test", docs: 25, retry: 0, wantChannelID: smallOnlyChannel},
		{name: "rerank small batch retry reaches second eligible channel", path: "/v1/rerank", modelName: "rerank-route-test", docs: 25, retry: 1, wantChannelID: fullBatchChannel},
		{name: "rerank large batch excludes small-only channel", path: "/v1/rerank", modelName: "rerank-route-test", docs: 26, retry: 0, wantChannelID: fullBatchChannel},
		{name: "rerank large batch retry cannot escape tier", path: "/v1/rerank", modelName: "rerank-route-test", docs: 200, retry: 99, wantChannelID: fullBatchChannel},
		{name: "rerank over limit rejects before channel selection", path: "/v1/rerank", modelName: "rerank-route-test", docs: 201, retry: 0, wantRejected: true},
		{name: "embedding small batch uses small pool", path: "/v1/embeddings", modelName: "embedding-route-test", docs: 25, retry: 0, wantChannelID: smallOnlyChannel},
		{name: "embedding large batch uses full-capacity channel", path: "/v1/embeddings", modelName: "embedding-route-test", docs: 26, retry: 0, wantChannelID: fullBatchChannel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", tt.path, nil)
			common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyEstimatedDocs, tt.docs)

			result, err := GetChannelByRoute(&RetryParam{
				Ctx:        ctx,
				TokenGroup: "default",
				ModelName:  tt.modelName,
				Retry:      common.GetPointer(tt.retry),
			})
			require.NoError(t, err)
			require.True(t, result.Matched)
			assert.Equal(t, tt.wantRejected, result.Rejected)
			if tt.wantRejected {
				assert.Nil(t, result.Channel)
				assert.Equal(t, "Candidate documents cannot exceed 200", result.RejectMessage)
				return
			}
			require.NotNil(t, result.Channel)
			assert.Equal(t, tt.wantChannelID, result.Channel.Id)
		})
	}
}

func documentCapacityRouteTiers(channelIDs []int) []operation_setting.RouteTier {
	return []operation_setting.RouteTier{
		{
			Label:      "small",
			Conditions: []operation_setting.RouteTierCondition{{Var: "docs", Op: "<=", Value: 25}},
			ChannelIDs: channelIDs,
		},
		{
			Label: "large",
			Conditions: []operation_setting.RouteTierCondition{
				{Var: "docs", Op: ">", Value: 25},
				{Var: "docs", Op: "<=", Value: 200},
			},
			ChannelIDs: []int{channelIDs[1]},
		},
		{
			Label:         "over-limit",
			Conditions:    []operation_setting.RouteTierCondition{{Var: "docs", Op: ">", Value: 200}},
			Reject:        true,
			RejectMessage: "Candidate documents cannot exceed 200",
		},
	}
}

func TestGetChannelByRouteCanRejectEveryBatchAboveTwentyFive(t *testing.T) {
	cfg := operation_setting.GetChannelRouteSetting()
	originalEnabled := cfg.Enabled
	originalRules := cfg.Rules
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.Rules = originalRules
	})
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{{
		Name:       "small-only-rerank",
		ModelRegex: []string{"^rerank-model$"},
		PathRegex:  []string{"^/v1/rerank$"},
		Strict:     true,
		RouteTiers: []operation_setting.RouteTier{
			{
				Label:      "small",
				Conditions: []operation_setting.RouteTierCondition{{Var: "docs", Op: "<=", Value: 25}},
				ChannelIDs: []int{1, 2},
			},
			{
				Label:         "reject-large",
				Conditions:    []operation_setting.RouteTierCondition{{Var: "docs", Op: ">", Value: 25}},
				Reject:        true,
				RejectMessage: "This rerank endpoint accepts at most 25 documents",
			},
		},
	}}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/rerank", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyEstimatedDocs, 26)

	result, err := GetChannelByRoute(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "rerank-model",
		Retry:      common.GetPointer(0),
	})
	require.NoError(t, err)
	require.True(t, result.Matched)
	assert.True(t, result.Rejected)
	assert.Equal(t, "This rerank endpoint accepts at most 25 documents", result.RejectMessage)
}

func TestGetChannelByRouteDefersConditionalRejectUntilMetricKnown(t *testing.T) {
	cfg := operation_setting.GetChannelRouteSetting()
	originalEnabled := cfg.Enabled
	originalRules := cfg.Rules
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.Rules = originalRules
	})
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{{
		Name:       "docs-limit",
		ModelRegex: []string{"^rerank-model$"},
		Strict:     true,
		RouteTiers: []operation_setting.RouteTier{{
			Conditions: []operation_setting.RouteTierCondition{{Var: "docs", Op: ">", Value: 200}},
			Reject:     true,
		}},
	}}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/rerank", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	result, err := GetChannelByRoute(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "rerank-model",
		Retry:      common.GetPointer(0),
	})
	require.NoError(t, err)
	assert.True(t, result.Matched)
	assert.False(t, result.Rejected)
	assert.True(t, result.Deferred)
	assert.True(t, result.NeedsReroute)
	assert.True(t, result.Tiered)
}

func TestGetChannelByRouteCatchAllRejectWaitsForEarlierUnknownTier(t *testing.T) {
	cfg := operation_setting.GetChannelRouteSetting()
	originalEnabled := cfg.Enabled
	originalRules := cfg.Rules
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.Rules = originalRules
	})
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{{
		Name:       "mixed-metrics",
		ModelRegex: []string{"^model$"},
		Strict:     true,
		RouteTiers: []operation_setting.RouteTier{
			{
				Conditions: []operation_setting.RouteTierCondition{{Var: "len", Op: "<", Value: 100}},
				ChannelIDs: []int{1},
			},
			{Reject: true, RejectMessage: "request too large"},
		},
	}}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	result, err := GetChannelByRoute(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "model",
		Retry:      common.GetPointer(0),
	})
	require.NoError(t, err)
	assert.False(t, result.Rejected)
	assert.True(t, result.Deferred)
	assert.True(t, result.NeedsReroute)
}
