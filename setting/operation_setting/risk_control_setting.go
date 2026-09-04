package operation_setting

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// RiskAutoBanRule 定义一条基于滥用指标的自动封禁规则。
type RiskAutoBanRule struct {
	Enabled     bool   `json:"enabled"`
	Metric      string `json:"metric"`       // ip_multi_user | user_multi_ip | ip_multi_token | user_tiny_request | user_error_burst
	WindowHours int    `json:"window_hours"` // 统计窗口(小时),<=0 时回退默认 24
	Threshold   int    `json:"threshold"`    // 指标严格大于该值时触发
	Action      string `json:"action"`       // alert | disable_user | ban_ip | ban_both
	// BanMinutes 是该规则的固定处置时长(分钟),对封禁 IP 与禁用账号一视同仁,
	// 0 表示走全局累犯阶梯。
	BanMinutes int `json:"ban_minutes"`
}

// RiskControlSetting 是风控管理的全部可配置项,通过独立 config 快照热更新。
type RiskControlSetting struct {
	Enabled           bool     `json:"enabled"`             // 风控总开关,默认关闭
	UaBlacklist       []string `json:"ua_blacklist"`        // 每条为子串或正则
	UaBlacklistAction string   `json:"ua_blacklist_action"` // "block" | "disable_user"
	IpBlacklist       []string `json:"ip_blacklist"`        // 每条为精确 IP 或 CIDR,命中直接拒绝调用
	ScanMinutes       int      `json:"scan_minutes"`        // 自动封禁扫描周期(分钟),<=0 时回退默认 10
	WhitelistUserIds  []int    `json:"whitelist_user_ids"`  // 完全豁免:不拦截、不处置(仍计入统计)
	// PublicKeyUserIds 是持有公开/共享密钥的账号:滥用者不是账号主人,
	// 因此这些账号永不被自动禁用,但来源 IP 的封禁照常生效,拦截也照常生效。
	PublicKeyUserIds     []int             `json:"public_key_user_ids"`
	AutoBanRules         []RiskAutoBanRule `json:"auto_ban_rules"`
	TinyRequestMaxTokens int               `json:"tiny_request_max_tokens"` // 微量请求(测活)判定:prompt 与 completion tokens 均不超过该值
	EventRetentionDays   int               `json:"event_retention_days"`    // 拦截/告警事件保留天数(封禁与解禁记录永久保留)

	// 自动封禁的累犯升级阶梯,IP 封禁与账号禁用共用。IpBanEscalationMinutes 为逐次
	// 递增的处置时长(例如 [5, 30, 1440] 表示首次 5 分钟、再犯 30 分钟、第三次起 1 天);
	// 为空时回退到 IpBanFirstMinutes / IpBanSecondMinutes 两档,保持旧配置可用。
	// 字段名与配置键沿用 ip_ban_ 前缀:阶梯先只用于 IP,扩展到账号后改键会让
	// 升级前配好的阶梯静默失效,不值得为命名整洁付这个代价。
	IpBanEscalationMinutes []int `json:"ip_ban_escalation_minutes"`
	IpBanFirstMinutes      int   `json:"ip_ban_first_minutes"`     // 旧配置:首次临时封禁时长(分钟)
	IpBanSecondMinutes     int   `json:"ip_ban_second_minutes"`    // 旧配置:第二次及以后的临时封禁时长(分钟)
	IpBanPermanentOffense  int   `json:"ip_ban_permanent_offense"` // 第 N 次违规起永久,0 表示永不升级为永久

	// IpBanIpv6PrefixLength 是自动封禁 IPv6 地址时归并到的前缀长度。
	// 运营商通常给客户端一整段前缀,客户端可在段内自由更换地址(隐私扩展地址甚至每小时一换),
	// 只封 /128 等于没封。128 表示不归并、按单地址封禁。手动封禁不受此设置影响。
	IpBanIpv6PrefixLength int `json:"ip_ban_ipv6_prefix_length"`

	// Probe Guard:请求内实时检测「单 IP 短窗口遍历多个不同模型」的批量测活行为。
	ProbeGuardEnabled       bool   `json:"probe_guard_enabled"`
	ProbeGuardDryRun        bool   `json:"probe_guard_dry_run"`        // 只记告警事件,不实际封禁
	ProbeGuardWindowSeconds int    `json:"probe_guard_window_seconds"` // 滑动窗口(秒)
	ProbeGuardModelCount    int    `json:"probe_guard_model_count"`    // 窗口内不同模型数达到该值即触发
	ProbeGuardAction        string `json:"probe_guard_action"`         // alert | ban_ip | disable_user | ban_both
	ProbeGuardBanMinutes    int    `json:"probe_guard_ban_minutes"`    // 固定封禁时长(分钟),0 表示走全局累犯阶梯

	// Error Guard:响应后实时检测「单 IP 短窗口内高频错误响应」,按状态码筛选。
	// 与 Probe Guard 互补:后者看请求了多少不同模型,前者看被拒绝了多少次。
	ErrorGuardEnabled       bool   `json:"error_guard_enabled"`
	ErrorGuardDryRun        bool   `json:"error_guard_dry_run"`        // 只记告警事件,不实际封禁
	ErrorGuardWindowSeconds int    `json:"error_guard_window_seconds"` // 滑动窗口(秒)
	ErrorGuardThreshold     int    `json:"error_guard_threshold"`      // 窗口内匹配的错误响应数达到该值即触发
	ErrorGuardStatusCodes   []int  `json:"error_guard_status_codes"`   // 只统计这些状态码,为空时用默认列表
	ErrorGuardAction        string `json:"error_guard_action"`         // alert | ban_ip | disable_user | ban_both
	ErrorGuardBanMinutes    int    `json:"error_guard_ban_minutes"`    // 固定封禁时长(分钟),0 表示走全局累犯阶梯
}

const (
	RiskControlSettingPrefix = "risk_control_setting."

	RiskUaActionBlock       = "block"
	RiskUaActionDisableUser = "disable_user"

	RiskMetricIpMultiUser     = "ip_multi_user"
	RiskMetricUserMultiIp     = "user_multi_ip"
	RiskMetricIpMultiToken    = "ip_multi_token"
	RiskMetricUserTinyRequest = "user_tiny_request"
	RiskMetricUserErrorBurst  = "user_error_burst"

	// 风控只处置账号与 IP 两个层次,不涉及单个密钥
	RiskRuleActionAlert       = "alert"        // 仅记录告警事件
	RiskRuleActionDisableUser = "disable_user" // 禁用命中的账号
	RiskRuleActionBanIp       = "ban_ip"       // 封禁来源 IP
	RiskRuleActionBanBoth     = "ban_both"     // 同时封禁来源 IP 与账号

	RiskDefaultWindowHours = 24
	RiskMaxWindowHours     = 24 * 7
	RiskDefaultScanMinutes = 10
	RiskMaxScanMinutes     = 24 * 60

	RiskDefaultTinyRequestMaxTokens = 16
	RiskMaxTinyRequestMaxTokens     = 1024
	RiskDefaultEventRetentionDays   = 30
	RiskMaxEventRetentionDays       = 365

	RiskDefaultIpBanFirstMinutes     = 10
	RiskDefaultIpBanSecondMinutes    = 60
	RiskMaxIpBanMinutes              = 30 * 24 * 60
	RiskDefaultIpBanPermanentOffense = 3
	RiskMaxIpBanPermanentOffense     = 100
	RiskMaxIpBanEscalationSteps      = 10

	// IPv6 归并前缀长度:默认 /64,即运营商分配给单个客户端的最小整段。
	// 下限 32 防止一次封掉整个运营商,128 表示按单地址封禁。
	RiskDefaultIpBanIpv6PrefixLength = 64
	RiskMinIpBanIpv6PrefixLength     = 32
	RiskMaxIpBanIpv6PrefixLength     = 128

	RiskDefaultProbeGuardWindowSeconds = 60
	RiskMinProbeGuardWindowSeconds     = 10
	RiskMaxProbeGuardWindowSeconds     = 3600
	RiskDefaultProbeGuardModelCount    = 5
	RiskMinProbeGuardModelCount        = 2
	RiskMaxProbeGuardModelCount        = 1000

	RiskDefaultErrorGuardWindowSeconds = 60
	RiskMinErrorGuardWindowSeconds     = 10
	RiskMaxErrorGuardWindowSeconds     = 3600
	RiskDefaultErrorGuardThreshold     = 5
	RiskMinErrorGuardThreshold         = 2
	RiskMaxErrorGuardThreshold         = 100000
	RiskMaxErrorGuardStatusCodes       = 20
)

// RiskDefaultErrorGuardStatusCodes 是 Error Guard 未配置状态码时关注的默认集合:
// 400 参数错误、401 密钥无效、403 无权限、404 模型不存在,都是批量测活的典型响应。
// 有意不含 5xx —— 上游故障时的服务端错误不该让正常用户被封。
var RiskDefaultErrorGuardStatusCodes = []int{400, 401, 403, 404}

// IsIpDimensionRiskMetric 判断指标是否按 IP 维度统计。
// IP 维度指标才允许 ban_ip 处置动作。
func IsIpDimensionRiskMetric(metric string) bool {
	return metric == RiskMetricIpMultiUser || metric == RiskMetricIpMultiToken
}

// IsValidRiskMetric 判断字符串是否为已支持的自动封禁指标。
func IsValidRiskMetric(metric string) bool {
	switch metric {
	case RiskMetricIpMultiUser, RiskMetricUserMultiIp, RiskMetricIpMultiToken,
		RiskMetricUserTinyRequest, RiskMetricUserErrorBurst:
		return true
	}
	return false
}

// IsValidRiskRuleAction 判断字符串是否为已支持的处置动作。
func IsValidRiskRuleAction(action string) bool {
	switch action {
	case RiskRuleActionAlert, RiskRuleActionDisableUser,
		RiskRuleActionBanIp, RiskRuleActionBanBoth:
		return true
	}
	return false
}

// RiskActionBansIp 判断动作是否包含封禁来源 IP。
func RiskActionBansIp(action string) bool {
	return action == RiskRuleActionBanIp || action == RiskRuleActionBanBoth
}

// RiskActionDisablesUser 判断动作是否包含禁用账号。
func RiskActionDisablesUser(action string) bool {
	return action == RiskRuleActionDisableUser || action == RiskRuleActionBanBoth
}

// ValidateRiskControlOption 校验单个风控配置项。
// 通用设置接口会逐项保存配置，因此必须在落库前拒绝非法值，避免数据库、
// OptionMap 与请求使用的配置快照出现不一致。
func ValidateRiskControlOption(key string, value string) error {
	if !strings.HasPrefix(key, RiskControlSettingPrefix) {
		return nil
	}

	field := strings.TrimPrefix(key, RiskControlSettingPrefix)
	switch field {
	case "enabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("风控总开关必须为 true 或 false")
		}
	case "ua_blacklist":
		var entries []string
		if err := common.UnmarshalJsonStr(value, &entries); err != nil || entries == nil {
			return fmt.Errorf("UA 黑名单必须是字符串数组")
		}
		for index, entry := range entries {
			if strings.TrimSpace(entry) == "" {
				return fmt.Errorf("UA 黑名单第 %d 项不能为空", index+1)
			}
		}
	case "ua_blacklist_action":
		if value != RiskUaActionBlock && value != RiskUaActionDisableUser {
			return fmt.Errorf("UA 黑名单动作必须为 block 或 disable_user")
		}
	case "ip_blacklist":
		var entries []string
		if err := common.UnmarshalJsonStr(value, &entries); err != nil || entries == nil {
			return fmt.Errorf("IP 黑名单必须是字符串数组")
		}
		for index, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				return fmt.Errorf("IP 黑名单第 %d 项不能为空", index+1)
			}
			if net.ParseIP(entry) == nil {
				if _, _, err := net.ParseCIDR(entry); err != nil {
					return fmt.Errorf("IP 黑名单第 %d 项不是有效的 IP 或 CIDR", index+1)
				}
			}
		}
	case "scan_minutes":
		scanMinutes, err := strconv.Atoi(value)
		if err != nil || scanMinutes < 1 || scanMinutes > RiskMaxScanMinutes {
			return fmt.Errorf("自动扫描周期必须为 1 到 %d 分钟", RiskMaxScanMinutes)
		}
	case "whitelist_user_ids":
		var userIds []int
		if err := common.UnmarshalJsonStr(value, &userIds); err != nil || userIds == nil {
			return fmt.Errorf("风控白名单必须是用户 ID 数组")
		}
		for index, userId := range userIds {
			if userId <= 0 {
				return fmt.Errorf("风控白名单第 %d 项必须是正整数", index+1)
			}
		}
	case "public_key_user_ids":
		var userIds []int
		if err := common.UnmarshalJsonStr(value, &userIds); err != nil || userIds == nil {
			return fmt.Errorf("公开密钥账号必须是用户 ID 数组")
		}
		for index, userId := range userIds {
			if userId <= 0 {
				return fmt.Errorf("公开密钥账号第 %d 项必须是正整数", index+1)
			}
		}
	case "tiny_request_max_tokens":
		maxTokens, err := strconv.Atoi(value)
		if err != nil || maxTokens < 1 || maxTokens > RiskMaxTinyRequestMaxTokens {
			return fmt.Errorf("微量请求判定阈值必须为 1 到 %d", RiskMaxTinyRequestMaxTokens)
		}
	case "event_retention_days":
		days, err := strconv.Atoi(value)
		if err != nil || days < 1 || days > RiskMaxEventRetentionDays {
			return fmt.Errorf("风控事件保留天数必须为 1 到 %d", RiskMaxEventRetentionDays)
		}
	case "ip_ban_first_minutes", "ip_ban_second_minutes":
		minutes, err := strconv.Atoi(value)
		if err != nil || minutes < 1 || minutes > RiskMaxIpBanMinutes {
			return fmt.Errorf("临时封禁时长必须为 1 到 %d 分钟", RiskMaxIpBanMinutes)
		}
	case "ip_ban_escalation_minutes":
		var steps []int
		if err := common.UnmarshalJsonStr(value, &steps); err != nil || steps == nil {
			return fmt.Errorf("封禁升级阶梯必须是分钟数数组")
		}
		if len(steps) > RiskMaxIpBanEscalationSteps {
			return fmt.Errorf("封禁升级阶梯最多 %d 级", RiskMaxIpBanEscalationSteps)
		}
		for index, minutes := range steps {
			if minutes < 1 || minutes > RiskMaxIpBanMinutes {
				return fmt.Errorf("封禁升级阶梯第 %d 级必须为 1 到 %d 分钟", index+1, RiskMaxIpBanMinutes)
			}
		}
	case "ip_ban_permanent_offense":
		count, err := strconv.Atoi(value)
		if err != nil || count < 0 || count > RiskMaxIpBanPermanentOffense {
			return fmt.Errorf("永久封禁触发次数必须为 0 到 %d(0 表示永不永久封禁)", RiskMaxIpBanPermanentOffense)
		}
	case "ip_ban_ipv6_prefix_length":
		length, err := strconv.Atoi(value)
		if err != nil || length < RiskMinIpBanIpv6PrefixLength || length > RiskMaxIpBanIpv6PrefixLength {
			return fmt.Errorf("IPv6 封禁前缀长度必须为 %d 到 %d(%d 表示按单地址封禁)",
				RiskMinIpBanIpv6PrefixLength, RiskMaxIpBanIpv6PrefixLength, RiskMaxIpBanIpv6PrefixLength)
		}
	case "probe_guard_enabled", "probe_guard_dry_run":
		if value != "true" && value != "false" {
			return fmt.Errorf("Probe Guard 开关必须为 true 或 false")
		}
	case "probe_guard_window_seconds":
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < RiskMinProbeGuardWindowSeconds || seconds > RiskMaxProbeGuardWindowSeconds {
			return fmt.Errorf("Probe Guard 窗口必须为 %d 到 %d 秒", RiskMinProbeGuardWindowSeconds, RiskMaxProbeGuardWindowSeconds)
		}
	case "probe_guard_model_count":
		count, err := strconv.Atoi(value)
		if err != nil || count < RiskMinProbeGuardModelCount || count > RiskMaxProbeGuardModelCount {
			return fmt.Errorf("Probe Guard 模型数阈值必须为 %d 到 %d", RiskMinProbeGuardModelCount, RiskMaxProbeGuardModelCount)
		}
	case "error_guard_enabled", "error_guard_dry_run":
		if value != "true" && value != "false" {
			return fmt.Errorf("Error Guard 开关必须为 true 或 false")
		}
	case "error_guard_window_seconds":
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < RiskMinErrorGuardWindowSeconds || seconds > RiskMaxErrorGuardWindowSeconds {
			return fmt.Errorf("Error Guard 窗口必须为 %d 到 %d 秒", RiskMinErrorGuardWindowSeconds, RiskMaxErrorGuardWindowSeconds)
		}
	case "error_guard_threshold":
		count, err := strconv.Atoi(value)
		if err != nil || count < RiskMinErrorGuardThreshold || count > RiskMaxErrorGuardThreshold {
			return fmt.Errorf("Error Guard 错误次数阈值必须为 %d 到 %d", RiskMinErrorGuardThreshold, RiskMaxErrorGuardThreshold)
		}
	case "probe_guard_action", "error_guard_action":
		if !IsValidRiskRuleAction(value) {
			return fmt.Errorf("处置动作必须为 alert、ban_ip、disable_user 或 ban_both")
		}
	case "probe_guard_ban_minutes", "error_guard_ban_minutes":
		minutes, err := strconv.Atoi(value)
		if err != nil || minutes < 0 || minutes > RiskMaxIpBanMinutes {
			return fmt.Errorf("固定封禁时长必须为 0 到 %d 分钟(0 表示走升级阶梯)", RiskMaxIpBanMinutes)
		}
	case "error_guard_status_codes":
		var codes []int
		if err := common.UnmarshalJsonStr(value, &codes); err != nil || codes == nil {
			return fmt.Errorf("Error Guard 状态码必须是数字数组")
		}
		if len(codes) > RiskMaxErrorGuardStatusCodes {
			return fmt.Errorf("Error Guard 状态码最多 %d 个", RiskMaxErrorGuardStatusCodes)
		}
		for index, code := range codes {
			if code < 400 || code > 599 {
				return fmt.Errorf("Error Guard 状态码第 %d 项必须为 400 到 599", index+1)
			}
		}
	case "auto_ban_rules":
		var rules []RiskAutoBanRule
		if err := common.UnmarshalJsonStr(value, &rules); err != nil || rules == nil {
			return fmt.Errorf("自动封禁规则必须是规则数组")
		}
		for index, rule := range rules {
			if !IsValidRiskMetric(rule.Metric) {
				return fmt.Errorf("自动封禁规则第 %d 项指标无效", index+1)
			}
			if rule.WindowHours < 1 || rule.WindowHours > RiskMaxWindowHours {
				return fmt.Errorf("自动封禁规则第 %d 项统计窗口必须为 1 到 %d 小时", index+1, RiskMaxWindowHours)
			}
			if rule.Threshold < 1 {
				return fmt.Errorf("自动封禁规则第 %d 项阈值必须是正整数", index+1)
			}
			if !IsValidRiskRuleAction(rule.Action) {
				return fmt.Errorf("自动封禁规则第 %d 项动作无效", index+1)
			}
			// 用户维度指标没有单一来源 IP,封 IP 会牵连该窗口内的无关地址
			if RiskActionBansIp(rule.Action) && !IsIpDimensionRiskMetric(rule.Metric) {
				return fmt.Errorf("自动封禁规则第 %d 项:封禁 IP 动作仅支持 IP 维度指标", index+1)
			}
			if rule.BanMinutes < 0 || rule.BanMinutes > RiskMaxIpBanMinutes {
				return fmt.Errorf("自动封禁规则第 %d 项封禁时长必须为 0 到 %d 分钟(0 表示走升级阶梯)", index+1, RiskMaxIpBanMinutes)
			}
		}
	default:
		return fmt.Errorf("未知的风控配置项: %s", field)
	}
	return nil
}

// riskControlSetting 仅作为配置框架(基于反射、无同步)就地修改的暂存区。
// 请求 goroutine 绝不能直接读取它,而应读取下方发布的不可变快照。
var riskControlSetting = RiskControlSetting{
	Enabled:              false,
	UaBlacklist:          []string{},
	UaBlacklistAction:    RiskUaActionBlock,
	IpBlacklist:          []string{},
	ScanMinutes:          RiskDefaultScanMinutes,
	WhitelistUserIds:     []int{},
	PublicKeyUserIds:     []int{},
	AutoBanRules:         []RiskAutoBanRule{},
	TinyRequestMaxTokens: RiskDefaultTinyRequestMaxTokens,
	EventRetentionDays:   RiskDefaultEventRetentionDays,

	IpBanEscalationMinutes: []int{},
	IpBanFirstMinutes:      RiskDefaultIpBanFirstMinutes,
	IpBanSecondMinutes:     RiskDefaultIpBanSecondMinutes,
	IpBanPermanentOffense:  RiskDefaultIpBanPermanentOffense,
	IpBanIpv6PrefixLength:  RiskDefaultIpBanIpv6PrefixLength,

	ProbeGuardEnabled:       false,
	ProbeGuardDryRun:        true,
	ProbeGuardWindowSeconds: RiskDefaultProbeGuardWindowSeconds,
	ProbeGuardModelCount:    RiskDefaultProbeGuardModelCount,
	ProbeGuardAction:        RiskRuleActionBanIp,
	ProbeGuardBanMinutes:    0,

	ErrorGuardEnabled:       false,
	ErrorGuardDryRun:        true,
	ErrorGuardWindowSeconds: RiskDefaultErrorGuardWindowSeconds,
	ErrorGuardThreshold:     RiskDefaultErrorGuardThreshold,
	ErrorGuardStatusCodes:   []int{},
	ErrorGuardAction:        RiskRuleActionBanIp,
	ErrorGuardBanMinutes:    0,
}

// ResolvedIpBanFirstMinutes 返回生效的首次临时封禁时长(分钟)。
func (s *RiskControlSetting) ResolvedIpBanFirstMinutes() int {
	if s == nil || s.IpBanFirstMinutes < 1 || s.IpBanFirstMinutes > RiskMaxIpBanMinutes {
		return RiskDefaultIpBanFirstMinutes
	}
	return s.IpBanFirstMinutes
}

// ResolvedIpBanSecondMinutes 返回生效的再犯临时封禁时长(分钟)。
func (s *RiskControlSetting) ResolvedIpBanSecondMinutes() int {
	if s == nil || s.IpBanSecondMinutes < 1 || s.IpBanSecondMinutes > RiskMaxIpBanMinutes {
		return RiskDefaultIpBanSecondMinutes
	}
	return s.IpBanSecondMinutes
}

// ResolvedBanPermanentOffense 返回生效的永久封禁触发次数,0 表示永不升级为永久。
// 对 IP 封禁与账号禁用同样生效。
func (s *RiskControlSetting) ResolvedBanPermanentOffense() int {
	if s == nil || s.IpBanPermanentOffense < 0 || s.IpBanPermanentOffense > RiskMaxIpBanPermanentOffense {
		return RiskDefaultIpBanPermanentOffense
	}
	return s.IpBanPermanentOffense
}

// ResolvedBanEscalationMinutes 返回生效的累犯处置阶梯(分钟),IP 封禁与账号禁用共用。
// 优先使用显式配置的阶梯;未配置时回退到旧的首次/再犯两档,
// 因此升级前配置好的 ip_ban_first_minutes / ip_ban_second_minutes 继续有效。
// 违规次数超出阶梯长度时停在最后一档(除非达到永久封禁次数)。
func (s *RiskControlSetting) ResolvedBanEscalationMinutes() []int {
	if s != nil && len(s.IpBanEscalationMinutes) > 0 {
		steps := make([]int, 0, len(s.IpBanEscalationMinutes))
		for _, minutes := range s.IpBanEscalationMinutes {
			if minutes >= 1 && minutes <= RiskMaxIpBanMinutes {
				steps = append(steps, minutes)
			}
		}
		if len(steps) > 0 {
			return steps
		}
	}
	return []int{s.ResolvedIpBanFirstMinutes(), s.ResolvedIpBanSecondMinutes()}
}

// ResolvedIpBanIpv6PrefixLength 返回自动封禁 IPv6 地址时归并到的前缀长度。
func (s *RiskControlSetting) ResolvedIpBanIpv6PrefixLength() int {
	if s == nil || s.IpBanIpv6PrefixLength < RiskMinIpBanIpv6PrefixLength || s.IpBanIpv6PrefixLength > RiskMaxIpBanIpv6PrefixLength {
		return RiskDefaultIpBanIpv6PrefixLength
	}
	return s.IpBanIpv6PrefixLength
}

// ResolvedErrorGuardWindowSeconds 返回生效的 Error Guard 滑动窗口(秒)。
func (s *RiskControlSetting) ResolvedErrorGuardWindowSeconds() int {
	if s == nil || s.ErrorGuardWindowSeconds < RiskMinErrorGuardWindowSeconds || s.ErrorGuardWindowSeconds > RiskMaxErrorGuardWindowSeconds {
		return RiskDefaultErrorGuardWindowSeconds
	}
	return s.ErrorGuardWindowSeconds
}

// ResolvedErrorGuardThreshold 返回生效的 Error Guard 错误次数阈值。
func (s *RiskControlSetting) ResolvedErrorGuardThreshold() int {
	if s == nil || s.ErrorGuardThreshold < RiskMinErrorGuardThreshold || s.ErrorGuardThreshold > RiskMaxErrorGuardThreshold {
		return RiskDefaultErrorGuardThreshold
	}
	return s.ErrorGuardThreshold
}

// ResolvedProbeGuardAction 返回生效的 Probe Guard 处置动作。
// 默认封禁来源 IP 而不动账号:测活者通常持他人泄露的密钥,封账号会误伤密钥主人。
func (s *RiskControlSetting) ResolvedProbeGuardAction() string {
	if s == nil || !IsValidRiskRuleAction(s.ProbeGuardAction) {
		return RiskRuleActionBanIp
	}
	return s.ProbeGuardAction
}

// ResolvedErrorGuardAction 返回生效的 Error Guard 处置动作。
func (s *RiskControlSetting) ResolvedErrorGuardAction() string {
	if s == nil || !IsValidRiskRuleAction(s.ErrorGuardAction) {
		return RiskRuleActionBanIp
	}
	return s.ErrorGuardAction
}

// ResolvedProbeGuardBanMinutes 返回 Probe Guard 的固定封禁时长,0 表示走累犯阶梯。
func (s *RiskControlSetting) ResolvedProbeGuardBanMinutes() int {
	if s == nil || s.ProbeGuardBanMinutes < 0 || s.ProbeGuardBanMinutes > RiskMaxIpBanMinutes {
		return 0
	}
	return s.ProbeGuardBanMinutes
}

// ResolvedErrorGuardBanMinutes 返回 Error Guard 的固定封禁时长,0 表示走累犯阶梯。
func (s *RiskControlSetting) ResolvedErrorGuardBanMinutes() int {
	if s == nil || s.ErrorGuardBanMinutes < 0 || s.ErrorGuardBanMinutes > RiskMaxIpBanMinutes {
		return 0
	}
	return s.ErrorGuardBanMinutes
}

// ResolvedErrorGuardStatusCodes 返回生效的关注状态码。
// 未配置时用默认集合而非"全部错误",避免上游 5xx 故障期间误封正常用户。
func (s *RiskControlSetting) ResolvedErrorGuardStatusCodes() []int {
	if s == nil || len(s.ErrorGuardStatusCodes) == 0 {
		return RiskDefaultErrorGuardStatusCodes
	}
	codes := make([]int, 0, len(s.ErrorGuardStatusCodes))
	for _, code := range s.ErrorGuardStatusCodes {
		if code >= 400 && code <= 599 {
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return RiskDefaultErrorGuardStatusCodes
	}
	return codes
}

// ResolvedProbeGuardWindowSeconds 返回生效的 Probe Guard 滑动窗口(秒)。
func (s *RiskControlSetting) ResolvedProbeGuardWindowSeconds() int {
	if s == nil || s.ProbeGuardWindowSeconds < RiskMinProbeGuardWindowSeconds || s.ProbeGuardWindowSeconds > RiskMaxProbeGuardWindowSeconds {
		return RiskDefaultProbeGuardWindowSeconds
	}
	return s.ProbeGuardWindowSeconds
}

// ResolvedProbeGuardModelCount 返回生效的 Probe Guard 不同模型数阈值。
func (s *RiskControlSetting) ResolvedProbeGuardModelCount() int {
	if s == nil || s.ProbeGuardModelCount < RiskMinProbeGuardModelCount || s.ProbeGuardModelCount > RiskMaxProbeGuardModelCount {
		return RiskDefaultProbeGuardModelCount
	}
	return s.ProbeGuardModelCount
}

// ResolvedTinyRequestMaxTokens 返回生效的微量请求判定阈值,未配置或越界时回退默认值。
func (s *RiskControlSetting) ResolvedTinyRequestMaxTokens() int {
	if s == nil || s.TinyRequestMaxTokens < 1 || s.TinyRequestMaxTokens > RiskMaxTinyRequestMaxTokens {
		return RiskDefaultTinyRequestMaxTokens
	}
	return s.TinyRequestMaxTokens
}

// ResolvedEventRetentionDays 返回生效的风控事件保留天数,未配置或越界时回退默认值。
func (s *RiskControlSetting) ResolvedEventRetentionDays() int {
	if s == nil || s.EventRetentionDays < 1 || s.EventRetentionDays > RiskMaxEventRetentionDays {
		return RiskDefaultEventRetentionDays
	}
	return s.EventRetentionDays
}

var riskControlSnapshot atomic.Pointer[RiskControlSetting]

func init() {
	config.GlobalConfig.Register("risk_control_setting", &riskControlSetting)
	SyncRiskControlSetting()
}

// GetRiskControlSetting 返回只读快照,可被任意请求 goroutine 安全并发读取。
func GetRiskControlSetting() *RiskControlSetting {
	return riskControlSnapshot.Load()
}

// SyncRiskControlSetting 将暂存配置深拷贝后发布为只读快照。
// 每次更新暂存结构后都必须调用(见 model/option.go 的 handleConfigUpdate)。
func SyncRiskControlSetting() {
	snapshot := &RiskControlSetting{}
	data, err := common.Marshal(riskControlSetting)
	if err != nil || common.Unmarshal(data, snapshot) != nil {
		snapshot = &RiskControlSetting{Enabled: riskControlSetting.Enabled}
	}
	riskControlSnapshot.Store(snapshot)
}

// SetRiskControlSettingForTest 直接发布一个快照,仅供测试使用。
func SetRiskControlSettingForTest(setting *RiskControlSetting) {
	if setting == nil {
		setting = &RiskControlSetting{}
	}
	riskControlSnapshot.Store(setting)
}
