package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tradeTestDay = "2026-10-03"

// tradeUsd 把美元换成额度单位，1 USDT = 1 美元额度。
func tradeUsd(usd float64) int {
	return common.QuotaFromFloat(usd * common.QuotaPerUnit)
}

func seedTradeUser(t *testing.T, id int, quotaUsd float64) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: id, Username: "trade-user-" + strconv.Itoa(id), AffCode: "trade-aff-" + strconv.Itoa(id), Quota: tradeUsd(quotaUsd)}).Error)
}

// tradeRoundTrip 买入 1 个单位再卖出，模拟一次盈利或亏损的交易，不收手续费。
func tradeRoundTrip(t *testing.T, userId int, buyUsd float64, sellUsd float64) {
	t.Helper()
	const qty = 100_000_000
	_, err := PlaceTradeOrder(TradeOrderInput{UserId: userId, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket, Qty: qty,
		Fill: TradeFill{Qty: qty, Amount: tradeUsd(buyUsd), Price: "1"}})
	require.NoError(t, err)
	_, err = PlaceTradeOrder(TradeOrderInput{UserId: userId, Symbol: "BTCUSDT", Side: TradeSideSell, Type: TradeOrderTypeMarket, Qty: qty,
		Fill: TradeFill{Qty: qty, Amount: tradeUsd(sellUsd), Price: "1"}})
	require.NoError(t, err)
}

func tradeTestAccount(t *testing.T, userId int) TradeAccount {
	t.Helper()
	account, err := GetTradeAccount(userId)
	require.NoError(t, err)
	return account
}

// 额度转入转出与主钱包同进同退：余额不够时两边都不动。
func TestTradeQuotaTransferMovesValueAtomically(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 3001, 10)

	account, err := TransferQuotaToTrade(3001, tradeUsd(4))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(4), account.Cash)
	_, err = TransferQuotaToTrade(3001, tradeUsd(7))
	require.ErrorIs(t, err, ErrTradeQuotaInsufficient)

	quota, err := GetUserQuota(3001, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(6), quota)
	assert.Equal(t, tradeUsd(4), tradeTestAccount(t, 3001).Cash)

	_, profit, err := TransferQuotaFromTrade(3001, tradeUsd(1.5), tradeTestDay, tradeUsd(1), nil)
	require.NoError(t, err)
	assert.Zero(t, profit, "转回自己转入的额度不算盈利")
	quota, err = GetUserQuota(3001, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(7.5), quota)
	stored := tradeTestAccount(t, 3001)
	assert.Equal(t, tradeUsd(2.5), stored.Cash)
	assert.Equal(t, tradeUsd(2.5), stored.QuotaPrincipal)

	ledgers, total, err := GetTradeLedgers(3001, "", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	assert.Equal(t, TradeLedgerQuotaOut, ledgers[0].Type)
	assert.Equal(t, -tradeUsd(1.5), ledgers[0].Amount)
	assert.Equal(t, tradeUsd(2.5), ledgers[0].Balance)
}

// 只有可用资金能转出；超过转入额度的部分算盈利，受每天的上限约束。
func TestTradeProfitLeavesUnderTheDailyCap(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 3002, 10)
	_, err := TransferQuotaToTrade(3002, tradeUsd(10))
	require.NoError(t, err)
	tradeRoundTrip(t, 3002, 10, 15)

	_, _, err = TransferQuotaFromTrade(3002, tradeUsd(16), tradeTestDay, tradeUsd(100), nil)
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded, "账户里只有 15 美元")
	_, _, err = TransferQuotaFromTrade(3002, tradeUsd(15), tradeTestDay, tradeUsd(2), nil)
	require.ErrorIs(t, err, ErrTradeProfitOutLimit, "盈利 5 美元超过当天的上限")

	_, profit, err := TransferQuotaFromTrade(3002, tradeUsd(12), tradeTestDay, tradeUsd(2), nil)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(2), profit, "先转回本金 10 美元，其余算盈利")
	_, _, err = TransferQuotaFromTrade(3002, tradeUsd(1), tradeTestDay, tradeUsd(2), nil)
	require.ErrorIs(t, err, ErrTradeProfitOutLimit, "当天的盈利额度已经用完")
	_, profit, err = TransferQuotaFromTrade(3002, tradeUsd(1), "2026-10-04", tradeUsd(2), nil)
	require.NoError(t, err, "第二天重新计算")
	assert.Equal(t, tradeUsd(1), profit)

	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3002, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket, Qty: 100_000_000,
		Fill: TradeFill{Qty: 100_000_000, Amount: tradeUsd(1.5), Price: "1.5"}})
	require.NoError(t, err)
	_, _, err = TransferQuotaFromTrade(3002, tradeUsd(1), "2026-10-04", 0, nil)
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded, "持仓里的钱要卖出之后才能转出")

	account := tradeTestAccount(t, 3002)
	assert.Equal(t, tradeUsd(0.5), account.Cash)
	assert.Zero(t, account.QuotaPrincipal)
	quota, err := GetUserQuota(3002, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(13), quota)
}

// 限价买单冻结资金，成交从冻结里付款，成交完退回多冻结的部分；已经结束的委托不能再成交。
func TestTradeLimitBuyFreezesThenFillsFromTheFreeze(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 3005, 1000)
	_, err := TransferQuotaToTrade(3005, tradeUsd(1000))
	require.NoError(t, err)

	order, err := PlaceTradeOrder(TradeOrderInput{UserId: 3005, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeLimit,
		Price: "100", Qty: 200_000_000, Rest: true, Freeze: tradeUsd(200.2)})
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusOpen, order.Status)
	account := tradeTestAccount(t, 3005)
	assert.Equal(t, tradeUsd(799.8), account.Cash)
	assert.Equal(t, tradeUsd(200.2), account.Frozen)

	order, err = FillTradeOrder(order.Id, TradeFill{Qty: 100_000_000, Amount: tradeUsd(99), Fee: tradeUsd(0.099), Price: "99"})
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusOpen, order.Status)
	order, err = FillTradeOrder(order.Id, TradeFill{Qty: 100_000_000, Amount: tradeUsd(100), Fee: tradeUsd(0.1), Price: "100"})
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusFilled, order.Status)
	assert.Zero(t, order.Frozen)
	_, err = FillTradeOrder(order.Id, TradeFill{Qty: 1, Amount: 1, Price: "100"})
	require.ErrorIs(t, err, ErrTradeOrderNotOpen)

	account = tradeTestAccount(t, 3005)
	assert.Zero(t, account.Frozen)
	assert.Equal(t, tradeUsd(800.801), account.Cash)
	positions, err := GetTradePositions(3005)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.EqualValues(t, 200_000_000, positions[0].Qty)
	assert.Equal(t, tradeUsd(199.199), positions[0].Cost)
	fills, err := GetTradeOrderFills(3005, order.Id)
	require.NoError(t, err)
	assert.Len(t, fills, 2)
}

// 下单超出可用资金、可用持仓或单个交易对的持仓上限时拒绝，账户与持仓都不变。
func TestPlaceTradeOrderRejectsOverspendAndOversell(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 3006, 10)
	_, err := TransferQuotaToTrade(3006, tradeUsd(10))
	require.NoError(t, err)

	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3006, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeLimit,
		Price: "11", Qty: 100_000_000, Rest: true, Freeze: tradeUsd(11)})
	require.ErrorIs(t, err, ErrTradeCashInsufficient)
	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3006, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket,
		Qty: 100_000_000, Fill: TradeFill{Qty: 100_000_000, Amount: tradeUsd(10), Fee: 1, Price: "10"}})
	require.ErrorIs(t, err, ErrTradeCashInsufficient)
	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3006, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket,
		Qty: 100_000_000, Fill: TradeFill{Qty: 100_000_000, Amount: tradeUsd(6), Price: "6"}, MaxPositionCost: tradeUsd(5)})
	require.ErrorIs(t, err, ErrTradePositionLimit)
	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3006, Symbol: "BTCUSDT", Side: TradeSideSell, Type: TradeOrderTypeMarket,
		Qty: 1, Fill: TradeFill{Qty: 1, Amount: 1, Price: "1"}})
	require.ErrorIs(t, err, ErrTradePositionInsufficient)

	// 挂着的买单冻结的资金也算进持仓上限。
	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3006, Symbol: "ETHUSDT", Side: TradeSideBuy, Type: TradeOrderTypeLimit,
		Price: "4", Qty: 100_000_000, Rest: true, Freeze: tradeUsd(4), MaxPositionCost: tradeUsd(5)})
	require.NoError(t, err)
	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3006, Symbol: "ETHUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket,
		Qty: 100_000_000, Fill: TradeFill{Qty: 100_000_000, Amount: tradeUsd(2), Price: "2"}, MaxPositionCost: tradeUsd(5)})
	require.ErrorIs(t, err, ErrTradePositionLimit)

	account := tradeTestAccount(t, 3006)
	assert.Equal(t, tradeUsd(6), account.Cash)
	assert.Equal(t, tradeUsd(4), account.Frozen)
	positions, err := GetTradePositions(3006)
	require.NoError(t, err)
	assert.Empty(t, positions)
}

// 市价单吃不完的部分撤销；按金额买入的市价单以实际成交为准。
func TestTradeMarketOrderRemainderIsCanceled(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 3007, 100)
	_, err := TransferQuotaToTrade(3007, tradeUsd(100))
	require.NoError(t, err)

	order, err := PlaceTradeOrder(TradeOrderInput{UserId: 3007, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket,
		Qty: 200_000_000, Fill: TradeFill{Qty: 100_000_000, Amount: tradeUsd(10), Fee: tradeUsd(0.01), Price: "10"}})
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusCanceled, order.Status)
	assert.Equal(t, TradeCancelByDepth, order.CancelReason)
	assert.EqualValues(t, 100_000_000, order.FilledQty)

	order, err = PlaceTradeOrder(TradeOrderInput{UserId: 3007, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket,
		Budget: tradeUsd(20), Fill: TradeFill{Qty: 150_000_000, Amount: tradeUsd(15), Fee: tradeUsd(0.015), Price: "10"}})
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusFilled, order.Status)
	assert.EqualValues(t, 150_000_000, order.Qty)

	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3007, Symbol: "BTCUSDT", Side: TradeSideSell, Type: TradeOrderTypeMarket, Qty: 100})
	require.ErrorIs(t, err, ErrTradeNoFill)
	assert.Equal(t, tradeUsd(100-10.01-15.015), tradeTestAccount(t, 3007).Cash)
}

// 撤单退回冻结的资金或数量，已经结束的委托不能再撤。
func TestCancelTradeOrderReleasesWhatItFroze(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 3008, 100)
	_, err := TransferQuotaToTrade(3008, tradeUsd(100))
	require.NoError(t, err)
	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3008, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket,
		Qty: 300_000_000, Fill: TradeFill{Qty: 300_000_000, Amount: tradeUsd(30), Price: "10"}})
	require.NoError(t, err)

	buy, err := PlaceTradeOrder(TradeOrderInput{UserId: 3008, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeLimit,
		Price: "9", Qty: 100_000_000, Rest: true, Freeze: tradeUsd(9.009)})
	require.NoError(t, err)
	sell, err := PlaceTradeOrder(TradeOrderInput{UserId: 3008, Symbol: "BTCUSDT", Side: TradeSideSell, Type: TradeOrderTypeLimit,
		Price: "12", Qty: 200_000_000, Rest: true})
	require.NoError(t, err)
	_, err = PlaceTradeOrder(TradeOrderInput{UserId: 3008, Symbol: "BTCUSDT", Side: TradeSideSell, Type: TradeOrderTypeMarket,
		Qty: 200_000_000, Fill: TradeFill{Qty: 200_000_000, Amount: tradeUsd(20), Price: "10"}})
	require.ErrorIs(t, err, ErrTradePositionInsufficient, "挂单冻结的数量不能再卖")

	_, err = CancelTradeOrder(3009, buy.Id, TradeCancelByUser)
	require.Error(t, err, "不能撤别人的委托")
	canceled, err := CancelTradeOrder(3008, buy.Id, TradeCancelByUser)
	require.NoError(t, err)
	assert.Equal(t, TradeOrderStatusCanceled, canceled.Status)
	_, err = CancelTradeOrder(3008, sell.Id, TradeCancelByUser)
	require.NoError(t, err)
	_, err = CancelTradeOrder(3008, sell.Id, TradeCancelByUser)
	require.ErrorIs(t, err, ErrTradeOrderNotOpen)

	account := tradeTestAccount(t, 3008)
	assert.Equal(t, tradeUsd(70), account.Cash)
	assert.Zero(t, account.Frozen)
	positions, err := GetTradePositions(3008)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Zero(t, positions[0].FrozenQty)
}
