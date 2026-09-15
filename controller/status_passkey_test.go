package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 匿名状态接口不得暴露 Passkey 的允许来源列表，其中可能包含内网域名。
func TestGetStatusDoesNotExposePasskeyOrigins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := system_setting.GetPasskeySettings()
	originalSettings := *settings
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		*settings = originalSettings
		system_setting.ServerAddress = originalServerAddress
	})
	system_setting.ServerAddress = "https://www.example.com"
	*settings = system_setting.PasskeySettings{Enabled: true, RPID: "example.com", Origins: "https://www.example.com,https://private.example.com"}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	GetStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	assert.NotContains(t, payload.Data, "passkey_origins")
	assert.NotContains(t, recorder.Body.String(), "private.example.com")
	assert.Equal(t, true, payload.Data["passkey_login"])
	assert.Equal(t, "example.com", payload.Data["passkey_rp_id"])
}
