package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type completeRegistrationResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

// 构造真实 OAuth/微信回调及补填注册接口，仅替换外部身份提供商。
func setupCompleteRegistrationTest(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()

	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldDB, oldLogs, oldReadDB := model.DB, model.LOG_DB, model.RO_DB
	oldRedis := common.RedisEnabled
	initModelListColumnNames(t)
	require.NoError(t, i18n.Init())

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	model.RO_DB = nil
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.InvitationCode{}, &model.Log{}, &model.SecurityFlow{}, &model.TwoFA{}, &model.TwoFABackupCode{}))
	t.Cleanup(func() {
		model.DB, model.LOG_DB, model.RO_DB = oldDB, oldLogs, oldReadDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
		common.RedisEnabled = oldRedis
		require.NoError(t, sqlDB.Close())
	})

	oldRegister := common.RegisterEnabled
	oldInvitation := common.InvitationCodeEnabled
	oldRatio := common.InvitationCodeRewardRatio
	oldInvitee := common.QuotaForInvitee
	oldNewUser := common.QuotaForNewUser
	oldWeChatEnabled, oldWeChatAddress := common.WeChatAuthEnabled, common.WeChatServerAddress
	common.RegisterEnabled = true
	common.InvitationCodeEnabled = true
	common.InvitationCodeRewardRatio = 50
	common.QuotaForInvitee = 100
	common.QuotaForNewUser = 0
	common.WeChatAuthEnabled = true
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := common.Marshal(wechatLoginResponse{Success: true, Data: r.URL.Query().Get("code")})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(payload)
	}))
	common.WeChatServerAddress = bridge.URL
	priorProvider := oauth.GetProvider("github")
	oauth.Register("github", &registrationTestOAuth{})
	t.Cleanup(func() {
		common.RegisterEnabled = oldRegister
		common.InvitationCodeEnabled = oldInvitation
		common.InvitationCodeRewardRatio = oldRatio
		common.QuotaForInvitee = oldInvitee
		common.QuotaForNewUser = oldNewUser
		common.WeChatAuthEnabled, common.WeChatServerAddress = oldWeChatEnabled, oldWeChatAddress
		bridge.Close()
		oauth.Unregister("github")
		if priorProvider != nil {
			oauth.Register("github", priorProvider)
		}
	})

	router := gin.New()
	store := cookie.NewStore([]byte("complete-registration-test"))
	router.Use(sessions.Sessions("session", store))
	router.GET("/api/oauth/wechat", WeChatAuth)
	router.GET("/api/oauth/state", GenerateOAuthCode)
	router.GET("/api/oauth/:provider", HandleOAuth)
	router.POST("/api/oauth/complete_registration", CompleteOAuthRegistration)
	router.GET("/api/user/logout", Logout)
	router.POST("/api/user/login/2fa", Verify2FALogin)
	router.GET("/api/user/self", middleware.UserAuth(), GetSelf)
	return db, router
}

type registrationTestOAuth struct{}

func (*registrationTestOAuth) GetName() string { return "Registration Test" }
func (*registrationTestOAuth) IsEnabled() bool { return true }
func (*registrationTestOAuth) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return &oauth.OAuthToken{}, nil
}
func (*registrationTestOAuth) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return &oauth.OAuthUser{ProviderUserID: "github-pending-1", Username: "oauth-user"}, nil
}
func (*registrationTestOAuth) IsUserIDTaken(subject string) bool {
	return model.IsGitHubIdAlreadyTaken(subject)
}
func (*registrationTestOAuth) FillUserByProviderID(user *model.User, subject string) error {
	return model.DB.Where("github_id = ?", subject).First(user).Error
}
func (*registrationTestOAuth) SetProviderUserID(user *model.User, subject string) {
	user.GitHubId = subject
}
func (*registrationTestOAuth) GetProviderPrefix() string { return "github_" }

func registrationCallback(t *testing.T, router *gin.Engine, provider string) (*httptest.ResponseRecorder, completeRegistrationResponse) {
	t.Helper()
	path := "/api/oauth/wechat?code=wx-pending-1"
	var cookies []*http.Cookie
	if provider == "github" {
		state, response := accountRequest(t, router, http.MethodGet, "/api/oauth/state?provider=github", nil, nil, "")
		require.Equal(t, true, response["success"])
		path = "/api/oauth/github?code=test&state=" + url.QueryEscape(response["data"].(string))
		cookies = state.Result().Cookies()
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var response completeRegistrationResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder, response
}

func postCompleteRegistration(t *testing.T, router *gin.Engine, cookies []*http.Cookie, invitationCode string) (*httptest.ResponseRecorder, completeRegistrationResponse) {
	t.Helper()
	body, err := common.Marshal(map[string]string{"invitation_code": invitationCode})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/oauth/complete_registration", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for _, ck := range cookies {
		request.AddCookie(ck)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var resp completeRegistrationResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	return recorder, resp
}

func registrationSessionCookies(t *testing.T, recorder *httptest.ResponseRecorder) []*http.Cookie {
	t.Helper()
	// OAuth 回调会多次保存同一会话，浏览器仅保留最后一个 Set-Cookie。
	cookies := recorder.Result().Cookies()
	for i := len(cookies) - 1; i >= 0; i-- {
		if cookies[i].Name == "session" {
			return []*http.Cookie{cookies[i]}
		}
	}
	require.FailNow(t, "response did not update the session cookie")
	return nil
}

// 补填邀请码完成微信注册的完整契约：无效码返回 reason 且可换码重试；有效码建号、
// 落库邀请人、归属邀请码并发放奖励；成功后旧 Cookie 无法重放且不消费新码。
func TestCompleteOAuthRegistrationWeChatPending(t *testing.T) {
	db, router := setupCompleteRegistrationTest(t)

	inviter := model.User{Username: "inviter-user", Password: "password", Status: common.UserStatusEnabled, AffCode: "aff-inviter"}
	require.NoError(t, db.Create(&inviter).Error)
	code := model.InvitationCode{
		UserId: inviter.Id, Code: "invite-code-1", Quota: 1000,
		Status: common.InvitationCodeStatusEnabled, CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, code.Insert())
	spareCode := model.InvitationCode{
		UserId: inviter.Id, Code: "invite-code-2", Quota: 1000,
		Status: common.InvitationCodeStatusEnabled, CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, spareCode.Insert())

	stashRecorder, response := registrationCallback(t, router, "wechat")
	require.Equal(t, oauthReasonInvitationCodeRequired, response.Data["reason"])
	require.Equal(t, http.StatusOK, stashRecorder.Code)
	pendingCookies := registrationSessionCookies(t, stashRecorder)
	require.NotEmpty(t, pendingCookies)

	// 无效邀请码：返回 reason 供前端留在表单重试，不创建用户
	retry, resp := postCompleteRegistration(t, router, pendingCookies, "no-such-code")
	assert.False(t, resp.Success)
	assert.Equal(t, "invitation_code_required", resp.Data["reason"])
	pendingCookies = retry.Result().Cookies()
	require.NotEmpty(t, pendingCookies)
	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("wechat_id = ?", "wx-pending-1").Count(&count).Error)
	assert.Zero(t, count)

	// 同一 session 换有效码重试：注册成功并登录
	recorder, resp := postCompleteRegistration(t, router, pendingCookies, "invite-code-1")
	require.True(t, resp.Success, "unexpected failure: %s", resp.Message)

	var created model.User
	require.NoError(t, db.Where("wechat_id = ?", "wx-pending-1").First(&created).Error)
	assert.Equal(t, inviter.Id, created.InviterId)
	// 注册使用只建立邀请关系并获得 QuotaForInvitee(100)；码面额奖励只在兑换路径发放
	assert.Equal(t, 100, created.Quota)

	used, err := model.GetInvitationCodeByCode("invite-code-1")
	require.NoError(t, err)
	assert.Equal(t, common.InvitationCodeStatusUsed, used.Status)
	assert.Equal(t, common.InvitationCodeUsedTypeRegister, used.UsedType)
	assert.Equal(t, created.Id, used.UsedUserId)

	// 成功后 pending 已清除：用登录后的新 cookie 再提交，不得再次注册或消费邀请码
	loggedInCookies := recorder.Result().Cookies()
	require.NotEmpty(t, loggedInCookies)
	_, resp = postCompleteRegistration(t, router, loggedInCookies, "invite-code-2")
	assert.False(t, resp.Success)

	// 旧 Cookie 中的注册凭证已在服务端核销，重复提交不得恢复登录。
	_, resp = postCompleteRegistration(t, router, pendingCookies, "invite-code-2")
	assert.False(t, resp.Success)
	spare, err := model.GetInvitationCodeByCode("invite-code-2")
	require.NoError(t, err)
	assert.Equal(t, common.InvitationCodeStatusEnabled, spare.Status)
	require.NoError(t, db.Model(&model.User{}).Where("wechat_id = ?", "wx-pending-1").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

// 没有暂存身份（直接调用或会话过期）时必须拒绝，且不消费邀请码。
func TestCompleteOAuthRegistrationWithoutPending(t *testing.T) {
	db, router := setupCompleteRegistrationTest(t)

	inviter := model.User{Username: "inviter-user", Password: "password", Status: common.UserStatusEnabled, AffCode: "aff-inviter"}
	require.NoError(t, db.Create(&inviter).Error)
	code := model.InvitationCode{
		UserId: inviter.Id, Code: "invite-code-1", Quota: 1000,
		Status: common.InvitationCodeStatusEnabled, CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, code.Insert())

	_, resp := postCompleteRegistration(t, router, nil, "invite-code-1")
	assert.False(t, resp.Success)

	unused, err := model.GetInvitationCodeByCode("invite-code-1")
	require.NoError(t, err)
	assert.Equal(t, common.InvitationCodeStatusEnabled, unused.Status)
	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestCompleteOAuthRegistrationCannotRestoreRevokedLogin(t *testing.T) {
	for _, provider := range []string{"wechat", "github"} {
		for _, revoke := range []string{"logout", "auth_version"} {
			t.Run(provider+"/"+revoke, func(t *testing.T) {
				db, router := setupCompleteRegistrationTest(t)
				for _, code := range []string{"registration", "unused"} {
					require.NoError(t, db.Create(&model.InvitationCode{Code: code, Status: common.InvitationCodeStatusEnabled}).Error)
				}
				first, response := registrationCallback(t, router, provider)
				require.Equal(t, oauthReasonInvitationCodeRequired, response.Data["reason"])
				second, response := registrationCallback(t, router, provider)
				require.Equal(t, oauthReasonInvitationCodeRequired, response.Data["reason"])
				pendingCookies := registrationSessionCookies(t, first)
				login, response := postCompleteRegistration(t, router, pendingCookies, "registration")
				require.True(t, response.Success, response.Message)
				userID := int(response.Data["id"].(float64))
				loginCookies := login.Result().Cookies()
				if revoke == "logout" {
					_, result := accountRequest(t, router, http.MethodGet, "/api/user/logout", loginCookies, nil, "")
					require.Equal(t, true, result["success"])
				} else {
					require.NoError(t, db.Model(&model.User{}).Where("id = ?", userID).Update("auth_version", gorm.Expr("auth_version + 1")).Error)
				}
				denied, _ := accountRequest(t, router, http.MethodGet, "/api/user/self", loginCookies, nil, "")
				assert.Equal(t, http.StatusUnauthorized, denied.Code)
				for _, captured := range [][]*http.Cookie{pendingCookies, registrationSessionCookies(t, second)} {
					_, replay := postCompleteRegistration(t, router, captured, "unused")
					assert.False(t, replay.Success, "已核销或属于已注册身份的待注册凭证不能恢复会话")
				}
				unused, err := model.GetInvitationCodeByCode("unused")
				require.NoError(t, err)
				assert.Equal(t, common.InvitationCodeStatusEnabled, unused.Status)

				// 已有用户重新通过身份提供商登录仍可用，并保留已配置的二次验证。
				common.RegisterEnabled = false
				require.NoError(t, db.Create(&model.TwoFA{UserId: userID, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}).Error)
				fresh, response := registrationCallback(t, router, provider)
				require.True(t, response.Success, response.Message)
				assert.Equal(t, true, response.Data["require_2fa"])
				denied, _ = accountRequest(t, router, http.MethodGet, "/api/user/self", registrationSessionCookies(t, fresh), nil, "")
				assert.Equal(t, http.StatusUnauthorized, denied.Code)
			})
		}
	}
}

func TestCompleteOAuthRegistrationExpiryAndRetry(t *testing.T) {
	for _, provider := range []string{"wechat", "github"} {
		t.Run(provider, func(t *testing.T) {
			db, router := setupCompleteRegistrationTest(t)
			require.NoError(t, db.Create(&model.InvitationCode{Code: "valid", Status: common.InvitationCodeStatusEnabled}).Error)
			pending, response := registrationCallback(t, router, provider)
			require.Equal(t, oauthReasonInvitationCodeRequired, response.Data["reason"])
			var original model.SecurityFlow
			require.NoError(t, db.Where("kind = ?", pendingOAuthRegistrationKind).First(&original).Error)
			retry, response := postCompleteRegistration(t, router, registrationSessionCookies(t, pending), "invalid")
			require.Equal(t, oauthReasonInvitationCodeRequired, response.Data["reason"])
			var replacement model.SecurityFlow
			require.NoError(t, db.Where("kind = ? AND consumed = ?", pendingOAuthRegistrationKind, false).First(&replacement).Error)
			assert.Equal(t, original.ExpiresAt, replacement.ExpiresAt, "换码重试不能延长已验证身份的有效期")
			_, replay := postCompleteRegistration(t, router, registrationSessionCookies(t, pending), "valid")
			assert.False(t, replay.Success, "换码时已替换的旧令牌不能重用")
			require.NoError(t, db.Model(&model.SecurityFlow{}).Where("id = ?", replacement.ID).Update("expires_at", time.Now().Add(-time.Second).Unix()).Error)
			_, response = postCompleteRegistration(t, router, retry.Result().Cookies(), "valid")
			assert.False(t, response.Success)
			var users int64
			require.NoError(t, db.Model(&model.User{}).Count(&users).Error)
			assert.Zero(t, users)
			unused, err := model.GetInvitationCodeByCode("valid")
			require.NoError(t, err)
			assert.Equal(t, common.InvitationCodeStatusEnabled, unused.Status)
		})
	}
}

func TestCompleteOAuthRegistrationConcurrentReplay(t *testing.T) {
	for _, provider := range []string{"wechat", "github"} {
		t.Run(provider, func(t *testing.T) {
			db, router := setupCompleteRegistrationTest(t)
			pending, response := registrationCallback(t, router, provider)
			require.Equal(t, oauthReasonInvitationCodeRequired, response.Data["reason"])
			start := make(chan struct{})
			results := make(chan *httptest.ResponseRecorder, 2)
			for _, code := range []string{"first", "second"} {
				require.NoError(t, db.Create(&model.InvitationCode{Code: code, Status: common.InvitationCodeStatusEnabled}).Error)
				payload, err := common.Marshal(map[string]string{"invitation_code": code})
				require.NoError(t, err)
				request := httptest.NewRequest(http.MethodPost, "/api/oauth/complete_registration", bytes.NewReader(payload))
				request.Header.Set("Content-Type", "application/json")
				for _, cookie := range registrationSessionCookies(t, pending) {
					request.AddCookie(cookie)
				}
				go func() {
					<-start
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, request)
					results <- recorder
				}()
			}
			close(start)
			successes := 0
			for range 2 {
				recorder := <-results
				var result completeRegistrationResponse
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
				if result.Success {
					successes++
				}
			}
			assert.Equal(t, 1, successes, "同一个待注册 Cookie 只能完成一次注册和登录")
			var count int64
			require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
			assert.Equal(t, int64(1), count)
			require.NoError(t, db.Model(&model.InvitationCode{}).Where("status = ?", common.InvitationCodeStatusUsed).Count(&count).Error)
			assert.Equal(t, int64(1), count)
			require.NoError(t, db.Model(&model.SecurityFlow{}).Where("kind = ?", "session").Count(&count).Error)
			assert.Equal(t, int64(1), count)
		})
	}
}
