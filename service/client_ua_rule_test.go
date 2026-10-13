package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestMatchClientUaRuleRequestContext(t *testing.T) {
	t.Cleanup(operation_setting.SetClientUaRuleSettingForTest([]operation_setting.ClientUaRule{
		{Name: "vip-cli-only", GroupRegex: []string{"^vip$"}, UaAllow: []string{"claude-cli/"}},
	}))

	tests := []struct {
		name       string
		path       string
		usingGroup string
		userGroup  string
		want       bool
	}{
		{name: "single token group is matched", path: "/v1/messages", usingGroup: "vip", userGroup: "default", want: true},
		{name: "other token group untouched", path: "/v1/messages", usingGroup: "default", userGroup: "vip", want: false},
		{name: "auto token falls back to user group", path: "/v1/messages", usingGroup: "auto", userGroup: "vip", want: true},
		{name: "playground follows the same rules", path: "/pg/chat/completions", usingGroup: "vip", userGroup: "vip", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", tt.path, nil)
			ctx.Request.Header.Set("User-Agent", "Claude-Desktop/0.14.10")
			common.SetContextKey(ctx, constant.ContextKeyUsingGroup, tt.usingGroup)
			common.SetContextKey(ctx, constant.ContextKeyUserGroup, tt.userGroup)

			rule := MatchClientUaRule(ctx, "claude-opus-4-1", 0)
			assert.Equal(t, tt.want, rule != nil)
		})
	}
}
