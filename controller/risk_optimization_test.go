package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRiskOptimizationEndpoints(t *testing.T) {
	riskAdminFixture(t)
	gin.SetMode(gin.TestMode)

	u1 := model.User{Username: "sess_u1", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "su1"}
	u2 := model.User{Username: "sess_u2", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "su2"}
	root := model.User{Username: "root_admin", Password: "password123", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "rt1"}
	require.NoError(t, model.DB.Create(&u1).Error)
	require.NoError(t, model.DB.Create(&u2).Error)
	require.NoError(t, model.DB.Create(&root).Error)

	now := common.GetTimestamp()
	sharedSess := "shared-cli-session-1"
	for _, entry := range []model.Log{
		{UserId: u1.Id, Username: u1.Username, TokenId: 201, TokenName: "t1", Ip: "198.51.100.1", Type: model.LogTypeConsume, Quota: 120, CreatedAt: now - 30,
			SessionId: sharedSess, SessionSource: "header:x-claude-code-session-id", SessionHash: common.SessionIdentifierHash(sharedSess)},
		{UserId: u2.Id, Username: u2.Username, TokenId: 202, TokenName: "t2", Ip: "203.0.113.2", Type: model.LogTypeConsume, Quota: 80, CreatedAt: now - 10,
			SessionId: sharedSess, SessionSource: "header:x-claude-code-session-id", SessionHash: common.SessionIdentifierHash(sharedSess)},
	} {
		require.NoError(t, model.LOG_DB.Create(&entry).Error)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 999)
		c.Set("role", common.RoleAdminUser)
		c.Next()
	})
	router.GET("/api/risk/shared_sessions", GetSharedSessions)
	router.GET("/api/risk/rankings", GetRiskRankings)
	router.POST("/api/risk/probe_payload/preview", PreviewProbePayloadText)
	router.GET("/api/risk/probe_payload/users/:id", GetProbePayloadUserState)
	router.POST("/api/risk/probe_payload/users/:id/reset", ResetProbePayloadUserState)
	router.POST("/api/risk/ip_bans", AddIpBan)
	router.PUT("/api/risk/ip_bans/:id", UpdateIpBan)
	router.GET("/api/risk/ip_bans/:id/users", GetIpBanRelatedUsers)

	// 1. 跨账号会话关联查询
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/risk/shared_sessions?kind=session&hours=24&p=1&page_size=20", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var sessResp struct {
		Success bool `json:"success"`
		Data    struct {
			Total int64                     `json:"total"`
			Items []model.SharedSessionItem `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &sessResp))
	require.True(t, sessResp.Success)
	assert.EqualValues(t, 1, sessResp.Data.Total)
	require.Len(t, sessResp.Data.Items, 1)
	assert.Equal(t, sharedSess, sessResp.Data.Items[0].Identifier)

	// 2. 令牌维度排行
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/risk/rankings?metric=token_overview&paginated=true&p=1&page_size=20", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var tokResp struct {
		Success bool `json:"success"`
		Data    struct {
			Total int64                     `json:"total"`
			Items []model.TokenOverviewItem `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &tokResp))
	require.True(t, tokResp.Success)
	assert.EqualValues(t, 2, tokResp.Data.Total)

	// 3. 单轮测活文本规则预览
	w = httptest.NewRecorder()
	body := `{"text":"  reply  with  EXACTLY  ok ","rules":"Reply with exactly OK\nsay OK"}`
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/risk/probe_payload/preview", bytes.NewBufferString(body)))
	require.Equal(t, http.StatusOK, w.Code)
	var previewResp struct {
		Success bool `json:"success"`
		Data    struct {
			Matched bool `json:"matched"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &previewResp))
	assert.True(t, previewResp.Success)
	assert.True(t, previewResp.Data.Matched)

	// 4. 普通管理员不能重置 root 的测活状态
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/risk/probe_payload/users/"+strconv.Itoa(root.Id)+"/reset", nil))
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 5. 添加带连带封号的 IP 封禁、编辑、查询关联用户
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/risk/ip_bans", bytes.NewBufferString(`{"target":"198.51.100.99","reason":"abuse","expire_minutes":0,"auto_ban_user":true}`))
	req.RemoteAddr = "192.0.2.1:9999"
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var banResp struct {
		Success bool        `json:"success"`
		Data    model.IpBan `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &banResp))
	require.True(t, banResp.Success)
	assert.True(t, banResp.Data.AutoBanUser)

	require.NoError(t, model.RecordIpBanUserBan(banResp.Data.Id, u1.Id, "198.51.100.99", "cascade"))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/risk/ip_bans/"+strconv.Itoa(banResp.Data.Id)+"/users", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var relResp struct {
		Success bool `json:"success"`
		Data    struct {
			Items []model.IpBanRelatedUser `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &relResp))
	require.True(t, relResp.Success)
	require.Len(t, relResp.Data.Items, 1)
	assert.Equal(t, u1.Id, relResp.Data.Items[0].UserId)

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/risk/ip_bans/"+strconv.Itoa(banResp.Data.Id), bytes.NewBufferString(`{"reason":"edited","expire_minutes":60,"auto_ban_user":false}`)))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &banResp))
	require.True(t, banResp.Success)
	assert.Equal(t, "edited", banResp.Data.Reason)
	assert.False(t, banResp.Data.AutoBanUser)
	assert.Greater(t, banResp.Data.ExpiresAt, int64(0))

	// 6. 新风控配置校验
	require.NoError(t, operation_setting.ValidateRiskControlOption(operation_setting.RiskControlSettingPrefix+"probe_payload_enabled", "true"))
	require.NoError(t, operation_setting.ValidateRiskControlOption(operation_setting.RiskControlSettingPrefix+"session_risk_recording_enabled", "true"))
	require.Error(t, operation_setting.ValidateRiskControlOption(operation_setting.RiskControlSettingPrefix+"probe_payload_threshold", "0"))
}
