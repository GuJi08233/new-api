package router

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetRouter(router *gin.Engine, assets WebAssets) {
	// WebSocket relay must not pass through gzip middleware. Session auth keeps
	// the proxy from being an open relay: it is only reachable by logged-in users
	// paying on-chain. UserAuth cannot be used here because a browser WebSocket
	// handshake carries the session cookie but no custom headers.
	router.GET("/api/walletconnect/relay", middleware.RouteTag("api"), middleware.GlobalAPIRateLimit(), middleware.WebSocketUserAuth(), controller.WalletConnectRelayProxy)
	// 模拟盘的行情推送是长连接 SSE，同样不能经过 gzip：压缩器会攒着数据不发。
	router.GET("/api/trade/stream", middleware.RouteTag("api"), middleware.GlobalAPIRateLimit(), middleware.UserAuth(), middleware.IpBanGuardApi(), controller.StreamTradeMarket)
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetVideoRouter(router)
	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")
	if common.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		SetWebRouter(router, assets)
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(func(c *gin.Context) {
			c.Set(middleware.RouteTagKey, "web")
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
		})
	}
}
