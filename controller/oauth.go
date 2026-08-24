package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// providerParams returns map with Provider key for i18n templates
func providerParams(name string) map[string]any {
	return map[string]any{"Provider": name}
}

// GenerateOAuthCode generates a state code for OAuth CSRF protection
func GenerateOAuthCode(c *gin.Context) {
	session := sessions.Default(c)
	state := common.GetRandomString(12)
	invitationCode := c.Query("invitation_code")
	if invitationCode != "" {
		session.Set("invitation_code", invitationCode)
	}
	session.Set("oauth_state", state)
	err := session.Save()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    state,
	})
}

// HandleOAuth handles OAuth callback for all standard OAuth providers
func HandleOAuth(c *gin.Context) {
	providerName := c.Param("provider")
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthUnknownProvider),
		})
		return
	}

	session := sessions.Default(c)

	// 1. Validate state (CSRF protection)
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}
	session.Delete("oauth_state")
	session.Save()

	// 2. Check if user is already logged in (bind flow)
	username := session.Get("username")
	if username != nil {
		handleOAuthBind(c, provider)
		return
	}

	// 3. Check if provider is enabled
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// 4. Handle error from provider
	errorCode := c.Query("error")
	if errorCode != "" {
		errorDescription := c.Query("error_description")
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": errorDescription,
		})
		return
	}

	// 5. Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// 6. Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// 7. Find or create user
	user, err := findOrCreateOAuthUser(c, provider, oauthUser, session)
	if err != nil {
		// 邀请码缺失/无效时暂存已验证的 OAuth 身份，前端补填邀请码后经
		// CompleteOAuthRegistration 免二次授权完成注册
		var invitationErr *OAuthInvitationCodeError
		if errors.As(err, &invitationErr) {
			if pendingUser, mErr := common.Marshal(oauthUser); mErr == nil {
				session.Set(sessionKeyPendingOAuthProvider, providerName)
				session.Set(sessionKeyPendingOAuthUser, string(pendingUser))
				_ = session.Save()
			}
		}
		respondOAuthUserError(c, err)
		return
	}

	// 8. Check user status
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}

	// 9. Setup login
	setupLogin(user, c)
}

// handleOAuthBind handles binding OAuth account to existing user
func handleOAuthBind(c *gin.Context, provider oauth.Provider) {
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Check if this OAuth account is already bound (check both new ID and legacy ID)
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
		return
	}
	// Also check legacy ID to prevent duplicate bindings during migration period
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		if provider.IsUserIDTaken(legacyID) {
			common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
			return
		}
	}

	// Get current user from session
	session := sessions.Default(c)
	id := session.Get("id")
	user := model.User{Id: id.(int)}
	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Handle binding based on provider type
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: use user_oauth_bindings table
		err = model.UpdateUserOAuthBinding(user.Id, genericProvider.GetProviderId(), oauthUser.ProviderUserID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		// Built-in provider: update user record directly
		provider.SetProviderUserID(&user, oauthUser.ProviderUserID)
		err = user.Update(false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, gin.H{
		"action": "bind",
	})
}

// findOrCreateOAuthUser finds existing user or creates new user
func findOrCreateOAuthUser(c *gin.Context, provider oauth.Provider, oauthUser *oauth.OAuthUser, session sessions.Session) (*model.User, error) {
	user := &model.User{}

	// Check if user already exists with new ID
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		err := provider.FillUserByProviderID(user, oauthUser.ProviderUserID)
		if err != nil {
			return nil, err
		}
		// Check if user has been deleted
		if user.Id == 0 {
			return nil, &OAuthUserDeletedError{}
		}
		return user, nil
	}

	// Try to find user with legacy ID (for GitHub migration from login to numeric ID)
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		if provider.IsUserIDTaken(legacyID) {
			err := provider.FillUserByProviderID(user, legacyID)
			if err != nil {
				return nil, err
			}
			if user.Id != 0 {
				// Found user with legacy ID, migrate to new ID
				common.SysLog(fmt.Sprintf("[OAuth] Migrating user %d from legacy_id=%s to new_id=%s",
					user.Id, legacyID, oauthUser.ProviderUserID))
				if err := user.UpdateGitHubId(oauthUser.ProviderUserID); err != nil {
					common.SysError(fmt.Sprintf("[OAuth] Failed to migrate user %d: %s", user.Id, err.Error()))
					// Continue with login even if migration fails
				}
				return user, nil
			}
		}
	}

	// User doesn't exist, create new user if registration is enabled
	if !common.RegisterEnabled {
		return nil, &OAuthRegistrationDisabledError{}
	}

	// 邀请码验证（OAuth 注册也需要邀请码）
	var invitationCodeRecord *model.InvitationCode
	if common.InvitationCodeEnabled {
		invCode, _ := session.Get("invitation_code").(string)
		if invCode == "" {
			return nil, &OAuthInvitationCodeError{MsgKey: i18n.MsgInvitationCodeRequired}
		}
		var icErr error
		invitationCodeRecord, icErr = model.GetInvitationCodeByCode(invCode)
		if icErr != nil || invitationCodeRecord == nil {
			return nil, &OAuthInvitationCodeError{MsgKey: i18n.MsgInvitationCodeInvalid}
		}
		if invitationCodeRecord.Status != common.InvitationCodeStatusEnabled {
			return nil, &OAuthInvitationCodeError{MsgKey: i18n.MsgInvitationCodeUsed}
		}
	}

	// Set up new user
	user.Username = provider.GetProviderPrefix() + strconv.Itoa(model.GetMaxUserId()+1)

	if oauthUser.Username != "" {
		if exists, err := model.CheckUserExistOrDeleted(oauthUser.Username, ""); err == nil && !exists {
			// 防止索引退化
			if len(oauthUser.Username) <= model.UserNameMaxLength {
				user.Username = oauthUser.Username
			}
		}
	}

	if oauthUser.DisplayName != "" {
		user.DisplayName = oauthUser.DisplayName
	} else if oauthUser.Username != "" {
		user.DisplayName = oauthUser.Username
	} else {
		user.DisplayName = provider.GetName() + " User"
	}
	if oauthUser.Email != "" {
		user.Email = model.NormalizeEmail(oauthUser.Email)
		if err := model.EnsureEmailAvailable(user.Email, 0); err != nil {
			if errors.Is(err, model.ErrEmailAlreadyTaken) {
				return nil, &OAuthEmailAlreadyTakenError{}
			}
			return nil, err
		}
	}
	user.Role = common.RoleCommonUser
	user.Status = common.UserStatusEnabled

	// Handle inviter: 仅支持邀请码生成者
	inviterId := 0
	if common.InvitationCodeEnabled && invitationCodeRecord != nil {
		// 原子占用邀请码，防止并发注册用同一个码同时通过校验
		reserved, reserveErr := model.ReserveInvitationCode(invitationCodeRecord.Code)
		if reserveErr != nil {
			if errors.Is(reserveErr, model.ErrInvitationCodeNotUsable) {
				return nil, &OAuthInvitationCodeError{MsgKey: i18n.MsgInvitationCodeUsed}
			}
			return nil, &OAuthInvitationCodeError{MsgKey: i18n.MsgInvitationCodeInvalid}
		}
		invitationCodeRecord = reserved
		inviterId = invitationCodeRecord.UserId
	}
	// Insert 的 inviterId 参数只负责发放邀请奖励，邀请人字段必须显式落库
	user.InviterId = inviterId

	// Use transaction to ensure user creation and OAuth binding are atomic
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: create user and binding in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, inviterId); err != nil {
				return err
			}

			// Create OAuth binding
			binding := &model.UserOAuthBinding{
				UserId:         user.Id,
				ProviderId:     genericProvider.GetProviderId(),
				ProviderUserId: oauthUser.ProviderUserID,
				IsRegistration: true, // 账号由此登录方式注册,用户不可自行解绑
			}
			if err := model.CreateUserOAuthBindingWithTx(tx, binding); err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			model.ReleaseInvitationCode(invitationCodeRecord)
			return nil, err
		}

		// Perform post-transaction tasks (logs, sidebar config, inviter rewards)
		user.FinalizeOAuthUserCreation(inviterId)
		// 将邀请码归属到新用户并发放奖励
		if common.InvitationCodeEnabled && invitationCodeRecord != nil {
			_ = model.FinalizeInvitationCodeUsage(invitationCodeRecord, user.Id)
		}
	} else {
		// Built-in provider: create user and update provider ID in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, inviterId); err != nil {
				return err
			}

			// Set the provider user ID on the user model and update
			provider.SetProviderUserID(user, oauthUser.ProviderUserID)
			if err := tx.Model(user).Updates(map[string]interface{}{
				"github_id":    user.GitHubId,
				"discord_id":   user.DiscordId,
				"oidc_id":      user.OidcId,
				"linux_do_id":  user.LinuxDOId,
				"steam_openid": user.SteamOpenId,
				"wechat_id":    user.WeChatId,
				"telegram_id":  user.TelegramId,
			}).Error; err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			model.ReleaseInvitationCode(invitationCodeRecord)
			return nil, err
		}

		// Perform post-transaction tasks
		user.FinalizeOAuthUserCreation(inviterId)
		// 将邀请码归属到新用户并发放奖励
		if common.InvitationCodeEnabled && invitationCodeRecord != nil {
			_ = model.FinalizeInvitationCodeUsage(invitationCodeRecord, user.Id)
		}
	}

	return user, nil
}

// Error types for OAuth
type OAuthUserDeletedError struct{}

func (e *OAuthUserDeletedError) Error() string {
	return "user has been deleted"
}

type OAuthRegistrationDisabledError struct{}

func (e *OAuthRegistrationDisabledError) Error() string {
	return "registration is disabled"
}

type OAuthEmailAlreadyTakenError struct{}

func (e *OAuthEmailAlreadyTakenError) Error() string {
	return "email is already in use"
}

// OAuthInvitationCodeError 表示注册被邀请码问题（缺失/无效/已使用）阻断。
// 响应会附带 reason=invitation_code_required，前端据此引导补填邀请码而不是终止流程。
type OAuthInvitationCodeError struct {
	MsgKey string
}

func (e *OAuthInvitationCodeError) Error() string {
	return e.MsgKey
}

// 前端识别的失败原因标记：账号未注册且需要补填邀请码
const oauthReasonInvitationCodeRequired = "invitation_code_required"

// 因邀请码问题中断注册时，暂存已通过身份验证的第三方身份；
// 补填邀请码后经 CompleteOAuthRegistration 免二次授权完成注册
const (
	sessionKeyPendingOAuthProvider = "pending_oauth_provider"
	sessionKeyPendingOAuthUser     = "pending_oauth_user"
	sessionKeyPendingWeChatId      = "pending_wechat_id"
)

// respondOAuthUserError 统一输出第三方注册/登录失败响应（findOrCreateOAuthUser、
// registerWeChatUser 共用）。邀请码类失败附带 reason，供前端进入补填邀请码流程。
func respondOAuthUserError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrEmailAlreadyTaken) {
		common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		return
	}
	switch e := err.(type) {
	case *OAuthUserDeletedError:
		common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
	case *OAuthRegistrationDisabledError:
		common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
	case *OAuthEmailAlreadyTakenError:
		common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
	case *OAuthInvitationCodeError:
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": i18n.T(c, e.MsgKey),
			"data":    gin.H{"reason": oauthReasonInvitationCodeRequired},
		})
	default:
		common.ApiError(c, err)
	}
}

// CompleteOAuthRegistration 用邀请码完成此前因邀请码问题中断的第三方注册。
// 身份信息来自回调阶段暂存的 session，无需再次跳转授权/重新获取验证码。
func CompleteOAuthRegistration(c *gin.Context) {
	var req struct {
		InvitationCode string `json:"invitation_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	req.InvitationCode = strings.TrimSpace(req.InvitationCode)
	if req.InvitationCode == "" {
		common.ApiErrorI18n(c, i18n.MsgInvitationCodeRequired)
		return
	}
	session := sessions.Default(c)
	// 已登录会话不允许消费残留的暂存身份建新号
	if session.Get("username") != nil {
		common.ApiErrorI18n(c, i18n.MsgOAuthPendingNotFound)
		return
	}

	// 微信注册的待完成身份（验证码已在回调阶段核销，直接用暂存的 wechatId）
	if wechatId, _ := session.Get(sessionKeyPendingWeChatId).(string); wechatId != "" {
		user, err := registerWeChatUser(wechatId, req.InvitationCode)
		if err != nil {
			// 邀请码错误时保留暂存身份，允许换码重试
			respondOAuthUserError(c, err)
			return
		}
		if user.Status != common.UserStatusEnabled {
			common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
			return
		}
		session.Delete(sessionKeyPendingWeChatId)
		session.Delete("invitation_code")
		setupLogin(user, c)
		return
	}

	providerName, _ := session.Get(sessionKeyPendingOAuthProvider).(string)
	pendingUserJson, _ := session.Get(sessionKeyPendingOAuthUser).(string)
	if providerName == "" || pendingUserJson == "" {
		common.ApiErrorI18n(c, i18n.MsgOAuthPendingNotFound)
		return
	}
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		common.ApiErrorI18n(c, i18n.MsgOAuthUnknownProvider)
		return
	}
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}
	var oauthUser oauth.OAuthUser
	if err := common.UnmarshalJsonStr(pendingUserJson, &oauthUser); err != nil {
		session.Delete(sessionKeyPendingOAuthProvider)
		session.Delete(sessionKeyPendingOAuthUser)
		_ = session.Save()
		common.ApiErrorI18n(c, i18n.MsgOAuthPendingNotFound)
		return
	}
	session.Set("invitation_code", req.InvitationCode)
	user, err := findOrCreateOAuthUser(c, provider, &oauthUser, session)
	if err != nil {
		// 邀请码错误时保留暂存身份，允许换码重试
		respondOAuthUserError(c, err)
		return
	}
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}
	session.Delete(sessionKeyPendingOAuthProvider)
	session.Delete(sessionKeyPendingOAuthUser)
	session.Delete("invitation_code")
	setupLogin(user, c)
}

// handleOAuthError handles OAuth errors and returns translated message
func handleOAuthError(c *gin.Context, err error) {
	switch e := err.(type) {
	case *oauth.OAuthError:
		if e.Params != nil {
			common.ApiErrorI18n(c, e.MsgKey, e.Params)
		} else {
			common.ApiErrorI18n(c, e.MsgKey)
		}
	case *oauth.AccessDeniedError:
		common.ApiErrorMsg(c, e.Message)
	case *oauth.TrustLevelError:
		common.ApiErrorI18n(c, i18n.MsgOAuthTrustLevelLow)
	default:
		common.ApiError(c, err)
	}
}
