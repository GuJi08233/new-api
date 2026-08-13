package middleware

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// IpAutoLookup 在 relay 请求处理完成后，异步预取客户端 IP 的归属地信息并缓存到
// ip_infos 表，之后日志/IP 标签展示无需再等待外部接口查询。
// 仅对 RouteTag=relay 且认证通过的请求生效；可通过 ip_location_setting.auto_lookup 关闭。
func IpAutoLookup() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		tag, _ := c.Get(RouteTagKey)
		if tag != "relay" || !operation_setting.GetIpLocationSetting().AutoLookup {
			return
		}
		// 认证未通过的请求(401/403、匿名路由)不会产生消费日志，跳过预取，
		// 避免为扫描器噪音 IP 调用外部接口并膨胀 ip_infos 表。
		if c.GetInt("id") <= 0 {
			return
		}
		service.ScheduleIpInfoLookup(c.ClientIP())
	}
}
