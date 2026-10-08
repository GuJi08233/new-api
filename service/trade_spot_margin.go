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

// 现货借款的利率是 Binance 普通用户借 USDT 的日利率，它每小时调整一次。每个节点各自每半小时取一次(哪个节点上下单、还款都会
// 收利息)，取不到时继续用手上的。接口在 www.binance.com 的 /bapi 下，与合约风险限额档位一样。
const (
	tradeSpotRateBaseURL = "https://www.binance.com"
	tradeSpotRateRefresh = 30 * time.Minute
	tradeSpotRateRetry   = 5 * time.Minute
)

// SpotPrice 是现货的新鲜买一价，用来给借款的抵押估值：连接断了或者盘口太久没确认就不新鲜，暂停放款、转出与强平。
func (m tradeFuturesMarks) SpotPrice(symbol string) (decimal.Decimal, bool) {
	market := tradeMarket
	market.mu.RLock()
	defer market.mu.RUnlock()
	book := market.books[symbol]
	maxAge := time.Duration(operation_setting.GetTradeSetting().StaleMs) * time.Millisecond
	if maxAge <= 0 {
		maxAge = 3 * time.Second
	}
	if book == nil || !market.connected || len(book.bids) == 0 || time.Since(market.bookSyncedAt) > maxAge || !book.bids[0].Price.IsPositive() {
		return decimal.Zero, false
	}
	return book.bids[0].Price, true
}

// EnsureTradeSpotPricesFresh 在有现货借款的用户买入现货、把钱转出去、给合约或预测用之前确认一次现货盘口：这些检查按现货买一
// 算风险率，主节点每秒确认一次盘口，其他节点平时不确认。没有借款时什么都不做。
func EnsureTradeSpotPricesFresh(ctx context.Context, userId int) error {
	loan, err := model.GetTradeSpotMargin(userId)
	if err != nil {
		return err
	}
	if loan.Principal+loan.Interest == 0 {
		return nil
	}
	staleMs := operation_setting.GetTradeSetting().StaleMs
	tradeMarket.mu.RLock()
	synced := tradeMarket.bookSyncedAt
	tradeMarket.mu.RUnlock()
	if time.Since(synced) < time.Duration(staleMs)*time.Millisecond/2 {
		return nil
	}
	return tradeMarket.awaitFreshBook(ctx, time.Now(), staleMs)
}

// runTradeSpotRate 每个节点都跑，定时从 Binance 取借 USDT 的日利率。模拟盘没开杠杆、也没有人借着钱时不去取。
func runTradeSpotRate() {
	for {
		wait := tradeSpotRateRetry
		if tradeSpotRateWanted() {
			if err := refreshTradeSpotRate(); err != nil {
				common.SysError("trade spot margin: failed to refresh the borrow rate: " + err.Error())
			} else {
				wait = tradeSpotRateRefresh
			}
		}
		time.Sleep(wait)
	}
}

func tradeSpotRateWanted() bool {
	setting := operation_setting.GetTradeSetting()
	if setting.Enabled && setting.SpotMaxLeverage > 1 {
		return true
	}
	users, err := model.ListTradeSpotMarginUsers()
	return err == nil && len(users) > 0
}

func refreshTradeSpotRate() error {
	httpClient, _, err := tradeMarketTransport(operation_setting.GetTradeSetting().ProxyUrl)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), tradeRestTimeout)
	defer cancel()
	rate, err := binance.NewClient(tradeSpotRateBaseURL, httpClient).MarginDailyInterestRate(ctx, "USDT")
	if err != nil {
		return err
	}
	model.SetTradeSpotDailyRate(rate)
	return nil
}

// RunTradeSpotMarginRisk 只由主节点运行：每秒看一遍有借款的账户，收到期的利息、发追加保证金提醒、强平。关闭新交易不会免去
// 已有借款的计息和风险检查。
func RunTradeSpotMarginRisk() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		users, err := model.ListTradeSpotMarginUsers()
		if err != nil {
			common.SysError("trade spot margin: list borrowers: " + err.Error())
			continue
		}
		if len(users) == 0 {
			continue
		}
		setting := operation_setting.GetTradeSetting()
		if err = tradeMarket.awaitFreshBook(context.Background(), time.Now(), min(5000, max(100, setting.StaleMs))); err != nil {
			continue
		}
		for _, userId := range users {
			if err := settleTradeSpotMarginUser(userId, setting); err != nil && !errors.Is(err, model.ErrTradeMarkUnavailable) {
				common.SysError(fmt.Sprintf("trade spot margin: user %d: %v", userId, err))
			}
		}
	}
}

// settleTradeSpotMarginUser 先不加锁地估一遍：利息没到期、风险率高于追加保证金线(也没有要清掉的提醒记号)时什么都不做。要强平时
// 按流动性从好到差锁住持仓的交易对，拿到扣掉别的委托吃掉的数量的盘口再进事务，成交之后记下吃掉的数量。
func settleTradeSpotMarginUser(userId int, setting *operation_setting.TradeSetting) error {
	marks := tradeFuturesMarks{market: futuresMarket, maxAge: 5 * time.Second}
	risk, loan, positions, err := model.ReadTradeSpotRisk(userId, marks)
	if err != nil {
		return err
	}
	liquidate := risk.AtOrBelow(model.TradeSpotLiquidationLevel)
	called := risk.AtOrBelow(model.TradeSpotMarginCallLevel(setting.SpotMaxLeverage))
	if common.GetTimestamp() < loan.ChargedUntil && !liquidate && called == (loan.MarginCallAt != 0) {
		return nil
	}
	pricing, _, err := tradePricing(setting)
	if err != nil {
		return err
	}
	var books []model.TradeSpotBook
	views := map[string]tradeBookView{}
	if liquidate {
		for _, symbol := range tradeSpotLiquidationOrder(positions) {
			unlock := tradeMarket.lockSymbol(symbol)
			defer unlock()
			view, ok := tradeMarket.bookView(symbol)
			if !ok {
				continue
			}
			rules, _ := tradeMarket.Rules(symbol)
			views[symbol] = view
			books = append(books, model.TradeSpotBook{Symbol: symbol, Bids: view.Bids, Step: rules.StepSize})
		}
	}
	result, err := model.SettleTradeSpotMargin(userId, marks, books, pricing, setting.SpotMaxLeverage)
	if err != nil {
		return err
	}
	for symbol, fill := range result.Fills {
		view := views[symbol]
		tradeMarket.recordTaken(symbol, view.Version, tradesim.Sell, view.Bids, fill)
	}
	for _, order := range result.Canceled {
		tradeMarket.dropResting(order.Symbol, order.Id)
	}
	if result.Liquidated {
		futuresMarket.refreshRisk(userId)
	}
	return nil
}

// tradeSpotLiquidationOrder 是强平卖出现货的顺序：照 Binance 先卖流动性好的，按交易对列表的顺序(加密货币在前、美股代币在后)，
// 列表里没有的放最后。
func tradeSpotLiquidationOrder(positions []model.TradePosition) []string {
	rank := func(symbol string) int {
		for i, info := range operation_setting.TradeSymbols {
			if info.Symbol == symbol {
				return i
			}
		}
		return len(operation_setting.TradeSymbols)
	}
	symbols := make([]string, 0, len(positions))
	for _, position := range positions {
		if position.Qty > 0 && !slices.Contains(symbols, position.Symbol) {
			symbols = append(symbols, position.Symbol)
		}
	}
	slices.SortStableFunc(symbols, func(a, b string) int {
		if rank(a) != rank(b) {
			return rank(a) - rank(b)
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	})
	return symbols
}
