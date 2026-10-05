package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每日盈亏按类别拆分靠账单汇总：现货按交易对汇总买卖的资金流，合约汇总合约成交与仓位账单动过的资金(开平仓、调整保证金、
// 资金费与强平)；时间窗之外的账单不算。管理页的全站汇总同样来自这几张表。
func TestTradeFlowsAndStatsSumTheLedger(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4101, 100)
	seedTradeFuturesAccount(t, 4102, 50)

	tradeRoundTrip(t, 4101, 20, 23)
	openTradeFutures(t, 4101, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "500", 10, 0.25, nil)
	_, err := closeTradeFutures(4101, tradeFuturesSymbol, TradeFuturesLong, tradeFuturesCoin/2, "260", 0.13)
	require.NoError(t, err)
	_, err = AdjustTradeFuturesMargin(4101, tradeFuturesSymbol, TradeFuturesLong, tradeUsd(5), decimal.RequireFromString("520"), nil)
	require.NoError(t, err)
	end := common.GetTimestamp() + 1

	flows, err := ListTradeSymbolFlows([]int{4101, 4102}, 0, end)
	require.NoError(t, err)
	assert.Equal(t, []TradeSymbolFlow{{UserId: 4101, Symbol: "BTCUSDT", Amount: tradeUsd(3)}}, flows)
	// 开仓花 50 保证金加 0.25 手续费，平一半拿回 25 保证金加 10 盈利减 0.13 手续费，再追加 5 保证金。
	futuresFlow := -(tradeUsd(50) + tradeUsd(0.25)) + tradeUsd(25) + tradeUsd(10) - tradeUsd(0.13) - tradeUsd(5)
	futuresFlows, err := ListTradeFuturesFlows([]int{4101, 4102}, 0, end)
	require.NoError(t, err)
	assert.Equal(t, []TradeUserFlow{{UserId: 4101, Amount: futuresFlow}}, futuresFlows)

	flows, err = ListTradeSymbolFlows([]int{4101}, end, end+10)
	require.NoError(t, err)
	assert.Empty(t, flows)
	futuresFlows, err = ListTradeFuturesFlows([]int{4101}, end, end+10)
	require.NoError(t, err)
	assert.Empty(t, futuresFlows)

	stats, err := GetTradeStats()
	require.NoError(t, err)
	assert.EqualValues(t, 2, stats.Accounts)
	assert.Equal(t, tradeUsd(150), stats.NetIn)
	assert.Equal(t, tradeUsd(100)+tradeUsd(3)+futuresFlow+tradeUsd(50), stats.Cash+stats.Frozen)
	assert.Zero(t, stats.PositionCost)
	assert.EqualValues(t, 1, stats.FuturesPositions)
	assert.Equal(t, tradeUsd(30), stats.FuturesMargin)
}

// 同一天重复拍快照以后拍的为准；查询按日期取一段，或者取某天之前最近的一份。
func TestTradeSnapshotsUpsertByDay(t *testing.T) {
	truncateTables(t)
	require.NoError(t, SaveTradeSnapshots([]TradeSnapshot{
		{UserId: 4111, Day: "2026-10-01", Equity: 100, FuturesValue: 10, FuturesFlow: -5, CreatedAt: 1},
		{UserId: 4111, Day: "2026-10-02", Equity: 120, CreatedAt: 2},
	}))
	retaken := TradeSnapshot{UserId: 4111, Day: "2026-10-02", Equity: 130, FuturesValue: 7, FuturesFlow: 2, CreatedAt: 3}
	require.NoError(t, SaveTradeSnapshots([]TradeSnapshot{retaken}))

	snapshots, err := GetTradeSnapshots(4111, "2026-10-01", "2026-10-31")
	require.NoError(t, err)
	require.Len(t, snapshots, 2)
	assert.Equal(t, 100, snapshots[0].Equity)
	assert.Equal(t, retaken, snapshots[1])

	previous, err := GetLatestTradeSnapshotBefore(4111, "2026-10-02")
	require.NoError(t, err)
	require.NotNil(t, previous)
	assert.Equal(t, "2026-10-01", previous.Day)
	previous, err = GetLatestTradeSnapshotBefore(4111, "2026-10-01")
	require.NoError(t, err)
	assert.Nil(t, previous)

	require.NoError(t, MarkTradeSnapshotDay("2026-10-02", 1))
	require.NoError(t, MarkTradeSnapshotDay("2026-10-02", 1), "marking the same day again is a no-op")
	done, err := HasTradeSnapshotDay("2026-10-02")
	require.NoError(t, err)
	assert.True(t, done)
}
