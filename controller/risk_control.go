package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
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
		Keyword:       c.Query("keyword"),
	}
	if excludeWhitelist {
		overviewQuery.ExcludeUserIds = whitelistUserIds
	}
	meta["whitelist_excluded"] = excludeWhitelist && len(whitelistUserIds) > 0
	var total int64
	if c.Query("paginated") == "true" {
		page, pageErr := riskPageQuery(c)
		if pageErr != nil {
			common.ApiError(c, pageErr)
			return
		}
		overviewQuery.Limit, overviewQuery.Offset = page.PageSize, page.GetStartIdx()
		var countErr error
		total, countErr = model.CountRiskOverview(metric, overviewQuery)
		if countErr != nil {
			common.ApiError(c, countErr)
			return
		}
	}

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
	case model.RiskMetricTokenOverview:
		items, err = model.GetTokenOverviewRanking(overviewQuery)
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
	// 日志统计与账号状态分库读取，避免管理员依据历史状态进行处置。
	switch rows := items.(type) {
	case []model.UserOverviewItem:
		ids := make([]int, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.UserId)
		}
		states, stateErr := riskUserStates(ids, c.GetInt("role"))
		if stateErr != nil {
			common.SysError("risk control: failed to load current user states: " + stateErr.Error())
			meta["state_available"] = false
		}
		meta["user_states"] = states
	case []model.TokenOverviewItem:
		ids := make([]int, 0, len(rows))
		for _, row := range rows {
			if row.UserId > 0 {
				ids = append(ids, row.UserId)
			}
		}
		states, stateErr := riskUserStates(ids, c.GetInt("role"))
		if stateErr != nil {
			common.SysError("risk control: failed to load current user states: " + stateErr.Error())
			meta["state_available"] = false
		}
		meta["user_states"] = states
	case []model.IpOverviewItem:
		ips := make([]string, 0, len(rows))
		for _, row := range rows {
			ips = append(ips, row.Ip)
		}
		states, stateErr := model.GetIpBanStates(ips)
		if stateErr != nil {
			common.SysError("risk control: failed to load current ip bans: " + stateErr.Error())
			meta["state_available"] = false
		}
		meta["ip_states"] = states
	}

	common.ApiSuccess(c, gin.H{
		"metric": metric,
		"items":  items,
		"meta":   meta,
		"total":  total,
	})
}

// GetMultiAccountRanking 返回多账号关联(一人多号)统计:关联账号数达到阈值的来源地址。
// query: hours, limit, min_users, include_requests, exclude_whitelist
//
// 纯统计接口,不触发任何处置——研判结果由管理员在页面上人工决定封禁与否。
func GetMultiAccountRanking(c *gin.Context) {
	query := buildMultiAccountQuery(c)
	var total int64
	if c.Query("paginated") == "true" {
		page, err := riskPageQuery(c)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		query.Offset, query.Limit = page.GetStartIdx(), page.PageSize
		total, err = model.CountMultiAccountRanking(query)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	items, err := model.GetMultiAccountRanking(query)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	minUsers, maxWindowHours := query.EffectiveLimits()
	common.ApiSuccess(c, gin.H{
		"items": items,
		"total": total,
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
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.UserId)
	}
	states, err := riskUserStates(ids, c.GetInt("role"))
	if err != nil {
		common.SysError("risk control: failed to load multi-account user states: " + err.Error())
	}
	for i := range items {
		items[i].CanManage = states[items[i].UserId].CanManage
		if !items[i].CanManage {
			items[i].Bindings = nil
		}
	}

	common.ApiSuccess(c, gin.H{"ip": ip, "items": items, "user_states": states, "state_available": err == nil})
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
		Keyword:         c.Query("keyword"),
	}
	if setting := operation_setting.GetRiskControlSetting(); excludeWhitelist && setting != nil {
		query.ExcludeUserIds = setting.WhitelistUserIds
	}
	return query
}

// GetRiskEvents 分页查询风控事件(拦截记录、封禁/解禁记录、告警)。
// query: event_type, category(block|ban|unban|alert), keyword, user_id, ip, ua, start_timestamp, end_timestamp,
// rule_id, review_status, assignee_id, unassigned=true, p, page_size。
// keyword 是数字时按用户 ID 查,是地址或网段时按 IP 查,其余按用户名、UA、规则与原因包含匹配。
func GetRiskEvents(c *gin.Context) {
	pageInfo, pageErr := riskPageQuery(c)
	if pageErr != nil {
		common.ApiError(c, pageErr)
		return
	}
	eventType := c.Query("event_type")
	if eventType != "" && !model.IsValidRiskEventType(eventType) {
		common.ApiErrorMsg(c, "invalid event type")
		return
	}
	category := c.Query("category")
	categoryTypes := model.RiskEventCategoryTypes(category)
	if category != "" && categoryTypes == nil {
		common.ApiErrorMsg(c, "invalid event category")
		return
	}
	userId, userErr := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	if userErr != nil || userId < 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	ip, keyword := c.Query("ip"), strings.TrimSpace(c.Query("keyword"))
	if id, err := strconv.Atoi(keyword); err == nil && id > 0 && userId == 0 {
		userId, keyword = id, ""
	} else if target, err := model.NormalizeIpBanTarget(keyword); err == nil && ip == "" {
		ip, keyword = target, ""
	}
	assigneeId, assigneeErr := strconv.Atoi(c.DefaultQuery("assignee_id", "0"))
	status := c.Query("review_status")
	if (status != "" && !model.IsValidRiskReviewStatus(status)) || assigneeErr != nil || assigneeId < 0 {
		common.ApiErrorMsg(c, "事件处理筛选条件无效")
		return
	}
	startAt, startErr := strconv.ParseInt(c.DefaultQuery("start_timestamp", "0"), 10, 64)
	endAt, endErr := strconv.ParseInt(c.DefaultQuery("end_timestamp", "0"), 10, 64)
	if startErr != nil || endErr != nil || startAt < 0 || endAt < 0 || (endAt > 0 && startAt > endAt) {
		common.ApiErrorMsg(c, "时间范围无效")
		return
	}

	events, total, err := model.QueryRiskEvents(model.RiskEventQuery{
		EventType: eventType, EventTypes: categoryTypes, UserId: userId, Ip: ip, Ua: c.Query("ua"), StartAt: startAt, EndAt: endAt,
		RuleId: c.Query("rule_id"), ReviewStatus: status, AssigneeId: assigneeId, Keyword: keyword,
		Unassigned: c.Query("unassigned") == "true",
	}, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(events)
	common.ApiSuccess(c, pageInfo)
}

func riskPageQuery(c *gin.Context) (*common.PageInfo, error) {
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.PageSize < 1 || page.Page > 1000000 {
		return nil, fmt.Errorf("分页参数无效")
	}
	return page, nil
}

// GetRiskDetail 下钻单个目标(用户 / IP / UA)的明细。
// query: type=user|ip|ua, value, hours, exclude_whitelist
//
// items 是主关联列表,内容随 type 而变(user→IP、ip→用户、ua→用户),保持既有契约;
// 另外按维度返回补充分区:uas / ips 为明细列表,errors 为错误状态码分布,timeline 为按整点的调用与错误次数。
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

	// 时间线只是辅助判读,读取失败时不影响其余明细
	if timeline, err := model.GetRiskDetailTimeline(target); err != nil {
		common.SysError("risk control: failed to load detail timeline: " + err.Error())
	} else {
		payload["timeline"] = timeline
		payload["bucket_seconds"] = model.RiskActivityBucketSeconds
	}

	common.ApiSuccess(c, payload)
}

// GetSharedSessions 返回跨账号共用会话 ID 或缓存关联键的排行与关联明细。
// query: kind(session|cache), hours, min_users, exclude_whitelist, keyword, sort_by, sort_order, p, page_size
func GetSharedSessions(c *gin.Context) {
	pageInfo, pageErr := riskPageQuery(c)
	if pageErr != nil {
		common.ApiError(c, pageErr)
		return
	}
	kind := c.DefaultQuery("kind", model.SharedSessionKindSession)
	if kind != model.SharedSessionKindSession && kind != model.SharedSessionKindCache {
		common.ApiErrorMsg(c, "无效的会话关联维度")
		return
	}
	hours, _ := strconv.Atoi(c.Query("hours"))
	minUsers, _ := strconv.Atoi(c.Query("min_users"))
	excludeWhitelist, _ := strconv.ParseBool(c.Query("exclude_whitelist"))

	setting := operation_setting.GetRiskControlSetting()
	query := model.SharedSessionQuery{
		Kind:      kind,
		Hours:     hours,
		MinUsers:  minUsers,
		Keyword:   c.Query("keyword"),
		SortBy:    c.Query("sort_by"),
		SortOrder: c.Query("sort_order"),
		Offset:    pageInfo.GetStartIdx(),
		Limit:     pageInfo.GetPageSize(),
	}
	if excludeWhitelist && setting != nil {
		query.ExcludeUserIds = setting.WhitelistUserIds
	}

	items, total, err := model.GetSharedSessionRanking(query)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	userIds := make([]int, 0)
	seen := map[int]bool{}
	for _, item := range items {
		for _, user := range item.Users {
			if user.UserId > 0 && !seen[user.UserId] {
				seen[user.UserId] = true
				userIds = append(userIds, user.UserId)
			}
		}
	}
	states, stateErr := riskUserStates(userIds, c.GetInt("role"))
	if stateErr != nil {
		common.SysError("risk control: failed to load shared session user states: " + stateErr.Error())
	}

	common.ApiSuccess(c, gin.H{
		"items":       items,
		"total":       total,
		"user_states": states,
		"meta": gin.H{
			"kind":                      kind,
			"session_recording_enabled": setting != nil && setting.SessionRiskRecordingEnabled,
			"whitelist_excluded":        excludeWhitelist && setting != nil && len(setting.WhitelistUserIds) > 0,
			"state_available":           stateErr == nil,
		},
	})
}

// PreviewProbePayloadText 用实时防护相同的归一化与精确匹配逻辑测试单轮测活规则。
func PreviewProbePayloadText(c *gin.Context) {
	var req struct {
		Text  string `json:"text"`
		Rules string `json:"rules"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024), &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if _, err := operation_setting.ParseProbePayloadTextRules(req.Rules); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Text) > 64*1024 {
		common.ApiErrorMsg(c, "测试文本不能超过 64 KiB")
		return
	}
	match := service.MatchProbePayloadText(req.Text, req.Rules)
	common.ApiSuccess(c, gin.H{"matched": match != nil, "match": match})
}

// GetProbePayloadUserState 查看单个账号的单轮内容测活累计状态。
func GetProbePayloadUserState(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	states, err := riskUserStates([]int{userId}, c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	userState, ok := states[userId]
	if !ok {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}
	probeState, err := model.GetRiskProbeUserState(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"state": probeState, "user_state": userState})
}

// ResetProbePayloadUserState 清零账号的单轮内容测活计数与待定窗口（不改变账号当前的封禁状态）。
func ResetProbePayloadUserState(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	states, err := riskUserStates([]int{userId}, c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	userState, ok := states[userId]
	if !ok {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}
	if !userState.CanManage {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "没有权限管理该账号"})
		return
	}
	if err := model.ResetRiskProbeUserState(userId); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, userId, "risk.probe_payload.reset", map[string]interface{}{"user_id": userId})
	common.ApiSuccess(c, nil)
}
