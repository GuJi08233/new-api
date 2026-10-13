package middleware

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// abortWithEntryFormatError 按入口协议输出中间件层的拒绝错误：
// /v1/messages 返回与 controller.Relay 一致的 Claude 错误结构，其余返回 OpenAI 错误结构。
func abortWithEntryFormatError(c *gin.Context, err *types.NewAPIError) {
	err.SetMessage(common.MessageWithRequestId(err.Error(), c.GetString(common.RequestIdKey)))
	if strings.HasPrefix(c.Request.URL.Path, "/v1/messages") {
		c.JSON(err.StatusCode, gin.H{"type": "error", "error": err.ToClaudeError()})
	} else {
		c.JSON(err.StatusCode, gin.H{"error": err.ToOpenAIError()})
	}
	c.Abort()
}

func abortWithOpenAiMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
	codeStr := ""
	if len(code) > 0 {
		codeStr = string(code[0])
	}
	userId := c.GetInt("id")
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":    "new_api_error",
			"code":    codeStr,
		},
	})
	c.Abort()
	logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | %s", userId, message))
}

func abortWithMidjourneyMessage(c *gin.Context, statusCode int, code int, description string) {
	c.JSON(statusCode, gin.H{
		"description": description,
		"type":        "new_api_error",
		"code":        code,
	})
	c.Abort()
	logger.LogError(c.Request.Context(), description)
}
