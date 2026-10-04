package model

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/tradesim"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tradeFuturesSymbol = "BTCUSDT"
	tradeFuturesOther  = "ETHUSDT"
	// tradeFuturesCoin 是 1 个币的数量(10^-8)。
	tradeFuturesCoin = int64(100_000_000)
)

// tradeTestMarket 是测试用的合约行情：固定的标记价格；风险限额两档，名义价值 1000 以下最高 100 倍、维持保证金率 0.5%，
// 1000 以上最高 20 倍、2.5%(速算数 20)。
type tradeTestMarket map[string]string

func (m tradeTestMarket) MarkPrice(symbol string) (decimal.Decimal, bool) {
	mark, ok := m[symbol]
	if !ok {
		return decimal.Zero, false
	}
	return decimal.RequireFromString(mark), true
}

func (tradeTestMarket) Brackets(string) tradesim.Brackets {
	return tradesim.Brackets{
		{Floor: decimal.Zero, Cap: decimal.NewFromInt(1000), MaxLeverage: 100, Mmr: decimal.RequireFromString("0.005")},
		{Floor: decimal.NewFromInt(1000), Cap: decimal.NewFromInt(1_000_000), MaxLeverage: 20, Mmr: decimal.RequireFromString("0.025"), MaintAmount: decimal.NewFromInt(20)},
	}
}

func seedTradeFuturesAccount(t *testing.T, userId int, usd float64) {
	t.Helper()
	seedTradeUser(t, userId, usd)
	_, err := TransferQuotaToTrade(userId, tradeUsd(usd))
	require.NoError(t, err)
}

// openTradeFutures 以成交价值 value 市价开仓 qty，按 value / qty 查档位；market 为空时用没有标记价格的测试行情。
func openTradeFutures(t *testing.T, userId int, symbol string, mode string, side string, qty int64, value string, leverage int, feeUsd float64, market TradeFuturesMarket) {
	t.Helper()
	if market == nil {
		market = tradeTestMarket{}
	}
	fillValue := decimal.RequireFromString(value)
	_, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: userId, Symbol: symbol, Side: side, Action: TradeFuturesOpen,
		Type: TradeOrderTypeMarket, MarginMode: mode, Leverage: leverage, Qty: qty, RefPrice: fillValue.Div(tradesim.QtyFromUnits(qty)),
		Market: market, Fill: TradeFuturesFill{Qty: qty, Value: fillValue, Fee: tradeUsd(feeUsd), Price: "1"}})
	require.NoError(t, err)
}

func closeTradeFutures(userId int, symbol string, side string, qty int64, value string, feeUsd float64) (*TradeFuturesOrder, error) {
	return PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: userId, Symbol: symbol, Side: side, Action: TradeFuturesClose,
		Type: TradeOrderTypeMarket, Qty: qty, Fill: TradeFuturesFill{Qty: qty, Value: decimal.RequireFromString(value), Fee: tradeUsd(feeUsd), Price: "1"}})
}

func tradeFuturesHistoryOf(t *testing.T, userId int) []TradeFuturesHistory {
	t.Helper()
	items, _, err := GetTradeFuturesHistory(userId, "", 0, 10)
	require.NoError(t, err)
	return items
}

func tradeTestPosition(t *testing.T, userId int, symbol string, side string) *TradeFuturesPosition {
	t.Helper()
	position, err := GetTradeFuturesPosition(userId, symbol, side)
	require.NoError(t, err)
	return position
}

// 逐仓开仓从资金里拿出保证金和手续费，平仓按比例释放保证金并结算盈亏；平完时这一段写进历史，净盈亏正好是资金的变化，
// 这一段的账单都记着同一个仓位编号(第一次开仓成交的账单 id)。
func TestTradeFuturesOpenAndCloseSettleMarginAndPnl(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4001, 100)

	openTradeFutures(t, 4001, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "500", 10, 0.25, nil)
	position := tradeTestPosition(t, 4001, tradeFuturesSymbol, TradeFuturesLong)
	assert.Equal(t, tradeUsd(50), position.Margin, "500 USDT at 10x")
	assert.Equal(t, tradeUsd(49.75), tradeTestAccount(t, 4001).Cash)

	_, err := closeTradeFutures(4001, tradeFuturesSymbol, TradeFuturesLong, tradeFuturesCoin*4/10, "220", 0.11)
	require.NoError(t, err)
	position = tradeTestPosition(t, 4001, tradeFuturesSymbol, TradeFuturesLong)
	assert.Equal(t, tradeUsd(30), position.Margin)
	assert.Equal(t, "300", position.EntryValue)
	assert.Equal(t, tradeUsd(89.64), tradeTestAccount(t, 4001).Cash, "20 USDT margin + 20 USDT profit - 0.11 fee")

	_, err = closeTradeFutures(4001, tradeFuturesSymbol, TradeFuturesLong, tradeFuturesCoin*6/10, "300", 0.15)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(119.49), tradeTestAccount(t, 4001).Cash)
	positions, err := GetTradeFuturesPositions(4001)
	require.NoError(t, err)
	assert.Empty(t, positions)

	history := tradeFuturesHistoryOf(t, 4001)
	require.Len(t, history, 1)
	assert.Equal(t, "500", history[0].EntryPrice)
	assert.Equal(t, "520", history[0].ClosePrice)
	assert.Equal(t, tradeUsd(20), history[0].RealizedPnl)
	assert.Equal(t, tradeUsd(0.51), history[0].Fees)
	assert.Equal(t, tradeUsd(19.49), history[0].Pnl, "net PnL equals the change of cash")
	assert.Equal(t, tradeUsd(50), history[0].InitialMargin)
	assert.Equal(t, TradeFuturesCloseByUser, history[0].CloseReason)

	ledgers, _, err := GetTradeLedgers(4001, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, ledgers, 4)
	assert.Equal(t, TradeLedgerFuturesClose, ledgers[0].Type)
	assert.Equal(t, tradeUsd(29.85), ledgers[0].Amount)
	assert.Equal(t, tradeUsd(20), ledgers[1].Pnl)
	assert.Equal(t, TradeLedgerFuturesOpen, ledgers[2].Type)
	assert.Equal(t, -tradeUsd(50.25), ledgers[2].Amount)
	assert.Equal(t, ledgers[2].Id, history[0].PositionId, "the first opening fill names the position")
	for _, entry := range ledgers[:3] {
		assert.Equal(t, history[0].PositionId, entry.PositionId)
	}
	fills, err := GetTradeFuturesPositionFills(4001, []int{history[0].PositionId, 0})
	require.NoError(t, err)
	assert.Len(t, fills, 3, "position 0 does not pull in the spot ledger")
}

// 逐仓平仓亏掉的超过仓位的保证金时只亏保证金，多出来的由站点承担，资金不会变成负数。
func TestTradeFuturesLossIsCappedAtTheMargin(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4002, 100)
	openTradeFutures(t, 4002, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesShort, tradeFuturesCoin, "100", 10, 0, nil)

	_, err := closeTradeFutures(4002, tradeFuturesSymbol, TradeFuturesShort, tradeFuturesCoin*2, "250", 0)
	require.ErrorIs(t, err, ErrTradePositionInsufficient, "cannot close more than held")
	_, err = closeTradeFutures(4002, tradeFuturesSymbol, TradeFuturesShort, tradeFuturesCoin, "125", 0.1)
	require.NoError(t, err)

	assert.Equal(t, tradeUsd(90), tradeTestAccount(t, 4002).Cash)
	history := tradeFuturesHistoryOf(t, 4002)
	require.Len(t, history, 1)
	assert.Equal(t, -tradeUsd(10), history[0].Pnl, "only the margin is lost")
	assert.Equal(t, -tradeUsd(9.9), history[0].RealizedPnl, "the loss beyond the margin and the fee is borne by the site")
}

// 逐仓强平前在锁住的仓位上重新判断，强平时撤掉它挂着的平仓委托，按标记价格平掉，收回剩下的保证金减去吃单手续费。
func TestTradeFuturesLiquidationReturnsWhatIsLeft(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4003, 100)
	openTradeFutures(t, 4003, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, nil)
	closing, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4003, Symbol: tradeFuturesSymbol, Side: TradeFuturesLong,
		Action: TradeFuturesClose, Type: TradeOrderTypeLimit, Price: "120", Qty: tradeFuturesCoin / 2, Rest: true})
	require.NoError(t, err)
	brackets := tradeTestMarket{}.Brackets(tradeFuturesSymbol)

	liquidated, err := LiquidateTradeFuturesPosition(4003, tradeFuturesSymbol, TradeFuturesLong, decimal.RequireFromString("95"), brackets, 4)
	require.NoError(t, err)
	assert.False(t, liquidated, "margin 10 - loss 5 is still above the 0.475 maintenance margin")

	liquidated, err = LiquidateTradeFuturesPosition(4003, tradeFuturesSymbol, TradeFuturesLong, decimal.RequireFromString("90.4"), brackets, 4)
	require.NoError(t, err)
	assert.True(t, liquidated, "0.4 left is below the 0.452 maintenance margin")
	// 剩下 0.4 USDT，减去 90.4 × 0.04% = 0.03616 的手续费。
	assert.Equal(t, tradeUsd(90)+181_920, tradeTestAccount(t, 4003).Cash)
	positions, err := GetTradeFuturesPositions(4003)
	require.NoError(t, err)
	assert.Empty(t, positions)

	orders, _, err := GetTradeFuturesOrders(4003, false, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, orders, 3)
	assert.Equal(t, TradeFuturesTriggerLiquidation, orders[0].Trigger)
	assert.Equal(t, -tradeUsd(9.6), orders[0].RealizedPnl)
	assert.Equal(t, 18_080, orders[0].Fee)
	assert.Equal(t, closing.Id, orders[1].Id)
	assert.Equal(t, TradeCancelByPosition, orders[1].CancelReason)

	history := tradeFuturesHistoryOf(t, 4003)
	require.Len(t, history, 1)
	assert.Equal(t, TradeFuturesCloseByLiquidation, history[0].CloseReason)
	assert.Equal(t, -tradeUsd(9.6)-18_080, history[0].Pnl)
	assert.Equal(t, "90.4", history[0].ClosePrice)

	liquidated, err = LiquidateTradeFuturesPosition(4003, tradeFuturesSymbol, TradeFuturesLong, decimal.RequireFromString("50"), brackets, 4)
	require.NoError(t, err)
	assert.False(t, liquidated, "nothing left to liquidate")
}

// 全仓不把保证金拿出资金，只付手续费；全仓占着的保证金不能再拿去开逐仓、买现货或转出，浮动盈利也不能转出。
func TestTradeFuturesCrossOccupiesWithoutMovingCash(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4010, 100)
	market := tradeTestMarket{tradeFuturesSymbol: "500"}
	openTradeFutures(t, 4010, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "500", 10, 0.25, market)
	assert.Equal(t, tradeUsd(99.75), tradeTestAccount(t, 4010).Cash, "only the fee leaves the cash")
	assert.Equal(t, tradeUsd(50), tradeTestPosition(t, 4010, tradeFuturesSymbol, TradeFuturesLong).Margin, "50 USDT occupied")

	_, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4010, Symbol: tradeFuturesOther, Side: TradeFuturesLong, Action: TradeFuturesOpen,
		Type: TradeOrderTypeMarket, MarginMode: TradeFuturesIsolated, Leverage: 10, Qty: tradeFuturesCoin, RefPrice: decimal.NewFromInt(600), Market: market,
		Fill: TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.NewFromInt(600), Price: "600"}})
	require.ErrorIs(t, err, ErrTradeCashInsufficient, "60 isolated margin > 99.75 - 50 occupied")
	openTradeFutures(t, 4010, tradeFuturesOther, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "400", 10, 0, market)
	assert.Equal(t, tradeUsd(59.75), tradeTestAccount(t, 4010).Cash)

	_, err = PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4010, Symbol: tradeFuturesSymbol, Side: TradeFuturesShort, Action: TradeFuturesOpen,
		Type: TradeOrderTypeMarket, MarginMode: TradeFuturesIsolated, Leverage: 10, Qty: tradeFuturesCoin, RefPrice: decimal.NewFromInt(500), Market: market,
		Fill: TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.NewFromInt(500), Price: "500"}})
	require.ErrorIs(t, err, ErrTradeFuturesModeMismatch, "the contract is already traded on cross margin")

	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 4010, Symbol: "SOLUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket, Qty: tradeFuturesCoin,
		Fill: TradeFill{Qty: tradeFuturesCoin, Amount: tradeUsd(20), Price: "20"}, Market: market})
	require.ErrorIs(t, err, ErrTradeCashInsufficient, "only 9.75 is not occupied")

	_, _, err = TransferQuotaFromTrade(4010, tradeUsd(10), tradeTestDay, 0, market)
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded)
	_, _, err = TransferQuotaFromTrade(4010, tradeUsd(9.75), tradeTestDay, 0, market)
	require.NoError(t, err)
	_, _, err = TransferQuotaFromTrade(4010, tradeUsd(1), tradeTestDay, 0, tradeTestMarket{tradeFuturesSymbol: "550"})
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded, "a floating profit cannot be withdrawn")
	_, _, err = TransferQuotaFromTrade(4010, tradeUsd(1), tradeTestDay, 0, nil)
	require.ErrorIs(t, err, ErrTradeMarkUnavailable, "cross positions need a mark price")
}

// 有别的全仓仓位撑着时，平掉亏损的全仓仓位可以让资金暂时变成负的；全仓仓位都没了还是负的(跳空)由站点补到 0。
func TestTradeFuturesCrossDeficitIsCoveredWhenNoCrossPositionIsLeft(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4011, 100)
	market := tradeTestMarket{tradeFuturesSymbol: "100", tradeFuturesOther: "100"}
	openTradeFutures(t, 4011, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, market)
	openTradeFutures(t, 4011, tradeFuturesOther, TradeFuturesCross, TradeFuturesShort, tradeFuturesCoin, "100", 10, 0, market)

	_, err := closeTradeFutures(4011, tradeFuturesOther, TradeFuturesShort, tradeFuturesCoin, "300", 0)
	require.NoError(t, err)
	assert.Equal(t, -tradeUsd(100), tradeTestAccount(t, 4011).Cash, "the long still holds a 200 USDT gain")
	_, err = closeTradeFutures(4011, tradeFuturesSymbol, TradeFuturesLong, tradeFuturesCoin, "300", 0)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(100), tradeTestAccount(t, 4011).Cash)

	openTradeFutures(t, 4011, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, 10*tradeFuturesCoin, "1000", 10, 0, market)
	_, err = closeTradeFutures(4011, tradeFuturesSymbol, TradeFuturesLong, 10*tradeFuturesCoin, "850", 0)
	require.NoError(t, err)
	assert.Zero(t, tradeTestAccount(t, 4011).Cash, "the 50 USDT beyond the cash is covered")
	ledgers, _, err := GetTradeLedgers(4011, "", 0, 2)
	require.NoError(t, err)
	assert.Equal(t, TradeLedgerFuturesCover, ledgers[0].Type)
	assert.Equal(t, tradeUsd(50), ledgers[0].Amount)
	assert.Equal(t, -tradeUsd(150), ledgers[1].Amount)
}

// 资金加全仓浮动盈亏跌到全仓维持保证金时，全部全仓仓位按标记价格一起强平，全仓开仓挂单撤销，平完还是负的由站点补到 0。
func TestTradeFuturesCrossLiquidationClosesEveryCrossPosition(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4012, 100)
	market := tradeTestMarket{tradeFuturesSymbol: "100", tradeFuturesOther: "100"}
	openTradeFutures(t, 4012, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, market)
	openTradeFutures(t, 4012, tradeFuturesOther, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, market)
	resting, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4012, Symbol: tradeFuturesOther, Side: TradeFuturesLong, Action: TradeFuturesOpen,
		Type: TradeOrderTypeLimit, MarginMode: TradeFuturesCross, Leverage: 10, Price: "90", Qty: tradeFuturesCoin, Rest: true,
		Freeze: tradeUsd(9), Reserve: tradeUsd(90), RefPrice: decimal.NewFromInt(90), Market: market})
	require.NoError(t, err)

	liquidated, err := LiquidateTradeFuturesCross(4012, tradeTestMarket{tradeFuturesSymbol: "95", tradeFuturesOther: "95"}, 4)
	require.NoError(t, err)
	assert.False(t, liquidated, "91 cash - 10 loss is far above the maintenance margin")
	_, err = LiquidateTradeFuturesCross(4012, tradeTestMarket{tradeFuturesSymbol: "50"}, 4)
	require.ErrorIs(t, err, ErrTradeMarkUnavailable)

	liquidated, err = LiquidateTradeFuturesCross(4012, tradeTestMarket{tradeFuturesSymbol: "50", tradeFuturesOther: "50"}, 4)
	require.NoError(t, err)
	assert.True(t, liquidated)
	account := tradeTestAccount(t, 4012)
	assert.Zero(t, account.Cash)
	assert.Zero(t, account.Frozen)
	positions, err := GetTradeFuturesPositions(4012)
	require.NoError(t, err)
	assert.Empty(t, positions)
	orders, _, err := GetTradeFuturesOrders(4012, false, tradeFuturesOther, 0, 10)
	require.NoError(t, err)
	require.Len(t, orders, 3)
	assert.Equal(t, resting.Id, orders[1].Id)
	assert.Equal(t, TradeCancelByLiquidation, orders[1].CancelReason)
	// 91 资金 - 2 × (50 亏损 + 0.02 手续费) + 9 退回的冻结 = -0.04，由站点补上。
	ledgers, _, err := GetTradeLedgers(4012, TradeLedgerFuturesCover, 0, 1)
	require.NoError(t, err)
	require.Len(t, ledgers, 1)
	assert.Equal(t, 20_000, ledgers[0].Amount)
	history := tradeFuturesHistoryOf(t, 4012)
	require.Len(t, history, 2)
	assert.Equal(t, TradeFuturesCloseByLiquidation, history[0].CloseReason)
	assert.Equal(t, TradeFuturesCross, history[0].MarginMode)
}

// 逐仓的资金费记在仓位的保证金上，同一次只结算一遍，开仓之前的那次不结算，付不起时付完剩下的保证金为止；全仓的资金费直接进出资金。
func TestTradeFuturesFundingSettlesOnce(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4004, 300)
	market := tradeTestMarket{tradeFuturesOther: "1000"}
	openTradeFutures(t, 4004, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "1000", 10, 0, nil)
	openTradeFutures(t, 4004, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesShort, tradeFuturesCoin/100, "10", 10, 0, nil)
	openTradeFutures(t, 4004, tradeFuturesOther, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "1000", 10, 0, market)
	at := tradeTestPosition(t, 4004, tradeFuturesSymbol, TradeFuturesLong).FundingAt
	rate, mark := decimal.RequireFromString("0.0001"), decimal.RequireFromString("1000")

	applied, err := ApplyTradeFuturesFunding(4004, tradeFuturesSymbol, TradeFuturesLong, at-1, rate, mark)
	require.NoError(t, err)
	assert.False(t, applied, "funding before the position was opened")
	applied, err = ApplyTradeFuturesFunding(4004, tradeFuturesSymbol, TradeFuturesLong, at+1000, rate, mark)
	require.NoError(t, err)
	assert.True(t, applied)
	applied, err = ApplyTradeFuturesFunding(4004, tradeFuturesSymbol, TradeFuturesLong, at+1000, rate, mark)
	require.NoError(t, err)
	assert.False(t, applied, "the same funding time is settled once")
	position := tradeTestPosition(t, 4004, tradeFuturesSymbol, TradeFuturesLong)
	assert.Equal(t, tradeUsd(99.9), position.Margin, "a long pays 0.01% of 1000 USDT")
	assert.Equal(t, -tradeUsd(0.1), position.Funding)

	applied, err = ApplyTradeFuturesFunding(4004, tradeFuturesSymbol, TradeFuturesShort, at+10_000, decimal.RequireFromString("-0.2"), mark)
	require.NoError(t, err)
	assert.True(t, applied)
	position = tradeTestPosition(t, 4004, tradeFuturesSymbol, TradeFuturesShort)
	assert.Zero(t, position.Margin, "a short pays at most its margin")
	assert.Equal(t, -tradeUsd(1), position.Funding)
	assert.Equal(t, tradeUsd(199), tradeTestAccount(t, 4004).Cash, "isolated funding never touches the cash")

	applied, err = ApplyTradeFuturesFunding(4004, tradeFuturesOther, TradeFuturesLong, at+1000, rate, mark)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, tradeUsd(198.9), tradeTestAccount(t, 4004).Cash, "cross funding is paid from the cash")
	assert.Equal(t, tradeUsd(100), tradeTestPosition(t, 4004, tradeFuturesOther, TradeFuturesLong).Margin)
}

// 止盈止损每组最多 4 档，数量合计不超过仓位；触发时核对档位还在、价格确实到了，平掉到价那几档的数量，扣完的档位删掉。
func TestTradeFuturesTriggerLevelsCloseTheirQuantity(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4013, 100)
	openTradeFutures(t, 4013, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, 2*tradeFuturesCoin, "200", 10, 0, nil)

	_, err := SetTradeFuturesLevels(4013, tradeFuturesSymbol, TradeFuturesLong, TradeFuturesTriggerStopLoss,
		[]TradeFuturesLevel{{Price: "92", Qty: 2*tradeFuturesCoin + 1}})
	require.ErrorIs(t, err, ErrTradeFuturesLevelsInvalid, "more than the position")
	five := make([]TradeFuturesLevel, 5)
	for i := range five {
		five[i] = TradeFuturesLevel{Price: "92", Qty: 1}
	}
	_, err = SetTradeFuturesLevels(4013, tradeFuturesSymbol, TradeFuturesLong, TradeFuturesTriggerStopLoss, five)
	require.ErrorIs(t, err, ErrTradeFuturesLevelsInvalid)
	_, err = SetTradeFuturesLevels(4013, tradeFuturesSymbol, TradeFuturesLong, TradeFuturesTriggerStopLoss,
		[]TradeFuturesLevel{{Price: "92", Qty: 2 * tradeFuturesCoin}})
	require.NoError(t, err)
	position, err := SetTradeFuturesLevels(4013, tradeFuturesSymbol, TradeFuturesLong, TradeFuturesTriggerTakeProfit,
		[]TradeFuturesLevel{{Price: "110", Qty: tradeFuturesCoin / 2}, {Price: "120", Qty: tradeFuturesCoin}})
	require.NoError(t, err)
	takeProfits, err := ParseTradeFuturesLevels(position.TakeProfits)
	require.NoError(t, err)
	require.Len(t, takeProfits, 2)
	stopLosses, err := ParseTradeFuturesLevels(position.StopLosses)
	require.NoError(t, err)
	require.Len(t, stopLosses, 1)

	trigger := func(kind string, id int64, price string, qty int64, value string) error {
		_, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4013, Symbol: tradeFuturesSymbol, Side: TradeFuturesLong,
			Action: TradeFuturesClose, Type: TradeOrderTypeMarket, Qty: qty, Trigger: kind, TriggerLevels: []int64{id},
			TriggerPrice: decimal.RequireFromString(price), Fill: TradeFuturesFill{Qty: qty, Value: decimal.RequireFromString(value), Price: price}})
		return err
	}
	require.NoError(t, trigger(TradeFuturesTriggerTakeProfit, takeProfits[0].Id, "111", tradeFuturesCoin/2, "55.5"))
	position = tradeTestPosition(t, 4013, tradeFuturesSymbol, TradeFuturesLong)
	assert.EqualValues(t, tradeFuturesCoin*3/2, position.Qty)
	left, err := ParseTradeFuturesLevels(position.TakeProfits)
	require.NoError(t, err)
	require.Len(t, left, 1)
	assert.Equal(t, "120", left[0].Price)
	assert.Equal(t, tradeUsd(90.5), tradeTestAccount(t, 4013).Cash, "5 margin + 5.5 profit")

	require.ErrorIs(t, trigger(TradeFuturesTriggerTakeProfit, takeProfits[0].Id, "111", tradeFuturesCoin/2, "55.5"), ErrTradeFuturesTriggerChanged)
	require.ErrorIs(t, trigger(TradeFuturesTriggerStopLoss, stopLosses[0].Id, "95", tradeFuturesCoin, "95"), ErrTradeFuturesTriggerChanged,
		"95 has not reached the 92 stop")
	require.NoError(t, trigger(TradeFuturesTriggerStopLoss, stopLosses[0].Id, "91", tradeFuturesCoin*3/2, "136.5"))
	assert.Zero(t, tradeTestPosition(t, 4013, tradeFuturesSymbol, TradeFuturesLong).Qty)
	assert.Equal(t, tradeUsd(92), tradeTestAccount(t, 4013).Cash, "15 margin - 13.5 loss")
	history := tradeFuturesHistoryOf(t, 4013)
	require.Len(t, history, 1)
	assert.Equal(t, TradeFuturesTriggerStopLoss, history[0].CloseReason)
}

// 同一个合约上多空仓位与开仓挂单只能用一种保证金模式、一个杠杆；杠杆不能超过开仓后名义价值所在档位的上限；
// 多空仓位与开仓委托加起来不能超过持仓上限。
func TestTradeFuturesModeLeverageAndCapsPerContract(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4005, 1000)
	market := tradeTestMarket{}
	openTradeFutures(t, 4005, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "500", 10, 0, market)

	open := func(side string, mode string, leverage int, value string, maxValue int) error {
		_, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4005, Symbol: tradeFuturesSymbol, Side: side, Action: TradeFuturesOpen,
			Type: TradeOrderTypeMarket, MarginMode: mode, Leverage: leverage, Qty: tradeFuturesCoin, RefPrice: decimal.RequireFromString(value),
			Market: market, MaxPositionValue: maxValue, Fill: TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.RequireFromString(value), Price: value}})
		return err
	}
	require.ErrorIs(t, open(TradeFuturesShort, TradeFuturesIsolated, 5, "50", 0), ErrTradeFuturesLeverageMismatch, "the short shares the contract leverage")
	require.ErrorIs(t, open(TradeFuturesShort, TradeFuturesCross, 10, "50", 0), ErrTradeFuturesModeMismatch)
	_, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4005, Symbol: tradeFuturesSymbol, Side: TradeFuturesShort, Action: TradeFuturesOpen,
		Type: TradeOrderTypeLimit, MarginMode: TradeFuturesIsolated, Leverage: 10, Price: "400", Qty: tradeFuturesCoin, Rest: true,
		Freeze: tradeUsd(40), Reserve: tradeUsd(400), RefPrice: decimal.NewFromInt(400), Market: market, MaxPositionValue: tradeUsd(1000)})
	require.NoError(t, err)
	require.ErrorIs(t, open(TradeFuturesShort, TradeFuturesIsolated, 10, "101", tradeUsd(1000)), ErrTradePositionLimit, "500 held + 400 resting + 101 > 1000")

	_, err = PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4005, Symbol: tradeFuturesOther, Side: TradeFuturesLong, Action: TradeFuturesOpen,
		Type: TradeOrderTypeMarket, MarginMode: TradeFuturesIsolated, Leverage: 50, Qty: 30 * tradeFuturesCoin, RefPrice: decimal.NewFromInt(100),
		Market: market, Fill: TradeFuturesFill{Qty: 30 * tradeFuturesCoin, Value: decimal.NewFromInt(3000), Price: "100"}})
	require.ErrorIs(t, err, ErrTradeFuturesLeverageTooHigh, "3000 USDT is in the 20x bracket")

	account := tradeTestAccount(t, 4005)
	assert.Equal(t, tradeUsd(910), account.Cash)
	assert.Equal(t, tradeUsd(40), account.Frozen)
}

// 限价开仓冻结保证金与手续费，每次成交放回它那一份冻结、付掉实际的保证金和手续费，成交完退回多冻结的部分；限价平仓占着数量，撤单后释放。
func TestTradeFuturesLimitOrdersFreezeThenFill(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4006, 100)
	order, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4006, Symbol: tradeFuturesSymbol, Side: TradeFuturesLong, Action: TradeFuturesOpen,
		Type: TradeOrderTypeLimit, MarginMode: TradeFuturesIsolated, Leverage: 10, Price: "100", Qty: tradeFuturesCoin * 2, Rest: true,
		Freeze: tradeUsd(20.04), Reserve: tradeUsd(200), RefPrice: decimal.NewFromInt(100), Market: tradeTestMarket{},
		TakeProfits: []TradeFuturesLevel{{Price: "150", Qty: tradeFuturesCoin}}})
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(79.96), tradeTestAccount(t, 4006).Cash)

	order, err = FillTradeFuturesOrder(order.Id, TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.RequireFromString("100"), Fee: tradeUsd(0.02), Price: "100"}, nil)
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusOpen, order.Status)
	assert.Equal(t, tradeUsd(10.02), order.Frozen)
	assert.Equal(t, tradeUsd(100), order.Reserved)
	assert.Empty(t, order.TakeProfits, "the take profit moved to the position on the first fill")
	position := tradeTestPosition(t, 4006, tradeFuturesSymbol, TradeFuturesLong)
	takeProfits, err := ParseTradeFuturesLevels(position.TakeProfits)
	require.NoError(t, err)
	require.Len(t, takeProfits, 1)
	order, err = FillTradeFuturesOrder(order.Id, TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.RequireFromString("100"), Fee: tradeUsd(0.01), Price: "100"}, nil)
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusFilled, order.Status)
	account := tradeTestAccount(t, 4006)
	assert.Zero(t, account.Frozen)
	assert.Equal(t, tradeUsd(79.97), account.Cash, "the unused 0.01 USDT fee reserve comes back")
	_, err = FillTradeFuturesOrder(order.Id, TradeFuturesFill{Qty: 1, Value: decimal.RequireFromString("1"), Price: "1"}, nil)
	require.ErrorIs(t, err, ErrTradeOrderNotOpen)

	closing, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4006, Symbol: tradeFuturesSymbol, Side: TradeFuturesLong, Action: TradeFuturesClose,
		Type: TradeOrderTypeLimit, Price: "110", Qty: tradeFuturesCoin, Rest: true})
	require.NoError(t, err)
	assert.Equal(t, 10, closing.Leverage)
	assert.Equal(t, TradeFuturesIsolated, closing.MarginMode)
	assert.Equal(t, position.PositionId, closing.PositionId)
	_, err = closeTradeFutures(4006, tradeFuturesSymbol, TradeFuturesLong, tradeFuturesCoin*2, "200", 0)
	require.ErrorIs(t, err, ErrTradePositionInsufficient, "the resting close holds one coin")
	_, err = CancelTradeFuturesOrder(4006, closing.Id, TradeCancelByUser)
	require.NoError(t, err)
	position = tradeTestPosition(t, 4006, tradeFuturesSymbol, TradeFuturesLong)
	assert.Zero(t, position.FrozenQty)
	assert.Equal(t, tradeUsd(20), position.Margin)
	assert.EqualValues(t, tradeFuturesCoin*2, position.Qty)
}

// 减少逐仓保证金后保证金加上浮动亏损不能低于按标记价格算的起始保证金；追加要有可用资金；全仓仓位没有自己的保证金。
func TestTradeFuturesAdjustMarginKeepsTheRequirement(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4007, 100)
	openTradeFutures(t, 4007, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 5, 0, nil)
	mark := decimal.RequireFromString("100")

	_, err := AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, tradeUsd(81), mark, nil)
	require.ErrorIs(t, err, ErrTradeCashInsufficient)
	_, err = AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, -tradeUsd(1), mark, nil)
	require.ErrorIs(t, err, ErrTradeFuturesMarginTooLow, "already at the 20 USDT needed at 100")
	position, err := AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, tradeUsd(30), mark, nil)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(50), position.Margin)
	_, err = AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, -tradeUsd(23), decimal.RequireFromString("90"), nil)
	require.ErrorIs(t, err, ErrTradeFuturesMarginTooLow, "50 - 23 - 10 loss < 18 needed at 90")
	position, err = AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, -tradeUsd(22), decimal.RequireFromString("90"), nil)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(28), position.Margin)
	assert.Equal(t, tradeUsd(72), tradeTestAccount(t, 4007).Cash)

	market := tradeTestMarket{tradeFuturesOther: "100"}
	openTradeFutures(t, 4007, tradeFuturesOther, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin/10, "10", 10, 0, market)
	_, err = AdjustTradeFuturesMargin(4007, tradeFuturesOther, TradeFuturesLong, tradeUsd(1), mark, market)
	require.ErrorIs(t, err, ErrTradeFuturesCrossMargin)
}

// 调整杠杆对一个合约的多空仓位一起生效：逐仓只能调高，多出来的保证金退回资金；全仓重算占用，变多时全仓可用要够；
// 有开仓挂单时不能调；不能超过档位上限。
func TestTradeFuturesAdjustLeverage(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4014, 100)
	market := tradeTestMarket{tradeFuturesSymbol: "100", tradeFuturesOther: "100"}
	openTradeFutures(t, 4014, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 5, 0, market)

	positions, err := AdjustTradeFuturesLeverage(4014, tradeFuturesSymbol, 10, market)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, tradeUsd(10), positions[0].Margin)
	assert.Equal(t, tradeUsd(90), tradeTestAccount(t, 4014).Cash, "the 10 USDT no longer needed comes back")
	_, err = AdjustTradeFuturesLeverage(4014, tradeFuturesSymbol, 5, market)
	require.ErrorIs(t, err, ErrTradeFuturesLeverageDown)
	_, err = AdjustTradeFuturesLeverage(4014, tradeFuturesSymbol, 150, market)
	require.ErrorIs(t, err, ErrTradeFuturesLeverageTooHigh)
	_, err = AdjustTradeFuturesLeverage(4014, "SOLUSDT", 3, market)
	require.ErrorIs(t, err, ErrTradeFuturesNoPosition)

	openTradeFutures(t, 4014, tradeFuturesOther, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, market)
	_, err = AdjustTradeFuturesLeverage(4014, tradeFuturesOther, 1, market)
	require.ErrorIs(t, err, ErrTradeCashInsufficient, "90 more occupied > 80 available")
	positions, err = AdjustTradeFuturesLeverage(4014, tradeFuturesOther, 2, market)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(50), positions[0].Margin)
	assert.Equal(t, tradeUsd(90), tradeTestAccount(t, 4014).Cash, "cross leverage only changes what is occupied")

	_, err = PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4014, Symbol: tradeFuturesOther, Side: TradeFuturesLong, Action: TradeFuturesOpen,
		Type: TradeOrderTypeLimit, MarginMode: TradeFuturesCross, Leverage: 2, Price: "90", Qty: tradeFuturesCoin / 10, Rest: true,
		Freeze: tradeUsd(4.5), Reserve: tradeUsd(9), RefPrice: decimal.NewFromInt(90), Market: market})
	require.NoError(t, err)
	_, err = AdjustTradeFuturesLeverage(4014, tradeFuturesOther, 3, market)
	require.ErrorIs(t, err, ErrTradeFuturesOpenOrders)
}
