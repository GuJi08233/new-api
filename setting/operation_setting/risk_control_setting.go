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

// RiskAutoBanRule 定义一条基于 IP 滥用指标的自动封禁规则。
type RiskAutoBanRule struct {
	Enabled     bool   `json:"enabled"`
	Metric      string `json:"metric"`       // "ip_multi_user" | "user_multi_ip"
	WindowHours int    `json:"window_hours"` // 统计窗口(小时),<=0 时回退默认 24
	Threshold   int    `json:"threshold"`    // 指标严格大于该值时触发
	Action      string `json:"action"`       // "alert" | "disable_user"
}

// RiskControlSetting 是风控管理的全部可配置项,通过独立 config 快照热更新。
type RiskControlSetting struct {
	Enabled           bool              `json:"enabled"`             // 风控总开关,默认关闭
	UaBlacklist       []string          `json:"ua_blacklist"`        // 每条为子串或正则
	UaBlacklistAction string            `json:"ua_blacklist_action"` // "block" | "disable_user"
	IpBlacklist       []string          `json:"ip_blacklist"`        // 每条为精确 IP 或 CIDR,命中直接拒绝调用
	ScanMinutes       int               `json:"scan_minutes"`        // 自动封禁扫描周期(分钟),<=0 时回退默认 10
	WhitelistUserIds  []int             `json:"whitelist_user_ids"`  // 永不被自动处置的用户
	AutoBanRules      []RiskAutoBanRule `json:"auto_ban_rules"`
}

const (
	RiskControlSettingPrefix = "risk_control_setting."

	RiskUaActionBlock       = "block"
	RiskUaActionDisableUser = "disable_user"

	RiskMetricIpMultiUser = "ip_multi_user"
	RiskMetricUserMultiIp = "user_multi_ip"

	RiskRuleActionAlert       = "alert"
	RiskRuleActionDisableUser = "disable_user"

	RiskDefaultWindowHours = 24
	RiskMaxWindowHours     = 24 * 7
	RiskDefaultScanMinutes = 10
	RiskMaxScanMinutes     = 24 * 60
)

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
	case "auto_ban_rules":
		var rules []RiskAutoBanRule
		if err := common.UnmarshalJsonStr(value, &rules); err != nil || rules == nil {
			return fmt.Errorf("自动封禁规则必须是规则数组")
		}
		for index, rule := range rules {
			if rule.Metric != RiskMetricIpMultiUser && rule.Metric != RiskMetricUserMultiIp {
				return fmt.Errorf("自动封禁规则第 %d 项指标无效", index+1)
			}
			if rule.WindowHours < 1 || rule.WindowHours > RiskMaxWindowHours {
				return fmt.Errorf("自动封禁规则第 %d 项统计窗口必须为 1 到 %d 小时", index+1, RiskMaxWindowHours)
			}
			if rule.Threshold < 1 {
				return fmt.Errorf("自动封禁规则第 %d 项阈值必须是正整数", index+1)
			}
			if rule.Action != RiskRuleActionAlert && rule.Action != RiskRuleActionDisableUser {
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
	Enabled:           false,
	UaBlacklist:       []string{},
	UaBlacklistAction: RiskUaActionBlock,
	IpBlacklist:       []string{},
	ScanMinutes:       RiskDefaultScanMinutes,
	WhitelistUserIds:  []int{},
	AutoBanRules:      []RiskAutoBanRule{},
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
