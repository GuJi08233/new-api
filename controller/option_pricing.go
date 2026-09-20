package controller

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func UpdateModelPricing(c *gin.Context) {
	var request struct {
		Options map[string]string `json:"options"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateModelPricingOptions(request.Options); err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidatePricingCache()
	keys := make([]string, 0, len(request.Options))
	for key := range request.Options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	recordManageAudit(c, "option.update", map[string]interface{}{"key": strings.Join(keys, ", ")})
	common.ApiSuccess(c, nil)
}
