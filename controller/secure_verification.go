package controller

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type UniversalVerifyRequest struct {
	Method      string `json:"method"`
	Code        string `json:"code"`
	Password    string `json:"password"`
	Scope       string `json:"scope"`
	ContextHash string `json:"context_hash"`
}

func validVerificationScope(scope, digest string) bool {
	if len(digest) != 64 {
		return false
	}
	switch scope {
	case "channel.key.read", "account.delete", "access_token.generate", "password.change", "passkey.register", "passkey.delete", "2fa.setup", "account.bind.email", "account.bind.wechat", "account.bind.telegram", "account.bind.oauth", "account.unbind.oauth":
		return true
	}
	return false
}

func verificationAccount(c *gin.Context) (*model.User, model.SecurityIdentity, error) {
	identity, err := middleware.CookieSecurityIdentity(c)
	if err != nil {
		return nil, identity, err
	}
	if _, err := model.ReadSecurityIdentity(identity); err != nil {
		return nil, identity, err
	}
	var user model.User
	err = model.DB.First(&user, identity.UserID).Error
	return &user, identity, err
}

func SecurityVerificationRequirements(c *gin.Context) {
	user, _, err := verificationAccount(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var factor model.TwoFA
	err = model.DB.Where("user_id = ? AND is_enabled = ?", user.Id, true).First(&factor).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiError(c, err)
		return
	}
	var passkeys int64
	if err := model.DB.Model(&model.PasskeyCredential{}).Where("user_id = ?", user.Id).Count(&passkeys).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	providers := []gin.H{}
	for slug, provider := range oauth.GetAllProviders() {
		if !provider.IsEnabled() {
			continue
		}
		bound := false
		if generic, ok := provider.(*oauth.GenericOAuthProvider); ok {
			var count int64
			if err := model.DB.Model(&model.UserOAuthBinding{}).Where("user_id = ? AND provider_id = ?", user.Id, generic.GetProviderId()).Count(&count).Error; err != nil {
				common.ApiError(c, err)
				return
			}
			bound = count > 0
		} else {
			column := oauthBindingColumn(slug)
			values := map[string]string{"github_id": user.GitHubId, "discord_id": user.DiscordId, "oidc_id": user.OidcId, "linux_do_id": user.LinuxDOId, "steam_openid": user.SteamOpenId}
			bound = values[column] != ""
		}
		if bound {
			providers = append(providers, gin.H{"slug": slug, "name": provider.GetName()})
		}
	}
	has2FA := factor.Id > 0
	hasPasskey := passkeys > 0 && system_setting.GetPasskeySettings().Enabled
	common.ApiSuccess(c, gin.H{"has2FA": has2FA, "hasPasskey": hasPasskey, "hasPassword": !has2FA && !hasPasskey && user.Password != "", "hasWeChat": !has2FA && !hasPasskey && user.WeChatId != "" && common.WeChatAuthEnabled, "hasTelegram": !has2FA && !hasPasskey && user.TelegramId != "" && common.TelegramOAuthEnabled, "telegramBotName": common.TelegramBotName, "oauthProviders": providers})
}

func UniversalVerify(c *gin.Context) {
	var req UniversalVerifyRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || !validVerificationScope(req.Scope, req.ContextHash) {
		common.ApiErrorMsg(c, "无效的安全验证操作")
		return
	}
	user, identity, err := verificationAccount(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var twoFA model.TwoFA
	err = model.DB.Where("user_id = ? AND is_enabled = ?", user.Id, true).First(&twoFA).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiError(c, err)
		return
	}
	var passkeys int64
	if err := model.DB.Model(&model.PasskeyCredential{}).Where("user_id = ?", user.Id).Count(&passkeys).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if !system_setting.GetPasskeySettings().Enabled {
		passkeys = 0
	}
	verified := false
	switch req.Method {
	case "2fa":
		if twoFA.Id > 0 {
			verified, err = model.VerifyTwoFactorCode(user.Id, req.Code, true)
			if err != nil {
				common.ApiError(c, err)
				return
			}
		}
	case "password":
		verified = twoFA.Id == 0 && passkeys == 0 && user.Password != "" && common.ValidatePasswordAndHash(req.Password, user.Password)
	case "telegram":
		if twoFA.Id == 0 && passkeys == 0 && user.TelegramId != "" && common.TelegramOAuthEnabled {
			params, err := url.ParseQuery(req.Code)
			if err == nil {
				id, err := verifyTelegramAuthorization(params, common.TelegramBotToken, time.Now())
				verified = err == nil && id == user.TelegramId
			}
		}
	case "wechat":
		if twoFA.Id == 0 && passkeys == 0 && user.WeChatId != "" && common.WeChatAuthEnabled {
			id, err := getWeChatIdByCode(strings.TrimSpace(req.Code))
			verified = err == nil && id == user.WeChatId
		}
	}
	if !verified {
		common.ApiErrorMsg(c, "验证失败，请检查凭据")
		return
	}
	writeSecurityProof(c, identity, req.Scope, req.ContextHash)
}

func writeSecurityProof(c *gin.Context, identity model.SecurityIdentity, scope, digest string) {
	if !validVerificationScope(scope, digest) {
		common.ApiErrorMsg(c, "无效的安全验证操作")
		return
	}
	if _, err := model.ReadSecurityIdentity(identity); err != nil {
		common.ApiError(c, err)
		return
	}
	expires := time.Now().Add(5 * time.Minute).Unix()
	token, err := model.CreateSecurityFlow(model.SecurityFlow{UserID: identity.UserID, Version: identity.Version, SessionID: identity.SessionID, Kind: "proof", Scope: scope, ContextHash: digest, ExpiresAt: expires})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"proof_token": token, "expires_at": expires, "action": "verification"}})
}

func oauthBindingColumn(slug string) string {
	return map[string]string{"github": "github_id", "discord": "discord_id", "oidc": "oidc_id", "linuxdo": "linux_do_id", "steam": "steam_openid"}[slug]
}

func saveSecurityVersion(c *gin.Context, version int64) error {
	session := sessions.Default(c)
	session.Set("auth_version", version)
	return session.Save()
}

func getCurrentTwoFA(id int) (*model.TwoFA, error) {
	var factor model.TwoFA
	err := model.DB.Where("user_id = ?", id).First(&factor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &factor, err
}
