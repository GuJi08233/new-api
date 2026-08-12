package middleware

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// IpAutoLookup 在 relay 请求处理完成后，异步预取客户端 IP 的归属地信息并缓存到
// ip_infos 表，之后日志/IP 标签展示无需再等待外部接口查询。
// 仅对 RouteTag=relay 的路由生效；可通过 ip_location_setting.auto_lookup 关闭。
func IpAutoLookup() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		tag, _ := c.Get(RouteTagKey)
		if tag != "relay" || !operation_setting.GetIpLocationSetting().AutoLookup {
			return
		}
		service.ScheduleIpInfoLookup(c.ClientIP())
	}
}
