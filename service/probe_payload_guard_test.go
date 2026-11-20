package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchProbePayloadRequestFormats(t *testing.T) {
	cfg := &operation_setting.RiskControlSetting{
		ProbePayloadTextRules:   operation_setting.RiskDefaultProbePayloadTextRules,
		ProbePayloadPingEnabled: true,
	}

	// 1. OpenAI 单轮纯文本（全角/空格/大小写变体应归一化匹配）
	oaiProbe := &dto.GeneralOpenAIRequest{
		Model: "gpt-4o",
		Messages: []dto.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "  ＲＥＰＬＹ   with   EXACTLY   ok "},
		},
	}
	match := MatchProbePayloadRequest(oaiProbe, cfg)
	require.NotNil(t, match)
	assert.Equal(t, "Reply with exactly OK", match.Rule)

	// 2. OpenAI 多轮对话应跳过
	oaiMultiTurn := &dto.GeneralOpenAIRequest{
		Model: "gpt-4o",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
			{Role: "user", Content: "say OK"},
		},
	}
	assert.Nil(t, MatchProbePayloadRequest(oaiMultiTurn, cfg))

	// 3. Claude 单轮图片块应跳过，纯文本块应命中
	claudeImage := &dto.ClaudeRequest{
		Model: "claude-3-7-sonnet",
		Messages: []dto.ClaudeMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "say OK"},
				map[string]any{"type": "image", "source": map[string]any{"type": "base64"}},
			},
		}},
	}
	assert.Nil(t, MatchProbePayloadRequest(claudeImage, cfg))

	claudeText := &dto.ClaudeRequest{
		Model:    "claude-3-7-sonnet",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "回复两个字：可用"}},
	}
	require.NotNil(t, MatchProbePayloadRequest(claudeText, cfg))

	// 4. OpenAI Responses probe_ping 工具探测命中，携带 previous_response_id 跳过
	pingReq := &dto.OpenAIResponsesRequest{
		Model:      "gpt-5",
		Input:      json.RawMessage(`"Call the probe_ping function with ok=true to acknowledge readiness. You must use the tool."`),
		Tools:      json.RawMessage(`[{"type":"function","name":"probe_ping","parameters":{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}}]`),
		ToolChoice: json.RawMessage(`"required"`),
	}
	pingMatch := MatchProbePayloadRequest(pingReq, cfg)
	require.NotNil(t, pingMatch)
	assert.Equal(t, "tool:probe_ping", pingMatch.RuleId)

	pingReq.PreviousResponseID = "resp_prev_1"
	assert.Nil(t, MatchProbePayloadRequest(pingReq, cfg))
}

func TestCheckProbePayloadGuardEnforcesAndRespectsExemptions(t *testing.T) {
	setupRealtimeGuardTest(t)
	gin.SetMode(gin.TestMode)

	regular := model.User{Username: "probe_bad", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "prb1"}
	sharedKey := model.User{Username: "probe_shared", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "prb2"}
	require.NoError(t, model.DB.Create(&regular).Error)
	require.NoError(t, model.DB.Create(&sharedKey).Error)

	operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{
		Enabled:                   true,
		ProbePayloadEnabled:       true,
		ProbePayloadDryRun:        false,
		ProbePayloadTextRules:     "say OK",
		ProbePayloadPingEnabled:   true,
		ProbePayloadWindowSeconds: 60,
		ProbePayloadThreshold:     2,
		ProbePayloadAction:        operation_setting.RiskRuleActionDisableUser,
		PublicKeyUserIds:          []int{sharedKey.Id},
		IpBanEscalationMinutes:    []int{10, 60},
	})
	t.Cleanup(func() { operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{}) })

	makeCtx := func(uid int) *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Request.RemoteAddr = "198.51.100.20:12345"
		c.Request.Header.Set("User-Agent", "probe-client/1.0")
		c.Set("id", uid)
		c.Set("role", common.RoleCommonUser)
		c.Set(common.RequestIdKey, "req-test")
		return c
	}
	probeReq := &dto.GeneralOpenAIRequest{
		Model:    "gpt-4o",
		Messages: []dto.Message{{Role: "user", Content: "say OK"}},
	}

	now := time.Now()
	assert.NoError(t, CheckProbePayloadGuard(makeCtx(regular.Id), probeReq, now))
	err := CheckProbePayloadGuard(makeCtx(regular.Id), probeReq, now.Add(time.Second))
	require.ErrorIs(t, err, ErrProbePayloadBlocked)

	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, regular.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, refreshed.Status)

	// 公开密钥账号：会拒绝测活请求，但不会禁用账号本身
	assert.NoError(t, CheckProbePayloadGuard(makeCtx(sharedKey.Id), probeReq, now))
	err = CheckProbePayloadGuard(makeCtx(sharedKey.Id), probeReq, now.Add(time.Second))
	require.ErrorIs(t, err, ErrProbePayloadBlocked)
	var refreshedShared model.User
	require.NoError(t, model.DB.First(&refreshedShared, sharedKey.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, refreshedShared.Status, "公开密钥账号豁免自动禁用")
}

func TestIpBanAutoBanUserCascade(t *testing.T) {
	setupRealtimeGuardTest(t)
	gin.SetMode(gin.TestMode)

	victim := model.User{Username: "ip_cascade_u1", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "ipc1"}
	shared := model.User{Username: "ip_cascade_pub", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "ipc2"}
	require.NoError(t, model.DB.Create(&victim).Error)
	require.NoError(t, model.DB.Create(&shared).Error)

	ban, changed, err := model.UpsertIpBan("203.0.113.88", "malicious proxy", 0, model.RiskBanSourceManual, 1, true)
	require.NoError(t, err)
	require.True(t, changed)

	operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{
		Enabled:          false, // 动态 IP 封禁独立于总开关生效
		PublicKeyUserIds: []int{shared.Id},
	})
	t.Cleanup(func() { operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{}) })

	reqCtx := func(uid int) *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Request.RemoteAddr = "203.0.113.88:5555"
		c.Set("id", uid)
		return c
	}

	assert.ErrorIs(t, CheckRequestRisk(reqCtx(victim.Id)), ErrRiskBlocked)
	var updated model.User
	require.NoError(t, model.DB.First(&updated, victim.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, updated.Status)
	related, err := model.GetIpBanRelatedUsers(ban.Id, 20)
	require.NoError(t, err)
	require.Len(t, related, 1)
	assert.Equal(t, victim.Id, related[0].UserId)

	// 公开密钥账号请求仍被拦截，但不连带禁用账号
	assert.ErrorIs(t, CheckRequestRisk(reqCtx(shared.Id)), ErrRiskBlocked)
	var updatedShared model.User
	require.NoError(t, model.DB.First(&updatedShared, shared.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, updatedShared.Status)
}

func TestSessionMultiUserScanRulePreviewAndAlert(t *testing.T) {
	setupRealtimeGuardTest(t)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
	now := common.GetTimestamp()
	sid := "scan-shared-sess"
	for _, entry := range []model.Log{
		{UserId: 910, Username: "su910", TokenId: 1, Ip: "198.51.100.1", Type: model.LogTypeConsume, CreatedAt: now - 30, SessionId: sid, SessionHash: common.SessionIdentifierHash(sid)},
		{UserId: 911, Username: "su911", TokenId: 2, Ip: "198.51.100.2", Type: model.LogTypeConsume, CreatedAt: now - 20, SessionId: sid, SessionHash: common.SessionIdentifierHash(sid)},
		{UserId: 912, Username: "su912", TokenId: 3, Ip: "198.51.100.3", Type: model.LogTypeConsume, CreatedAt: now - 10, SessionId: sid, SessionHash: common.SessionIdentifierHash(sid)},
	} {
		require.NoError(t, model.LOG_DB.Create(&entry).Error)
	}

	rule := operation_setting.RiskAutoBanRule{
		Id:          "rule-sess-1",
		Name:        "共享会话检测",
		Enabled:     true,
		Metric:      operation_setting.RiskMetricSessionMultiUser,
		WindowHours: 24,
		Threshold:   2,
		Action:      operation_setting.RiskRuleActionAlert,
	}
	preview, err := PreviewRiskRule(rule, 16, nil)
	require.NoError(t, err)
	require.Len(t, preview.Hits, 1)
	assert.Equal(t, model.SharedSessionKindSession, preview.Hits[0].Type)
	assert.Equal(t, sid, preview.Hits[0].Value)
	assert.Equal(t, 3, preview.Hits[0].Observed)
	assert.ElementsMatch(t, []int{910, 911, 912}, preview.Hits[0].UserIds)
}
