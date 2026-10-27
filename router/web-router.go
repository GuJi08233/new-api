package router

import (
	"embed"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded frontend assets.
type WebAssets struct {
	BuildFS   embed.FS
	IndexPage []byte
}

// SetWebRouter serves the dashboard on unmatched paths. Task plugin routes
// are tried first so plugin-declared vendor APIs bypass the web middlewares.
func SetWebRouter(router *gin.Engine, assets WebAssets, pluginDispatcher gin.HandlerFunc) {
	webFS := common.EmbedFolder(assets.BuildFS, "web/classic/dist")

	router.NoRoute(pluginDispatcher, gzip.Gzip(gzip.DefaultCompression), middleware.GlobalWebRateLimit(), middleware.Cache(), static.Serve("/", webFS), func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") {
			controller.RelayNotFound(c)
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", assets.IndexPage)
	})
}
