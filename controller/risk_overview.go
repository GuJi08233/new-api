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

func GetRiskOverview(c *gin.Context) {
	setting := operation_setting.GetRiskControlSetting()
	since := time.Now().Add(-24 * time.Hour).Unix()
	payload := gin.H{"setting": setting, "node": common.NodeName, "is_master": common.IsMasterNode,
		"runtime": service.GetRiskRuntimeStatus(), "ip_log_enabled": common.IsGlobalRecordIpLogEnabled(),
		"ua_log_enabled": common.IsGlobalRecordUaLogEnabled(), "consume_log_enabled": common.LogConsumeEnabled}
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
	coverage, err := model.GetRiskLogCoverage(since)
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
