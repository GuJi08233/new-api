package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/binance"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// TradeFuturesLevelRequest 是用户设的一档止盈或止损：触发价与触发时要平的数量。
type TradeFuturesLevelRequest struct {
	Price decimal.Decimal
	Qty   decimal.Decimal
}

// TradeFuturesOrderRequest 是用户提交的一笔合约委托。开仓要给保证金模式(空时是全仓)与杠杆，可以同时设最多 4 档止盈、4 档止损，
// 成交后设到仓位上；平仓时 Qty 为 0 表示平掉全部可平的数量。
type TradeFuturesOrderRequest struct {
	Symbol      string
	Side        string
	Action      string
	Type        string
	MarginMode  string
	Qty         decimal.Decimal
	Price       decimal.Decimal
	Leverage    int
	TakeProfits []TradeFuturesLevelRequest
	StopLosses  []TradeFuturesLevelRequest
}

var (
	ErrTradeFuturesDisabled = errors.New("futures trading is disabled")
	ErrTradeLeverageInvalid = errors.New("futures leverage is out of range")
	ErrTradeTpSlInvalid     = errors.New("take profit or stop loss is on the wrong side of the price")
)

// tradeFuturesMarks 把合约行情中心交给记账用：最新的标记价格(maxAge > 0 时只给这么新的，强平用)与风险限额档位。
type tradeFuturesMarks struct {
	market *TradeMarket
	maxAge time.Duration
}

func (m tradeFuturesMarks) MarkPrice(symbol string) (decimal.Decimal, bool) {
	if m.maxAge > 0 {
		return m.market.freshMark(symbol, m.maxAge)
	}
	mark, ok := m.market.MarkPrice(symbol)
	if !ok || !mark.Mark.IsPositive() {
		return decimal.Zero, false
	}
	return mark.Mark, true
}

func (m tradeFuturesMarks) Brackets(symbol string) tradesim.Brackets {
	return TradeFuturesBrackets(symbol)
}

// TradeAccountingMarket 是转出、买入现货时检查全仓可用要用的合约行情。
func TradeAccountingMarket() model.TradeFuturesMarket {
	return tradeFuturesMarks{market: futuresMarket}
}

// TradeFuturesLeverageLimit 是这个合约现在能选的最高杠杆：配置的上限与 Binance 风险限额第一档的上限中较小的一个。
func TradeFuturesLeverageLimit(symbol string) int {
	limit := operation_setting.TradeMaxLeverage
	if brackets := TradeFuturesBrackets(symbol); len(brackets) > 0 {
		limit = brackets[0].MaxLeverage
	}
	return operation_setting.GetTradeSetting().FuturesLeverageLimit(limit)
}

// tradeFuturesLevelsOf 检查一组止盈(kind 为 tp)或止损(sl)并换成记账用的档位：触发价大于 0 且在参考价格 reference 的正确一侧
// (多仓止盈高于、止损低于参考价，空仓反过来)，数量按步长取整后大于 0。
func tradeFuturesLevelsOf(side string, kind string, reference decimal.Decimal, step decimal.Decimal, levels []TradeFuturesLevelRequest) ([]model.TradeFuturesLevel, error) {
	if len(levels) > model.TradeFuturesMaxLevels {
		return nil, model.ErrTradeFuturesLevelsInvalid
	}
	rising := (kind == model.TradeFuturesTriggerTakeProfit) == (side == model.TradeFuturesLong)
	out := make([]model.TradeFuturesLevel, 0, len(levels))
	for _, level := range levels {
		qty := level.Qty.Div(step).Floor().Mul(step)
		if !level.Price.IsPositive() || !qty.IsPositive() {
			return nil, model.ErrTradeFuturesLevelsInvalid
		}
		if rising && !level.Price.GreaterThan(reference) || !rising && !level.Price.LessThan(reference) {
			return nil, ErrTradeTpSlInvalid
		}
		out = append(out, model.TradeFuturesLevel{Price: level.Price.String(), Qty: tradesim.QtyUnits(qty)})
	}
	return out, nil
}

// tradeFuturesBookSide 是一笔合约委托在盘口上的买卖方向：开多与平空是买，开空与平多是卖。
func tradeFuturesBookSide(side string, action string) tradesim.Side {
	positionSide := tradesim.PositionSide(side)
	if action == model.TradeFuturesOpen {
		return positionSide.OpenSide()
	}
	return positionSide.CloseSide()
}

// tradeFuturesStep 是合约的数量步长，没有规则时按最小数量单位。
func tradeFuturesStep(rules binance.SymbolInfo) decimal.Decimal {
	if rules.StepSize.IsPositive() {
		return rules.StepSize
	}
	return decimal.New(1, -tradesim.QtyDecimals)
}

// PlaceTradeFuturesOrder 下一笔合约委托。与现货一样，市价单和下单时就能成交的限价单先等一份下单之后的盘口，再在合约的成交锁里
// 按盘口逐档算出成交并落库；限价单没成交的部分挂单，由主节点的撮合在盘口满足限价时按限价成交(收挂单手续费)。
// 开仓要合约开放；平仓随时可以，合约关闭后也能把仓位平掉。
func PlaceTradeFuturesOrder(ctx context.Context, userId int, req TradeFuturesOrderRequest) (*model.TradeFuturesOrder, error) {
	arrived := time.Now()
	setting := operation_setting.GetTradeSetting()
	if _, ok := operation_setting.TradeFuturesSymbolOf(req.Symbol); !ok {
		return nil, ErrTradeSymbolClosed
	}
	isOpen := req.Action == model.TradeFuturesOpen
	isLimit := req.Type == model.TradeOrderTypeLimit
	if req.MarginMode == "" {
		req.MarginMode = model.TradeFuturesCross
	}
	switch {
	case req.Side != model.TradeFuturesLong && req.Side != model.TradeFuturesShort,
		req.Action != model.TradeFuturesOpen && req.Action != model.TradeFuturesClose,
		req.Type != model.TradeOrderTypeMarket && req.Type != model.TradeOrderTypeLimit,
		isOpen && req.MarginMode != model.TradeFuturesCross && req.MarginMode != model.TradeFuturesIsolated,
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
	if isOpen && (req.Leverage < 1 || req.Leverage > TradeFuturesLeverageLimit(req.Symbol)) {
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
	step := tradeFuturesStep(rules)
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
		if req.Price.LessThan(mark.Mark.Mul(TradePriceBandLow)) || req.Price.GreaterThan(mark.Mark.Mul(TradePriceBandHigh)) {
			return nil, ErrTradePriceInvalid
		}
		reference = req.Price
	}
	if isOpen && rules.MinNotional.IsPositive() && qty.Mul(reference).LessThan(rules.MinNotional) {
		return nil, ErrTradeNotionalTooSmall
	}
	var takeProfits, stopLosses []model.TradeFuturesLevel
	if isOpen {
		if takeProfits, err = tradeFuturesLevelsOf(req.Side, model.TradeFuturesTriggerTakeProfit, reference, step, req.TakeProfits); err != nil {
			return nil, err
		}
		if stopLosses, err = tradeFuturesLevelsOf(req.Side, model.TradeFuturesTriggerStopLoss, reference, step, req.StopLosses); err != nil {
			return nil, err
		}
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
		MarginMode:       req.MarginMode,
		Leverage:         req.Leverage,
		Qty:              tradesim.QtyUnits(qty),
		Rest:             isLimit,
		MaxPositionValue: setting.FuturesMaxPositionUsd * perUsd,
		RefPrice:         reference,
		TakeProfits:      takeProfits,
		StopLosses:       stopLosses,
		Market:           tradeFuturesMarks{market: market},
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
		if !isLimit {
			input.RefPrice = fill.AvgPrice()
		}
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
	market.refreshRisk(userId)
	return order, nil
}

// CancelTradeFuturesOrder 撤销用户自己挂着的一笔合约委托。
func CancelTradeFuturesOrder(userId int, orderId int) (*model.TradeFuturesOrder, error) {
	order, err := model.CancelTradeFuturesOrder(userId, orderId, model.TradeCancelByUser)
	if err != nil {
		return nil, err
	}
	futuresMarket.dropResting(order.Symbol, order.Id)
	futuresMarket.refreshRisk(userId)
	return order, nil
}

// closeTradeFuturesPosition 按市价平掉一个仓位：先撤掉它挂着的平仓委托，再平掉全部数量。盘口不够时平掉能平的部分。
func closeTradeFuturesPosition(ctx context.Context, userId int, symbol string, side string) (*model.TradeFuturesOrder, error) {
	canceled, err := model.CancelTradeFuturesClosing(userId, symbol, side)
	if err != nil {
		return nil, err
	}
	for _, order := range canceled {
		futuresMarket.dropResting(order.Symbol, order.Id)
	}
	return PlaceTradeFuturesOrder(ctx, userId, TradeFuturesOrderRequest{Symbol: symbol, Side: side, Action: model.TradeFuturesClose, Type: model.TradeOrderTypeMarket})
}

// TradeFuturesCloseFailure 是一键平仓里没平掉的一个仓位与原因。
type TradeFuturesCloseFailure struct {
	Symbol string `json:"symbol"`
	Side   string `json:"side"`
	Error  error  `json:"-"`
}

// CloseAllTradeFutures 按市价平掉用户的全部合约仓位，每个仓位单独成交，一个没平掉不影响别的。返回平掉的委托与没平掉的仓位。
func CloseAllTradeFutures(ctx context.Context, userId int) ([]*model.TradeFuturesOrder, []TradeFuturesCloseFailure, error) {
	positions, err := model.GetTradeFuturesPositions(userId)
	if err != nil {
		return nil, nil, err
	}
	var closed []*model.TradeFuturesOrder
	var failures []TradeFuturesCloseFailure
	for _, position := range positions {
		order, err := closeTradeFuturesPosition(ctx, userId, position.Symbol, position.Side)
		if err != nil {
			failures = append(failures, TradeFuturesCloseFailure{Symbol: position.Symbol, Side: position.Side, Error: err})
			continue
		}
		closed = append(closed, order)
	}
	return closed, failures, nil
}

// ReverseTradeFutures 反手：按市价平掉一个仓位，再按平掉的数量、同样的保证金模式与杠杆市价开反方向的仓位。两步各自成交，
// 第二步失败时仓位已经平掉，返回平仓委托与开仓的错误。止盈止损不带过去。
func ReverseTradeFutures(ctx context.Context, userId int, symbol string, side string) (*model.TradeFuturesOrder, *model.TradeFuturesOrder, error) {
	if side != model.TradeFuturesLong && side != model.TradeFuturesShort {
		return nil, nil, ErrTradeOrderInvalid
	}
	position, err := model.GetTradeFuturesPosition(userId, symbol, side)
	if err != nil {
		return nil, nil, err
	}
	if position.Qty <= 0 {
		return nil, nil, model.ErrTradeFuturesNoPosition
	}
	mode, leverage := position.MarginMode, position.Leverage
	closed, err := closeTradeFuturesPosition(ctx, userId, symbol, side)
	if err != nil {
		return nil, nil, err
	}
	opposite := model.TradeFuturesShort
	if side == model.TradeFuturesShort {
		opposite = model.TradeFuturesLong
	}
	opened, err := PlaceTradeFuturesOrder(ctx, userId, TradeFuturesOrderRequest{Symbol: symbol, Side: opposite, Action: model.TradeFuturesOpen,
		Type: model.TradeOrderTypeMarket, MarginMode: mode, Leverage: leverage, Qty: tradesim.QtyFromUnits(closed.FilledQty)})
	return closed, opened, err
}

// AdjustTradeFuturesMargin 给逐仓仓位追加或减少保证金，amount 是 USDT。减少要按标记价格检查剩下的保证金够不够，没有标记价格时拒绝。
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
	position, err := model.AdjustTradeFuturesMargin(userId, symbol, side, quota, mark.Mark, tradeFuturesMarks{market: futuresMarket})
	if err != nil {
		return nil, err
	}
	futuresMarket.refreshRisk(userId)
	return position, nil
}

// SetTradeFuturesLevels 换掉仓位的一组止盈(kind 为 tp)或止损(sl)，空表示全部取消。触发价按当前标记价格检查方向：已经越过的
// 价格会立刻触发，不让设。
func SetTradeFuturesLevels(userId int, symbol string, side string, kind string, levels []TradeFuturesLevelRequest) (*model.TradeFuturesPosition, error) {
	if _, ok := operation_setting.TradeFuturesSymbolOf(symbol); !ok {
		return nil, ErrTradeSymbolClosed
	}
	if side != model.TradeFuturesLong && side != model.TradeFuturesShort ||
		kind != model.TradeFuturesTriggerTakeProfit && kind != model.TradeFuturesTriggerStopLoss {
		return nil, ErrTradeOrderInvalid
	}
	mark, ok := futuresMarket.MarkPrice(symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	rules, ok := futuresMarket.Rules(symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	converted, err := tradeFuturesLevelsOf(side, kind, mark.Mark, tradeFuturesStep(rules), levels)
	if err != nil {
		return nil, err
	}
	position, err := model.SetTradeFuturesLevels(userId, symbol, side, kind, converted)
	if err != nil {
		return nil, err
	}
	futuresMarket.refreshRisk(userId)
	return position, nil
}

// AdjustTradeFuturesLeverage 调整一个合约的杠杆(多空两边的仓位一起)，见 model.AdjustTradeFuturesLeverage。
func AdjustTradeFuturesLeverage(userId int, symbol string, leverage int) ([]model.TradeFuturesPosition, error) {
	if _, ok := operation_setting.TradeFuturesSymbolOf(symbol); !ok {
		return nil, ErrTradeSymbolClosed
	}
	if leverage < 1 || leverage > TradeFuturesLeverageLimit(symbol) {
		return nil, ErrTradeLeverageInvalid
	}
	positions, err := model.AdjustTradeFuturesLeverage(userId, symbol, leverage, tradeFuturesMarks{market: futuresMarket})
	if err != nil {
		return nil, err
	}
	futuresMarket.refreshRisk(userId)
	return positions, nil
}

// TradeFuturesLevelView 是给页面看的一档止盈或止损。
type TradeFuturesLevelView struct {
	Id    int64  `json:"id"`
	Price string `json:"price"`
	Qty   string `json:"qty"`
}

// TradeFuturesLevelViews 把库里记的一组档位换成给页面看的样子。
func TradeFuturesLevelViews(raw string) []TradeFuturesLevelView {
	levels, err := model.ParseTradeFuturesLevels(raw)
	if err != nil {
		return []TradeFuturesLevelView{}
	}
	views := make([]TradeFuturesLevelView, len(levels))
	for i, level := range levels {
		views[i] = TradeFuturesLevelView{Id: level.Id, Price: level.Price, Qty: tradesim.QtyFromUnits(level.Qty).String()}
	}
	return views
}

// TradeFuturesHolding 是一个合约仓位按当前标记价格的估值。金额都是额度单位。
type TradeFuturesHolding struct {
	Symbol     string `json:"symbol"`
	Ticker     string `json:"ticker"`
	Kind       string `json:"kind"`
	Side       string `json:"side"`
	PositionId int    `json:"position_id"`
	MarginMode string `json:"margin_mode"`
	Leverage   int    `json:"leverage"`
	Qty        string `json:"qty"`
	FrozenQty  string `json:"frozen_qty"`
	// EntryPrice 是开仓均价；Mark 是估值用的标记价格，没有行情时为空，这时按开仓价估值(浮动盈亏为 0)。
	EntryPrice string `json:"entry_price"`
	Mark       string `json:"mark"`
	// Margin 是逐仓的保证金或全仓的占用。
	Margin int `json:"margin"`
	// Pnl 是按标记价格的浮动盈亏。Value 是这个仓位算进总资产的部分：逐仓是保证金加浮动盈亏(最少为 0，最多亏掉保证金)，
	// 全仓只有浮动盈亏(保证金还在资金里)。
	Pnl         int `json:"pnl"`
	Value       int `json:"value"`
	Maintenance int `json:"maintenance"`
	// LiquidationPrice 是预估强平价：逐仓按自己的保证金算，全仓按资金、全仓挂单冻结的钱加其他全仓仓位的浮动盈亏减去它们的维持保证金算。
	LiquidationPrice string                  `json:"liquidation_price"`
	TakeProfits      []TradeFuturesLevelView `json:"take_profits"`
	StopLosses       []TradeFuturesLevelView `json:"stop_losses"`
	Funding          int                     `json:"funding"`
	// FundingRate 是本期按目前情况估出的资金费率，NextFunding 是按它和标记价格估出的下一次资金费(收到为正)。
	FundingRate   string `json:"funding_rate"`
	NextFunding   int    `json:"next_funding"`
	RealizedPnl   int    `json:"realized_pnl"`
	Fees          int    `json:"fees"`
	InitialMargin int    `json:"initial_margin"`
	OpenedAt      int64  `json:"opened_at"`
}

// TradeCrossSummary 是用户全仓的汇总(额度单位)：占用的保证金、挂着的全仓开仓委托冻结的钱、浮动盈亏、维持保证金，全仓可用与
// 能转出的资金。
type TradeCrossSummary struct {
	Used         int `json:"used"`
	Pending      int `json:"pending"`
	Upnl         int `json:"upnl"`
	Maintenance  int `json:"maintenance"`
	Available    int `json:"available"`
	Withdrawable int `json:"withdrawable"`
}

// ValueTradeFutures 按当前标记价格给合约仓位估值，返回每个仓位、它们算进总资产的价值合计与全仓汇总(额度单位)。cash 是账户的资金，
// crossPending 是挂着的全仓开仓委托冻结的钱，全仓可用与全仓的预估强平价要用。
func ValueTradeFutures(cash int, crossPending int, positions []model.TradeFuturesPosition) ([]TradeFuturesHolding, int, TradeCrossSummary, error) {
	holdings := make([]TradeFuturesHolding, 0, len(positions))
	cross := TradeCrossSummary{Pending: crossPending}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return holdings, 0, cross, err
	}
	pricing := tradesim.Pricing{QuotaPerUsd: decimal.NewFromInt(int64(perUsd))}
	total := 0
	for _, position := range positions {
		entryValue, err := decimal.NewFromString(position.EntryValue)
		if err != nil {
			return holdings, 0, cross, err
		}
		info, _ := operation_setting.TradeFuturesSymbolOf(position.Symbol)
		brackets := TradeFuturesBrackets(position.Symbol)
		qty := tradesim.QtyFromUnits(position.Qty)
		holding := TradeFuturesHolding{
			Symbol:        position.Symbol,
			Ticker:        info.Ticker,
			Kind:          info.Kind,
			Side:          position.Side,
			PositionId:    position.PositionId,
			MarginMode:    position.MarginMode,
			Leverage:      position.Leverage,
			Qty:           qty.String(),
			FrozenQty:     tradesim.QtyFromUnits(position.FrozenQty).String(),
			Margin:        position.Margin,
			TakeProfits:   TradeFuturesLevelViews(position.TakeProfits),
			StopLosses:    TradeFuturesLevelViews(position.StopLosses),
			Funding:       position.Funding,
			RealizedPnl:   position.RealizedPnl,
			Fees:          position.Fees,
			InitialMargin: position.InitialMargin,
			OpenedAt:      position.OpenedAt,
		}
		if qty.IsPositive() {
			holding.EntryPrice = entryValue.Div(qty).Round(tradesim.QtyDecimals).String()
		}
		if mark, ok := futuresMarket.MarkPrice(position.Symbol); ok && mark.Mark.IsPositive() {
			markValue := mark.Mark.Mul(qty)
			if holding.Pnl, err = pricing.Pnl(tradesim.PositionSide(position.Side), entryValue, markValue); err != nil {
				return holdings, 0, cross, err
			}
			if holding.Maintenance, err = pricing.Maintenance(markValue, brackets); err != nil {
				return holdings, 0, cross, err
			}
			if holding.NextFunding, err = pricing.Funding(tradesim.PositionSide(position.Side), markValue, mark.FundingRate); err != nil {
				return holdings, 0, cross, err
			}
			holding.Mark, holding.FundingRate = mark.Mark.String(), mark.FundingRate.String()
		}
		if position.MarginMode == model.TradeFuturesCross {
			holding.Value = holding.Pnl
			cross.Used += position.Margin
			cross.Upnl += holding.Pnl
			cross.Maintenance += holding.Maintenance
		} else {
			holding.Value = max(0, position.Margin+holding.Pnl)
			holding.LiquidationPrice = pricing.LiquidationPrice(tradesim.PositionSide(position.Side), entryValue, position.Qty, position.Margin, brackets).String()
		}
		total += holding.Value
		holdings = append(holdings, holding)
	}
	for i := range holdings {
		holding := &holdings[i]
		if holding.MarginMode != model.TradeFuturesCross {
			continue
		}
		position := positions[i]
		entryValue, _ := decimal.NewFromString(position.EntryValue)
		backing := cash + crossPending + (cross.Upnl - holding.Pnl) - (cross.Maintenance - holding.Maintenance)
		holding.LiquidationPrice = pricing.LiquidationPrice(tradesim.PositionSide(position.Side), entryValue, position.Qty, backing,
			TradeFuturesBrackets(position.Symbol)).String()
	}
	cross.Available = cash + cross.Upnl - cross.Used
	cross.Withdrawable = max(0, min(cash, cash+min(cross.Upnl, 0)-cross.Used))
	return holdings, total, cross, nil
}
