package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func setRiskSetting(t *testing.T, setting *operation_setting.RiskControlSetting) {
	t.Helper()
	t.Cleanup(func() {
		operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{})
	})
	operation_setting.SetRiskControlSettingForTest(setting)
}

func TestMatchUaEntry(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		ua    string
		want  bool
	}{
		{name: "substring case-insensitive", entry: "curl", ua: "Curl/8.0.1", want: true},
		{name: "substring no match", entry: "wget", ua: "curl/8.0.1", want: false},
		{name: "regex match", entry: `^python-requests/2\.\d+`, ua: "python-requests/2.31.0", want: true},
		{name: "regex no match", entry: `^python-requests/2\.\d+`, ua: "python-httpx/0.27", want: false},
		{name: "empty entry", entry: "", ua: "anything", want: false},
		{name: "invalid regex falls back to substring", entry: "bad[regex", ua: "some BAD[REGEX client", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchUaEntry(tt.entry, tt.ua); got != tt.want {
				t.Fatalf("matchUaEntry(%q, %q) = %t, want %t", tt.entry, tt.ua, got, tt.want)
			}
		})
	}
}

func TestMatchUaBlacklist(t *testing.T) {
	setRiskSetting(t, &operation_setting.RiskControlSetting{
		Enabled:     true,
		UaBlacklist: []string{"curl", `^Go-http-client`},
	})

	if matched, rule := MatchUaBlacklist("curl/8.0"); !matched || rule != "curl" {
		t.Fatalf("expected curl to match, got matched=%t rule=%q", matched, rule)
	}
	if matched, _ := MatchUaBlacklist("Go-http-client/2.0"); !matched {
		t.Fatal("expected Go-http-client to match")
	}
	if matched, _ := MatchUaBlacklist("Mozilla/5.0"); matched {
		t.Fatal("expected Mozilla not to match")
	}
	if matched, _ := MatchUaBlacklist(""); matched {
		t.Fatal("expected empty UA not to match")
	}
}

func TestMatchUaBlacklistDisabled(t *testing.T) {
	setRiskSetting(t, &operation_setting.RiskControlSetting{
		Enabled:     false,
		UaBlacklist: []string{"curl"},
	})
	if matched, _ := MatchUaBlacklist("curl/8.0"); matched {
		t.Fatal("expected no match when risk control disabled")
	}
}

func TestCheckRequestRiskBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setRiskSetting(t, &operation_setting.RiskControlSetting{
		Enabled:           true,
		UaBlacklist:       []string{"badbot"},
		UaBlacklistAction: operation_setting.RiskUaActionBlock,
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Request.Header.Set("User-Agent", "BadBot/1.0")
	if err := CheckRequestRisk(ctx); err == nil {
		t.Fatal("expected blocked error for blacklisted UA")
	}

	ctx2, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx2.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx2.Request.Header.Set("User-Agent", "GoodClient/1.0")
	if err := CheckRequestRisk(ctx2); err != nil {
		t.Fatalf("expected nil for normal UA, got %v", err)
	}
}

func TestCheckRequestRiskDisabledZeroCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setRiskSetting(t, &operation_setting.RiskControlSetting{Enabled: false})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Request.Header.Set("User-Agent", "anything")
	if err := CheckRequestRisk(ctx); err != nil {
		t.Fatalf("expected nil when disabled, got %v", err)
	}
}

func TestIsRiskWhitelisted(t *testing.T) {
	setting := &operation_setting.RiskControlSetting{WhitelistUserIds: []int{7, 42}}
	if !isRiskWhitelisted(setting, 42) {
		t.Fatal("expected user 42 whitelisted")
	}
	if isRiskWhitelisted(setting, 1) {
		t.Fatal("expected user 1 not whitelisted")
	}
	if isRiskWhitelisted(nil, 42) {
		t.Fatal("expected nil setting to whitelist nobody")
	}
}

func TestMatchIpBlacklist(t *testing.T) {
	setRiskSetting(t, &operation_setting.RiskControlSetting{
		Enabled:     true,
		IpBlacklist: []string{"1.2.3.4", "10.0.0.0/8", "2001:db8::/32", "not-an-ip"},
	})

	tests := []struct {
		ip   string
		want bool
	}{
		{ip: "1.2.3.4", want: true},        // 精确 IP
		{ip: "1.2.3.5", want: false},       // 相邻 IP 不命中
		{ip: "10.20.30.40", want: true},    // CIDR 命中
		{ip: "11.0.0.1", want: false},      // CIDR 外
		{ip: "2001:db8::1", want: true},    // IPv6 CIDR
		{ip: "2001:db9::1", want: false},   // IPv6 CIDR 外
		{ip: "", want: false},              // 空 IP
		{ip: "garbage", want: false},       // 非法 IP
	}
	for _, tt := range tests {
		if got, _ := MatchIpBlacklist(tt.ip); got != tt.want {
			t.Fatalf("MatchIpBlacklist(%q) = %t, want %t", tt.ip, got, tt.want)
		}
	}
}

func TestMatchIpBlacklistDisabled(t *testing.T) {
	setRiskSetting(t, &operation_setting.RiskControlSetting{
		Enabled:     false,
		IpBlacklist: []string{"1.2.3.4"},
	})
	if matched, _ := MatchIpBlacklist("1.2.3.4"); matched {
		t.Fatal("expected no match when risk control disabled")
	}
}

func TestCheckRequestRiskIpBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setRiskSetting(t, &operation_setting.RiskControlSetting{
		Enabled:     true,
		IpBlacklist: []string{"203.0.113.0/24"},
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Request.RemoteAddr = "203.0.113.7:12345"
	if err := CheckRequestRisk(ctx); err == nil {
		t.Fatal("expected blocked error for blacklisted IP")
	}

	ctx2, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx2.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx2.Request.RemoteAddr = "198.51.100.7:12345"
	if err := CheckRequestRisk(ctx2); err != nil {
		t.Fatalf("expected nil for non-blacklisted IP, got %v", err)
	}
}
