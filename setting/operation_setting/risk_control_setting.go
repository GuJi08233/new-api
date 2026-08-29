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
	Action      string `json:"action"`       // "alert" | "disable_user"
}

// RiskControlSetting 是风控管理的全部可配置项,通过独立 config 快照热更新。
type RiskControlSetting struct {
	Enabled              bool              `json:"enabled"`             // 风控总开关,默认关闭
	UaBlacklist          []string          `json:"ua_blacklist"`        // 每条为子串或正则
	UaBlacklistAction    string            `json:"ua_blacklist_action"` // "block" | "disable_user"
	IpBlacklist          []string          `json:"ip_blacklist"`        // 每条为精确 IP 或 CIDR,命中直接拒绝调用
	ScanMinutes          int               `json:"scan_minutes"`        // 自动封禁扫描周期(分钟),<=0 时回退默认 10
	WhitelistUserIds     []int             `json:"whitelist_user_ids"`  // 永不被自动处置的用户
	AutoBanRules         []RiskAutoBanRule `json:"auto_ban_rules"`
	TinyRequestMaxTokens int               `json:"tiny_request_max_tokens"` // 微量请求(测活)判定:prompt 与 completion tokens 均不超过该值
	EventRetentionDays   int               `json:"event_retention_days"`    // 拦截/告警事件保留天数(封禁与解禁记录永久保留)

	// 自动 IP 封禁的累犯升级阶梯:首次临时封禁 → 再犯加时 → 达到次数后永久。
	IpBanFirstMinutes     int `json:"ip_ban_first_minutes"`     // 首次临时封禁时长(分钟)
	IpBanSecondMinutes    int `json:"ip_ban_second_minutes"`    // 第二次及以后的临时封禁时长(分钟)
	IpBanPermanentOffense int `json:"ip_ban_permanent_offense"` // 第 N 次违规起永久封禁

	// Probe Guard:请求内实时检测「单 IP 短窗口遍历多个不同模型」的批量测活行为。
	ProbeGuardEnabled       bool `json:"probe_guard_enabled"`
	ProbeGuardDryRun        bool `json:"probe_guard_dry_run"`        // 只记告警事件,不实际封禁
	ProbeGuardWindowSeconds int  `json:"probe_guard_window_seconds"` // 滑动窗口(秒)
	ProbeGuardModelCount    int  `json:"probe_guard_model_count"`    // 窗口内不同模型数达到该值即触发
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

	RiskRuleActionAlert       = "alert"
	RiskRuleActionDisableUser = "disable_user"
	RiskRuleActionBanIp       = "ban_ip"

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

	RiskDefaultProbeGuardWindowSeconds = 60
	RiskMinProbeGuardWindowSeconds     = 10
	RiskMaxProbeGuardWindowSeconds     = 3600
	RiskDefaultProbeGuardModelCount    = 5
	RiskMinProbeGuardModelCount        = 2
	RiskMaxProbeGuardModelCount        = 1000
)

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
	case "ip_ban_permanent_offense":
		count, err := strconv.Atoi(value)
		if err != nil || count < 1 || count > RiskMaxIpBanPermanentOffense {
			return fmt.Errorf("永久封禁触发次数必须为 1 到 %d", RiskMaxIpBanPermanentOffense)
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
			switch rule.Action {
			case RiskRuleActionAlert, RiskRuleActionDisableUser:
			case RiskRuleActionBanIp:
				if !IsIpDimensionRiskMetric(rule.Metric) {
					return fmt.Errorf("自动封禁规则第 %d 项:封禁 IP 动作仅支持 IP 维度指标", index+1)
				}
			default:
				return fmt.Errorf("自动封禁规则第 %d 项动作无效", index+1)
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
	AutoBanRules:         []RiskAutoBanRule{},
	TinyRequestMaxTokens: RiskDefaultTinyRequestMaxTokens,
	EventRetentionDays:   RiskDefaultEventRetentionDays,

	IpBanFirstMinutes:     RiskDefaultIpBanFirstMinutes,
	IpBanSecondMinutes:    RiskDefaultIpBanSecondMinutes,
	IpBanPermanentOffense: RiskDefaultIpBanPermanentOffense,

	ProbeGuardEnabled:       false,
	ProbeGuardDryRun:        true,
	ProbeGuardWindowSeconds: RiskDefaultProbeGuardWindowSeconds,
	ProbeGuardModelCount:    RiskDefaultProbeGuardModelCount,
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

// ResolvedIpBanPermanentOffense 返回生效的永久封禁触发次数。
func (s *RiskControlSetting) ResolvedIpBanPermanentOffense() int {
	if s == nil || s.IpBanPermanentOffense < 1 || s.IpBanPermanentOffense > RiskMaxIpBanPermanentOffense {
		return RiskDefaultIpBanPermanentOffense
	}
	return s.IpBanPermanentOffense
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
