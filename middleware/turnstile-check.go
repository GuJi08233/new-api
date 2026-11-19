package middleware

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

var verifyTurnstileFunc = service.VerifyTurnstile

func TurnstileCheck() gin.HandlerFunc {
	return turnstileCheck(false)
}

// TurnstileCheckPerRequest enforces a fresh, single-use Turnstile token on
// every request even when the visitor already passed an earlier challenge in
// this session. Used for email-sending endpoints to prevent session-replay spam.
func TurnstileCheckPerRequest() gin.HandlerFunc {
	return turnstileCheck(true)
}

func turnstileCheck(perRequest bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled {
			if !perRequest {
				session := sessions.Default(c)
				if session.Get("turnstile") != nil {
					c.Next()
					return
				}
			}
			response := c.Query("turnstile")
			if response == "" {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile token 为空",
				})
				c.Abort()
				return
			}
			if err := verifyTurnstileFunc(response, c.ClientIP()); err != nil {
				message := "Turnstile 校验失败，请刷新重试！"
				switch {
				case errors.Is(err, service.ErrTurnstileRejected):
				case errors.Is(err, service.ErrTurnstileSiteError):
					// 站点密钥没填或填错、Cloudflare 内部出错：记进日志给管理员排查，用户看到的仍是通用提示，
					// 不把站点的配置问题暴露出去。
					common.SysLog(err.Error())
				default:
					// 连不上 Cloudflare 或回应无法解析，与以前一样把原因告诉用户。
					common.SysLog(err.Error())
					message = err.Error()
				}
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": message,
				})
				c.Abort()
				return
			}
			if !perRequest {
				session := sessions.Default(c)
				session.Set("turnstile", true)
				err := session.Save()
				if err != nil {
					c.JSON(http.StatusOK, gin.H{
						"message": "无法保存会话信息，请重试",
						"success": false,
					})
					c.Abort()
					return
				}
			}
		}
		c.Next()
	}
}
