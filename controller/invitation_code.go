package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ========== 用户接口 ==========

// GenerateInvitationCode 用户花费额度生成邀请码
func GenerateInvitationCode(c *gin.Context) {
	if !common.InvitationCodeEnabled {
		common.ApiErrorI18n(c, i18n.MsgFeatureDisabled)
		return
	}

	userId := c.GetInt("id")

	type GenerateRequest struct {
		Remark string `json:"remark"`
		Count  int    `json:"count"`
	}
	var req GenerateRequest
	_ = c.ShouldBindJSON(&req)
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 100 {
		common.ApiErrorI18n(c, i18n.MsgInvitationCodeCountMax)
		return
	}

	codes, err := model.GenerateInvitationCodesForUser(userId, req.Count, req.Remark)
	if err != nil {
		if err.Error() == "额度不足" {
			common.ApiErrorI18n(c, i18n.MsgInvitationCodeQuotaInsufficient)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgInvitationCodeCreateFailed)
		return
	}

	common.ApiSuccess(c, gin.H{
		"count": req.Count,
		"items": codes,
	})
}

// GetMyInvitationCodes 获取用户自己生成的邀请码列表
func GetMyInvitationCodes(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	codes, total, err := model.GetMyInvitationCodes(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 让生成者能看到自己邀请码的去向（使用者与用途）
	model.AttachInvitationCodeUsers(codes)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(codes)
	common.ApiSuccess(c, pageInfo)
}

func DeleteMyUsedInvitationCodes(c *gin.Context) {
	userId := c.GetInt("id")
	rows, err := model.DeleteUsedInvitationCodesByUser(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": rows})
}

func BatchDeleteMyInvitationCodes(c *gin.Context) {
	userId := c.GetInt("id")
	var req struct {
		Ids []int `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Ids) == 0 {
		common.ApiErrorMsg(c, "请选择需要删除的邀请码")
		return
	}
	rows, err := model.DeleteInvitationCodesByUser(userId, req.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": rows})
}

// ========== 管理员接口 ==========

// GetAllInvitationCodes 获取所有邀请码列表
func GetAllInvitationCodes(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	codes, total, err := model.GetAllInvitationCodes(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.AttachInvitationCodeUsers(codes)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(codes)
	common.ApiSuccess(c, pageInfo)
}

// SearchInvitationCodes 搜索邀请码
func SearchInvitationCodes(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	codes, total, err := model.SearchInvitationCodes(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.AttachInvitationCodeUsers(codes)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(codes)
	common.ApiSuccess(c, pageInfo)
}

// AddInvitationCode 管理员批量生成邀请码
func AddInvitationCode(c *gin.Context) {
	invitationCode := model.InvitationCode{}
	err := c.ShouldBindJSON(&invitationCode)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if invitationCode.Count <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvitationCodeCountPositive)
		return
	}
	if invitationCode.Count > 100 {
		common.ApiErrorI18n(c, i18n.MsgInvitationCodeCountMax)
		return
	}

	userId := c.GetInt("id")
	totalPrice := common.InvitationCodePrice * invitationCode.Count

	// 检查并扣减额度
	if totalPrice > 0 {
		userQuota, err := model.GetUserQuota(userId, true)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if userQuota < totalPrice {
			common.ApiErrorI18n(c, i18n.MsgInvitationCodeQuotaInsufficient)
			return
		}
		err = model.DecreaseUserQuota(userId, totalPrice, true)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	var codes []string
	for j := 0; j < invitationCode.Count; j++ {
		code := common.GetUUID()
		cleanCode := model.InvitationCode{
			UserId:      userId,
			Code:        code,
			Quota:       common.InvitationCodePrice, // 记录每个邀请码消耗的额度
			Status:      common.InvitationCodeStatusEnabled,
			CreatedTime: common.GetTimestamp(),
			Remark:      invitationCode.Remark,
		}
		err = cleanCode.Insert()
		if err != nil {
			common.SysError("failed to insert invitation code: " + err.Error())
			// 退还未成功生成的部分额度
			if common.InvitationCodePrice > 0 {
				refund := common.InvitationCodePrice * (invitationCode.Count - j)
				_ = model.IncreaseUserQuota(userId, refund, true)
			}
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgInvitationCodeCreateFailed),
				"data":    codes,
			})
			return
		}
		codes = append(codes, code)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    codes,
	})
}

// DeleteInvitationCode 删除邀请码
func DeleteInvitationCode(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := model.DeleteInvitationCodeById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// UpdateInvitationCode 更新邀请码状态
func UpdateInvitationCode(c *gin.Context) {
	invitationCode := model.InvitationCode{}
	err := c.ShouldBindJSON(&invitationCode)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cleanCode, err := model.GetInvitationCodeById(invitationCode.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cleanCode.Status = invitationCode.Status
	if invitationCode.Remark != "" {
		cleanCode.Remark = invitationCode.Remark
	}
	err = cleanCode.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    cleanCode,
	})
}

// DeleteUsedInvitationCodes 批量清理已使用的邀请码
func DeleteUsedInvitationCodes(c *gin.Context) {
	rows, err := model.DeleteUsedInvitationCodes()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
}
