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
// query: metric(见 model.RiskMetric*), hours, limit,
// 合并视图额外支持 exclude_whitelist=true 排除风控白名单用户的数据,
// 以及 sort_by / sort_order 指定服务端排序(决定 top N 取自哪个维度)。
func GetRiskRankings(c *gin.Context) {
	metric := c.DefaultQuery("metric", model.RiskMetricIpMultiUser)
	hours, _ := strconv.Atoi(c.Query("hours"))
	limit, _ := strconv.Atoi(c.Query("limit"))

	setting := operation_setting.GetRiskControlSetting()
	whitelistUserIds := []int{}
	if setting != nil {
		whitelistUserIds = setting.WhitelistUserIds
	}
	meta := gin.H{
		"ip_log_enabled":          common.IsGlobalRecordIpLogEnabled(),
		"ua_log_enabled":          common.IsGlobalRecordUaLogEnabled(),
		"setting_enabled":         setting != nil && setting.Enabled,
		"tiny_request_max_tokens": setting.ResolvedTinyRequestMaxTokens(),
		"whitelist_count":         len(whitelistUserIds),
	}

	excludeWhitelist, _ := strconv.ParseBool(c.Query("exclude_whitelist"))
	overviewQuery := model.RiskOverviewQuery{
		Hours:         hours,
		Limit:         limit,
		TinyMaxTokens: setting.ResolvedTinyRequestMaxTokens(),
		SortBy:        c.Query("sort_by"),
		SortOrder:     c.Query("sort_order"),
	}
	if excludeWhitelist {
		overviewQuery.ExcludeUserIds = whitelistUserIds
	}
	meta["whitelist_excluded"] = excludeWhitelist && len(whitelistUserIds) > 0

	var (
		items interface{}
		err   error
	)
	switch metric {
	case model.RiskMetricUserOverview:
		items, err = model.GetUserOverviewRanking(overviewQuery)
	case model.RiskMetricIpOverview:
		items, err = model.GetIpOverviewRanking(overviewQuery)
	case model.RiskMetricUserMultiIp:
		items, err = model.GetUserMultiIpRanking(hours, limit, overviewQuery.ExcludeUserIds)
	case model.RiskMetricUa:
		items, err = model.GetUaOverviewRanking(overviewQuery)
	case model.RiskMetricIpMultiUser:
		items, err = model.GetIpMultiUserRanking(hours, limit, overviewQuery.ExcludeUserIds)
	case model.RiskMetricIpMultiToken:
		items, err = model.GetIpMultiTokenRanking(hours, limit, overviewQuery.ExcludeUserIds)
	case model.RiskMetricUserTinyRequest:
		items, err = model.GetUserTinyRequestRanking(hours, limit, setting.ResolvedTinyRequestMaxTokens(), overviewQuery.ExcludeUserIds)
	case model.RiskMetricUserErrorBurst:
		items, err = model.GetUserErrorRanking(hours, limit, overviewQuery.ExcludeUserIds)
	case model.RiskMetricTokenMultiIp:
		items, err = model.GetTokenMultiIpRanking(hours, limit, overviewQuery.ExcludeUserIds)
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

// GetMultiAccountRanking 返回多账号关联(一人多号)统计:关联账号数达到阈值的来源地址。
// query: hours, limit, min_users, include_requests, exclude_whitelist
//
// 纯统计接口,不触发任何处置——研判结果由管理员在页面上人工决定封禁与否。
func GetMultiAccountRanking(c *gin.Context) {
	query := buildMultiAccountQuery(c)
	items, err := model.GetMultiAccountRanking(query)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	minUsers, maxWindowHours := query.EffectiveLimits()
	common.ApiSuccess(c, gin.H{
		"items": items,
		"meta": gin.H{
			"ip_log_enabled":   common.IsGlobalRecordIpLogEnabled(),
			"include_requests": query.IncludeRequests,
			"min_users":        minUsers,
			"max_window_hours": maxWindowHours,
		},
	})
}

// GetMultiAccountDetail 下钻某个地址,列出它关联的账号、各自的证据构成与第三方绑定。
// query: ip, hours, include_requests, exclude_whitelist
//
// 绑定明细沿用用户管理的越权保护:管理不到的角色只出统计,不出身份信息,
// 否则普通管理员能从研判页读到 root 的绑定,绕开 /api/user/:id 上的同一道检查。
func GetMultiAccountDetail(c *gin.Context) {
	ip := c.Query("ip")
	if ip == "" {
		common.ApiErrorMsg(c, "ip is required")
		return
	}
	items, err := model.GetMultiAccountUsers(ip, buildMultiAccountQuery(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.AttachMultiAccountUserProfiles(items)
	myRole := c.GetInt("role")
	for i := range items {
		items[i].CanManage = canManageTargetRole(myRole, items[i].Role)
		if !items[i].CanManage {
			items[i].Bindings = nil
		}
	}

	common.ApiSuccess(c, gin.H{"ip": ip, "items": items})
}

func buildMultiAccountQuery(c *gin.Context) model.MultiAccountQuery {
	hours, _ := strconv.Atoi(c.Query("hours"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	minUsers, _ := strconv.Atoi(c.Query("min_users"))
	includeRequests, _ := strconv.ParseBool(c.Query("include_requests"))
	excludeWhitelist, _ := strconv.ParseBool(c.Query("exclude_whitelist"))

	query := model.MultiAccountQuery{
		Hours:           hours,
		Limit:           limit,
		MinUsers:        minUsers,
		IncludeRequests: includeRequests,
	}
	if setting := operation_setting.GetRiskControlSetting(); excludeWhitelist && setting != nil {
		query.ExcludeUserIds = setting.WhitelistUserIds
	}
	return query
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

// GetRiskDetail 下钻单个目标(用户 / IP / UA)的明细。
// query: type=user|ip|ua, value, hours, exclude_whitelist
//
// items 是主关联列表,内容随 type 而变(user→IP、ip→用户、ua→用户),保持既有契约;
// 另外按维度返回补充分区:uas / ips 为明细列表,errors 为错误状态码分布。
func GetRiskDetail(c *gin.Context) {
	hours, _ := strconv.Atoi(c.Query("hours"))
	excludeWhitelist, _ := strconv.ParseBool(c.Query("exclude_whitelist"))

	target := model.RiskDetailTarget{
		Type:  c.Query("type"),
		Value: c.Query("value"),
		Hours: hours,
	}
	if setting := operation_setting.GetRiskControlSetting(); excludeWhitelist && setting != nil {
		target.ExcludeUserIds = setting.WhitelistUserIds
	}

	payload := gin.H{"type": target.Type, "value": target.Value}

	// 主关联列表:看这个目标背后是哪些人,或这个人用了哪些地址
	var (
		items interface{}
		err   error
	)
	switch target.Type {
	case model.RiskDetailTypeUser:
		items, err = model.GetRiskDetailIps(target)
	case model.RiskDetailTypeIp, model.RiskDetailTypeUa:
		items, err = model.GetRiskDetailUsers(target)
	default:
		common.ApiErrorMsg(c, "invalid detail type")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	payload["items"] = items

	// 补充分区:UA 维度补 IP 明细,用户/IP 维度补 UA 明细
	if target.Type == model.RiskDetailTypeUa {
		ips, err := model.GetRiskDetailIps(target)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		payload["ips"] = ips
	} else {
		uas, err := model.GetRiskDetailUas(target)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		payload["uas"] = uas
	}

	errorItems, sampled, err := model.GetRiskDetailErrorStatuses(target)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	payload["errors"] = errorItems
	payload["errors_sampled"] = sampled

	common.ApiSuccess(c, payload)
}
