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
	return gin.H{"id": position.Id, "market_key": "btc-5m", "asset": "BTC", "interval_sec": model.TradePredictionWindow,
		"window_start": position.WindowStart, "side": position.Side,
		"shares": tradeQty(position.Qty), "initial_shares": tradeQty(position.InitialQty), "cost": position.Cost, "total_cost": position.TotalCost,
		"payout": position.Payout, "avg_price": position.AvgPrice, "status": position.Status, "created_at": position.CreatedAt, "updated_at": position.UpdatedAt}
}

func tradePredictionSeriesPositionView(position model.TradePredictionSeriesPosition) gin.H {
	return gin.H{"id": position.Id, "market_key": position.MarketKey, "asset": position.Asset, "interval_sec": position.IntervalSec,
		"window_start": position.WindowStart, "side": position.Side,
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
	asset, interval := c.Query("asset"), c.Query("interval")
	if asset == "" && interval == "" {
		view := service.GetTradePredictionMarket(c.Request.Context())
		common.ApiSuccess(c, gin.H{
			"enabled":          view.Enabled,
			"status":           view.Status,
			"source":           view.Source,
			"server_time":      view.ServerTime,
			"round":            view.Round,
			"up":               view.Up,
			"down":             view.Down,
			"updated_at":       view.UpdatedAt,
			"fee_rate":         view.FeeRate,
			"min_amount_usd":   view.MinAmountUsd,
			"max_amount_usd":   view.MaxAmountUsd,
			"max_position_usd": view.MaxPositionUsd,
			"market_key":       "btc-5m",
			"asset":            "BTC",
			"interval":         "5m",
			"interval_sec":     model.TradePredictionWindow,
			"markets":          service.TradePredictionMarkets,
		})
		return
	}
	spec, ok := service.ResolveTradePredictionMarketSpec(asset, interval)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	view, _ := service.GetTradePredictionMarketFor(c.Request.Context(), spec.Asset, spec.Interval)
	common.ApiSuccess(c, gin.H{
		"enabled":          view.Enabled,
		"status":           view.Status,
		"source":           view.Source,
		"server_time":      view.ServerTime,
		"round":            view.Round,
		"up":               view.Up,
		"down":             view.Down,
		"updated_at":       view.UpdatedAt,
		"fee_rate":         view.FeeRate,
		"min_amount_usd":   view.MinAmountUsd,
		"max_amount_usd":   view.MaxAmountUsd,
		"max_position_usd": view.MaxPositionUsd,
		"market_key":       spec.MarketKey,
		"asset":            spec.Asset,
		"interval":         spec.Interval,
		"interval_sec":     spec.IntervalSec,
		"markets":          service.TradePredictionMarkets,
	})
}

// GetTradePredictionLive 是预测页每秒拉一次的价格、目标价与最近成交；支持 asset / interval 参数。
func GetTradePredictionLive(c *gin.Context) {
	since, _ := strconv.ParseInt(c.Query("since"), 10, 64)
	asset, interval := c.Query("asset"), c.Query("interval")
	if asset == "" && interval == "" {
		common.ApiSuccess(c, service.GetTradePredictionLive(since))
		return
	}
	view, ok := service.GetTradePredictionLiveFor(c.Request.Context(), asset, interval, since)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	common.ApiSuccess(c, view)
}

func BuyTradePrediction(c *gin.Context) {
	var request struct {
		Asset       string `json:"asset"`
		Interval    string `json:"interval"`
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
	spec, ok := service.ResolveTradePredictionMarketSpec(request.Asset, request.Interval)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	orderReq := service.TradePredictionOrderRequest{WindowStart: request.WindowStart, Side: request.Side, Amount: amount, LimitPrice: price}
	if spec.DefaultBtc5m {
		position, err := service.BuyTradePrediction(c.Request.Context(), c.GetInt("id"), orderReq)
		if err != nil {
			respondTradePredictionError(c, err)
			return
		}
		common.ApiSuccess(c, tradePredictionPositionView(*position))
		return
	}
	position, err := service.BuyTradePredictionSeries(c.Request.Context(), c.GetInt("id"), spec, orderReq)
	if err != nil {
		respondTradePredictionError(c, err)
		return
	}
	common.ApiSuccess(c, tradePredictionSeriesPositionView(*position))
}

func SellTradePrediction(c *gin.Context) {
	positionId, err := strconv.Atoi(c.Param("id"))
	if err != nil || positionId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	var request struct {
		Asset     string `json:"asset"`
		Interval  string `json:"interval"`
		MarketKey string `json:"market_key"`
		Shares    string `json:"shares"`
		MinPrice  string `json:"min_price"`
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
	isSeries := (request.MarketKey != "" && request.MarketKey != "btc-5m") ||
		(request.Asset != "" && request.Asset != "BTC") ||
		(request.Interval != "" && request.Interval != "5m")
	if isSeries {
		position, err := service.SellTradePredictionSeries(c.Request.Context(), c.GetInt("id"), positionId, shares, price)
		if err != nil {
			respondTradePredictionError(c, err)
			return
		}
		common.ApiSuccess(c, tradePredictionSeriesPositionView(*position))
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
	spec, ok := service.ResolveTradePredictionMarketSpec(c.Query("asset"), c.Query("interval"))
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if spec.DefaultBtc5m {
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
		return
	}
	rows, total, err := model.ListTradePredictionSeriesPositions(c.GetInt("id"), spec.MarketKey, c.Query("status") != "all", (page-1)*pageSize, pageSize)
	if err != nil {
		respondTradePredictionError(c, err)
		return
	}
	views := make([]gin.H, 0, len(rows))
	for _, position := range rows {
		views = append(views, tradePredictionSeriesPositionView(position))
	}
	common.ApiSuccess(c, gin.H{"items": views, "total": total})
}

func GetTradePredictionRounds(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page = max(1, min(page, 100000))
	pageSize = max(1, min(pageSize, 100))
	spec, ok := service.ResolveTradePredictionMarketSpec(c.Query("asset"), c.Query("interval"))
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if spec.DefaultBtc5m {
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
		return
	}
	rows, total, err := model.ListTradePredictionSeriesRounds(spec.MarketKey, (page-1)*pageSize, pageSize)
	if err != nil {
		respondTradePredictionError(c, err)
		return
	}
	views := make([]service.TradePredictionRoundView, 0, len(rows))
	for _, round := range rows {
		views = append(views, service.PredictionRoundView(model.TradePredictionRound{
			WindowStart: round.WindowStart,
			EndTime:     round.EndTime,
			Slug:        round.Slug,
			Title:       round.Title,
			Outcome:     round.Outcome,
			SettledAt:   round.SettledAt,
		}))
	}
	common.ApiSuccess(c, gin.H{"items": views, "total": total})
}
