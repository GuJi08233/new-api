package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/binance"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// 合约仓位的风控只在主节点运行：每收到一个合约的标记价格(每秒一次)，就检查这个合约上所有仓位该不该强平、止盈止损有没有到价；
// 仓位索引定时从数据库重建，本节点改过的仓位立刻重读。资金费按 Binance 已经结算的费率与标记价格结算，记在仓位的保证金上。

const (
	// tradeRiskMarkMaxAge 是强平与止盈止损能用的标记价格最多多旧：断线时手里的旧价格不能拿来强平。
	tradeRiskMarkMaxAge = 5 * time.Second
	// tradeFundingPoll 是多久检查一次有没有要结算的资金费；tradeFundingGrace 是过了结算时间多久再去查费率，
	// 给 Binance 一点时间把这一期的结果发布出来。
	tradeFundingPoll  = 15 * time.Second
	tradeFundingGrace = 5 * time.Second
	tradeFundingRetry = time.Minute
	tradeFundingBatch = 100
)

// tradeRiskPosition 是风控索引里的一个仓位。
type tradeRiskPosition struct {
	UserId     int
	Side       string
	Qty        int64
	EntryValue decimal.Decimal
	Margin     int
	TakeProfit decimal.Decimal
	StopLoss   decimal.Decimal
	// takeProfit 与 stopLoss 是库里记的原样字符串，下止盈止损单时用来核对没被改过。
	takeProfit string
	stopLoss   string
}

func tradeRiskPositionOf(position model.TradeFuturesPosition) (tradeRiskPosition, error) {
	entry := tradeRiskPosition{
		UserId:     position.UserId,
		Side:       position.Side,
		Qty:        position.Qty,
		Margin:     position.Margin,
		takeProfit: position.TakeProfit,
		stopLoss:   position.StopLoss,
	}
	var err error
	if entry.EntryValue, err = decimal.NewFromString(position.EntryValue); err != nil {
		return entry, err
	}
	if position.TakeProfit != "" {
		if entry.TakeProfit, err = decimal.NewFromString(position.TakeProfit); err != nil {
			return entry, err
		}
	}
	if position.StopLoss != "" {
		if entry.StopLoss, err = decimal.NewFromString(position.StopLoss); err != nil {
			return entry, err
		}
	}
	return entry, nil
}

// trigger 返回标记价格 mark 触发了这个仓位的止盈(tp)还是止损(sl)，都没有时为空。多仓涨到止盈价、跌到止损价触发，空仓反过来。
func (p tradeRiskPosition) trigger(mark decimal.Decimal) string {
	long := p.Side == model.TradeFuturesLong
	switch {
	case p.StopLoss.IsPositive() && (long && mark.LessThanOrEqual(p.StopLoss) || !long && mark.GreaterThanOrEqual(p.StopLoss)):
		return model.TradeFuturesTriggerStopLoss
	case p.TakeProfit.IsPositive() && (long && mark.GreaterThanOrEqual(p.TakeProfit) || !long && mark.LessThanOrEqual(p.TakeProfit)):
		return model.TradeFuturesTriggerTakeProfit
	}
	return ""
}

// signalRisk 通知风控这个合约有了新的标记价格。同一合约的多次通知合并成一次。风控只在主节点运行，其他节点不记。
func (m *TradeMarket) signalRisk(symbol string) {
	if !m.futures || !common.IsMasterNode {
		return
	}
	m.riskMu.Lock()
	m.riskPending[symbol] = true
	m.riskMu.Unlock()
	select {
	case m.riskSignal <- struct{}{}:
	default:
	}
}

// refreshRisk 在本节点改过一个仓位之后(下单、撤单、调整保证金、止盈止损、资金费)把它重新读进主节点的风控索引并检查一次；
// 其他节点改的由定时重建读到。
func (m *TradeMarket) refreshRisk(userId int, symbol string, side string) {
	if !m.futures || !common.IsMasterNode {
		return
	}
	position, err := model.GetTradeFuturesPosition(userId, symbol, side)
	if err != nil {
		common.SysError(fmt.Sprintf("trade futures risk: failed to load the %s %s position of user %d: %v", symbol, side, userId, err))
		return
	}
	entry, err := tradeRiskPositionOf(*position)
	if err != nil {
		common.SysError(fmt.Sprintf("trade futures risk: the %s %s position of user %d is invalid: %v", symbol, side, userId, err))
		return
	}
	m.riskMu.Lock()
	positions := slices.DeleteFunc(m.positions[symbol], func(p tradeRiskPosition) bool { return p.UserId == userId && p.Side == side })
	if position.Qty > 0 {
		positions = append(positions, entry)
	}
	m.positions[symbol] = positions
	m.riskMu.Unlock()
	m.signalRisk(symbol)
}

// reloadRisk 从数据库重建风控索引。
func (m *TradeMarket) reloadRisk() error {
	positions, err := model.ListOpenTradeFuturesPositions()
	if err != nil {
		return err
	}
	index := map[string][]tradeRiskPosition{}
	for _, position := range positions {
		entry, err := tradeRiskPositionOf(position)
		if err != nil {
			common.SysError(fmt.Sprintf("trade futures risk: the %s %s position of user %d is invalid: %v", position.Symbol, position.Side, position.UserId, err))
			continue
		}
		index[position.Symbol] = append(index[position.Symbol], entry)
	}
	m.riskMu.Lock()
	m.positions = index
	m.riskMu.Unlock()
	return nil
}

// guard 是主节点上的合约风控：标记价格一更新就检查这个合约上的仓位，另外定时重建索引并全部检查一遍。
func (m *TradeMarket) guard() {
	reload := time.NewTicker(tradeMatchReload)
	defer reload.Stop()
	for {
		var symbols []string
		select {
		case <-reload.C:
			if err := m.reloadRisk(); err != nil {
				common.SysError("trade futures risk: failed to load positions: " + err.Error())
				continue
			}
			m.riskMu.Lock()
			for symbol := range m.positions {
				symbols = append(symbols, symbol)
			}
			m.riskMu.Unlock()
		case <-m.riskSignal:
			m.riskMu.Lock()
			for symbol := range m.riskPending {
				symbols = append(symbols, symbol)
			}
			m.riskPending = map[string]bool{}
			m.riskMu.Unlock()
		}
		for _, symbol := range symbols {
			m.checkRisk(symbol)
		}
	}
}

// checkRisk 用最新的标记价格检查一个合约上的仓位：该强平的强平(保证金全部亏掉)；没到强平而价格到了止盈或止损的，按盘口市价平掉。
// 索引里的仓位可能是旧的，强平与止盈止损都在事务里按锁住的仓位重新核对。
func (m *TradeMarket) checkRisk(symbol string) {
	mark, ok := m.freshMark(symbol, tradeRiskMarkMaxAge)
	if !ok {
		return
	}
	info, ok := operation_setting.TradeFuturesSymbolOf(symbol)
	if !ok {
		return
	}
	pricing, _, err := tradePricing(operation_setting.GetTradeSetting())
	if err != nil {
		return
	}
	m.riskMu.Lock()
	positions := slices.Clone(m.positions[symbol])
	m.riskMu.Unlock()
	for _, position := range positions {
		hit, err := pricing.Liquidatable(tradesim.PositionSide(position.Side), position.EntryValue, position.Qty, position.Margin, mark, info.FuturesMmrBps)
		if err != nil {
			common.SysError(fmt.Sprintf("trade futures risk: failed to check the %s %s position of user %d: %v", symbol, position.Side, position.UserId, err))
			continue
		}
		if hit {
			liquidated, err := model.LiquidateTradeFuturesPosition(position.UserId, symbol, position.Side, mark, info.FuturesMmrBps)
			if err != nil {
				common.SysError(fmt.Sprintf("trade futures risk: failed to liquidate the %s %s position of user %d: %v", symbol, position.Side, position.UserId, err))
				continue
			}
			if liquidated {
				common.SysLog(fmt.Sprintf("trade futures: liquidated the %s %s position of user %d at mark %s", symbol, position.Side, position.UserId, mark))
			}
			m.refreshRisk(position.UserId, symbol, position.Side)
			continue
		}
		if trigger := position.trigger(mark); trigger != "" {
			m.closeByTrigger(symbol, position, trigger)
		}
	}
}

// closeByTrigger 在止盈或止损到价时按盘口市价平掉整个仓位(先撤掉仓位挂着的平仓委托)。盘口不够时平掉能平的部分，
// 剩下的仓位留着止盈止损，下一次标记价格到来时再平。
func (m *TradeMarket) closeByTrigger(symbol string, position tradeRiskPosition, trigger string) {
	setting := operation_setting.GetTradeSetting()
	rules, ok := m.Rules(symbol)
	if !ok {
		return
	}
	pricing, _, err := tradePricing(setting)
	if err != nil {
		return
	}
	triggerPrice := position.takeProfit
	if trigger == model.TradeFuturesTriggerStopLoss {
		triggerPrice = position.stopLoss
	}
	unlock := m.lockSymbol(symbol)
	defer unlock()
	view, ok := m.bookView(symbol)
	if !ok {
		return
	}
	side := tradesim.PositionSide(position.Side).CloseSide()
	levels := view.Bids
	if side == tradesim.Buy {
		levels = view.Asks
	}
	fill := tradesim.Walk(levels, side, tradesim.QtyFromUnits(position.Qty), decimal.Zero, rules.StepSize)
	if !fill.Qty.IsPositive() {
		return
	}
	fee, err := pricing.TradeFee(fill.Notional, setting.FuturesTakerFeeBps)
	if err != nil {
		return
	}
	_, err = model.PlaceTradeFuturesOrder(model.TradeFuturesOrderInput{
		UserId:       position.UserId,
		Symbol:       symbol,
		Side:         position.Side,
		Action:       model.TradeFuturesClose,
		Type:         model.TradeOrderTypeMarket,
		Qty:          position.Qty,
		Fill:         model.TradeFuturesFill{Qty: tradesim.QtyUnits(fill.Qty), Value: fill.Notional, Fee: fee, Price: fill.AvgPrice().Round(tradesim.QtyDecimals).String()},
		Trigger:      trigger,
		TriggerPrice: triggerPrice,
	})
	switch {
	case err == nil:
		m.recordTaken(symbol, view.Version, side, levels, fill)
	case errors.Is(err, model.ErrTradeFuturesTriggerChanged), errors.Is(err, model.ErrTradeFuturesNoPosition), errors.Is(err, model.ErrTradePositionInsufficient):
		// 仓位或止盈止损刚被改过，索引是旧的。
	default:
		common.SysError(fmt.Sprintf("trade futures risk: failed to close the %s %s position of user %d by %s: %v", symbol, position.Side, position.UserId, trigger, err))
	}
	m.refreshRisk(position.UserId, symbol, position.Side)
}

// settleFunding 是主节点上的资金费结算：定时检查每个连着的合约，有仓位还没结算的已结算期次时，按 Binance 公布的费率与
// 结算标记价格逐个仓位结算。Binance 结算之后到下一次结算之前不再查，没有仓位的合约不查。
func (m *TradeMarket) settleFunding() {
	ticker := time.NewTicker(tradeFundingPoll)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.RLock()
		symbols, client := m.config.Symbols, m.client
		m.mu.RUnlock()
		if client == nil {
			continue
		}
		for _, symbol := range symbols {
			m.settleSymbolFunding(client, symbol)
		}
	}
}

func (m *TradeMarket) settleSymbolFunding(client *binance.Client, symbol string) {
	now := time.Now()
	m.fundingMu.Lock()
	quiet := m.fundingQuiet[symbol]
	m.fundingMu.Unlock()
	if now.Before(quiet) {
		return
	}
	// 查完之后到 Binance 下一次结算(再过一会儿)之前不用再查；不知道下一次结算时间时过一分钟再看。
	m.mu.RLock()
	mark, hasMark := m.marks[symbol]
	m.mu.RUnlock()
	next := now.Add(tradeFundingRetry)
	if hasMark && mark.NextFundingTime > now.UnixMilli() {
		next = time.UnixMilli(mark.NextFundingTime).Add(tradeFundingGrace)
	}
	from, has, err := model.GetTradeFuturesFundingFrom(symbol)
	if err != nil {
		common.SysError(fmt.Sprintf("trade futures funding: failed to read %s positions: %v", symbol, err))
		return
	}
	if !has {
		m.setFundingQuiet(symbol, next)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), tradeRestTimeout)
	defer cancel()
	rates, err := client.FundingRates(ctx, symbol, from+1, tradeFundingBatch)
	if err != nil {
		m.recordError(fmt.Errorf("binance funding rates %s: %w", symbol, err))
		m.setFundingQuiet(symbol, now.Add(tradeFundingRetry))
		return
	}
	settled := from
	for _, rate := range rates {
		if rate.FundingTime <= from || rate.FundingTime > now.UnixMilli() {
			continue
		}
		settled = max(settled, rate.FundingTime)
		positions, err := model.ListTradeFuturesFundingDue(symbol, rate.FundingTime)
		if err != nil {
			common.SysError(fmt.Sprintf("trade futures funding: failed to list %s positions: %v", symbol, err))
			return
		}
		for _, position := range positions {
			applied, err := model.ApplyTradeFuturesFunding(position.UserId, symbol, position.Side, rate.FundingTime, rate.Rate, rate.MarkPrice)
			if err != nil {
				common.SysError(fmt.Sprintf("trade futures funding: failed to settle %s for user %d: %v", symbol, position.UserId, err))
				continue
			}
			if applied {
				m.refreshRisk(position.UserId, symbol, position.Side)
			}
		}
	}
	switch {
	case len(rates) >= tradeFundingBatch:
		next = now
	case hasMark && mark.lastFundingTime > settled:
		// 刚过去的这一期 Binance 还没公布，过一分钟再查。
		next = now.Add(tradeFundingRetry)
	}
	m.setFundingQuiet(symbol, next)
}

func (m *TradeMarket) setFundingQuiet(symbol string, until time.Time) {
	m.fundingMu.Lock()
	m.fundingQuiet[symbol] = until
	m.fundingMu.Unlock()
}
