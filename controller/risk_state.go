package controller

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func riskUserStates(userIds []int, operatorRole int) (map[int]model.RiskUserState, error) {
	states, err := model.GetRiskUserStates(userIds)
	if err != nil {
		return nil, err
	}
	setting := operation_setting.GetRiskControlSetting()
	for id, state := range states {
		state.CanManage = canManageTargetRole(operatorRole, state.Role)
		state.CanDisable = state.CanManage && state.Role != common.RoleRootUser && (state.Status != common.UserStatusDisabled || state.DisableExpiresAt > 0)
		state.CanEnable = state.CanManage && state.Status == common.UserStatusDisabled
		state.AccountExempt = state.Role >= common.RoleAdminUser
		if setting != nil {
			for _, whitelistedId := range setting.WhitelistUserIds {
				state.Whitelisted = state.Whitelisted || whitelistedId == id
			}
			state.AccountExempt = state.AccountExempt || state.Whitelisted
			for _, exemptId := range setting.PublicKeyUserIds {
				state.AccountExempt = state.AccountExempt || exemptId == id
			}
		}
		states[id] = state
	}
	return states, nil
}

// GetRiskState 返回当前处置状态，账号与 IP 封禁均从主库读取。
func GetRiskState(c *gin.Context) {
	subjectType, value := c.Query("type"), c.Query("value")
	payload := gin.H{"type": subjectType, "value": value}
	switch subjectType {
	case model.RiskDetailTypeUser:
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			common.ApiErrorMsg(c, "无效的用户 ID")
			return
		}
		states, err := riskUserStates([]int{id}, c.GetInt("role"))
		if err != nil {
			common.ApiError(c, err)
			return
		}
		state, exists := states[id]
		if !exists {
			common.ApiErrorMsg(c, "用户不存在")
			return
		}
		payload["user"] = state
	case model.RiskDetailTypeIp:
		target, err := model.NormalizeIpBanTarget(value)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		states, err := model.GetIpBanStates([]string{target})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		payload["value"], payload["ip_bans"] = target, states[target]
		blocked, rule := service.MatchIpBlacklist(target)
		payload["blacklist_blocked"], payload["blacklist_rule"] = blocked, rule
		payload["is_current_ip"] = model.IpBanTargetCovers(target, c.ClientIP())
	case model.RiskDetailTypeUa:
		if value == "" {
			common.ApiErrorMsg(c, "User-Agent 不能为空")
			return
		}
		blocked, rule := service.MatchUaBlacklist(value)
		payload["blacklist_blocked"], payload["blacklist_rule"] = blocked, rule
	default:
		common.ApiErrorMsg(c, fmt.Sprintf("invalid detail type: %s", subjectType))
		return
	}
	common.ApiSuccess(c, payload)
}
