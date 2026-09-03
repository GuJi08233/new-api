package middleware

import (
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ErrorGuard 在响应完成后把错误状态码交给实时错误率防护统计。
// 注册在认证之前,因此认证失败(401)、参数校验失败(400)这些不进入 relay 的
// 拒绝同样能被统计到 —— 那正是批量测活最典型的响应。
//
// 只挂在对外的 API 流量上(见 router/relay-router.go):控制台自身的请求
// (playground、管理接口)不参与,避免管理员误操作把自己的 IP 封掉。
func ErrorGuard() func(c *gin.Context) {
	return func(c *gin.Context) {
		c.Next()
		service.RecordErrorGuardResponse(c, c.Writer.Status())
	}
}
