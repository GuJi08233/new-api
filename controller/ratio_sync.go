package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

const (
	defaultTimeoutSeconds       = 10
	maxTimeoutSeconds           = 60
	maxRatioConfigBytes         = 10 << 20 // 10MB
	floatEpsilon                = 1e-9
	modelsDevAPIURL             = "https://models.dev/api.json"
	modelsDevInputCostRatioBase = 1000.0
	// modelsDevThirdPartyRank 是转售/聚合商所在的档位，低于它的都算官方来源
	modelsDevThirdPartyRank = 3
	// maxUpstreamCandidates 限制下发给前端的可切换来源数量
	maxUpstreamCandidates = 8
	// modelsDevCacheTTL 缓存解析后的候选，避免逐个模型查询时反复下载整份 api.json
	modelsDevCacheTTL = 10 * time.Minute
)

var (
	modelsDevCacheMutex sync.RWMutex
	modelsDevCache      map[string][]modelsDevCandidate
	modelsDevCachedAt   time.Time
)

func nearlyEqual(a, b float64) bool {
	if a > b {
		return a-b < floatEpsilon
	}
	return b-a < floatEpsilon
}

func valuesEqual(a, b interface{}) bool {
	af, aok := a.(float64)
	bf, bok := b.(float64)
	if aok && bok {
		return nearlyEqual(af, bf)
	}
	return a == b
}

var pricingSyncFields = []string{
	"model_ratio",
	"completion_ratio",
	"cache_ratio",
	"create_cache_ratio",
	"image_ratio",
	"audio_ratio",
	"audio_completion_ratio",
	"model_price",
	billing_setting.BillingModeField,
	billing_setting.BillingExprField,
}

var numericPricingSyncFields = map[string]bool{
	"model_ratio":            true,
	"completion_ratio":       true,
	"cache_ratio":            true,
	"create_cache_ratio":     true,
	"image_ratio":            true,
	"audio_ratio":            true,
	"audio_completion_ratio": true,
	"model_price":            true,
}

func valueMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]float64:
		return lo.MapValues(typed, func(value float64, _ string) any { return value })
	case map[string]string:
		return lo.MapValues(typed, func(value string, _ string) any { return value })
	default:
		return nil
	}
}

func asFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func normalizeSyncValue(field string, value any) any {
	if numericPricingSyncFields[field] {
		if parsed, ok := asFloat64(value); ok {
			return parsed
		}
	}
	return value
}

func getLocalPricingSyncData() map[string]any {
	data := billing_setting.GetPricingSyncData(map[string]any(ratio_setting.GetExposedData()))
	data["image_ratio"] = ratio_setting.GetImageRatioCopy()
	data["audio_ratio"] = ratio_setting.GetAudioRatioCopy()
	data["audio_completion_ratio"] = ratio_setting.GetAudioCompletionRatioCopy()
	return data
}

// FetchUpstreamRatios 从 models.dev 拉取价格并与本地定价比对，返回可同步的差异项。
func FetchUpstreamRatios(c *gin.Context) {
	var req dto.UpstreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.SysError("failed to bind upstream request: " + err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数格式错误"})
		return
	}

	if req.Timeout <= 0 || req.Timeout > maxTimeoutSeconds {
		req.Timeout = defaultTimeoutSeconds
	}

	selector := newModelsDevSelector(req.SourceMode, req.PreferredProviders)
	pricing, err := fetchModelsDevPricing(c.Request.Context(), req.Timeout, selector)
	if err != nil {
		logger.LogWarn(c.Request.Context(), "failed to fetch models.dev pricing: "+err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取 models.dev 价格失败：" + err.Error()})
		return
	}

	var modelFilter map[string]struct{}
	switch {
	case len(req.ModelNames) > 0:
		modelFilter = make(map[string]struct{}, len(req.ModelNames))
		for _, m := range req.ModelNames {
			modelFilter[m] = struct{}{}
		}
	case req.OnlyEnabledModels:
		enabledModels, err := model.GetEnabledModelsWithError()
		if err != nil {
			logger.LogError(c.Request.Context(), "failed to query enabled models: "+err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询已启用模型失败"})
			return
		}
		modelFilter = make(map[string]struct{}, len(enabledModels))
		for _, m := range enabledModels {
			modelFilter[m] = struct{}{}
		}
	}

	differences := buildDifferences(getLocalPricingSyncData(), pricing.Data, modelFilter)

	// 显式按模型名查询时，即使价格和本地一致也要回传来源与候选，
	// 这样可视化编辑器能展示"这个模型在 models.dev 上有哪些报价"。
	reportedModels := make(map[string]struct{}, len(differences))
	if len(req.ModelNames) > 0 {
		for _, modelName := range req.ModelNames {
			reportedModels[modelName] = struct{}{}
		}
	} else {
		for modelName := range differences {
			reportedModels[modelName] = struct{}{}
		}
	}

	reportedSources := make(map[string]dto.UpstreamSource, len(reportedModels))
	reportedCandidates := make(map[string][]dto.UpstreamCandidate, len(reportedModels))
	for modelName := range reportedModels {
		if source, ok := pricing.Sources[modelName]; ok {
			reportedSources[modelName] = source
		}
		if candidates, ok := pricing.Candidates[modelName]; ok {
			reportedCandidates[modelName] = candidates
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"differences": differences,
			"sources":     reportedSources,
			"candidates":  reportedCandidates,
			"providers":   pricing.Providers,
		},
	})
}

func fetchModelsDevPricing(ctx context.Context, timeoutSeconds int, selector modelsDevSelector) (*modelsDevPricing, error) {
	if cached := cachedModelsDevCandidates(); cached != nil {
		return selectModelsDevPricing(cached, selector), nil
	}

	transport := &http.Transport{
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	client := &http.Client{Transport: transport}

	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// 简单重试：最多 3 次，指数退避
	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodGet, modelsDevAPIURL, nil)
		if err != nil {
			return nil, err
		}
		resp, lastErr = client.Do(httpReq)
		if lastErr == nil {
			break
		}
		time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned %s", resp.Status)
	}

	candidatesByModel, err := parseModelsDevCandidates(io.LimitReader(resp.Body, maxRatioConfigBytes))
	if err != nil {
		return nil, err
	}

	modelsDevCacheMutex.Lock()
	modelsDevCache = candidatesByModel
	modelsDevCachedAt = time.Now()
	modelsDevCacheMutex.Unlock()

	return selectModelsDevPricing(candidatesByModel, selector), nil
}

func cachedModelsDevCandidates() map[string][]modelsDevCandidate {
	modelsDevCacheMutex.RLock()
	defer modelsDevCacheMutex.RUnlock()
	if modelsDevCache == nil || time.Since(modelsDevCachedAt) > modelsDevCacheTTL {
		return nil
	}
	return modelsDevCache
}

func buildDifferences(localData, upstreamData map[string]any, modelFilter map[string]struct{}) map[string]map[string]dto.DifferenceItem {
	allModels := make(map[string]struct{})
	for _, field := range pricingSyncFields {
		for modelName := range valueMap(localData[field]) {
			allModels[modelName] = struct{}{}
		}
		for modelName := range valueMap(upstreamData[field]) {
			allModels[modelName] = struct{}{}
		}
	}

	// 当传入 modelFilter 时，严格只保留其中的模型。本地残留的定价配置不应
	// 让禁用或归档模型重新出现在同步列表中。
	if modelFilter != nil {
		filtered := make(map[string]struct{})
		for modelName := range allModels {
			if _, wanted := modelFilter[modelName]; wanted {
				filtered[modelName] = struct{}{}
			}
		}
		allModels = filtered
	}

	differences := make(map[string]map[string]dto.DifferenceItem)
	for modelName := range allModels {
		for _, field := range pricingSyncFields {
			upstreamRaw, hasUpstream := valueMap(upstreamData[field])[modelName]
			if !hasUpstream {
				continue
			}
			upstreamValue := normalizeSyncValue(field, upstreamRaw)

			var localValue interface{}
			if localRaw, hasLocal := valueMap(localData[field])[modelName]; hasLocal {
				localValue = normalizeSyncValue(field, localRaw)
				if valuesEqual(localValue, upstreamValue) {
					continue
				}
			}

			if differences[modelName] == nil {
				differences[modelName] = make(map[string]dto.DifferenceItem)
			}
			differences[modelName][field] = dto.DifferenceItem{
				Current:  localValue,
				Upstream: upstreamValue,
			}
		}
	}

	return differences
}

func roundRatioValue(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}

// modelsDevPricing 是一次 models.dev 拉取的结果：选中的价格、来源，以及
// 每个模型可切换的其它来源和全部提供商清单。
type modelsDevPricing struct {
	Data       map[string]any
	Sources    map[string]dto.UpstreamSource
	Candidates map[string][]dto.UpstreamCandidate
	Providers  []dto.UpstreamProvider
}

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	Cost modelsDevCost `json:"cost"`
}

type modelsDevCost struct {
	Input       *float64        `json:"input"`
	Output      *float64        `json:"output"`
	CacheRead   *float64        `json:"cache_read"`
	CacheWrite  *float64        `json:"cache_write"`
	InputAudio  *float64        `json:"input_audio"`
	OutputAudio *float64        `json:"output_audio"`
	Tiers       []modelsDevTier `json:"tiers"`
}

// modelsDevTier 是上下文超过 Tier.Size 之后适用的一档价格
type modelsDevTier struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
	Tier       struct {
		Type string  `json:"type"`
		Size float64 `json:"size"`
	} `json:"tier"`
}

type modelsDevCandidate struct {
	Provider    string
	Rank        int
	Input       float64
	Output      *float64
	CacheRead   *float64
	CacheWrite  *float64
	InputAudio  *float64
	OutputAudio *float64
	Tiers       []modelsDevTier
}

// models.dev 上同一个模型 id 常被几十家提供商收录，价格能差好几倍。
// 这里按来源可信度分档，取第一个有条目的档位：
//
//	0 原厂：模型自己的厂商（deepseek-* 取 deepseek，而不是托管它的 alibaba-cn）
//	1 其它模型厂商自营入口
//	2 厂商官方云托管
//	3 转售/聚合商
//
// modelsDevVendorPrefixes 把模型 id 的家族前缀映射到原厂 provider。
var modelsDevVendorPrefixes = []struct {
	Prefix    string
	Providers []string
}{
	{"gpt-", []string{"openai"}},
	{"chatgpt", []string{"openai"}},
	{"codex", []string{"openai"}},
	{"o1", []string{"openai"}},
	{"o3", []string{"openai"}},
	{"o4", []string{"openai"}},
	{"claude-", []string{"anthropic"}},
	{"gemini-", []string{"google"}},
	{"gemma-", []string{"google"}},
	{"deepseek", []string{"deepseek"}},
	{"grok-", []string{"xai"}},
	{"qwen", []string{"alibaba", "alibaba-cn"}},
	{"qwq", []string{"alibaba", "alibaba-cn"}},
	{"qvq", []string{"alibaba", "alibaba-cn"}},
	{"kimi-", []string{"moonshotai", "moonshotai-cn"}},
	{"moonshot", []string{"moonshotai", "moonshotai-cn"}},
	{"glm-", []string{"zhipuai", "zai"}},
	{"minimax", []string{"minimax", "minimax-cn"}},
	{"abab", []string{"minimax", "minimax-cn"}},
	{"step-", []string{"stepfun", "stepfun-ai"}},
	{"doubao-", []string{"volcengine"}},
	{"llama-", []string{"meta", "llama"}},
	{"mistral", []string{"mistral"}},
	{"mixtral", []string{"mistral"}},
	{"magistral", []string{"mistral"}},
	{"codestral", []string{"mistral"}},
	{"devstral", []string{"mistral"}},
	{"ministral", []string{"mistral"}},
	{"pixtral", []string{"mistral"}},
	{"voxtral", []string{"mistral"}},
	{"command-", []string{"cohere"}},
	{"sonar", []string{"perplexity"}},
	{"solar-", []string{"upstage"}},
	{"nemotron", []string{"nvidia"}},
	{"sensechat", []string{"sensenova"}},
	{"sensenova", []string{"sensenova"}},
	{"mimo-", []string{"xiaomi"}},
	{"longcat", []string{"longcat"}},
	{"morph-", []string{"morph"}},
	{"mercury", []string{"inception"}},
	{"sarvam", []string{"sarvam"}},
}

var modelsDevFirstPartyProviders = map[string]struct{}{
	"openai": {}, "anthropic": {}, "google": {}, "deepseek": {}, "xai": {},
	"mistral": {}, "meta": {}, "llama": {}, "cohere": {}, "perplexity": {},
	"alibaba": {}, "alibaba-cn": {}, "moonshotai": {}, "moonshotai-cn": {},
	"zhipuai": {}, "zai": {}, "minimax": {}, "minimax-cn": {}, "stepfun": {},
	"stepfun-ai": {}, "volcengine": {}, "sensenova": {}, "upstage": {},
	"nvidia": {}, "inception": {}, "longcat": {}, "xiaomi": {}, "sarvam": {},
	"morph": {}, "bailing": {}, "thinkingmachines": {},
}

// 厂商官方云托管：价格通常与官方一致，仅在没有厂商自营条目时使用。
var modelsDevCloudProviders = map[string]struct{}{
	"azure": {}, "azure-cognitive-services": {}, "amazon-bedrock": {},
	"google-vertex": {}, "google-vertex-anthropic": {},
}

func isModelsDevVendorOfModel(provider, modelID string) bool {
	// 聚合商常用 "vendor/model" 形式的 id，比较家族前缀前先去掉这一段
	if idx := strings.LastIndex(modelID, "/"); idx >= 0 {
		modelID = modelID[idx+1:]
	}
	modelID = strings.ToLower(modelID)

	for _, entry := range modelsDevVendorPrefixes {
		if !strings.HasPrefix(modelID, entry.Prefix) {
			continue
		}
		for _, vendor := range entry.Providers {
			if provider == vendor {
				return true
			}
		}
	}
	return false
}

func modelsDevProviderRank(provider, modelID string) int {
	if isModelsDevVendorOfModel(provider, modelID) {
		return 0
	}
	if _, ok := modelsDevFirstPartyProviders[provider]; ok {
		return 1
	}
	if _, ok := modelsDevCloudProviders[provider]; ok {
		return 2
	}
	return modelsDevThirdPartyRank
}

func cloneFloatPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func isValidNonNegativeCost(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	return v >= 0
}

func buildModelsDevCandidate(provider, modelID string, cost modelsDevCost) (modelsDevCandidate, bool) {
	if cost.Input == nil {
		return modelsDevCandidate{}, false
	}

	input := *cost.Input
	if !isValidNonNegativeCost(input) {
		return modelsDevCandidate{}, false
	}

	var output *float64
	if cost.Output != nil {
		if !isValidNonNegativeCost(*cost.Output) {
			return modelsDevCandidate{}, false
		}
		output = cloneFloatPtr(cost.Output)
	}

	// input=0/output>0 cannot be transformed into local ratio.
	if input == 0 && output != nil && *output > 0 {
		return modelsDevCandidate{}, false
	}

	// 这些是可选的补充计价项，单项无效时只忽略该项，不影响整条报价
	optionalCost := func(value *float64) *float64 {
		if value != nil && isValidNonNegativeCost(*value) {
			return cloneFloatPtr(value)
		}
		return nil
	}

	return modelsDevCandidate{
		Provider:    provider,
		Rank:        modelsDevProviderRank(provider, modelID),
		Input:       input,
		Output:      output,
		CacheRead:   optionalCost(cost.CacheRead),
		CacheWrite:  optionalCost(cost.CacheWrite),
		InputAudio:  optionalCost(cost.InputAudio),
		OutputAudio: optionalCost(cost.OutputAudio),
		Tiers:       validModelsDevTiers(cost.Tiers),
	}, true
}

// validModelsDevTiers 只保留有阈值和输入价的上下文档位，并按阈值升序排列
func validModelsDevTiers(tiers []modelsDevTier) []modelsDevTier {
	valid := make([]modelsDevTier, 0, len(tiers))
	for _, tier := range tiers {
		if tier.Tier.Type != "context" || tier.Tier.Size <= 0 {
			continue
		}
		if tier.Input == nil || !isValidNonNegativeCost(*tier.Input) {
			continue
		}
		valid = append(valid, tier)
	}
	if len(valid) == 0 {
		return nil
	}
	sort.Slice(valid, func(i, j int) bool {
		return valid[i].Tier.Size < valid[j].Tier.Size
	})
	return valid
}

// modelsDevSelector 决定同一个模型的多个报价里用哪一个作为默认值。
type modelsDevSelector struct {
	officialOnly  bool
	preferredRank map[string]int
}

func newModelsDevSelector(mode string, preferredProviders []string) modelsDevSelector {
	switch mode {
	case dto.UpstreamSourceModeOfficialOnly:
		return modelsDevSelector{officialOnly: true}
	case dto.UpstreamSourceModePrefer:
		preferredRank := make(map[string]int, len(preferredProviders))
		for index, provider := range preferredProviders {
			if _, exists := preferredRank[provider]; !exists {
				preferredRank[provider] = index
			}
		}
		return modelsDevSelector{preferredRank: preferredRank}
	default:
		return modelsDevSelector{}
	}
}

func (s modelsDevSelector) allows(candidate modelsDevCandidate) bool {
	return !s.officialOnly || candidate.Rank < modelsDevThirdPartyRank
}

// betterThan 返回 a 是否应该排在 b 前面
func (s modelsDevSelector) betterThan(a, b modelsDevCandidate) bool {
	if len(s.preferredRank) > 0 {
		aIndex, aPreferred := s.preferredRank[a.Provider]
		bIndex, bPreferred := s.preferredRank[b.Provider]
		if aPreferred != bPreferred {
			return aPreferred
		}
		if aPreferred && aIndex != bIndex {
			return aIndex < bIndex
		}
	}
	if a.Rank != b.Rank {
		// Official pricing wins even when a reseller quotes less.
		return a.Rank < b.Rank
	}
	aNonZero := a.Input > 0
	bNonZero := b.Input > 0
	if aNonZero != bNonZero {
		// Prefer non-zero pricing data; this matches "cheapest non-zero" conflict policy.
		return aNonZero
	}
	if aNonZero && !nearlyEqual(a.Input, b.Input) {
		return a.Input < b.Input
	}
	// Stable tie-breaker for deterministic result.
	return a.Provider < b.Provider
}

func formatExprNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// buildModelsDevTierCost 拼出一档的计费表达式。缺失的子类别不写成独立项，
// 对应的 token 会落回 p/c 按基础价计费（见 pkg/billingexpr/expr.md 的自动排除机制）。
func buildModelsDevTierCost(input float64, output, cacheRead, cacheWrite *float64) string {
	parts := []string{"p * " + formatExprNumber(input)}
	if output != nil {
		parts = append(parts, "c * "+formatExprNumber(*output))
	}
	if cacheRead != nil {
		parts = append(parts, "cr * "+formatExprNumber(*cacheRead))
	}
	if cacheWrite != nil {
		parts = append(parts, "cc * "+formatExprNumber(*cacheWrite))
	}
	return strings.Join(parts, " + ")
}

// buildModelsDevTieredExpr 把 models.dev 的上下文档位翻译成本地的阶梯计费表达式。
// models.dev 的 tier.size 表示"上下文超过该长度后改用这一档价格"，因此用 len
// 逐级判断；表达式系数是真实的 $/1M 价格，不做倍率换算。
func buildModelsDevTieredExpr(candidate modelsDevCandidate) string {
	if len(candidate.Tiers) == 0 {
		return ""
	}

	segments := []string{
		fmt.Sprintf("len <= %s ? tier(\"ctx<=%s\", %s)",
			formatExprNumber(candidate.Tiers[0].Tier.Size),
			formatExprNumber(candidate.Tiers[0].Tier.Size),
			buildModelsDevTierCost(candidate.Input, candidate.Output, candidate.CacheRead, candidate.CacheWrite),
		),
	}

	for index, tier := range candidate.Tiers {
		cost := buildModelsDevTierCost(*tier.Input, tier.Output, tier.CacheRead, tier.CacheWrite)
		if index+1 < len(candidate.Tiers) {
			next := candidate.Tiers[index+1].Tier.Size
			segments = append(segments, fmt.Sprintf("len <= %s ? tier(\"ctx<=%s\", %s)",
				formatExprNumber(next), formatExprNumber(next), cost))
			continue
		}
		segments = append(segments, fmt.Sprintf("tier(\"ctx>%s\", %s)",
			formatExprNumber(tier.Tier.Size), cost))
	}

	return strings.Join(segments, " : ")
}

func toUpstreamCandidate(candidate modelsDevCandidate) dto.UpstreamCandidate {
	out := dto.UpstreamCandidate{
		Provider:    candidate.Provider,
		Official:    candidate.Rank < modelsDevThirdPartyRank,
		BillingExpr: buildModelsDevTieredExpr(candidate),
	}
	if candidate.Input == 0 {
		return out
	}

	out.ModelRatio = roundRatioValue(candidate.Input * float64(ratio_setting.USD) / modelsDevInputCostRatioBase)
	out.CompletionRatio = ratioAgainst(candidate.Output, candidate.Input)
	out.CacheRatio = ratioAgainst(candidate.CacheRead, candidate.Input)
	out.CreateCacheRatio = ratioAgainst(candidate.CacheWrite, candidate.Input)
	out.AudioRatio = ratioAgainst(candidate.InputAudio, candidate.Input)
	if candidate.InputAudio != nil && *candidate.InputAudio > 0 {
		// 本地的音频补全倍率是相对音频输入价，而不是文本输入价
		out.AudioCompletionRatio = ratioAgainst(candidate.OutputAudio, *candidate.InputAudio)
	}
	return out
}

func ratioAgainst(value *float64, base float64) *float64 {
	if value == nil || base <= 0 {
		return nil
	}
	ratio := roundRatioValue(*value / base)
	return &ratio
}

// convertModelsDevToRatioData parses models.dev /api.json and converts
// provider pricing metadata into local ratio format.
// models.dev costs are USD per 1M tokens:
//
//	model_ratio = input_cost_per_1M / 2
//	completion_ratio = output_cost / input_cost
//	cache_ratio = cache_read_cost / input_cost
//
// Duplicate model keys across providers are ordered by the selector: by default
// the model's own vendor wins over other vendors, their official clouds and
// finally resellers, and within the same rank the cheapest non-zero input cost
// wins. The remaining candidates are returned too, so the caller can offer them
// as alternative sources.
func convertModelsDevToRatioData(reader io.Reader, selector modelsDevSelector) (*modelsDevPricing, error) {
	candidatesByModel, err := parseModelsDevCandidates(reader)
	if err != nil {
		return nil, err
	}
	return selectModelsDevPricing(candidatesByModel, selector), nil
}

// parseModelsDevCandidates 把 api.json 解析成 模型 -> 全部可用报价。
// 结果会被缓存复用，调用方不得原地修改。
func parseModelsDevCandidates(reader io.Reader) (map[string][]modelsDevCandidate, error) {
	var upstreamData map[string]modelsDevProvider
	if err := common.DecodeJson(reader, &upstreamData); err != nil {
		return nil, fmt.Errorf("failed to decode models.dev response: %w", err)
	}
	if len(upstreamData) == 0 {
		return nil, fmt.Errorf("empty models.dev response")
	}

	providers := make([]string, 0, len(upstreamData))
	for provider := range upstreamData {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	candidatesByModel := make(map[string][]modelsDevCandidate)
	for _, provider := range providers {
		providerData := upstreamData[provider]
		if len(providerData.Models) == 0 {
			continue
		}

		modelNames := make([]string, 0, len(providerData.Models))
		for modelName := range providerData.Models {
			modelNames = append(modelNames, modelName)
		}
		sort.Strings(modelNames)

		for _, modelName := range modelNames {
			candidate, ok := buildModelsDevCandidate(provider, modelName, providerData.Models[modelName].Cost)
			if !ok {
				continue
			}
			candidatesByModel[modelName] = append(candidatesByModel[modelName], candidate)
		}
	}

	if len(candidatesByModel) == 0 {
		return nil, fmt.Errorf("no valid models.dev pricing entries found")
	}

	return candidatesByModel, nil
}

// selectModelsDevPricing 按来源偏好为每个模型挑一份默认报价，并保留其它候选。
func selectModelsDevPricing(candidatesByModel map[string][]modelsDevCandidate, selector modelsDevSelector) *modelsDevPricing {
	modelRatioMap := make(map[string]any)
	completionRatioMap := make(map[string]any)
	cacheRatioMap := make(map[string]any)
	createCacheRatioMap := make(map[string]any)
	audioRatioMap := make(map[string]any)
	audioCompletionRatioMap := make(map[string]any)
	billingModeMap := make(map[string]any)
	billingExprMap := make(map[string]any)
	sources := make(map[string]dto.UpstreamSource, len(candidatesByModel))
	candidates := make(map[string][]dto.UpstreamCandidate, len(candidatesByModel))
	providerModelCount := make(map[string]int)
	providerOfficial := make(map[string]bool)

	for modelName, cachedCandidates := range candidatesByModel {
		// 缓存的候选是共享只读数据，排序前必须复制
		modelCandidates := make([]modelsDevCandidate, len(cachedCandidates))
		copy(modelCandidates, cachedCandidates)
		sort.SliceStable(modelCandidates, func(i, j int) bool {
			return selector.betterThan(modelCandidates[i], modelCandidates[j])
		})

		reported := modelCandidates
		if len(reported) > maxUpstreamCandidates {
			reported = reported[:maxUpstreamCandidates]
		}
		dtoCandidates := make([]dto.UpstreamCandidate, 0, len(reported))
		for _, candidate := range reported {
			dtoCandidates = append(dtoCandidates, toUpstreamCandidate(candidate))
		}
		candidates[modelName] = dtoCandidates

		for _, candidate := range modelCandidates {
			providerModelCount[candidate.Provider]++
			if candidate.Rank < modelsDevThirdPartyRank {
				providerOfficial[candidate.Provider] = true
			}
		}

		selected, ok := lo.Find(modelCandidates, selector.allows)
		if !ok {
			// official_only 下没有官方报价的模型不参与同步
			continue
		}

		sources[modelName] = dto.UpstreamSource{
			Provider: selected.Provider,
			Official: selected.Rank < modelsDevThirdPartyRank,
		}

		if expr := buildModelsDevTieredExpr(selected); expr != "" {
			billingModeMap[modelName] = billing_setting.BillingModeTieredExpr
			billingExprMap[modelName] = expr
		}

		if selected.Input == 0 {
			modelRatioMap[modelName] = 0.0
			continue
		}

		modelRatio := selected.Input * float64(ratio_setting.USD) / modelsDevInputCostRatioBase
		modelRatioMap[modelName] = roundRatioValue(modelRatio)

		putRatio := func(target map[string]any, value *float64, base float64) {
			if ratio := ratioAgainst(value, base); ratio != nil {
				target[modelName] = *ratio
			}
		}
		putRatio(completionRatioMap, selected.Output, selected.Input)
		putRatio(cacheRatioMap, selected.CacheRead, selected.Input)
		putRatio(createCacheRatioMap, selected.CacheWrite, selected.Input)
		putRatio(audioRatioMap, selected.InputAudio, selected.Input)
		if selected.InputAudio != nil {
			putRatio(audioCompletionRatioMap, selected.OutputAudio, *selected.InputAudio)
		}
	}

	converted := make(map[string]any)
	for field, values := range map[string]map[string]any{
		"model_ratio":                    modelRatioMap,
		"completion_ratio":               completionRatioMap,
		"cache_ratio":                    cacheRatioMap,
		"create_cache_ratio":             createCacheRatioMap,
		"audio_ratio":                    audioRatioMap,
		"audio_completion_ratio":         audioCompletionRatioMap,
		billing_setting.BillingModeField: billingModeMap,
		billing_setting.BillingExprField: billingExprMap,
	} {
		if len(values) > 0 {
			converted[field] = values
		}
	}

	providerList := make([]dto.UpstreamProvider, 0, len(providerModelCount))
	for provider, count := range providerModelCount {
		providerList = append(providerList, dto.UpstreamProvider{
			Provider:   provider,
			Official:   providerOfficial[provider],
			ModelCount: count,
		})
	}
	sort.Slice(providerList, func(i, j int) bool {
		if providerList[i].Official != providerList[j].Official {
			return providerList[i].Official
		}
		if providerList[i].ModelCount != providerList[j].ModelCount {
			return providerList[i].ModelCount > providerList[j].ModelCount
		}
		return providerList[i].Provider < providerList[j].Provider
	})

	return &modelsDevPricing{
		Data:       converted,
		Sources:    sources,
		Candidates: candidates,
		Providers:  providerList,
	}
}
