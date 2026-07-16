package operation_setting

import (
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
	RiskUaActionBlock       = "block"
	RiskUaActionDisableUser = "disable_user"

	RiskMetricIpMultiUser = "ip_multi_user"
	RiskMetricUserMultiIp = "user_multi_ip"

	RiskRuleActionAlert       = "alert"
	RiskRuleActionDisableUser = "disable_user"

	RiskDefaultWindowHours = 24
	RiskDefaultScanMinutes = 10
)

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
