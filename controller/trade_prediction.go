package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func tradePredictionPositionView(position model.TradePredictionPosition) gin.H {
	return gin.H{"id": position.Id, "window_start": position.WindowStart, "side": position.Side,
		"shares": tradeQty(position.Qty), "initial_shares": tradeQty(position.InitialQty), "cost": position.Cost, "total_cost": position.TotalCost,
		"payout": position.Payout, "avg_price": position.AvgPrice, "status": position.Status, "created_at": position.CreatedAt, "updated_at": position.UpdatedAt}
}

func respondTradePredictionError(c *gin.Context, err error) {
	code := "prediction_order_failed"
	message := i18n.T(c, i18n.MsgTradeOrderFailed)
	switch {
	case errors.Is(err, model.ErrTradePredictionClosed):
		code = "prediction_round_closed"
		message = i18n.T(c, i18n.MsgTradeOrderNotOpen)
	case errors.Is(err, model.ErrTradePredictionQuote):
		code = "prediction_quote_unavailable"
		message = i18n.T(c, i18n.MsgTradeMarketUnavailable)
	case errors.Is(err, model.ErrTradePredictionOutcome):
		code = "prediction_outcome_pending"
		message = i18n.T(c, i18n.MsgTradeMarketUnavailable)
	case errors.Is(err, service.ErrTradeQtyTooSmall):
		message = i18n.T(c, i18n.MsgTradePredictionTooSmall)
	case errors.Is(err, service.ErrTradeNoLiquidity):
		message = i18n.T(c, i18n.MsgTradePredictionNoFill)
	default:
		message = tradeOrderErrorMessage(c, "", err)
	}
	c.JSON(http.StatusOK, gin.H{"success": false, "message": message, "code": code})
}

func GetTradePredictionMarket(c *gin.Context) {
	common.ApiSuccess(c, service.GetTradePredictionMarket(c.Request.Context()))
}

func BuyTradePrediction(c *gin.Context) {
	var request struct {
		WindowStart int64  `json:"window_start"`
		Side        string `json:"side"`
		Amount      string `json:"amount"`
		MaxPrice    string `json:"max_price"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	amount, amountErr := parseTradeDecimal(request.Amount)
	price, priceErr := parseTradeDecimal(request.MaxPrice)
	if amountErr != nil || priceErr != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	position, err := service.BuyTradePrediction(c.Request.Context(), c.GetInt("id"), service.TradePredictionOrderRequest{WindowStart: request.WindowStart, Side: request.Side, Amount: amount, LimitPrice: price})
	if err != nil {
		respondTradePredictionError(c, err)
		return
	}
	common.ApiSuccess(c, tradePredictionPositionView(*position))
}

func SellTradePrediction(c *gin.Context) {
	positionId, err := strconv.Atoi(c.Param("id"))
	if err != nil || positionId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	var request struct {
		Shares   string `json:"shares"`
		MinPrice string `json:"min_price"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	shares, sharesErr := parseTradeDecimal(request.Shares)
	price, priceErr := parseTradeDecimal(request.MinPrice)
	if sharesErr != nil || priceErr != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	position, err := service.SellTradePrediction(c.Request.Context(), c.GetInt("id"), positionId, shares, price)
	if err != nil {
		respondTradePredictionError(c, err)
		return
	}
	common.ApiSuccess(c, tradePredictionPositionView(*position))
}

func GetTradePredictionPositions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page = max(1, min(page, 100000))
	pageSize = max(1, min(pageSize, 100))
	rows, total, err := model.ListTradePredictionPositions(c.GetInt("id"), c.Query("status") != "all", (page-1)*pageSize, pageSize)
	if err != nil {
		respondTradePredictionError(c, err)
		return
	}
	views := make([]gin.H, 0, len(rows))
	for _, position := range rows {
		views = append(views, tradePredictionPositionView(position))
	}
	common.ApiSuccess(c, gin.H{"items": views, "total": total})
}

func GetTradePredictionRounds(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page = max(1, min(page, 100000))
	pageSize = max(1, min(pageSize, 100))
	rows, total, err := model.ListTradePredictionRounds((page-1)*pageSize, pageSize)
	if err != nil {
		respondTradePredictionError(c, err)
		return
	}
	views := make([]service.TradePredictionRoundView, 0, len(rows))
	for _, round := range rows {
		views = append(views, service.PredictionRoundView(round))
	}
	common.ApiSuccess(c, gin.H{"items": views, "total": total})
}
