package middleware

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled {
			session := sessions.Default(c)
			turnstileChecked := session.Get("turnstile")
			if turnstileChecked != nil {
				c.Next()
				return
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
			if err := service.VerifyTurnstile(response, c.ClientIP()); err != nil {
				message := "Turnstile 校验失败，请刷新重试！"
				if !errors.Is(err, service.ErrTurnstileRejected) {
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
		c.Next()
	}
}
