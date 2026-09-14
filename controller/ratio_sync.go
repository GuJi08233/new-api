package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
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
	// llm-metadata 的两份产物：all.json 与 models.dev 结构一致但只保留厂商自营入口，
	// ratio_config 是上游预生成的 new-api 计费表达式。两者都挂在 SYNC_UPSTREAM_BASE 下，
	// 换镜像时和模型元数据同步一起生效。
	llmMetadataAllModelsPath   = "/api/all.json"
	llmMetadataRatioConfigPath = "/api/newapi/ratio_config-v1-base.json"
	// modelsDevThirdPartyRank 是转售/聚合商所在的档位，低于它的都算官方来源
	modelsDevThirdPartyRank = 3
	// maxUpstreamCandidates 限制下发给前端的可切换来源数量
	maxUpstreamCandidates = 8
	// upstreamCatalogTTL 缓存解析后的候选，避免逐个模型查询时反复下载整份上游数据
	upstreamCatalogTTL = 10 * time.Minute
)

// upstreamCatalog 是某个上游解析后的全量报价，按上游种类缓存复用。
// candidates 为共享只读数据，使用方排序前必须复制。
type upstreamCatalog struct {
	candidates map[string][]modelsDevCandidate
	// exprOverrides 是上游直接给出的成品计费表达式，用于本地无法从 cost 推导的计费形态
	exprOverrides map[string]string
	fetchedAt     time.Time
}

var (
	upstreamCatalogMutex sync.RWMutex
	upstreamCatalogs     = make(map[string]*upstreamCatalog)
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

// FetchUpstreamRatios 从所选上游拉取价格并与本地定价比对，返回可同步的差异项。
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

	upstreamKind := dto.UpstreamKindModelsDev
	if req.Upstream == dto.UpstreamKindLLMMetadata {
		upstreamKind = dto.UpstreamKindLLMMetadata
	}

	selector := newModelsDevSelector(req.SourceMode, req.PreferredProviders)
	pricing, err := fetchUpstreamPricing(c.Request.Context(), upstreamKind, req.Timeout, selector)
	if err != nil {
		logger.LogWarn(c.Request.Context(), "failed to fetch "+upstreamKind+" pricing: "+err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取 " + upstreamKind + " 价格失败：" + err.Error()})
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
	// 这样可视化编辑器能展示"这个模型在上游有哪些报价"。
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

func fetchUpstreamPricing(ctx context.Context, kind string, timeoutSeconds int, selector modelsDevSelector) (*upstreamPricing, error) {
	upstreamCatalogMutex.RLock()
	cached, ok := upstreamCatalogs[kind]
	upstreamCatalogMutex.RUnlock()
	if ok && time.Since(cached.fetchedAt) <= upstreamCatalogTTL {
		return selectUpstreamPricing(cached, selector), nil
	}

	var catalog *upstreamCatalog
	var err error
	if kind == dto.UpstreamKindLLMMetadata {
		catalog, err = fetchLLMMetadataCatalog(ctx, timeoutSeconds)
	} else {
		catalog, err = fetchModelsDevCatalog(ctx, timeoutSeconds)
	}
	if err != nil {
		return nil, err
	}

	catalog.fetchedAt = time.Now()
	upstreamCatalogMutex.Lock()
	upstreamCatalogs[kind] = catalog
	upstreamCatalogMutex.Unlock()

	return selectUpstreamPricing(catalog, selector), nil
}

// fetchUpstreamJSON 下载一个上游 JSON 文件并交给 decode 解析，
// 连接失败时最多重试 3 次，指数退避。
func fetchUpstreamJSON(ctx context.Context, url string, timeoutSeconds int, decode func(io.Reader) error) error {
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

	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, lastErr = client.Do(httpReq)
		if lastErr == nil {
			break
		}
		time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return decode(io.LimitReader(resp.Body, maxRatioConfigBytes))
}

func fetchModelsDevCatalog(ctx context.Context, timeoutSeconds int) (*upstreamCatalog, error) {
	var candidates map[string][]modelsDevCandidate
	err := fetchUpstreamJSON(ctx, modelsDevAPIURL, timeoutSeconds, func(body io.Reader) error {
		parsed, parseErr := parseModelsDevCandidates(body)
		candidates = parsed
		return parseErr
	})
	if err != nil {
		return nil, err
	}
	return &upstreamCatalog{candidates: candidates}, nil
}

// fetchLLMMetadataCatalog 取 basellm/llm-metadata 的两份产物：all.json 提供
// 与 models.dev 同构的成本数据（已过滤掉聚合商与转售商），ratio_config 提供
// 上游预生成的计费表达式，只用于本地推导不出来的那几类计费。
func fetchLLMMetadataCatalog(ctx context.Context, timeoutSeconds int) (*upstreamCatalog, error) {
	base := strings.TrimRight(getUpstreamBase(), "/")

	var candidates map[string][]modelsDevCandidate
	var config struct {
		Data struct {
			BillingExpr map[string]string `json:"billing_expr"`
		} `json:"data"`
	}
	var candidatesErr, configErr error

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		candidatesErr = fetchUpstreamJSON(ctx, base+llmMetadataAllModelsPath, timeoutSeconds, func(body io.Reader) error {
			parsed, parseErr := parseModelsDevCandidates(body)
			candidates = parsed
			return parseErr
		})
	}()
	go func() {
		defer wg.Done()
		// 表达式缺失不致命，本地仍能从 all.json 的 cost 推导出倍率与阶梯价
		configErr = fetchUpstreamJSON(ctx, base+llmMetadataRatioConfigPath, timeoutSeconds, func(body io.Reader) error {
			return common.DecodeJson(body, &config)
		})
	}()
	wg.Wait()

	if candidatesErr != nil {
		return nil, candidatesErr
	}
	if configErr != nil {
		logger.LogWarn(ctx, "llm-metadata ratio config unavailable, falling back to cost-derived pricing: "+configErr.Error())
	}

	// 上游只收录厂商自营入口，但套餐类 id（alibaba-token-plan 等）不在本地的
	// 一方名单里。统一按官方看待，免得被当成转售价排到最后。
	for _, modelCandidates := range candidates {
		for index := range modelCandidates {
			if modelCandidates[index].Rank >= modelsDevThirdPartyRank {
				modelCandidates[index].Rank = modelsDevThirdPartyRank - 1
			}
		}
	}

	return &upstreamCatalog{
		candidates:    candidates,
		exprOverrides: irreducibleBillingExprs(config.Data.BillingExpr),
	}, nil
}

// exprInputCoefficient 匹配计费表达式里的输入单价项，例如 "p * 0.4"
var exprInputCoefficient = regexp.MustCompile(`\bp \* ([0-9.]+)`)

// exprUsesInputPrice 判断表达式是否按给定的输入单价计费。上游成品表达式与本地
// 候选来自同一提供商时，其中必有一档的输入系数等于该候选的 input 价。
func exprUsesInputPrice(expr string, input float64) bool {
	for _, match := range exprInputCoefficient.FindAllStringSubmatch(expr, -1) {
		if value, err := strconv.ParseFloat(match[1], 64); err == nil && nearlyEqual(value, input) {
			return true
		}
	}
	return false
}

// irreducibleBillingExprs 挑出本地无法从 cost 字段推导的计费形态：Anthropic 的
// 1 小时缓存写（cc1h）、思考模式开关（param）和错峰定价（weekday/hour）。其余
// 模型仍走本地转换，档位命名才能统一成 272K / 1M。
func irreducibleBillingExprs(exprs map[string]string) map[string]string {
	overrides := make(map[string]string)
	for modelName, expr := range exprs {
		if strings.Contains(expr, "cc1h") || strings.Contains(expr, "param(") ||
			strings.Contains(expr, "weekday(") || strings.Contains(expr, "hour(") {
			overrides[modelName] = expr
		}
	}
	return overrides
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

// upstreamPricing 是一次上游拉取的结果：选中的价格、来源，以及
// 每个模型可切换的其它来源和全部提供商清单。
type upstreamPricing struct {
	Data       map[string]any
	Sources    map[string]dto.UpstreamSource
	Candidates map[string][]dto.UpstreamCandidate
	Providers  []dto.UpstreamProvider
}

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	Cost  modelsDevCost `json:"cost"`
	Limit struct {
		Context float64 `json:"context"`
	} `json:"limit"`
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
	// ContextLimit 是模型的上下文上限，用于给最后一档阶梯命名
	ContextLimit float64
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

func buildModelsDevCandidate(provider, modelID string, upstream modelsDevModel) (modelsDevCandidate, bool) {
	cost := upstream.Cost
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
		Provider:     provider,
		Rank:         modelsDevProviderRank(provider, modelID),
		Input:        input,
		Output:       output,
		CacheRead:    optionalCost(cost.CacheRead),
		CacheWrite:   optionalCost(cost.CacheWrite),
		InputAudio:   optionalCost(cost.InputAudio),
		OutputAudio:  optionalCost(cost.OutputAudio),
		Tiers:        validModelsDevTiers(cost.Tiers),
		ContextLimit: upstream.Limit.Context,
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

	// 同阈值的重复档位只留第一条：后面那条的分支永远不可达，档位名还会撞车
	deduped := valid[:1]
	for _, tier := range valid[1:] {
		if tier.Tier.Size != deduped[len(deduped)-1].Tier.Size {
			deduped = append(deduped, tier)
		}
	}
	return deduped
}

// formatContextTierName 把上下文 token 数写成人读的档位名（272000 -> 272K，
// 1048576 -> 1M），这个名字会出现在计费日志里。十进制和二进制整除都认，
// 其余按 5% 容差取整，免得 1050000 这类近似上限显示成 1.1M。
func formatContextTierName(tokens float64) string {
	unit, binary, suffix := 1000.0, 1024.0, "K"
	if tokens >= 1000000 {
		unit, binary, suffix = 1000000, 1048576, "M"
	}
	for _, base := range []float64{unit, binary} {
		if math.Mod(tokens, base) == 0 {
			return formatExprNumber(tokens/base) + suffix
		}
	}
	value := tokens / unit
	if rounded := math.Round(value); rounded > 0 && math.Abs(value-rounded)/value < 0.05 {
		return formatExprNumber(rounded) + suffix
	}
	return formatExprNumber(math.Round(value*10)/10) + suffix
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
// 逐级判断；表达式系数是真实的 $/1M 价格，不做倍率换算。档位数不限，多档会展开
// 成右结合的嵌套三元式（256K / 512K / 1M）。
func buildModelsDevTieredExpr(candidate modelsDevCandidate) string {
	if len(candidate.Tiers) == 0 {
		return ""
	}

	segments := []string{
		fmt.Sprintf("len <= %s ? tier(%q, %s)",
			formatExprNumber(candidate.Tiers[0].Tier.Size),
			formatContextTierName(candidate.Tiers[0].Tier.Size),
			buildModelsDevTierCost(candidate.Input, candidate.Output, candidate.CacheRead, candidate.CacheWrite),
		),
	}

	for index, tier := range candidate.Tiers {
		cost := buildModelsDevTierCost(*tier.Input, tier.Output, tier.CacheRead, tier.CacheWrite)
		if index+1 < len(candidate.Tiers) {
			next := candidate.Tiers[index+1].Tier.Size
			segments = append(segments, fmt.Sprintf("len <= %s ? tier(%q, %s)",
				formatExprNumber(next), formatContextTierName(next), cost))
			continue
		}

		// 末档按模型的上下文上限命名（272K 档之后就是 1M）。上游偶尔给出小于
		// 档位阈值的上限，或上限与阈值同名，这时退回 "272K+"，避免两档重名。
		thresholdName := formatContextTierName(tier.Tier.Size)
		finalName := thresholdName + "+"
		if candidate.ContextLimit > tier.Tier.Size {
			if limitName := formatContextTierName(candidate.ContextLimit); limitName != thresholdName {
				finalName = limitName
			}
		}
		segments = append(segments, fmt.Sprintf("tier(%q, %s)", finalName, cost))
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
func convertModelsDevToRatioData(reader io.Reader, selector modelsDevSelector) (*upstreamPricing, error) {
	candidatesByModel, err := parseModelsDevCandidates(reader)
	if err != nil {
		return nil, err
	}
	return selectUpstreamPricing(&upstreamCatalog{candidates: candidatesByModel}, selector), nil
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
			candidate, ok := buildModelsDevCandidate(provider, modelName, providerData.Models[modelName])
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

// selectUpstreamPricing 按来源偏好为每个模型挑一份默认报价，并保留其它候选。
func selectUpstreamPricing(catalog *upstreamCatalog, selector modelsDevSelector) *upstreamPricing {
	candidatesByModel := catalog.candidates
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

		// 上游成品表达式里有本地推导不出的 cc1h / 思考模式 / 错峰，但它是按上游
		// 自己选定的来源写的。倍率必须取自同一来源，否则会出现输入价 0.115 配
		// p * 0.4 这种自相矛盾的配置（阿里国内站与国际站的报价差）。用户没有指定
		// 首选提供商时改选价格一致的来源，否则尊重用户选择，放弃成品表达式。
		expr, useUpstreamExpr := catalog.exprOverrides[modelName]
		if useUpstreamExpr && !exprUsesInputPrice(expr, selected.Input) {
			matched, found := lo.Find(modelCandidates, func(candidate modelsDevCandidate) bool {
				return selector.allows(candidate) && exprUsesInputPrice(expr, candidate.Input)
			})
			if found && len(selector.preferredRank) == 0 {
				selected = matched
			} else {
				useUpstreamExpr = false
			}
		}
		if !useUpstreamExpr {
			expr = buildModelsDevTieredExpr(selected)
		}

		sources[modelName] = dto.UpstreamSource{
			Provider: selected.Provider,
			Official: selected.Rank < modelsDevThirdPartyRank,
		}

		if expr != "" {
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

	return &upstreamPricing{
		Data:       converted,
		Sources:    sources,
		Candidates: candidates,
		Providers:  providerList,
	}
}
