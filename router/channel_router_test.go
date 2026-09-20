package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func channelRouterFixture(t *testing.T) (*gorm.DB, *gin.Engine, model.User) {
	t.Helper()
	require.NoError(t, i18n.Init())
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channels.db")), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogs, previousReadDB := model.DB, model.LOG_DB, model.RO_DB
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	previousMaster := common.IsMasterNode
	previousRate, previousCritical := common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB, model.RO_DB = db, db, nil
	common.RedisEnabled, common.MemoryCacheEnabled, common.IsMasterNode = false, false, true
	common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = false, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB, model.RO_DB = previousDB, previousLogs, previousReadDB
		common.RedisEnabled, common.MemoryCacheEnabled, common.IsMasterNode = previousRedis, previousMemory, previousMaster
		common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = previousRate, previousCritical
		common.SetDatabaseTypes(previousMainType, previousLogType)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}, &model.Log{}, &model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db))
	accessToken := "channel-route-admin-token"
	admin := model.User{Username: "channel-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AccessToken: &accessToken}
	require.NoError(t, db.Create(&admin).Error)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("channel-router-cookie-secret"))))
	SetApiRouter(engine)
	return db, engine, admin
}

func channelRouterRequest(t *testing.T, engine *gin.Engine, user model.User, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, "/api/channel"+path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+user.GetAccessToken())
	request.Header.Set("New-Api-User", strconv.Itoa(user.Id))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return recorder, response
}

func TestSetApiRouterChannelDeleteRequiresSensitiveGrant(t *testing.T) {
	db, engine, admin := channelRouterFixture(t)
	channel := model.Channel{Name: "protected", Type: 1, Key: "secret", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	path := fmt.Sprintf("/%d", channel.Id)
	read, response := channelRouterRequest(t, engine, admin, http.MethodGet, path, "")
	assert.Equal(t, http.StatusOK, read.Code)
	require.Equal(t, true, response["success"])
	assert.Empty(t, response["data"].(map[string]any)["key"], "默认读取权限不得包含渠道密钥")
	denied, response := channelRouterRequest(t, engine, admin, http.MethodDelete, path, "")
	assert.Equal(t, http.StatusForbidden, denied.Code)
	assert.Equal(t, false, response["success"])
	var count int64
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{
		authz.ResourceChannel: {authz.ActionSensitiveWrite: true},
	}))
	allowed, response := channelRouterRequest(t, engine, admin, http.MethodDelete, path, "")
	assert.Equal(t, http.StatusOK, allowed.Code)
	assert.Equal(t, true, response["success"])
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func TestSetApiRouterChannelPermissionRevocation(t *testing.T) {
	_, engine, admin := channelRouterFixture(t)
	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{
		authz.ResourceChannel: {authz.ActionRead: false, authz.ActionOperate: false, authz.ActionWrite: false, authz.ActionSensitiveWrite: false},
	}))
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/"}, {http.MethodGet, "/bound"}, {http.MethodGet, "/1"},
		{http.MethodGet, "/test/1"}, {http.MethodGet, "/update_balance/1"},
		{http.MethodPost, "/1/status"}, {http.MethodPost, "/status/batch"},
		{http.MethodPost, "/tag/enabled"}, {http.MethodPost, "/multi_key/manage"},
		{http.MethodPut, "/"}, {http.MethodPut, "/tag"}, {http.MethodPost, "/batch/tag"},
		{http.MethodPost, "/upstream_updates/apply"}, {http.MethodPost, "/upstream_updates/detect"},
		{http.MethodPost, "/"}, {http.MethodPost, "/batch"}, {http.MethodDelete, "/disabled"},
		{http.MethodPost, "/copy/1"}, {http.MethodPost, "/ollama/pull"}, {http.MethodDelete, "/ollama/delete"},
		{http.MethodPost, "/codex/oauth/start"}, {http.MethodPost, "/codex/oauth/complete"},
		{http.MethodPost, "/1/codex/oauth/start"}, {http.MethodPost, "/1/codex/oauth/complete"},
		{http.MethodPost, "/1/codex/refresh"}, {http.MethodGet, "/1/codex/usage"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			recorder, response := channelRouterRequest(t, engine, admin, route.method, route.path, "{}")
			assert.Equal(t, http.StatusForbidden, recorder.Code)
			assert.Equal(t, false, response["success"])
		})
	}
}

func TestSetApiRouterChannelRootRestrictionsRemain(t *testing.T) {
	_, engine, admin := channelRouterFixture(t)
	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{
		authz.ResourceChannel: {authz.ActionSensitiveWrite: true, authz.ActionSecretView: true},
	}))
	for _, path := range []string{"/fetch_models", "/1/key"} {
		recorder, response := channelRouterRequest(t, engine, admin, http.MethodPost, path, "{}")
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, false, response["success"])
		assert.Equal(t, i18n.T(nil, i18n.MsgAuthInsufficientPrivilege), response["message"])
	}
}
