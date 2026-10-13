package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type riskBatchTarget struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type riskBatchRequest struct {
	Action        string            `json:"action"`
	Targets       []riskBatchTarget `json:"targets"`
	Reason        string            `json:"reason"`
	ExpireMinutes int               `json:"expire_minutes"`
	Preview       bool              `json:"preview"`
}

type riskBatchResult struct {
	Target  riskBatchTarget `json:"target"`
	Allowed bool            `json:"allowed"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	State   interface{}     `json:"state,omitempty"`
}

func ManageRiskBatch(c *gin.Context) {
	var request riskBatchRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Targets) == 0 || len(request.Targets) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "批量操作每次支持 1 到 100 个目标"})
		return
	}
	switch request.Action {
	case "disable_user", "enable_user", "ban_ip", "unban_ip":
	case "whitelist_global", "whitelist_account":
		if c.GetInt("role") != common.RoleRootUser {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "仅 root 可以修改风控豁免名单"})
			return
		}
	default:
		common.ApiErrorMsg(c, "批量处置动作无效")
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if (!request.Preview && request.Reason == "") || utf8.RuneCountInString(request.Reason) > 512 || request.ExpireMinutes < 0 || request.ExpireMinutes > operation_setting.RiskMaxIpBanMinutes {
		common.ApiErrorMsg(c, "请填写不超过 512 字的原因，并检查封禁时长")
		return
	}
	results := make([]riskBatchResult, 0, len(request.Targets))
	seen := map[string]bool{}
	for _, target := range request.Targets {
		if target.Type == "ip" {
			if normalized, err := model.NormalizeIpBanTarget(target.Value); err == nil {
				target.Value = normalized
			}
		} else if target.Type == "user" || target.Type == "ip_ban" {
			if id, err := strconv.Atoi(target.Value); err == nil && id > 0 {
				target.Value = strconv.Itoa(id)
			}
		}
		key := target.Type + "\x00" + target.Value
		if seen[key] {
			results = append(results, riskBatchResult{Target: target, Status: "skipped", Message: "目标重复"})
			continue
		}
		seen[key] = true
		result := previewRiskBatchTarget(c, target, request.Action)
		if !request.Preview && result.Allowed {
			changed, err := applyRiskBatchTarget(c, request, result)
			if err != nil {
				result.Status, result.Message = "failed", err.Error()
			} else if !changed {
				result.Status, result.Message = "skipped", "已是目标状态"
			} else {
				result.Status, result.Message = "success", "操作成功"
			}
		}
		results = append(results, result)
	}
	common.ApiSuccess(c, gin.H{"results": results, "preview": request.Preview})
}

func previewRiskBatchTarget(c *gin.Context, target riskBatchTarget, action string) riskBatchResult {
	result := riskBatchResult{Target: target, Status: "failed"}
	switch target.Type {
	case "user":
		if action != "disable_user" && action != "enable_user" && action != "whitelist_global" && action != "whitelist_account" {
			result.Message = "动作与目标类型不匹配"
			return result
		}
		id, err := strconv.Atoi(target.Value)
		if err != nil || id <= 0 {
			result.Message = "无效的用户 ID"
			return result
		}
		states, err := riskUserStates([]int{id}, c.GetInt("role"))
		if err != nil {
			result.Message = err.Error()
			return result
		}
		state, exists := states[id]
		if !exists {
			result.Message = "用户不存在"
			return result
		}
		result.State = state
		if !state.CanManage {
			result.Message = "没有权限管理该账号"
			return result
		}
		if action == "disable_user" && state.Role == common.RoleRootUser {
			result.Message = "不能禁用 root 账号"
			return result
		}
		if (action == "disable_user" && !state.CanDisable) || (action == "enable_user" && !state.CanEnable) {
			result.Status, result.Message = "skipped", "已是目标状态"
			return result
		}
	case "ip":
		if action != "ban_ip" {
			result.Message = "动作与目标类型不匹配"
			return result
		}
		normalized, err := model.NormalizeIpBanTarget(target.Value)
		if err != nil {
			result.Message = err.Error()
			return result
		}
		result.Target.Value = normalized
		if model.IpBanTargetCovers(normalized, c.ClientIP()) {
			result.Message = "不能封禁当前请求来源的 IP"
			return result
		}
		states, err := model.GetIpBanStates([]string{normalized})
		if err != nil {
			result.Message = err.Error()
			return result
		}
		result.State = states[normalized]
	case "ip_ban":
		if action != "unban_ip" {
			result.Message = "动作与目标类型不匹配"
			return result
		}
		id, err := strconv.Atoi(target.Value)
		if err != nil || id <= 0 {
			result.Message = "无效的封禁 ID"
			return result
		}
		ban, err := model.GetIpBanById(id)
		if err != nil {
			result.Message = "封禁记录不存在"
			return result
		}
		result.State = ban
		if ban.ExpiresAt > 0 && ban.ExpiresAt <= common.GetTimestamp() {
			result.Status, result.Message = "skipped", "封禁已过期"
			return result
		}
	default:
		result.Message = "目标类型无效"
		return result
	}
	result.Allowed, result.Status, result.Message = true, "ready", "可以执行"
	return result
}

func applyRiskBatchTarget(c *gin.Context, request riskBatchRequest, result riskBatchResult) (bool, error) {
	target := result.Target
	operatorId := c.GetInt("id")
	switch target.Type {
	case "user":
		state := result.State.(model.RiskUserState)
		if request.Action == "whitelist_global" || request.Action == "whitelist_account" {
			changed, err := model.AddRiskExemptUser(state.Id, state.Role, request.Action == "whitelist_account")
			if err == nil && changed {
				recordManageAuditFor(c, state.Id, "risk.exemption.add", map[string]interface{}{"scope": request.Action, "reason": request.Reason})
			}
			return changed, err
		}
		status, eventType := common.UserStatusDisabled, model.RiskEventBanManual
		if request.Action == "enable_user" {
			status, eventType = common.UserStatusEnabled, model.RiskEventUnban
		}
		if err := model.DB.Transaction(func(tx *gorm.DB) error {
			return model.SetManualUserRiskState(tx, state.Id, state.Role, status, request.Reason)
		}); err != nil {
			return false, err
		}
		if err := model.InvalidateUserCache(state.Id); err != nil {
			common.SysError(err.Error())
		}
		if err := model.InvalidateUserTokensCache(state.Id); err != nil {
			common.SysError(err.Error())
		}
		if err := model.InsertRiskEvent(&model.RiskEvent{EventType: eventType, UserId: state.Id, Username: state.Username, Reason: request.Reason, OperatorId: operatorId}); err != nil {
			common.SysError("failed to record risk event: " + err.Error())
		}
		recordManageAuditFor(c, state.Id, "risk.batch.user", map[string]interface{}{"action": request.Action, "reason": request.Reason})
		return true, nil
	case "ip":
		var expiresAt int64
		if request.ExpireMinutes > 0 {
			expiresAt = time.Now().Add(time.Duration(request.ExpireMinutes) * time.Minute).Unix()
		}
		_, changed, err := model.UpsertIpBan(target.Value, request.Reason, expiresAt, model.RiskBanSourceManual, operatorId)
		if err != nil {
			return false, err
		}
		if changed {
			if err := model.InsertRiskEvent(&model.RiskEvent{EventType: model.RiskEventBanIp, Ip: target.Value, Rule: model.RiskBanSourceManual, Reason: request.Reason, OperatorId: operatorId}); err != nil {
				common.SysError("failed to record ip ban: " + err.Error())
			}
			recordManageAudit(c, "risk.batch.ip_ban", map[string]interface{}{"target": target.Value, "expires_at": expiresAt, "reason": request.Reason})
		}
		return changed, nil
	case "ip_ban":
		ban := result.State.(*model.IpBan)
		if _, err := model.ReleaseRiskIpBan(ban.Id, operatorId, request.Reason); err != nil {
			return false, err
		}
		recordManageAudit(c, "risk.batch.ip_unban", map[string]interface{}{"ban_id": ban.Id, "target": ban.Target, "reason": request.Reason})
		return true, nil
	}
	return false, fmt.Errorf("目标类型无效: %s", target.Type)
}
