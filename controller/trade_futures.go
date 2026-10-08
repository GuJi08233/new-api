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

// tradeFuturesLevelRequest 是页面提交的一档止盈或止损：触发价与要平的数量。
type tradeFuturesLevelRequest struct {
	Price string `json:"price"`
	Qty   string `json:"qty"`
}

type tradeFuturesOrderRequest struct {
	Symbol      string                     `json:"symbol"`
	Side        string                     `json:"side"`
	Action      string                     `json:"action"`
	Type        string                     `json:"type"`
	MarginMode  string                     `json:"margin_mode"`
	Qty         string                     `json:"qty"`
	Price       string                     `json:"price"`
	Leverage    int                        `json:"leverage"`
	TakeProfits []tradeFuturesLevelRequest `json:"take_profits"`
	StopLosses  []tradeFuturesLevelRequest `json:"stop_losses"`
}

type tradeFuturesPositionRequest struct {
	Symbol string `json:"symbol"`
	Side   string `json:"side"`
}

type tradeFuturesMarginRequest struct {
	Symbol string `json:"symbol"`
	Side   string `json:"side"`
	// Direction 是 add(追加)或 reduce(减少)，Amount 是 USDT。
	Direction string `json:"direction"`
	Amount    string `json:"amount"`
}

type tradeFuturesLevelsRequest struct {
	Symbol string `json:"symbol"`
	Side   string `json:"side"`
	// Kind 是 tp(止盈)或 sl(止损)，Levels 是这一组的全部档位，空表示全部取消。
	Kind   string                     `json:"kind"`
	Levels []tradeFuturesLevelRequest `json:"levels"`
}

type tradeFuturesLeverageRequest struct {
	Symbol   string `json:"symbol"`
	Leverage int    `json:"leverage"`
}

// parseTradeFuturesLevels 解析页面提交的一组止盈或止损档位。
func parseTradeFuturesLevels(levels []tradeFuturesLevelRequest) ([]service.TradeFuturesLevelRequest, error) {
	out := make([]service.TradeFuturesLevelRequest, len(levels))
	for i, level := range levels {
		price, err := parseTradeDecimal(level.Price)
		if err != nil {
			return nil, err
		}
		qty, err := parseTradeDecimal(level.Qty)
		if err != nil {
			return nil, err
		}
		out[i] = service.TradeFuturesLevelRequest{Price: price, Qty: qty}
	}
	return out, nil
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
		"margin_mode":   order.MarginMode,
		"leverage":      order.Leverage,
		"position_id":   order.PositionId,
		"price":         order.Price,
		"qty":           tradeQty(order.Qty),
		"filled_qty":    tradeQty(order.FilledQty),
		"filled_value":  order.FilledValue,
		"avg_price":     avgPrice,
		"fee":           order.Fee,
		"realized_pnl":  order.RealizedPnl,
		"frozen":        order.Frozen,
		"take_profits":  service.TradeFuturesLevelViews(order.TakeProfits),
		"stop_losses":   service.TradeFuturesLevelViews(order.StopLosses),
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
		if info.Futures == "" {
			continue
		}
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
			"session":      info.Session,
			"spot":         info.Symbol,
			"open":         setting.FuturesOpenEnabled(info.Futures),
			"max_leverage": service.TradeFuturesLeverageLimit(info.Futures),
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

// GetTradeFuturesBrackets 返回一个合约的风险限额档位(名义价值区间、最高杠杆、维持保证金率与速算数)与现在能选的最高杠杆，
// 页面按它估算强平价、维持保证金率和每个杠杆能开多大的仓位。
func GetTradeFuturesBrackets(c *gin.Context) {
	symbol := c.Query("symbol")
	if _, ok := operation_setting.TradeFuturesSymbolOf(symbol); !ok {
		common.ApiErrorI18n(c, i18n.MsgTradeSymbolClosed)
		return
	}
	brackets := service.TradeFuturesBrackets(symbol)
	items := make([]gin.H, len(brackets))
	for i, bracket := range brackets {
		items[i] = gin.H{
			"floor":        bracket.Floor.String(),
			"cap":          bracket.Cap.String(),
			"max_leverage": bracket.MaxLeverage,
			"mmr":          bracket.Mmr.String(),
			"maint_amount": bracket.MaintAmount.String(),
		}
	}
	common.ApiSuccess(c, gin.H{"brackets": items, "max_leverage": service.TradeFuturesLeverageLimit(symbol)})
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

// tradeFuturesErrorMessage 把合约下单、撤单与调整仓位的错误翻译给用户，其余的交给 tradeOrderErrorMessage。
func tradeFuturesErrorMessage(c *gin.Context, symbol string, err error) string {
	setting := operation_setting.GetTradeSetting()
	rules, _ := service.GetFuturesMarket().Rules(symbol)
	switch {
	case errors.Is(err, service.ErrTradeFuturesDisabled):
		return i18n.T(c, i18n.MsgTradeFuturesDisabled)
	case errors.Is(err, service.ErrTradeLeverageInvalid):
		return i18n.T(c, i18n.MsgTradeLeverageInvalid, map[string]any{"Max": service.TradeFuturesLeverageLimit(symbol)})
	case errors.Is(err, service.ErrTradeTpSlInvalid):
		return i18n.T(c, i18n.MsgTradeTpSlInvalid)
	case errors.Is(err, service.ErrTradePriceInvalid):
		hundred := decimal.NewFromInt(100)
		return i18n.T(c, i18n.MsgTradeFuturesPriceInvalid, map[string]any{"Low": service.TradePriceBandLow.Mul(hundred).String(),
			"High": service.TradePriceBandHigh.Mul(hundred).String()})
	case errors.Is(err, service.ErrTradeOrderTooLarge):
		return i18n.T(c, i18n.MsgTradeFuturesQtyTooLarge, map[string]any{"Max": rules.MarketMaxQty.String()})
	case errors.Is(err, service.ErrTradeQtyTooSmall):
		return i18n.T(c, i18n.MsgTradeQtyTooSmall, map[string]any{"Min": rules.MinQty.String()})
	case errors.Is(err, service.ErrTradeNotionalTooSmall):
		return i18n.T(c, i18n.MsgTradeNotionalTooSmall, map[string]any{"Min": rules.MinNotional.String()})
	case errors.Is(err, model.ErrTradePositionLimit):
		return i18n.T(c, i18n.MsgTradeFuturesPositionLimit, map[string]any{"Max": setting.FuturesMaxPositionUsd})
	case errors.Is(err, model.ErrTradeFuturesNoPosition):
		return i18n.T(c, i18n.MsgTradeFuturesNoPosition)
	case errors.Is(err, model.ErrTradeFuturesLeverageMismatch):
		return i18n.T(c, i18n.MsgTradeFuturesLeverageMismatch)
	case errors.Is(err, model.ErrTradeFuturesModeMismatch):
		return i18n.T(c, i18n.MsgTradeFuturesModeMismatch)
	case errors.Is(err, model.ErrTradeFuturesLeverageTooHigh):
		return i18n.T(c, i18n.MsgTradeFuturesLeverageTooHigh)
	case errors.Is(err, model.ErrTradeFuturesLeverageDown):
		return i18n.T(c, i18n.MsgTradeFuturesLeverageDown)
	case errors.Is(err, model.ErrTradeFuturesOpenOrders):
		return i18n.T(c, i18n.MsgTradeFuturesOpenOrders)
	case errors.Is(err, model.ErrTradeFuturesCrossMargin):
		return i18n.T(c, i18n.MsgTradeFuturesCrossMargin)
	case errors.Is(err, model.ErrTradeFuturesLevelsInvalid):
		return i18n.T(c, i18n.MsgTradeFuturesLevelsInvalid, map[string]any{"Max": model.TradeFuturesMaxLevels})
	case errors.Is(err, model.ErrTradeFuturesMarginTooLow):
		return i18n.T(c, i18n.MsgTradeFuturesMarginTooLow)
	case errors.Is(err, model.ErrTradeAmountInvalid):
		return i18n.T(c, i18n.MsgTradeAmountInvalid)
	default:
		return tradeOrderErrorMessage(c, "", err)
	}
}

func respondTradeFuturesError(c *gin.Context, symbol string, err error) {
	common.ApiErrorMsg(c, tradeFuturesErrorMessage(c, symbol, err))
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
	takeProfits, tpErr := parseTradeFuturesLevels(req.TakeProfits)
	stopLosses, slErr := parseTradeFuturesLevels(req.StopLosses)
	if qtyErr != nil || priceErr != nil || tpErr != nil || slErr != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	order, err := service.PlaceTradeFuturesOrder(c.Request.Context(), c.GetInt("id"), service.TradeFuturesOrderRequest{
		Symbol:      req.Symbol,
		Side:        req.Side,
		Action:      req.Action,
		Type:        req.Type,
		MarginMode:  req.MarginMode,
		Qty:         qty,
		Price:       price,
		Leverage:    req.Leverage,
		TakeProfits: takeProfits,
		StopLosses:  stopLosses,
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

// tradeFuturesFillViews 把合约成交的账单换成给页面看的样子，每笔带上委托的仓位方向与触发来源(止盈、止损、强平)：开多、平空是买入，
// 开空、平多是卖出。
func tradeFuturesFillViews(userId int, fills []model.TradeLedger) ([]gin.H, error) {
	orderIds := make([]int, len(fills))
	for i, fill := range fills {
		orderIds[i] = fill.OrderId
	}
	orders, err := model.GetTradeFuturesOrdersOf(userId, orderIds)
	if err != nil {
		return nil, err
	}
	items := make([]gin.H, len(fills))
	for i, fill := range fills {
		items[i] = tradeLedgerView(fill)
		items[i]["side"] = orders[fill.OrderId].Side
		items[i]["trigger"] = orders[fill.OrderId].Trigger
		items[i]["position_id"] = fill.PositionId
	}
	return items, nil
}

// GetTradeFuturesFills 返回用户在一个合约上最近的成交，K 线图用它标出开平仓的位置。
func GetTradeFuturesFills(c *gin.Context) {
	userId := c.GetInt("id")
	fills, err := model.GetTradeFuturesFills(userId, c.Query("symbol"), tradeFillMarks)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items, err := tradeFuturesFillViews(userId, fills)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

// GetTradeFuturesPositions 返回用户持有的合约仓位与全仓汇总，按当前标记价格估值。
func GetTradeFuturesPositions(c *gin.Context) {
	userId := c.GetInt("id")
	account, err := model.GetTradeAccount(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	positions, err := model.GetTradeFuturesPositions(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pending, err := model.ListTradeFuturesCrossPending([]int{userId})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	holdings, value, cross, err := service.ValueTradeFutures(account.Cash, pending[userId], positions)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	_, valuation, err := service.ValueTradeUser(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cross = valuation.Cross
	common.ApiSuccess(c, gin.H{"positions": holdings, "value": value, "cross": cross, "cash": account.Cash})
}

// AdjustTradeFuturesMargin 给自己的逐仓仓位追加或减少保证金。
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

// SetTradeFuturesLevels 换掉自己仓位的一组止盈或止损，空表示全部取消。
func SetTradeFuturesLevels(c *gin.Context) {
	var req tradeFuturesLevelsRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	levels, err := parseTradeFuturesLevels(req.Levels)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	position, err := service.SetTradeFuturesLevels(c.GetInt("id"), req.Symbol, req.Side, req.Kind, levels)
	if err != nil {
		respondTradeFuturesError(c, req.Symbol, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"take_profits": service.TradeFuturesLevelViews(position.TakeProfits),
		"stop_losses":  service.TradeFuturesLevelViews(position.StopLosses),
	})
}

// AdjustTradeFuturesLeverage 调整自己一个合约的杠杆，多空两边的仓位一起调。
func AdjustTradeFuturesLeverage(c *gin.Context) {
	var req tradeFuturesLeverageRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	positions, err := service.AdjustTradeFuturesLeverage(c.GetInt("id"), req.Symbol, req.Leverage)
	if err != nil {
		respondTradeFuturesError(c, req.Symbol, err)
		return
	}
	items := make([]gin.H, len(positions))
	for i, position := range positions {
		items[i] = gin.H{"side": position.Side, "leverage": position.Leverage, "margin": position.Margin}
	}
	common.ApiSuccess(c, gin.H{"leverage": req.Leverage, "positions": items})
}

// CloseAllTradeFutures 按市价平掉自己的全部合约仓位，挂着的开仓委托不动。返回平掉的委托与没平掉的仓位(带原因)。
func CloseAllTradeFutures(c *gin.Context) {
	closed, failures, err := service.CloseAllTradeFutures(c.Request.Context(), c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	orders := make([]gin.H, len(closed))
	for i, order := range closed {
		orders[i] = tradeFuturesOrderView(order)
	}
	failed := make([]gin.H, len(failures))
	for i, failure := range failures {
		failed[i] = gin.H{"symbol": failure.Symbol, "side": failure.Side, "message": tradeFuturesErrorMessage(c, failure.Symbol, failure.Error)}
	}
	common.ApiSuccess(c, gin.H{"closed": orders, "failed": failed})
}

// ReverseTradeFutures 反手：按市价平掉自己的一个仓位，再按平掉的数量、同样的保证金模式与杠杆市价开反方向的仓位。平掉了而反向
// 没开成时也算成功，带上开仓失败的原因。
func ReverseTradeFutures(c *gin.Context) {
	var req tradeFuturesPositionRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	closed, opened, err := service.ReverseTradeFutures(c.Request.Context(), c.GetInt("id"), req.Symbol, req.Side)
	if closed == nil {
		respondTradeFuturesError(c, req.Symbol, err)
		return
	}
	result := gin.H{"closed": tradeFuturesOrderView(closed)}
	if err != nil {
		result["open_error"] = tradeFuturesErrorMessage(c, req.Symbol, err)
	} else {
		result["opened"] = tradeFuturesOrderView(opened)
	}
	common.ApiSuccess(c, result)
}

// GetTradeFuturesHistory 分页列出已经结束的仓位与它们的每一次成交，symbol 为空时不限合约。
func GetTradeFuturesHistory(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	history, total, err := model.GetTradeFuturesHistory(userId, c.Query("symbol"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	positionIds := make([]int, len(history))
	for i, item := range history {
		positionIds[i] = item.PositionId
	}
	fills, err := model.GetTradeFuturesPositionFills(userId, positionIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	fillViews, err := tradeFuturesFillViews(userId, fills)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	fillsOf := map[int][]gin.H{}
	for i, fill := range fills {
		fillsOf[fill.PositionId] = append(fillsOf[fill.PositionId], fillViews[i])
	}
	items := make([]gin.H, len(history))
	for i, item := range history {
		positionFills := fillsOf[item.PositionId]
		if positionFills == nil {
			positionFills = []gin.H{}
		}
		items[i] = gin.H{
			"id":             item.Id,
			"position_id":    item.PositionId,
			"symbol":         item.Symbol,
			"side":           item.Side,
			"margin_mode":    item.MarginMode,
			"leverage":       item.Leverage,
			"qty":            tradeQty(item.Qty),
			"entry_price":    item.EntryPrice,
			"close_price":    item.ClosePrice,
			"realized_pnl":   item.RealizedPnl,
			"fees":           item.Fees,
			"funding":        item.Funding,
			"pnl":            item.Pnl,
			"margin_in":      item.MarginIn,
			"initial_margin": item.InitialMargin,
			"close_reason":   item.CloseReason,
			"opened_at":      item.OpenedAt,
			"closed_at":      item.ClosedAt,
			"fills":          positionFills,
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
