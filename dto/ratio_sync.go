package dto

const (
	// UpstreamSourceModeAuto 官方优先，同档内取最便宜的非零报价
	UpstreamSourceModeAuto = "auto"
	// UpstreamSourceModeOfficialOnly 只用官方来源，没有官方条目的模型不参与同步
	UpstreamSourceModeOfficialOnly = "official_only"
	// UpstreamSourceModePrefer 按 PreferredProviders 的顺序优先，其余回落到 auto 规则
	UpstreamSourceModePrefer = "prefer"
)

const (
	// UpstreamKindModelsDev 直连 models.dev/api.json，覆盖全量模型，含转售商报价
	UpstreamKindModelsDev = "models.dev"
	// UpstreamKindLLMMetadata 使用 basellm/llm-metadata 预生成的官方定价，
	// 只收录厂商自营入口，并补全 1 小时缓存写、思考模式、错峰这几类计费
	UpstreamKindLLMMetadata = "llm-metadata"
)

type UpstreamRequest struct {
	Timeout int `json:"timeout"`
	// OnlyEnabledModels 只比对当前启用渠道中的模型
	OnlyEnabledModels bool `json:"only_enabled_models"`
	// ModelNames 只比对指定模型，优先于 OnlyEnabledModels
	ModelNames []string `json:"model_names"`
	// Upstream 取值见 UpstreamKind* 常量，留空按 models.dev 处理
	Upstream string `json:"upstream"`
	// SourceMode 取值见 UpstreamSourceMode* 常量，留空按 auto 处理
	SourceMode string `json:"source_mode"`
	// PreferredProviders 仅在 SourceMode 为 prefer 时生效，按给定顺序优先
	PreferredProviders []string `json:"preferred_providers"`
}

// DifferenceItem 差异项
// Current 为本地值，未配置时为 nil
// Upstream 为 models.dev 的值

type DifferenceItem struct {
	Current  interface{} `json:"current"`
	Upstream interface{} `json:"upstream"`
}

// UpstreamSource 说明某个模型的上游价格取自 models.dev 的哪个提供商，
// Official 为 true 表示模型厂商自营或其官方云托管入口，而非转售/聚合商。
type UpstreamSource struct {
	Provider string `json:"provider"`
	Official bool   `json:"official"`
}

// UpstreamCandidate 是某个模型在 models.dev 上的一个可选报价来源，
// 倍率已换算为本地口径，前端切换来源时直接使用。
type UpstreamCandidate struct {
	Provider             string   `json:"provider"`
	Official             bool     `json:"official"`
	ModelRatio           float64  `json:"model_ratio"`
	CompletionRatio      *float64 `json:"completion_ratio,omitempty"`
	CacheRatio           *float64 `json:"cache_ratio,omitempty"`
	CreateCacheRatio     *float64 `json:"create_cache_ratio,omitempty"`
	AudioRatio           *float64 `json:"audio_ratio,omitempty"`
	AudioCompletionRatio *float64 `json:"audio_completion_ratio,omitempty"`
	// BillingExpr 非空表示该来源有上下文阶梯价，可整体改用表达式计费
	BillingExpr string `json:"billing_expr,omitempty"`
}

// UpstreamProvider 用于前端选择首选提供商
type UpstreamProvider struct {
	Provider   string `json:"provider"`
	Official   bool   `json:"official"`
	ModelCount int    `json:"model_count"`
}
