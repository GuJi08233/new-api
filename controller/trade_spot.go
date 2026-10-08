package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// tradeSpotSettings 是现货与杠杆的规则：最高杠杆、借 USDT 与借各交易对的币的实时日利率、开仓线、追加保证金线、强平线、转出线与
// 强平费。
func tradeSpotSettings(setting *operation_setting.TradeSetting) gin.H {
	assetRates := gin.H{}
	for _, symbol := range setting.Symbols {
		if rate, ok := model.TradeSpotAssetDailyRate(model.TradeSpotBaseAsset(symbol)); ok {
			assetRates[symbol] = rate.String()
		}
	}
	return gin.H{
		"enabled":             setting.Enabled,
		"max_leverage":        setting.SpotMaxLeverage,
		"daily_rate":          model.TradeSpotDailyRate().String(),
		"asset_rates":         assetRates,
		"initial_level":       model.TradeSpotInitialLevel(setting.SpotMaxLeverage).StringFixed(2),
		"margin_call_level":   model.TradeSpotMarginCallLevel(setting.SpotMaxLeverage).StringFixed(2),
		"liquidation_level":   model.TradeSpotLiquidationLevel.StringFixed(2),
		"transfer_level":      model.TradeSpotTransferLevel.StringFixed(2),
		"liquidation_fee_bps": model.TradeSpotLiquidationFeeBps,
	}
}

func RepayTradeSpotMargin(c *gin.Context) {
	var request struct {
		Amount string `json:"amount"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
		return
	}
	amount, err := parseTradeDecimal(request.Amount)
	if err != nil || !amount.IsPositive() {
		common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
		return
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	quota, err := common.QuotaFromDecimalStrict(amount.Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil || quota <= 0 {
		common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
		return
	}
	_, err = model.RepayTradeSpotMargin(c.GetInt("id"), quota, service.TradeAccountingMarket())
	if err != nil {
		respondTradeOrderError(c, "", err)
		return
	}
	view, err := tradeSelfView(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// CoverTradeSpotAsset 买回一个交易对上借着的全部币还掉(市价按盘口买，多买的零头进持仓)，返回最新的账户视图。
func CoverTradeSpotAsset(c *gin.Context) {
	var request struct {
		Symbol string `json:"symbol"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	if _, ok := operation_setting.TradeSymbolOf(request.Symbol); !ok {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	userId := c.GetInt("id")
	if _, err := service.CoverTradeSpotAsset(c.Request.Context(), userId, request.Symbol); err != nil {
		respondTradeOrderError(c, request.Symbol, err)
		return
	}
	view, err := tradeSelfView(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}
