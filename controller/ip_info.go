package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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

// ResetIpInfo 清空所有 IP 归属地缓存，下次查询会重新拉取外部接口。
// 仅超级管理员可调用（路由注册在 RootAuth 下）。
func ResetIpInfo(c *gin.Context) {
	deleted, err := model.ClearAllIpInfo()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": deleted})
}
