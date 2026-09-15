package controller

import (
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCustomOAuthUnbindTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.CustomOAuthProvider{}, &model.UserOAuthBinding{}, &model.SecurityFlow{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func insertUnbindFixture(t *testing.T, db *gorm.DB, disableUnbind bool, isRegistration bool) (*model.User, *model.CustomOAuthProvider) {
	t.Helper()

	user := &model.User{Username: "u-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")), Password: "test-password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)

	provider := &model.CustomOAuthProvider{
		Name:                  "Test Provider",
		Slug:                  "test-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")),
		Enabled:               true,
		ClientId:              "cid",
		ClientSecret:          "secret",
		AuthorizationEndpoint: "https://example.com/auth",
		TokenEndpoint:         "https://example.com/token",
		UserInfoEndpoint:      "https://example.com/userinfo",
		DisableUnbind:         disableUnbind,
	}
	require.NoError(t, db.Create(provider).Error)

	binding := &model.UserOAuthBinding{
		UserId:         user.Id,
		ProviderId:     provider.Id,
		ProviderUserId: "ext-1",
		IsRegistration: isRegistration,
	}
	require.NoError(t, db.Create(binding).Error)
	return user, provider
}

func performSelfUnbind(t *testing.T, userId int, providerId int) *httptest.ResponseRecorder {
	t.Helper()
	sid, err := model.CreateSecurityFlow(model.SecurityFlow{UserID: userId, Kind: "session", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)
	identity := model.SecurityIdentity{UserID: userId, SessionID: model.SecurityTokenHash(sid)}
	proof, err := model.CreateSecurityFlow(model.SecurityFlow{UserID: userId, SessionID: identity.SessionID, Kind: "proof", Scope: "account.unbind.oauth", ContextHash: model.SecurityTokenHash(fmt.Sprint(providerId)), ExpiresAt: time.Now().Add(time.Minute).Unix()})
	require.NoError(t, err)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("unbind-test"))))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", userId)
		session.Set("auth_version", int64(0))
		session.Set("security_session", identity.SessionID)
		c.Set("id", userId)
	})
	router.DELETE("/api/user/oauth/bindings/:provider_id", UnbindCustomOAuth)
	request := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/user/oauth/bindings/%d", providerId), nil)
	request.Header.Set("X-Security-Proof", proof)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeApiResponse(t *testing.T, recorder *httptest.ResponseRecorder) (bool, string) {
	t.Helper()
	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return body.Success, body.Message
}

func bindingExists(t *testing.T, db *gorm.DB, userId, providerId int) bool {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&model.UserOAuthBinding{}).Where("user_id = ? AND provider_id = ?", userId, providerId).Count(&count).Error)
	return count > 0
}

func TestUnbindCustomOAuthRejectsRegistrationBinding(t *testing.T) {
	db := setupCustomOAuthUnbindTestDB(t)
	user, provider := insertUnbindFixture(t, db, false, true)

	recorder := performSelfUnbind(t, user.Id, provider.Id)
	success, message := decodeApiResponse(t, recorder)

	assert.False(t, success)
	assert.Contains(t, message, "注册")
	assert.True(t, bindingExists(t, db, user.Id, provider.Id), "registration binding must survive")
}

func TestUnbindCustomOAuthRejectsWhenProviderDisallows(t *testing.T) {
	db := setupCustomOAuthUnbindTestDB(t)
	user, provider := insertUnbindFixture(t, db, true, false)

	recorder := performSelfUnbind(t, user.Id, provider.Id)
	success, message := decodeApiResponse(t, recorder)

	assert.False(t, success)
	assert.Contains(t, message, "禁止")
	assert.True(t, bindingExists(t, db, user.Id, provider.Id), "binding must survive when provider disallows unbind")
}

func TestUnbindCustomOAuthAllowsRegularBinding(t *testing.T) {
	db := setupCustomOAuthUnbindTestDB(t)
	user, provider := insertUnbindFixture(t, db, false, false)

	recorder := performSelfUnbind(t, user.Id, provider.Id)
	success, _ := decodeApiResponse(t, recorder)

	assert.True(t, success)
	assert.False(t, bindingExists(t, db, user.Id, provider.Id), "regular binding should be removed")
}

func TestUnbindCustomOAuthByAdminBypassesRestrictions(t *testing.T) {
	db := setupCustomOAuthUnbindTestDB(t)
	// 注册来源 + 提供商禁止解绑,管理员仍可解绑
	user, provider := insertUnbindFixture(t, db, true, true)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/user/1/oauth/bindings/1", nil)
	ctx.Set("role", common.RoleRootUser)
	ctx.Params = gin.Params{
		{Key: "id", Value: fmt.Sprintf("%d", user.Id)},
		{Key: "provider_id", Value: fmt.Sprintf("%d", provider.Id)},
	}
	UnbindCustomOAuthByAdmin(ctx)

	success, _ := decodeApiResponse(t, recorder)
	assert.True(t, success)
	assert.False(t, bindingExists(t, db, user.Id, provider.Id), "admin unbind must bypass restrictions")
}
