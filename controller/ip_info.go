package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetIpInfo 返回某 IP 的归属地信息（优先数据库缓存，未命中时查询外部接口）。
// query: ip
func GetIpInfo(c *gin.Context) {
	ip := c.Query("ip")
	if ip == "" {
		common.ApiErrorMsg(c, "ip is required")
		return
	}
	info, err := service.LookupIpInfo(ip)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, info)
}
