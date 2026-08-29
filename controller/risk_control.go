package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// GetRiskRankings 返回风控排行榜。
// query: metric=ip_multi_user|user_multi_ip|ua, hours, limit
func GetRiskRankings(c *gin.Context) {
	metric := c.DefaultQuery("metric", model.RiskMetricIpMultiUser)
	hours, _ := strconv.Atoi(c.Query("hours"))
	limit, _ := strconv.Atoi(c.Query("limit"))

	setting := operation_setting.GetRiskControlSetting()
	meta := gin.H{
		"ip_log_enabled":          common.IsGlobalRecordIpLogEnabled(),
		"ua_log_enabled":          common.IsGlobalRecordUaLogEnabled(),
		"setting_enabled":         setting != nil && setting.Enabled,
		"tiny_request_max_tokens": setting.ResolvedTinyRequestMaxTokens(),
	}

	var (
		items interface{}
		err   error
	)
	switch metric {
	case model.RiskMetricUserMultiIp:
		items, err = model.GetUserMultiIpRanking(hours, limit)
	case model.RiskMetricUa:
		items, err = model.GetUaRanking(hours, limit)
	case model.RiskMetricIpMultiUser:
		items, err = model.GetIpMultiUserRanking(hours, limit)
	case model.RiskMetricIpMultiToken:
		items, err = model.GetIpMultiTokenRanking(hours, limit)
	case model.RiskMetricUserTinyRequest:
		items, err = model.GetUserTinyRequestRanking(hours, limit, setting.ResolvedTinyRequestMaxTokens())
	case model.RiskMetricUserErrorBurst:
		items, err = model.GetUserErrorRanking(hours, limit)
	case model.RiskMetricTokenMultiIp:
		items, err = model.GetTokenMultiIpRanking(hours, limit)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid risk metric",
		})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"metric": metric,
		"items":  items,
		"meta":   meta,
	})
}

// GetRiskEvents 分页查询风控事件(拦截记录、封禁/解禁记录、告警)。
// query: event_type, user_id, ip, p, page_size
func GetRiskEvents(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	eventType := c.Query("event_type")
	if eventType != "" && !model.IsValidRiskEventType(eventType) {
		common.ApiErrorMsg(c, "invalid event type")
		return
	}
	userId, _ := strconv.Atoi(c.Query("user_id"))
	ip := c.Query("ip")

	events, total, err := model.GetRiskEvents(eventType, userId, ip, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(events)
	common.ApiSuccess(c, pageInfo)
}

// GetRiskDetail 下钻某 IP 关联的用户明细,或某用户使用的 IP 明细。
// query: type=ip|user, value, hours
func GetRiskDetail(c *gin.Context) {
	detailType := c.Query("type")
	value := c.Query("value")
	hours, _ := strconv.Atoi(c.Query("hours"))

	switch detailType {
	case "ip":
		if value == "" {
			common.ApiErrorMsg(c, "ip is required")
			return
		}
		items, err := model.GetIpUserDetail(value, hours)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"type": "ip", "value": value, "items": items})
	case "user":
		userId, convErr := strconv.Atoi(value)
		if convErr != nil || userId <= 0 {
			common.ApiErrorMsg(c, "invalid user id")
			return
		}
		items, err := model.GetUserIpDetail(userId, hours)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"type": "user", "value": value, "items": items})
	default:
		common.ApiErrorMsg(c, "invalid detail type")
	}
}
