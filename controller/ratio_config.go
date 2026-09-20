package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetRatioConfig(c *gin.Context) {
	if !ratio_setting.IsExposeRatioEnabled() {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "倍率配置接口未启用",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    ratio_setting.GetExposedData(),
	})
}

// SyncGroupPricingRequest 同步分组定价配置请求
type SyncGroupPricingRequest struct {
	SourceGroup  string   `json:"source_group"`
	TargetGroups []string `json:"target_groups"`
	ModelNames   []string `json:"model_names,omitempty"` // 可选：只同步指定的模型
	FromGlobal   bool     `json:"from_global,omitempty"` // 是否从全局配置同步
}

// SyncGroupPricing 同步分组定价配置
func SyncGroupPricing(c *gin.Context) {
	var req SyncGroupPricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}

	if req.SourceGroup == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "源分组不能为空",
		})
		return
	}

	if len(req.TargetGroups) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "目标分组不能为空",
		})
		return
	}

	// 检查源分组是否有配置（从全局同步时跳过此检查）
	if !req.FromGlobal && req.SourceGroup != "global" && !ratio_setting.HasGroupPricingConfig(req.SourceGroup) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "源分组没有定价配置",
		})
		return
	}

	// 计算、校验、落库和运行时发布在同一写入锁内完成，失败时不留下半同步状态。
	if err := model.SyncGroupPricingOptions(req.SourceGroup, req.TargetGroups, req.ModelNames, req.FromGlobal); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "同步失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "同步成功",
	})
}
