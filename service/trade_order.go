package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// TradeOrderRequest 是用户提交的一笔委托。市价买单可以不给数量而给 Amount(USDT，含手续费)，按金额买入。Borrow 为真时是借币
// 卖出(只用于按数量的市价卖出)；Cover 为真时是买回借着的币还掉(CoverTradeSpotAsset 用)，模拟盘或交易对关了也能买。
type TradeOrderRequest struct {
	Symbol   string
	Side     string
	Type     string
	Qty      decimal.Decimal
	Amount   decimal.Decimal
	Price    decimal.Decimal
	Leverage int
	Borrow   bool
	Cover    bool
}

var (
	ErrTradeDisabled         = errors.New("paper trading is disabled")
	ErrTradeSymbolClosed     = errors.New("trade symbol is not open")
	ErrTradeOrderInvalid     = errors.New("trade order is invalid")
	ErrTradeQtyTooSmall      = errors.New("trade quantity is below the minimum")
	ErrTradeNotionalTooSmall = errors.New("trade order value is below the minimum")
	ErrTradeOrderTooLarge    = errors.New("trade order value exceeds the limit")
	ErrTradePriceInvalid     = errors.New("trade limit price is invalid")
	ErrTradeNoLiquidity      = errors.New("the order book cannot fill this order")
)

// 限价必须在当前价格的 0.5 到 1.5 倍之间：远离市价的委托不会成交，只会长期冻结资金。
var (
	TradePriceBandLow  = decimal.RequireFromString("0.5")
	TradePriceBandHigh = decimal.RequireFromString("1.5")
)

// tradePricing 是按当前配置把美元换成额度单位、收手续费的参数。
func tradePricing(setting *operation_setting.TradeSetting) (tradesim.Pricing, int, error) {
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return tradesim.Pricing{}, 0, err
	}
	return tradesim.Pricing{QuotaPerUsd: decimal.NewFromInt(int64(perUsd)), FeeBps: setting.FeeBps}, perUsd, nil
}

// PlaceTradeOrder 下一笔委托。市价单和下单时就能成交的限价单先等一份下单之后的盘口(见 awaitFreshBook)，再在交易对的成交锁里
// 按盘口逐档算出成交并落库；限价单没成交的部分挂单，由主节点的撮合在盘口满足限价时成交。
func PlaceTradeOrder(ctx context.Context, userId int, req TradeOrderRequest) (*model.TradeOrder, error) {
	arrived := time.Now()
	setting := operation_setting.GetTradeSetting()
	// 卖出与买回还币是减少风险，模拟盘或交易对关了也能做；借币卖出是新开的，和买入一样要开着。
	closing := req.Side == model.TradeSideSell && !req.Borrow || req.Cover
	if !setting.Enabled && !closing {
		return nil, ErrTradeDisabled
	}
	if !setting.SymbolEnabled(req.Symbol) && !closing {
		return nil, ErrTradeSymbolClosed
	}
	isBuy := req.Side == model.TradeSideBuy
	isLimit := req.Type == model.TradeOrderTypeLimit
	byAmount := !isLimit && isBuy && req.Amount.IsPositive()
	if req.Leverage == 0 {
		req.Leverage = 1
	}
	if req.Leverage < 1 || req.Leverage > min(model.TradeSpotMaxLeverage, setting.SpotMaxLeverage) ||
		req.Leverage > 1 && (!isBuy || isLimit || byAmount) ||
		req.Borrow && (isBuy || isLimit || req.Leverage > 1 || setting.SpotMaxLeverage <= 1) {
		return nil, model.ErrTradeSpotFinancingInvalid
	}
	switch {
	case req.Side != model.TradeSideBuy && req.Side != model.TradeSideSell,
		req.Type != model.TradeOrderTypeMarket && req.Type != model.TradeOrderTypeLimit,
		req.Qty.IsNegative() || req.Amount.IsNegative() || req.Price.IsNegative(),
		byAmount && req.Qty.IsPositive(),
		!byAmount && !req.Qty.IsPositive(),
		isLimit && !req.Price.IsPositive():
		return nil, ErrTradeOrderInvalid
	}
	market := tradeMarket
	rules, ok := market.Rules(req.Symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	reference, ok := market.LastPrice(req.Symbol)
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
	if !byAmount && (!qty.IsPositive() || qty.LessThan(rules.MinQty)) {
		return nil, ErrTradeQtyTooSmall
	}
	if isLimit {
		if rules.TickSize.IsPositive() && !req.Price.Mod(rules.TickSize).IsZero() {
			return nil, ErrTradePriceInvalid
		}
		if req.Price.LessThan(reference.Mul(TradePriceBandLow)) || req.Price.GreaterThan(reference.Mul(TradePriceBandHigh)) {
			return nil, ErrTradePriceInvalid
		}
	}
	value := req.Amount
	if !byAmount {
		value = qty.Mul(reference)
		if isLimit {
			value = qty.Mul(req.Price)
		}
	}
	if rules.MinNotional.IsPositive() && value.LessThan(rules.MinNotional) {
		return nil, ErrTradeNotionalTooSmall
	}
	maxOrder := decimal.NewFromInt(int64(setting.MaxOrderUsd))
	if value.GreaterThan(maxOrder) {
		return nil, ErrTradeOrderTooLarge
	}

	side, limit := tradesim.Sell, decimal.Zero
	if isBuy {
		side = tradesim.Buy
	}
	price := ""
	if isLimit {
		limit, price = req.Price, req.Price.String()
	}
	view, ok := market.bookView(req.Symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	if !isLimit || tradeBookCrosses(view, side, limit) {
		if err := market.awaitFreshBook(ctx, arrived, setting.StaleMs); err != nil {
			return nil, err
		}
	} else if isBuy {
		// 有现货借款时买入之后要按买一检查风险率，挂单不确认盘口的话先确认一次。
		if err := EnsureTradeSpotPricesFresh(ctx, userId); err != nil {
			return nil, err
		}
	}

	unlock := market.lockSymbol(req.Symbol)
	defer unlock()
	if view, ok = market.bookView(req.Symbol); !ok {
		return nil, ErrTradeMarketUnavailable
	}
	levels := view.Bids
	if isBuy {
		levels = view.Asks
	}
	input := model.TradeOrderInput{
		UserId:          userId,
		Symbol:          req.Symbol,
		Side:            req.Side,
		Type:            req.Type,
		Price:           price,
		Qty:             tradesim.QtyUnits(qty),
		Rest:            isLimit,
		Leverage:        req.Leverage,
		Borrow:          req.Borrow,
		SpotMaxLeverage: setting.SpotMaxLeverage,
		MaxPositionCost: setting.MaxPositionUsd * perUsd,
	}
	var fill tradesim.Fill
	var amount, fee int
	switch {
	case byAmount:
		budget, err := pricing.CreditQuota(req.Amount)
		if err != nil {
			return nil, ErrTradeOrderTooLarge
		}
		input.Qty, input.Budget = 0, budget
		if fill, amount, fee, err = pricing.BuyWithBudget(levels, budget, decimal.Zero, step); err != nil {
			return nil, err
		}
	case isBuy:
		fill = tradesim.Walk(levels, side, qty, limit, step)
		if amount, fee, err = pricing.BuyCost(fill); err != nil {
			return nil, err
		}
	default:
		fill = tradesim.Walk(levels, side, qty, limit, step)
		if amount, fee, err = pricing.SellProceeds(fill); err != nil {
			return nil, err
		}
	}
	if !isLimit {
		if !fill.Qty.IsPositive() {
			return nil, ErrTradeNoLiquidity
		}
		if fill.Notional.GreaterThan(maxOrder) {
			return nil, ErrTradeOrderTooLarge
		}
	}
	if fill.Qty.IsPositive() {
		input.Fill = model.TradeFill{Qty: tradesim.QtyUnits(fill.Qty), Value: fill.Notional, Amount: amount, Fee: fee,
			Price: fill.AvgPrice().Round(tradesim.QtyDecimals).String()}
	}
	if isLimit && isBuy {
		// 挂单部分按限价冻结资金并预留手续费，之后的成交都按限价付款，冻结的钱够付。
		frozen, err := pricing.DebitQuota(limit.Mul(qty.Sub(fill.Qty)))
		if err != nil {
			return nil, ErrTradeOrderTooLarge
		}
		input.Freeze = frozen + pricing.FeeQuota(frozen)
	}
	input.Market = tradeFuturesMarks{market: futuresMarket}
	order, err := model.PlaceTradeOrder(input)
	if err != nil {
		return nil, err
	}
	if fill.Qty.IsPositive() {
		market.recordTaken(req.Symbol, view.Version, side, levels, fill)
	}
	if order.Status == model.TradeOrderStatusOpen {
		market.addResting(order)
	}
	return order, nil
}

// CoverTradeSpotAsset 买回 symbol 上借着的全部币还掉：数量是欠的币(算上到期还没收的利息)向上取到步长，不到最小下单数量或最小
// 成交额时补足，多买的进持仓。按市价在盘口上买，买到的先还借币。
func CoverTradeSpotAsset(ctx context.Context, userId int, symbol string) (*model.TradeOrder, error) {
	loans, err := model.GetTradeSpotAssetLoans([]int{userId})
	if err != nil {
		return nil, err
	}
	loans, err = model.AccruedTradeSpotAssetLoans(loans, common.GetTimestamp())
	if err != nil {
		return nil, err
	}
	debt := int64(0)
	for _, loan := range loans {
		if loan.Symbol == symbol {
			debt = loan.Debt()
		}
	}
	if debt <= 0 {
		return nil, model.ErrTradePositionInsufficient
	}
	rules, ok := tradeMarket.Rules(symbol)
	if !ok {
		return nil, ErrTradeMarketUnavailable
	}
	price, ok := tradeMarket.LastPrice(symbol)
	if !ok || !price.IsPositive() {
		return nil, ErrTradeMarketUnavailable
	}
	step := rules.StepSize
	if !step.IsPositive() {
		step = decimal.New(1, -tradesim.QtyDecimals)
	}
	qty := decimal.Max(tradesim.QtyFromUnits(debt), rules.MinQty)
	if rules.MinNotional.IsPositive() {
		// 按最新价补足最小成交额，再多留一个步长，免得卖一比最新价低时成交额差一点点。
		qty = decimal.Max(qty, rules.MinNotional.Div(price).Add(step))
	}
	qty = qty.Div(step).Ceil().Mul(step)
	return PlaceTradeOrder(ctx, userId, TradeOrderRequest{Symbol: symbol, Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: qty, Leverage: 1, Cover: true})
}

// tradeBookCrosses 表示限价单在这份盘口上能立刻成交：买单的限价不低于卖一，卖单的限价不高于买一。
func tradeBookCrosses(view tradeBookView, side tradesim.Side, limit decimal.Decimal) bool {
	if side == tradesim.Buy {
		return len(view.Asks) > 0 && view.Asks[0].Price.LessThanOrEqual(limit)
	}
	return len(view.Bids) > 0 && view.Bids[0].Price.GreaterThanOrEqual(limit)
}

// CancelTradeOrder 撤销用户自己挂着的一笔限价委托。
func CancelTradeOrder(userId int, orderId int) (*model.TradeOrder, error) {
	order, err := model.CancelTradeOrder(userId, orderId, model.TradeCancelByUser)
	if err != nil {
		return nil, err
	}
	tradeMarket.dropResting(order.Symbol, order.Id)
	return order, nil
}
