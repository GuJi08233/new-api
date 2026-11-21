package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type SubscriptionGameCoinPayRequest struct {
	PlanId int `json:"plan_id"`
}

// SubscriptionRequestGameCoinPay 使用游戏币购买订阅套餐(1 游戏币 = 1 美元)。
func SubscriptionRequestGameCoinPay(c *gin.Context) {
	var req SubscriptionGameCoinPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	gameSetting := operation_setting.GetGameSetting()
	if !gameSetting.Enabled || !gameSetting.SubscriptionEnabled {
		common.ApiErrorMsg(c, "当前未开放游戏币购买订阅")
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.AllowsPaymentMethod(model.SubscriptionPayMethodGameCoin) {
		common.ApiErrorMsg(c, "该套餐不支持此支付方式")
		return
	}

	userId := c.GetInt("id")
	if err := model.CheckSubscriptionPlanPurchaseAllowed(userId, plan, true); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	balance, err := model.PurchaseSubscriptionWithGameCoins(model.ClientLogSource(c), userId, req.PlanId)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrGameCoinInsufficient):
			common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
		case errors.Is(err, model.ErrGameCoinLockedWager):
			common.ApiErrorI18n(c, i18n.MsgGameCoinLockedWager)
		default:
			common.ApiErrorMsg(c, err.Error())
		}
		return
	}
	common.ApiSuccess(c, gin.H{"balance": balance})
}
