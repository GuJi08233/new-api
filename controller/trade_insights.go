package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetTradeInsights(c *gin.Context) {
	view, exists := service.GetTradeInsights(c.Request.Context(), c.Param("kind"))
	if !exists {
		c.Status(http.StatusNotFound)
		return
	}
	common.ApiSuccess(c, view)
}
