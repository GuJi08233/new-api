package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func respondTradeShortError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrTradeShortDisabled):
		common.ApiErrorI18n(c, i18n.MsgTradeShortDisabled)
	case errors.Is(err, model.ErrTradeShortRoundClosed):
		common.ApiErrorI18n(c, i18n.MsgTradeShortRoundClosed)
	case errors.Is(err, model.ErrTradeShortLimitReached):
		common.ApiErrorI18n(c, i18n.MsgTradeShortLimitReached)
	case errors.Is(err, model.ErrTradeShortBonusNotAllowed):
		common.ApiErrorI18n(c, i18n.MsgTradeShortBonusNotAllowed)
	case errors.Is(err, model.ErrTradeShortHedgeForbidden):
		common.ApiErrorI18n(c, i18n.MsgTradeShortHedgeForbidden)
	case errors.Is(err, model.ErrTradeShortModeNotAllowed):
		common.ApiErrorI18n(c, i18n.MsgTradeShortModeNotAllowed)
	case errors.Is(err, model.ErrTradeShortSpreadTooWide):
		common.ApiErrorI18n(c, i18n.MsgTradeShortSpreadTooWide)
	case errors.Is(err, model.ErrTradeShortFundingWindow):
		common.ApiErrorI18n(c, i18n.MsgTradeShortFundingWindow)
	case errors.Is(err, model.ErrTradeShortInvalid):
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
	default:
		respondTradeOrderError(c, "", err)
	}
}

// GetTradeShortConfig 返回短线玩法配置、热门 PK 对决列表与共享奖池局(PK/区间/触碰)。
func GetTradeShortConfig(c *gin.Context) {
	setting := operation_setting.GetTradeSetting()
	pkDuration, _ := strconv.ParseInt(c.DefaultQuery("pk_duration", "900"), 10, 64)
	rangeDuration, _ := strconv.ParseInt(c.DefaultQuery("range_duration", "3600"), 10, 64)
	touchDuration, _ := strconv.ParseInt(c.DefaultQuery("touch_duration", "900"), 10, 64)
	pkRounds, _ := service.GetTradeShortPoolRounds(model.TradeShortKindPK, pkDuration)
	rangeRounds, _ := service.GetTradeShortPoolRounds(model.TradeShortKindRange, rangeDuration)
	touchRounds, _ := service.GetTradeShortPoolRounds(model.TradeShortKindTouch, touchDuration)

	common.ApiSuccess(c, gin.H{
		"enabled":        setting.Enabled && setting.ShortEventsEnabled,
		"server_time":    common.GetTimestamp(),
		"fee_bps":        setting.ShortEventFeeBps,
		"payout_bps":     setting.ShortEventPayoutBps,
		"max_order_usd":  setting.ShortEventMaxOrderUsd,
		"max_active_usd": setting.ShortEventMaxActiveUsd,
		"pk_matchups":    service.TradePKMatchups,
		"pk_rounds":      pkRounds,
		"range_rounds":   rangeRounds,
		"touch_rounds":   touchRounds,
	})
}

func GetTradeShortRounds(c *gin.Context) {
	kind := c.DefaultQuery("kind", model.TradeShortKindPK)
	duration, _ := strconv.ParseInt(c.DefaultQuery("duration", "900"), 10, 64)
	rounds, err := service.GetTradeShortPoolRounds(kind, duration)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"server_time": common.GetTimestamp(),
		"items":       rounds,
	})
}

func PlaceTradeShortOrder(c *gin.Context) {
	var req struct {
		Kind        string `json:"kind"`
		Mode        string `json:"mode"`
		RoundId     int    `json:"round_id"`
		SymbolA     string `json:"symbol_a"`
		SymbolB     string `json:"symbol_b"`
		Choice      string `json:"choice"`
		DurationSec int64  `json:"duration_sec"`
		Amount      string `json:"amount"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	amount, err := parseTradeDecimal(req.Amount)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	order, err := service.PlaceTradeShortOrder(c.Request.Context(), c.GetInt("id"), service.PlaceTradeShortRequest{
		Kind:        req.Kind,
		Mode:        req.Mode,
		RoundId:     req.RoundId,
		SymbolA:     req.SymbolA,
		SymbolB:     req.SymbolB,
		Choice:      req.Choice,
		DurationSec: req.DurationSec,
		AmountUsd:   amount,
	})
	if err != nil {
		respondTradeShortError(c, err)
		return
	}
	common.ApiSuccess(c, order)
}

func GetTradeShortOrders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page = max(1, min(page, 100000))
	pageSize = max(1, min(pageSize, 100))
	kind := c.Query("kind")
	activeOnly := c.Query("status") == "active"
	items, total, err := model.ListTradeShortOrders(c.GetInt("id"), kind, activeOnly, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"server_time": common.GetTimestamp(),
		"items":       items,
		"total":       total,
	})
}
