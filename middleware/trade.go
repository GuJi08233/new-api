package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// TradeRequestBodyMaxBytes 是模拟盘接口请求体的上限，委托与转账都只有几个字段。
const TradeRequestBodyMaxBytes = 4 << 10

// TradeEnabled 在模拟盘关闭时拒绝请求。关闭后仍然开放的操作(撤单、转出、查询)不挂这个中间件。
func TradeEnabled() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !operation_setting.GetTradeSetting().Enabled {
			common.ApiErrorI18n(c, i18n.MsgTradeDisabled)
			c.Abort()
			return
		}
		c.Next()
	}
}

// TradeRequestBodyLimit 限制模拟盘接口的请求体大小。
func TradeRequestBodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, TradeRequestBodyMaxBytes)
		c.Next()
	}
}

// TradeOrderRateLimit 按用户限制下单与撤单的频率：每笔市价单都要等一次行情连接的 ping，还要占用交易对的成交锁。
func TradeOrderRateLimit() gin.HandlerFunc {
	return userRateLimitFactory(60, 60, "TR:order")
}
