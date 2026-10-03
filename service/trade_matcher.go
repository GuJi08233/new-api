package service

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// tradeRestingOrder 是撮合索引里一笔挂着的限价委托。Side 是盘口上的买卖方向；合约委托另外记下用户、仓位方向与是不是开仓。
type tradeRestingOrder struct {
	Id        int
	Side      string
	Price     decimal.Decimal
	Remaining decimal.Decimal

	UserId       int
	PositionSide string
	Open         bool
}

// tradeFuturesResting 把一笔挂着的合约委托换成撮合索引里的样子。
func tradeFuturesResting(order *model.TradeFuturesOrder) (tradeRestingOrder, error) {
	price, err := decimal.NewFromString(order.Price)
	if err != nil {
		return tradeRestingOrder{}, err
	}
	return tradeRestingOrder{
		Id:           order.Id,
		Side:         string(tradeFuturesBookSide(order.Side, order.Action)),
		Price:        price,
		Remaining:    tradesim.QtyFromUnits(order.Qty - order.FilledQty),
		UserId:       order.UserId,
		PositionSide: order.Side,
		Open:         order.Action == model.TradeFuturesOpen,
	}, nil
}

// tradeMatchReload 是撮合从数据库重建挂单索引的间隔：别的节点下的单靠它进入索引，本节点下的单下单后立刻加入。
const tradeMatchReload = 3 * time.Second

// signalMatch 通知撮合这个交易对的盘口变了。同一交易对的多次通知合并成一次。撮合只在主节点运行，其他节点不记。
func (m *TradeMarket) signalMatch(symbol string) {
	if !common.IsMasterNode {
		return
	}
	m.matchMu.Lock()
	m.matchPending[symbol] = true
	m.matchMu.Unlock()
	select {
	case m.matchSignal <- struct{}{}:
	default:
	}
}

// addResting 把本节点刚挂出的限价委托加进撮合索引，不必等下一次重建。其他节点不撮合，由主节点定时从数据库读到。
func (m *TradeMarket) addResting(order *model.TradeOrder) {
	if !common.IsMasterNode {
		return
	}
	price, err := decimal.NewFromString(order.Price)
	if err != nil {
		return
	}
	m.matchMu.Lock()
	orders := append(m.resting[order.Symbol], tradeRestingOrder{
		Id:        order.Id,
		Side:      order.Side,
		Price:     price,
		Remaining: tradesim.QtyFromUnits(order.Qty - order.FilledQty),
	})
	slices.SortStableFunc(orders, compareTradeResting)
	m.resting[order.Symbol] = orders
	m.matchMu.Unlock()
	m.signalMatch(order.Symbol)
}

// addFuturesResting 把本节点刚挂出的合约限价委托加进撮合索引，同 addResting。
func (m *TradeMarket) addFuturesResting(order *model.TradeFuturesOrder) {
	if !common.IsMasterNode {
		return
	}
	resting, err := tradeFuturesResting(order)
	if err != nil {
		return
	}
	m.matchMu.Lock()
	orders := append(m.resting[order.Symbol], resting)
	slices.SortStableFunc(orders, compareTradeResting)
	m.resting[order.Symbol] = orders
	m.matchMu.Unlock()
	m.signalMatch(order.Symbol)
}

func (m *TradeMarket) dropResting(symbol string, orderId int) {
	m.matchMu.Lock()
	m.resting[symbol] = slices.DeleteFunc(m.resting[symbol], func(order tradeRestingOrder) bool { return order.Id == orderId })
	m.matchMu.Unlock()
}

// compareTradeResting 决定同一交易对挂单的撮合顺序：价格更优的先成交(买单限价高的、卖单限价低的)，同价先挂的先成交。
func compareTradeResting(a, b tradeRestingOrder) int {
	if a.Side != b.Side {
		return cmp.Compare(a.Side, b.Side)
	}
	if c := a.Price.Cmp(b.Price); c != 0 {
		if a.Side == model.TradeSideBuy {
			return -c
		}
		return c
	}
	return cmp.Compare(a.Id, b.Id)
}

// reloadResting 从数据库重建挂单索引。
func (m *TradeMarket) reloadResting() error {
	resting := map[string][]tradeRestingOrder{}
	if m.futures {
		orders, err := model.ListOpenTradeFuturesOrders()
		if err != nil {
			return err
		}
		for i := range orders {
			order, err := tradeFuturesResting(&orders[i])
			if err != nil {
				common.SysError(fmt.Sprintf("trade matcher: futures order %d has an invalid price %q", orders[i].Id, orders[i].Price))
				continue
			}
			resting[orders[i].Symbol] = append(resting[orders[i].Symbol], order)
		}
	} else {
		orders, err := model.ListOpenTradeOrders()
		if err != nil {
			return err
		}
		for _, order := range orders {
			price, err := decimal.NewFromString(order.Price)
			if err != nil {
				common.SysError(fmt.Sprintf("trade matcher: order %d has an invalid price %q", order.Id, order.Price))
				continue
			}
			resting[order.Symbol] = append(resting[order.Symbol], tradeRestingOrder{
				Id:        order.Id,
				Side:      order.Side,
				Price:     price,
				Remaining: tradesim.QtyFromUnits(order.Qty - order.FilledQty),
			})
		}
	}
	for _, orders := range resting {
		slices.SortStableFunc(orders, compareTradeResting)
	}
	m.matchMu.Lock()
	m.resting = resting
	m.matchMu.Unlock()
	return nil
}

// match 是主节点上的撮合：盘口一有变化就检查这个交易对挂着的限价单，另外定时重建索引并全部检查一遍。
func (m *TradeMarket) match() {
	reload := time.NewTicker(tradeMatchReload)
	defer reload.Stop()
	for {
		var symbols []string
		select {
		case <-reload.C:
			if !m.futures && !operation_setting.GetTradeSetting().Enabled {
				continue
			}
			if err := m.reloadResting(); err != nil {
				common.SysError("trade matcher: failed to load open orders: " + err.Error())
				continue
			}
			m.matchMu.Lock()
			for symbol := range m.resting {
				symbols = append(symbols, symbol)
			}
			m.matchMu.Unlock()
		case <-m.matchSignal:
			m.matchMu.Lock()
			for symbol := range m.matchPending {
				symbols = append(symbols, symbol)
			}
			m.matchPending = map[string]bool{}
			m.matchMu.Unlock()
		}
		for _, symbol := range symbols {
			m.matchSymbol(symbol)
		}
	}
}

// matchSymbol 用当前盘口撮合一个交易对挂着的限价单。挂单按限价成交，能成交多少受盘口上价格不劣于限价的数量限制，
// 前面的委托吃掉的数量后面的委托不能再用。
func (m *TradeMarket) matchSymbol(symbol string) {
	if m.futures {
		m.matchFuturesSymbol(symbol)
		return
	}
	setting := operation_setting.GetTradeSetting()
	if !setting.Enabled || !setting.SymbolEnabled(symbol) {
		return
	}
	m.matchMu.Lock()
	orders := slices.Clone(m.resting[symbol])
	m.matchMu.Unlock()
	if len(orders) == 0 {
		return
	}
	rules, ok := m.Rules(symbol)
	if !ok {
		return
	}
	pricing, _, err := tradePricing(setting)
	if err != nil {
		return
	}
	unlock := m.lockSymbol(symbol)
	defer unlock()
	for _, order := range orders {
		view, ok := m.bookView(symbol)
		if !ok {
			return
		}
		side, levels := tradesim.Sell, view.Bids
		if order.Side == model.TradeSideBuy {
			side, levels = tradesim.Buy, view.Asks
		}
		fill := tradesim.Walk(levels, side, order.Remaining, order.Price, rules.StepSize)
		if !fill.Qty.IsPositive() {
			continue
		}
		notional := order.Price.Mul(fill.Qty)
		var amount int
		if side == tradesim.Buy {
			amount, err = pricing.DebitQuota(notional)
		} else {
			amount, err = pricing.CreditQuota(notional)
		}
		if err != nil {
			common.SysError(fmt.Sprintf("trade matcher: order %d fill is out of range: %v", order.Id, err))
			continue
		}
		updated, err := model.FillTradeOrder(order.Id, model.TradeFill{
			Qty:    tradesim.QtyUnits(fill.Qty),
			Amount: amount,
			Fee:    pricing.FeeQuota(amount),
			Price:  order.Price.String(),
		})
		switch {
		case errors.Is(err, model.ErrTradeOrderNotOpen):
			m.dropResting(symbol, order.Id)
			continue
		case errors.Is(err, model.ErrTradeCashInsufficient):
			// 多次部分成交的取整累计起来可能比冻结的多出几个额度单位，可用资金也不够付时撤掉剩余部分。
			if _, cancelErr := model.CancelTradeOrder(0, order.Id, model.TradeCancelByBalance); cancelErr != nil && !errors.Is(cancelErr, model.ErrTradeOrderNotOpen) {
				common.SysError(fmt.Sprintf("trade matcher: failed to cancel order %d: %v", order.Id, cancelErr))
			}
			m.dropResting(symbol, order.Id)
			continue
		case err != nil:
			common.SysError(fmt.Sprintf("trade matcher: failed to fill order %d: %v", order.Id, err))
			continue
		}
		m.recordTaken(symbol, view.Version, side, levels, fill)
		if updated.Status != model.TradeOrderStatusOpen {
			m.dropResting(symbol, order.Id)
			continue
		}
		remaining := tradesim.QtyFromUnits(updated.Qty - updated.FilledQty)
		m.matchMu.Lock()
		for i := range m.resting[symbol] {
			if m.resting[symbol][i].Id == order.Id {
				m.resting[symbol][i].Remaining = remaining
			}
		}
		m.matchMu.Unlock()
	}
}

// matchFuturesSymbol 用当前盘口撮合一个合约挂着的限价委托，规则同 matchSymbol，按挂单费率收手续费。合约关闭或这个合约停止开仓时，
// 开仓挂单留着等用户撤销，平仓挂单照常成交。
func (m *TradeMarket) matchFuturesSymbol(symbol string) {
	setting := operation_setting.GetTradeSetting()
	m.matchMu.Lock()
	orders := slices.Clone(m.resting[symbol])
	m.matchMu.Unlock()
	if len(orders) == 0 {
		return
	}
	rules, ok := m.Rules(symbol)
	if !ok {
		return
	}
	pricing, _, err := tradePricing(setting)
	if err != nil {
		return
	}
	unlock := m.lockSymbol(symbol)
	defer unlock()
	for _, order := range orders {
		if order.Open && !setting.FuturesOpenEnabled(symbol) {
			continue
		}
		view, ok := m.bookView(symbol)
		if !ok {
			return
		}
		side, levels := tradesim.Sell, view.Bids
		if order.Side == string(tradesim.Buy) {
			side, levels = tradesim.Buy, view.Asks
		}
		fill := tradesim.Walk(levels, side, order.Remaining, order.Price, rules.StepSize)
		if !fill.Qty.IsPositive() {
			continue
		}
		value := order.Price.Mul(fill.Qty)
		fee, err := pricing.TradeFee(value, setting.FuturesMakerFeeBps)
		if err != nil {
			common.SysError(fmt.Sprintf("trade matcher: futures order %d fill is out of range: %v", order.Id, err))
			continue
		}
		updated, err := model.FillTradeFuturesOrder(order.Id, model.TradeFuturesFill{
			Qty:   tradesim.QtyUnits(fill.Qty),
			Value: value,
			Fee:   fee,
			Price: order.Price.String(),
		})
		switch {
		case errors.Is(err, model.ErrTradeOrderNotOpen):
			m.dropResting(symbol, order.Id)
			continue
		case errors.Is(err, model.ErrTradeCashInsufficient), errors.Is(err, model.ErrTradePositionInsufficient), errors.Is(err, model.ErrTradeFuturesLeverageMismatch):
			// 冻结的钱被取整吃光、仓位已经变小或者杠杆对不上(都不该发生)，撤掉剩下的部分。
			if _, cancelErr := model.CancelTradeFuturesOrder(0, order.Id, model.TradeCancelByBalance); cancelErr != nil && !errors.Is(cancelErr, model.ErrTradeOrderNotOpen) {
				common.SysError(fmt.Sprintf("trade matcher: failed to cancel futures order %d: %v", order.Id, cancelErr))
			}
			m.dropResting(symbol, order.Id)
			continue
		case err != nil:
			common.SysError(fmt.Sprintf("trade matcher: failed to fill futures order %d: %v", order.Id, err))
			continue
		}
		m.recordTaken(symbol, view.Version, side, levels, fill)
		m.refreshRisk(order.UserId, symbol, order.PositionSide)
		if updated.Status != model.TradeOrderStatusOpen {
			m.dropResting(symbol, order.Id)
			continue
		}
		remaining := tradesim.QtyFromUnits(updated.Qty - updated.FilledQty)
		m.matchMu.Lock()
		for i := range m.resting[symbol] {
			if m.resting[symbol][i].Id == order.Id {
				m.resting[symbol][i].Remaining = remaining
			}
		}
		m.matchMu.Unlock()
	}
}
