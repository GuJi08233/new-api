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

// riskBanHitsWindow 是封禁列表附带的近期拦截次数的统计窗口。
const riskBanHitsWindow = 24 * time.Hour

// GetIpBans 分页查询动态 IP 封禁列表,每条附带近 24 小时被它拦截的请求次数。
// query: keyword, status(active|expired|permanent), source, p, page_size
func GetIpBans(c *gin.Context) {
	pageInfo, pageErr := riskPageQuery(c)
	if pageErr != nil {
		common.ApiError(c, pageErr)
		return
	}
	filter := model.IpBanQuery{Keyword: c.Query("keyword"), Status: c.Query("status"), Source: c.Query("source")}
	switch filter.Status {
	case "", model.IpBanStatusActive, model.IpBanStatusExpired, model.IpBanStatusPermanent:
	default:
		common.ApiErrorMsg(c, "封禁状态筛选无效")
		return
	}
	bans, total, err := model.GetIpBans(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	targets := make([]string, 0, len(bans))
	banIds := make([]int, 0, len(bans))
	for _, ban := range bans {
		targets = append(targets, ban.Target)
		banIds = append(banIds, ban.Id)
	}
	if hits, err := model.GetRecentBanBlockHits(targets, time.Now().Add(-riskBanHitsWindow).Unix()); err != nil {
		common.SysError("risk control: failed to load ip ban hits: " + err.Error())
	} else {
		for _, ban := range bans {
			ban.RecentHits = hits[ban.Target]
		}
	}
	if counts, err := model.GetIpBanRelatedUserCounts(banIds); err != nil {
		common.SysError("risk control: failed to load ip ban related user counts: " + err.Error())
	} else {
		for _, ban := range bans {
			ban.RelatedUsers = counts[ban.Id]
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(bans)
	common.ApiSuccess(c, pageInfo)
}

type addIpBanRequest struct {
	Target        string `json:"target"`
	Reason        string `json:"reason"`
	ExpireMinutes int    `json:"expire_minutes"` // 0 表示永久
	AutoBanUser   bool   `json:"auto_ban_user"`
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

	ban, changed, err := model.UpsertIpBan(target, reason, expiresAt, model.RiskBanSourceManual, operatorId, req.AutoBanUser)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if changed {
		if err := model.InsertRiskEvent(&model.RiskEvent{
			EventType:  model.RiskEventBanIp,
			Ip:         target,
			Rule:       model.RiskBanSourceManual,
			Reason:     reason,
			OperatorId: operatorId,
		}); err != nil {
			common.SysLog("failed to record ip ban event: " + err.Error())
		}
	}
	common.ApiSuccess(c, ban)
}

type updateIpBanRequest struct {
	Reason        string `json:"reason"`
	ExpireMinutes int    `json:"expire_minutes"` // 0 表示永久
	AutoBanUser   bool   `json:"auto_ban_user"`
}

// UpdateIpBan 编辑已有 IP 封禁的原因、期限与连带封号开关。
func UpdateIpBan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid ban id")
		return
	}
	var req updateIpBanRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if req.ExpireMinutes < 0 || req.ExpireMinutes > operation_setting.RiskMaxIpBanMinutes {
		common.ApiErrorMsg(c, "封禁时长无效")
		return
	}
	var expiresAt int64
	if req.ExpireMinutes > 0 {
		expiresAt = time.Now().Add(time.Duration(req.ExpireMinutes) * time.Minute).Unix()
	}
	ban, err := model.UpdateIpBanById(id, req.Reason, expiresAt, req.AutoBanUser)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "risk.ip_ban.update", map[string]interface{}{
		"ban_id":        ban.Id,
		"target":        ban.Target,
		"expires_at":    ban.ExpiresAt,
		"auto_ban_user": ban.AutoBanUser,
		"reason":        ban.Reason,
	})
	common.ApiSuccess(c, ban)
}

// GetIpBanRelatedUsers 列出某条 IP 封禁规则连带禁用的账号及当前主库状态。
func GetIpBanRelatedUsers(c *gin.Context) {
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
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := model.GetIpBanRelatedUsers(id, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	userIds := make([]int, 0, len(items))
	for _, item := range items {
		if item.UserId > 0 {
			userIds = append(userIds, item.UserId)
		}
	}
	states, stateErr := riskUserStates(userIds, c.GetInt("role"))
	if stateErr != nil {
		common.SysError("risk control: failed to load ip ban related user states: " + stateErr.Error())
	}
	common.ApiSuccess(c, gin.H{
		"ban":             ban,
		"items":           items,
		"user_states":     states,
		"state_available": stateErr == nil,
	})
}

// DeleteIpBan 解除一条 IP 封禁并写入 unban_ip 风控事件。
func DeleteIpBan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid ban id")
		return
	}
	if _, err := model.ReleaseRiskIpBan(id, c.GetInt("id"), ""); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
