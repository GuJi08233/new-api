package model

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tradeFuturesSymbol = "BTCUSDT"

// tradeFuturesCoin 是 1 个币的数量(10^-8)。
const tradeFuturesCoin = int64(100_000_000)

func seedTradeFuturesAccount(t *testing.T, userId int, usd float64) {
	t.Helper()
	seedTradeUser(t, userId, usd)
	_, err := TransferQuotaToTrade(userId, tradeUsd(usd))
	require.NoError(t, err)
}

// openTradeFutures 按 value 的成交价值市价开仓 qty。
func openTradeFutures(t *testing.T, userId int, side string, qty int64, value string, leverage int, feeUsd float64) {
	t.Helper()
	_, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: userId, Symbol: tradeFuturesSymbol, Side: side, Action: TradeFuturesOpen,
		Type: TradeOrderTypeMarket, Leverage: leverage, Qty: qty,
		Fill: TradeFuturesFill{Qty: qty, Value: decimal.RequireFromString(value), Fee: tradeUsd(feeUsd), Price: "1"}})
	require.NoError(t, err)
}

func closeTradeFutures(side string, userId int, qty int64, value string, feeUsd float64) (*TradeFuturesOrder, error) {
	return PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: userId, Symbol: tradeFuturesSymbol, Side: side, Action: TradeFuturesClose,
		Type: TradeOrderTypeMarket, Qty: qty, Fill: TradeFuturesFill{Qty: qty, Value: decimal.RequireFromString(value), Fee: tradeUsd(feeUsd), Price: "1"}})
}

func tradeFuturesHistoryOf(t *testing.T, userId int) []TradeFuturesHistory {
	t.Helper()
	items, _, err := GetTradeFuturesHistory(userId, "", 0, 10)
	require.NoError(t, err)
	return items
}

// 开仓从可用资金拿出保证金和手续费，平仓按比例释放保证金并结算盈亏；平完时这一段写进历史，净盈亏正好是可用资金的变化。
func TestTradeFuturesOpenAndCloseSettleMarginAndPnl(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4001, 100)

	openTradeFutures(t, 4001, TradeFuturesLong, tradeFuturesCoin, "500", 10, 0.25)
	position, err := GetTradeFuturesPosition(4001, tradeFuturesSymbol, TradeFuturesLong)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(50), position.Margin, "500 USDT at 10x")
	assert.Equal(t, tradeUsd(49.75), tradeTestAccount(t, 4001).Cash)

	_, err = closeTradeFutures(TradeFuturesLong, 4001, tradeFuturesCoin*4/10, "220", 0.11)
	require.NoError(t, err)
	position, err = GetTradeFuturesPosition(4001, tradeFuturesSymbol, TradeFuturesLong)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(30), position.Margin)
	assert.Equal(t, "300", position.EntryValue)
	assert.Equal(t, tradeUsd(89.64), tradeTestAccount(t, 4001).Cash, "20 USDT margin + 20 USDT profit - 0.11 fee")

	_, err = closeTradeFutures(TradeFuturesLong, 4001, tradeFuturesCoin*6/10, "300", 0.15)
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
	assert.Equal(t, TradeFuturesCloseByUser, history[0].CloseReason)

	ledgers, _, err := GetTradeLedgers(4001, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, ledgers, 4)
	assert.Equal(t, TradeLedgerFuturesClose, ledgers[0].Type)
	assert.Equal(t, tradeUsd(29.85), ledgers[0].Amount)
	assert.Equal(t, tradeUsd(20), ledgers[1].Pnl)
	assert.Equal(t, TradeLedgerFuturesOpen, ledgers[2].Type)
	assert.Equal(t, -tradeUsd(50.25), ledgers[2].Amount)
}

// 平仓亏掉的超过仓位的保证金时只亏保证金，多出来的由站点承担，可用资金不会变成负数。
func TestTradeFuturesLossIsCappedAtTheMargin(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4002, 100)
	openTradeFutures(t, 4002, TradeFuturesShort, tradeFuturesCoin, "100", 10, 0)

	_, err := closeTradeFutures(TradeFuturesShort, 4002, tradeFuturesCoin*2, "250", 0)
	require.ErrorIs(t, err, ErrTradePositionInsufficient, "cannot close more than held")
	_, err = closeTradeFutures(TradeFuturesShort, 4002, tradeFuturesCoin, "125", 0.1)
	require.NoError(t, err)

	assert.Equal(t, tradeUsd(90), tradeTestAccount(t, 4002).Cash)
	history := tradeFuturesHistoryOf(t, 4002)
	require.Len(t, history, 1)
	assert.Equal(t, -tradeUsd(10), history[0].Pnl, "only the margin is lost")
	assert.Equal(t, -tradeUsd(9.9), history[0].RealizedPnl, "the loss beyond the margin and the fee is borne by the site")
}

// 强平前在锁住的仓位上重新判断，强平时撤掉它挂着的平仓委托，保证金全部亏掉，可用资金不变。
func TestTradeFuturesLiquidationLosesTheMargin(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4003, 100)
	openTradeFutures(t, 4003, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0)
	closing, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4003, Symbol: tradeFuturesSymbol, Side: TradeFuturesLong,
		Action: TradeFuturesClose, Type: TradeOrderTypeLimit, Price: "120", Qty: tradeFuturesCoin / 2, Rest: true})
	require.NoError(t, err)

	liquidated, err := LiquidateTradeFuturesPosition(4003, tradeFuturesSymbol, TradeFuturesLong, decimal.RequireFromString("95"), 50)
	require.NoError(t, err)
	assert.False(t, liquidated, "margin 10 - loss 5 is still above the maintenance margin")

	liquidated, err = LiquidateTradeFuturesPosition(4003, tradeFuturesSymbol, TradeFuturesLong, decimal.RequireFromString("90.4"), 50)
	require.NoError(t, err)
	assert.True(t, liquidated)
	assert.Equal(t, tradeUsd(90), tradeTestAccount(t, 4003).Cash)
	positions, err := GetTradeFuturesPositions(4003)
	require.NoError(t, err)
	assert.Empty(t, positions)

	orders, _, err := GetTradeFuturesOrders(4003, false, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, orders, 3)
	assert.Equal(t, TradeFuturesTriggerLiquidation, orders[0].Trigger)
	assert.Equal(t, -tradeUsd(10), orders[0].RealizedPnl)
	assert.Equal(t, closing.Id, orders[1].Id)
	assert.Equal(t, TradeCancelByPosition, orders[1].CancelReason)

	history := tradeFuturesHistoryOf(t, 4003)
	require.Len(t, history, 1)
	assert.Equal(t, TradeFuturesCloseByLiquidation, history[0].CloseReason)
	assert.Equal(t, -tradeUsd(10), history[0].Pnl)
	assert.Equal(t, "90.4", history[0].ClosePrice)

	liquidated, err = LiquidateTradeFuturesPosition(4003, tradeFuturesSymbol, TradeFuturesLong, decimal.RequireFromString("50"), 50)
	require.NoError(t, err)
	assert.False(t, liquidated, "nothing left to liquidate")
}

// 资金费记在仓位的保证金上，同一次只结算一遍，开仓之前的那次不结算；付不起时付完剩下的保证金为止。
func TestTradeFuturesFundingSettlesOnceIntoTheMargin(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4004, 200)
	openTradeFutures(t, 4004, TradeFuturesLong, tradeFuturesCoin, "1000", 10, 0)
	openTradeFutures(t, 4004, TradeFuturesShort, tradeFuturesCoin/100, "10", 10, 0)
	position, err := GetTradeFuturesPosition(4004, tradeFuturesSymbol, TradeFuturesLong)
	require.NoError(t, err)
	at := position.FundingAt

	applied, err := ApplyTradeFuturesFunding(4004, tradeFuturesSymbol, TradeFuturesLong, at-1, decimal.RequireFromString("0.0001"), decimal.RequireFromString("1000"))
	require.NoError(t, err)
	assert.False(t, applied, "funding before the position was opened")
	applied, err = ApplyTradeFuturesFunding(4004, tradeFuturesSymbol, TradeFuturesLong, at+1000, decimal.RequireFromString("0.0001"), decimal.RequireFromString("1000"))
	require.NoError(t, err)
	assert.True(t, applied)
	applied, err = ApplyTradeFuturesFunding(4004, tradeFuturesSymbol, TradeFuturesLong, at+1000, decimal.RequireFromString("0.0001"), decimal.RequireFromString("1000"))
	require.NoError(t, err)
	assert.False(t, applied, "the same funding time is settled once")

	position, err = GetTradeFuturesPosition(4004, tradeFuturesSymbol, TradeFuturesLong)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(99.9), position.Margin, "a long pays 0.01% of 1000 USDT")
	assert.Equal(t, -tradeUsd(0.1), position.Funding)

	applied, err = ApplyTradeFuturesFunding(4004, tradeFuturesSymbol, TradeFuturesShort, at+10_000, decimal.RequireFromString("-0.2"), decimal.RequireFromString("1000"))
	require.NoError(t, err)
	assert.True(t, applied)
	position, err = GetTradeFuturesPosition(4004, tradeFuturesSymbol, TradeFuturesShort)
	require.NoError(t, err)
	assert.Zero(t, position.Margin, "a short pays at most its margin")
	assert.Equal(t, -tradeUsd(1), position.Funding)
	assert.Equal(t, tradeUsd(99), tradeTestAccount(t, 4004).Cash, "funding never touches the free cash")
}

// 同一个方向上的仓位与挂着的开仓委托只能用一个杠杆；多空仓位与开仓委托加起来不能超过持仓上限。
func TestTradeFuturesLeverageAndPositionCap(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4005, 1000)
	openTradeFutures(t, 4005, TradeFuturesLong, tradeFuturesCoin, "500", 10, 0)

	_, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4005, Symbol: tradeFuturesSymbol, Side: TradeFuturesLong, Action: TradeFuturesOpen,
		Type: TradeOrderTypeMarket, Leverage: 20, Qty: tradeFuturesCoin, Fill: TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.RequireFromString("500"), Price: "500"}})
	require.ErrorIs(t, err, ErrTradeFuturesLeverageMismatch)
	_, err = PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4005, Symbol: tradeFuturesSymbol, Side: TradeFuturesShort, Action: TradeFuturesOpen,
		Type: TradeOrderTypeLimit, Leverage: 5, Price: "400", Qty: tradeFuturesCoin, Rest: true, Freeze: tradeUsd(80), Reserve: tradeUsd(400),
		MaxPositionValue: tradeUsd(1000)})
	require.NoError(t, err, "the short side has its own leverage")
	_, err = PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4005, Symbol: tradeFuturesSymbol, Side: TradeFuturesShort, Action: TradeFuturesOpen,
		Type: TradeOrderTypeMarket, Leverage: 10, Qty: tradeFuturesCoin, Fill: TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.RequireFromString("50"), Price: "50"}})
	require.ErrorIs(t, err, ErrTradeFuturesLeverageMismatch, "an open order already fixed the short leverage")
	_, err = PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4005, Symbol: tradeFuturesSymbol, Side: TradeFuturesShort, Action: TradeFuturesOpen,
		Type: TradeOrderTypeMarket, Leverage: 5, Qty: tradeFuturesCoin, Fill: TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.RequireFromString("101"), Price: "101"},
		MaxPositionValue: tradeUsd(1000)})
	require.ErrorIs(t, err, ErrTradePositionLimit, "500 held + 400 resting + 101 > 1000")

	account := tradeTestAccount(t, 4005)
	assert.Equal(t, tradeUsd(870), account.Cash)
	assert.Equal(t, tradeUsd(80), account.Frozen)
}

// 限价开仓冻结保证金与手续费，成交从冻结里付，成交完退回多冻结的部分；限价平仓占着数量，撤单后释放。
func TestTradeFuturesLimitOrdersFreezeThenFill(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4006, 100)
	order, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4006, Symbol: tradeFuturesSymbol, Side: TradeFuturesLong, Action: TradeFuturesOpen,
		Type: TradeOrderTypeLimit, Leverage: 10, Price: "100", Qty: tradeFuturesCoin * 2, Rest: true, Freeze: tradeUsd(20.04), Reserve: tradeUsd(200)})
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(79.96), tradeTestAccount(t, 4006).Cash)

	order, err = FillTradeFuturesOrder(order.Id, TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.RequireFromString("100"), Fee: tradeUsd(0.02), Price: "100"})
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusOpen, order.Status)
	assert.Equal(t, tradeUsd(10.02), order.Frozen)
	assert.Equal(t, tradeUsd(100), order.Reserved)
	order, err = FillTradeFuturesOrder(order.Id, TradeFuturesFill{Qty: tradeFuturesCoin, Value: decimal.RequireFromString("100"), Fee: tradeUsd(0.01), Price: "100"})
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusFilled, order.Status)
	account := tradeTestAccount(t, 4006)
	assert.Zero(t, account.Frozen)
	assert.Equal(t, tradeUsd(79.97), account.Cash, "the unused 0.01 USDT fee reserve comes back")
	_, err = FillTradeFuturesOrder(order.Id, TradeFuturesFill{Qty: 1, Value: decimal.RequireFromString("1"), Price: "1"})
	require.ErrorIs(t, err, ErrTradeOrderNotOpen)

	closing, err := PlaceTradeFuturesOrder(TradeFuturesOrderInput{UserId: 4006, Symbol: tradeFuturesSymbol, Side: TradeFuturesLong, Action: TradeFuturesClose,
		Type: TradeOrderTypeLimit, Price: "110", Qty: tradeFuturesCoin, Rest: true})
	require.NoError(t, err)
	assert.Equal(t, 10, closing.Leverage)
	_, err = closeTradeFutures(TradeFuturesLong, 4006, tradeFuturesCoin*2, "200", 0)
	require.ErrorIs(t, err, ErrTradePositionInsufficient, "the resting close holds one coin")
	_, err = CancelTradeFuturesOrder(4006, closing.Id, TradeCancelByUser)
	require.NoError(t, err)
	position, err := GetTradeFuturesPosition(4006, tradeFuturesSymbol, TradeFuturesLong)
	require.NoError(t, err)
	assert.Zero(t, position.FrozenQty)
	assert.Equal(t, tradeUsd(20), position.Margin)
	assert.EqualValues(t, tradeFuturesCoin*2, position.Qty)
}

// 减少保证金后保证金加上浮动亏损不能低于按标记价格算的起始保证金；追加要有可用资金。
func TestTradeFuturesAdjustMarginKeepsTheRequirement(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4007, 100)
	openTradeFutures(t, 4007, TradeFuturesLong, tradeFuturesCoin, "100", 5, 0)

	_, err := AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, tradeUsd(81), decimal.RequireFromString("100"))
	require.ErrorIs(t, err, ErrTradeCashInsufficient)
	_, err = AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, -tradeUsd(1), decimal.RequireFromString("100"))
	require.ErrorIs(t, err, ErrTradeFuturesMarginTooLow, "already at the 20 USDT needed at 100")
	position, err := AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, tradeUsd(30), decimal.RequireFromString("100"))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(50), position.Margin)
	_, err = AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, -tradeUsd(23), decimal.RequireFromString("90"))
	require.ErrorIs(t, err, ErrTradeFuturesMarginTooLow, "50 - 23 - 10 loss < 18 needed at 90")
	position, err = AdjustTradeFuturesMargin(4007, tradeFuturesSymbol, TradeFuturesLong, -tradeUsd(22), decimal.RequireFromString("90"))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(28), position.Margin)
	assert.Equal(t, tradeUsd(72), tradeTestAccount(t, 4007).Cash)
}
