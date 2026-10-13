package operation_setting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompileUaPattern 覆盖风控 UA 黑名单与客户端 UA 准入规则共用的条目写法。
func TestCompileUaPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entry   string
		ua      string
		want    bool
		wantErr bool
	}{
		{name: "substring ignores case", entry: "Claude-CLI", ua: "claude-cli/2.0.1 (external, cli)", want: true},
		{name: "substring keeps dots literal", entry: "curl/8.0", ua: "curl/8x0", want: false},
		{name: "regex ignores case", entry: `re:^CLAUDE-cli/\d`, ua: "claude-cli/2.0.1", want: true},
		{name: "regex anchors apply", entry: "re:^curl", ua: "not curl", want: false},
		{name: "surrounding spaces are trimmed", entry: "  wget  ", ua: "Wget/1.21", want: true},
		{name: "empty entry", entry: "  ", wantErr: true},
		{name: "empty regex", entry: "re: ", wantErr: true},
		{name: "invalid regex", entry: "re:(", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, err := CompileUaPattern(tt.entry)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, pattern.Match(strings.ToLower(tt.ua)))
		})
	}
}

// TestNormalizeRiskUaBlacklist 覆盖保存入口:非法正则整项拒绝并指出第几条,合法条目去掉首尾空白后保存。
func TestNormalizeRiskUaBlacklist(t *testing.T) {
	t.Parallel()

	key := RiskControlSettingPrefix + "ua_blacklist"
	normalized, err := NormalizeRiskControlOption(key, `[" curl ","re:^Go-http-client"]`)
	require.NoError(t, err)
	assert.Equal(t, `["curl","re:^Go-http-client"]`, normalized)

	_, err = NormalizeRiskControlOption(key, `["curl","re:bad[regex"]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "第 2 项")

	_, err = NormalizeRiskControlOption(key, `"curl"`)
	assert.Error(t, err)
}
