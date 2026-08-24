package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

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

// setupCompleteRegistrationTest 构造带 cookie session 的路由：/stash 模拟微信回调
// 因缺邀请码而暂存 pending_wechat_id，complete_registration 为被测接口。
func setupCompleteRegistrationTest(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()

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
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.InvitationCode{}, &model.Log{}))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	oldRegister := common.RegisterEnabled
	oldInvitation := common.InvitationCodeEnabled
	oldRatio := common.InvitationCodeRewardRatio
	oldInvitee := common.QuotaForInvitee
	oldNewUser := common.QuotaForNewUser
	common.RegisterEnabled = true
	common.InvitationCodeEnabled = true
	common.InvitationCodeRewardRatio = 50
	common.QuotaForInvitee = 100
	common.QuotaForNewUser = 0
	t.Cleanup(func() {
		common.RegisterEnabled = oldRegister
		common.InvitationCodeEnabled = oldInvitation
		common.InvitationCodeRewardRatio = oldRatio
		common.QuotaForInvitee = oldInvitee
		common.QuotaForNewUser = oldNewUser
	})

	router := gin.New()
	store := cookie.NewStore([]byte("complete-registration-test"))
	router.Use(sessions.Sessions("session", store))
	router.GET("/stash", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(sessionKeyPendingWeChatId, c.Query("wechat_id"))
		require.NoError(t, session.Save())
		c.Status(http.StatusOK)
	})
	router.POST("/api/oauth/complete_registration", CompleteOAuthRegistration)
	return db, router
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

// 补填邀请码完成微信注册的完整契约：无效码返回 reason 且可换码重试；有效码建号、
// 落库邀请人、归属邀请码并发放奖励；成功后 pending 清除；旧 cookie 重复提交幂等
// 返回已有用户且不消费新码。
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

	// 模拟微信回调缺邀请码后的暂存
	stashRecorder := httptest.NewRecorder()
	router.ServeHTTP(stashRecorder, httptest.NewRequest(http.MethodGet, "/stash?wechat_id=wx-pending-1", nil))
	require.Equal(t, http.StatusOK, stashRecorder.Code)
	pendingCookies := stashRecorder.Result().Cookies()
	require.NotEmpty(t, pendingCookies)

	// 无效邀请码：返回 reason 供前端留在表单重试，不创建用户
	_, resp := postCompleteRegistration(t, router, pendingCookies, "no-such-code")
	assert.False(t, resp.Success)
	assert.Equal(t, "invitation_code_required", resp.Data["reason"])
	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("wechat_id = ?", "wx-pending-1").Count(&count).Error)
	assert.Zero(t, count)

	// 同一 session 换有效码重试：注册成功并登录
	recorder, resp := postCompleteRegistration(t, router, pendingCookies, "invite-code-1")
	require.True(t, resp.Success, "unexpected failure: %s", resp.Message)

	var created model.User
	require.NoError(t, db.Where("wechat_id = ?", "wx-pending-1").First(&created).Error)
	assert.Equal(t, inviter.Id, created.InviterId)
	// 被邀请者奖励 = QuotaForInvitee(100) + 码额度 1000 × 50%
	assert.Equal(t, 600, created.Quota)

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

	// 旧 cookie（仍带 pending）重复提交：幂等返回已有用户，不消费新码
	_, resp = postCompleteRegistration(t, router, pendingCookies, "invite-code-2")
	require.True(t, resp.Success, "unexpected failure: %s", resp.Message)
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
