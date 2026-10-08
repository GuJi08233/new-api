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

// 合约仓位的风控只在主节点运行：每收到一个合约的标记价格(每秒一次)或最新成交价，就检查这个合约上的仓位：逐仓该不该强平、全仓用户的
// 账户该不该强平、止损(按标记价格)与止盈(按最新成交价)有没有到价。仓位索引定时从数据库重建，本节点改过的用户立刻重读。
// 资金费按 Binance 已经结算的费率与标记价格结算，逐仓记在仓位的保证金上，全仓进出资金。

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

// tradeRiskLevel 是风控索引里的一档止盈或止损。
type tradeRiskLevel struct {
	Id    int64
	Price decimal.Decimal
	Qty   int64
}

// tradeRiskPosition 是风控索引里的一个仓位。
type tradeRiskPosition struct {
	UserId      int
	Symbol      string
	Side        string
	MarginMode  string
	Qty         int64
	EntryValue  decimal.Decimal
	Margin      int
	TakeProfits []tradeRiskLevel
	StopLosses  []tradeRiskLevel
}

func tradeRiskPositionOf(position model.TradeFuturesPosition) (tradeRiskPosition, error) {
	entry := tradeRiskPosition{
		UserId:     position.UserId,
		Symbol:     position.Symbol,
		Side:       position.Side,
		MarginMode: position.MarginMode,
		Qty:        position.Qty,
		Margin:     position.Margin,
	}
	var err error
	if entry.EntryValue, err = decimal.NewFromString(position.EntryValue); err != nil {
		return entry, err
	}
	for _, list := range []struct {
		raw    string
		target *[]tradeRiskLevel
	}{{position.TakeProfits, &entry.TakeProfits}, {position.StopLosses, &entry.StopLosses}} {
		levels, err := model.ParseTradeFuturesLevels(list.raw)
		if err != nil {
			return entry, err
		}
		for _, level := range levels {
			price, err := decimal.NewFromString(level.Price)
			if err != nil {
				return entry, err
			}
			*list.target = append(*list.target, tradeRiskLevel{Id: level.Id, Price: price, Qty: level.Qty})
		}
	}
	return entry, nil
}

// hitTradeRiskLevels 返回价格 price 让哪几档到价以及它们要平的数量合计。rising 为真时价格涨到档位触发(多仓止盈、空仓止损)，
// 否则跌到档位触发。
func hitTradeRiskLevels(levels []tradeRiskLevel, price decimal.Decimal, rising bool) ([]int64, int64) {
	var ids []int64
	var qty int64
	for _, level := range levels {
		if rising && price.GreaterThanOrEqual(level.Price) || !rising && price.LessThanOrEqual(level.Price) {
			ids = append(ids, level.Id)
			qty += level.Qty
		}
	}
	return ids, qty
}

// signalRisk 通知风控这个合约有了新的标记价格或成交价。同一合约的多次通知合并成一次。风控只在主节点运行，其他节点不记。
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

// refreshRisk 在本节点改过一个用户的合约之后(下单、撤单、调整保证金与杠杆、止盈止损、资金费)把他的仓位与资金重新读进主节点的
// 风控索引，并检查一次相关的合约；其他节点改的由定时重建读到。
func (m *TradeMarket) refreshRisk(userId int) {
	if !m.futures || !common.IsMasterNode {
		return
	}
	positions, err := model.GetTradeFuturesPositions(userId)
	if err != nil {
		common.SysError(fmt.Sprintf("trade futures risk: failed to load the positions of user %d: %v", userId, err))
		return
	}
	account, err := model.GetTradeAccount(userId)
	if err != nil {
		common.SysError(fmt.Sprintf("trade futures risk: failed to load the account of user %d: %v", userId, err))
		return
	}
	pending, err := model.ListTradeFuturesCrossPending([]int{userId})
	if err != nil {
		common.SysError(fmt.Sprintf("trade futures risk: failed to load the cross orders of user %d: %v", userId, err))
		return
	}
	entries := make([]tradeRiskPosition, 0, len(positions))
	for _, position := range positions {
		entry, err := tradeRiskPositionOf(position)
		if err != nil {
			common.SysError(fmt.Sprintf("trade futures risk: the %s %s position of user %d is invalid: %v", position.Symbol, position.Side, userId, err))
			continue
		}
		entries = append(entries, entry)
	}
	symbols := map[string]bool{}
	m.riskMu.Lock()
	for symbol, list := range m.positions {
		before := len(list)
		list = slices.DeleteFunc(list, func(p tradeRiskPosition) bool { return p.UserId == userId })
		if len(list) != before {
			symbols[symbol] = true
		}
		m.positions[symbol] = list
	}
	var cross []tradeRiskPosition
	for _, entry := range entries {
		m.positions[entry.Symbol] = append(m.positions[entry.Symbol], entry)
		symbols[entry.Symbol] = true
		if entry.MarginMode == model.TradeFuturesCross {
			cross = append(cross, entry)
		}
	}
	if len(cross) > 0 {
		m.crossPositions[userId], m.crossCash[userId] = cross, account.Cash+pending[userId]
	} else {
		delete(m.crossPositions, userId)
		delete(m.crossCash, userId)
	}
	m.riskMu.Unlock()
	for symbol := range symbols {
		m.signalRisk(symbol)
	}
}

// reloadRisk 从数据库重建风控索引。
func (m *TradeMarket) reloadRisk() error {
	positions, err := model.ListOpenTradeFuturesPositions()
	if err != nil {
		return err
	}
	index := map[string][]tradeRiskPosition{}
	cross := map[int][]tradeRiskPosition{}
	for _, position := range positions {
		entry, err := tradeRiskPositionOf(position)
		if err != nil {
			common.SysError(fmt.Sprintf("trade futures risk: the %s %s position of user %d is invalid: %v", position.Symbol, position.Side, position.UserId, err))
			continue
		}
		index[position.Symbol] = append(index[position.Symbol], entry)
		if entry.MarginMode == model.TradeFuturesCross {
			cross[entry.UserId] = append(cross[entry.UserId], entry)
		}
	}
	userIds := make([]int, 0, len(cross))
	for userId := range cross {
		userIds = append(userIds, userId)
	}
	accounts, err := model.ListTradeAccountsOf(userIds)
	if err != nil {
		return err
	}
	pending, err := model.ListTradeFuturesCrossPending(userIds)
	if err != nil {
		return err
	}
	cash := make(map[int]int, len(accounts))
	for _, account := range accounts {
		cash[account.UserId] = account.Cash + pending[account.UserId]
	}
	m.riskMu.Lock()
	m.positions, m.crossPositions, m.crossCash = index, cross, cash
	m.riskMu.Unlock()
	return nil
}

// guard 是主节点上的合约风控：标记价格或成交价一更新就检查这个合约上的仓位，另外定时重建索引并全部检查一遍。
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

// checkRisk 用最新的标记价格与成交价检查一个合约上的仓位：全仓用户的账户该强平的整体强平；逐仓该强平的强平；没强平而止损(标记价格)
// 或止盈(最新成交价)到价的，按盘口市价平掉到价那几档的数量。索引可能是旧的，强平与止盈止损都在事务里按锁住的数据重新核对。
func (m *TradeMarket) checkRisk(symbol string) {
	mark, ok := m.freshMark(symbol, tradeRiskMarkMaxAge)
	if !ok {
		return
	}
	setting := operation_setting.GetTradeSetting()
	pricing, _, err := tradePricing(setting)
	if err != nil {
		return
	}
	m.mu.RLock()
	last, hasLast := decimal.Zero, false
	if ticker, ok := m.tickers[symbol]; ok && m.dataConnected && ticker.Close.IsPositive() {
		last, hasLast = ticker.Close, true
	}
	m.mu.RUnlock()
	brackets := TradeFuturesBrackets(symbol)
	m.riskMu.Lock()
	positions := slices.Clone(m.positions[symbol])
	m.riskMu.Unlock()

	checked := map[int]bool{}
	for _, position := range positions {
		if position.MarginMode == model.TradeFuturesCross && !checked[position.UserId] {
			checked[position.UserId] = true
			m.checkCross(position.UserId)
		}
	}
	for _, position := range positions {
		if position.MarginMode != model.TradeFuturesCross {
			hit, err := pricing.Liquidatable(tradesim.PositionSide(position.Side), position.EntryValue, position.Qty, position.Margin, mark, brackets)
			if err != nil {
				common.SysError(fmt.Sprintf("trade futures risk: failed to check the %s %s position of user %d: %v", symbol, position.Side, position.UserId, err))
				continue
			}
			if hit {
				liquidated, err := model.LiquidateTradeFuturesPosition(position.UserId, symbol, position.Side, mark, brackets, setting.FuturesTakerFeeBps)
				if err != nil {
					common.SysError(fmt.Sprintf("trade futures risk: failed to liquidate the %s %s position of user %d: %v", symbol, position.Side, position.UserId, err))
					continue
				}
				if liquidated {
					common.SysLog(fmt.Sprintf("trade futures: liquidated the %s %s position of user %d at mark %s", symbol, position.Side, position.UserId, mark))
				}
				m.refreshRisk(position.UserId)
				continue
			}
		}
		long := position.Side == model.TradeFuturesLong
		if ids, qty := hitTradeRiskLevels(position.StopLosses, mark, !long); len(ids) > 0 {
			m.closeByTrigger(position, model.TradeFuturesTriggerStopLoss, ids, min(qty, position.Qty), mark)
			continue
		}
		if !hasLast {
			continue
		}
		if ids, qty := hitTradeRiskLevels(position.TakeProfits, last, long); len(ids) > 0 {
			m.closeByTrigger(position, model.TradeFuturesTriggerTakeProfit, ids, min(qty, position.Qty), last)
		}
	}
}

// checkCross 用索引里的资金(加上全仓挂单冻结的钱)与新鲜的标记价格估一下用户的全仓：全仓权益跌到全仓维持保证金时，在事务里核对后
// 整体强平。有一个全仓合约没有新鲜的标记价格就不检查。
func (m *TradeMarket) checkCross(userId int) {
	m.riskMu.Lock()
	positions := slices.Clone(m.crossPositions[userId])
	cash := m.crossCash[userId]
	m.riskMu.Unlock()
	if len(positions) == 0 {
		return
	}
	setting := operation_setting.GetTradeSetting()
	pricing, _, err := tradePricing(setting)
	if err != nil {
		return
	}
	equity, maintenance := cash, 0
	for _, position := range positions {
		mark, ok := m.freshMark(position.Symbol, tradeRiskMarkMaxAge)
		if !ok {
			return
		}
		markValue := mark.Mul(tradesim.QtyFromUnits(position.Qty))
		pnl, err := pricing.Pnl(tradesim.PositionSide(position.Side), position.EntryValue, markValue)
		if err != nil {
			return
		}
		required, err := pricing.Maintenance(markValue, TradeFuturesBrackets(position.Symbol))
		if err != nil {
			return
		}
		equity += pnl
		maintenance += required
	}
	if equity > maintenance {
		return
	}
	liquidated, err := model.LiquidateTradeFuturesCross(userId, tradeFuturesMarks{market: m, maxAge: tradeRiskMarkMaxAge}, setting.FuturesTakerFeeBps)
	if err != nil {
		if !errors.Is(err, model.ErrTradeMarkUnavailable) {
			common.SysError(fmt.Sprintf("trade futures risk: failed to liquidate the cross positions of user %d: %v", userId, err))
		}
		return
	}
	if liquidated {
		common.SysLog(fmt.Sprintf("trade futures: liquidated the cross positions of user %d", userId))
	}
	m.refreshRisk(userId)
}

// closeByTrigger 在止盈或止损到价时按盘口市价平掉到价那几档的数量 qty(不超过仓位)，price 是让它们到价的价格。盘口连接断开时不平，
// 盘口不够时平掉能平的部分，剩下的数量还留在这几档里，下一次价格到来时再平。
func (m *TradeMarket) closeByTrigger(position tradeRiskPosition, trigger string, ids []int64, qty int64, price decimal.Decimal) {
	setting := operation_setting.GetTradeSetting()
	rules, ok := m.Rules(position.Symbol)
	if !ok || qty <= 0 {
		return
	}
	pricing, _, err := tradePricing(setting)
	if err != nil {
		return
	}
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()
	if !connected {
		return
	}
	unlock := m.lockSymbol(position.Symbol)
	defer unlock()
	view, ok := m.bookView(position.Symbol)
	if !ok {
		return
	}
	side := tradesim.PositionSide(position.Side).CloseSide()
	levels := view.Bids
	if side == tradesim.Buy {
		levels = view.Asks
	}
	fill := tradesim.Walk(levels, side, tradesim.QtyFromUnits(qty), decimal.Zero, tradeFuturesStep(rules))
	if !fill.Qty.IsPositive() {
		return
	}
	fee, err := pricing.TradeFee(fill.Notional, setting.FuturesTakerFeeBps)
	if err != nil {
		return
	}
	_, err = model.PlaceTradeFuturesOrder(model.TradeFuturesOrderInput{
		UserId:        position.UserId,
		Symbol:        position.Symbol,
		Side:          position.Side,
		Action:        model.TradeFuturesClose,
		Type:          model.TradeOrderTypeMarket,
		Qty:           qty,
		Fill:          model.TradeFuturesFill{Qty: tradesim.QtyUnits(fill.Qty), Value: fill.Notional, Fee: fee, Price: fill.AvgPrice().Round(tradesim.QtyDecimals).String()},
		Trigger:       trigger,
		TriggerLevels: ids,
		TriggerPrice:  price,
	})
	switch {
	case err == nil:
		m.recordTaken(position.Symbol, view.Version, side, levels, fill)
	case errors.Is(err, model.ErrTradeFuturesTriggerChanged), errors.Is(err, model.ErrTradeFuturesNoPosition), errors.Is(err, model.ErrTradePositionInsufficient):
		// 仓位或止盈止损刚被改过，索引是旧的。
	default:
		common.SysError(fmt.Sprintf("trade futures risk: failed to close the %s %s position of user %d by %s: %v", position.Symbol, position.Side, position.UserId, trigger, err))
	}
	m.refreshRisk(position.UserId)
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
				m.refreshRisk(position.UserId)
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
