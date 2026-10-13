package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// MatchClientUaRule 返回本次请求违反的客户端 UA 准入规则，无副作用。
// channelID 为 0 时只评估未绑定渠道的规则(选路前)，大于 0 时只评估绑定了该渠道的规则。
// 管理端 Playground 走浏览器会话而非 API 客户端，不受客户端规则约束。
func MatchClientUaRule(c *gin.Context, modelName string, channelID int) *operation_setting.ClientUaRule {
	if c == nil || c.Request == nil || strings.HasPrefix(c.Request.URL.Path, "/pg/") {
		return nil
	}
	return operation_setting.FindViolatedClientUaRule(operation_setting.ClientUaRequest{
		Model:     modelName,
		Group:     getChannelRouteMatchGroup(&RetryParam{Ctx: c}),
		Path:      c.Request.URL.Path,
		ChannelID: channelID,
		UserAgent: c.Request.UserAgent(),
	})
}

// RejectByClientUaRule 在请求违反客户端 UA 准入规则时返回不可重试的拒绝错误，
// 同时打上风控标记(Error Guard 不计入 IP 封禁)并写入聚合限流的风控拦截事件。
func RejectByClientUaRule(c *gin.Context, modelName string, channelID int) *types.NewAPIError {
	rule := MatchClientUaRule(c, modelName, channelID)
	if rule == nil {
		return nil
	}
	userId := c.GetInt("id")
	ua := c.Request.UserAgent()
	logger.LogWarn(c, fmt.Sprintf("client ua rule %q rejected user %d, model=%q, channel=%d, ua=%q", rule.Name, userId, modelName, channelID, ua))
	recordBlockEvent(model.RiskEventBlockClientUa, userId, c.ClientIP(), ua, rule.Name)
	markRiskBlocked(c)

	message := strings.TrimSpace(rule.Message)
	if message == "" {
		message = i18n.T(c, i18n.MsgDistributorClientUaRejected, map[string]any{"Model": modelName, "Rule": rule.Name})
	}
	statusCode := rule.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusForbidden
	}
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeAccessDenied, statusCode, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}
