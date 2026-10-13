package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindViolatedClientUaRule(t *testing.T) {
	const (
		cliUa     = "claude-cli/2.1.161 (external, cli)"
		cliNewUa  = "claude-cli/2.1.266 (external, cli)"
		sdkCliUa  = "claude-cli/2.1.296 (external, sdk-cli)"
		desktopUa = "Claude-Desktop/0.14.10 (Windows)"
	)
	t.Cleanup(SetClientUaRuleSettingForTest([]ClientUaRule{
		{Name: "opus-cli-only", ModelRegex: []string{"^claude-opus"}, UaAllow: []string{"(external, cli)"}},
		{Name: "no-sdk-cli", ModelRegex: []string{"^claude-"}, UaDeny: []string{"re:\\(external, sdk-cli\\)$"}},
		{Name: "channel-12-claude-code", ChannelIDs: []int{12}, UaAllow: []string{"re:^CLAUDE-CLI/"}},
		{Name: "vip-messages-only", GroupRegex: []string{"^vip$"}, PathRegex: []string{"^/v1/messages$"}, UaAllow: []string{"claude-cli/"}},
		{Name: "disabled", Disabled: true, UaDeny: []string{"claude"}},
	}))

	tests := []struct {
		name string
		req  ClientUaRequest
		want string
	}{
		{name: "cli allowed on opus", req: ClientUaRequest{Model: "claude-opus-4-1", UserAgent: cliUa}, want: ""},
		{name: "newer cli version still allowed", req: ClientUaRequest{Model: "claude-opus-4-1", UserAgent: cliNewUa}, want: ""},
		{name: "sdk-cli rejected on opus by allow list", req: ClientUaRequest{Model: "claude-opus-4-1", UserAgent: sdkCliUa}, want: "opus-cli-only"},
		{name: "sdk-cli rejected on sonnet by deny list", req: ClientUaRequest{Model: "claude-sonnet-4-5", UserAgent: sdkCliUa}, want: "no-sdk-cli"},
		{name: "desktop rejected on opus", req: ClientUaRequest{Model: "claude-opus-4-1", UserAgent: desktopUa}, want: "opus-cli-only"},
		{name: "desktop allowed on sonnet", req: ClientUaRequest{Model: "claude-sonnet-4-5", UserAgent: desktopUa}, want: ""},
		{name: "empty ua rejected by allow list", req: ClientUaRequest{Model: "claude-opus-4-1"}, want: "opus-cli-only"},
		{name: "channel rule skipped before routing", req: ClientUaRequest{Model: "gpt-4o", UserAgent: desktopUa}, want: ""},
		{name: "channel rule applies to bound channel", req: ClientUaRequest{Model: "gpt-4o", ChannelID: 12, UserAgent: desktopUa}, want: "channel-12-claude-code"},
		{name: "channel regex is case insensitive", req: ClientUaRequest{Model: "gpt-4o", ChannelID: 12, UserAgent: cliUa}, want: ""},
		{name: "channel rule ignores other channels", req: ClientUaRequest{Model: "gpt-4o", ChannelID: 13, UserAgent: desktopUa}, want: ""},
		{name: "unbound rules not rechecked after routing", req: ClientUaRequest{Model: "claude-opus-4-1", ChannelID: 13, UserAgent: desktopUa}, want: ""},
		{name: "group and path both match", req: ClientUaRequest{Model: "gpt-4o", Group: "vip", Path: "/v1/messages", UserAgent: desktopUa}, want: "vip-messages-only"},
		{name: "group matches but path does not", req: ClientUaRequest{Model: "gpt-4o", Group: "vip", Path: "/v1/chat/completions", UserAgent: desktopUa}, want: ""},
		{name: "path matches but group does not", req: ClientUaRequest{Model: "gpt-4o", Group: "default", Path: "/v1/messages", UserAgent: desktopUa}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := FindViolatedClientUaRule(tt.req)
			if tt.want == "" {
				assert.Nil(t, rule)
				return
			}
			require.NotNil(t, rule)
			assert.Equal(t, tt.want, rule.Name)
		})
	}
}

func TestFindViolatedClientUaRuleDenyWinsOverAllow(t *testing.T) {
	t.Cleanup(SetClientUaRuleSettingForTest([]ClientUaRule{
		{Name: "cli-but-not-old", UaAllow: []string{"claude-cli/"}, UaDeny: []string{"claude-cli/2.1.161 "}},
	}))

	assert.Nil(t, FindViolatedClientUaRule(ClientUaRequest{UserAgent: "claude-cli/2.1.266 (external, cli)"}))
	rule := FindViolatedClientUaRule(ClientUaRequest{UserAgent: "claude-cli/2.1.161 (external, cli)"})
	require.NotNil(t, rule)
	assert.Equal(t, "cli-but-not-old", rule.Name)
}

func TestFindViolatedClientUaRuleDisabledSetting(t *testing.T) {
	t.Cleanup(SetClientUaRuleSettingForTest([]ClientUaRule{{Name: "deny-all", UaDeny: []string{"re:.*"}}}))
	require.NotNil(t, FindViolatedClientUaRule(ClientUaRequest{UserAgent: "curl/8.0"}))

	clientUaRuleSetting.Enabled = false
	SyncClientUaRuleSetting()
	assert.Nil(t, FindViolatedClientUaRule(ClientUaRequest{UserAgent: "curl/8.0"}))
}

func TestValidateClientUaRuleOption(t *testing.T) {
	encode := func(rules []ClientUaRule) string {
		data, err := common.Marshal(rules)
		require.NoError(t, err)
		return string(data)
	}
	key := ClientUaRuleSettingPrefix + "rules"

	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{name: "unrelated key", key: "channel_route_setting.rules", value: "not json"},
		{name: "enabled true", key: ClientUaRuleSettingPrefix + "enabled", value: "true"},
		{name: "enabled invalid", key: ClientUaRuleSettingPrefix + "enabled", value: "yes", wantErr: true},
		{name: "empty list", key: key, value: "[]"},
		{name: "valid rules", key: key, value: encode([]ClientUaRule{
			{Name: "a", ModelRegex: []string{"^claude-opus"}, UaAllow: []string{"(external, cli)", "re:^claude-cli/"}, StatusCode: 403},
			{Name: "b", ChannelIDs: []int{3}, UaDeny: []string{"sdk-cli"}},
		})},
		{name: "invalid json", key: key, value: "{", wantErr: true},
		{name: "missing name", key: key, value: encode([]ClientUaRule{{UaAllow: []string{"x"}}}), wantErr: true},
		{name: "duplicate name", key: key, value: encode([]ClientUaRule{{Name: "a", UaAllow: []string{"x"}}, {Name: "a", UaDeny: []string{"y"}}}), wantErr: true},
		{name: "no ua lists", key: key, value: encode([]ClientUaRule{{Name: "a", ModelRegex: []string{"^gpt"}}}), wantErr: true},
		{name: "invalid model regex", key: key, value: encode([]ClientUaRule{{Name: "a", ModelRegex: []string{"("}, UaAllow: []string{"x"}}}), wantErr: true},
		{name: "invalid ua regex", key: key, value: encode([]ClientUaRule{{Name: "a", UaAllow: []string{"re:("}}}), wantErr: true},
		{name: "empty ua regex", key: key, value: encode([]ClientUaRule{{Name: "a", UaAllow: []string{"re:"}}}), wantErr: true},
		{name: "blank ua entry", key: key, value: encode([]ClientUaRule{{Name: "a", UaDeny: []string{" "}}}), wantErr: true},
		{name: "status code below 400", key: key, value: encode([]ClientUaRule{{Name: "a", UaDeny: []string{"x"}, StatusCode: 302}}), wantErr: true},
		{name: "status code 5xx", key: key, value: encode([]ClientUaRule{{Name: "a", UaDeny: []string{"x"}, StatusCode: 503}}), wantErr: true},
		{name: "non positive channel id", key: key, value: encode([]ClientUaRule{{Name: "a", UaDeny: []string{"x"}, ChannelIDs: []int{0}}}), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClientUaRuleOption(tt.key, tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
