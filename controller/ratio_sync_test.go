package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDifferencesFiltersStrictlyToRequestedModels(t *testing.T) {
	localData := map[string]any{
		"model_ratio": map[string]float64{
			"archived-priced-model": 1,
		},
	}
	upstreamData := map[string]any{
		"model_ratio": map[string]float64{
			"enabled-model":         2,
			"hidden-model":          3,
			"archived-priced-model": 4,
		},
	}

	tests := []struct {
		name           string
		modelFilter    map[string]struct{}
		includedModels []string
		excludedModels []string
	}{
		{
			name:           "filter disabled",
			modelFilter:    nil,
			includedModels: []string{"enabled-model", "hidden-model", "archived-priced-model"},
		},
		{
			name:           "only requested models retained",
			modelFilter:    map[string]struct{}{"enabled-model": {}},
			includedModels: []string{"enabled-model"},
			excludedModels: []string{"hidden-model", "archived-priced-model"},
		},
		{
			name:           "empty filter excludes every model",
			modelFilter:    map[string]struct{}{},
			excludedModels: []string{"enabled-model", "hidden-model", "archived-priced-model"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			differences := buildDifferences(localData, upstreamData, test.modelFilter)

			for _, modelName := range test.includedModels {
				assert.Contains(t, differences, modelName)
			}
			for _, modelName := range test.excludedModels {
				assert.NotContains(t, differences, modelName)
			}
		})
	}
}

func TestBuildDifferencesOnlyReportsUpstreamValuesThatDiffer(t *testing.T) {
	localData := map[string]any{
		"model_ratio": map[string]float64{
			"same-model":       1.5,
			"changed-model":    1.5,
			"local-only-model": 1.5,
		},
		"completion_ratio": map[string]float64{
			"changed-model": 4,
		},
	}
	upstreamData := map[string]any{
		"model_ratio": map[string]float64{
			"same-model":          1.5,
			"changed-model":       2.5,
			"upstream-only-model": 3,
		},
		"completion_ratio": map[string]float64{
			"changed-model": 4,
		},
	}

	differences := buildDifferences(localData, upstreamData, nil)

	// 上游与本地一致，或上游根本没有该模型，都不应产生可同步项
	assert.NotContains(t, differences, "same-model")
	assert.NotContains(t, differences, "local-only-model")

	changed := differences["changed-model"]
	require.Contains(t, changed, "model_ratio")
	assert.Equal(t, 1.5, changed["model_ratio"].Current)
	assert.Equal(t, 2.5, changed["model_ratio"].Upstream)
	// completion_ratio 两侧相同，不应出现
	assert.NotContains(t, changed, "completion_ratio")

	upstreamOnly := differences["upstream-only-model"]
	require.Contains(t, upstreamOnly, "model_ratio")
	assert.Nil(t, upstreamOnly["model_ratio"].Current)
	assert.Equal(t, float64(3), upstreamOnly["model_ratio"].Upstream)
}

func TestConvertModelsDevToRatioDataPicksCheapestNonZeroProvider(t *testing.T) {
	payload := `{
		"zeta-reseller": {"models": {"shared-model": {"cost": {"input": 5, "output": 15, "cache_read": 0.5}}}},
		"alpha-reseller": {"models": {"shared-model": {"cost": {"input": 2, "output": 8, "cache_read": 0.2}}}},
		"free-tier": {"models": {"free-model": {"cost": {"input": 0, "output": 0}}}},
		"no-price": {"models": {"unpriced-model": {"cost": {"output": 3}}}}
	}`

	pricing, err := convertModelsDevToRatioData(strings.NewReader(payload), newModelsDevSelector(dto.UpstreamSourceModeAuto, nil))
	require.NoError(t, err)
	converted, sources := pricing.Data, pricing.Sources

	modelRatio, ok := converted["model_ratio"].(map[string]any)
	require.True(t, ok)
	// models.dev 的 input 是 $/1M tokens，本地倍率 = input / 2
	assert.Equal(t, 1.0, modelRatio["shared-model"])
	assert.Equal(t, 0.0, modelRatio["free-model"])
	assert.NotContains(t, modelRatio, "unpriced-model")

	completionRatio, ok := converted["completion_ratio"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 4.0, completionRatio["shared-model"])

	cacheRatio, ok := converted["cache_ratio"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0.1, cacheRatio["shared-model"])

	// 全是转售商时如实标记，避免把转售价当官方价用
	assert.Equal(t, "alpha-reseller", sources["shared-model"].Provider)
	assert.False(t, sources["shared-model"].Official)
}

func TestConvertModelsDevToRatioDataPrefersOfficialPricingOverCheaperResellers(t *testing.T) {
	payload := `{
		"cheap-reseller": {"models": {"gpt-x": {"cost": {"input": 2, "output": 8}}}},
		"openai": {"models": {"gpt-x": {"cost": {"input": 10, "output": 50}}}},
		"azure": {"models": {"gpt-x": {"cost": {"input": 11, "output": 55}}, "cloud-only": {"cost": {"input": 4, "output": 12}}}},
		"another-reseller": {"models": {"cloud-only": {"cost": {"input": 1, "output": 3}}}}
	}`

	pricing, err := convertModelsDevToRatioData(strings.NewReader(payload), newModelsDevSelector(dto.UpstreamSourceModeAuto, nil))
	require.NoError(t, err)
	converted, sources := pricing.Data, pricing.Sources

	modelRatio, ok := converted["model_ratio"].(map[string]any)
	require.True(t, ok)

	// 厂商自营优先于更便宜的转售商，也优先于官方云托管
	assert.Equal(t, 5.0, modelRatio["gpt-x"])
	assert.Equal(t, "openai", sources["gpt-x"].Provider)
	assert.True(t, sources["gpt-x"].Official)

	// 没有自营条目时退到官方云托管，而不是最便宜的转售商
	assert.Equal(t, 2.0, modelRatio["cloud-only"])
	assert.Equal(t, "azure", sources["cloud-only"].Provider)
	assert.True(t, sources["cloud-only"].Official)
}

func TestConvertModelsDevToRatioDataPrefersModelVendorOverOtherVendorsHosting(t *testing.T) {
	// alibaba-cn 托管 DeepSeek 的报价比 DeepSeek 原厂低，但原厂价才是官方定价
	payload := `{
		"alibaba-cn": {"models": {"deepseek-v4-flash": {"cost": {"input": 0.14, "output": 0.28}}}},
		"deepseek": {"models": {"deepseek-v4-flash": {"cost": {"input": 0.15, "output": 0.6}}}}
	}`

	pricing, err := convertModelsDevToRatioData(strings.NewReader(payload), newModelsDevSelector(dto.UpstreamSourceModeAuto, nil))
	require.NoError(t, err)
	converted, sources := pricing.Data, pricing.Sources

	modelRatio, ok := converted["model_ratio"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0.075, modelRatio["deepseek-v4-flash"])
	assert.Equal(t, "deepseek", sources["deepseek-v4-flash"].Provider)

	completionRatio, ok := converted["completion_ratio"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 4.0, completionRatio["deepseek-v4-flash"])
}

func TestConvertModelsDevToRatioDataMapsCacheWriteAndAudioCosts(t *testing.T) {
	payload := `{
		"openai": {"models": {"gpt-x-audio": {"cost": {
			"input": 10, "output": 50, "cache_read": 1, "cache_write": 12.5,
			"input_audio": 40, "output_audio": 80
		}}}}
	}`

	pricing, err := convertModelsDevToRatioData(
		strings.NewReader(payload),
		newModelsDevSelector(dto.UpstreamSourceModeAuto, nil),
	)
	require.NoError(t, err)

	ratioOf := func(field string) any {
		values, ok := pricing.Data[field].(map[string]any)
		require.True(t, ok, "missing field %s", field)
		return values["gpt-x-audio"]
	}

	assert.Equal(t, 5.0, ratioOf("model_ratio"))
	assert.Equal(t, 5.0, ratioOf("completion_ratio"))
	assert.Equal(t, 0.1, ratioOf("cache_ratio"))
	// 缓存创建价相对输入价
	assert.Equal(t, 1.25, ratioOf("create_cache_ratio"))
	// 音频输入相对文本输入价，音频输出相对音频输入价
	assert.Equal(t, 4.0, ratioOf("audio_ratio"))
	assert.Equal(t, 2.0, ratioOf("audio_completion_ratio"))

	candidate := pricing.Candidates["gpt-x-audio"][0]
	require.NotNil(t, candidate.CreateCacheRatio)
	assert.Equal(t, 1.25, *candidate.CreateCacheRatio)
	require.NotNil(t, candidate.AudioCompletionRatio)
	assert.Equal(t, 2.0, *candidate.AudioCompletionRatio)
}

func TestConvertModelsDevToRatioDataBuildsTieredExpressionFromContextTiers(t *testing.T) {
	payload := `{
		"openai": {"models": {"gpt-x-tiered": {
			"limit": {"context": 1050000},
			"cost": {
				"input": 10, "output": 50, "cache_read": 1, "cache_write": 12.5,
				"tiers": [
					{"input": 30, "output": 90, "tier": {"type": "context", "size": 500000}},
					{"input": 20, "output": 75, "cache_read": 2, "cache_write": 25, "tier": {"type": "context", "size": 272000}},
					{"input": 99, "output": 99, "tier": {"type": "output", "size": 1000}}
				]
			}
		}}}
	}`

	pricing, err := convertModelsDevToRatioData(
		strings.NewReader(payload),
		newModelsDevSelector(dto.UpstreamSourceModeAuto, nil),
	)
	require.NoError(t, err)

	modes, ok := pricing.Data[billing_setting.BillingModeField].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, billing_setting.BillingModeTieredExpr, modes["gpt-x-tiered"])

	exprs, ok := pricing.Data[billing_setting.BillingExprField].(map[string]any)
	require.True(t, ok)
	expr, ok := exprs["gpt-x-tiered"].(string)
	require.True(t, ok)

	// 档位按上下文阈值升序展开，非 context 档位被忽略，末档用上下文上限命名
	assert.Equal(t,
		`len <= 272000 ? tier("272K", p * 10 + c * 50 + cr * 1 + cc * 12.5) : `+
			`len <= 500000 ? tier("500K", p * 20 + c * 75 + cr * 2 + cc * 25) : `+
			`tier("1M", p * 30 + c * 90)`,
		expr)

	// 生成的表达式必须能被计费引擎编译，否则保存时会被拒绝
	require.NoError(t, billing_setting.SmokeTestExpr(expr))
	assert.Equal(t, expr, pricing.Candidates["gpt-x-tiered"][0].BillingExpr)
}

func TestFormatContextTierNameMatchesAdvertisedContextSizes(t *testing.T) {
	tests := map[float64]string{
		32768:   "32K",  // 二进制整除
		128000:  "128K", // 十进制整除优先，不能写成 125K
		131072:  "128K", // 十进制除不尽时才按 1024 折算
		272000:  "272K",
		1000000: "1M",
		1048576: "1M",
		1050000: "1M",   // gpt-6-astra 的上限，5% 容差内取整
		1047576: "1M",   // gpt-4.1 的上限
		1500000: "1.5M", // 超出容差时保留一位小数
		991000:  "991K",
	}
	for tokens, expected := range tests {
		assert.Equal(t, expected, formatContextTierName(tokens), "tokens=%v", tokens)
	}
}

func TestBuildTieredExpressionFallsBackWhenContextLimitCannotNameFinalTier(t *testing.T) {
	// 上游偶尔给出不大于档位阈值的上下文上限，或上限与阈值折算后同名，
	// 这时末档必须退回 "200K+"，否则表达式里会出现两个同名档位
	tests := map[string]string{
		"limit equals threshold": `{"limit": {"context": 200000}, "cost": {"input": 3, "output": 15,
			"tiers": [{"input": 6, "output": 22.5, "tier": {"type": "context", "size": 200000}}]}}`,
		"limit below threshold": `{"limit": {"context": 20000}, "cost": {"input": 3, "output": 15,
			"tiers": [{"input": 6, "output": 22.5, "tier": {"type": "context", "size": 200000}}]}}`,
		"limit missing": `{"cost": {"input": 3, "output": 15,
			"tiers": [{"input": 6, "output": 22.5, "tier": {"type": "context", "size": 200000}}]}}`,
	}

	for name, modelJSON := range tests {
		t.Run(name, func(t *testing.T) {
			pricing, err := convertModelsDevToRatioData(
				strings.NewReader(`{"anthropic": {"models": {"claude-x": `+modelJSON+`}}}`),
				newModelsDevSelector(dto.UpstreamSourceModeAuto, nil),
			)
			require.NoError(t, err)

			exprs, ok := pricing.Data[billing_setting.BillingExprField].(map[string]any)
			require.True(t, ok)
			assert.Equal(t,
				`len <= 200000 ? tier("200K", p * 3 + c * 15) : tier("200K+", p * 6 + c * 22.5)`,
				exprs["claude-x"])
		})
	}
}

func TestIrreducibleBillingExprsKeepsOnlyLocallyUnreproducibleShapes(t *testing.T) {
	overrides := irreducibleBillingExprs(map[string]string{
		"flat":     `tier("standard", p * 1 + c * 3)`,
		"tiered":   `len <= 272000 ? tier("272K", p * 10 + c * 50) : tier("1M", p * 20 + c * 75)`,
		"cache-1h": `tier("standard", p * 5 + cc1h * 10 + c * 25)`,
		"thinking": `param("enable_thinking") == true ? tier("thinking", p * 0.4 + c * 4) : tier("standard", p * 0.4 + c * 1.2)`,
		"off-peak": `weekday("UTC") >= 1 ? tier("peak", p * 0.3 + c * 1.2) : tier("off_peak", p * 0.15 + c * 0.6)`,
	})

	// 平价和上下文阶梯本地都能从 cost 推出来，交给本地转换才能统一档位命名；
	// 1 小时缓存写、思考模式、错峰这三类 cost 字段里没有，只能用上游成品
	assert.ElementsMatch(t, []string{"cache-1h", "thinking", "off-peak"}, lo.Keys(overrides))
}

func TestSelectUpstreamPricingPrefersUpstreamExprOverLocallyDerivedOne(t *testing.T) {
	candidates, err := parseModelsDevCandidates(strings.NewReader(`{
		"anthropic": {"models": {"claude-x": {"limit": {"context": 200000},
			"cost": {"input": 5, "output": 25, "cache_read": 0.5, "cache_write": 6.25}}}},
		"deepseek": {"models": {"deepseek-x": {"cost": {"input": 0.15, "output": 0.6}}}}
	}`))
	require.NoError(t, err)

	upstreamExpr := `tier("standard", p * 5 + cr * 0.5 + cc * 6.25 + cc1h * 10 + c * 25)`
	pricing := selectUpstreamPricing(
		&upstreamCatalog{candidates: candidates, exprOverrides: map[string]string{"claude-x": upstreamExpr}},
		newModelsDevSelector(dto.UpstreamSourceModeAuto, nil),
	)

	exprs, ok := pricing.Data[billing_setting.BillingExprField].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, upstreamExpr, exprs["claude-x"])
	// 既没有上游表达式也没有阶梯价的模型不应被切到表达式计费
	assert.NotContains(t, exprs, "deepseek-x")

	// 倍率照常下发，用户仍可把该模型改回按量计费
	modelRatio, ok := pricing.Data["model_ratio"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 2.5, modelRatio["claude-x"])
}

func TestSelectUpstreamPricingKeepsRatioAndExprFromSameProvider(t *testing.T) {
	// 阿里国内站报价低于国际站，但上游成品表达式是按国际站价写的。若倍率取国内站、
	// 表达式取上游，同一个模型就会出现输入价 0.115 配 p * 0.4 的矛盾配置。
	candidates, err := parseModelsDevCandidates(strings.NewReader(`{
		"alibaba-cn": {"models": {"qwen-x": {"cost": {"input": 0.115, "output": 0.345}}}},
		"alibaba": {"models": {"qwen-x": {"cost": {"input": 0.4, "output": 1.2}}}}
	}`))
	require.NoError(t, err)

	upstreamExpr := `param("enable_thinking") == true ? tier("thinking", p * 0.4 + c * 4) : tier("standard", p * 0.4 + c * 1.2)`
	catalog := &upstreamCatalog{
		candidates:    candidates,
		exprOverrides: map[string]string{"qwen-x": upstreamExpr},
	}

	t.Run("auto switches to the source the expression was priced from", func(t *testing.T) {
		pricing := selectUpstreamPricing(catalog, newModelsDevSelector(dto.UpstreamSourceModeAuto, nil))

		assert.Equal(t, "alibaba", pricing.Sources["qwen-x"].Provider)
		modelRatio, ok := pricing.Data["model_ratio"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, 0.2, modelRatio["qwen-x"])
		exprs, ok := pricing.Data[billing_setting.BillingExprField].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, upstreamExpr, exprs["qwen-x"])
	})

	t.Run("preferred provider wins and the upstream expression is dropped", func(t *testing.T) {
		pricing := selectUpstreamPricing(catalog,
			newModelsDevSelector(dto.UpstreamSourceModePrefer, []string{"alibaba-cn"}))

		assert.Equal(t, "alibaba-cn", pricing.Sources["qwen-x"].Provider)
		modelRatio, ok := pricing.Data["model_ratio"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, 0.0575, modelRatio["qwen-x"])
		// 该来源既没有阶梯价也用不了上游表达式，不应被切到表达式计费
		exprs, _ := pricing.Data[billing_setting.BillingExprField].(map[string]any)
		assert.NotContains(t, exprs, "qwen-x")
	})
}

const multiSourcePayload = `{
	"cheap-reseller": {"models": {"gpt-x": {"cost": {"input": 2, "output": 8}}, "reseller-only": {"cost": {"input": 1, "output": 3}}}},
	"openai": {"models": {"gpt-x": {"cost": {"input": 10, "output": 50}}}},
	"azure": {"models": {"gpt-x": {"cost": {"input": 11, "output": 55}}}}
}`

func TestConvertModelsDevToRatioDataOfficialOnlyDropsModelsWithoutOfficialSource(t *testing.T) {
	pricing, err := convertModelsDevToRatioData(
		strings.NewReader(multiSourcePayload),
		newModelsDevSelector(dto.UpstreamSourceModeOfficialOnly, nil),
	)
	require.NoError(t, err)

	modelRatio, ok := pricing.Data["model_ratio"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 5.0, modelRatio["gpt-x"])
	// 只有转售商报价的模型不参与同步，而不是退而求其次用转售价
	assert.NotContains(t, modelRatio, "reseller-only")
	assert.NotContains(t, pricing.Sources, "reseller-only")

	// 候选仍然下发，供用户手动切换到其它来源
	require.Len(t, pricing.Candidates["gpt-x"], 3)
	assert.Equal(t, "openai", pricing.Candidates["gpt-x"][0].Provider)
}

func TestConvertModelsDevToRatioDataPreferredProvidersWinOverOfficial(t *testing.T) {
	pricing, err := convertModelsDevToRatioData(
		strings.NewReader(multiSourcePayload),
		newModelsDevSelector(dto.UpstreamSourceModePrefer, []string{"azure", "cheap-reseller"}),
	)
	require.NoError(t, err)

	modelRatio, ok := pricing.Data["model_ratio"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 5.5, modelRatio["gpt-x"])
	assert.Equal(t, "azure", pricing.Sources["gpt-x"].Provider)
	// 指定的提供商按给定顺序排在前面，未指定的回落到官方优先
	candidates := pricing.Candidates["gpt-x"]
	require.Len(t, candidates, 3)
	assert.Equal(t, []string{"azure", "cheap-reseller", "openai"},
		[]string{candidates[0].Provider, candidates[1].Provider, candidates[2].Provider})

	// 首选提供商没有的模型仍按官方优先规则给出来源
	assert.Equal(t, "cheap-reseller", pricing.Sources["reseller-only"].Provider)
}

func TestConvertModelsDevToRatioDataReportsCandidateRatiosAndProviders(t *testing.T) {
	pricing, err := convertModelsDevToRatioData(
		strings.NewReader(multiSourcePayload),
		newModelsDevSelector(dto.UpstreamSourceModeAuto, nil),
	)
	require.NoError(t, err)

	candidates := pricing.Candidates["gpt-x"]
	require.Len(t, candidates, 3)
	// 候选携带的是已换算成本地口径的倍率，前端切换来源时直接使用
	assert.Equal(t, "openai", candidates[0].Provider)
	assert.Equal(t, 5.0, candidates[0].ModelRatio)
	require.NotNil(t, candidates[0].CompletionRatio)
	assert.Equal(t, 5.0, *candidates[0].CompletionRatio)
	assert.Nil(t, candidates[0].CacheRatio)

	assert.Equal(t, "azure", candidates[1].Provider)
	assert.True(t, candidates[1].Official)
	assert.Equal(t, "cheap-reseller", candidates[2].Provider)
	assert.False(t, candidates[2].Official)

	providersByName := make(map[string]dto.UpstreamProvider, len(pricing.Providers))
	for _, provider := range pricing.Providers {
		providersByName[provider.Provider] = provider
	}
	assert.Equal(t, 2, providersByName["cheap-reseller"].ModelCount)
	assert.False(t, providersByName["cheap-reseller"].Official)
	assert.True(t, providersByName["openai"].Official)
}
