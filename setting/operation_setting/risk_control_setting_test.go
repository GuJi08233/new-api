package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRiskControlOptionAcceptsValidValues(t *testing.T) {
	t.Parallel()

	validOptions := map[string]string{
		RiskControlSettingPrefix + "enabled":             "true",
		RiskControlSettingPrefix + "ua_blacklist":        `["curl","^Go-http-client"]`,
		RiskControlSettingPrefix + "ua_blacklist_action": RiskUaActionDisableUser,
		RiskControlSettingPrefix + "ip_blacklist":        `["192.0.2.1","10.0.0.0/8","2001:db8::/32"]`,
		RiskControlSettingPrefix + "scan_minutes":        "10",
		RiskControlSettingPrefix + "whitelist_user_ids":  `[1,42]`,
		RiskControlSettingPrefix + "auto_ban_rules":      `[{"enabled":true,"metric":"ip_multi_user","window_hours":24,"threshold":3,"action":"disable_user"},{"enabled":false,"metric":"user_multi_ip","window_hours":168,"threshold":1,"action":"alert"}]`,
		"unrelated_setting.enabled":                      "not-a-bool",
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
