package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type wechatLoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func getWeChatIdByCode(code string) (string, error) {
	if code == "" {
		return "", errors.New("无效的参数")
	}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/wechat/user?code=%s", common.WeChatServerAddress, url.QueryEscape(code)), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", common.WeChatServerToken)
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	httpResponse, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer httpResponse.Body.Close()
	var res wechatLoginResponse
	err = json.NewDecoder(httpResponse.Body).Decode(&res)
	if err != nil {
		return "", err
	}
	if !res.Success {
		return "", errors.New(res.Message)
	}
	if res.Data == "" {
		return "", errors.New("验证码错误或已过期")
	}
	return res.Data, nil
}

func WeChatAuth(c *gin.Context) {
	if !common.WeChatAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员未开启通过微信登录以及注册",
			"success": false,
		})
		return
	}
	code := c.Query("code")
	wechatId, err := getWeChatIdByCode(code)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	user := model.User{
		WeChatId: wechatId,
	}
	if model.IsWeChatIdAlreadyTaken(wechatId) {
		err := user.FillUserByWeChatId()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		if user.Id == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "用户已注销",
			})
			return
		}
	} else {
		invCode := c.Query("invitation_code")
		if invCode == "" {
			session := sessions.Default(c)
			if v, ok := session.Get("invitation_code").(string); ok {
				invCode = v
			}
		}
		registered, err := registerWeChatUser(wechatId, invCode)
		if err != nil {
			// 邀请码缺失/无效时暂存已验证的微信身份，前端补填邀请码后经
			// CompleteOAuthRegistration 免二次验证码完成注册
			var invitationErr *OAuthInvitationCodeError
			if errors.As(err, &invitationErr) {
				session := sessions.Default(c)
				session.Set(sessionKeyPendingWeChatId, wechatId)
				_ = session.Save()
			}
			respondOAuthUserError(c, err)
			return
		}
		user = *registered
	}

	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "用户已被封禁",
			"success": false,
		})
		return
	}
	setupLogin(&user, c)
}

// registerWeChatUser 用已通过验证码换取的微信身份完成注册，遵循邀请码
// 「占用 → 建号 → 归属/失败释放」两阶段协议。若该微信号已注册（重复或
// 并发提交），幂等返回已有用户。
func registerWeChatUser(wechatId string, invCode string) (*model.User, error) {
	if model.IsWeChatIdAlreadyTaken(wechatId) {
		existing := &model.User{WeChatId: wechatId}
		if err := existing.FillUserByWeChatId(); err != nil {
			return nil, err
		}
		if existing.Id == 0 {
			return nil, &OAuthUserDeletedError{}
		}
		return existing, nil
	}
	if !common.RegisterEnabled {
		return nil, &OAuthRegistrationDisabledError{}
	}
	var invitationCodeRecord *model.InvitationCode
	inviterId := 0
	if common.InvitationCodeEnabled {
		if invCode == "" {
			return nil, &OAuthInvitationCodeError{MsgKey: i18n.MsgInvitationCodeRequired}
		}
		record, err := model.ReserveInvitationCode(invCode)
		if err != nil {
			if errors.Is(err, model.ErrInvitationCodeNotUsable) {
				return nil, &OAuthInvitationCodeError{MsgKey: i18n.MsgInvitationCodeUsed}
			}
			return nil, &OAuthInvitationCodeError{MsgKey: i18n.MsgInvitationCodeInvalid}
		}
		invitationCodeRecord = record
		inviterId = record.UserId
	}
	user := &model.User{
		WeChatId:    wechatId,
		Username:    "wechat_" + strconv.Itoa(model.GetMaxUserId()+1),
		DisplayName: "WeChat User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		InviterId:   inviterId,
	}
	if err := user.Insert(inviterId); err != nil {
		model.ReleaseInvitationCode(invitationCodeRecord)
		return nil, err
	}
	// 将邀请码归属到新用户并发放奖励
	if invitationCodeRecord != nil {
		_ = model.FinalizeInvitationCodeUsage(invitationCodeRecord, user.Id)
	}
	return user, nil
}

type wechatBindRequest struct {
	Code string `json:"code"`
}

func WeChatBind(c *gin.Context) {
	if !common.WeChatAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员未开启通过微信登录以及注册",
			"success": false,
		})
		return
	}
	var req wechatBindRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的请求",
		})
		return
	}
	code := req.Code
	wechatId, err := getWeChatIdByCode(code)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	if model.IsWeChatIdAlreadyTaken(wechatId) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该微信账号已被绑定",
		})
		return
	}
	session := sessions.Default(c)
	id := session.Get("id")
	user := model.User{
		Id: id.(int),
	}
	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user.WeChatId = wechatId
	err = user.Update(false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}
