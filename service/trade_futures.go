package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// TradeFuturesOrderRequest 是用户提交的一笔合约委托。平仓时 Qty 为 0 表示平掉全部可平的数量；止盈止损只在开仓时可选，
// 成交后设到仓位上，0 表示不设。
type TradeFuturesOrderRequest struct {
	Symbol     string
	Side       string
	Action     string
	Type       string
	Qty        decimal.Decimal
	Price      decimal.Decimal
	Leverage   int
	TakeProfit decimal.Decimal
	StopLoss   decimal.Decimal
}

var (
	ErrTradeFuturesDisabled = errors.New("futures trading is disabled")
	ErrTradeLeverageInvalid = errors.New("futures leverage is out of range")
	ErrTradeTpSlInvalid     = errors.New("take profit or stop loss is on the wrong side of the price")
)

// 限价没有 PERCENT_PRICE 过滤器时，与现货一样限制在标记价格的 0.5 到 1.5 倍之间。
func tradeFuturesPriceBand(low decimal.Decimal, high decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	if !low.IsPositive() || !high.IsPositive() {
		return tradePriceBandLow, tradePriceBandHigh
	}
	return low, high
}

// tradeFuturesTpSlValid 检查止盈止损在参考价格的正确一侧：多仓止盈高于参考价、止损低于参考价，空仓反过来。0 表示不设。
func tradeFuturesTpSlValid(side string, reference decimal.Decimal, takeProfit decimal.Decimal, stopLoss decimal.Decimal) bool {
	if takeProfit.IsNegative() || stopLoss.IsNegative() {
		return false
	}
	if side == model.TradeFuturesLong {
		return (takeProfit.IsZero() || takeProfit.GreaterThan(reference)) && (stopLoss.IsZero() || stopLoss.LessThan(reference))
	}
	return (takeProfit.IsZero() || takeProfit.LessThan(reference)) && (stopLoss.IsZero() || stopLoss.GreaterThan(reference))
}

// tradeTriggerPrice 把止盈止损价记成字符串，0 记成空串(不设)。
func tradeTriggerPrice(price decimal.Decimal) string {
	if price.IsZero() {
		return ""
	}
	return price.String()
}

// tradeFuturesBookSide 是一笔合约委托在盘口上的买卖方向：开多与平空是买，开空与平多是卖。
func tradeFuturesBookSide(side string, action string) tradesim.Side {
	positionSide := tradesim.PositionSide(side)
	if action == model.TradeFuturesOpen {
		return positionSide.OpenSide()
	}
	return positionSide.CloseSide()
}

// PlaceTradeFuturesOrder 下一笔合约委托。与现货一样，市价单和下单时就能成交的限价单先等一份下单之后的盘口，再在合约的成交锁里
// 按盘口逐档算出成交并落库；限价单没成交的部分挂单，由主节点的撮合在盘口满足限价时按限价成交(收挂单手续费)。
// 开仓要合约开放；平仓随时可以，合约关闭后也能把仓位平掉。
func PlaceTradeFuturesOrder(ctx context.Context, userId int, req TradeFuturesOrderRequest) (*model.TradeFuturesOrder, error) {
	arrived := time.Now()
	setting := operation_setting.GetTradeSetting()
	info, ok := operation_setting.TradeFuturesSymbolOf(req.Symbol)
	if !ok {
		return nil, ErrTradeSymbolClosed
	}
	isOpen := req.Action == model.TradeFuturesOpen
	isLimit := req.Type == model.TradeOrderTypeLimit
	switch {
	case req.Side != model.TradeFuturesLong && req.Side != model.TradeFuturesShort,
		req.Action != model.TradeFuturesOpen && req.Action != model.TradeFuturesClose,
		req.Type != model.TradeOrderTypeMarket && req.Type != model.TradeOrderTypeLimit,
		req.Qty.IsNegative() || req.Price.IsNegative(),
		isOpen && !req.Qty.IsPositive(),
		isLimit && !req.Price.IsPositive():
		return nil, ErrTradeOrderInvalid
	}
	if isOpen && !setting.FuturesOpenEnabled(req.Symbol) {
		if !setting.Enabled || !setting.FuturesEnabled {
			return nil, ErrTradeFuturesDisabled
		}
		return nil, ErrTradeSymbolClosed
	}
	if isOpen && (req.Leverage < 1 || req.Leverage > setting.FuturesLeverageLimit(info)) {
		return nil, ErrTradeLeverageInvalid
	}
	market := futuresMarket
	rules, ok := market.Rules(req.Symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	mark, ok := market.MarkPrice(req.Symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	pricing, perUsd, err := tradePricing(setting)
	if err != nil {
		return nil, err
	}
	step := rules.StepSize
	if !step.IsPositive() {
		step = decimal.New(1, -tradesim.QtyDecimals)
	}
	qty := req.Qty.Div(step).Floor().Mul(step)
	if !isOpen && !req.Qty.IsPositive() {
		position, err := model.GetTradeFuturesPosition(userId, req.Symbol, req.Side)
		if err != nil {
			return nil, err
		}
		if position.Qty <= position.FrozenQty {
			return nil, model.ErrTradeFuturesNoPosition
		}
		// 全部平掉时不按步长取整：Binance 以后调大步长也不会留下平不掉的零头。
		qty = tradesim.QtyFromUnits(position.Qty - position.FrozenQty)
	}
	if !qty.IsPositive() || isOpen && qty.LessThan(rules.MinQty) {
		return nil, ErrTradeQtyTooSmall
	}
	if !isLimit && rules.MarketMaxQty.IsPositive() && qty.GreaterThan(rules.MarketMaxQty) {
		return nil, ErrTradeOrderTooLarge
	}
	reference := mark.Mark
	if isLimit {
		if rules.TickSize.IsPositive() && !req.Price.Mod(rules.TickSize).IsZero() {
			return nil, ErrTradePriceInvalid
		}
		low, high := tradeFuturesPriceBand(rules.PriceDown, rules.PriceUp)
		if req.Price.LessThan(mark.Mark.Mul(low)) || req.Price.GreaterThan(mark.Mark.Mul(high)) {
			return nil, ErrTradePriceInvalid
		}
		reference = req.Price
	}
	if isOpen && rules.MinNotional.IsPositive() && qty.Mul(reference).LessThan(rules.MinNotional) {
		return nil, ErrTradeNotionalTooSmall
	}
	if isOpen && !tradeFuturesTpSlValid(req.Side, reference, req.TakeProfit, req.StopLoss) {
		return nil, ErrTradeTpSlInvalid
	}

	side, limit := tradeFuturesBookSide(req.Side, req.Action), decimal.Zero
	if isLimit {
		limit = req.Price
	}
	view, ok := market.bookView(req.Symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	if !isLimit || tradeBookCrosses(view, side, limit) {
		if err := market.awaitFreshBook(ctx, arrived, setting.StaleMs); err != nil {
			return nil, err
		}
	}

	unlock := market.lockSymbol(req.Symbol)
	defer unlock()
	if view, ok = market.bookView(req.Symbol); !ok {
		return nil, ErrTradeMarketUnavailable
	}
	levels := view.Bids
	if side == tradesim.Buy {
		levels = view.Asks
	}
	fill := tradesim.Walk(levels, side, qty, limit, step)
	if !isLimit && !fill.Qty.IsPositive() {
		return nil, ErrTradeNoLiquidity
	}
	input := model.TradeFuturesOrderInput{
		UserId:           userId,
		Symbol:           req.Symbol,
		Side:             req.Side,
		Action:           req.Action,
		Type:             req.Type,
		Leverage:         req.Leverage,
		Qty:              tradesim.QtyUnits(qty),
		Rest:             isLimit,
		MaxPositionValue: setting.FuturesMaxPositionUsd * perUsd,
	}
	if isOpen {
		input.TakeProfit, input.StopLoss = tradeTriggerPrice(req.TakeProfit), tradeTriggerPrice(req.StopLoss)
	}
	if isLimit {
		input.Price = req.Price.String()
	}
	if fill.Qty.IsPositive() {
		fee, err := pricing.TradeFee(fill.Notional, setting.FuturesTakerFeeBps)
		if err != nil {
			return nil, ErrTradeOrderTooLarge
		}
		input.Fill = model.TradeFuturesFill{Qty: tradesim.QtyUnits(fill.Qty), Value: fill.Notional, Fee: fee, Price: fill.AvgPrice().Round(tradesim.QtyDecimals).String()}
	}
	if isLimit && isOpen {
		// 挂单部分按限价冻结保证金，并按吃单费率预留手续费：之后按挂单费率成交，冻结的钱够付，成交完退回多出的。
		restValue := req.Price.Mul(qty.Sub(fill.Qty))
		margin, err := pricing.Margin(restValue, req.Leverage)
		if err != nil {
			return nil, ErrTradeOrderTooLarge
		}
		fee, err := pricing.TradeFee(restValue, setting.FuturesTakerFeeBps)
		if err != nil {
			return nil, ErrTradeOrderTooLarge
		}
		if input.Reserve, err = pricing.DebitQuota(restValue); err != nil {
			return nil, ErrTradeOrderTooLarge
		}
		input.Freeze = margin + fee
	}
	order, err := model.PlaceTradeFuturesOrder(input)
	if err != nil {
		return nil, err
	}
	if fill.Qty.IsPositive() {
		market.recordTaken(req.Symbol, view.Version, side, levels, fill)
	}
	if order.Status == model.TradeOrderStatusOpen {
		market.addFuturesResting(order)
	}
	market.refreshRisk(userId, req.Symbol, req.Side)
	return order, nil
}

// CancelTradeFuturesOrder 撤销用户自己挂着的一笔合约委托。
func CancelTradeFuturesOrder(userId int, orderId int) (*model.TradeFuturesOrder, error) {
	order, err := model.CancelTradeFuturesOrder(userId, orderId, model.TradeCancelByUser)
	if err != nil {
		return nil, err
	}
	futuresMarket.dropResting(order.Symbol, order.Id)
	futuresMarket.refreshRisk(userId, order.Symbol, order.Side)
	return order, nil
}

// AdjustTradeFuturesMargin 给仓位追加或减少保证金，amount 是 USDT。减少要按标记价格检查剩下的保证金够不够，没有标记价格时拒绝。
func AdjustTradeFuturesMargin(userId int, symbol string, side string, amount decimal.Decimal, add bool) (*model.TradeFuturesPosition, error) {
	if _, ok := operation_setting.TradeFuturesSymbolOf(symbol); !ok {
		return nil, ErrTradeSymbolClosed
	}
	if side != model.TradeFuturesLong && side != model.TradeFuturesShort || !amount.IsPositive() {
		return nil, ErrTradeOrderInvalid
	}
	mark, ok := futuresMarket.MarkPrice(symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	pricing, _, err := tradePricing(operation_setting.GetTradeSetting())
	if err != nil {
		return nil, err
	}
	quota, err := pricing.CreditQuota(amount)
	if err != nil || quota < 1 {
		return nil, model.ErrTradeAmountInvalid
	}
	if !add {
		quota = -quota
	}
	position, err := model.AdjustTradeFuturesMargin(userId, symbol, side, quota, mark.Mark)
	if err != nil {
		return nil, err
	}
	futuresMarket.refreshRisk(userId, symbol, side)
	return position, nil
}

// SetTradeFuturesTpSl 设置仓位的止盈止损(0 表示不设)，按当前标记价格检查方向：已经越过的价格会立刻触发，不让设。
func SetTradeFuturesTpSl(userId int, symbol string, side string, takeProfit decimal.Decimal, stopLoss decimal.Decimal) (*model.TradeFuturesPosition, error) {
	if _, ok := operation_setting.TradeFuturesSymbolOf(symbol); !ok {
		return nil, ErrTradeSymbolClosed
	}
	if side != model.TradeFuturesLong && side != model.TradeFuturesShort {
		return nil, ErrTradeOrderInvalid
	}
	mark, ok := futuresMarket.MarkPrice(symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	if !tradeFuturesTpSlValid(side, mark.Mark, takeProfit, stopLoss) {
		return nil, ErrTradeTpSlInvalid
	}
	position, err := model.SetTradeFuturesTpSl(userId, symbol, side, tradeTriggerPrice(takeProfit), tradeTriggerPrice(stopLoss))
	if err != nil {
		return nil, err
	}
	futuresMarket.refreshRisk(userId, symbol, side)
	return position, nil
}

// TradeFuturesHolding 是一个合约仓位按当前标记价格的估值。金额都是额度单位。
type TradeFuturesHolding struct {
	Symbol    string `json:"symbol"`
	Ticker    string `json:"ticker"`
	Kind      string `json:"kind"`
	Side      string `json:"side"`
	Leverage  int    `json:"leverage"`
	Qty       string `json:"qty"`
	FrozenQty string `json:"frozen_qty"`
	// EntryPrice 是开仓均价；Mark 是估值用的标记价格，没有行情时为空，这时按开仓价估值(浮动盈亏为 0)。
	EntryPrice string `json:"entry_price"`
	Mark       string `json:"mark"`
	Margin     int    `json:"margin"`
	// Pnl 是按标记价格的浮动盈亏，Value 是这个仓位现在值多少：保证金加浮动盈亏，最少为 0(逐仓最多亏掉保证金)。
	Pnl         int `json:"pnl"`
	Value       int `json:"value"`
	Maintenance int `json:"maintenance"`
	// MmrBps 是这个合约的维持保证金率(万分之几)，页面用它估算调整保证金之后的强平价。
	MmrBps           int    `json:"mmr_bps"`
	LiquidationPrice string `json:"liquidation_price"`
	TakeProfit       string `json:"take_profit"`
	StopLoss         string `json:"stop_loss"`
	Funding          int    `json:"funding"`
	RealizedPnl      int    `json:"realized_pnl"`
	Fees             int    `json:"fees"`
	OpenedAt         int64  `json:"opened_at"`
}

// ValueTradeFutures 按当前标记价格给合约仓位估值，返回每个仓位与它们的价值合计(额度单位)。
func ValueTradeFutures(positions []model.TradeFuturesPosition) ([]TradeFuturesHolding, int, error) {
	holdings := make([]TradeFuturesHolding, 0, len(positions))
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return holdings, 0, err
	}
	pricing := tradesim.Pricing{QuotaPerUsd: decimal.NewFromInt(int64(perUsd))}
	total := 0
	for _, position := range positions {
		entryValue, err := decimal.NewFromString(position.EntryValue)
		if err != nil {
			return holdings, 0, err
		}
		info, _ := operation_setting.TradeFuturesSymbolOf(position.Symbol)
		qty := tradesim.QtyFromUnits(position.Qty)
		holding := TradeFuturesHolding{
			Symbol:      position.Symbol,
			Ticker:      info.Ticker,
			Kind:        info.Kind,
			Side:        position.Side,
			Leverage:    position.Leverage,
			Qty:         qty.String(),
			FrozenQty:   tradesim.QtyFromUnits(position.FrozenQty).String(),
			Margin:      position.Margin,
			Value:       position.Margin,
			MmrBps:      info.FuturesMmrBps,
			TakeProfit:  position.TakeProfit,
			StopLoss:    position.StopLoss,
			Funding:     position.Funding,
			RealizedPnl: position.RealizedPnl,
			Fees:        position.Fees,
			OpenedAt:    position.OpenedAt,
		}
		if qty.IsPositive() {
			holding.EntryPrice = entryValue.Div(qty).Round(tradesim.QtyDecimals).String()
			holding.LiquidationPrice = pricing.LiquidationPrice(tradesim.PositionSide(position.Side), entryValue, position.Qty, position.Margin, info.FuturesMmrBps).String()
		}
		if mark, ok := futuresMarket.MarkPrice(position.Symbol); ok {
			markValue := mark.Mark.Mul(qty)
			if holding.Pnl, err = pricing.Pnl(tradesim.PositionSide(position.Side), entryValue, markValue); err != nil {
				return holdings, 0, err
			}
			if holding.Maintenance, err = pricing.Maintenance(markValue, info.FuturesMmrBps); err != nil {
				return holdings, 0, err
			}
			holding.Mark = mark.Mark.String()
			holding.Value = max(0, position.Margin+holding.Pnl)
		}
		total += holding.Value
		holdings = append(holdings, holding)
	}
	return holdings, total, nil
}
