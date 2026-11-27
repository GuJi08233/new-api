package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func GetTradeSprints(c *gin.Context) {
	setting := operation_setting.GetTradeSetting()
	active, recent, err := service.GetTradeSprintBoard(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"enabled":        setting.Enabled && setting.SprintEnabled,
		"server_time":    common.GetTimestamp(),
		"entry_fee_usd":  setting.SprintEntryFeeUsd,
		"fee_bps":        setting.SprintFeeBps,
		"min_equity_usd": model.TradeSprintMinEquityUsd,
		"active":         active,
		"recent":         recent,
	})
}

func JoinTradeSprint(c *gin.Context) {
	var req struct {
		Kind string `json:"kind"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Kind != model.TradeSprintKind4H && req.Kind != model.TradeSprintKindDaily {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	entry, err := service.JoinCurrentTradeSprint(c.GetInt("id"), req.Kind)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrTradeSprintDisabled):
			common.ApiErrorI18n(c, i18n.MsgTradeSprintDisabled)
		case errors.Is(err, model.ErrTradeSprintAlreadyJoined):
			common.ApiErrorI18n(c, i18n.MsgTradeSprintAlreadyJoined)
		case errors.Is(err, model.ErrTradeSprintClosed):
			common.ApiErrorI18n(c, i18n.MsgTradeSprintClosed)
		case errors.Is(err, model.ErrTradeSprintEquityTooLow):
			common.ApiErrorI18n(c, i18n.MsgTradeSprintEquityTooLow)
		default:
			respondTradeOrderError(c, "", err)
		}
		return
	}
	common.ApiSuccess(c, entry)
}
