package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type SubscriptionBalancePayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestBalancePay(c *gin.Context) {
	var req SubscriptionBalancePayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	userId := c.GetInt("id")
	// 与网关支付一致：全局限购需要把未完成的挂起订单也计入，
	// 事务内的兜底校验（CreateUserSubscriptionFromPlanTx）不含挂起订单。
	if err := model.CheckSubscriptionPlanPurchaseAllowed(userId, plan, true); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	if err := model.PurchaseSubscriptionWithBalance(model.ClientLogSource(c), userId, req.PlanId); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}
