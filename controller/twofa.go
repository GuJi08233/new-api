package controller

import (
	"errors"
	"gorm.io/gorm"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// Setup2FARequest 设置2FA请求结构
type Setup2FARequest struct {
	Code string `json:"code" binding:"required"`
}

// Verify2FARequest 验证2FA请求结构
type Verify2FARequest struct {
	Code string `json:"code" binding:"required"`
}

// Setup2FAResponse 设置2FA响应结构
type Setup2FAResponse struct {
	Secret      string   `json:"secret"`
	QRCodeData  string   `json:"qr_code_data"`
	BackupCodes []string `json:"backup_codes"`
}

// Setup2FA 初始化2FA设置
func Setup2FA(c *gin.Context) {
	if !middleware.RequireSecurityProof(c, "2fa.setup", "") {
		return
	}
	user, identity, err := verificationAccount(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var existing int64
	if err := model.DB.Model(&model.TwoFA{}).Where("user_id = ? AND is_enabled = ?", user.Id, true).Count(&existing).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if existing > 0 {
		common.ApiErrorMsg(c, "用户已启用2FA")
		return
	}
	key, err := common.GenerateTOTPSecret(user.Username)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	codes, err := common.GenerateBackupCodes()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 数据库中只暂存哈希后的恢复码，初始化不会更改任何现有认证因子。
	hashes := make([]string, len(codes))
	for i, code := range codes {
		hash, err := common.HashBackupCode(code)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		hashes[i] = hash
	}
	payload, err := common.Marshal(twoFAEnrollment{Secret: key.Secret(), CodeHashes: hashes})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.CreateSecurityFlow(model.SecurityFlow{UserID: user.Id, Version: identity.Version, SessionID: identity.SessionID, Kind: "2fa-enroll", Payload: string(payload), ExpiresAt: time.Now().Add(5 * time.Minute).Unix()})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session := sessions.Default(c)
	session.Set("pending_2fa_enrollment", token)
	if err := session.Save(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, Setup2FAResponse{Secret: key.Secret(), QRCodeData: common.GenerateQRCodeData(key.Secret(), user.Username), BackupCodes: codes})
}

type twoFAEnrollment struct {
	Secret     string   `json:"secret"`
	CodeHashes []string `json:"code_hashes"`
}

func Enable2FA(c *gin.Context) {
	var req Setup2FARequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	identity, err := middleware.CookieSecurityIdentity(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session := sessions.Default(c)
	token, _ := session.Get("pending_2fa_enrollment").(string)
	flow, err := model.GetSecurityFlow(token, "2fa-enroll")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var payload twoFAEnrollment
	if err := common.UnmarshalJsonStr(flow.Payload, &payload); err != nil {
		common.ApiError(c, err)
		return
	}
	// 启用时使用的验证码所在时间步立即计入已使用，同一验证码不能再用于签发安全证明。
	step, matched := common.MatchTOTPStep(payload.Secret, req.Code, time.Now())
	if !matched {
		common.ApiErrorMsg(c, "验证码或备用码错误，请重试")
		return
	}
	if _, err := model.ConsumeSecurityFlow(token, "2fa-enroll", identity, "", ""); err != nil {
		common.ApiError(c, err)
		return
	}
	version, err := model.CommitAccountSecurity(identity, func(tx *gorm.DB, user *model.User) error {
		var count int64
		if err := tx.Model(&model.TwoFA{}).Where("user_id = ? AND is_enabled = ?", user.Id, true).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("用户已启用2FA")
		}
		if err := tx.Unscoped().Where("user_id = ?", user.Id).Delete(&model.TwoFA{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", user.Id).Delete(&model.TwoFABackupCode{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.TwoFA{UserId: user.Id, Secret: payload.Secret, IsEnabled: true, LastUsedStep: step}).Error; err != nil {
			return err
		}
		for _, hash := range payload.CodeHashes {
			if err := tx.Create(&model.TwoFABackupCode{UserId: user.Id, CodeHash: hash}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session.Delete("pending_2fa_enrollment")
	if err := saveSecurityVersion(c, version); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(model.ClientLogSource(c), identity.UserID, model.LogTypeSystem, "成功启用两步验证")
	common.ApiSuccess(c, nil)
}

// Disable2FA 禁用2FA
func Disable2FA(c *gin.Context) {
	var req Verify2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}

	userId := c.GetInt("id")
	if !verifySecondFactorForRequest(c, userId, req.Code, true) {
		return
	}

	identity, err := middleware.CookieSecurityIdentity(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	version, err := model.CommitAccountSecurity(identity, func(tx *gorm.DB, user *model.User) error {
		if err := tx.Unscoped().Where("user_id = ?", user.Id).Delete(&model.TwoFABackupCode{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("user_id = ?", user.Id).Delete(&model.TwoFA{}).Error
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := saveSecurityVersion(c, version); err != nil {
		common.ApiError(c, err)
		return
	}

	// 记录操作日志
	model.RecordLog(model.ClientLogSource(c), userId, model.LogTypeSystem, "禁用两步验证")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "两步验证已禁用",
	})
}

// verifySecondFactorForRequest 用数据库中的当前因子校验验证码或备用码；失败时已写出响应。
func verifySecondFactorForRequest(c *gin.Context, userId int, code string, allowBackupCode bool) bool {
	verified, err := model.VerifyTwoFactorCode(userId, code, allowBackupCode)
	if err != nil {
		message := err.Error()
		if errors.Is(err, model.ErrTwoFANotEnabled) {
			message = "用户未启用2FA"
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": message,
		})
		return false
	}
	if !verified {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "验证码或备用码错误，请重试",
		})
		return false
	}
	return true
}

// Get2FAStatus 获取用户2FA状态
func Get2FAStatus(c *gin.Context) {
	userId := c.GetInt("id")

	twoFA, err := getCurrentTwoFA(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	status := map[string]interface{}{
		"enabled": false,
		"locked":  false,
	}

	if twoFA != nil {
		status["enabled"] = twoFA.IsEnabled
		status["locked"] = twoFA.IsLocked()
		if twoFA.IsEnabled {
			// 获取剩余备用码数量
			backupCount, err := model.GetUnusedBackupCodeCount(userId)
			if err != nil {
				common.SysLog("获取备用码数量失败: " + err.Error())
			} else {
				status["backup_codes_remaining"] = backupCount
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    status,
	})
}

// RegenerateBackupCodes 重新生成备用码
func RegenerateBackupCodes(c *gin.Context) {
	var req Verify2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}

	userId := c.GetInt("id")

	// 重新生成备用码只接受认证器验证码，不能用旧备用码换新备用码
	cleanCode, err := common.ValidateNumericCode(req.Code)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if !verifySecondFactorForRequest(c, userId, cleanCode, false) {
		return
	}

	// 生成新的备用码
	backupCodes, err := common.GenerateBackupCodes()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "生成备用码失败",
		})
		common.SysLog("生成备用码失败: " + err.Error())
		return
	}

	hashes := make([]string, len(backupCodes))
	for i, code := range backupCodes {
		hash, err := common.HashBackupCode(code)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		hashes[i] = hash
	}
	identity, err := middleware.CookieSecurityIdentity(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	version, err := model.CommitAccountSecurity(identity, func(tx *gorm.DB, user *model.User) error {
		if err := tx.Unscoped().Where("user_id = ?", user.Id).Delete(&model.TwoFABackupCode{}).Error; err != nil {
			return err
		}
		for _, hash := range hashes {
			if err := tx.Create(&model.TwoFABackupCode{UserId: user.Id, CodeHash: hash}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := saveSecurityVersion(c, version); err != nil {
		common.ApiError(c, err)
		return
	}

	// 记录操作日志
	model.RecordLog(model.ClientLogSource(c), userId, model.LogTypeSystem, "重新生成两步验证备用码")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "备用码重新生成成功",
		"data": map[string]interface{}{
			"backup_codes": backupCodes,
		},
	})
}

// Verify2FALogin 登录时验证2FA
func Verify2FALogin(c *gin.Context) {
	var req Verify2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}

	session := sessions.Default(c)
	token, _ := session.Get("pending_login").(string)
	flow, err := model.GetSecurityFlow(token, "login")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user, err := model.GetUserById(flow.UserID, false)
	if err != nil || user.AuthVersion != flow.Version || user.Status != common.UserStatusEnabled {
		common.ApiErrorMsg(c, "登录状态已失效，请重新登录")
		return
	}
	// 直接对数据库中的当前因子校验并记录使用：因子在此期间被更换或禁用时校验不会通过，
	// 校验之后再被更换则由 auth_version 使登录挑战和会话失效。
	if !verifySecondFactorForRequest(c, user.Id, req.Code, true) {
		return
	}

	if _, err := model.ConsumeSecurityFlow(token, "login", model.SecurityIdentity{UserID: flow.UserID, Version: flow.Version}, "", ""); err != nil {
		common.ApiError(c, err)
		return
	}
	c.Set("login_factor_verified", true)
	setupLogin(user, c)
}

// Admin2FAStats 管理员获取2FA统计信息
func Admin2FAStats(c *gin.Context) {
	stats, err := model.GetTwoFAStats()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}

// AdminDisable2FA 管理员强制禁用用户2FA
func AdminDisable2FA(c *gin.Context) {
	userIdStr := c.Param("id")
	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户ID格式错误",
		})
		return
	}

	// 检查目标用户权限
	targetUser, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, targetUser.Role) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权操作同级或更高级用户的2FA设置",
		})
		return
	}

	// 禁用2FA
	if err := model.RevokeAccountAuthentication(userId, "2fa", 0); err != nil {
		if errors.Is(err, model.ErrTwoFANotEnabled) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "用户未启用2FA",
			})
			return
		}
		common.ApiError(c, err)
		return
	}

	recordManageAuditFor(c, userId, "user.2fa_disable", nil)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "用户2FA已被强制禁用",
	})
}
