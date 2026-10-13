package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// riskOverviewTopLimit 是概览里拦截来源与规则排行的条数。
const riskOverviewTopLimit = 8

// GetRiskOverview 返回概览:处置统计、活动趋势、拦截来源与规则排行、扫描与本节点状态。
// query: range=24h|7d(默认 24h),只影响事件统计;日志覆盖始终统计近 24 小时,避免在大日志表上做长窗口扫描。
func GetRiskOverview(c *gin.Context) {
	setting := operation_setting.GetRiskControlSetting()
	rangeKey, rangeHours := "24h", 24
	if c.Query("range") == "7d" {
		rangeKey, rangeHours = "7d", 24*7
	}
	now := time.Now()
	since := now.Add(-time.Duration(rangeHours) * time.Hour).Unix()
	payload := gin.H{"setting": setting, "node": common.NodeName, "is_master": common.IsMasterNode,
		"runtime": service.GetRiskRuntimeStatus(), "ip_log_enabled": common.IsGlobalRecordIpLogEnabled(),
		"ua_log_enabled": common.IsGlobalRecordUaLogEnabled(), "consume_log_enabled": common.LogConsumeEnabled,
		"client_ua": operation_setting.GetClientUaRuleStatus(), "range": rangeKey, "since": since,
		"bucket_seconds": model.RiskActivityBucketSeconds}
	// 独立读取主库与日志库；单个分区失败时保留其余信息，不把缺失数据当成零。
	errors := gin.H{}
	counts, err := model.GetRiskDispositionCounts()
	if err != nil {
		errors["counts"] = err.Error()
	} else {
		payload["counts"] = counts
	}
	scan, err := model.GetLatestRiskScan()
	if err != nil {
		errors["scan"] = err.Error()
	} else {
		payload["scan"] = scan
	}
	totals, err := model.GetRecentRiskEventTotals(since)
	if err != nil {
		errors["events"] = err.Error()
	} else {
		payload["events"] = totals
	}
	activity, err := model.GetRiskActivityBuckets(since)
	if err != nil {
		errors["activity"] = err.Error()
	} else {
		payload["activity"] = activity
	}
	blocked, err := model.GetRiskTopBlockedIps(since, riskOverviewTopLimit)
	if err != nil {
		errors["top_blocked_ips"] = err.Error()
	} else {
		payload["top_blocked_ips"] = blocked
	}
	rules, err := model.GetRiskTopRules(since, riskOverviewTopLimit)
	if err != nil {
		errors["top_rules"] = err.Error()
	} else {
		payload["top_rules"] = rules
	}
	pending, _, err := model.QueryRiskEvents(model.RiskEventQuery{EventType: model.RiskEventAlert,
		ReviewStatus: model.RiskReviewPending}, 0, 6)
	if err != nil {
		errors["pending"] = err.Error()
	} else {
		payload["pending"] = pending
	}
	coverage, err := model.GetRiskLogCoverage(now.Add(-24 * time.Hour).Unix())
	if err != nil {
		errors["coverage"] = err.Error()
	} else {
		payload["coverage"] = coverage
	}
	payload["errors"] = errors
	common.ApiSuccess(c, payload)
}

func GetRiskRules(c *gin.Context) {
	setting := operation_setting.GetRiskControlSetting()
	common.ApiSuccess(c, gin.H{"rules": setting.AutoBanRules})
}

func PreviewRiskRule(c *gin.Context) {
	var request struct {
		Rule          operation_setting.RiskAutoBanRule `json:"rule"`
		TinyMaxTokens int                               `json:"tiny_max_tokens"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	rules, err := common.Marshal([]operation_setting.RiskAutoBanRule{request.Rule})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := operation_setting.ValidateRiskControlOption(operation_setting.RiskControlSettingPrefix+"auto_ban_rules", string(rules)); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.TinyMaxTokens == 0 {
		request.TinyMaxTokens = operation_setting.GetRiskControlSetting().ResolvedTinyRequestMaxTokens()
	}
	if request.TinyMaxTokens < 1 || request.TinyMaxTokens > operation_setting.RiskMaxTinyRequestMaxTokens {
		common.ApiErrorMsg(c, "微量请求判定阈值无效")
		return
	}
	preview, err := service.PreviewRiskRule(request.Rule, request.TinyMaxTokens, operation_setting.GetRiskControlSetting().WhitelistUserIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, preview)
}

func ReviewRiskEvent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid event id")
		return
	}
	var request struct {
		Status     string `json:"status"`
		Note       string `json:"note"`
		AssigneeId int    `json:"assignee_id"`
		Version    int    `json:"version"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	event, err := model.ReviewRiskEvent(id, request.Version, request.Status, strings.TrimSpace(request.Note), request.AssigneeId, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "risk.event.review", map[string]interface{}{"event_id": id, "status": request.Status, "assignee_id": request.AssigneeId})
	common.ApiSuccess(c, event)
}

func GetRiskEventReviews(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid event id")
		return
	}
	event, err := model.GetRiskEventById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	reviews, err := model.GetRiskEventReviews(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"event": event, "items": reviews})
}
