package operation_setting

import (
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// 客户端 UA 准入规则：按入站 User-Agent 限制哪些客户端可以调用指定模型/分组/路径/渠道。
// UA 由客户端任意填写，规则只用于约束正常客户端，不构成安全边界。
// UA 条目的写法见 UaPattern，与风控 UA 黑名单一致。

const ClientUaRuleSettingPrefix = "client_ua_rule_setting."

type ClientUaRule struct {
	Name       string   `json:"name"`
	Disabled   bool     `json:"disabled,omitempty"`
	ModelRegex []string `json:"model_regex,omitempty"` // 空=所有模型；匹配客户端请求的原始模型名
	GroupRegex []string `json:"group_regex,omitempty"` // 空=不限；分组取值与静态渠道路由一致
	PathRegex  []string `json:"path_regex,omitempty"`  // 空=不限；匹配请求路径
	ChannelIDs []int    `json:"channel_ids,omitempty"` // 空=选路前判定；非空=选中这些渠道时才判定
	UaAllow    []string `json:"ua_allow,omitempty"`    // 非空时 UA 必须命中其一
	UaDeny     []string `json:"ua_deny,omitempty"`     // 命中任一即拒绝，优先于 UaAllow
	Message    string   `json:"message,omitempty"`     // 空则使用本地化默认文案
	StatusCode int      `json:"status_code,omitempty"` // 0 表示 403，仅允许 400-499
}

type ClientUaRuleSetting struct {
	Enabled bool           `json:"enabled"`
	Rules   []ClientUaRule `json:"rules"`
}

// ClientUaRequest 是一次准入判定所需的请求特征。
type ClientUaRequest struct {
	Model     string
	Group     string
	Path      string
	ChannelID int // 0 表示选路前判定，只评估未绑定渠道的规则
	UserAgent string
}

type compiledClientUaRule struct {
	rule    ClientUaRule
	models  []*regexp.Regexp
	groups  []*regexp.Regexp
	paths   []*regexp.Regexp
	allow   []UaPattern
	deny    []UaPattern
	channel map[int]struct{}
}

// clientUaRuleSetting is only a staging area mutated in place by the config
// framework (reflection-based, unsynchronized). Request goroutines must never
// read it directly; they read the immutable compiled snapshot published below.
var clientUaRuleSetting = ClientUaRuleSetting{
	Enabled: false,
	Rules:   []ClientUaRule{},
}

// clientUaRuleSnapshot 只保存生效中的已编译规则；总开关关闭时为空切片，请求路径零开销。
var clientUaRuleSnapshot atomic.Pointer[[]compiledClientUaRule]

// ClientUaRuleStatus 概括客户端 UA 准入规则的生效情况，供风控概览展示。
type ClientUaRuleStatus struct {
	Enabled    bool `json:"enabled"`
	Configured int  `json:"configured"` // 已配置的规则数，含停用与无法编译的规则
	Active     int  `json:"active"`     // 正在生效的规则数
}

var clientUaRuleStatus atomic.Pointer[ClientUaRuleStatus]

// GetClientUaRuleStatus 返回最近一次发布的规则生效情况。
func GetClientUaRuleStatus() ClientUaRuleStatus {
	if status := clientUaRuleStatus.Load(); status != nil {
		return *status
	}
	return ClientUaRuleStatus{}
}

func init() {
	config.GlobalConfig.Register("client_ua_rule_setting", &clientUaRuleSetting)
	SyncClientUaRuleSetting()
}

// SyncClientUaRuleSetting 把暂存配置预编译后发布为只读快照。
// 每次暂存结构更新后都必须调用(见 model/option.go 的 handleConfigUpdate)。
// 保存入口已校验过规则；回放到无法编译的历史规则时跳过该条并记录系统日志。
func SyncClientUaRuleSetting() {
	compiled := make([]compiledClientUaRule, 0)
	if clientUaRuleSetting.Enabled {
		var rules []ClientUaRule
		data, err := common.Marshal(clientUaRuleSetting.Rules)
		if err == nil {
			err = common.Unmarshal(data, &rules)
		}
		if err != nil {
			common.SysError(fmt.Sprintf("client ua rule: failed to copy rules: %s", err.Error()))
		}
		for index, rule := range rules {
			if rule.Disabled {
				continue
			}
			item, err := compileClientUaRule(rule)
			if err != nil {
				common.SysError(fmt.Sprintf("client ua rule: skip rule #%d %q: %s", index+1, rule.Name, err.Error()))
				continue
			}
			compiled = append(compiled, item)
		}
	}
	clientUaRuleSnapshot.Store(&compiled)
	clientUaRuleStatus.Store(&ClientUaRuleStatus{Enabled: clientUaRuleSetting.Enabled,
		Configured: len(clientUaRuleSetting.Rules), Active: len(compiled)})
}

// SyncConfig lets ConfigManager publish a fresh compiled snapshot after a
// complete configuration load (startup and tests).
func (s *ClientUaRuleSetting) SyncConfig() {
	if s == &clientUaRuleSetting {
		SyncClientUaRuleSetting()
	}
}

// SetClientUaRuleSettingForTest 启用给定规则并发布快照，返回恢复原配置的函数，供跨包测试使用。
func SetClientUaRuleSettingForTest(rules []ClientUaRule) (restore func()) {
	previous := clientUaRuleSetting
	clientUaRuleSetting = ClientUaRuleSetting{Enabled: true, Rules: rules}
	SyncClientUaRuleSetting()
	return func() {
		clientUaRuleSetting = previous
		SyncClientUaRuleSetting()
	}
}

// FindViolatedClientUaRule 返回请求违反的第一条规则(按配置顺序)，全部通过时返回 nil。
func FindViolatedClientUaRule(req ClientUaRequest) *ClientUaRule {
	snapshot := clientUaRuleSnapshot.Load()
	if snapshot == nil || len(*snapshot) == 0 {
		return nil
	}
	ua := strings.ToLower(req.UserAgent)
	for i := range *snapshot {
		item := &(*snapshot)[i]
		if req.ChannelID == 0 {
			if len(item.channel) > 0 {
				continue
			}
		} else if _, ok := item.channel[req.ChannelID]; !ok {
			continue
		}
		if !matchAnyClientUaRegex(item.models, req.Model) ||
			!matchAnyClientUaRegex(item.groups, req.Group) ||
			!matchAnyClientUaRegex(item.paths, req.Path) {
			continue
		}
		if matchAnyClientUa(item.deny, ua) || (len(item.allow) > 0 && !matchAnyClientUa(item.allow, ua)) {
			rule := item.rule
			return &rule
		}
	}
	return nil
}

// matchAnyClientUaRegex 空条件视为不限。
func matchAnyClientUaRegex(patterns []*regexp.Regexp, value string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, re := range patterns {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

// matchAnyClientUa 的 ua 参数必须已转小写。
func matchAnyClientUa(patterns []UaPattern, ua string) bool {
	for _, pattern := range patterns {
		if pattern.Match(ua) {
			return true
		}
	}
	return false
}

// compileClientUaRule 校验并预编译一条规则；保存校验与快照发布共用，保证两者口径一致。
func compileClientUaRule(rule ClientUaRule) (compiledClientUaRule, error) {
	item := compiledClientUaRule{rule: rule}
	if strings.TrimSpace(rule.Name) == "" {
		return item, fmt.Errorf("规则名称不能为空")
	}
	if len(rule.UaAllow) == 0 && len(rule.UaDeny) == 0 {
		return item, fmt.Errorf("规则 %q 的 UA 白名单与黑名单不能同时为空", rule.Name)
	}
	if rule.StatusCode != 0 && (rule.StatusCode < 400 || rule.StatusCode > 499) {
		return item, fmt.Errorf("规则 %q 的状态码必须在 400-499 之间", rule.Name)
	}

	var err error
	if item.models, err = compileClientUaRegexList(rule.Name, "模型", rule.ModelRegex); err != nil {
		return item, err
	}
	if item.groups, err = compileClientUaRegexList(rule.Name, "分组", rule.GroupRegex); err != nil {
		return item, err
	}
	if item.paths, err = compileClientUaRegexList(rule.Name, "路径", rule.PathRegex); err != nil {
		return item, err
	}
	if item.allow, err = compileClientUaMatchers(rule.Name, "UA 白名单", rule.UaAllow); err != nil {
		return item, err
	}
	if item.deny, err = compileClientUaMatchers(rule.Name, "UA 黑名单", rule.UaDeny); err != nil {
		return item, err
	}
	if len(rule.ChannelIDs) > 0 {
		item.channel = make(map[int]struct{}, len(rule.ChannelIDs))
		for _, id := range rule.ChannelIDs {
			if id <= 0 {
				return item, fmt.Errorf("规则 %q 的渠道 ID 必须为正整数", rule.Name)
			}
			item.channel[id] = struct{}{}
		}
	}
	return item, nil
}

func compileClientUaRegexList(ruleName string, label string, patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for index, pattern := range patterns {
		if strings.TrimSpace(pattern) == "" {
			return nil, fmt.Errorf("规则 %q 的%s条件第 %d 项不能为空", ruleName, label, index+1)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("规则 %q 的%s条件第 %d 项不是合法正则: %s", ruleName, label, index+1, err.Error())
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

func compileClientUaMatchers(ruleName string, label string, entries []string) ([]UaPattern, error) {
	patterns := make([]UaPattern, 0, len(entries))
	for index, entry := range entries {
		pattern, err := CompileUaPattern(entry)
		if err != nil {
			return nil, fmt.Errorf("规则 %q 的 %s第 %d 项%s", ruleName, label, index+1, err.Error())
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

// ValidateClientUaRuleOption 在保存前校验客户端 UA 规则配置项。
func ValidateClientUaRuleOption(key string, value string) error {
	field, ok := strings.CutPrefix(key, ClientUaRuleSettingPrefix)
	if !ok {
		return nil
	}
	switch field {
	case "enabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("客户端 UA 规则开关必须为 true 或 false")
		}
	case "rules":
		var rules []ClientUaRule
		if err := common.UnmarshalJsonStr(value, &rules); err != nil {
			return fmt.Errorf("客户端 UA 规则必须是合法的 JSON 数组: %s", err.Error())
		}
		names := make(map[string]struct{}, len(rules))
		for _, rule := range rules {
			if _, err := compileClientUaRule(rule); err != nil {
				return err
			}
			name := strings.TrimSpace(rule.Name)
			if _, exists := names[name]; exists {
				return fmt.Errorf("规则名称 %q 重复", name)
			}
			names[name] = struct{}{}
		}
	}
	return nil
}
