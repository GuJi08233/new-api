package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// UpdateRiskSettings 一次校验并提交风控页面的关联配置，仅允许 root 调用。
func UpdateRiskSettings(c *gin.Context) {
	var values map[string]string
	if err := common.DecodeJson(c.Request.Body, &values); err != nil || len(values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	fieldErrors := make(map[string]string)
	keys := make([]string, 0, len(values))
	for key, value := range values {
		keys = append(keys, key)
		if !strings.HasPrefix(key, operation_setting.RiskControlSettingPrefix) && !strings.HasPrefix(key, operation_setting.IpLocationSettingPrefix) {
			fieldErrors[key] = "该配置项不属于风控设置"
			continue
		}
		normalized, err := operation_setting.NormalizeRiskControlOption(key, value)
		if err != nil {
			fieldErrors[key] = err.Error()
			continue
		}
		value = normalized
		values[key] = value
		if err := operation_setting.ValidateRiskControlOption(key, value); err != nil {
			fieldErrors[key] = err.Error()
		}
		if err := operation_setting.ValidateIpLocationOption(key, value); err != nil {
			fieldErrors[key] = err.Error()
		}
	}
	if len(fieldErrors) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "风控配置校验失败", "data": gin.H{"field_errors": fieldErrors}})
		return
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}
	// 审计与响应均不回传可能包含服务商密钥的配置值。
	recordManageAudit(c, "risk.settings.update", map[string]interface{}{"keys": keys})
	applied := make(map[string]string, len(values))
	for key, value := range values {
		if key != operation_setting.IpLocationSettingPrefix+"gitee_api_key" {
			applied[key] = value
		}
	}
	common.ApiSuccess(c, gin.H{"options": applied})
}
