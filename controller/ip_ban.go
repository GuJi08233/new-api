package controller

import (
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// GetIpBans 分页查询动态 IP 封禁列表。
// query: keyword, p, page_size
func GetIpBans(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	bans, total, err := model.GetIpBans(c.Query("keyword"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(bans)
	common.ApiSuccess(c, pageInfo)
}

type addIpBanRequest struct {
	Target        string `json:"target"`
	Reason        string `json:"reason"`
	ExpireMinutes int    `json:"expire_minutes"` // 0 表示永久
}

// AddIpBan 手动添加一条 IP 封禁,立即生效并写入 ban_ip 风控事件。
func AddIpBan(c *gin.Context) {
	var req addIpBanRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	target, err := model.NormalizeIpBanTarget(req.Target)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if req.ExpireMinutes < 0 || req.ExpireMinutes > operation_setting.RiskMaxIpBanMinutes {
		common.ApiErrorMsg(c, "封禁时长无效")
		return
	}
	// 防呆:禁止封禁当前请求来源 IP,避免管理员把自己锁在门外。
	if model.IpBanTargetCovers(target, c.ClientIP()) {
		common.ApiErrorMsg(c, "不能封禁当前请求来源的 IP")
		return
	}

	var expiresAt int64
	if req.ExpireMinutes > 0 {
		expiresAt = time.Now().Add(time.Duration(req.ExpireMinutes) * time.Minute).Unix()
	}
	reason := strings.TrimSpace(req.Reason)
	operatorId := c.GetInt("id")

	ban, changed, err := model.UpsertIpBan(target, reason, expiresAt, model.IpBanSourceManual, operatorId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if changed {
		if err := model.InsertRiskEvent(&model.RiskEvent{
			EventType:  model.RiskEventBanIp,
			Ip:         target,
			Rule:       model.IpBanSourceManual,
			Reason:     reason,
			OperatorId: operatorId,
		}); err != nil {
			common.SysLog("failed to record ip ban event: " + err.Error())
		}
	}
	common.ApiSuccess(c, ban)
}

// DeleteIpBan 解除一条 IP 封禁并写入 unban_ip 风控事件。
func DeleteIpBan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid ban id")
		return
	}
	ban, err := model.GetIpBanById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteIpBanById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InsertRiskEvent(&model.RiskEvent{
		EventType:  model.RiskEventUnbanIp,
		Ip:         ban.Target,
		Rule:       ban.Source,
		Reason:     ban.Reason,
		OperatorId: c.GetInt("id"),
	}); err != nil {
		common.SysLog("failed to record ip unban event: " + err.Error())
	}
	common.ApiSuccess(c, nil)
}
