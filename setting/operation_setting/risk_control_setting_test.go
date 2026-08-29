package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRiskControlOptionAcceptsValidValues(t *testing.T) {
	t.Parallel()

	validOptions := map[string]string{
		RiskControlSettingPrefix + "enabled":                    "true",
		RiskControlSettingPrefix + "ua_blacklist":               `["curl","^Go-http-client"]`,
		RiskControlSettingPrefix + "ua_blacklist_action":        RiskUaActionDisableUser,
		RiskControlSettingPrefix + "ip_blacklist":               `["192.0.2.1","10.0.0.0/8","2001:db8::/32"]`,
		RiskControlSettingPrefix + "scan_minutes":               "10",
		RiskControlSettingPrefix + "whitelist_user_ids":         `[1,42]`,
		RiskControlSettingPrefix + "auto_ban_rules":             `[{"enabled":true,"metric":"ip_multi_user","window_hours":24,"threshold":3,"action":"disable_user"},{"enabled":false,"metric":"user_multi_ip","window_hours":168,"threshold":1,"action":"alert"},{"enabled":true,"metric":"ip_multi_token","window_hours":24,"threshold":10,"action":"alert"},{"enabled":true,"metric":"user_tiny_request","window_hours":24,"threshold":200,"action":"alert"},{"enabled":true,"metric":"user_error_burst","window_hours":24,"threshold":100,"action":"disable_user"},{"enabled":true,"metric":"ip_multi_token","window_hours":24,"threshold":10,"action":"ban_ip"}]`,
		RiskControlSettingPrefix + "tiny_request_max_tokens":    "16",
		RiskControlSettingPrefix + "event_retention_days":       "30",
		RiskControlSettingPrefix + "ip_ban_first_minutes":       "10",
		RiskControlSettingPrefix + "ip_ban_second_minutes":      "60",
		RiskControlSettingPrefix + "ip_ban_permanent_offense":   "3",
		RiskControlSettingPrefix + "probe_guard_enabled":        "true",
		RiskControlSettingPrefix + "probe_guard_dry_run":        "false",
		RiskControlSettingPrefix + "probe_guard_window_seconds": "60",
		RiskControlSettingPrefix + "probe_guard_model_count":    "5",
		"unrelated_setting.enabled":                             "not-a-bool",
	}

	for key, value := range validOptions {
		key, value := key, value
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidateRiskControlOption(key, value))
		})
	}
}

func TestValidateRiskControlOptionRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		value   string
		message string
	}{
		{name: "invalid bool", key: "enabled", value: "1", message: "true 或 false"},
		{name: "UA blacklist must be array", key: "ua_blacklist", value: `"curl"`, message: "字符串数组"},
		{name: "UA blacklist rejects empty entry", key: "ua_blacklist", value: `["curl",""]`, message: "不能为空"},
		{name: "invalid UA action", key: "ua_blacklist_action", value: "ban", message: "动作"},
		{name: "IP blacklist must be array", key: "ip_blacklist", value: `{}`, message: "字符串数组"},
		{name: "invalid IP entry", key: "ip_blacklist", value: `["not-an-ip"]`, message: "IP 或 CIDR"},
		{name: "scan interval below minimum", key: "scan_minutes", value: "0", message: "1 到 1440"},
		{name: "scan interval above maximum", key: "scan_minutes", value: "1441", message: "1 到 1440"},
		{name: "scan interval is not integer", key: "scan_minutes", value: "1.5", message: "1 到 1440"},
		{name: "whitelist must be array", key: "whitelist_user_ids", value: `null`, message: "用户 ID 数组"},
		{name: "invalid whitelist user", key: "whitelist_user_ids", value: `[0]`, message: "正整数"},
		{name: "rules must be array", key: "auto_ban_rules", value: `{}`, message: "规则数组"},
		{name: "invalid rule metric", key: "auto_ban_rules", value: `[{"metric":"ua","window_hours":24,"threshold":1,"action":"alert"}]`, message: "指标无效"},
		{name: "rule window below minimum", key: "auto_ban_rules", value: `[{"metric":"ip_multi_user","window_hours":0,"threshold":1,"action":"alert"}]`, message: "统计窗口"},
		{name: "rule window above maximum", key: "auto_ban_rules", value: `[{"metric":"ip_multi_user","window_hours":169,"threshold":1,"action":"alert"}]`, message: "统计窗口"},
		{name: "invalid rule threshold", key: "auto_ban_rules", value: `[{"metric":"ip_multi_user","window_hours":24,"threshold":0,"action":"alert"}]`, message: "阈值"},
		{name: "invalid rule action", key: "auto_ban_rules", value: `[{"metric":"ip_multi_user","window_hours":24,"threshold":1,"action":"block"}]`, message: "动作无效"},
		{name: "tiny threshold below minimum", key: "tiny_request_max_tokens", value: "0", message: "1 到 1024"},
		{name: "tiny threshold above maximum", key: "tiny_request_max_tokens", value: "1025", message: "1 到 1024"},
		{name: "tiny threshold is not integer", key: "tiny_request_max_tokens", value: "abc", message: "1 到 1024"},
		{name: "retention below minimum", key: "event_retention_days", value: "0", message: "1 到 365"},
		{name: "retention above maximum", key: "event_retention_days", value: "366", message: "1 到 365"},
		{name: "ban_ip rejected for user metric", key: "auto_ban_rules", value: `[{"metric":"user_multi_ip","window_hours":24,"threshold":3,"action":"ban_ip"}]`, message: "仅支持 IP 维度指标"},
		{name: "ip ban minutes below minimum", key: "ip_ban_first_minutes", value: "0", message: "临时封禁时长"},
		{name: "ip ban minutes above maximum", key: "ip_ban_second_minutes", value: "43201", message: "临时封禁时长"},
		{name: "permanent offense below minimum", key: "ip_ban_permanent_offense", value: "0", message: "永久封禁触发次数"},
		{name: "probe guard switch invalid", key: "probe_guard_enabled", value: "yes", message: "true 或 false"},
		{name: "probe guard window below minimum", key: "probe_guard_window_seconds", value: "9", message: "10 到 3600"},
		{name: "probe guard model count below minimum", key: "probe_guard_model_count", value: "1", message: "2 到 1000"},
		{name: "unknown risk setting", key: "unknown", value: "value", message: "未知的风控配置项"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRiskControlOption(RiskControlSettingPrefix+test.key, test.value)
			require.Error(t, err)
			assert.ErrorContains(t, err, test.message)
		})
	}
}

func TestRiskControlResolvedFallbacks(t *testing.T) {
	t.Parallel()

	var nilSetting *RiskControlSetting
	assert.Equal(t, RiskDefaultTinyRequestMaxTokens, nilSetting.ResolvedTinyRequestMaxTokens())
	assert.Equal(t, RiskDefaultEventRetentionDays, nilSetting.ResolvedEventRetentionDays())

	zero := &RiskControlSetting{}
	assert.Equal(t, RiskDefaultTinyRequestMaxTokens, zero.ResolvedTinyRequestMaxTokens())
	assert.Equal(t, RiskDefaultEventRetentionDays, zero.ResolvedEventRetentionDays())

	outOfRange := &RiskControlSetting{TinyRequestMaxTokens: RiskMaxTinyRequestMaxTokens + 1, EventRetentionDays: RiskMaxEventRetentionDays + 1}
	assert.Equal(t, RiskDefaultTinyRequestMaxTokens, outOfRange.ResolvedTinyRequestMaxTokens())
	assert.Equal(t, RiskDefaultEventRetentionDays, outOfRange.ResolvedEventRetentionDays())

	configured := &RiskControlSetting{TinyRequestMaxTokens: 64, EventRetentionDays: 90}
	assert.Equal(t, 64, configured.ResolvedTinyRequestMaxTokens())
	assert.Equal(t, 90, configured.ResolvedEventRetentionDays())
}

func TestRiskControlIpBanAndProbeGuardFallbacks(t *testing.T) {
	t.Parallel()

	var nilSetting *RiskControlSetting
	assert.Equal(t, RiskDefaultIpBanFirstMinutes, nilSetting.ResolvedIpBanFirstMinutes())
	assert.Equal(t, RiskDefaultIpBanSecondMinutes, nilSetting.ResolvedIpBanSecondMinutes())
	assert.Equal(t, RiskDefaultIpBanPermanentOffense, nilSetting.ResolvedIpBanPermanentOffense())
	assert.Equal(t, RiskDefaultProbeGuardWindowSeconds, nilSetting.ResolvedProbeGuardWindowSeconds())
	assert.Equal(t, RiskDefaultProbeGuardModelCount, nilSetting.ResolvedProbeGuardModelCount())

	configured := &RiskControlSetting{
		IpBanFirstMinutes:       30,
		IpBanSecondMinutes:      360,
		IpBanPermanentOffense:   5,
		ProbeGuardWindowSeconds: 120,
		ProbeGuardModelCount:    8,
	}
	assert.Equal(t, 30, configured.ResolvedIpBanFirstMinutes())
	assert.Equal(t, 360, configured.ResolvedIpBanSecondMinutes())
	assert.Equal(t, 5, configured.ResolvedIpBanPermanentOffense())
	assert.Equal(t, 120, configured.ResolvedProbeGuardWindowSeconds())
	assert.Equal(t, 8, configured.ResolvedProbeGuardModelCount())
}
