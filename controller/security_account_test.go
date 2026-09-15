package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func accountSecurityFixture(t *testing.T) (*gorm.DB, *gin.Engine, model.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "account.db")), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogs := model.DB, model.LOG_DB
	previousRedis := common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogs, previousRedis
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SecurityFlow{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.PasskeyCredential{}, &model.UserOAuthBinding{}, &model.Log{}))
	hash, err := common.Password2Hash("correct-password")
	require.NoError(t, err)
	user := model.User{Username: "alice", Password: hash, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "security"}
	require.NoError(t, db.Create(&user).Error)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("security-integration-cookie-secret"))))
	router.POST("/primary", func(c *gin.Context) { setupLogin(&user, c) })
	router.POST("/api/user/login/2fa", Verify2FALogin)
	router.GET("/api/user/self", middleware.UserAuth(), GetSelf)
	router.GET("/api/verify/requirements", middleware.UserAuth(), SecurityVerificationRequirements)
	router.POST("/api/verify", middleware.UserAuth(), UniversalVerify)
	router.GET("/api/user/token", middleware.UserAuth(), GenerateAccessToken)
	router.DELETE("/api/user/self", middleware.UserAuth(), DeleteSelf)
	router.GET("/api/user/logout", Logout)
	router.GET("/api/oauth/state", GenerateOAuthCode)
	router.GET("/api/oauth/:provider", HandleOAuth)
	return db, router, user
}

func accountRequest(t *testing.T, router *gin.Engine, method, path string, cookies []*http.Cookie, body any, proof string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	data, err := common.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("New-Api-User", "1")
	request.Header.Set("X-Security-Proof", proof)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return recorder, response
}

func TestAccountSecurityPasswordProofCannotAuthorizeOtherOperation(t *testing.T) {
	db, router, user := accountSecurityFixture(t)
	login, response := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	require.Equal(t, true, response["success"])
	cookies := login.Result().Cookies()
	denied, challenge := accountRequest(t, router, http.MethodGet, "/api/user/token", cookies, nil, "")
	require.Equal(t, http.StatusForbidden, denied.Code)
	_, verified := accountRequest(t, router, http.MethodPost, "/api/verify", cookies, map[string]any{"method": "password", "password": "correct-password", "scope": challenge["scope"], "context_hash": challenge["context_hash"]}, "")
	require.Equal(t, true, verified["success"])
	proof := verified["data"].(map[string]any)["proof_token"].(string)
	wrong, _ := accountRequest(t, router, http.MethodDelete, "/api/user/self", cookies, nil, proof)
	assert.Equal(t, http.StatusForbidden, wrong.Code)
	_, generated := accountRequest(t, router, http.MethodGet, "/api/user/token", cookies, nil, proof)
	require.Equal(t, true, generated["success"])
	reused, _ := accountRequest(t, router, http.MethodGet, "/api/user/token", cookies, nil, proof)
	assert.Equal(t, http.StatusForbidden, reused.Code)
	var current model.User
	require.NoError(t, db.First(&current, user.Id).Error)
	assert.Equal(t, generated["data"], current.GetAccessToken())
}

func TestAccountSecurityEveryPrimaryLoginRequiresTOTP(t *testing.T) {
	db, router, user := accountSecurityFixture(t)
	secret := "JBSWY3DPEHPK3PXP"
	require.NoError(t, db.Create(&model.TwoFA{UserId: user.Id, Secret: secret, IsEnabled: true}).Error)
	login, response := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	require.Equal(t, true, response["data"].(map[string]any)["require_2fa"])
	cookies := login.Result().Cookies()
	denied, _ := accountRequest(t, router, http.MethodGet, "/api/user/self", cookies, nil, "")
	assert.Equal(t, http.StatusUnauthorized, denied.Code)
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	verified, completed := accountRequest(t, router, http.MethodPost, "/api/user/login/2fa", cookies, map[string]string{"code": code}, "")
	require.Equal(t, true, completed["success"])
	_, replay := accountRequest(t, router, http.MethodPost, "/api/user/login/2fa", cookies, map[string]string{"code": code}, "")
	assert.Equal(t, false, replay["success"])
	_, passwordAttempt := accountRequest(t, router, http.MethodPost, "/api/verify", verified.Result().Cookies(), map[string]string{"method": "password", "password": "correct-password", "scope": "account.delete", "context_hash": model.SecurityTokenHash("")}, "")
	assert.Equal(t, false, passwordAttempt["success"], "不能用密码绕过已配置的TOTP")
}

func TestAccountSecurityDeleteRevokesCookie(t *testing.T) {
	_, router, _ := accountSecurityFixture(t)
	login, _ := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	cookies := login.Result().Cookies()
	_, verified := accountRequest(t, router, http.MethodPost, "/api/verify", cookies, map[string]string{"method": "password", "password": "correct-password", "scope": "account.delete", "context_hash": model.SecurityTokenHash("")}, "")
	require.Equal(t, true, verified["success"])
	proof := fmt.Sprint(verified["data"].(map[string]any)["proof_token"])
	_, deleted := accountRequest(t, router, http.MethodDelete, "/api/user/self", cookies, nil, proof)
	require.Equal(t, true, deleted["success"])
	denied, _ := accountRequest(t, router, http.MethodGet, "/api/user/self", cookies, nil, "")
	assert.Equal(t, http.StatusUnauthorized, denied.Code)
}

type accountTestOAuth struct{ userID int }

func (p *accountTestOAuth) GetName() string { return "Security Test" }
func (p *accountTestOAuth) IsEnabled() bool { return true }
func (p *accountTestOAuth) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return &oauth.OAuthToken{}, nil
}
func (p *accountTestOAuth) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return &oauth.OAuthUser{ProviderUserID: "bound-user"}, nil
}
func (p *accountTestOAuth) IsUserIDTaken(string) bool { return true }
func (p *accountTestOAuth) FillUserByProviderID(user *model.User, _ string) error {
	return model.DB.First(user, p.userID).Error
}
func (p *accountTestOAuth) SetProviderUserID(*model.User, string) {}
func (p *accountTestOAuth) GetProviderPrefix() string             { return "test_" }

func TestAccountSecurityOAuthOnlyRequiresExistingIdentity(t *testing.T) {
	db, router, user := accountSecurityFixture(t)
	provider := &accountTestOAuth{userID: user.Id}
	prior := oauth.GetProvider("github")
	oauth.Register("github", provider)
	t.Cleanup(func() {
		oauth.Unregister("github")
		if prior != nil {
			oauth.Register("github", prior)
		}
	})
	login, _ := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	cookies := login.Result().Cookies()
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Updates(map[string]any{"password": "", "github_id": "bound-user"}).Error)
	digest := model.SecurityTokenHash("")
	path := "/api/oauth/state?provider=github&verification=true&scope=account.delete&context_hash=" + digest
	begin, result := accountRequest(t, router, http.MethodGet, path, cookies, nil, "")
	require.Equal(t, true, result["success"])
	state := result["data"].(string)
	// 回调始终核对原有 provider 身份，不能用另一账户证明当前身份。
	other := model.User{Username: "other", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "other"}
	require.NoError(t, db.Create(&other).Error)
	provider.userID = other.Id
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("github_id", "different").Error)
	_, rejected := accountRequest(t, router, http.MethodGet, "/api/oauth/github?state="+url.QueryEscape(state)+"&code=test", begin.Result().Cookies(), nil, "")
	assert.Equal(t, false, rejected["success"])
	provider.userID = user.Id
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("github_id", "bound-user").Error)
	begin, result = accountRequest(t, router, http.MethodGet, path, cookies, nil, "")
	require.Equal(t, true, result["success"])
	state = result["data"].(string)
	_, verified := accountRequest(t, router, http.MethodGet, "/api/oauth/github?state="+url.QueryEscape(state)+"&code=test", begin.Result().Cookies(), nil, "")
	require.Equal(t, true, verified["success"])
	assert.NotEmpty(t, verified["data"].(map[string]any)["proof_token"])
}

func TestAccountSecurityLogoutRejectsCapturedCookie(t *testing.T) {
	_, router, _ := accountSecurityFixture(t)
	login, _ := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	cookies := login.Result().Cookies()
	_, loggedOut := accountRequest(t, router, http.MethodGet, "/api/user/logout", cookies, nil, "")
	require.Equal(t, true, loggedOut["success"])
	denied, _ := accountRequest(t, router, http.MethodGet, "/api/user/self", cookies, nil, "")
	assert.Equal(t, http.StatusUnauthorized, denied.Code)
}
