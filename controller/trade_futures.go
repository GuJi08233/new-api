package controller

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type tradeFuturesOrderRequest struct {
	Symbol     string `json:"symbol"`
	Side       string `json:"side"`
	Action     string `json:"action"`
	Type       string `json:"type"`
	Qty        string `json:"qty"`
	Price      string `json:"price"`
	Leverage   int    `json:"leverage"`
	TakeProfit string `json:"take_profit"`
	StopLoss   string `json:"stop_loss"`
}

type tradeFuturesMarginRequest struct {
	Symbol string `json:"symbol"`
	Side   string `json:"side"`
	// Direction 是 add(追加)或 reduce(减少)，Amount 是 USDT。
	Direction string `json:"direction"`
	Amount    string `json:"amount"`
}

type tradeFuturesTpSlRequest struct {
	Symbol     string `json:"symbol"`
	Side       string `json:"side"`
	TakeProfit string `json:"take_profit"`
	StopLoss   string `json:"stop_loss"`
}

// tradeFuturesOrderView 是给页面看的合约委托：数量是十进制字符串，金额是额度单位，AvgPrice 是成交均价。
func tradeFuturesOrderView(order *model.TradeFuturesOrder) gin.H {
	avgPrice := ""
	if value, err := decimal.NewFromString(order.FilledValue); err == nil && order.FilledQty > 0 {
		avgPrice = value.Div(tradesim.QtyFromUnits(order.FilledQty)).Round(tradesim.QtyDecimals).String()
	}
	return gin.H{
		"id":            order.Id,
		"symbol":        order.Symbol,
		"side":          order.Side,
		"action":        order.Action,
		"type":          order.Type,
		"status":        order.Status,
		"leverage":      order.Leverage,
		"price":         order.Price,
		"qty":           tradeQty(order.Qty),
		"filled_qty":    tradeQty(order.FilledQty),
		"filled_value":  order.FilledValue,
		"avg_price":     avgPrice,
		"fee":           order.Fee,
		"realized_pnl":  order.RealizedPnl,
		"frozen":        order.Frozen,
		"take_profit":   order.TakeProfit,
		"stop_loss":     order.StopLoss,
		"trigger":       order.Trigger,
		"cancel_reason": order.CancelReason,
		"created_at":    order.CreatedAt,
		"finished_at":   order.FinishedAt,
	}
}

// tradeFuturesSymbolsFor 是给用户看的合约：开放开仓的合约，加上用户还持有仓位的合约(合约关掉以后也要能平仓)。
func tradeFuturesSymbolsFor(userId int, setting *operation_setting.TradeSetting) ([]operation_setting.TradeSymbolInfo, error) {
	positions, err := model.GetTradeFuturesPositions(userId)
	if err != nil {
		return nil, err
	}
	var infos []operation_setting.TradeSymbolInfo
	for _, info := range operation_setting.TradeSymbols {
		held := slices.ContainsFunc(positions, func(position model.TradeFuturesPosition) bool { return position.Symbol == info.Futures })
		if setting.FuturesOpenEnabled(info.Futures) || held {
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// GetTradeFuturesMarket 返回用户能看到的合约：行情(含标记价格与资金费率)、交易规则、杠杆上限与最近 24 小时的走势。
func GetTradeFuturesMarket(c *gin.Context) {
	setting := operation_setting.GetTradeSetting()
	infos, err := tradeFuturesSymbolsFor(c.GetInt("id"), setting)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	market := service.GetFuturesMarket()
	ctx := c.Request.Context()
	sparklines := make([][]string, len(infos))
	var wg sync.WaitGroup
	for i, info := range infos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sparklines[i], _ = market.Sparkline(ctx, info.Futures)
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(tradeSparklineTimeout):
	}
	items := make([]gin.H, 0, len(infos))
	for i, info := range infos {
		item := gin.H{
			"symbol":       info.Futures,
			"ticker":       info.Ticker,
			"kind":         info.Kind,
			"open":         setting.FuturesOpenEnabled(info.Futures),
			"max_leverage": setting.FuturesLeverageLimit(info),
			"mmr_bps":      info.FuturesMmrBps,
		}
		if quote, ok := market.Quote(info.Futures); ok {
			item["quote"] = quote
		}
		if rules, ok := market.Rules(info.Futures); ok {
			item["rules"] = gin.H{
				"tick_size":      rules.TickSize.String(),
				"step_size":      rules.StepSize.String(),
				"min_qty":        rules.MinQty.String(),
				"market_max_qty": rules.MarketMaxQty.String(),
				"min_notional":   rules.MinNotional.String(),
				"price_up":       rules.PriceUp.String(),
				"price_down":     rules.PriceDown.String(),
			}
		}
		select {
		case <-done:
			item["sparkline"] = sparklines[i]
		default:
		}
		items = append(items, item)
	}
	status := market.Status()
	common.ApiSuccess(c, gin.H{
		"symbols":          items,
		"enabled":          setting.Enabled && setting.FuturesEnabled,
		"connected":        status.Connected && status.DataConnected,
		"taker_fee_bps":    setting.FuturesTakerFeeBps,
		"maker_fee_bps":    setting.FuturesMakerFeeBps,
		"max_position_usd": setting.FuturesMaxPositionUsd,
	})
}

// GetTradeFuturesKlines 返回合约的 K 线，格式同 GetTradeKlines。
func GetTradeFuturesKlines(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.Query("interval")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "300"))
	endTime, _ := strconv.ParseInt(c.DefaultQuery("end_time", "0"), 10, 64)
	if _, ok := operation_setting.TradeFuturesSymbolOf(symbol); !ok {
		common.ApiErrorI18n(c, i18n.MsgTradeSymbolClosed)
		return
	}
	if !slices.Contains(service.TradeKlineIntervals, interval) || limit < 1 || limit > service.TradeKlineMaxLimit || endTime < 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	klines, err := service.GetFuturesMarket().Klines(c.Request.Context(), symbol, interval, limit, endTime)
	if err != nil {
		common.SysError(fmt.Sprintf("trade futures klines %s %s failed: %v", symbol, interval, err))
		common.ApiErrorI18n(c, i18n.MsgTradeMarketUnavailable)
		return
	}
	rows := make([][6]any, len(klines))
	for i, kline := range klines {
		rows[i] = [6]any{kline.OpenTime, kline.Open.String(), kline.High.String(), kline.Low.String(), kline.Close.String(), kline.Volume.String()}
	}
	common.ApiSuccess(c, rows)
}

// respondTradeFuturesError 把合约下单、撤单与调整仓位的错误翻译给用户，其余的交给 respondTradeOrderError。
func respondTradeFuturesError(c *gin.Context, symbol string, err error) {
	setting := operation_setting.GetTradeSetting()
	rules, _ := service.GetFuturesMarket().Rules(symbol)
	info, _ := operation_setting.TradeFuturesSymbolOf(symbol)
	switch {
	case errors.Is(err, service.ErrTradeFuturesDisabled):
		common.ApiErrorI18n(c, i18n.MsgTradeFuturesDisabled)
	case errors.Is(err, service.ErrTradeLeverageInvalid):
		common.ApiErrorI18n(c, i18n.MsgTradeLeverageInvalid, map[string]any{"Max": setting.FuturesLeverageLimit(info)})
	case errors.Is(err, service.ErrTradeTpSlInvalid):
		common.ApiErrorI18n(c, i18n.MsgTradeTpSlInvalid)
	case errors.Is(err, service.ErrTradePriceInvalid):
		low, high := decimal.RequireFromString("0.5"), decimal.RequireFromString("1.5")
		if rules.PriceDown.IsPositive() && rules.PriceUp.IsPositive() {
			low, high = rules.PriceDown, rules.PriceUp
		}
		hundred := decimal.NewFromInt(100)
		common.ApiErrorI18n(c, i18n.MsgTradeFuturesPriceInvalid, map[string]any{"Low": low.Mul(hundred).String(), "High": high.Mul(hundred).String()})
	case errors.Is(err, service.ErrTradeOrderTooLarge):
		common.ApiErrorI18n(c, i18n.MsgTradeFuturesQtyTooLarge, map[string]any{"Max": rules.MarketMaxQty.String()})
	case errors.Is(err, service.ErrTradeQtyTooSmall):
		common.ApiErrorI18n(c, i18n.MsgTradeQtyTooSmall, map[string]any{"Min": rules.MinQty.String()})
	case errors.Is(err, service.ErrTradeNotionalTooSmall):
		common.ApiErrorI18n(c, i18n.MsgTradeNotionalTooSmall, map[string]any{"Min": rules.MinNotional.String()})
	case errors.Is(err, model.ErrTradePositionLimit):
		common.ApiErrorI18n(c, i18n.MsgTradeFuturesPositionLimit, map[string]any{"Max": setting.FuturesMaxPositionUsd})
	case errors.Is(err, model.ErrTradeFuturesNoPosition):
		common.ApiErrorI18n(c, i18n.MsgTradeFuturesNoPosition)
	case errors.Is(err, model.ErrTradeFuturesLeverageMismatch):
		common.ApiErrorI18n(c, i18n.MsgTradeFuturesLeverageMismatch)
	case errors.Is(err, model.ErrTradeFuturesMarginTooLow):
		common.ApiErrorI18n(c, i18n.MsgTradeFuturesMarginTooLow)
	case errors.Is(err, model.ErrTradeAmountInvalid):
		common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
	default:
		respondTradeOrderError(c, "", err)
	}
}

// PlaceTradeFuturesOrder 下一笔合约委托：开仓要合约开放，平仓随时可以。
func PlaceTradeFuturesOrder(c *gin.Context) {
	var req tradeFuturesOrderRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	qty, qtyErr := parseTradeDecimal(req.Qty)
	price, priceErr := parseTradeDecimal(req.Price)
	takeProfit, tpErr := parseTradeDecimal(req.TakeProfit)
	stopLoss, slErr := parseTradeDecimal(req.StopLoss)
	if qtyErr != nil || priceErr != nil || tpErr != nil || slErr != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	order, err := service.PlaceTradeFuturesOrder(c.Request.Context(), c.GetInt("id"), service.TradeFuturesOrderRequest{
		Symbol:     req.Symbol,
		Side:       req.Side,
		Action:     req.Action,
		Type:       req.Type,
		Qty:        qty,
		Price:      price,
		Leverage:   req.Leverage,
		TakeProfit: takeProfit,
		StopLoss:   stopLoss,
	})
	if err != nil {
		respondTradeFuturesError(c, req.Symbol, err)
		return
	}
	common.ApiSuccess(c, tradeFuturesOrderView(order))
}

// CancelTradeFuturesOrder 撤销自己挂着的一笔合约委托，合约关闭时也能撤。
func CancelTradeFuturesOrder(c *gin.Context) {
	orderId, err := strconv.Atoi(c.Param("id"))
	if err != nil || orderId < 1 {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderNotOpen)
		return
	}
	order, err := service.CancelTradeFuturesOrder(c.GetInt("id"), orderId)
	if err != nil {
		respondTradeFuturesError(c, "", err)
		return
	}
	common.ApiSuccess(c, tradeFuturesOrderView(order))
}

// GetTradeFuturesOrders 分页列出合约委托：status=open 是挂着的，否则是已经结束的；symbol 为空时不限合约。
func GetTradeFuturesOrders(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	orders, total, err := model.GetTradeFuturesOrders(c.GetInt("id"), c.Query("status") == model.TradeOrderStatusOpen, c.Query("symbol"),
		pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, len(orders))
	for i := range orders {
		items[i] = tradeFuturesOrderView(&orders[i])
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

// GetTradeFuturesOrderFills 返回一笔合约委托的每一次成交。
func GetTradeFuturesOrderFills(c *gin.Context) {
	orderId, err := strconv.Atoi(c.Param("id"))
	if err != nil || orderId < 1 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	fills, err := model.GetTradeFuturesOrderFills(c.GetInt("id"), orderId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, len(fills))
	for i, fill := range fills {
		items[i] = tradeLedgerView(fill)
	}
	common.ApiSuccess(c, items)
}

// GetTradeFuturesFills 返回用户在一个合约上最近的成交，K 线图用它标出开平仓的位置。每笔成交带上委托的仓位方向：
// 开多、平空是买入，开空、平多是卖出。
func GetTradeFuturesFills(c *gin.Context) {
	userId := c.GetInt("id")
	fills, err := model.GetTradeFuturesFills(userId, c.Query("symbol"), tradeFillMarks)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	orderIds := make([]int, len(fills))
	for i, fill := range fills {
		orderIds[i] = fill.OrderId
	}
	sides, err := model.GetTradeFuturesOrderSides(userId, orderIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, len(fills))
	for i, fill := range fills {
		items[i] = tradeLedgerView(fill)
		items[i]["side"] = sides[fill.OrderId]
	}
	common.ApiSuccess(c, items)
}

// GetTradeFuturesPositions 返回用户持有的合约仓位，按当前标记价格估值。
func GetTradeFuturesPositions(c *gin.Context) {
	positions, err := model.GetTradeFuturesPositions(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	holdings, value, err := service.ValueTradeFutures(positions)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"positions": holdings, "value": value})
}

// AdjustTradeFuturesMargin 给自己的仓位追加或减少保证金。
func AdjustTradeFuturesMargin(c *gin.Context) {
	var req tradeFuturesMarginRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
		return
	}
	amount, err := parseTradeDecimal(req.Amount)
	if err != nil || !amount.IsPositive() || req.Direction != "add" && req.Direction != "reduce" {
		common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
		return
	}
	position, err := service.AdjustTradeFuturesMargin(c.GetInt("id"), req.Symbol, req.Side, amount, req.Direction == "add")
	if err != nil {
		respondTradeFuturesError(c, req.Symbol, err)
		return
	}
	common.ApiSuccess(c, gin.H{"margin": position.Margin})
}

// SetTradeFuturesTpSl 设置自己仓位的止盈止损，空串或 0 表示不设。
func SetTradeFuturesTpSl(c *gin.Context) {
	var req tradeFuturesTpSlRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	takeProfit, tpErr := parseTradeDecimal(req.TakeProfit)
	stopLoss, slErr := parseTradeDecimal(req.StopLoss)
	if tpErr != nil || slErr != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	position, err := service.SetTradeFuturesTpSl(c.GetInt("id"), req.Symbol, req.Side, takeProfit, stopLoss)
	if err != nil {
		respondTradeFuturesError(c, req.Symbol, err)
		return
	}
	common.ApiSuccess(c, gin.H{"take_profit": position.TakeProfit, "stop_loss": position.StopLoss})
}

// GetTradeFuturesHistory 分页列出已经结束的仓位，symbol 为空时不限合约。
func GetTradeFuturesHistory(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	history, total, err := model.GetTradeFuturesHistory(c.GetInt("id"), c.Query("symbol"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, len(history))
	for i, item := range history {
		items[i] = gin.H{
			"id":           item.Id,
			"symbol":       item.Symbol,
			"side":         item.Side,
			"leverage":     item.Leverage,
			"qty":          tradeQty(item.Qty),
			"entry_price":  item.EntryPrice,
			"close_price":  item.ClosePrice,
			"realized_pnl": item.RealizedPnl,
			"fees":         item.Fees,
			"funding":      item.Funding,
			"pnl":          item.Pnl,
			"margin_in":    item.MarginIn,
			"close_reason": item.CloseReason,
			"opened_at":    item.OpenedAt,
			"closed_at":    item.ClosedAt,
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
