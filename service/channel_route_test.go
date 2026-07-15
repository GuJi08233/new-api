package service

import (
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
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

func TestCollectRouteCandidatesForGroupDeduplicates(t *testing.T) {
	candidates := collectRouteCandidatesForGroup("", "model", []int{1, 1, 2})
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates for empty group")
	}
}

func TestCollectUnknownTokenChannelIDs(t *testing.T) {
	tiers := []operation_setting.RouteTier{
		{
			Conditions: []operation_setting.RouteTierCondition{{Var: "len", Op: "<", Value: 1000}},
			ChannelIDs: []int{1, 2},
		},
		{ChannelIDs: []int{3}},
	}

	if got := collectUnknownTokenChannelIDs(tiers, false); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("standard relay placeholder IDs = %v, want all tier IDs", got)
	}
	if got := collectUnknownTokenChannelIDs(tiers, true); !slices.Equal(got, []int{3}) {
		t.Fatalf("task fallback IDs = %v, want catch-all tier IDs", got)
	}

	withoutCatchAll := tiers[:1]
	if got := collectUnknownTokenChannelIDs(withoutCatchAll, true); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("task IDs without catch-all = %v, want union fallback", got)
	}
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
			got := evaluateRouteCondition(tt.cond, tt.tokens)
			if got != tt.expected {
				t.Fatalf("evaluateRouteCondition(%v, %d) = %v, want %v", tt.cond, tt.tokens, got, tt.expected)
			}
		})
	}
}

func TestEvaluateRouteTier_EmptyConditions(t *testing.T) {
	if !evaluateRouteTier(nil, 500) {
		t.Fatalf("empty conditions should match")
	}
	if !evaluateRouteTier([]operation_setting.RouteTierCondition{}, 500) {
		t.Fatalf("empty conditions should match")
	}
}

func TestEvaluateRouteTier_ANDLogic(t *testing.T) {
	conditions := []operation_setting.RouteTierCondition{
		{Var: "len", Op: ">=", Value: 100},
		{Var: "len", Op: "<", Value: 1000},
	}
	if !evaluateRouteTier(conditions, 500) {
		t.Fatalf("expected 500 to match 100 <= len < 1000")
	}
	if evaluateRouteTier(conditions, 50) {
		t.Fatalf("expected 50 to not match 100 <= len < 1000")
	}
	if evaluateRouteTier(conditions, 1000) {
		t.Fatalf("expected 1000 to not match 100 <= len < 1000")
	}
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
