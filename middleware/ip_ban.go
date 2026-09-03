package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// 动态 IP 封禁的完整校验在 Distribute 里执行,但有两类入口不经过它:
// 只挂了 TokenAuth 的模型列表路由,以及注册、验证码这类未认证入口——
// 被封禁的地址照样能在那里刷号、刷邮件。以下两个中间件补上这段作用域,
// 只做封禁匹配(纯内存,未命中零开销),不涉及黑名单与实时防护。
//
// 有意不挂在登录与找回密码上:那是出问题时把自己救回来的路径,
// 封禁不该把管理员锁在控制台外面。

// IpBanGuard 用于中转路由,返回 OpenAI 风格的错误体。
// 应注册在 TokenAuth 之后,这样全局白名单账号能凭身份豁免。
func IpBanGuard() func(c *gin.Context) {
	return func(c *gin.Context) {
		if !service.CheckIpBan(c) {
			c.Next()
			return
		}
		abortWithOpenAiMessage(c, http.StatusForbidden,
			i18n.T(c, i18n.MsgRiskControlBlocked), types.ErrorCodeAccessDenied)
	}
}

// IpBanGuardApi 用于控制台接口,返回 {success, message} 响应体。
func IpBanGuardApi() func(c *gin.Context) {
	return func(c *gin.Context) {
		if !service.CheckIpBan(c) {
			c.Next()
			return
		}
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgRiskControlBlocked),
		})
		c.Abort()
	}
}
